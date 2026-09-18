package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDesktopAuthorizationScopeAndRevocation(t *testing.T) {
	db := walletTestDB(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'desktop@test.local','','admin'),(2,'member@test.local','','user')`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("desktop-auth-test"), billing: billingConfig{BaseURL: "http://localhost:15680"}}
	call := func(method, path, token, body string, status int) map[string]any {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d want %d", method, path, w.Code, status)
		}
		var value map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &value)
		return value
	}
	state := strings.Repeat("a", 43)
	body := `{"channel":"test","state":"` + state + `"}`
	call("POST", "/api/desktop-auth", "", body, 401)
	call("POST", "/api/desktop-auth", s.token(2), body, 403)
	call("POST", "/api/desktop-auth", s.token(1), `{"channel":"other","state":"invalid"}`, 400)
	token := call("POST", "/api/desktop-auth", s.token(1), body, 200)["token"].(string)
	result := call("GET", "/api/desktop-auth/session", token, "", 200)
	if result["state"] != state || result["channel"] != "test" || result["user"].(map[string]any)["id"] != float64(1) {
		t.Fatal("授权身份或环境错误")
	}
	call("GET", "/api/me", token, "", 401)
	call("GET", "/api/desktop-auth/session", s.token(1), "", 401)
	call("GET", "/api/desktop-auth/session", token+"x", "", 401)
	if _, err := db.Exec(`UPDATE users SET session_version=session_version+1,updated_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	call("GET", "/api/desktop-auth/session", token, "", 401)
	token = call("POST", "/api/desktop-auth", s.token(1), body, 200)["token"].(string)
	if _, err := db.Exec(`UPDATE users SET disabled=true,updated_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	call("GET", "/api/desktop-auth/session", token, "", 401)
}
