package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAccountOwnerExactLookupAndPermissions(t *testing.T) {
	db, call := accountSettingsTest(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash,disabled) VALUES(4,'disabled@test.local','hidden-password',true),(5,'__superadmin__','',false)`); err != nil {
		t.Fatal(err)
	}
	path := "/api/accounts/1/owner"
	input := map[string]any{"email": " MEMBER@TEST.LOCAL "}
	call("POST", path, 0, input, 401)
	call("POST", path, 2, input, 403)
	call("PATCH", path, 2, map[string]any{"email": "member@test.local", "user_id": 2, "expected_owner_id": 2}, 403)
	for _, actor := range []int64{1, 5} {
		data := call("POST", path, actor, input, 200)
		u := data["user"].(map[string]any)
		if len(u) != 2 || u["id"] != float64(2) || u["email"] != "member@test.local" {
			t.Fatal("精确查找应仅返回目标用户 ID 和邮箱", data)
		}
	}
	for _, email := range []string{"", "member", "member@", "用户 <member@test.local>", "__superadmin__", strings.Repeat("a", 255) + "@test.local"} {
		call("POST", path, 1, map[string]any{"email": email}, 400)
	}
	for _, email := range []string{"member@test.loca", "%@test.local", "_ember@test.local", "gone@test.local", "disabled@test.local", "member+team@test.local"} {
		call("POST", path, 1, map[string]any{"email": email}, 404)
	}
	call("POST", path, 1, map[string]any{"email": "member@test.local", "q": "member"}, 400)
	call("POST", "/api/accounts/5/owner", 1, input, 404)
	call("POST", "/api/accounts/999/owner", 1, input, 404)
	call("GET", path, 1, nil, 405)
	var owner, group, changes int
	if err := db.QueryRow(`SELECT user_id,group_id FROM chatgpt_accounts WHERE id=1`).Scan(&owner, &group); err != nil || owner != 2 || group != 2 {
		t.Fatal("查找用户不能修改账号", owner, group, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM operation_events WHERE entity_type='account' AND action='owner'`).Scan(&changes); err != nil || changes != 0 {
		t.Fatal("查找用户不应写入归属变更记录", changes, err)
	}
}

