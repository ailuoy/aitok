package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

type AccountGroup struct {
	ID     int64  `json:"id"`
	UserID int64  `json:"user_id"`
	Name   string `json:"name"`
	Count  int    `json:"account_count"`
}

func groupError(w http.ResponseWriter, err error) {
	var pg *pgconn.PgError
	if errors.Is(err, sql.ErrNoRows) {
		reply(w, map[string]string{"error": "分组或账号不存在"}, 404)
		return
	}
	if errors.As(err, &pg) && pg.Code == "23505" {
		reply(w, map[string]string{"error": "相同名称的分组已存在"}, 409)
		return
	}
	reply(w, map[string]string{"error": "分组操作失败，请重试"}, 500)
}

func (s *Server) accountGroups(w http.ResponseWriter, r *http.Request) {
	user, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return
	}
	admin, err := s.isAdmin(r.Context(), user)
	if err != nil {
		groupError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var id int64
	if r.URL.Path != "/api/account-groups" {
		id, err = strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/account-groups/"), 10, 64)
		if err != nil || id <= 0 {
			http.NotFound(w, r)
			return
		}
	}
	if r.Method == "GET" && id == 0 {
		rows, err := s.db.QueryContext(r.Context(), `SELECT g.id,g.user_id,g.name,count(a.id) FROM account_groups g LEFT JOIN chatgpt_accounts a ON a.group_id=g.id AND a.deleted_at IS NULL WHERE g.deleted_at IS NULL AND (g.user_id=$1 OR $2) GROUP BY g.id ORDER BY lower(g.name),g.id`, user, admin)
		if err != nil {
			groupError(w, err)
			return
		}
		defer rows.Close()
		groups := []AccountGroup{}
		for rows.Next() {
			var g AccountGroup
			if err = rows.Scan(&g.ID, &g.UserID, &g.Name, &g.Count); err != nil {
				groupError(w, err)
				return
			}
			groups = append(groups, g)
		}
		if err = rows.Err(); err != nil {
			groupError(w, err)
			return
		}
		reply(w, map[string]any{"groups": groups}, 200)
		return
	}
	if (r.Method == "POST" && id == 0) || (r.Method == "PATCH" && id > 0) {
		r.Body = http.MaxBytesReader(w, r.Body, 2048)
		var in struct {
			Name   string `json:"name"`
			UserID int64  `json:"user_id"`
		}
		if jsonBody(r, &in) != nil {
			reply(w, map[string]string{"error": "请求格式错误"}, 400)
			return
		}
		in.Name = strings.Join(strings.Fields(in.Name), " ")
		if in.Name == "" || utf8.RuneCountInString(in.Name) > 80 {
			reply(w, map[string]string{"error": "分组名称需为 1 到 80 个字符"}, 400)
			return
		}
		if in.UserID == 0 {
			in.UserID = user
		}
		if !admin && in.UserID != user {
			reply(w, map[string]string{"error": "不能为其他用户创建分组"}, 403)
			return
		}
		var g AccountGroup
		status := 200
		if id == 0 {
			status = 201
			err = s.db.QueryRowContext(r.Context(), `INSERT INTO account_groups(user_id,name) SELECT id,$2 FROM users WHERE id=$1 AND deleted_at IS NULL RETURNING id,user_id,name`, in.UserID, in.Name).Scan(&g.ID, &g.UserID, &g.Name)
		} else {
			err = s.db.QueryRowContext(r.Context(), `UPDATE account_groups SET updated_at=NOW(),name=$1 WHERE id=$2 AND deleted_at IS NULL AND (user_id=$3 OR $4) RETURNING id,user_id,name`, in.Name, id, user, admin).Scan(&g.ID, &g.UserID, &g.Name)
		}
		if err != nil {
			groupError(w, err)
			return
		}
		reply(w, map[string]any{"group": g}, status)
		return
	}
	if r.Method == "DELETE" && id > 0 {
		tx, err := s.db.BeginTx(r.Context(), nil)
		if err != nil {
			groupError(w, err)
			return
		}
		defer tx.Rollback()
		result, err := tx.ExecContext(r.Context(), `UPDATE account_groups SET updated_at=NOW(),deleted_at=NOW() WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR $3)`, id, user, admin)
		if err != nil {
			groupError(w, err)
			return
		}
		count, err := result.RowsAffected()
		if err != nil {
			groupError(w, err)
			return
		}
		if count == 0 {
			http.NotFound(w, r)
			return
		}
		if _, err = tx.ExecContext(r.Context(), `UPDATE chatgpt_accounts SET updated_at=NOW(),group_id=NULL WHERE group_id=$1 AND deleted_at IS NULL`, id); err != nil {
			groupError(w, err)
			return
		}
		if err = tx.Commit(); err != nil {
			groupError(w, err)
			return
		}
		w.WriteHeader(204)
		return
	}
	w.WriteHeader(405)
}

func (s *Server) setAccountGroup(w http.ResponseWriter, r *http.Request, user, id int64) {
	admin, err := s.isAdmin(r.Context(), user)
	if err != nil {
		groupError(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var in struct {
		GroupID *int64 `json:"group_id"`
	}
	if jsonBody(r, &in) != nil || (in.GroupID != nil && *in.GroupID <= 0) {
		reply(w, map[string]string{"error": "请选择有效分组"}, 400)
		return
	}
	// 账号和目标分组须属于同一用户；管理员也不能跨用户绑定。
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		groupError(w, err)
		return
	}
	defer tx.Rollback()
	if in.GroupID != nil {
		var groupOwner int64
		if err = tx.QueryRowContext(r.Context(), `SELECT user_id FROM account_groups WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, *in.GroupID).Scan(&groupOwner); err != nil {
			groupError(w, err)
			return
		}
	}
	var groupID *int64
	err = tx.QueryRowContext(r.Context(), `UPDATE chatgpt_accounts a SET updated_at=NOW(),group_id=$1 WHERE a.id=$2 AND a.deleted_at IS NULL AND (a.user_id=$3 OR $4) AND ($1::bigint IS NULL OR EXISTS (SELECT 1 FROM account_groups g WHERE g.id=$1 AND g.deleted_at IS NULL AND g.user_id=a.user_id)) RETURNING group_id`, in.GroupID, id, user, admin).Scan(&groupID)
	if err != nil {
		groupError(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		groupError(w, err)
		return
	}
	reply(w, map[string]any{"group_id": groupID}, 200)
}

func (s *Server) recordAccountLogin(w http.ResponseWriter, r *http.Request, user, id int64) {
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var in struct {
		At time.Time `json:"logged_in_at"`
	}
	if jsonBody(r, &in) != nil || in.At.IsZero() || in.At.After(time.Now().Add(time.Minute)) || in.At.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		reply(w, map[string]string{"error": "登录时间无效"}, 400)
		return
	}
	var at time.Time
	err := s.db.QueryRowContext(r.Context(), `UPDATE chatgpt_accounts SET updated_at=NOW(),last_login_at=GREATEST(last_login_at,$1) WHERE id=$2 AND deleted_at IS NULL AND (user_id=$3 OR $4) RETURNING last_login_at`, in.At, id, user, s.permitted(r.Context(), user, "accounts")).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		reply(w, map[string]string{"error": "登录时间保存失败"}, 500)
		return
	}
	reply(w, map[string]any{"last_login_at": at}, 200)
}
