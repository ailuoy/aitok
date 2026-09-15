package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type RechargeOrder struct {
	ID                int64           `json:"id"`
	OrderNo           string          `json:"order_no"`
	UserID            int64           `json:"user_id"`
	AccountID         int64           `json:"account_id"`
	AccountEmail      string          `json:"account_email"`
	PackageID         int64           `json:"package_id"`
	Package           RechargePackage `json:"package_snapshot"`
	PeriodStart       string          `json:"period_start"`
	PeriodEnd         string          `json:"period_end"`
	SaleUSDMinor      int64           `json:"sale_usd_minor"`
	WalletTokens      int64           `json:"wallet_tokens"`
	PaymentMethod     string          `json:"payment_method"`
	PaymentStatus     string          `json:"payment_status"`
	FulfillmentStatus string          `json:"fulfillment_status"`
	PaymentReference  string          `json:"payment_reference"`
	PurchaseReference string          `json:"purchase_reference"`
	CardID            *int64          `json:"card_id"`
	CostUSDMinor      int64           `json:"cost_usd_minor"`
	RefundedUSDMinor  int64           `json:"refunded_usd_minor"`
	RefundedTokens    int64           `json:"refunded_tokens"`
	AssigneeID        *int64          `json:"assignee_id"`
	Evidence          string          `json:"evidence"`
	FailureReason     string          `json:"failure_reason"`
	Notes             string          `json:"notes"`
	Version           int64           `json:"version"`
	VerifiedAt        *time.Time      `json:"verified_at"`
}

