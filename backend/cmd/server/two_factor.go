package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// RFC 6238：SHA-1、六位数字、30 秒步长；允许客户端时钟偏差一个时间步。
func totpCode(secret string, step int64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil || len(key) < 20 || step < 0 {
		return ""
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	digest := mac.Sum(nil)
	offset := digest[len(digest)-1] & 15
	value := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1000000)
}

func matchTOTP(secret, code string, now time.Time, last int64) int64 {
	if len(code) != 6 || strings.IndexFunc(code, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return -1
	}
	for step := now.Unix()/30 - 1; step <= now.Unix()/30+1; step++ {
		if step > last && subtle.ConstantTimeCompare([]byte(totpCode(secret, step)), []byte(code)) == 1 {
			return step
		}
	}
	return -1
}

func totpEnvelope(user int64) string { return "aitok-totp-v1:" + strconv.FormatInt(user, 10) + ":" }
func readTOTP(ciphertext string, user int64) (string, error) {
	raw, err := decryptSession(ciphertext)
	if err != nil || !strings.HasPrefix(raw, totpEnvelope(user)) {
		return "", fmt.Errorf("无法读取验证器配置")
	}
	return strings.TrimPrefix(raw, totpEnvelope(user)), nil
}

// 验证与消费时间步同事务，两个浏览器入口并发请求也不能复用验证码。
func (s *Server) consumeTOTP(w http.ResponseWriter, r *http.Request, user int64, code string, confirm bool) bool {
	if !s.permitted(r.Context(), user, "accounts") {
		reply(w, map[string]string{"error": "仅管理员可以验证 2FA"}, 403)
		return false
	}
	if !s.allowAttempt(r.Context(), "totp:"+strconv.FormatInt(user, 10), 10, 5*time.Minute) {
		reply(w, map[string]string{"error": "验证过于频繁，请 5 分钟后重试"}, 429)
		return false
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		operationError(w, err)
		return false
	}
	defer tx.Rollback()
	var active, pending string
	var enabled, expires sql.NullTime
	var last int64
	err = tx.QueryRowContext(r.Context(), `SELECT COALESCE(totp_ciphertext,''),COALESCE(totp_pending_ciphertext,''),totp_enabled_at,totp_pending_expires_at,totp_last_step FROM users WHERE id=$1 AND deleted_at IS NULL AND NOT disabled AND (role='admin' OR email=$2) FOR UPDATE`, user, adminIdentity).Scan(&active, &pending, &enabled, &expires, &last)
	if err != nil {
		operationError(w, err)
		return false
	}
	if confirm {
		if enabled.Valid || !expires.Valid || !expires.Time.After(time.Now()) {
			reply(w, map[string]string{"error": "绑定已完成或二维码已过期，请重新设置"}, 409)
			return false
		}
		active = pending
	} else if !enabled.Valid {
		reply(w, map[string]string{"error": "请先在「我的 → 两步验证」绑定验证器", "code": "two_factor_required"}, 403)
		return false
	}
	secret, err := readTOTP(active, user)
	if err != nil {
		reply(w, map[string]string{"error": "验证器配置不可用，请联系系统维护人员"}, 503)
		return false
	}
	step := matchTOTP(secret, code, time.Now(), last)
	if step < 0 {
		reply(w, map[string]string{"error": "验证码无效或已使用，请输入验证器中的新验证码"}, 403)
		return false
	}
	if confirm {
		_, err = tx.ExecContext(r.Context(), `UPDATE users SET totp_ciphertext=totp_pending_ciphertext,totp_pending_ciphertext=NULL,totp_pending_expires_at=NULL,totp_enabled_at=NOW(),totp_last_step=$2,updated_at=NOW() WHERE id=$1`, user, step)
	} else {
		_, err = tx.ExecContext(r.Context(), `UPDATE users SET totp_last_step=$2,updated_at=NOW() WHERE id=$1`, user, step)
	}
	if err == nil && confirm {
		err = recordEvent(r.Context(), tx, user, user, "user", "two_factor_enabled", eventKey(), nil, map[string]bool{"enabled": true})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return false
	}
	return true
}

func (s *Server) twoFactor(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	user, ok := s.requirePermission(w, r, "accounts")
	if !ok {
		return
	}
	if r.Method == "GET" {
		var enabled bool
		if err := s.db.QueryRowContext(r.Context(), `SELECT totp_enabled_at IS NOT NULL FROM users WHERE id=$1 AND deleted_at IS NULL`, user).Scan(&enabled); err != nil {
			operationError(w, err)
			return
		}
		reply(w, map[string]bool{"enabled": enabled}, 200)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var in struct {
		Action   string `json:"action"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if jsonBody(r, &in) != nil {
		w.WriteHeader(400)
		return
	}
	if in.Action == "confirm" {
		if s.consumeTOTP(w, r, user, in.Code, true) {
			reply(w, map[string]bool{"enabled": true}, 200)
		}
		return
	}
	if in.Action != "setup" {
		w.WriteHeader(400)
		return
	}
	if !s.allowAttempt(r.Context(), "totp-setup:"+strconv.FormatInt(user, 10), 5, 15*time.Minute) {
		reply(w, map[string]string{"error": "设置过于频繁，请稍后重试"}, 429)
		return
	}
	var email, password string
	if err := s.db.QueryRowContext(r.Context(), `SELECT email,password_hash FROM users WHERE id=$1 AND deleted_at IS NULL AND NOT disabled`, user).Scan(&email, &password); err != nil {
		operationError(w, err)
		return
	}
	valid := passwordMatches(password, in.Password)
	if email == adminIdentity {
		a, b := sha256.Sum256([]byte(in.Password)), sha256.Sum256([]byte(s.admin.Password))
		valid = s.admin.Password != "" && subtle.ConstantTimeCompare(a[:], b[:]) == 1
		email = s.admin.Username
	}
	if !valid {
		reply(w, map[string]string{"error": "当前登录密码不正确"}, 403)
		return
	}
	key := make([]byte, 20)
	if _, err := rand.Read(key); err != nil {
		operationError(w, err)
		return
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(key)
	encrypted, err := encryptSession(totpEnvelope(user) + secret)
	if err != nil {
		reply(w, map[string]string{"error": "未配置验证器密钥加密"}, 503)
		return
	}
	result, err := s.db.ExecContext(r.Context(), `UPDATE users SET totp_pending_ciphertext=$2,totp_pending_expires_at=NOW()+INTERVAL '10 minutes',updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL AND NOT disabled AND (role='admin' OR email=$3) AND totp_enabled_at IS NULL`, user, encrypted, adminIdentity)
	if err != nil {
		operationError(w, err)
		return
	}
	if n, _ := result.RowsAffected(); n != 1 {
		reply(w, map[string]string{"error": "验证器已绑定，不能覆盖现有配置"}, 409)
		return
	}
	query := url.Values{"secret": {secret}, "issuer": {"AiTok"}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	reply(w, map[string]string{"otpauth_url": "otpauth://totp/" + url.PathEscape("AiTok:"+email) + "?" + query.Encode(), "secret": secret}, 200)
}
