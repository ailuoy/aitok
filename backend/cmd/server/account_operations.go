package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) insertAccount(ctx context.Context, user int64, label, email, encrypted string) (Account, error) {
	a := Account{UserID: user, Label: label, Email: email, HasSession: true}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer tx.Rollback()
	// 用户行充当新账号身份的并发保护，不依赖数据库外键。
	var owner int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL AND NOT disabled FOR UPDATE`, user).Scan(&owner)
	if err != nil {
		return a, err
	}
	var duplicate bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM chatgpt_accounts WHERE user_id=$1 AND lower(trim(email))=$2 AND deleted_at IS NULL)`, user, email).Scan(&duplicate)
	if err != nil {
		return a, err
	}
	if duplicate {
		return a, fmt.Errorf("该邮箱账号已存在，请更新原账号的 Session")
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO chatgpt_accounts(user_id,label,email,session_ciphertext) VALUES($1,$2,$3,$4) RETURNING id,created_at,renewal_enabled`, user, label, email, encrypted).Scan(&a.ID, &a.CreatedAt, &a.RenewalEnabled)
	if err == nil {
		err = recordEvent(ctx, tx, user, a.ID, "account", "create", eventKey(), map[string]any{}, map[string]any{"email": email, "label": label})
	}
	if err == nil {
		err = tx.Commit()
	}
	return a, err
}

func (s *Server) accountPage(w http.ResponseWriter, r *http.Request, user int64, admin bool) {
	p, size, valid := pageParameters(r)
	if !valid {
		w.WriteHeader(400)
		return
	}
	order, err := accountOrder(r.URL.Query().Get("sort"), r.URL.Query().Get("direction"), admin)
	if err != nil {
		reply(w, map[string]string{"error": err.Error()}, 400)
		return
	}
	group := r.URL.Query().Get("group")
	if !admin {
		group = ""
	}
	gid := int64(-1)
	if group == "none" {
		gid = 0
	} else if group != "" {
		var err error
		gid, err = strconv.ParseInt(group, 10, 64)
		if err != nil || gid < 1 {
			w.WriteHeader(400)
			return
		}
	}
	const filter = ` FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id AND u.deleted_at IS NULL LEFT JOIN account_groups g ON g.id=a.group_id AND g.deleted_at IS NULL LEFT JOIN bank_cards c ON c.id=a.payment_card_id AND c.deleted_at IS NULL LEFT JOIN addresses b ON b.id=a.billing_address_id AND b.deleted_at IS NULL WHERE a.deleted_at IS NULL AND (a.user_id=$1 OR $2) AND strpos(lower(a.label||' '||a.email),lower($3))>0 AND ($4::bigint=-1 OR COALESCE(a.group_id,0)=$4)`
	args := []any{user, admin, r.URL.Query().Get("q"), gid}
	var total int
	if err := s.db.QueryRowContext(r.Context(), `SELECT count(*)`+filter, args...).Scan(&total); err != nil {
		operationError(w, err)
		return
	}
	limit, offset := size, (p-1)*size
	export := r.URL.Path == "/api/accounts/export"
	if export {
		limit = 10000
		offset = 0
		if total > limit {
			reply(w, map[string]string{"error": "请筛选到一万条以内后导出"}, 400)
			return
		}
	}
	projection := `(to_jsonb(a)-'session_ciphertext'-'api_key')||jsonb_build_object('has_session',COALESCE(a.session_ciphertext,'')<>'','owner_email',CASE WHEN u.email='__superadmin__' THEN '超级管理员' ELSE u.email END,` + accountPaymentCardJSON + `,` + accountBillingAddressJSON + `)`
	if !admin {
		projection = `jsonb_build_object('id',a.id,'label',a.label,'email',a.email,'last_login_at',a.last_login_at)`
	}
	rows, err := jsonRows(r.Context(), s.db, "SELECT "+projection+filter+` ORDER BY `+order+` LIMIT $5 OFFSET $6`, append(args, limit, offset)...)
	if err != nil {
		operationError(w, err)
		return
	}
	if export {
		data := [][]string{}
		for _, raw := range rows {
			var a Account
			_ = json.Unmarshal(raw, &a)
			end := ""
			if a.SubscriptionEndsAt != nil {
				end = *a.SubscriptionEndsAt
			}
			data = append(data, []string{strconv.FormatInt(a.ID, 10), a.Email, a.Label, a.VerifiedPlan, end})
		}
		writeCSV(w, "accounts.csv", []string{"ID", "邮箱", "名称", "核验套餐", "核验到期日"}, data)
		return
	}
	reply(w, map[string]any{"accounts": rows, "total": total, "page": p, "page_size": size}, 200)
}

// 排序字段及方向只允许固定枚举，先排序再分页；空日期置后，同值用 ID 稳定排序。
func accountOrder(key, direction string, admin bool) (string, error) {
	if key == "" {
		key = "id"
	}
	if direction == "" {
		direction = "desc"
	}
	fields := map[string]string{"id": "a.id", "account": "lower(a.label)", "last_login_at": "a.last_login_at"}
	if admin {
		fields["owner"] = "lower(CASE WHEN u.email='__superadmin__' THEN '超级管理员' ELSE u.email END)"
		fields["group"] = "lower(g.name)"
		fields["session"] = "(COALESCE(a.session_ciphertext,'')<>'')"
		fields["renewal_date"] = "a.renewal_date"
		fields["renewal_enabled"] = "a.renewal_enabled"
		fields["payment_card"] = "lower(c.label)"
	}
	field, ok := fields[key]
	if !ok || (direction != "asc" && direction != "desc") {
		return "", fmt.Errorf("排序字段或方向无效")
	}
	return field + " " + strings.ToUpper(direction) + " NULLS LAST, a.id DESC", nil
}

var _ = sql.ErrNoRows
