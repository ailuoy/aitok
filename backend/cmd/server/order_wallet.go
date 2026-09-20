package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
)

// 美元使用美分，代币使用百分之一代币；历史整代币字段保持原单位。
type orderWalletQuote struct {
	UserID            int64  `json:"user_id"`
	UserEmail         string `json:"user_email"`
	AccountEmail      string `json:"account_email"`
	AccountLabel      string `json:"account_label"`
	AmountUSDMinor    int64  `json:"amount_usd_minor"`
	TokensMinor       int64  `json:"tokens_minor"`
	TokensPerUSD      int64  `json:"tokens_per_usd"`
	BalanceMinor      int64  `json:"balance_minor"`
	BalanceAfterMinor int64  `json:"balance_after_minor"`
	Version           int64  `json:"version"`
}

func (s *Server) orderWalletQuote(w http.ResponseWriter, r *http.Request, user, id int64) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	if !s.permitted(r.Context(), user, "finance") {
		w.WriteHeader(403)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	var raw json.RawMessage
	err = tx.QueryRowContext(r.Context(), `SELECT to_jsonb(o) FROM recharge_orders o WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&raw)
	var o RechargeOrder
	if err == nil {
		err = json.Unmarshal(raw, &o)
	}
	if err != nil {
		operationError(w, err)
		return
	}
	quote, err := s.walletDebitQuote(r, tx, o, 0)
	if err != nil {
		reply(w, map[string]string{"error": err.Error()}, 409)
		return
	}
	reply(w, quote, 200)
}

// 提交时先锁确认过的用户，再锁账号，避免与账号绑定操作的锁顺序冲突。
func (s *Server) walletDebitQuote(r *http.Request, tx *sql.Tx, o RechargeOrder, expectedUser int64) (orderWalletQuote, error) {
	q := orderWalletQuote{AmountUSDMinor: o.ReceivedUSDMinor, TokensPerUSD: s.billing.TokensPerUSD, Version: o.Version}
	if o.OrderStatus != "active" || o.PaymentStatus != "paid" || o.FulfillmentStatus != "completed" || o.ReceivedAt == nil || o.ReceivedUSDMinor <= 0 || o.PaymentMethod == "wallet" {
		return q, fmt.Errorf("仅已开通且已记录实收金额的正常订单可扣款；历史钱包付款不能再次扣款")
	}
	if q.TokensPerUSD <= 0 || q.AmountUSDMinor > math.MaxInt64/q.TokensPerUSD {
		return q, fmt.Errorf("代币汇率或扣款金额无效")
	}
	q.TokensMinor = q.AmountUSDMinor * q.TokensPerUSD
	accountLock := ""
	if expectedUser > 0 {
		var id int64
		if err := tx.QueryRowContext(r.Context(), `SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL AND NOT disabled FOR UPDATE`, expectedUser).Scan(&id); err != nil {
			return q, fmt.Errorf("绑定用户不可用，请刷新后重试")
		}
		accountLock = " FOR UPDATE OF a"
	}
	err := tx.QueryRowContext(r.Context(), `SELECT a.user_id,u.email,a.email,a.label FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id AND u.deleted_at IS NULL AND NOT u.disabled WHERE a.id=$1 AND a.deleted_at IS NULL`+accountLock, o.AccountID).Scan(&q.UserID, &q.UserEmail, &q.AccountEmail, &q.AccountLabel)
	if err != nil {
		return q, fmt.Errorf("账号或当前绑定用户不可用")
	}
	if expectedUser > 0 && q.UserID != expectedUser {
		return q, fmt.Errorf("账号绑定用户已改变，请重新确认扣款用户")
	}
	// 金融防重明确读取全部历史，软删除不能重新收费。
	var charged, unresolved bool
	err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM order_wallet_debits WHERE order_id=$1) OR EXISTS(SELECT 1 FROM wallet_ledger WHERE reference=$2)`, o.ID, "order:"+o.OrderNo).Scan(&charged)
	if err != nil {
		return q, err
	}
	if charged {
		return q, fmt.Errorf("此订单已经扣除钱包，不可重复扣款")
	}
	err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM payment_exceptions e JOIN topup_orders t ON t.order_no=e.order_no WHERE t.user_id=$1 AND e.kind IN ('refund','dispute') AND e.status='pending')`, q.UserID).Scan(&unresolved)
	if err != nil {
		return q, err
	}
	if unresolved {
		return q, fmt.Errorf("钱包存在待处理退款或拒付，请先核对")
	}
	lock := ""
	if expectedUser > 0 {
		lock = " FOR UPDATE"
	}
	var whole, fraction int64
	err = tx.QueryRowContext(r.Context(), `SELECT balance,balance_subunit FROM wallets WHERE user_id=$1 AND deleted_at IS NULL`+lock, q.UserID).Scan(&whole, &fraction)
	if err != nil && err != sql.ErrNoRows {
		return q, err
	}
	if whole > (math.MaxInt64-fraction)/100 {
		return q, fmt.Errorf("钱包余额超出处理范围")
	}
	q.BalanceMinor = whole*100 + fraction
	q.BalanceAfterMinor = q.BalanceMinor - q.TokensMinor
	return q, nil
}

func (s *Server) debitOrderWallet(r *http.Request, tx *sql.Tx, actor int64, o *RechargeOrder, in orderCommand) error {
	if in.ExpectedUserID < 1 {
		return fmt.Errorf("请先确认扣款用户和金额")
	}
	q, err := s.walletDebitQuote(r, tx, *o, in.ExpectedUserID)
	if err != nil {
		return err
	}
	if q.AmountUSDMinor != in.ExpectedReceivedUSDMinor || q.TokensPerUSD != in.ExpectedTokensPerUSD {
		return fmt.Errorf("实收金额或代币汇率已改变，请重新确认")
	}
	if q.BalanceAfterMinor < 0 {
		return fmt.Errorf("绑定用户钱包余额不足，请先充值")
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE wallets SET balance=$2,balance_subunit=$3,updated_at=NOW() WHERE user_id=$1 AND deleted_at IS NULL`, q.UserID, q.BalanceAfterMinor/100, q.BalanceAfterMinor%100)
	var ledgerID int64
	if err == nil {
		err = tx.QueryRowContext(r.Context(), `INSERT INTO wallet_ledger(user_id,amount,amount_subunit,balance_after,balance_after_subunit,kind,reference,description) VALUES($1,$2,$3,$4,$5,'order_consumption',$6,$7) RETURNING id`, q.UserID, -q.TokensMinor/100, -q.TokensMinor%100, q.BalanceAfterMinor/100, q.BalanceAfterMinor%100, "order:"+o.OrderNo, "账号消费 "+q.AccountEmail).Scan(&ledgerID)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO order_wallet_debits(order_id,user_id,account_id,account_email,account_label,order_no,amount_usd_minor,tokens_minor,tokens_per_usd,balance_after_minor,wallet_ledger_id,actor_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, o.ID, q.UserID, o.AccountID, q.AccountEmail, q.AccountLabel, o.OrderNo, q.AmountUSDMinor, q.TokensMinor, q.TokensPerUSD, q.BalanceAfterMinor, ledgerID, actor)
	}
	if err == nil {
		o.WalletDebit = &q
	}
	return err
}

