package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type loginChallenge struct {
	Scope   string `json:"scope"`
	UserID  int64  `json:"user_id"`
	Expires int64  `json:"expires"`
	Stamp   string `json:"stamp"`
}

// 第一阶段只签发短期登录验证凭证，不能用于访问任何业务接口。
func (s *Server) finishLogin(w http.ResponseWriter, r *http.Request, user int64) {
	var enabled bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT totp_enabled_at IS NOT NULL FROM users WHERE id=$1 AND deleted_at IS NULL AND NOT disabled`, user).Scan(&enabled); err != nil {
		operationError(w, err)
		return
	}
	if !enabled {
		reply(w, map[string]string{"token": s.token(user)}, 200)
		return
	}
	stamp, err := s.sessionStamp(r.Context(), user)
	if err != nil {
		operationError(w, err)
		return
	}
	body, _ := json.Marshal(loginChallenge{"login-2fa-v1", user, time.Now().Add(5 * time.Minute).Unix(), stamp})
	payload := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payload))
	reply(w, map[string]any{"two_factor_required": true, "challenge": payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))}, 200)
}

func (s *Server) verifyLoginTOTP(w http.ResponseWriter, r *http.Request, challenge, code string) {
	invalid := func() { reply(w, map[string]string{"error": "登录验证已失效，请重新登录"}, 401) }
	parts := strings.Split(challenge, ".")
	if len(parts) != 2 || len(challenge) > 2048 {
		invalid()
		return
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(parts[0]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		invalid()
		return
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	var claims loginChallenge
	if err != nil || json.Unmarshal(body, &claims) != nil || claims.Scope != "login-2fa-v1" || claims.UserID < 1 || claims.Expires <= time.Now().Unix() {
		invalid()
		return
	}
	stamp, err := s.sessionStamp(r.Context(), claims.UserID)
	if err != nil || !hmac.Equal([]byte(stamp), []byte(claims.Stamp)) {
		invalid()
		return
	}
	if s.consumeUserTOTP(w, r, claims.UserID, code, false, false) {
		reply(w, map[string]string{"token": s.token(claims.UserID)}, 200)
	}
}
