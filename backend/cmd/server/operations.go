package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func jsonRows(ctx context.Context, db queryer, query string, args ...any) ([]json.RawMessage, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		items = append(items, raw)
	}
	return items, rows.Err()
}

// 仅明确标记的业务提示可返回客户端，数据库原始错误始终隐藏。
type operationConflict string

func (e operationConflict) Error() string { return string(e) }

func operationError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		reply(w, map[string]string{"error": "记录不存在或无权访问"}, 404)
		return
	}
	var conflict operationConflict
	if errors.As(err, &conflict) {
		reply(w, map[string]string{"error": conflict.Error()}, 409)
		return
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		var message string
		switch pg.ConstraintName {
		case "bank_card_ledger_reference_unique", "recharge_orders_purchase_reference_unique":
			message = "交易号 / 凭证编号已用于记账，请核对银行卡流水和原订单；同一笔交易不能重复录入"
		case "bank_card_ledger_order_unique":
			message = "此订单已扣款，请刷新并查看订单详情，勿重复记账"
		case "recharge_orders_cycle_unique":
			message = "此账号已有重叠周期的充值订单，请处理原订单"
		}
		if message != "" {
			reply(w, map[string]string{"error": message}, 409)
			return
		}
	}
	// 不向客户端返回数据库错误或原始业务内容。
	reply(w, map[string]string{"error": "操作未完成，请检查记录状态、重复交易或关联对象后重试"}, 409)
}

func pageParameters(r *http.Request) (int, int, bool) {
	p := 1
	var err error
	if r.URL.Query().Get("page") != "" {
		p, err = strconv.Atoi(r.URL.Query().Get("page"))
	}
	size := 20
	if raw := r.URL.Query().Get("page_size"); raw != "" {
		parsed, sizeErr := strconv.Atoi(raw)
		if sizeErr != nil || parsed < 1 || parsed > 100 {
			return p, size, false
		}
		size = parsed
	}
	return p, size, err == nil && p > 0 && p <= 100000 && len([]rune(r.URL.Query().Get("q"))) <= 200
}
func pathID(path, prefix string) (int64, error) {
	return strconv.ParseInt(strings.TrimPrefix(path, prefix), 10, 64)
}

