package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type RechargeOrder struct {
	OrderSource          string          `json:"order_source"`
	ID                   int64           `json:"id"`
	OrderNo              string          `json:"order_no"`
	UserID               int64           `json:"user_id"`
	AccountID            int64           `json:"account_id"`
	AccountEmail         string          `json:"account_email"`
	PackageID            int64           `json:"package_id"`
	Package              RechargePackage `json:"package_snapshot"`
	PeriodStart          string          `json:"period_start"`
	PeriodEnd            string          `json:"period_end"`
	SaleUSDMinor         int64           `json:"sale_usd_minor"`
	WalletTokens         int64           `json:"wallet_tokens"`
	OrderStatus          string          `json:"order_status"`
	PaymentMethod        string          `json:"payment_method"`
	PaymentStatus        string          `json:"payment_status"`
	FulfillmentStatus    string          `json:"fulfillment_status"`
	PaymentReference     string          `json:"payment_reference"`
	PurchaseReference    string          `json:"purchase_reference"`
	CardID               *int64          `json:"card_id"`
	CostUSDMinor         int64           `json:"cost_usd_minor"`
	RefundedUSDMinor     int64           `json:"refunded_usd_minor"`
	RefundedTokens       int64           `json:"refunded_tokens"`
	AssigneeID           *int64          `json:"assignee_id"`
	Evidence             string          `json:"evidence"`
	FailureReason        string          `json:"failure_reason"`
	Notes                string          `json:"notes"`
	Version              int64           `json:"version"`
	VerifiedAt           *time.Time      `json:"verified_at"`
	ReceivedCurrency     string          `json:"received_currency"`
	ReceivedAmountMinor  int64           `json:"received_amount_minor"`
	ReceivedUSDMinor     int64           `json:"received_usd_minor"`
	ReceivedExchangeRate *CollectionRate `json:"received_exchange_rate"`
	ReceivedAt           *time.Time      `json:"received_at"`
	Profit               *OrderProfit    `json:"profit,omitempty"`
}

