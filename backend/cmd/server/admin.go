package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"os"
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
		Username: envDefault("SUPER_ADMIN_USERNAME", "admin"),
		Password: envDefault("SUPER_ADMIN_PASSWORD", "123456"),
	}
}

func (s *Server) loginAdmin(w http.ResponseWriter, r *http.Request, password string) {
	expected := sha256.Sum256([]byte(s.admin.Password))
	actual := sha256.Sum256([]byte(password))
	if subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
		reply(w, map[string]string{"error": "用户名、邮箱或密码错误"}, http.StatusUnauthorized)
		return
	}
	// 凭据由环境配置提供，数据库只保存稳定的用户 ID。
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO users(email,password_hash) VALUES($1,'') ON CONFLICT(email) DO NOTHING`, adminIdentity)
	if err != nil {
		reply(w, map[string]string{"error": "超管初始化失败"}, http.StatusInternalServerError)
		return
	}
	var id int64
	if err := s.db.QueryRowContext(r.Context(), `SELECT id FROM users WHERE email=$1`, adminIdentity).Scan(&id); err != nil {
		reply(w, map[string]string{"error": "超管登录失败"}, http.StatusInternalServerError)
		return
	}
	reply(w, map[string]string{"token": s.token(id)}, http.StatusOK)
}