func TestAccountOwnerBindingVisibilityPreservationAndAudit(t *testing.T) {
	db, call := accountSettingsTest(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(4,'target@test.local',''); UPDATE chatgpt_accounts SET payment_card_id=1 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	check := expectTimestampUpdate(t, db, "chatgpt_accounts", "id=1")
	var before, after string
	query := `SELECT (to_jsonb(a)-'user_id'-'group_id'-'updated_at')::text FROM chatgpt_accounts a WHERE id=1`
	if err := db.QueryRow(query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"email": "target@test.local", "user_id": 4, "expected_owner_id": 2}
	data := call("PATCH", "/api/accounts/1/owner", 1, input, 200)
	check()
	if data["user_id"] != float64(4) || data["owner_email"] != "target@test.local" || data["group_id"] != nil {
		t.Fatal("归属或分组未更新", data)
	}
	if err := db.QueryRow(query).Scan(&after); err != nil || before != after {
		t.Fatal("归属变更不应修改 Session、付款卡、订阅或其他账号字段", err)
	}
	// 同一请求重放不重复变更；同用户绑定保留已经重新设置的分组。
	if _, err := db.Exec(`INSERT INTO account_groups(id,user_id,name) VALUES(3,4,'Target'); UPDATE chatgpt_accounts SET group_id=3 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	data = call("PATCH", "/api/accounts/1/owner", 1, input, 200)
	if data["group_id"] != float64(3) {
		t.Fatal("重复绑定清除了当前分组", data)
	}
	for _, path := range []string{"/api/accounts", "/api/accounts?paged=1"} {
		for _, row := range call("GET", path, 2, nil, 200)["accounts"].([]any) {
			if row.(map[string]any)["id"] == float64(1) {
				t.Fatal("原所属用户仍能查看已转出的账号")
			}
		}
		rows := call("GET", path, 4, nil, 200)["accounts"].([]any)
		if len(rows) != 1 || rows[0].(map[string]any)["id"] != float64(1) {
			t.Fatal("新所属用户无法查看账号", rows)
		}
		encoded, _ := json.Marshal(rows)
		for _, field := range []string{"owner_email", "payment_card", "session_ciphertext", "group_id"} {
			if strings.Contains(string(encoded), field) {
				t.Fatal("普通用户收到管理字段", string(encoded))
			}
		}
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM operation_events WHERE actor_id=1 AND entity_type='account' AND entity_id=1 AND action='owner' AND before_data->>'user_id'='2' AND before_data->>'group_id'='2' AND after_data->>'user_id'='4' AND after_data->>'owner_email'='target@test.local' AND after_data->'_request'->>'resource'='/api/accounts/1/owner'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("归属审计缺失或重复", count, err)
	}
}

func TestAccountOwnerBindingRejectsChangedOrInvalidTargets(t *testing.T) {
	db, call := accountSettingsTest(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(4,'target@test.local',''); INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(6,4,'Duplicate',' A@Test.Local ')`); err != nil {
		t.Fatal(err)
	}
	path := "/api/accounts/1/owner"
	for _, input := range []any{
		map[string]any{"email": "target@test.local"},
		map[string]any{"user_id": 4, "expected_owner_id": 2},
		map[string]any{"email": "target@test.local", "user_id": 4},
		map[string]any{"email": "target@test.local", "user_id": -1, "expected_owner_id": 2},
	} {
		call("PATCH", path, 1, input, 400)
	}
	input := map[string]any{"email": "target@test.local", "user_id": 4, "expected_owner_id": 2}
	call("PATCH", path, 1, input, 409)
	if _, err := db.Exec(`UPDATE chatgpt_accounts SET deleted_at=NOW(),updated_at=NOW() WHERE id=6`); err != nil {
		t.Fatal(err)
	}
	call("PATCH", path, 1, map[string]any{"email": "target@test.local", "user_id": 2, "expected_owner_id": 2}, 409)
	call("PATCH", path, 1, map[string]any{"email": "target@test.local", "user_id": 4, "expected_owner_id": 1}, 409)
	if _, err := db.Exec(`UPDATE users SET disabled=true,updated_at=NOW() WHERE id=4`); err != nil {
		t.Fatal(err)
	}
	call("PATCH", path, 1, input, 404)
	if _, err := db.Exec(`UPDATE users SET disabled=false,deleted_at=NOW(),updated_at=NOW() WHERE id=4; INSERT INTO users(id,email,password_hash) VALUES(5,'target@test.local','')`); err != nil {
		t.Fatal(err)
	}
	call("PATCH", path, 1, input, 409)
	var owner, group int
	if err := db.QueryRow(`SELECT user_id,group_id FROM chatgpt_accounts WHERE id=1`).Scan(&owner, &group); err != nil || owner != 2 || group != 2 {
		t.Fatal("失败绑定修改了账号", owner, group, err)
	}
	input["user_id"] = 5
	call("PATCH", path, 1, input, 200)
	if _, err := db.Exec(`UPDATE chatgpt_accounts SET deleted_at=NOW(),updated_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	call("PATCH", path, 1, input, 404)
}

func TestAccountOwnerAuditFailureRollsBack(t *testing.T) {
	db, call := accountSettingsTest(t)
	// 仅约束测试连接中的临时审计表，模拟审计写入失败。
	if _, err := db.Exec(`ALTER TABLE pg_temp.operation_events ADD CONSTRAINT reject_owner_audit CHECK(action<>'owner')`); err != nil {
		t.Fatal(err)
	}
	call("PATCH", "/api/accounts/1/owner", 1, map[string]any{"email": "admin@test.local", "user_id": 1, "expected_owner_id": 2}, 409)
	var owner, group int
	if err := db.QueryRow(`SELECT user_id,group_id FROM chatgpt_accounts WHERE id=1`).Scan(&owner, &group); err != nil || owner != 2 || group != 2 {
		t.Fatal("审计失败未回滚账号归属和分组", owner, group, err)
	}
}
