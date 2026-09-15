package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"os"
	"strings"
)

// 超管使用固定内部标识持久化账号，修改登录名不影响关联数据。
// 此标识不是邮箱，因此不能通过注册、验证码或找回密码接管。
const adminIdentity = "__superadmin__"

type adminConfig struct {
	Username string
	Password string
}

func envDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func loadAdminConfig() adminConfig {
	return adminConfig{
		Username: envDefault("ADMIN_USERNAME", envDefault("SUPER_ADMIN_USERNAME", "admin")),
		Password: envDefault("ADMIN_PASSWORD", envDefault("SUPER_ADMIN_PASSWORD", "")),
	}
}

func userRole(email, role string) string {
	if email == adminIdentity {
		return "super_admin"
	}
	if strings.TrimSpace(role) == "admin" {
		return "admin"
	}
	return "user"
}

func (s *Server) role(ctx context.Context, id int64) (string, error) {
	var email, role string
	err := s.db.QueryRowContext(ctx, `SELECT email,COALESCE(role,'') FROM users WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&email, &role)
	return userRole(email, role), err
}

func (s *Server) isAdmin(ctx context.Context, id int64) (bool, error) {
	role, err := s.role(ctx, id)
	return role == "admin" || role == "super_admin", err
}

func (s *Server) loginAdmin(w http.ResponseWriter, r *http.Request, password string) {
	if s.admin.Password == "" {
		reply(w, map[string]string{"error": "管理员登录尚未配置"}, 503)
		return
	}
	expected := sha256.Sum256([]byte(s.admin.Password))
	actual := sha256.Sum256([]byte(password))
	if subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
		reply(w, map[string]string{"error": "用户名、邮箱或密码错误"}, http.StatusUnauthorized)
		return
	}
	// 凭据由环境配置提供，数据库只保存稳定的用户 ID。
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO users(email,password_hash) VALUES($1,'') ON CONFLICT(email) WHERE deleted_at IS NULL DO NOTHING`, adminIdentity)
	if err != nil {
		reply(w, map[string]string{"error": "超管初始化失败"}, http.StatusInternalServerError)
		return
	}
	var id int64
	if err := s.db.QueryRowContext(r.Context(), `SELECT id FROM users WHERE email=$1 AND deleted_at IS NULL`, adminIdentity).Scan(&id); err != nil {
		reply(w, map[string]string{"error": "超管登录失败"}, http.StatusInternalServerError)
		return
	}
	reply(w, map[string]string{"token": s.token(id)}, http.StatusOK)
}
