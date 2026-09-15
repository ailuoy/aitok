package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAccountGroupsAndLogin(t *testing.T) {
	db := walletTestDB(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(1,'owner@example.com',''),(2,'other@example.com',''),(3,'__superadmin__',''); INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,1,'Account','chat@example.com'),(2,2,'Other','other@example.com')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE users SET role='admin' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("group-test")}
	call := func(method, path string, user int64, body any, status int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		if user != 0 {
			r.Header.Set("Authorization", "Bearer "+s.token(user))
		}
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	call("GET", "/api/account-groups", 0, nil, 401)
	call("POST", "/api/account-groups", 1, map[string]any{"name": " ", "user_id": 1}, 400)
	call("POST", "/api/account-groups", 2, map[string]any{"name": "Other", "user_id": 1}, 403)
	created := call("POST", "/api/account-groups", 1, map[string]any{"name": " Team A "}, 201)["group"].(map[string]any)
	id := int64(created["id"].(float64))
	path := fmt.Sprintf("/api/account-groups/%d", id)
	call("POST", "/api/account-groups", 1, map[string]any{"name": "team a"}, 409)
	call("PATCH", path, 2, map[string]any{"name": "Not mine"}, 403)
	call("PATCH", path, 1, map[string]any{"name": "Team B"}, 200)
	group := map[string]any{"group_id": id}
	call("PATCH", "/api/accounts/1/group", 2, group, 403)
	call("PATCH", "/api/accounts/2/group", 3, group, 404)
	call("PATCH", "/api/accounts/1/group", 1, group, 200)
	call("GET", "/api/account-groups", 2, nil, 403)
	list := call("GET", "/api/account-groups", 1, nil, 200)["groups"].([]any)
	if list[0].(map[string]any)["account_count"] != float64(1) {
		t.Fatal("分组计数错误")
	}
	at := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	call("POST", "/api/accounts/1/login", 2, map[string]any{"logged_in_at": at}, 403)
	call("POST", "/api/accounts/1/login", 1, map[string]any{"logged_in_at": time.Now().Add(time.Hour)}, 400)
	call("POST", "/api/accounts/1/login", 1, map[string]any{"logged_in_at": at}, 200)
	call("POST", "/api/accounts/1/login", 1, map[string]any{"logged_in_at": at.Add(-time.Hour)}, 200)
	accounts, err := s.listAccounts(context.Background(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].GroupID == nil || *accounts[0].GroupID != id || !accounts[0].LastLoginAt.Equal(at) {
		t.Fatal("分组或登录时间错误，旧事件不应倒退时间")
	}
	call("DELETE", path, 2, nil, 403)
	call("DELETE", path, 1, nil, 204)
	accounts, err = s.listAccounts(context.Background(), 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].GroupID != nil {
		t.Fatal("删除分组应保留账号并解除绑定")
	}
}
