package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

func (s *Server) insertAccount(ctx context.Context, user int64, label, email, encrypted string) (Account, error) {
	a := Account{UserID: user, Label: label, Email: email, HasSession: true, RenewalEnabled: true}
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
	err = tx.QueryRowContext(ctx, `INSERT INTO chatgpt_accounts(user_id,label,email,session_ciphertext) VALUES($1,$2,$3,$4) RETURNING id,created_at`, user, label, email, encrypted).Scan(&a.ID, &a.CreatedAt)
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
	const filter = ` FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id AND u.deleted_at IS NULL WHERE a.deleted_at IS NULL AND (a.user_id=$1 OR $2) AND strpos(lower(a.label||' '||a.email),lower($3))>0 AND ($4::bigint=-1 OR COALESCE(a.group_id,0)=$4)`
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
	projection := `(to_jsonb(a)-'session_ciphertext'-'api_key')||jsonb_build_object('has_session',COALESCE(a.session_ciphertext,'')<>'','owner_email',CASE WHEN u.email='__superadmin__' THEN '超级管理员' ELSE u.email END)`
	if !admin {
		projection = `jsonb_build_object('id',a.id,'label',a.label,'email',a.email,'last_login_at',a.last_login_at)`
	}
	rows, err := jsonRows(r.Context(), s.db, "SELECT "+projection+filter+` ORDER BY a.id DESC LIMIT $5 OFFSET $6`, append(args, limit, offset)...)
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

var _ = sql.ErrNoRows