func (s *Server) rechargeOrders(w http.ResponseWriter, r *http.Request) {
	user, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return
	}
	admin := s.permitted(r.Context(), user, "orders") || s.permitted(r.Context(), user, "finance") || s.permitted(r.Context(), user, "refunds")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/api/orders" || r.URL.Path == "/api/orders/export" {
		if r.Method == "GET" {
			s.listOrders(w, r, user, admin)
			return
		}
		if r.Method == "POST" && r.URL.Path == "/api/orders" {
			s.createRechargeOrder(w, r, user, s.permitted(r.Context(), user, "orders"))
			return
		}
		w.WriteHeader(405)
		return
	}
	id, err := pathID(r.URL.Path, "/api/orders/")
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	if r.Method == "GET" {
		var raw json.RawMessage
		err = s.db.QueryRowContext(r.Context(), `SELECT to_jsonb(o) FROM recharge_orders o WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR $3)`, id, user, admin).Scan(&raw)
		if err != nil {
			operationError(w, err)
			return
		}
		events, err := jsonRows(r.Context(), s.db, `SELECT to_jsonb(e) FROM operation_events e WHERE entity_type='order' AND entity_id=$1 ORDER BY id DESC`, id)
		if err != nil {
			operationError(w, err)
			return
		}
		reply(w, map[string]any{"order": raw, "events": events}, 200)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	s.orderAction(w, r, user, id, s.permitted(r.Context(), user, "orders"))
}

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request, user int64, admin bool) {
	p, size, ok := pageParameters(r)
	if !ok {
		reply(w, map[string]string{"error": "查询参数无效"}, 400)
		return
	}
	filter := ` WHERE o.deleted_at IS NULL AND (o.user_id=$1 OR $2) AND strpos(lower(o.order_no||' '||o.account_email||' '||o.notes),lower($3))>0 AND ($4='' OR o.fulfillment_status=$4)`
	args := []any{user, admin, r.URL.Query().Get("q"), r.URL.Query().Get("status")}
	var total int
	if err := s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM recharge_orders o`+filter, args...).Scan(&total); err != nil {
		operationError(w, err)
		return
	}
	limit := size
	offset := (p - 1) * size
	if r.URL.Path == "/api/orders/export" {
		limit = 10000
		offset = 0
		if total > limit {
			reply(w, map[string]string{"error": "请先筛选到一万条以内再导出"}, 400)
			return
		}
	}
	rows, err := jsonRows(r.Context(), s.db, `SELECT to_jsonb(o) FROM recharge_orders o`+filter+` ORDER BY id DESC LIMIT $5 OFFSET $6`, append(args, limit, offset)...)
	if err != nil {
		operationError(w, err)
		return
	}
	if r.URL.Path == "/api/orders/export" {
		data := [][]string{}
		for _, raw := range rows {
			var o RechargeOrder
			if json.Unmarshal(raw, &o) != nil {
				operationError(w, fmt.Errorf("decode"))
				return
			}
			data = append(data, []string{o.OrderNo, o.AccountEmail, o.Package.Name, o.PeriodStart, o.PeriodEnd, o.PaymentStatus, o.FulfillmentStatus, fmt.Sprintf("%.2f", float64(o.SaleUSDMinor)/100), fmt.Sprintf("%.2f", float64(o.CostUSDMinor)/100), o.PaymentReference, o.PurchaseReference})
		}
		writeCSV(w, "recharge-orders.csv", []string{"订单号", "账号", "套餐", "周期开始", "周期结束", "收款状态", "开通状态", "售价USD", "成本USD", "收款凭证", "购买交易号"}, data)
		return
	}
	reply(w, map[string]any{"orders": rows, "total": total, "page": p, "page_size": size, "can_manage": s.permitted(r.Context(), user, "orders"), "can_finance": s.permitted(r.Context(), user, "finance"), "can_refund": s.permitted(r.Context(), user, "refunds")}, 200)
}

func (s *Server) createRechargeOrder(w http.ResponseWriter, r *http.Request, user int64, admin bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var in struct {
		AccountID            int64  `json:"account_id"`
		PackageID            int64  `json:"package_id"`
		Start                string `json:"period_start"`
		Notes                string `json:"notes"`
		RequestKey           string `json:"request_key"`
		ExpectedSaleUSDMinor *int64 `json:"expected_sale_usd_minor"`
	}
	if jsonBody(r, &in) != nil || in.AccountID < 1 || in.PackageID < 1 || !ledgerKeyPattern.MatchString(in.RequestKey) || len(in.Notes) > 2000 {
		reply(w, map[string]string{"error": "请选择账号、套餐并填写周期"}, 400)
		return
	}
	start, err := time.Parse("2006-01-02", in.Start)
	if err != nil || start.Year() < 2000 || start.Year() > 9996 {
		reply(w, map[string]string{"error": "周期开始日期无效"}, 400)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	// 账号锁覆盖并发下单；用户锁确保禁用后不能继续创建业务对象。
	var owner int64
	var email string
	err = tx.QueryRowContext(r.Context(), `SELECT a.user_id,lower(trim(a.email)) FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id WHERE a.id=$1 AND a.deleted_at IS NULL AND u.deleted_at IS NULL AND NOT u.disabled AND (a.user_id=$2 OR $3) FOR UPDATE OF a FOR SHARE OF u`, in.AccountID, user, admin).Scan(&owner, &email)
	if err != nil {
		operationError(w, err)
		return
	}
	var existing json.RawMessage
	err = tx.QueryRowContext(r.Context(), `SELECT to_jsonb(o) FROM recharge_orders o WHERE user_id=$1 AND request_key=$2`, owner, in.RequestKey).Scan(&existing)
	if err == nil {
		var old RechargeOrder
		_ = json.Unmarshal(existing, &old)
		if old.AccountID != in.AccountID || old.PackageID != in.PackageID || old.PeriodStart != in.Start || old.Notes != in.Notes {
			operationError(w, fmt.Errorf("idempotency mismatch"))
			return
		}
		reply(w, map[string]any{"order": existing}, 200)
		return
	}
	if err != sql.ErrNoRows {
		operationError(w, err)
		return
	}
	var pkg RechargePackage
	var raw json.RawMessage
	err = tx.QueryRowContext(r.Context(), `SELECT to_jsonb(p) FROM recharge_packages p WHERE id=$1 AND deleted_at IS NULL AND enabled FOR SHARE`, in.PackageID).Scan(&raw)
	if err != nil || json.Unmarshal(raw, &pkg) != nil {
		operationError(w, sql.ErrNoRows)
		return
	}
	var rate *ExchangeRate
	if pkg.AutoUSD {
		rate, err = latestExchangeRate(r.Context(), tx)
	}
	if err != nil {
		operationError(w, err)
		return
	}
	pricePackage(&pkg, rate, time.Now())
	if !pkg.PriceReady {
		reply(w, map[string]string{"error": "PHP/USD 汇率尚未就绪或超过 48 小时，请待同步成功后下单"}, 503)
		return
	}
	if in.ExpectedSaleUSDMinor != nil && *in.ExpectedSaleUSDMinor != pkg.SaleUSDMinor {
		reply(w, map[string]string{"error": "套餐美元价格已更新，请刷新后重新确认订单"}, 409)
		return
	}
	raw, err = json.Marshal(pkg)
	if err != nil {
		operationError(w, err)
		return
	}
	// 同一邮箱可能来自旧版本重复导入，按业务身份串行检查重叠周期。
	_, err = tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(hashtext('recharge-cycle'),hashtext($1))`, strconv.FormatInt(owner, 10)+":"+email)
	end := addMonthsClamped(start, pkg.Months)
	var overlaps bool
	if err == nil {
		err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM recharge_orders WHERE user_id=$1 AND account_email=$2 AND fulfillment_status<>'cancelled' AND period_start<$4 AND period_end>$3)`, owner, email, start, end).Scan(&overlaps)
	}
	if err != nil {
		operationError(w, err)
		return
	}
	if overlaps {
		reply(w, map[string]string{"error": "此账号已有重叠周期的充值订单，请处理原订单"}, 409)
		return
	}
	no := eventKey()
	var id int64
	err = tx.QueryRowContext(r.Context(), `INSERT INTO recharge_orders(order_no,user_id,account_id,account_email,package_id,package_snapshot,period_start,period_end,sale_usd_minor,wallet_tokens,notes,request_key) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`, no, owner, in.AccountID, email, in.PackageID, raw, start, end, pkg.SaleUSDMinor, pkg.WalletTokens, in.Notes, in.RequestKey).Scan(&id)
	if err == nil {
		err = recordEvent(r.Context(), tx, user, id, "order", "create", in.RequestKey, map[string]any{}, map[string]any{"order_no": no, "account_email": email, "package": pkg, "period_start": in.Start, "period_end": end.Format("2006-01-02")})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"id": id, "order_no": no}, 201)
}

type orderCommand struct {
	Action     string `json:"action"`
	RequestKey string `json:"request_key"`
	Version    int64  `json:"version"`
	Method     string `json:"method"`
	Reference  string `json:"reference"`
	Evidence   string `json:"evidence"`
	Reason     string `json:"reason"`
	Amount     string `json:"amount_usd"`
	CardID     int64  `json:"card_id"`
	AssigneeID int64  `json:"assignee_id"`
	Success    bool   `json:"success"`
	Plan       string `json:"plan"`
	End        string `json:"period_end"`
}

func (s *Server) orderAction(w http.ResponseWriter, r *http.Request, user, id int64, admin bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	var in orderCommand
	if jsonBody(r, &in) != nil || !ledgerKeyPattern.MatchString(in.RequestKey) || len(in.Evidence) > 4000 || len(in.Reference) > 200 || len(in.Reason) > 2000 {
		reply(w, map[string]string{"error": "操作参数无效"}, 400)
		return
	}
	in.Reference = strings.TrimSpace(in.Reference)
	in.Evidence = strings.TrimSpace(in.Evidence)
	finance := s.permitted(r.Context(), user, "finance")
	refund := s.permitted(r.Context(), user, "refunds")
	if (in.Action == "collect" && in.Method != "wallet" && !finance) || (in.Action == "purchase" && !finance) || (in.Action == "refund" && !refund) || ((in.Action == "verify" || in.Action == "assign" || in.Action == "retry") && !admin) {
		reply(w, map[string]string{"error": "没有此操作权限"}, 403)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	var raw json.RawMessage
	err = tx.QueryRowContext(r.Context(), `SELECT to_jsonb(o) FROM recharge_orders o WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR $3) FOR UPDATE`, id, user, admin || finance || refund).Scan(&raw)
	if err != nil {
		operationError(w, err)
		return
	}
	var o RechargeOrder
	if json.Unmarshal(raw, &o) != nil {
		operationError(w, fmt.Errorf("decode"))
		return
	}
	before := o
	var replay json.RawMessage
	err = tx.QueryRowContext(r.Context(), `SELECT after_data FROM operation_events WHERE entity_type='order' AND entity_id=$1 AND request_key=$2`, id, in.RequestKey).Scan(&replay)
	if err == nil {
		var v struct {
			Input orderCommand `json:"input"`
		}
		_ = json.Unmarshal(replay, &v)
		if v.Input != in {
			operationError(w, fmt.Errorf("idempotency mismatch"))
			return
		}
		reply(w, map[string]any{"replayed": true, "result": replay}, 200)
		return
	}
	if err != sql.ErrNoRows {
		operationError(w, err)
		return
	}
	err = nil
	if o.Version != in.Version {
		reply(w, map[string]string{"error": "订单已被其他人更新，请刷新后重试"}, 409)
		return
	}
	fail := func(message string) { reply(w, map[string]string{"error": message}, 409) }
	switch in.Action {
	case "collect":
		if o.PaymentStatus != "unpaid" || o.FulfillmentStatus == "cancelled" {
			fail("订单已收款或已取消")
			return
		}
		if in.Method == "wallet" {
			var unresolved bool
			if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM payment_exceptions e JOIN topup_orders t ON t.order_no=e.order_no WHERE t.user_id=$1 AND e.kind IN ('refund','dispute') AND e.status='pending')`, o.UserID).Scan(&unresolved); err != nil || unresolved {
				fail("钱包存在退款或拒付待处理事项，请先联系管理员核对")
				return
			}
			if user != o.UserID {
				fail("钱包付款必须由客户本人确认")
				return
			}
			if o.WalletTokens <= 0 {
				fail("此套餐未配置钱包价格")
				return
			}
			var balance int64
			err = tx.QueryRowContext(r.Context(), `UPDATE wallets SET balance=balance-$2,updated_at=NOW() WHERE user_id=$1 AND deleted_at IS NULL AND balance>=$2 RETURNING balance`, user, o.WalletTokens).Scan(&balance)
			if err == nil {
				_, err = tx.ExecContext(r.Context(), `INSERT INTO wallet_ledger(user_id,amount,balance_after,kind,reference,description) VALUES($1,$2,$3,'recharge_order',$4,$5)`, user, -o.WalletTokens, balance, "order:"+o.OrderNo, "充值订单 "+o.OrderNo)
			}
			o.PaymentReference = "wallet:" + o.OrderNo
		} else if in.Method == "manual" && in.Reference != "" && in.Evidence != "" {
			o.PaymentReference = in.Reference
		} else {
			fail("线下收款需填写交易号和凭证；或由客户使用钱包付款")
			return
		}
		o.PaymentMethod = in.Method
		o.PaymentStatus = "paid"
		o.Evidence = in.Evidence
	case "assign":
		if o.FulfillmentStatus == "completed" || o.FulfillmentStatus == "cancelled" {
			fail("已结束订单不能重新分配")
			return
		}
		var target int64
		err = tx.QueryRowContext(r.Context(), `SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL AND NOT disabled AND (email=$2 OR (role='admin' AND (permissions IS NULL OR 'orders'=ANY(permissions)))) FOR SHARE`, in.AssigneeID, adminIdentity).Scan(&target)
		o.AssigneeID = &target
	case "purchase":
		amount, valid := parseCardUSD(in.Amount)
		if !valid || in.CardID < 1 || in.Reference == "" || in.Evidence == "" || o.PaymentStatus != "paid" || o.CostUSDMinor != 0 || o.FulfillmentStatus == "cancelled" {
			fail("请确认已收款、官网交易号、实际扣款及凭证；同一订单不能重复购买记账")
			return
		}
		err = postCardEntry(r, tx, user, in.CardID, cardPosting{Kind: "subscription", Amount: -amount, OrderID: &o.ID, AccountID: &o.AccountID, AccountEmail: o.AccountEmail, AccountLabel: o.AccountEmail, PeriodStart: o.PeriodStart, PeriodEnd: o.PeriodEnd, Currency: o.Package.Currency, OriginalAmount: o.Package.OriginalAmountMinor, Reference: in.Reference, Notes: in.Evidence, Key: in.RequestKey}, true)
		o.CardID = &in.CardID
		o.CostUSDMinor = amount
		o.PurchaseReference = in.Reference
		o.Evidence = in.Evidence
		o.FulfillmentStatus = "verifying"
	case "verify":
		if o.CostUSDMinor == 0 || (o.PaymentStatus != "paid" && o.PaymentStatus != "partial_refund") || o.FulfillmentStatus == "cancelled" || o.FulfillmentStatus == "completed" {
			fail("请先记录官网扣款，已完成或退款订单不能重复核验")
			return
		}
		if in.Evidence == "" {
			fail("请填写官网账单、订阅页面等核验凭证")
			return
		}
		if in.Success {
			if in.Plan != o.Package.Plan || in.End != o.PeriodEnd {
				fail("核验套餐及到期日必须与订单一致；不一致请记录核验失败")
				return
			}
			var previous *time.Time
			err = tx.QueryRowContext(r.Context(), `SELECT renewal_date FROM chatgpt_accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, o.AccountID).Scan(&previous)
			if err != nil {
				operationError(w, err)
				return
			}
			var verified time.Time
			err = tx.QueryRowContext(r.Context(), `UPDATE chatgpt_accounts SET verified_plan=$2,verified_at=NOW(),subscription_ends_at=$3,renewal_date=$3,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL RETURNING verified_at`, o.AccountID, in.Plan, in.End).Scan(&verified)
			if err == nil {
				_, err = tx.ExecContext(r.Context(), `INSERT INTO renewal_date_audit(account_id,admin_id,previous_date,renewal_date) VALUES($1,$2,$3,$4)`, o.AccountID, user, previous, in.End)
			}
			o.VerifiedAt = &verified
			o.FulfillmentStatus = "completed"
			o.FailureReason = ""
		} else {
			if strings.TrimSpace(in.Reason) == "" {
				fail("请填写核验失败原因")
				return
			}
			o.FulfillmentStatus = "failed"
			o.FailureReason = in.Reason
		}
		o.Evidence = in.Evidence
	case "retry":
		if o.FulfillmentStatus != "failed" || o.PaymentStatus != "paid" {
			fail("只有已收款的失败订单可重新处理")
			return
		}
		o.FulfillmentStatus = "verifying"
		if o.CostUSDMinor == 0 {
			o.FulfillmentStatus = "processing"
		}
	case "refund":
		amount, valid := parseCardUSD(in.Amount)
		if !valid || amount > o.SaleUSDMinor-o.RefundedUSDMinor || o.PaymentStatus == "unpaid" || in.Reason == "" || in.Reference == "" || in.Evidence == "" {
			fail("退款需有效金额、原因、退款交易号及凭证，不得超过剩余实收")
			return
		}
		_, err = tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(hashtext('customer-refund'),hashtext($1))`, in.Reference)
		var duplicate bool
		if err == nil {
			err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM operation_events WHERE entity_type='order' AND action='refund' AND after_data->'input'->>'reference'=$1)`, in.Reference).Scan(&duplicate)
		}
		if err != nil {
			operationError(w, err)
			return
		}
		if duplicate {
			fail("退款交易号已登记，不能重复入账")
			return
		}
		// 客户退款与卡平台退回的购买成本独立处理，不自动伪造官网退款。
		o.RefundedUSDMinor += amount
		if o.PaymentMethod == "wallet" {
			// 按累计比例计算，最后一笔补齐舍入余数。
			target := new(big.Int).Quo(new(big.Int).Mul(big.NewInt(o.WalletTokens), big.NewInt(o.RefundedUSDMinor)), big.NewInt(o.SaleUSDMinor)).Int64()
			if o.RefundedUSDMinor == o.SaleUSDMinor {
				target = o.WalletTokens
			}
			delta := target - o.RefundedTokens
			var balance int64
			err = tx.QueryRowContext(r.Context(), `UPDATE wallets SET balance=balance+$2,updated_at=NOW() WHERE user_id=$1 AND deleted_at IS NULL RETURNING balance`, o.UserID, delta).Scan(&balance)
			if err == nil {
				_, err = tx.ExecContext(r.Context(), `INSERT INTO wallet_ledger(user_id,amount,balance_after,kind,reference,description) VALUES($1,$2,$3,'order_refund',$4,$5)`, o.UserID, delta, balance, "refund:"+o.OrderNo+":"+in.RequestKey, in.Reason)
			}
			o.RefundedTokens = target
		}
		o.PaymentStatus = "partial_refund"
		if o.RefundedUSDMinor == o.SaleUSDMinor {
			o.PaymentStatus = "refunded"
		}
		o.Evidence = in.Evidence
	case "cancel":
		if user != o.UserID && !admin && !refund {
			w.WriteHeader(403)
			return
		}
		if (o.PaymentStatus != "unpaid" && o.PaymentStatus != "refunded") || o.CostUSDMinor > 0 {
			fail("已付款订单请先走退款流程，不能直接取消")
			return
		}
		o.FulfillmentStatus = "cancelled"
	default:
		reply(w, map[string]string{"error": "未知订单操作"}, 400)
		return
	}
	if err != nil {
		operationError(w, err)
		return
	}
	o.Version++
	_, err = tx.ExecContext(r.Context(), `UPDATE recharge_orders SET payment_method=$2,payment_status=$3,fulfillment_status=$4,payment_reference=$5,purchase_reference=$6,card_id=$7,cost_usd_minor=$8,refunded_usd_minor=$9,refunded_tokens=$10,assignee_id=$11,evidence=$12,failure_reason=$13,version=$14,verified_at=$15,updated_at=NOW() WHERE id=$1`, id, o.PaymentMethod, o.PaymentStatus, o.FulfillmentStatus, o.PaymentReference, o.PurchaseReference, o.CardID, o.CostUSDMinor, o.RefundedUSDMinor, o.RefundedTokens, o.AssigneeID, o.Evidence, o.FailureReason, o.Version, o.VerifiedAt)
	if err == nil {
		err = recordEvent(r.Context(), tx, user, id, "order", in.Action, in.RequestKey, before, map[string]any{"input": in, "order": o})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"order": o}, 200)
}

// 月末订阅按目标月份最后一天截断，例如 1 月 31 日续一个月到 2 月 28 日。
func addMonthsClamped(start time.Time, months int) time.Time {
	first := time.Date(start.Year(), start.Month()+time.Month(months), 1, 0, 0, 0, 0, start.Location())
	day := start.Day()
	last := first.AddDate(0, 1, -1).Day()
	if day > last {
		day = last
	}
	return first.AddDate(0, 0, day-1)
}
