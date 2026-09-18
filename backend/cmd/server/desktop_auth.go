package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type desktopClaims struct {
	Scope    string `json:"scope"`
	UserID   int64  `json:"user_id"`
	Expires  int64  `json:"expires"`
	Stamp    string `json:"stamp"`
	State    string `json:"state"`
	Channel  string `json:"channel"`
	Audience string `json:"audience"`
}

var desktopStatePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// 桌面凭证仅用于确认助手登录身份，不能替代后台登录或账号导出的逐次 2FA。
func (s *Server) desktopAuthorize(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	user, ok := s.requirePermission(w, r, "accounts")
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var input struct {
		State   string `json:"state"`
		Channel string `json:"channel"`
	}
	if jsonBody(r, &input) != nil || !desktopStatePattern.MatchString(input.State) || (input.Channel != "test" && input.Channel != "production") {
		reply(w, map[string]string{"error": "授权请求无效，请从助手重新登录"}, 400)
		return
	}
	stamp, err := s.sessionStamp(r.Context(), user)
	if err != nil {
		reply(w, map[string]string{"error": "登录已失效，请重新登录"}, 401)
		return
	}
	claims := desktopClaims{"desktop", user, time.Now().Add(24 * time.Hour).Unix(), stamp, input.State, input.Channel, strings.TrimRight(s.billing.BaseURL, "/")}
	body, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payload))
	reply(w, map[string]string{"token": payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))}, 200)
}

func (s *Server) desktopSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	unauthorized := func() { reply(w, map[string]string{"error": "助手登录已失效，请重新授权登录"}, 401) }
	queryFailed := func(err error) {
		if errors.Is(err, sql.ErrNoRows) {
			unauthorized()
		} else {
			reply(w, map[string]string{"error": "后台暂时不可用，请稍后重试"}, http.StatusServiceUnavailable)
		}
	}
	parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
	if len(parts) != 2 || len(parts[0]) > 4096 {
		unauthorized()
		return
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(parts[0]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		unauthorized()
		return
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	var claims desktopClaims
	if err != nil || json.Unmarshal(body, &claims) != nil || claims.Scope != "desktop" || claims.UserID < 1 || claims.Expires <= time.Now().Unix() || claims.Audience != strings.TrimRight(s.billing.BaseURL, "/") || !desktopStatePattern.MatchString(claims.State) || (claims.Channel != "test" && claims.Channel != "production") {
		unauthorized()
		return
	}
	stamp, err := s.sessionStamp(r.Context(), claims.UserID)
	if err != nil {
		queryFailed(err)
		return
	}
	if !hmac.Equal([]byte(stamp), []byte(claims.Stamp)) {
		unauthorized()
		return
	}
	var user User
	if err = s.db.QueryRowContext(r.Context(), `SELECT id,email,COALESCE(role,'') FROM users WHERE id=$1 AND deleted_at IS NULL AND NOT disabled`, claims.UserID).Scan(&user.ID, &user.Email, &user.Role); err != nil {
		queryFailed(err)
		return
	}
	if user.Email != adminIdentity && user.Role != "admin" {
		unauthorized()
		return
	}
	user.Role = userRole(user.Email, user.Role)
	if user.Email == adminIdentity {
		user.Email = ""
		user.Username = s.admin.Username
	}
	reply(w, map[string]any{"user": user, "expires_at": claims.Expires, "state": claims.State, "channel": claims.Channel, "origin": claims.Audience}, 200)
}
