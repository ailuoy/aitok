package main

import (
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
	reply(w, map[string]any{"balance": balance, "ledger": ledger, "orders": orders, "renewals": renewals, "topup_options": s.billing.options(), "max_topup_quantity": 100, "stripe_enabled": s.billing.stripeEnabled(), "tokens_per_usd": s.billing.TokensPerUSD}, 200)
}