func recordEvent(ctx context.Context, tx *sql.Tx, actor, id int64, entity, action, key string, before, after any) error {
	b, err := json.Marshal(before)
	if err != nil {
		return err
	}
	a, err := json.Marshal(after)
	if err != nil {
		return err
	}
	request, _ := ctx.Value(requestAuditKey{}).(*requestAuditContext)
	if request != nil {
		var object map[string]json.RawMessage
		if json.Unmarshal(a, &object) == nil && object != nil {
			// 保留原业务数值的 JSON 精度，不能经 float64 转换金额或 ID。
			object["_request"], err = json.Marshal(map[string]string{"source": "server", "page": request.Page, "method": request.Method, "resource": request.Resource, "result": "success"})
			if err != nil {
				return err
			}
			a, err = json.Marshal(object)
			if err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO operation_events(actor_id,entity_type,entity_id,action,request_key,before_data,after_data) VALUES($1,$2,$3,$4,$5,$6,$7)`, actor, entity, id, action, key, b, a)
	if err == nil && request != nil {
		request.Recorded = true
	}
	return err
}

func eventKey() string { k, _ := newOrderNo(); return k }

type RechargePackage struct {
	ID                  int64         `json:"id"`
	Name                string        `json:"name"`
	Plan                string        `json:"plan"`
	Region              string        `json:"region"`
	Currency            string        `json:"currency"`
	OriginalAmountMinor int64         `json:"original_amount_minor"`
	SaleUSDMinor        int64         `json:"sale_usd_minor"`
	SaleCNYMinor        *int64        `json:"sale_cny_minor"`
	CNYPriceReady       bool          `json:"cny_price_ready"`
	WalletTokens        int64         `json:"wallet_tokens"`
	Months              int           `json:"months"`
	Enabled             bool          `json:"enabled"`
	Notes               string        `json:"notes"`
	AutoUSD             bool          `json:"auto_usd"`
	PriceReady          bool          `json:"price_ready"`
	ExchangeRate        *ExchangeRate `json:"exchange_rate,omitempty"`
}

func (s *Server) packages(w http.ResponseWriter, r *http.Request) {
	user, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return
	}
	admin := s.permitted(r.Context(), user, "packages")
	if r.Method == "GET" {
		rows, err := jsonRows(r.Context(), s.db, `SELECT to_jsonb(p) FROM recharge_packages p WHERE deleted_at IS NULL AND (enabled OR $1) ORDER BY id DESC`, admin)
		if err != nil {
			operationError(w, err)
			return
		}
		rate, err := latestExchangeRate(r.Context(), s.db)
		if err != nil {
			operationError(w, err)
			return
		}
		packages := make([]RechargePackage, 0, len(rows))
		for _, raw := range rows {
			var pkg RechargePackage
			if json.Unmarshal(raw, &pkg) != nil {
				operationError(w, errors.New("套餐格式无效"))
				return
			}
			pricePackage(&pkg, rate, time.Now())
			packages = append(packages, pkg)
		}
		w.Header().Set("Cache-Control", "no-store")
		reply(w, map[string]any{"packages": packages, "can_manage": admin, "exchange_rate": rate, "exchange_rate_fresh": rate.fresh(time.Now())}, 200)
		return
	}
	if !admin {
		reply(w, map[string]string{"error": "没有套餐管理权限"}, 403)
		return
	}
	id := int64(0)
	if r.URL.Path != "/api/packages" {
		id, err = pathID(r.URL.Path, "/api/packages/")
		if err != nil || id < 1 {
			http.NotFound(w, r)
			return
		}
	}
	if r.Method != "POST" && r.Method != "PATCH" && r.Method != "DELETE" {
		w.WriteHeader(405)
		return
	}
	var in RechargePackage
	if r.Method != "DELETE" {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if jsonBody(r, &in) != nil || strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 || len(in.Region) < 2 || len(in.Region) > 80 || len(in.Currency) != 3 || strings.ToUpper(in.Currency) != in.Currency || !strings.Contains("|plus|pro_5x|pro_20x|", "|"+in.Plan+"|") || in.OriginalAmountMinor <= 0 || in.OriginalAmountMinor > maxCardMoneyMinor || (!in.AutoUSD && (in.SaleUSDMinor <= 0 || in.SaleUSDMinor > maxCardMoneyMinor)) || (in.AutoUSD && in.Currency != "PHP") || in.WalletTokens < 0 || in.WalletTokens > maxCardMoneyMinor || in.Months < 1 || in.Months > 36 || len(in.Notes) > 2000 {
			reply(w, map[string]string{"error": "请填写有效套餐、地区、币种、金额和周期"}, 400)
			return
		}
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	if r.Method != "DELETE" {
		var rate *ExchangeRate
		if in.AutoUSD {
			rate, err = latestExchangeRate(r.Context(), tx)
		}
		if err != nil {
			operationError(w, err)
			return
		}
		pricePackage(&in, rate, time.Now())
		if !in.PriceReady {
			reply(w, map[string]string{"error": "PHP/USD 汇率尚未就绪或超过 48 小时，请待同步成功后保存"}, 503)
			return
		}
	}
	var before json.RawMessage = json.RawMessage(`{}`)
	if id > 0 {
		err = tx.QueryRowContext(r.Context(), `SELECT to_jsonb(p) FROM recharge_packages p WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&before)
		if err != nil {
			operationError(w, err)
			return
		}
	}
	if r.Method == "DELETE" {
		if id < 1 {
			w.WriteHeader(405)
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE recharge_packages SET deleted_at=NOW(),updated_at=NOW() WHERE id=$1`, id)
	} else if id == 0 && r.Method == "POST" {
		err = tx.QueryRowContext(r.Context(), `INSERT INTO recharge_packages(name,plan,region,currency,original_amount_minor,sale_usd_minor,wallet_tokens,months,enabled,notes,auto_usd) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`, in.Name, in.Plan, in.Region, in.Currency, in.OriginalAmountMinor, in.SaleUSDMinor, in.WalletTokens, in.Months, in.Enabled, in.Notes, in.AutoUSD).Scan(&id)
	} else if id > 0 && r.Method == "PATCH" {
		_, err = tx.ExecContext(r.Context(), `UPDATE recharge_packages SET name=$2,plan=$3,region=$4,currency=$5,original_amount_minor=$6,sale_usd_minor=$7,wallet_tokens=$8,months=$9,enabled=$10,notes=$11,auto_usd=$12,updated_at=NOW() WHERE id=$1`, id, in.Name, in.Plan, in.Region, in.Currency, in.OriginalAmountMinor, in.SaleUSDMinor, in.WalletTokens, in.Months, in.Enabled, in.Notes, in.AutoUSD)
	} else {
		w.WriteHeader(405)
		return
	}
	if err == nil {
		err = recordEvent(r.Context(), tx, user, id, "package", r.Method, eventKey(), before, in)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"id": id}, 200)
}

func csvCell(value string) string {
	if strings.HasPrefix(strings.TrimSpace(value), "=") || strings.HasPrefix(strings.TrimSpace(value), "+") || strings.HasPrefix(strings.TrimSpace(value), "-") || strings.HasPrefix(strings.TrimSpace(value), "@") {
		return "'" + value
	}
	return value
}
func writeCSV(w http.ResponseWriter, name string, header []string, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	writer := csv.NewWriter(w)
	_ = writer.Write(header)
	for _, row := range rows {
		for i := range row {
			row[i] = csvCell(row[i])
		}
		_ = writer.Write(row)
	}
	writer.Flush()
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	_, ok := s.requirePermission(w, r, "audit")
	if !ok {
		return
	}
	p, size, valid := pageParameters(r)
	if !valid {
		reply(w, map[string]string{"error": "页码无效"}, 400)
		return
	}
	const filter = ` FROM operation_events e LEFT JOIN users u ON u.id=e.actor_id WHERE e.deleted_at IS NULL AND strpos(lower(concat_ws(' ',e.entity_type,e.action,u.email,e.actor_id::text,e.after_data->>'page',e.after_data->'_request'->>'page')),lower($1))>0`
	var total int
	if err := s.db.QueryRowContext(r.Context(), `SELECT count(*)`+filter, r.URL.Query().Get("q")).Scan(&total); err != nil {
		operationError(w, err)
		return
	}
	rows, err := jsonRows(r.Context(), s.db, `SELECT to_jsonb(e)||jsonb_build_object('actor',CASE WHEN u.email='__superadmin__' THEN '超级管理员' ELSE COALESCE(u.email,'历史用户') END)`+filter+` ORDER BY e.id DESC LIMIT $3 OFFSET $2`, r.URL.Query().Get("q"), (p-1)*size, size)
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"events": rows, "total": total, "page": p, "page_size": size}, 200)
}

func (s *Server) notices(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	user, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return
	}
	admin := s.permitted(r.Context(), user, "orders")
	// 每次读取实时计算待办，不产生外部消息；按原始业务对象跳转处理。
	rows, err := jsonRows(r.Context(), s.db, `SELECT jsonb_build_object('kind','renewal','id',id,'label',email,'detail',subscription_ends_at::text,'path','/admin/accounts') FROM chatgpt_accounts WHERE deleted_at IS NULL AND renewal_enabled AND subscription_ends_at<=(NOW() AT TIME ZONE 'Asia/Shanghai')::date+7 AND (user_id=$1 OR $2)
UNION ALL SELECT jsonb_build_object('kind','order','id',id,'label',order_no,'detail',failure_reason,'path','/admin/orders') FROM recharge_orders WHERE deleted_at IS NULL AND order_status='active' AND (user_id=$1 OR $2) AND (fulfillment_status='failed' OR ((payment_status='paid' OR cost_usd_minor>0) AND fulfillment_status NOT IN ('completed','cancelled') AND updated_at<NOW()-INTERVAL '1 day'))
UNION ALL SELECT jsonb_build_object('kind','card','id',id,'label',label,'detail','可用余额低于预警值','path','/admin/bank-cards') FROM bank_cards WHERE deleted_at IS NULL AND low_balance_usd_minor>0 AND balance_usd_minor-reserved_usd_minor<low_balance_usd_minor AND (user_id=$1 OR $3)
UNION ALL SELECT jsonb_build_object('kind','payment','id',e.id,'label',e.order_no,'detail',e.detail,'path','/admin/payment-exceptions') FROM payment_exceptions e WHERE e.status IN ('pending','processing','submitted') AND $4
LIMIT 200`, user, admin, s.permitted(r.Context(), user, "finance"), s.permitted(r.Context(), user, "refunds"))
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"notices": rows}, 200)
}

func parsePeriod(start, end string) (time.Time, time.Time, error) {
	a, e := time.Parse("2006-01-02", start)
	if e != nil {
		return a, a, e
	}
	b, e := time.Parse("2006-01-02", end)
	if e != nil || !b.After(a) || b.Sub(a) > 4*366*24*time.Hour || a.Year() < 2000 {
		return a, b, fmt.Errorf("周期无效")
	}
	return a, b, nil
}
