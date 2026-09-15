package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type managedUser struct {
	User
	CreatedAt time.Time `json:"created_at"`
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	id, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return
	}
	role, err := s.role(r.Context(), id)
	if err != nil || role != "super_admin" {
		reply(w, map[string]string{"error": "仅超级管理员可以管理用户角色"}, 403)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/api/users" && r.Method == "GET" {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		page := 1
		if value := r.URL.Query().Get("page"); value != "" {
			page, err = strconv.Atoi(value)
		}
		if err != nil || page < 1 || page > 100000 || utf8.RuneCountInString(query) > 200 {
			reply(w, map[string]string{"error": "搜索内容或页码无效"}, 400)
			return
		}
		const filter = ` WHERE deleted_at IS NULL AND strpos(lower(CASE WHEN email=$1 THEN $2 ELSE email END),lower($3))>0`
		var total int
		if err = s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM users`+filter, adminIdentity, s.admin.Username, query).Scan(&total); err != nil {
			reply(w, map[string]string{"error": "读取用户失败"}, 500)
			return
		}
		rows, err := s.db.QueryContext(r.Context(), `SELECT id,email,COALESCE(role,''),created_at FROM users`+filter+` ORDER BY id DESC LIMIT 20 OFFSET $4`, adminIdentity, s.admin.Username, query, (page-1)*20)
		if err != nil {
			reply(w, map[string]string{"error": "读取用户失败"}, 500)
			return
		}
		defer rows.Close()
		users := []managedUser{}
		for rows.Next() {
			var u managedUser
			if err = rows.Scan(&u.ID, &u.Email, &u.Role, &u.CreatedAt); err != nil {
				reply(w, map[string]string{"error": "读取用户失败"}, 500)
				return
			}
			u.Role = userRole(u.Email, u.Role)
			if u.Role == "super_admin" {
				u.Email, u.Username = "", s.admin.Username
			}
			users = append(users, u)
		}
		if rows.Err() != nil {
			reply(w, map[string]string{"error": "读取用户失败"}, 500)
			return
		}
		reply(w, map[string]any{"users": users, "total": total, "page": page, "page_size": 20}, 200)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/users/"), "/")
	if len(parts) != 2 || parts[1] != "role" {
		http.NotFound(w, r)
		return
	}
	if r.Method != "PATCH" {
		w.WriteHeader(405)
		return
	}
	target, err := strconv.ParseInt(parts[0], 10, 64)
	var input struct {
		Role string `json:"role"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if err != nil || target < 1 || jsonBody(r, &input) != nil || (input.Role != "admin" && input.Role != "user") {
		reply(w, map[string]string{"error": "角色只能选择管理员或用户"}, 400)
		return
	}
	// 超管身份由环境配置保留，角色接口不能创建或降级超级管理员。
	var saved string
	err = s.db.QueryRowContext(r.Context(), `UPDATE users SET role=$1 WHERE id=$2 AND deleted_at IS NULL AND email<>$3 RETURNING role`, input.Role, target, adminIdentity).Scan(&saved)
	if errors.Is(err, sql.ErrNoRows) {
		reply(w, map[string]string{"error": "用户不存在或为不可修改的超级管理员"}, 404)
		return
	}
	if err != nil {
		reply(w, map[string]string{"error": "保存角色失败"}, 500)
		return
	}
	reply(w, map[string]string{"role": saved}, 200)
}
