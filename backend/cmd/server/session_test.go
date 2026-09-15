package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseChatGPTSession(t *testing.T) {
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":2000000000,"https://api.openai.com/profile":{"email":"JWT@example.com"}}`))
	for _, raw := range []string{
		`{"accessToken":"header.` + claims + `.sig","user":{"email":"session@example.com","name":"工作账号"},"refreshToken":"refresh-secret","expires":"2040-01-01T00:00:00Z"}`,
		`{"tokens":{"access_token":"header.` + claims + `.sig"},"user":{"name":"工作账号"}}`,
		`{"credentials":{"accessToken":"header.` + claims + `.sig"},"user":{"name":"工作账号"}}`,
	} {
		session, err := parseChatGPTSession(raw)
		if err != nil || session.Email != "jwt@example.com" || session.Name != "工作账号" || session.ExpiresAt.Unix() != 2000000000 {
			t.Fatalf("未正确解析 Session: %v", err)
		}
		encoded, _ := json.Marshal(session.BrowserJSON)
		if strings.Contains(string(encoded), "refresh-secret") || session.BrowserJSON["accessToken"] != session.AccessToken {
			t.Fatal("浏览器会话暴露长期凭据或未归一化 accessToken")
		}
	}
	for _, raw := range []string{`[]`, `{}`, `null`, `{"user":{"email":"x@example.com"}}`, `{"accessToken":123}`, `{"accessToken":"abc\r\ninjected"}`, `{"accessToken":"abc\u0000"}`, `{} {}`, strings.Repeat("x", 240001)} {
		if _, err := parseChatGPTSession(raw); err == nil {
			t.Fatal("无效 Session 被接受")
		}
	}
	session, err := parseChatGPTSession(`{"accessToken":"opaque-token","expires":"2020-01-01T00:00:00Z"}`)
	if err != nil || !session.ExpiresAt.Before(time.Now()) {
		t.Fatal("未识别会话过期时间")
	}
}

func TestDecryptSession(t *testing.T) {
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	raw := `{"accessToken":"test-secret"}`
	encrypted, err := encryptSession(raw)
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := decryptSession(encrypted); err != nil || plain != raw {
		t.Fatal("Session 解密失败")
	}
	for _, invalid := range []string{"", "invalid", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := decryptSession(invalid); err == nil {
			t.Fatal("无效密文未被拒绝")
		}
	}
	tampered, _ := base64.StdEncoding.DecodeString(encrypted)
	tampered[len(tampered)-1] ^= 1
	if _, err := decryptSession(base64.StdEncoding.EncodeToString(tampered)); err == nil {
		t.Fatal("篡改密文未被拒绝")
	}
}

func TestBrowserSessionRequiresAuthentication(t *testing.T) {
	s := &Server{secret: []byte("test-secret")}
	for _, route := range []struct{ method, path string }{{"POST", "/api/accounts/1/browser-session"}, {"PATCH", "/api/accounts/1/session"}} {
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
		if w.Code != 401 {
			t.Fatal("未登录请求未被拒绝")
		}
	}
}

func TestAccountSessionIntegration(t *testing.T) {
	db := walletTestDB(t)
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(1,'owner@example.com',''),(2,'other@example.com',''),(3,'__superadmin__','')`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("session-test-secret")}
	call := func(method, path string, user int64, input any, status int) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+s.token(user))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d，预期 %d", method, path, w.Code, status)
		}
		return w
	}
	raw := `{"accessToken":"access-test","refreshToken":"refresh-test","user":{"email":"chat@example.com","name":"工作账号"},"expires":"2099-01-01T00:00:00Z"}`
	w := call("POST", "/api/accounts", 1, map[string]string{"session_json": raw}, 201)
	var result struct{ Account Account }
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Account.Email != "chat@example.com" || result.Account.Label != "工作账号" {
		t.Fatal("未自动填充导入账号")
	}
	path := fmt.Sprintf("/api/accounts/%d", result.Account.ID)
	for _, user := range []int64{2, 3} {
		call("POST", path+"/browser-session", user, nil, 404)
	}
	call("PATCH", path+"/session", 2, map[string]string{"session_json": raw}, 404)
	call("PATCH", path+"/session", 3, map[string]string{"session_json": raw}, 200)
	w = call("POST", path+"/browser-session", 1, nil, 200)
	if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "refresh-test") || !strings.Contains(w.Body.String(), "access-test") {
		t.Fatal("浏览器会话缓存策略或凭据范围错误")
	}
	call("POST", "/api/accounts", 1, map[string]string{"email": "wrong@example.com", "session_json": raw}, 400)
	call("PATCH", path+"/session", 1, map[string]string{"session_json": strings.ReplaceAll(raw, "chat@example.com", "wrong@example.com")}, 409)
	call("PATCH", path+"/session", 1, map[string]string{"session_json": strings.ReplaceAll(raw, "2099", "2020")}, 200)
	call("POST", path+"/browser-session", 1, nil, 422)
	call("PATCH", path+"/session", 1, map[string]string{"session_json": raw}, 200)
	call("POST", path+"/browser-session", 1, nil, 200)
}