func (s *Server) rechargeOrders(w http.ResponseWriter, r *http.Request) {
	user, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return
	}
	admin := s.permitted(r.Context(), user, "orders") || s.permitted(r.Context(), user, "finance") || s.permitted(r.Context(), user, "refunds")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/api/orders/record" {
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		if !s.permitted(r.Context(), user, "finance") || !s.permitted(r.Context(), user, "orders") {
			w.WriteHeader(403)
			return
		}
		s.createRechargeOrder(w, r, user, true)
		return
	}
	if r.URL.Path == "/api/orders/collection-quote" {
		s.orderCollectionQuote(w, r, user, 0)
		return
	}
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
	quoteRequest := strings.HasSuffix(r.URL.Path, "/collection-quote")
	path := r.URL.Path
	if quoteRequest {
		path = strings.TrimSuffix(path, "/collection-quote")
	}
	id, err := pathID(path, "/api/orders/")
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	if quoteRequest {
		s.orderCollectionQuote(w, r, user, id)
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
		raw, err = orderWithProfit(raw)
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
	filter := ` WHERE o.deleted_at IS NULL AND (o.user_id=$1 OR $2) AND strpos(lower(o.order_no||' '||o.account_email||' '||o.notes||' '||o.order_source),lower($3))>0 AND ($4='' OR o.order_status=$4)`
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
	rows, err := jsonRows(r.Context(), s.db, `SELECT to_jsonb(o)-'evidence' FROM recharge_orders o`+filter+` ORDER BY id DESC LIMIT $5 OFFSET $6`, append(args, limit, offset)...)
	if err != nil {
		operationError(w, err)
		return
	}
	for i, raw := range rows {
		rows[i], err = orderWithProfit(raw)
		if err != nil {
			operationError(w, err)
			return
		}
	}
	if r.URL.Path == "/api/orders/export" {
		data := [][]string{}
		for _, raw := range rows {
			var o RechargeOrder
			if json.Unmarshal(raw, &o) != nil {
				operationError(w, fmt.Errorf("decode"))
				return
			}
			received, receivedUSD, profit, margin, profitBasis := "", "", "", "", ""
			if o.ReceivedCurrency != "" {
				received, receivedUSD = decimalMoney(o.ReceivedAmountMinor), decimalMoney(o.ReceivedUSDMinor)
			}
			if o.Profit != nil {
				profit, margin, profitBasis = decimalMoney(o.Profit.USDMinor), o.Profit.RatePercent, "已记账成本"
				if o.Profit.Estimated {
					profitBasis = "预计 SKU 成本"
				}
			}
			data = append(data, []string{o.OrderSource, o.OrderNo, o.AccountEmail, o.Package.Name, o.PeriodStart, o.PeriodEnd, o.OrderStatus, o.PaymentStatus, o.FulfillmentStatus, decimalMoney(o.SaleUSDMinor), decimalMoney(o.CostUSDMinor), o.ReceivedCurrency, received, receivedUSD, profit, margin, profitBasis, o.PaymentReference, o.PurchaseReference})
		}
		writeCSV(w, "recharge-orders.csv", []string{"订单来源", "订单号", "账号", "套餐", "周期开始", "周期结束", "订单状态", "收款状态", "开通状态", "售价USD", "成本USD", "实收币种", "实收金额", "实收折合USD", "毛利润USD", "毛利率%", "成本口径", "收款凭证", "购买交易号"}, data)
		return
	}
	// 下拉来源不受当前搜索和分页影响，且仅来自有权查看的未删除订单。
	var sources json.RawMessage
	err = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(jsonb_agg(source ORDER BY source),'[]'::jsonb) FROM (SELECT DISTINCT order_source AS source FROM recharge_orders WHERE deleted_at IS NULL AND (user_id=$1 OR $2) AND order_source<>'') s`, user, admin).Scan(&sources)
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"orders": rows, "sources": sources, "total": total, "page": p, "page_size": size, "can_manage": s.permitted(r.Context(), user, "orders"), "can_finance": s.permitted(r.Context(), user, "finance"), "can_refund": s.permitted(r.Context(), user, "refunds")}, 200)
}

func (s *Server) createRechargeOrder(w http.ResponseWriter, r *http.Request, user int64, admin bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxEvidenceBytes+(64<<10))
	var in struct {
		orderRecording
		AccountID            int64  `json:"account_id"`
		PackageID            int64  `json:"package_id"`
		Start                string `json:"period_start"`
		Notes                string `json:"notes"`
		RequestKey           string `json:"request_key"`
		ExpectedSaleUSDMinor *int64 `json:"expected_sale_usd_minor"`
	}
	recording := r.URL.Path == "/api/orders/record"
	if jsonBody(r, &in) != nil || in.AccountID < 1 || in.PackageID < 1 || !ledgerKeyPattern.MatchString(in.RequestKey) || len(in.Notes) > 2000 {
		reply(w, map[string]string{"error": "请选择账号、套餐并填写有效的订单信息"}, 400)
		return
	}
	if in.QuickMonth && !recording {
		reply(w, map[string]string{"error": "快速月订单请使用录入订单入口"}, 400)
		return
	}
	in.OrderSource = strings.Join(strings.Fields(in.OrderSource), " ")
	if utf8.RuneCountInString(in.OrderSource) > 80 {
		reply(w, map[string]string{"error": "订单来源不能超过 80 字"}, 400)
		return
	}
	if recording {
		if err := in.orderRecording.validate(); err != nil {
			reply(w, map[string]string{"error": err.Error()}, 400)
			return
		}
	}
	encodedInput, _ := json.Marshal(in)
	fingerprint := hash(string(encodedInput))
	start, err := time.Parse("2006-01-02", in.Start)
	if !recording && (err != nil || start.Year() < 2000 || start.Year() > 9996) {
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
	var renewalDate *time.Time
	var subscriptionPackageID *int64
	err = tx.QueryRowContext(r.Context(), `SELECT a.user_id,lower(trim(a.email)),a.renewal_date,(to_jsonb(a)->>'subscription_package_id')::bigint FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id WHERE a.id=$1 AND a.deleted_at IS NULL AND u.deleted_at IS NULL AND NOT u.disabled AND (a.user_id=$2 OR $3) FOR UPDATE OF a FOR SHARE OF u`, in.AccountID, user, admin).Scan(&owner, &email, &renewalDate, &subscriptionPackageID)
	if err != nil {
		operationError(w, err)
		return
	}
	var existing json.RawMessage
	err = tx.QueryRowContext(r.Context(), `SELECT to_jsonb(o) FROM recharge_orders o WHERE user_id=$1 AND request_key=$2`, owner, in.RequestKey).Scan(&existing)
	if err == nil {
		var old RechargeOrder
		_ = json.Unmarshal(existing, &old)
		if old.AccountID != in.AccountID || old.PackageID != in.PackageID || (!recording && old.PeriodStart != in.Start) || old.Notes != in.Notes {
			operationError(w, fmt.Errorf("idempotency mismatch"))
			return
		}
		if recording {
			var saved string
			err = tx.QueryRowContext(r.Context(), `SELECT after_data->>'fingerprint' FROM operation_events WHERE entity_type='order' AND entity_id=$1 AND action='record' AND request_key=$2`, old.ID, in.RequestKey).Scan(&saved)
			if err != nil || saved != fingerprint {
				operationError(w, fmt.Errorf("idempotency mismatch"))
				return
			}
		}
		reply(w, map[string]any{"order": existing}, 200)
		return
	}
	if err != sql.ErrNoRows {
		operationError(w, err)
		return
	}
	if recording {
		if in.QuickMonth {
			date := ""
			if renewalDate != nil {
				date = renewalDate.Format("2006-01-02")
			}
			if date != *in.ExpectedRenewalDate || subscriptionPackageID == nil || *subscriptionPackageID != in.PackageID {
				reply(w, map[string]string{"error": "账号的产品选型或续订日期已变更，请重新打开月订单确认"}, 409)
				return
			}
		}
		var postedAt time.Time
		if err = tx.QueryRowContext(r.Context(), `SELECT NOW()`).Scan(&postedAt); err != nil {
			operationError(w, err)
			return
		}
		day, _ := chargedOrderPeriod(postedAt, 1)
		start, _ = time.Parse("2006-01-02", day)
		if in.QuickMonth && renewalDate != nil {
			start = *renewalDate
		}
	}
	var pkg RechargePackage
	var raw json.RawMessage
	err = tx.QueryRowContext(r.Context(), `SELECT to_jsonb(p) FROM recharge_packages p WHERE id=$1 AND deleted_at IS NULL AND enabled FOR SHARE`, in.PackageID).Scan(&raw)
	if err != nil || json.Unmarshal(raw, &pkg) != nil {
		operationError(w, sql.ErrNoRows)
		return
	}
	if in.QuickMonth && (pkg.Months != 1 || start.Year() < 2000 || start.Year() > 9996) {
		reply(w, map[string]string{"error": "快速月订单需要一个月套餐及有效续订日期，请先在账号管理中设置"}, 400)
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
		// 防重读取完整历史，不能通过软删除绕开仍有效的订单。
		err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM recharge_orders WHERE user_id=$1 AND account_email=$2 AND order_status='active' AND payment_status<>'refunded' AND fulfillment_status<>'cancelled' AND period_start<$4 AND period_end>$3)`, owner, email, start, end).Scan(&overlaps)
	}
	if err != nil {
		operationError(w, err)
		return
	}
	if overlaps {
		reply(w, map[string]string{"error": "此账号已有重叠周期的充值订单，请处理原订单"}, 409)
		return
	}
	var no string
	var id int64
	// 订单号唯一约束覆盖全部历史；碰撞时重试，不复用或覆盖已有订单。
	for attempt := 0; attempt < 32; attempt++ {
		no, err = rechargeOrderNumber(time.Now())
		if err != nil {
			break
		}
		err = tx.QueryRowContext(r.Context(), `INSERT INTO recharge_orders(order_no,user_id,account_id,account_email,package_id,package_snapshot,period_start,period_end,sale_usd_minor,wallet_tokens,notes,request_key,order_source) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT (order_no) DO NOTHING RETURNING id`, no, owner, in.AccountID, email, in.PackageID, raw, start, end, pkg.SaleUSDMinor, pkg.WalletTokens, in.Notes, in.RequestKey, in.OrderSource).Scan(&id)
		if err != sql.ErrNoRows {
			break
		}
	}
	if err == sql.ErrNoRows {
		reply(w, map[string]string{"error": "当前订单号生成繁忙，请稍后重试"}, 503)
		return
	}
	if err == nil && recording {
		o := RechargeOrder{OrderSource: in.OrderSource, ID: id, OrderNo: no, UserID: owner, AccountID: in.AccountID, AccountEmail: email, PackageID: in.PackageID, Package: pkg, PeriodStart: start.Format("2006-01-02"), PeriodEnd: end.Format("2006-01-02"), SaleUSDMinor: pkg.SaleUSDMinor, WalletTokens: pkg.WalletTokens, OrderStatus: "active", PaymentStatus: "unpaid", FulfillmentStatus: "pending"}
		recordingInput := in.orderRecording
		recordingInput.CardID, err = boundOrderPaymentCard(r, tx, o.AccountID, in.CardID)
		if err == nil {
			err = recordOrderPosting(r, tx, user, &o, recordingInput, in.RequestKey)
		}
		if err == nil {
			err = saveRecordedOrder(r, tx, o)
		}
		if err == nil {
			o.Evidence = evidenceSummary(o.Evidence)
			err = recordEvent(r.Context(), tx, user, id, "order", "record", in.RequestKey, map[string]any{}, map[string]any{"fingerprint": fingerprint, "input": in.orderRecording, "order": o})
		}
	} else if err == nil {
		err = recordEvent(r.Context(), tx, user, id, "order", "create", in.RequestKey, map[string]any{}, map[string]any{"order_source": in.OrderSource, "order_no": no, "account_email": email, "package": pkg, "period_start": in.Start, "period_end": end.Format("2006-01-02")})
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

func rechargeOrderNumber(now time.Time) (string, error) {
	suffix, err := rand.Int(rand.Reader, big.NewInt(1000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%03d", now.In(time.FixedZone("UTC+8", 8*60*60)).Format("20060102150405"), suffix.Int64()), nil
}

type orderCommand struct {
	OrderSource      string `json:"order_source,omitempty"`
	Action           string `json:"action"`
	RequestKey       string `json:"request_key"`
	Version          int64  `json:"version"`
	Method           string `json:"method"`
	Reference        string `json:"reference"`
	Evidence         string `json:"evidence"`
	Reason           string `json:"reason"`
	Amount           string `json:"amount_usd"`
	CardID           int64  `json:"card_id"`
	AssigneeID       int64  `json:"assignee_id"`
	Success          bool   `json:"success"`
	Plan             string `json:"plan"`
	End              string `json:"period_end"`
	ReceivedCurrency string `json:"received_currency"`
	ReceivedAmount   string `json:"received_amount"`
	CollectionRateID int64  `json:"collection_rate_id"`
}

func (s *Server) orderAction(w http.ResponseWriter, r *http.Request, user, id int64, admin bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxEvidenceBytes+(64<<10))
	var in orderCommand
	if jsonBody(r, &in) != nil || !ledgerKeyPattern.MatchString(in.RequestKey) || len(in.Reference) > 200 || len(in.Reason) > 2000 {
		reply(w, map[string]string{"error": "操作参数无效"}, 400)
		return
	}
	if in.Action == "assign" || in.Action == "refund" {
		reply(w, map[string]string{"error": "此操作已停用，请刷新页面"}, 400)
		return
	}
	in.OrderSource = strings.Join(strings.Fields(in.OrderSource), " ")
	if utf8.RuneCountInString(in.OrderSource) > 80 {
		reply(w, map[string]string{"error": "订单来源不能超过 80 字"}, 400)
		return
	}
	in.Reference = strings.TrimSpace(in.Reference)
	in.Evidence = strings.TrimSpace(in.Evidence)
	if err := validateEvidence(in.Evidence); err != nil {
		reply(w, map[string]string{"error": err.Error()}, 400)
		return
	}
	finance := s.permitted(r.Context(), user, "finance")
	refund := s.permitted(r.Context(), user, "refunds")
	if (in.Action == "collect" && in.Method != "wallet" && !finance) || ((in.Action == "purchase" || in.Action == "record") && !finance) || (in.Action == "refund_note" && !refund) || ((in.Action == "verify" || in.Action == "retry" || in.Action == "discard") && !admin) {
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
	if o.OrderStatus != "active" || o.PaymentStatus == "refunded" || o.FulfillmentStatus == "cancelled" {
		fail("订单已退款或废弃，只能查看详情")
		return
	}
	switch in.Action {
	case "record":
		var cardID int64
		cardID, err = boundOrderPaymentCard(r, tx, o.AccountID, in.CardID)
		if err == nil {
			err = recordOrderPosting(r, tx, user, &o, orderRecording{OrderSource: in.OrderSource, CardID: cardID, Reference: in.Reference, Evidence: in.Evidence, ReceivedCurrency: in.ReceivedCurrency, ReceivedAmount: in.ReceivedAmount, CollectionRateID: in.CollectionRateID}, in.RequestKey)
		}
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
			if receiptErr := applyReceipt(r, tx, &o, in.ReceivedCurrency, in.ReceivedAmount, in.CollectionRateID); receiptErr != nil {
				fail(receiptErr.Error())
				return
			}
			o.PaymentReference = in.Reference
		} else {
			fail("线下收款需填写交易号和凭证；或由客户使用钱包付款")
			return
		}
		o.PaymentMethod = in.Method
		o.PaymentStatus = "paid"
		o.Evidence = in.Evidence
	case "purchase":
		amount := o.SaleUSDMinor
		if in.Amount != "" {
			submitted, valid := parseCardUSD(in.Amount)
			if !valid || submitted != amount {
				fail("扣款金额必须等于下单时的 SKU 价格")
				return
			}
		}
		if amount <= 0 || in.CardID < 1 || in.Reference == "" || in.Evidence == "" || o.PaymentStatus != "paid" || o.CostUSDMinor != 0 || o.FulfillmentStatus == "cancelled" {
			fail("请确认已收款、官网交易号、实际扣款及凭证；同一订单不能重复购买记账")
			return
		}
		err = recordOrderPosting(r, tx, user, &o, orderRecording{OrderSource: in.OrderSource, CardID: in.CardID, Reference: in.Reference, Evidence: in.Evidence}, in.RequestKey)
	case "verify", "retry":
		fail("开通核验已停用，订单扣款成功即开通")
		return
	case "refund_note":
		if (o.PaymentStatus != "paid" && o.PaymentStatus != "partial_refund") || strings.TrimSpace(in.Reason) == "" || in.Reference == "" || in.Evidence == "" || in.Amount != "" {
			fail("退款登记需原因、凭证编号及图文凭据，不填写退款金额")
			return
		}
		_, err = tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(hashtext('customer-refund'),hashtext($1))`, in.Reference)
		var duplicate bool
		if err == nil {
			err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM operation_events WHERE entity_type='order' AND action IN ('refund','refund_note') AND after_data->'input'->>'reference'=$1)`, in.Reference).Scan(&duplicate)
		}
		if err != nil {
			operationError(w, err)
			return
		}
		if duplicate {
			fail("此退款凭证编号已登记")
			return
		}
		// 退款结束订单并释放周期，不修改实退金额、收款记录或任何资金余额。
		o.OrderStatus = "refunded"
	case "discard":
		if strings.TrimSpace(in.Reason) == "" {
			fail("请填写废弃原因")
			return
		}
		o.OrderStatus = "discarded"
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
		o.OrderStatus = "discarded"
	default:
		reply(w, map[string]string{"error": "未知订单操作"}, 400)
		return
	}
	if err != nil {
		operationError(w, err)
		return
	}
	o.Version++
	var receivedRate any
	if o.ReceivedExchangeRate != nil {
		encoded, encodeErr := json.Marshal(o.ReceivedExchangeRate)
		if encodeErr != nil {
			operationError(w, encodeErr)
			return
		}
		receivedRate = string(encoded)
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE recharge_orders SET payment_method=$2,payment_status=$3,fulfillment_status=$4,payment_reference=$5,purchase_reference=$6,card_id=$7,cost_usd_minor=$8,refunded_usd_minor=$9,refunded_tokens=$10,assignee_id=$11,evidence=$12,failure_reason=$13,version=$14,verified_at=$15,order_status=$16,received_currency=$17,received_amount_minor=$18,received_usd_minor=$19,received_exchange_rate=$20,received_at=$21,period_start=$22,period_end=$23,order_source=$24,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, id, o.PaymentMethod, o.PaymentStatus, o.FulfillmentStatus, o.PaymentReference, o.PurchaseReference, o.CardID, o.CostUSDMinor, o.RefundedUSDMinor, o.RefundedTokens, o.AssigneeID, o.Evidence, o.FailureReason, o.Version, o.VerifiedAt, o.OrderStatus, o.ReceivedCurrency, o.ReceivedAmountMinor, o.ReceivedUSDMinor, receivedRate, o.ReceivedAt, o.PeriodStart, o.PeriodEnd, o.OrderSource)
	if err == nil {
		auditOrder := o
		before.Evidence = evidenceSummary(before.Evidence)
		auditOrder.Evidence = evidenceSummary(auditOrder.Evidence)
		err = recordEvent(r.Context(), tx, user, id, "order", in.Action, in.RequestKey, before, map[string]any{"input": in, "order": auditOrder})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	o.Profit = calculateOrderProfit(o)
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
