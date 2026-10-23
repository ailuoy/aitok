package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAccountUsageStatus(t *testing.T) {
	db := walletTestDB(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'admin@status.test','','admin'),(2,'member@status.test','','user'),(3,'deleted@status.test','','user');
INSERT INTO chatgpt_accounts(id,user_id,label,email,renewal_enabled,updated_at) VALUES(1,2,'状态测试','account@status.test',TRUE,NOW()-INTERVAL '1 day'),(2,3,'已删除用户账号','deleted-account@status.test',FALSE,NOW());
UPDATE users SET deleted_at=NOW(),updated_at=NOW() WHERE id=3;`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("account-status-test")}
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
	const path = "/api/accounts/1/usage-status"
	const snapshot = `SELECT (to_jsonb(a)-'usage_status'-'discard_reason'-'updated_at')::text FROM chatgpt_accounts a WHERE id=1`
	var before, after string
	if err := db.QueryRow(snapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}
	initial := call("GET", "/api/accounts?paged=1", nil, 1, 200)["accounts"].([]any)[0].(map[string]any)
	if initial["usage_status"] != "in_use" || initial["discard_reason"] != "" {
		t.Fatal("现有账号应默认使用中且无废弃理由", initial)
	}
	reason := "账号暂时停用\n<script>仅保存为文本</script>"
	input := map[string]any{"usage_status": "discarded", "discard_reason": reason}
	call("PATCH", path, input, 0, 401)
	call("PATCH", path, input, 2, 403)
	call("GET", path, nil, 1, 405)
	result := call("PATCH", path, input, 1, 200)
	if result["usage_status"] != "discarded" || result["discard_reason"] != reason {
		t.Fatal("状态及多行理由未完整保存", result)
	}
	call("PATCH", path, input, 1, 200)
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM operation_events WHERE entity_type='account' AND entity_id=1 AND action='usage_status'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("相同状态和理由不得重复写入业务审计", count, err)
	}
	var updated, leaked bool
	if err := db.QueryRow(`SELECT updated_at>NOW()-INTERVAL '1 minute' FROM chatgpt_accounts WHERE id=1`).Scan(&updated); err != nil || !updated {
		t.Fatal("未维护更新时间", err)
	}
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM operation_events WHERE before_data::text LIKE '%账号暂时停用%' OR after_data::text LIKE '%账号暂时停用%')`).Scan(&leaked); err != nil || leaked {
		t.Fatal("审计不得复制废弃理由正文", err)
	}
	if page := call("GET", "/api/accounts?paged=1", nil, 1, 200); page["total"] != float64(0) {
		t.Fatal("默认管理列表不应显示废弃账号", page)
	}
	for _, url := range []string{"/api/accounts", "/api/accounts?paged=1&usage_status=discarded"} {
		admin := call("GET", url, nil, 1, 200)["accounts"].([]any)[0].(map[string]any)
		if admin["usage_status"] != "discarded" || admin["discard_reason"] != reason {
			t.Fatal("管理员列表缺少状态或理由", admin)
		}
		member := call("GET", url, nil, 2, 200)["accounts"].([]any)[0].(map[string]any)
		for _, field := range []string{"usage_status", "discard_reason"} {
			if _, exists := member[field]; exists {
				t.Fatal("普通用户收到管理字段", field)
			}
		}
	}
	for _, invalid := range []any{map[string]any{}, map[string]any{"usage_status": "deleted"}, map[string]any{"usage_status": true}, map[string]any{"usage_status": "discarded", "discard_reason": 1}, map[string]any{"usage_status": "discarded", "discard_reason": strings.Repeat("字", 2001)}, map[string]any{"usage_status": "discarded", "discard_reason": "bad\x00"}} {
		call("PATCH", path, invalid, 1, 400)
	}
	// 审计写入失败时状态也必须回滚；约束仅作用于隔离临时表。
	if _, err := db.Exec(`ALTER TABLE operation_events ADD CONSTRAINT status_audit_failure CHECK(action<>'usage_status') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	call("PATCH", path, map[string]any{"usage_status": "in_use"}, 1, 409)
	var status, stored string
	if err := db.QueryRow(`SELECT usage_status,discard_reason FROM chatgpt_accounts WHERE id=1`).Scan(&status, &stored); err != nil || status != "discarded" || stored != reason {
		t.Fatal("审计失败后状态未回滚", status, stored, err)
	}
	if _, err := db.Exec(`ALTER TABLE operation_events DROP CONSTRAINT status_audit_failure`); err != nil {
		t.Fatal(err)
	}
	result = call("PATCH", path, map[string]any{"usage_status": "in_use"}, 1, 200)
	if result["discard_reason"] != reason {
		t.Fatal("恢复使用应保留最近废弃理由")
	}
	if page := call("GET", "/api/accounts?paged=1", nil, 1, 200); page["total"] != float64(1) {
		t.Fatal("恢复使用后必须回到默认列表", page)
	}
	if page := call("GET", "/api/accounts?paged=1&usage_status=discarded", nil, 1, 200); page["total"] != float64(0) {
		t.Fatal("恢复使用后必须移出废弃列表", page)
	}
	// 可不填写理由，也可在废弃后补充、修改或清空。
	call("PATCH", path, map[string]any{"usage_status": "discarded", "discard_reason": ""}, 1, 200)
	call("PATCH", path, map[string]any{"usage_status": "discarded", "discard_reason": strings.Repeat("字", 2000)}, 1, 200)
	call("PATCH", path, input, 1, 200)
	migration, err := os.ReadFile("../../migrations/033_account_usage_status.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT usage_status,discard_reason FROM chatgpt_accounts WHERE id=1`).Scan(&status, &stored); err != nil || status != "discarded" || stored != reason {
		t.Fatal("重放迁移修改了状态或理由", err)
	}
	if err = db.QueryRow(snapshot).Scan(&after); err != nil || before != after {
		t.Fatal("状态操作修改了账号其他数据", err)
	}
	call("PATCH", "/api/accounts/2/usage-status", input, 1, 404)
	call("PATCH", "/api/accounts/999/usage-status", input, 1, 404)
	call("DELETE", "/api/accounts/1", nil, 1, 204)
	call("PATCH", path, map[string]any{"usage_status": "in_use"}, 1, 404)
	if err = db.QueryRow(`SELECT usage_status,discard_reason FROM chatgpt_accounts WHERE id=1 AND deleted_at IS NOT NULL`).Scan(&status, &stored); err != nil || status != "discarded" || stored != reason {
		t.Fatal("软删除必须保留状态和理由", err)
	}
	if len(call("GET", "/api/accounts", nil, 1, 200)["accounts"].([]any)) != 0 {
		t.Fatal("普通列表仍显示已删除账号")
	}
}

