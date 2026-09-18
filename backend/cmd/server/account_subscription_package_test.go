package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAccountSubscriptionPackage(t *testing.T) {
	db := walletTestDB(t)
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,role,deleted_at) VALUES(1,'admin@package.test','','admin',NULL),(2,'member@package.test','','user',NULL),(3,'deleted@package.test','','user',NOW());
INSERT INTO chatgpt_accounts(id,user_id,label,email,verified_plan,verified_at,subscription_ends_at,renewal_date,updated_at) VALUES(1,2,'Account','account@package.test','plus','2026-09-01','2026-10-01','2026-10-01',NOW()-INTERVAL '1 day'),(2,3,'Hidden','hidden@package.test','',NULL,NULL,NULL,NOW());
INSERT INTO recharge_packages(id,name,plan,region,currency,original_amount_minor,sale_usd_minor,months,enabled,deleted_at) VALUES(1,'Plus PH','plus','PH','USD',2000,2000,1,true,NULL),(2,'Pro US','pro_5x','US','USD',10000,10000,3,false,NULL),(3,'Deleted','plus','PH','USD',2000,2000,1,true,NOW());`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("account-package-test")}
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
	path := "/api/accounts/1/subscription-package"
	selection := func(id any) map[string]any { return map[string]any{"subscription_package_id": id} }
	call("PATCH", path, selection(1), 0, 401)
	call("PATCH", path, selection(1), 2, 403)
	call("GET", path, nil, 1, 405)
	for _, input := range []any{map[string]any{}, selection(0), selection(-1), selection("1"), selection(1.5), selection(999), selection(3)} {
		call("PATCH", path, input, 1, 400)
	}
	var before, after string
	const accountSnapshot = `SELECT (to_jsonb(a)-'subscription_package_id'-'updated_at')::text FROM chatgpt_accounts a WHERE id=1`
	if err = db.QueryRow(accountSnapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if result := call("PATCH", path, selection(1), 1, 200); result["subscription_package_id"] != float64(1) {
		t.Fatal("未返回保存的套餐")
	}
	migration, err := os.ReadFile("../../migrations/031_account_subscription_package.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(migration)); err != nil {
		t.Fatal(err)
	}
	call("PATCH", path, selection(1), 1, 200)
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM operation_events WHERE action='subscription_package'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("重复选择不能重复产生业务变更", count, err)
	}
	var updated bool
	if err = db.QueryRow(`SELECT updated_at>NOW()-INTERVAL '1 minute' FROM chatgpt_accounts WHERE id=1`).Scan(&updated); err != nil || !updated {
		t.Fatal("更新时间未维护", err)
	}
	for _, url := range []string{"/api/accounts", "/api/accounts?paged=1"} {
		admin := call("GET", url, nil, 1, 200)["accounts"].([]any)[0].(map[string]any)
		if admin["subscription_package_id"] != float64(1) {
			t.Fatal("管理员列表缺少当前产品")
		}
		member := call("GET", url, nil, 2, 200)["accounts"].([]any)[0].(map[string]any)
		if _, exists := member["subscription_package_id"]; exists {
			t.Fatal("普通用户收到管理字段")
		}
	}
	// 模拟审计写入失败，套餐修改必须一起回滚；约束仅添加到隔离临时表。
	if _, err = db.Exec(`ALTER TABLE operation_events ADD CONSTRAINT package_audit_failure CHECK (action<>'subscription_package') NOT VALID`); err != nil {
		t.Fatal(err)
	}
	call("PATCH", path, selection(2), 1, 409)
	var stored int64
	if err = db.QueryRow(`SELECT subscription_package_id FROM chatgpt_accounts WHERE id=1`).Scan(&stored); err != nil || stored != 1 {
		t.Fatal("审计失败后套餐变更未回滚", err)
	}
	if _, err = db.Exec(`ALTER TABLE operation_events DROP CONSTRAINT package_audit_failure`); err != nil {
		t.Fatal(err)
	}
	// 下架套餐仍允许作为当前订阅记录，清空不改变开通凭据及日期。
	call("PATCH", path, selection(2), 1, 200)
	call("PATCH", path, selection(nil), 1, 200)
	if err = db.QueryRow(accountSnapshot).Scan(&after); err != nil || after != before {
		t.Fatal("人工选型修改了其他账号信息", err)
	}
	call("PATCH", "/api/accounts/2/subscription-package", selection(1), 1, 404)
	call("PATCH", "/api/accounts/999/subscription-package", selection(1), 1, 404)
	call("PATCH", path, selection(2), 1, 200)
	call("DELETE", "/api/packages/2", nil, 1, 200)
	var unbound, retained bool
	if err = db.QueryRow(`SELECT subscription_package_id IS NULL FROM chatgpt_accounts WHERE id=1`).Scan(&unbound); err != nil || !unbound {
		t.Fatal("删除套餐后未解除关联", err)
	}
	if err = db.QueryRow(`SELECT deleted_at IS NOT NULL FROM recharge_packages WHERE id=2`).Scan(&retained); err != nil || !retained {
		t.Fatal("套餐软删除未保留原行", err)
	}
	call("PATCH", path, selection(2), 1, 400)
	call("PATCH", path, selection(1), 1, 200)
	call("DELETE", "/api/accounts/1", nil, 1, 204)
	call("PATCH", path, selection(nil), 1, 404)
	if err = db.QueryRow(`SELECT subscription_package_id FROM chatgpt_accounts WHERE id=1 AND deleted_at IS NOT NULL`).Scan(&stored); err != nil || stored != 1 {
		t.Fatal("账号软删除未保留原选择", err)
	}
}
