package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"
)

type ledgerEntry struct {
	ID           int64     `json:"id"`
	Amount       int64     `json:"amount"`
	BalanceAfter int64     `json:"balance_after"`
	Kind         string    `json:"kind"`
	Description  string    `json:"description"`
	CreatedAt    time.Time `json:"created_at"`
}

type topupOrder struct {
	OrderNo         string    `json:"order_no"`
	AmountMinor     int64     `json:"amount_minor"`
	Tokens          int64     `json:"tokens"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	Quantity        int64     `json:"quantity"`
	UnitAmountMinor int64     `json:"unit_amount_minor"`
}

type renewalRecord struct {
	AccountID    int64     `json:"account_id"`
	AccountLabel string    `json:"account_label"`
	Tokens       int64     `json:"tokens"`
	Months       *int      `json:"months"`
	RenewalDate  string    `json:"renewal_date"`
	CreatedAt    time.Time `json:"created_at"`
	BalanceAfter *int64    `json:"balance_after"`
}

func (s *Server) walletDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	id, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return
	}
	if _, err = s.db.ExecContext(r.Context(), `INSERT INTO wallets(user_id) VALUES($1) ON CONFLICT DO NOTHING`, id); err != nil {
		reply(w, map[string]string{"error": "钱包初始化失败"}, 500)
		return
	}
	var balance int64
	if err = s.db.QueryRowContext(r.Context(), `SELECT balance FROM wallets WHERE user_id=$1`, id).Scan(&balance); err != nil {
		reply(w, map[string]string{"error": "读取余额失败"}, 500)
		return
	}
	ledger := []ledgerEntry{}
	rows, err := s.db.QueryContext(r.Context(), `SELECT id,amount,balance_after,kind,description,created_at FROM wallet_ledger WHERE user_id=$1 ORDER BY id DESC LIMIT 100`, id)
	if err != nil {
		reply(w, map[string]string{"error": "读取钱包流水失败"}, 500)
		return
	}
	for rows.Next() {
		var entry ledgerEntry
		if err = rows.Scan(&entry.ID, &entry.Amount, &entry.BalanceAfter, &entry.Kind, &entry.Description, &entry.CreatedAt); err != nil {
			break
		}
		ledger = append(ledger, entry)
	}
	rowErr := rows.Err()
	rows.Close()
	if err != nil || rowErr != nil {
		reply(w, map[string]string{"error": "读取钱包流水失败"}, 500)
		return
	}
	orders := []topupOrder{}
	rows, err = s.db.QueryContext(r.Context(), `SELECT order_no,amount_minor,tokens,status,created_at,quantity,COALESCE(unit_amount_minor,amount_minor) FROM topup_orders WHERE user_id=$1 ORDER BY created_at DESC LIMIT 100`, id)
	if err != nil {
		reply(w, map[string]string{"error": "读取充值订单失败"}, 500)
		return
	}
	for rows.Next() {
		var order topupOrder
		if err = rows.Scan(&order.OrderNo, &order.AmountMinor, &order.Tokens, &order.Status, &order.CreatedAt, &order.Quantity, &order.UnitAmountMinor); err != nil {
			break
		}
		orders = append(orders, order)
	}
	rowErr = rows.Err()
	rows.Close()
	if err != nil || rowErr != nil {
		reply(w, map[string]string{"error": "读取充值订单失败"}, 500)
		return
	}
	renewals := []renewalRecord{}
	rows, err = s.db.QueryContext(r.Context(), `SELECT r.account_id,COALESCE(r.account_label,a.label,'已删除账号 #'||r.account_id::text),r.tokens,r.months,r.renewal_date::text,r.created_at,l.balance_after FROM account_renewals r LEFT JOIN chatgpt_accounts a ON a.id=r.account_id LEFT JOIN wallet_ledger l ON l.reference='renewal:'||r.user_id::text||':'||r.request_key WHERE r.user_id=$1 ORDER BY r.created_at DESC LIMIT 100`, id)
	if err != nil {
		reply(w, map[string]string{"error": "读取账号扣款记录失败"}, 500)
		return
	}
	for rows.Next() {
		var record renewalRecord
		if err = rows.Scan(&record.AccountID, &record.AccountLabel, &record.Tokens, &record.Months, &record.RenewalDate, &record.CreatedAt, &record.BalanceAfter); err != nil {
			break
		}
		renewals = append(renewals, record)
	}
	rowErr = rows.Err()
	rows.Close()
	if err != nil || rowErr != nil {
		reply(w, map[string]string{"error": "读取账号扣款记录失败"}, 500)
		return
	}
	reply(w, map[string]any{"balance": balance, "ledger": ledger, "orders": orders, "renewals": renewals, "topup_options": s.billing.options(), "max_topup_quantity": 100, "stripe_enabled": s.billing.stripeEnabled(), "renewal_token_cost": s.billing.RenewalCost, "renewal_months": s.billing.RenewalMonths, "tokens_per_usd": s.billing.TokensPerUSD}, 200)
}

// 月末续订保留有效天数，例如 1 月 31 日续一个月到 2 月最后一天。
func addMonthsClamped(date time.Time, months int) time.Time {
	first := time.Date(date.Year(), date.Month()+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	lastDay := first.AddDate(0, 1, -1).Day()
	day := date.Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, time.UTC)
}

func (s *Server) renewAccount(w http.ResponseWriter, r *http.Request, id, aid int64) {
	var in struct {
		RequestKey     string `json:"request_key"`
		ExpectedCost   int64  `json:"expected_cost"`
		ExpectedMonths int    `json:"expected_months"`
	}
	if jsonBody(r, &in) != nil || !requestKeyPattern.MatchString(in.RequestKey) {
		reply(w, map[string]string{"error": "续订请求格式错误"}, 400)
		return
	}
	if in.ExpectedCost != s.billing.RenewalCost || in.ExpectedMonths != s.billing.RenewalMonths {
		reply(w, map[string]string{"error": "续订价格已更新，请刷新后确认"}, 409)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		reply(w, map[string]string{"error": "续订失败"}, 500)
		return
	}
	defer tx.Rollback()
	var current sql.NullTime
	var label string
	err = tx.QueryRowContext(r.Context(), `SELECT renewal_date,label FROM chatgpt_accounts WHERE id=$1 AND user_id=$2 FOR UPDATE`, aid, id).Scan(&current, &label)
	if errors.Is(err, sql.ErrNoRows) {
		reply(w, map[string]string{"error": "账号不存在或不属于当前用户"}, 404)
		return
	}
	if err != nil {
		reply(w, map[string]string{"error": "读取账号失败"}, 500)
		return
	}
	// 按用户串行化余额变更，也覆盖同一个请求键对不同账号的并发重试。
	_, err = tx.ExecContext(r.Context(), `INSERT INTO wallets(user_id) VALUES($1) ON CONFLICT DO NOTHING`, id)
	var balance int64
	if err == nil {
		err = tx.QueryRowContext(r.Context(), `SELECT balance FROM wallets WHERE user_id=$1 FOR UPDATE`, id).Scan(&balance)
	}
	if err != nil {
		reply(w, map[string]string{"error": "读取钱包失败"}, 500)
		return
	}
	var previousAccount int64
	var previousDate time.Time
	err = tx.QueryRowContext(r.Context(), `SELECT account_id,renewal_date FROM account_renewals WHERE user_id=$1 AND request_key=$2`, id, in.RequestKey).Scan(&previousAccount, &previousDate)
	if err == nil {
		if previousAccount != aid {
			reply(w, map[string]string{"error": "请求键已用于其他账号"}, 409)
			return
		}
		reply(w, map[string]any{"renewal_date": previousDate.Format("2006-01-02"), "balance": balance}, 200)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		reply(w, map[string]string{"error": "查询续订记录失败"}, 500)
		return
	}
	if balance < s.billing.RenewalCost {
		reply(w, map[string]string{"error": "钱包代币不足，请先充值"}, 409)
		return
	}
	// 业务日期以北京时间为准；未过期账号从原日期续期，过期账号从今天起算。
	now := time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600))
	base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if current.Valid && current.Time.After(base) {
		base = current.Time
	}
	date := addMonthsClamped(base, s.billing.RenewalMonths)
	balance -= s.billing.RenewalCost
	_, err = tx.ExecContext(r.Context(), `UPDATE wallets SET balance=$1 WHERE user_id=$2`, balance, id)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE chatgpt_accounts SET renewal_date=$1 WHERE id=$2`, date, aid)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO account_renewals(user_id,request_key,account_id,tokens,renewal_date,account_label,months) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, in.RequestKey, aid, s.billing.RenewalCost, date, label, s.billing.RenewalMonths)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO wallet_ledger(user_id,amount,balance_after,kind,reference,description) VALUES($1,$2,$3,'renewal',$4,$5)`, id, -s.billing.RenewalCost, balance, fmt.Sprintf("renewal:%d:%s", id, in.RequestKey), fmt.Sprintf("%s 续订 %d 个月", label, s.billing.RenewalMonths))
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		reply(w, map[string]string{"error": "续订失败，未扣除代币"}, 500)
		return
	}
	reply(w, map[string]any{"renewal_date": date.Format("2006-01-02"), "balance": balance}, 200)
}