func TestAccountUsageStatusBeforeMigration(t *testing.T) {
	db := walletTestDB(t)
	// 在隔离表模拟旧库；迁移前可读列表，迁移后旧账号获得默认值。
	if _, err := db.Exec(`ALTER TABLE chatgpt_accounts RENAME COLUMN usage_status TO pending_status;
ALTER TABLE chatgpt_accounts RENAME COLUMN discard_reason TO pending_reason;
ALTER TABLE chatgpt_accounts RENAME CONSTRAINT chatgpt_accounts_usage_status_check TO pending_status_check;
ALTER TABLE chatgpt_accounts RENAME CONSTRAINT chatgpt_accounts_discard_reason_check TO pending_reason_check;
INSERT INTO users(id,email,password_hash,role) VALUES(1,'admin@legacy-status.test','','admin');
INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,1,'旧账号','account@legacy-status.test');`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db}
	accounts, err := s.listAccounts(context.Background(), 1, true)
	if err != nil || len(accounts) != 1 || accounts[0].UsageStatus != "in_use" || accounts[0].DiscardReason != "" {
		t.Fatal("旧库列表默认状态错误", accounts, err)
	}
	migration, err := os.ReadFile("../../migrations/033_account_usage_status.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	var status, reason string
	if err = db.QueryRow(`SELECT usage_status,discard_reason FROM chatgpt_accounts WHERE id=1`).Scan(&status, &reason); err != nil || status != "in_use" || reason != "" {
		t.Fatal("旧账号默认值错误", err)
	}
}