// 独立白名单投影；不返回充值订单的成本、毛利、银行卡或操作者信息。
func (s *Server) consumptionOrders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	user, err := s.auth(r)
	if err != nil {
		w.WriteHeader(401)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	page, size, valid := pageParameters(r)
	if !valid {
		w.WriteHeader(400)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	var total int
	var balance float64
	err = tx.QueryRowContext(r.Context(), `SELECT count(*) FROM order_wallet_debits WHERE user_id=$1 AND deleted_at IS NULL`, user).Scan(&total)
	if err == nil {
		err = tx.QueryRowContext(r.Context(), `SELECT COALESCE((SELECT balance+balance_subunit/100.0 FROM wallets WHERE user_id=$1 AND deleted_at IS NULL),0)`, user).Scan(&balance)
	}
	if err != nil {
		operationError(w, err)
		return
	}
	rows, err := jsonRows(r.Context(), tx, `SELECT jsonb_build_object('id',id,'account_email',account_email,'account_label',account_label,'amount_usd_minor',amount_usd_minor,'tokens_minor',tokens_minor,'balance_after_minor',balance_after_minor,'created_at',created_at) FROM order_wallet_debits WHERE user_id=$1 AND deleted_at IS NULL ORDER BY id DESC LIMIT $2 OFFSET $3`, user, size, (page-1)*size)
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"orders": rows, "total": total, "balance": balance, "page": page, "page_size": size}, 200)
}

const accountSpentUSD = `COALESCE((SELECT sum(d.amount_usd_minor) FROM order_wallet_debits d WHERE d.account_id=a.id AND d.deleted_at IS NULL AND (d.user_id=$1 OR $2)),0)`
const orderWalletDebitJSON = `jsonb_build_object('wallet_debit',(SELECT jsonb_build_object('user_id',d.user_id,'account_email',d.account_email,'amount_usd_minor',d.amount_usd_minor,'tokens_minor',d.tokens_minor,'tokens_per_usd',d.tokens_per_usd,'balance_after_minor',d.balance_after_minor) FROM order_wallet_debits d WHERE d.order_id=o.id AND d.deleted_at IS NULL))`
