package main

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type cardPosting struct {
	Kind                                                                                string
	Amount                                                                              int64
	OrderID, AccountID, ReferenceID                                                     *int64
	AccountEmail, AccountLabel, PeriodStart, PeriodEnd, Currency, Reference, Notes, Key string
	OriginalAmount                                                                      int64
}

// 所有新增资金操作共用记账入口；调用方事务保证订单、余额、流水和审计一起提交。
func postCardEntry(r *http.Request, tx *sql.Tx, actor, cardID int64, p cardPosting, admin bool) error {
	var balance, reserved, limit, owner int64
	var status string
	var month, year int
	err := tx.QueryRowContext(r.Context(), `SELECT balance_usd_minor,reserved_usd_minor,daily_limit_usd_minor,user_id,status,exp_month,exp_year FROM bank_cards WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR $3) FOR UPDATE`, cardID, actor, admin).Scan(&balance, &reserved, &limit, &owner, &status, &month, &year)
	if err != nil {
		return err
	}
	if p.Amount == 0 || p.Amount > maxCardMoneyMinor || p.Amount < -maxCardMoneyMinor {
		return fmt.Errorf("amount")
	}
	if p.Amount < 0 {
		now := time.Now().UTC()
		if status != "active" || year < now.Year() || (year == now.Year() && month < int(now.Month())) {
			return fmt.Errorf("card unavailable")
		}
		if balance+p.Amount < reserved {
			return fmt.Errorf("insufficient available balance")
		}
		if limit > 0 {
			var spent int64
			err = tx.QueryRowContext(r.Context(), `SELECT COALESCE(-sum(amount_usd_minor),0) FROM bank_card_ledger WHERE card_id=$1 AND amount_usd_minor<0 AND created_at>=date_trunc('day',NOW() AT TIME ZONE 'Asia/Shanghai') AT TIME ZONE 'Asia/Shanghai'`, cardID).Scan(&spent)
			if err != nil {
				return err
			}
			if spent-p.Amount > limit {
				return fmt.Errorf("daily limit")
			}
		}
	}
	if balance+p.Amount < 0 || balance+p.Amount > maxCardMoneyMinor {
		return fmt.Errorf("balance range")
	}
	if p.ReferenceID != nil {
		var original int64
		var kind string
		err = tx.QueryRowContext(r.Context(), `SELECT amount_usd_minor,kind FROM bank_card_ledger WHERE id=$1 AND card_id=$2 FOR UPDATE`, *p.ReferenceID, cardID).Scan(&original, &kind)
		if err != nil {
			return err
		}
		if kind == "reversal" || kind == "refund" {
			return fmt.Errorf("cannot reverse compensation")
		}
		var already int64
		err = tx.QueryRowContext(r.Context(), `SELECT COALESCE(sum(amount_usd_minor),0) FROM bank_card_ledger WHERE reference_id=$1 AND kind IN ('refund','reversal')`, *p.ReferenceID).Scan(&already)
		if err != nil {
			return err
		}
		if p.Kind == "reversal" && (already != 0 || p.Amount != -original) {
			return fmt.Errorf("invalid reversal")
		}
		if p.Kind == "refund" && (original >= 0 || p.Amount <= 0 || already+p.Amount > -original) {
			return fmt.Errorf("refund exceeds original")
		}
	}
	if p.Kind == "subscription" && p.PeriodStart != "" {
		if _, err = tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(hashtext('card-cycle'),hashtext($1))`, p.AccountEmail); err != nil {
			return err
		}
		var overlaps bool
		err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM bank_card_ledger WHERE kind='subscription' AND account_email=$1 AND period_start<$3::date AND period_end>$2::date AND reversed_at IS NULL)`, p.AccountEmail, p.PeriodStart, p.PeriodEnd).Scan(&overlaps)
		if err != nil {
			return err
		}
		if overlaps {
			return fmt.Errorf("overlapping subscription charge")
		}
	}
	var start, end, original any
	if p.PeriodStart != "" {
		if _, _, err = parsePeriod(p.PeriodStart, p.PeriodEnd); err != nil {
			return err
		}
		start, end = p.PeriodStart, p.PeriodEnd
	}
	if p.OriginalAmount > 0 {
		original = p.OriginalAmount
	}
	if p.Currency == "" {
		p.Currency = "USD"
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO bank_card_ledger(card_id,actor_id,request_key,kind,amount_usd_minor,balance_after_usd_minor,account_id,account_label,account_email,notes,order_id,reference_id,external_reference,period_start,period_end,currency,original_amount_minor) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, cardID, actor, p.Key, p.Kind, p.Amount, balance+p.Amount, p.AccountID, p.AccountLabel, p.AccountEmail, p.Notes, p.OrderID, p.ReferenceID, p.Reference, start, end, p.Currency, original)
	if err != nil {
		return err
	}
	if p.Kind == "reversal" && p.ReferenceID != nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE bank_card_ledger SET reversed_at=NOW(),updated_at=NOW() WHERE id=$1`, *p.ReferenceID)
		if err != nil {
			return err
		}
		var orderID sql.NullInt64
		err = tx.QueryRowContext(r.Context(), `SELECT order_id FROM bank_card_ledger WHERE id=$1`, *p.ReferenceID).Scan(&orderID)
		if err != nil {
			return err
		}
		if orderID.Valid {
			var before json.RawMessage
			if err = tx.QueryRowContext(r.Context(), `SELECT to_jsonb(o) FROM recharge_orders o WHERE id=$1`, orderID.Int64).Scan(&before); err != nil {
				return err
			}
			// 仅撤销由此订单产生的最近核验，不能抹掉后续周期已核验的订阅。
			_, err = tx.ExecContext(r.Context(), `UPDATE chatgpt_accounts a SET verified_plan='',verified_at=NULL,subscription_ends_at=NULL,updated_at=NOW() FROM recharge_orders o WHERE o.id=$1 AND a.id=o.account_id AND a.verified_at=o.verified_at AND a.deleted_at IS NULL`, orderID.Int64)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(r.Context(), `UPDATE recharge_orders SET cost_usd_minor=0,purchase_reference='',card_id=NULL,fulfillment_status='processing',verified_at=NULL,version=version+1,updated_at=NOW() WHERE id=$1`, orderID.Int64)
			if err != nil {
				return err
			}
			if err = recordEvent(r.Context(), tx, actor, orderID.Int64, "order", "purchase_reversal", p.Key, before, map[string]any{"reference_id": *p.ReferenceID, "reference": p.Reference, "reason": p.Notes}); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE bank_cards SET balance_usd_minor=$2,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, cardID, balance+p.Amount)
	return err
}

func (s *Server) cardOperations(w http.ResponseWriter, r *http.Request) {
	user, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return
	}
	id, err := pathID(r.URL.Path, "/api/card-operations/")
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	admin := s.permitted(r.Context(), user, "finance") || s.permitted(r.Context(), user, "refunds")
	var owner int64
	err = s.db.QueryRowContext(r.Context(), `SELECT user_id FROM bank_cards WHERE id=$1 AND (user_id=$2 OR $3)`, id, user, admin).Scan(&owner)
	if err != nil {
		operationError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == "GET" {
		s.cardReconciliation(w, r, id)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	var in struct {
		Action      string `json:"action"`
		RequestKey  string `json:"request_key"`
		Kind        string `json:"kind"`
		Amount      string `json:"amount_usd"`
		ReferenceID int64  `json:"reference_id"`
		Reference   string `json:"reference"`
		Notes       string `json:"notes"`
		Status      string `json:"status"`
		Limit       int64  `json:"daily_limit_usd_minor"`
		Warning     int64  `json:"low_balance_usd_minor"`
		CSV         string `json:"csv"`
		HoldID      int64  `json:"hold_id"`
		RowID       int64  `json:"row_id"`
	}
	if jsonBody(r, &in) != nil || !ledgerKeyPattern.MatchString(in.RequestKey) || len(in.Reference) > 200 || len(in.Notes) > 2000 {
		reply(w, map[string]string{"error": "请求格式无效"}, 400)
		return
	}
	if (in.Action == "posting" && !s.permitted(r.Context(), user, "refunds")) || (in.Action != "posting" && user != owner && !s.permitted(r.Context(), user, "finance")) {
		reply(w, map[string]string{"error": "没有资金调整权限"}, 403)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	// 与订单购买保持一致：先锁订单，再锁卡片，避免冲正和购买互相等待。
	if in.Action == "posting" && in.Kind == "reversal" && in.ReferenceID > 0 {
		var linked sql.NullInt64
		err = tx.QueryRowContext(r.Context(), `SELECT order_id FROM bank_card_ledger WHERE id=$1 AND card_id=$2`, in.ReferenceID, id).Scan(&linked)
		if err != nil {
			operationError(w, err)
			return
		}
		if linked.Valid {
			var locked int64
			err = tx.QueryRowContext(r.Context(), `SELECT id FROM recharge_orders WHERE id=$1 FOR UPDATE`, linked.Int64).Scan(&locked)
			if err != nil {
				operationError(w, err)
				return
			}
		}
	}
	var before json.RawMessage
	err = tx.QueryRowContext(r.Context(), `SELECT jsonb_build_object('status',status,'balance',balance_usd_minor,'reserved',reserved_usd_minor,'limit',daily_limit_usd_minor,'warning',low_balance_usd_minor) FROM bank_cards WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&before)
	if err != nil {
		operationError(w, err)
		return
	}
	var old json.RawMessage
	err = tx.QueryRowContext(r.Context(), `SELECT after_data FROM operation_events WHERE entity_type='card' AND entity_id=$1 AND request_key=$2`, id, in.RequestKey).Scan(&old)
	if err == nil {
		var replay struct {
			Fingerprint string `json:"fingerprint"`
		}
		_ = json.Unmarshal(old, &replay)
		encoded, _ := json.Marshal(in)
		if replay.Fingerprint != hash(string(encoded)) {
			operationError(w, fmt.Errorf("idempotency"))
			return
		}
		reply(w, map[string]bool{"replayed": true}, 200)
		return
	}
	if err != sql.ErrNoRows {
		operationError(w, err)
		return
	}
	err = nil
	switch in.Action {
	case "configure":
		if !strings.Contains("|active|frozen|invalid|", "|"+in.Status+"|") || in.Limit < 0 || in.Warning < 0 || in.Limit > maxCardMoneyMinor || in.Warning > maxCardMoneyMinor {
			reply(w, map[string]string{"error": "卡片状态或限额无效"}, 400)
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE bank_cards SET status=$2,daily_limit_usd_minor=$3,low_balance_usd_minor=$4,updated_at=NOW() WHERE id=$1`, id, in.Status, in.Limit, in.Warning)
	case "posting":
		negative := strings.HasPrefix(in.Amount, "-")
		amount, ok := parseCardUSD(strings.TrimPrefix(in.Amount, "-"))
		if negative {
			amount = -amount
		}
		if !ok || in.Notes == "" || in.Reference == "" || !strings.Contains("|refund|reversal|fee|adjustment|", "|"+in.Kind+"|") || (in.Kind == "refund" && amount <= 0) || (in.Kind == "fee" && amount >= 0) {
			reply(w, map[string]string{"error": "请填写有效类型、带正负号的金额、交易号和调整原因"}, 400)
			return
		}
		var ref *int64
		if in.ReferenceID > 0 {
			ref = &in.ReferenceID
		}
		if (in.Kind == "refund" || in.Kind == "reversal") && ref == nil {
			reply(w, map[string]string{"error": "退款或冲正必须关联原流水 ID"}, 400)
			return
		}
		err = postCardEntry(r, tx, user, id, cardPosting{Kind: in.Kind, Amount: amount, ReferenceID: ref, Reference: in.Reference, Notes: in.Notes, Key: in.RequestKey}, admin)
	case "hold":
		amount, ok := parseCardUSD(in.Amount)
		if !ok || in.Reference == "" {
			reply(w, map[string]string{"error": "请填写冻结金额及预授权交易号"}, 400)
			return
		}
		var available int64
		var state string
		err = tx.QueryRowContext(r.Context(), `SELECT balance_usd_minor-reserved_usd_minor,status FROM bank_cards WHERE id=$1`, id).Scan(&available, &state)
		if err != nil || available < amount || state != "active" {
			operationError(w, fmt.Errorf("insufficient balance or unavailable"))
			return
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO card_holds(card_id,amount_usd_minor,reference,actor_id,notes) VALUES($1,$2,$3,$4,$5)`, id, amount, in.Reference, user, in.Notes)
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `UPDATE bank_cards SET reserved_usd_minor=reserved_usd_minor+$2,updated_at=NOW() WHERE id=$1`, id, amount)
		}
	case "release", "settle":
		var amount int64
		err = tx.QueryRowContext(r.Context(), `SELECT amount_usd_minor FROM card_holds WHERE id=$1 AND card_id=$2 AND status='held' AND deleted_at IS NULL FOR UPDATE`, in.HoldID, id).Scan(&amount)
		if err != nil {
			operationError(w, err)
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE bank_cards SET reserved_usd_minor=reserved_usd_minor-$2,updated_at=NOW() WHERE id=$1`, id, amount)
		status := "released"
		if in.Action == "settle" {
			if in.Reference == "" || in.Notes == "" {
				reply(w, map[string]string{"error": "结算需实际交易号和说明，充值订单扣款请先释放冻结再在订单中记账"}, 400)
				return
			}
			status = "settled"
			if err == nil {
				err = postCardEntry(r, tx, user, id, cardPosting{Kind: "adjustment", Amount: -amount, Reference: in.Reference, Notes: in.Notes, Key: in.RequestKey}, admin)
			}
		}
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `UPDATE card_holds SET status=$2,updated_at=NOW() WHERE id=$1`, in.HoldID, status)
		}
	case "import":
		reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(in.CSV, "\ufeff")))
		header, e := reader.Read()
		if e != nil || strings.Join(header, ",") != "transaction_id,amount_usd,occurred_at,description" {
			reply(w, map[string]string{"error": "CSV 表头需为 transaction_id,amount_usd,occurred_at,description"}, 400)
			return
		}
		count := 0
		for {
			row, e := reader.Read()
			if e == io.EOF {
				break
			}
			count++
			if e != nil || len(row) != 4 || count > 1000 {
				err = fmt.Errorf("invalid CSV")
				break
			}
			amount, ok := parseCardUSD(strings.TrimPrefix(row[1], "-"))
			if strings.HasPrefix(row[1], "-") {
				amount = -amount
			}
			at, e := time.Parse(time.RFC3339, row[2])
			if !ok || e != nil || strings.TrimSpace(row[0]) == "" || len(row[0]) > 200 || len(row[3]) > 1000 {
				err = fmt.Errorf("invalid row %d", count)
				break
			}
			var existingAmount int64
			var existingTime time.Time
			var description string
			e = tx.QueryRowContext(r.Context(), `SELECT amount_usd_minor,occurred_at,description FROM card_statement_rows WHERE card_id=$1 AND external_reference=$2`, id, row[0]).Scan(&existingAmount, &existingTime, &description)
			if e == nil {
				if existingAmount != amount || !existingTime.Equal(at) || description != row[3] {
					err = fmt.Errorf("duplicate transaction changed")
					break
				}
				continue
			}
			if e != sql.ErrNoRows {
				err = e
				break
			}
			_, err = tx.ExecContext(r.Context(), `INSERT INTO card_statement_rows(card_id,external_reference,amount_usd_minor,occurred_at,description,actor_id) VALUES($1,$2,$3,$4,$5,$6)`, id, row[0], amount, at, row[3], user)
			if err != nil {
				break
			}
		}
	case "resolve":
		if strings.TrimSpace(in.Notes) == "" {
			reply(w, map[string]string{"error": "请填写差异处理说明"}, 400)
			return
		}
		var rowID int64
		err = tx.QueryRowContext(r.Context(), `UPDATE card_statement_rows SET resolution=$3,updated_at=NOW() WHERE id=$1 AND card_id=$2 AND deleted_at IS NULL RETURNING id`, in.RowID, id, in.Notes).Scan(&rowID)
	default:
		w.WriteHeader(400)
		return
	}
	encoded, _ := json.Marshal(in)
	if err == nil {
		err = recordEvent(r.Context(), tx, user, id, "card", in.Action, in.RequestKey, before, map[string]any{"fingerprint": hash(string(encoded)), "kind": in.Kind, "reference": in.Reference, "reference_id": in.ReferenceID, "notes": in.Notes, "amount_usd": in.Amount, "status": in.Status, "limit": in.Limit, "warning": in.Warning, "hold_id": in.HoldID, "row_id": in.RowID})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]bool{"ok": true}, 200)
}

func (s *Server) cardReconciliation(w http.ResponseWriter, r *http.Request, id int64) {
	user, _ := s.auth(r)
	p, size, valid := pageParameters(r)
	if !valid {
		w.WriteHeader(400)
		return
	}
	// 对账保留完整历史，与历史流水查询口径一致。
	var total int
	if err := s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM card_statement_rows WHERE card_id=$1`, id).Scan(&total); err != nil {
		operationError(w, err)
		return
	}
	rows, err := jsonRows(r.Context(), s.db, `SELECT to_jsonb(s)||jsonb_build_object('ledger_id',l.id,'ledger_amount',l.amount_usd_minor,'result',CASE WHEN l.id IS NULL THEN 'missing' WHEN l.card_id<>s.card_id THEN 'wrong_card' WHEN l.amount_usd_minor<>s.amount_usd_minor THEN 'amount_mismatch' ELSE 'matched' END) FROM card_statement_rows s LEFT JOIN bank_card_ledger l ON l.external_reference=s.external_reference WHERE s.card_id=$1 ORDER BY s.occurred_at DESC,s.id DESC LIMIT $3 OFFSET $2`, id, (p-1)*size, size)
	if err != nil {
		operationError(w, err)
		return
	}
	holds, err := jsonRows(r.Context(), s.db, `SELECT to_jsonb(h) FROM card_holds h WHERE card_id=$1 AND deleted_at IS NULL ORDER BY id DESC LIMIT 100`, id)
	if err != nil {
		operationError(w, err)
		return
	}
	unmatched, err := jsonRows(r.Context(), s.db, `SELECT jsonb_build_object('id',l.id,'reference',l.external_reference,'amount',l.amount_usd_minor,'created_at',l.created_at) FROM bank_card_ledger l WHERE card_id=$1 AND NOT EXISTS(SELECT 1 FROM card_statement_rows s WHERE s.card_id=l.card_id AND s.external_reference=l.external_reference) ORDER BY id DESC LIMIT 50`, id)
	if err != nil {
		operationError(w, err)
		return
	}
	if r.URL.Query().Get("export") == "1" {
		records, err := jsonRows(r.Context(), s.db, `SELECT jsonb_build_object('id',id,'kind',kind,'amount',amount_usd_minor,'balance',balance_after_usd_minor,'reference',external_reference,'actor',actor_id,'time',created_at) FROM bank_card_ledger WHERE card_id=$1 ORDER BY id`, id)
		if err != nil {
			operationError(w, err)
			return
		}
		data := [][]string{}
		for _, raw := range records {
			var entry map[string]any
			_ = json.Unmarshal(raw, &entry)
			data = append(data, []string{fmt.Sprint(entry["id"]), fmt.Sprint(entry["kind"]), fmt.Sprint(entry["amount"]), fmt.Sprint(entry["balance"]), fmt.Sprint(entry["reference"]), fmt.Sprint(entry["actor"]), fmt.Sprint(entry["time"])})
		}
		writeCSV(w, "card-ledger-"+strconv.FormatInt(id, 10)+".csv", []string{"流水ID", "类型", "金额美分", "余额美分", "交易号", "操作者ID", "时间"}, data)
		return
	}
	reply(w, map[string]any{"statements": rows, "total": total, "page_size": size, "holds": holds, "unmatched_ledger": unmatched, "page": p, "can_adjust": s.permitted(r.Context(), user, "refunds")}, 200)
}
