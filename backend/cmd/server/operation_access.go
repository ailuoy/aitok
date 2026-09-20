package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *Server) userAccess(w http.ResponseWriter, r *http.Request, target int64) {
	user, err := s.auth(r)
	if err != nil {
		w.WriteHeader(401)
		return
	}
	role, err := s.role(r.Context(), user)
	if err != nil || role != "super_admin" {
		w.WriteHeader(403)
		return
	}
	if r.Method == "GET" {
		var raw json.RawMessage
		err = s.db.QueryRowContext(r.Context(), `SELECT jsonb_build_object('disabled',disabled,'permissions',permissions) FROM users WHERE id=$1 AND email<>$2 AND deleted_at IS NULL`, target, adminIdentity).Scan(&raw)
		if err != nil {
			operationError(w, err)
			return
		}
		reply(w, map[string]any{"access": raw, "permissions": permissionNames}, 200)
		return
	}
	if r.Method != "PATCH" {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var in struct {
		Disabled    bool     `json:"disabled"`
		Permissions []string `json:"permissions"`
	}
	if jsonBody(r, &in) != nil {
		w.WriteHeader(400)
		return
	}
	for _, permission := range in.Permissions {
		valid := false
		for _, known := range permissionNames {
			if permission == known {
				valid = true
			}
		}
		if !valid {
			reply(w, map[string]string{"error": "权限名称无效"}, 400)
			return
		}
	}
	if in.Permissions == nil {
		in.Permissions = []string{}
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	var before json.RawMessage
	err = tx.QueryRowContext(r.Context(), `SELECT jsonb_build_object('disabled',disabled,'permissions',permissions) FROM users WHERE id=$1 AND email<>$2 AND deleted_at IS NULL FOR UPDATE`, target, adminIdentity).Scan(&before)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE users SET disabled=$2,permissions=$3,session_version=session_version+1,updated_at=NOW() WHERE id=$1`, target, in.Disabled, in.Permissions)
	}
	if err == nil {
		err = recordEvent(r.Context(), tx, user, target, "user", "access", eventKey(), before, in)
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

type auditWriter struct {
	http.ResponseWriter
	status int
}

func (w *auditWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *auditWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}

func (s *Server) accessFilter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.db == nil {
			next.ServeHTTP(w, r)
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 2 || parts[0] != "api" {
			next.ServeHTTP(w, r)
			return
		}
		user, err := s.auth(r)
		if err == nil {
			role, roleErr := s.role(r.Context(), user)
			if roleErr != nil {
				reply(w, map[string]string{"error": "无法确认用户权限"}, 503)
				return
			}
			if role == "user" && !userEndpointAllowed(r.Method, r.URL.Path) {
				reply(w, map[string]string{"error": "普通用户仅可查看和添加自己的账号及使用本人钱包"}, 403)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) subscriptionSettings(w http.ResponseWriter, r *http.Request, user, id int64) {
	if r.Method != "PATCH" {
		w.WriteHeader(405)
		return
	}
	var in struct {
		Enabled *bool `json:"renewal_enabled"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if jsonBody(r, &in) != nil || in.Enabled == nil {
		w.WriteHeader(400)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	var previous bool
	err = tx.QueryRowContext(r.Context(), `SELECT a.renewal_enabled FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id AND u.deleted_at IS NULL WHERE a.id=$1 AND a.deleted_at IS NULL FOR UPDATE OF a`, id).Scan(&previous)
	if err == nil && previous != *in.Enabled {
		_, err = tx.ExecContext(r.Context(), `UPDATE chatgpt_accounts SET renewal_enabled=$2,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, id, *in.Enabled)
		if err == nil {
			err = recordEvent(r.Context(), tx, user, id, "account", "subscription", eventKey(), map[string]any{"renewal_enabled": previous}, map[string]any{"renewal_enabled": *in.Enabled})
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"renewal_enabled": *in.Enabled, "message": "已更新是否续订；此设置用于本系统续订安排与提醒"}, 200)
}

// 普通用户采用明确白名单，新管理接口默认不可访问。
func userEndpointAllowed(method, path string) bool {
	switch path {
	case "/api/me":
		return method == "GET"
	case "/api/wallet", "/api/consumption-orders":
		return method == "GET"
	case "/api/wallet/topups":
		return method == "POST"
	case "/api/accounts":
		return method == "GET" || method == "POST"
	case "/api/logout", "/api/admin-activity":
		return method == "POST"
	case "/api/login", "/api/register", "/api/login-code", "/api/send-code", "/api/forgot-password", "/api/reset-password", "/api/stripe/webhook":
		return true
	}
	if method == "POST" && strings.HasPrefix(path, "/api/wallet/topups/") {
		parts := strings.Split(strings.TrimPrefix(path, "/api/wallet/topups/"), "/")
		return len(parts) == 2 && parts[0] != "" && (parts[1] == "sync" || parts[1] == "refund")
	}
	return false
}
