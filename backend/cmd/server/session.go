package main

import (
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

type chatGPTSession struct {
	AccessToken string
	Email       string
	Name        string
	ExpiresAt   *time.Time
	BrowserJSON map[string]any
}

var loginCookieName = regexp.MustCompile(`^__Secure-(next-auth|authjs)\.session-token(\.[0-9]+)?$`)

func browserLoginCookies(root map[string]any) ([]map[string]string, error) {
	result := []map[string]string{}
	add := func(name, value string) error {
		if len(value) == 0 || len(value) > 16000 || (&http.Cookie{Name: name, Value: value}).Valid() != nil {
			return errors.New("网页登录 Cookie 格式无效")
		}
		result = append(result, map[string]string{"name": name, "value": value})
		return nil
	}
	if value, exists := root["sessionToken"]; exists {
		token, ok := value.(string)
		if !ok {
			return nil, errors.New("sessionToken 必须是 Cookie 字符串")
		}
		if err := add("__Secure-next-auth.session-token", token); err != nil {
			return nil, err
		}
	}
	if value, exists := root["cookies"]; exists {
		cookies, ok := value.([]any)
		if !ok || len(cookies) > 100 {
			return nil, errors.New("cookies 必须是最多 100 项的数组")
		}
		for _, item := range cookies {
			cookie, ok := item.(map[string]any)
			if !ok {
				return nil, errors.New("Cookie 必须包含 name 和 value")
			}
			name, _ := cookie["name"].(string)
			if !loginCookieName.MatchString(name) {
				continue
			}
			domain, _ := cookie["domain"].(string)
			if domain != "" && domain != "chatgpt.com" && domain != ".chatgpt.com" {
				continue
			}
			value, _ := cookie["value"].(string)
			if err := add(name, value); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

// JWT 字段仅作为导入提示，真实性仍由 ChatGPT 上游验证。
func parseChatGPTSession(raw string) (chatGPTSession, error) {
	var root map[string]any
	if len(raw) > 240000 || json.Unmarshal([]byte(raw), &root) != nil || len(root) == 0 {
		return chatGPTSession{}, errors.New("请输入完整的 Session JSON 对象")
	}
	stringAt := func(object map[string]any, keys ...string) string {
		for _, key := range keys {
			if value, ok := object[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
		return ""
	}
	var token string
	for _, object := range []map[string]any{root, sessionObject(root, "tokens"), sessionObject(root, "credentials")} {
		if token = stringAt(object, "accessToken", "access_token"); token != "" {
			break
		}
	}
	if token == "" || len(token) > 128000 || strings.IndexFunc(token, func(r rune) bool { return r <= 32 || r >= 127 }) >= 0 {
		return chatGPTSession{}, errors.New("Session 缺少有效的 accessToken，请重新复制 /api/auth/session 的完整 JSON")
	}
	user := sessionObject(root, "user")
	session := chatGPTSession{AccessToken: token, Email: stringAt(user, "email"), Name: stringAt(user, "name")}
	parts := strings.Split(token, ".")
	var claims map[string]any
	if len(parts) == 3 {
		if payload, err := base64.RawURLEncoding.DecodeString(parts[1]); err == nil {
			_ = json.Unmarshal(payload, &claims)
		}
	}
	profile := sessionObject(claims, "https://api.openai.com/profile")
	if email := stringAt(profile, "email"); email != "" {
		session.Email = email
	}
	session.Email = strings.ToLower(session.Email)
	if expiry, ok := claims["exp"].(float64); ok && expiry > 0 && expiry < 253402300800 {
		timestamp := time.Unix(int64(expiry), 0).UTC()
		session.ExpiresAt = &timestamp
	}
	if expiry, err := time.Parse(time.RFC3339, stringAt(root, "expires")); err == nil {
		if session.ExpiresAt == nil || expiry.Before(*session.ExpiresAt) {
			session.ExpiresAt = &expiry
		}
	}
	// 只传递会话字段及明确导入的 ChatGPT 登录 Cookie，不导出其他站点 Cookie。
	session.BrowserJSON = map[string]any{"accessToken": token}
	for _, key := range []string{"user", "expires", "account", "authProvider"} {
		if value, ok := root[key]; ok {
			session.BrowserJSON[key] = value
		}
	}
	cookies, err := browserLoginCookies(root)
	if err != nil {
		return chatGPTSession{}, err
	}
	if len(cookies) > 0 {
		session.BrowserJSON["cookies"] = cookies
	}
	return session, nil
}

func sessionObject(root map[string]any, key string) map[string]any {
	value, _ := root[key].(map[string]any)
	return value
}

func decryptSession(encoded string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(os.Getenv("SESSION_ENCRYPTION_KEY"))
	if err != nil || len(key) != 32 {
		return "", errors.New("未配置 SESSION_ENCRYPTION_KEY")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) < gcm.NonceSize()+gcm.Overhead() {
		return "", errors.New("Session 密文格式无效")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	return string(plain), err
}

func (s *Server) accountSession(w http.ResponseWriter, r *http.Request, userID, accountID int64, launch bool) {
	w.Header().Set("Cache-Control", "no-store")
	var encrypted, email string
	var err error
	// 后台管理员可更新账号 Session；旧版凭据导出接口仍仅限所有者。
	if launch {
		err = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(session_ciphertext,''),email FROM chatgpt_accounts WHERE id=$1 AND user_id=$2`, accountID, userID).Scan(&encrypted, &email)
	} else {
		encrypted, email, err = s.managedAccount(r.Context(), userID, accountID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		reply(w, map[string]string{"error": "账号不存在"}, 404)
		return
	}
	if err != nil {
		reply(w, map[string]string{"error": "读取账号失败"}, 500)
		return
	}
	var raw string
	stored, decodeErr := decodeAccountCredentials(encrypted)
	if launch {
		raw = stored.SessionJSON
		if decodeErr != nil {
			reply(w, map[string]string{"error": "无法读取 Session，请检查加密配置或重新导入"}, 422)
			return
		}
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		var in struct {
			SessionJSON string `json:"session_json"`
		}
		if jsonBody(r, &in) != nil {
			reply(w, map[string]string{"error": "请求格式错误或 Session JSON 过大"}, 400)
			return
		}
		raw = in.SessionJSON
	}
	session, err := parseChatGPTSession(raw)
	if err != nil {
		reply(w, map[string]string{"error": err.Error()}, 400)
		return
	}
	if session.Email != "" && !strings.EqualFold(session.Email, email) {
		reply(w, map[string]string{"error": "Session 中的邮箱与当前账号不一致，请为该邮箱新建账号"}, 409)
		return
	}
	if launch {
		if session.ExpiresAt != nil && !session.ExpiresAt.After(time.Now()) {
			reply(w, map[string]string{"error": "Session 已过期，请重新复制 /api/auth/session 并更新"}, 422)
			return
		}
		reply(w, map[string]any{"account_id": accountID, "session": session.BrowserJSON, "expires_at": session.ExpiresAt, "assistant_token": s.assistantToken(userID, accountID)}, 200)
		return
	}
	stored.SessionJSON = raw
	encoded, err := encodeAccountCredentials(stored)
	if err != nil {
		reply(w, map[string]string{"error": "账号加密未配置，请联系管理员"}, 503)
		return
	}
	result, err := s.db.ExecContext(r.Context(), `UPDATE chatgpt_accounts SET session_ciphertext=$1 WHERE id=$2 AND COALESCE(session_ciphertext,'')=$3`, encoded, accountID, encrypted)
	if err != nil {
		reply(w, map[string]string{"error": "更新 Session 失败"}, 500)
		return
	}
	if count, _ := result.RowsAffected(); count == 0 {
		reply(w, map[string]string{"error": "账号已被更新，请刷新后重试"}, 409)
		return
	}
	reply(w, map[string]string{"message": "Session 已更新"}, 200)
}
