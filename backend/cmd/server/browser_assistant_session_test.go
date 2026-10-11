package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBrowserAssistantNotesBoundToAccount(t *testing.T) {
	db, _ := accountSettingsTest(t)
	if _, err := db.Exec(`UPDATE chatgpt_accounts SET notes=CASE id WHEN 1 THEN E'账号一备注\n第二行' WHEN 2 THEN '其他账号备注' ELSE '' END`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("assistant-notes-test")}
	for _, c := range []struct {
		account int64
		notes   string
	}{{1, "账号一备注\n第二行"}, {2, "其他账号备注"}, {3, ""}} {
		r := httptest.NewRequest("GET", "/api/browser-assistant?account_id=2", nil)
		r.Header.Set("Authorization", "Bearer "+s.assistantToken(1, c.account))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		var data struct{ Notes string }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &data) != nil || data.Notes != c.notes {
			t.Fatalf("助手应只读取授权绑定账号备注: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestBrowserAssistantSessionUpdate(t *testing.T) {
	db, _ := accountSettingsTest(t)
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	s := &Server{db: db, secret: []byte("assistant-session-test")}
	original := accountCredentials{SessionJSON: `{"accessToken":"old-token","user":{"email":"a@test.local"}}`, ProxyURL: "socks5://127.0.0.1:1080"}
	encrypted, err := encodeAccountCredentials(original)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE chatgpt_accounts SET session_ciphertext=$1 WHERE id=1`, encrypted); err != nil {
		t.Fatal(err)
	}
	token := s.assistantToken(1, 1)
	fresh := `{"accessToken":"fresh-token","user":{"email":"a@test.local"},"expires":"2099-01-01T00:00:00Z","cookies":[{"name":"__Secure-next-auth.session-token","value":"fresh-cookie"}]}`
	call := func(method, path, token, raw string, want int) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"session_json": raw})
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: got %d want %d: %s", path, w.Code, want, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "fresh-token") || strings.Contains(w.Body.String(), "fresh-cookie") {
			t.Fatal("响应不应包含会话凭据或允许缓存")
		}
	}
	const path = "/api/browser-assistant/session"
	checkUpdated := expectTimestampUpdate(t, db, "chatgpt_accounts", "id=1")
	call("POST", path+"?account_id=2", token, fresh, 200)
	checkUpdated()
	read := func() string {
		t.Helper()
		var value string
		if err := db.QueryRow(`SELECT session_ciphertext FROM chatgpt_accounts WHERE id=1`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	saved := read()
	var updatedAt time.Time
	if err := db.QueryRow(`SELECT session_updated_at FROM chatgpt_accounts WHERE id=1`).Scan(&updatedAt); err != nil || time.Since(updatedAt) > time.Minute {
		t.Fatal("成功保存须记录服务端更新时间", err)
	}
	credentials, err := decodeAccountCredentials(saved)
	if err != nil || credentials.SessionJSON != fresh || credentials.ProxyURL != original.ProxyURL || strings.Contains(saved, "fresh-token") {
		t.Fatal("Session 未加密保存或代理配置被覆盖", err)
	}
	var other string
	if err := db.QueryRow(`SELECT session_ciphertext FROM chatgpt_accounts WHERE id=2`).Scan(&other); err != nil || other != "" {
		t.Fatal("不得使用请求参数更换目标账号", err)
	}
	call("POST", path, token, strings.ReplaceAll(fresh, "a@test.local", "other@test.local"), 409)
	call("POST", path, token, `{"accessToken":"no-email"}`, 409)
	call("POST", path, token, strings.ReplaceAll(fresh, "2099", "2020"), 422)
	call("POST", path, token, `{"user":{"email":"a@test.local"}}`, 400)
	call("POST", path, token+"invalid", fresh, 401)
	call("POST", path, s.token(1), fresh, 401)
	call("POST", path, s.assistantToken(2, 1), fresh, 401)
	call("POST", path, s.assistantToken(1, 999), fresh, 404)
	call("GET", path, token, "", 404)
	call("PATCH", path, token, fresh, 405)
	// 旧版只读凭证不能因新增接口而获得写入能力。
	parts := strings.Split(token, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(parts[0])
	body := base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(string(raw), "assistant-v2:", "assistant:", 1)))
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(body))
	legacy := body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	call("POST", path, legacy, fresh, 403)
	if read() != saved {
		t.Fatal("更新失败不得覆盖已有 Session")
	}
	if _, err := db.Exec(`UPDATE chatgpt_accounts SET notes='修改备注',updated_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	var after time.Time
	if err := db.QueryRow(`SELECT session_updated_at FROM chatgpt_accounts WHERE id=1`).Scan(&after); err != nil || !after.Equal(updatedAt) {
		t.Fatal("失败请求和普通资料修改不得改变 Session 更新时间", err)
	}
	var audit string
	if err := db.QueryRow(`SELECT string_agg(after_data::text,' ') FROM operation_events WHERE action='assistant_session_update'`).Scan(&audit); err != nil || !strings.Contains(audit, "success") || !strings.Contains(audit, "failure") || strings.Contains(audit, "fresh-token") || strings.Contains(audit, "fresh-cookie") {
		t.Fatal("审计须记录结果且不得包含凭据", err)
	}
	if _, err := db.Exec(`UPDATE chatgpt_accounts SET deleted_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	call("POST", path, token, fresh, 404)
	if _, err := db.Exec(`UPDATE users SET deleted_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	call("POST", path, token, fresh, 401)
}

func TestSessionRefreshWithoutTOTPHasLimitedScope(t *testing.T) {
	db, _ := accountSettingsTest(t)
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	s := &Server{db: db, secret: []byte("refresh-only-test")}
	raw := `{"accessToken":"refresh-token","user":{"email":"a@test.local"}}`
	encrypted, err := encryptSession(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE chatgpt_accounts SET session_ciphertext=$1 WHERE id=1`, encrypted); err != nil {
		t.Fatal(err)
	}
	call := func(method, path, token, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: got %d want %d", method, path, w.Code, want)
		}
		return w
	}
	// 管理员未绑定验证器也可自动更新，普通用户及未登录请求仍拒绝。
	w := call("POST", "/api/accounts/1/browser-session?mode=refresh", s.token(1), "", 200)
	var data struct {
		Token string `json:"assistant_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil || data.Token == "" {
		t.Fatal("未签发自动更新授权", err)
	}
	call("POST", "/api/accounts/1/browser-session?mode=refresh", s.token(2), "", 403)
	call("POST", "/api/accounts/1/browser-session?mode=refresh", "", "", 401)
	for _, path := range []string{"/api/browser-assistant", "/api/browser-assistant/cards/1"} {
		call("GET", path, data.Token, "", 403)
	}
	body, _ := json.Marshal(map[string]string{"session_json": raw})
	call("POST", "/api/browser-assistant/session", data.Token, string(body), 200)
	other, _ := json.Marshal(map[string]string{"session_json": strings.ReplaceAll(raw, "a@test.local", "b@test.local")})
	call("POST", "/api/browser-assistant/session?account_id=2", data.Token, string(other), 409)
	expired := s.scopedAssistantToken(1, 1, "session-refresh-v1", -time.Minute)
	call("POST", "/api/browser-assistant/session", expired, string(body), 401)
}
