package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUserPasswordChange(t *testing.T) {
	db := walletTestDB(t)
	oldPassword, newPassword := "original-password", "new-user-password"
	encoded, err := passwordHash(oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO users(id,email,password_hash,role,deleted_at,disabled) VALUES
(1,'member@example.com',$1,'user',NULL,false),
(2,'manager@example.com',$1,'admin',NULL,false),
(3,'__superadmin__','','',NULL,false),
(4,'deleted@example.com',$1,'user',NOW(),false),
(5,'disabled@example.com',$1,'user',NULL,true),
(6,'code-only@example.com','','user',NULL,false),
(7,'legacy@example.com',$2,'user',NULL,false)`, encoded, hash("short"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("password-test"), admin: adminConfig{"operator", "admin-password"}}
	tokens := map[int64]string{1: s.token(1), 2: s.token(2), 3: s.token(3)}
	call := func(method, path string, actor int64, input any, status int) string {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		if actor != 0 {
			r.Header.Set("Authorization", "Bearer "+tokens[actor])
		}
		r.Header.Set("X-Aitok-Page", "/admin/users")
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		for _, secret := range []string{oldPassword, newPassword, encoded} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("响应泄露密码或哈希")
			}
		}
		return w.Body.String()
	}
	input := func(password, confirm string) map[string]string {
		return map[string]string{"new_password": password, "confirm_password": confirm}
	}
	valid := input(newPassword, newPassword)
	call("PATCH", "/api/users/1/password", 0, valid, 401)
	call("PATCH", "/api/users/1/password", 1, valid, 403)
	call("PATCH", "/api/users/1/password", 2, valid, 403)
	call("GET", "/api/users/1/password", 3, nil, 405)
	for _, id := range []int{3, 4, 999} {
		call("PATCH", fmt.Sprintf("/api/users/%d/password", id), 3, valid, 404)
	}
	call("PATCH", "/api/users/0/password", 3, valid, 404)
	for _, body := range []any{
		map[string]string{}, input(newPassword, ""),
		input(newPassword, "different-password"),
		input("short", "short"),
		input(strings.Repeat("密", 25), strings.Repeat("密", 25)),
		map[string]any{"new_password": true},
	} {
		call("PATCH", "/api/users/1/password", 3, body, 400)
	}
	var stored string
	var version, audits int
	if err := db.QueryRow(`SELECT password_hash,session_version FROM users WHERE id=1`).Scan(&stored, &version); err != nil || stored != encoded || version != 0 {
		t.Fatal("失败请求修改了密码或会话", err)
	}
	check := expectTimestampUpdate(t, db, "users", "id=1")
	call("PATCH", "/api/users/1/password", 3, valid, 200)
	check()
	if err := db.QueryRow(`SELECT password_hash,session_version FROM users WHERE id=1`).Scan(&stored, &version); err != nil || !strings.HasPrefix(stored, "$2") || !passwordMatches(stored, newPassword) || passwordMatches(stored, oldPassword) || version != 1 {
		t.Fatal("密码未加密更新或会话未撤销", err)
	}
	call("GET", "/api/me", 1, nil, 401)
	call("POST", "/api/login", 0, map[string]string{"email": "member@example.com", "password": oldPassword}, 401)
	call("POST", "/api/login", 0, map[string]string{"email": "member@example.com", "password": newPassword}, 200)
	if err := db.QueryRow(`SELECT count(*) FROM operation_events WHERE entity_type='user' AND entity_id=1 AND action='password' AND after_data->>'password_changed'='true' AND after_data->>'sessions_revoked'='true'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("密码审计缺失或重复", audits, err)
	}
	var auditData string
	if err := db.QueryRow(`SELECT string_agg(before_data::text||after_data::text,' ') FROM operation_events`).Scan(&auditData); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{oldPassword, newPassword, encoded, stored} {
		if strings.Contains(auditData, secret) {
			t.Fatal("审计泄露密码或哈希")
		}
	}
	call("PATCH", "/api/users/5/password", 3, valid, 200)
	call("POST", "/api/login", 0, map[string]string{"email": "disabled@example.com", "password": newPassword}, 401)
	call("PATCH", "/api/users/7/password", 3, valid, 200)
	// 未设置密码的验证码用户也可以由超管直接设置密码。
	call("PATCH", "/api/users/6/password", 3, valid, 200)
	call("POST", "/api/login", 0, map[string]string{"email": "code-only@example.com", "password": newPassword}, 200)
	// 超管修改密码仍限制操作频率。
	for i := 0; i < 10; i++ {
		call("PATCH", "/api/users/2/password", 3, valid, 200)
	}
	call("PATCH", "/api/users/2/password", 3, valid, 429)
}

func TestUserPasswordAuditFailureRollsBack(t *testing.T) {
	db := walletTestDB(t)
	encoded, err := passwordHash("original-password")
	if err != nil {
		t.Fatal(err)
	}
	// 仅在隔离临时表注入审计失败，验证密码和会话版本一起回滚。
	_, err = db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(1,'member@example.com',$1),(3,'__superadmin__','')`, encoded)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`ALTER TABLE operation_events ADD CONSTRAINT reject_password_test CHECK(action<>'password')`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("password-test"), admin: adminConfig{"operator", "admin-password"}}
	r := httptest.NewRequest("PATCH", "/api/users/1/password", strings.NewReader(`{"new_password":"new-user-password","confirm_password":"new-user-password"}`))
	r.Header.Set("Authorization", "Bearer "+s.token(3))
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code < 400 {
		t.Fatal("审计失败仍返回成功")
	}
	var stored string
	var version int
	if err := db.QueryRow(`SELECT password_hash,session_version FROM users WHERE id=1`).Scan(&stored, &version); err != nil || stored != encoded || version != 0 {
		t.Fatal("审计失败后密码未回滚", err)
	}
}
