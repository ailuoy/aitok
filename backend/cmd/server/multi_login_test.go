package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminMultipleLoginsRemainValid(t *testing.T) {
	db := walletTestDB(t)
	password, err := passwordHash("shared-admin-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(email,password_hash,role) VALUES('shared-admin@test.local',$1,'admin')`, password); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("multi-login-test"), admin: adminConfig{"operator", "super-admin-password"}, billing: billingConfig{BaseURL: "http://localhost:15680"}}
	call := func(method, path, token string, body any) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s %s: got %d", method, path, w.Code)
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	for _, credentials := range []map[string]string{
		{"email": "shared-admin@test.local", "password": "shared-admin-password"},
		{"username": "operator", "password": "super-admin-password"},
	} {
		var webTokens, desktopTokens []string
		for i := 0; i < 3; i++ {
			web := call("POST", "/api/login", "", credentials)["token"].(string)
			webTokens = append(webTokens, web)
			desktop := call("POST", "/api/desktop-auth", web, map[string]string{"channel": "test", "state": strings.Repeat(string(rune('a'+i)), 43)})["token"].(string)
			desktopTokens = append(desktopTokens, desktop)
			// 后续人员登录和授权助手后，之前的网页与桌面凭据仍能使用。
			for _, token := range webTokens {
				call("GET", "/api/me", token, nil)
			}
			for _, token := range desktopTokens {
				call("GET", "/api/desktop-auth/session", token, nil)
			}
		}
	}
}
