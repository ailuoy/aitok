package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func passwordHash(value string) (string, error) {
	if len(value) < 8 || len(value) > 72 {
		return "", errors.New("密码需为 8 至 72 字节")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
	return string(b), err
}

// 旧 SHA-256 仅用于一次兼容验证，成功登录后升级到带盐密码哈希。
func passwordMatches(stored, password string) bool {
	if stored == "" || len(password) > 72 {
		return false
	}
	if strings.HasPrefix(stored, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(password)) == nil
	}
	return len(stored) == 64 && subtle.ConstantTimeCompare([]byte(stored), []byte(hash(password))) == 1
}

func (s *Server) sessionStamp(ctx context.Context, id int64) (string, error) {
	var email, password string
	var version int64
	err := s.db.QueryRowContext(ctx, `SELECT email,password_hash,session_version FROM users WHERE id=$1 AND deleted_at IS NULL AND NOT disabled`, id).Scan(&email, &password, &version)
	if err != nil {
		return "", err
	}
	if email == adminIdentity {
		password = s.admin.Username + ":" + s.admin.Password
	}
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(email + ":" + password + ":" + strconv.FormatInt(version, 10)))
	return hex.EncodeToString(m.Sum(nil)), nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	id, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return
	}
	_, err = s.db.ExecContext(r.Context(), `UPDATE users SET session_version=session_version+1,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, id)
	if err != nil {
		reply(w, map[string]string{"error": "退出失败，请重试"}, 500)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) allowAttempt(ctx context.Context, key string, limit int, window time.Duration) bool {
	var count int
	err := s.db.QueryRowContext(ctx, `INSERT INTO auth_limits(key,count) VALUES($1,1) ON CONFLICT(key) DO UPDATE SET count=CASE WHEN auth_limits.window_start<NOW()-($2*INTERVAL '1 second') THEN 1 ELSE auth_limits.count+1 END,window_start=CASE WHEN auth_limits.window_start<NOW()-($2*INTERVAL '1 second') THEN NOW() ELSE auth_limits.window_start END,updated_at=NOW() RETURNING count`, hash(key), int(window.Seconds())).Scan(&count)
	return err == nil && count <= limit
}

func (s *Server) securityFilter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.db == nil {
			next.ServeHTTP(w, r)
			return
		} // 无依赖的路由兼容测试。
		path := r.URL.Path
		authPath := path == "/api/login" || path == "/api/register" || path == "/api/login-code" || path == "/api/send-code" || path == "/api/forgot-password" || path == "/api/reset-password"
		if authPath {
			if r.Method != "POST" {
				w.WriteHeader(405)
				return
			}
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
			if err != nil {
				reply(w, map[string]string{"error": "请求内容过大"}, 413)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var input struct{ Email, Username string }
			_ = json.Unmarshal(body, &input)
			identity := strings.ToLower(strings.TrimSpace(input.Email))
			if path == "/api/login" && strings.TrimSpace(input.Username) != "" {
				identity = strings.ToLower(strings.TrimSpace(input.Username))
			}
			ip, _, _ := net.SplitHostPort(r.RemoteAddr)
			// 默认不相信客户端提交的 X-Forwarded-For；邮箱限流独立于反向代理来源。
			if !s.allowAttempt(r.Context(), "auth-ip:"+ip, 300, time.Minute) || (identity != "" && !s.allowAttempt(r.Context(), "auth-identity:"+identity, 20, 15*time.Minute)) {
				w.Header().Set("Retry-After", "900")
				reply(w, map[string]string{"error": "尝试过于频繁，请稍后重试"}, 429)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

var permissionNames = []string{"accounts", "orders", "finance", "cards", "card_numbers", "refunds", "packages", "audit"}

func (s *Server) permitted(ctx context.Context, user int64, permission string) bool {
	var email, role string
	err := s.db.QueryRowContext(ctx, `SELECT email,COALESCE(role,'') FROM users WHERE id=$1 AND deleted_at IS NULL AND NOT disabled`, user).Scan(&email, &role)
	return err == nil && (email == adminIdentity || role == "admin")
}

func (s *Server) requirePermission(w http.ResponseWriter, r *http.Request, permission string) (int64, bool) {
	id, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return 0, false
	}
	if !s.permitted(r.Context(), id, permission) {
		reply(w, map[string]string{"error": "没有此操作权限"}, 403)
		return 0, false
	}
	return id, true
}
