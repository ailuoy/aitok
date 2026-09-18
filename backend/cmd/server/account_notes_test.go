package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAccountNotesListBeforeMigration(t *testing.T) {
	db := walletTestDB(t)
	// 仅在隔离临时表中模拟尚未新增 notes 字段的旧库。
	_, err := db.Exec(`ALTER TABLE chatgpt_accounts RENAME COLUMN notes TO pending_notes;
INSERT INTO users(id,email,password_hash,role) VALUES(1,'admin@notes.test','','admin');
INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,1,'旧库账号','account@notes.test');`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db}
	accounts, err := s.listAccounts(context.Background(), 1, true)
	if err != nil {
		t.Fatal("迁移前账号列表读取失败", err)
	}
	if len(accounts) != 1 || accounts[0].Email != "account@notes.test" || accounts[0].Notes != "" {
		t.Fatal("迁移前账号列表内容异常")
	}
}

func TestAccountNotes(t *testing.T) {
	db := walletTestDB(t)
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'admin@notes.test','','admin'),(2,'member@notes.test','','user');
INSERT INTO chatgpt_accounts(id,user_id,label,email,updated_at) VALUES(1,2,'备注测试','account@notes.test',NOW()-INTERVAL '1 day');`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("account-notes-test")}
	call := func(method, path string, body any, user int64, status int) map[string]any {
		t.Helper()
		encoded, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
		if user > 0 {
			r.Header.Set("Authorization", "Bearer "+s.token(user))
		}
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		var result map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		return result
	}
	notes := "第一行：账号用途\n" + strings.Repeat("长文本备注", 2000) + "\n<script>不执行</script>"
	call("PATCH", "/api/accounts/1/notes", map[string]any{"notes": notes}, 0, 401)
	call("PATCH", "/api/accounts/1/notes", map[string]any{"notes": notes}, 2, 403)
	result := call("PATCH", "/api/accounts/1/notes", map[string]any{"notes": notes}, 1, 200)
	if result["notes"] != notes {
		t.Fatal("多行长文本未完整保存")
	}
	var created, updated time.Time
	if err := db.QueryRow(`SELECT created_at,updated_at FROM chatgpt_accounts WHERE id=1`).Scan(&created, &updated); err != nil || updated.Before(created) {
		t.Fatal("更新时间未维护", err)
	}
	for _, path := range []string{"/api/accounts", "/api/accounts?paged=1"} {
		admin := call("GET", path, nil, 1, 200)["accounts"].([]any)[0].(map[string]any)
		if admin["notes"] != notes {
			t.Fatal("管理员列表缺少备注")
		}
		member := call("GET", path, nil, 2, 200)["accounts"].([]any)[0].(map[string]any)
		if _, exists := member["notes"]; exists {
			t.Fatal("普通用户收到管理备注")
		}
	}
	for _, input := range []any{map[string]any{}, map[string]any{"notes": nil}, map[string]any{"notes": 1}, map[string]any{"notes": strings.Repeat("字", 20001)}, map[string]any{"notes": "bad\x00"}} {
		call("PATCH", "/api/accounts/1/notes", input, 1, 400)
	}
	// 迁移重放保留已保存内容，审计不得采集正文。
	migration, err := os.ReadFile("../../migrations/030_account_notes.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err = db.QueryRow(`SELECT notes FROM chatgpt_accounts WHERE id=1`).Scan(&stored); err != nil || stored != notes {
		t.Fatal("重放迁移修改了备注", err)
	}
	var leaked bool
	if err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM operation_events WHERE before_data::text LIKE '%长文本备注%' OR after_data::text LIKE '%长文本备注%')`).Scan(&leaked); err != nil || leaked {
		t.Fatal("审计泄露备注", err)
	}
	call("PATCH", "/api/accounts/1/notes", map[string]any{"notes": ""}, 1, 200)
	call("PATCH", "/api/accounts/999/notes", map[string]any{"notes": "无记录"}, 1, 404)
	call("DELETE", "/api/accounts/1", nil, 1, 204)
	call("PATCH", "/api/accounts/1/notes", map[string]any{"notes": "不能修改"}, 1, 404)
	if err = db.QueryRow(`SELECT notes FROM chatgpt_accounts WHERE id=1 AND deleted_at IS NOT NULL`).Scan(&stored); err != nil || stored != "" {
		t.Fatal("软删除原行未保留", err)
	}
}
