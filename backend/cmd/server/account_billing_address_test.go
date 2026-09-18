package main

import (
	"reflect"
	"testing"
)

func TestAccountBillingAddressLifecycle(t *testing.T) {
	db, call := accountSettingsTest(t)
	if _, err := db.Exec(`INSERT INTO addresses(id,address_line1,city,state,postal_code,country,user_id) VALUES (1,'100 Test Road','Portland','OR','97201','US',1),(2,'Deleted Road','Salem','OR','97301','US',1); UPDATE addresses SET full_name='Test User' WHERE id=1; UPDATE addresses SET deleted_at=NOW() WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	path := "/api/accounts/1/billing-address"
	call("PATCH", path, 2, map[string]any{"billing_address_id": 1}, 403)
	for _, input := range []any{map[string]any{}, map[string]any{"billing_address_id": 0}, map[string]any{"billing_address_id": "1"}, map[string]any{"random": true, "billing_address_id": 1}} {
		call("PATCH", path, 1, input, 400)
	}
	call("PATCH", path, 1, map[string]any{"billing_address_id": 2}, 409)
	call("PATCH", path, 1, map[string]any{"billing_address_id": 999}, 409)
	call("PATCH", "/api/accounts/5/billing-address", 1, map[string]any{"billing_address_id": 1}, 404)
	check := expectTimestampUpdate(t, db, "chatgpt_accounts", "id=1")
	bound := call("PATCH", path, 1, map[string]any{"billing_address_id": 1}, 200)
	check()
	if bound["billing_address_id"] != float64(1) || bound["billing_address_label"] != "100 Test Road, Portland, OR, 97201" {
		t.Fatal("地址绑定未返回摘要", bound)
	}
	call("PATCH", path, 1, map[string]any{"billing_address_id": 1}, 200)
	if summary, ok := bound["billing_address"].(map[string]any); !ok || summary["full_name"] != "Test User" || summary["address_line1"] != "100 Test Road" || summary["country"] != "US" {
		t.Fatal("缺少结构化地址摘要", bound)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM operation_events WHERE entity_type='account' AND entity_id=1 AND action='billing_address'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("绑定审计缺失或重复", count, err)
	}
	for _, path := range []string{"/api/accounts", "/api/accounts?paged=1"} {
		for _, row := range call("GET", path, 1, nil, 200)["accounts"].([]any) {
			a := row.(map[string]any)
			if a["id"] == float64(1) && (a["billing_address_id"] != bound["billing_address_id"] || a["billing_address_label"] != bound["billing_address_label"] || !reflect.DeepEqual(a["billing_address"], bound["billing_address"])) {
				t.Fatal("列表绑定摘要不一致", a)
			}
		}
		for _, row := range call("GET", path, 2, nil, 200)["accounts"].([]any) {
			if _, ok := row.(map[string]any)["billing_address"]; ok {
				t.Fatal("普通用户收到管理字段")
			}
		}
	}
	call("PATCH", path, 1, map[string]any{"billing_address_id": nil}, 200)
	if random := call("PATCH", path, 1, map[string]any{"random": true}, 200); random["billing_address_id"] != float64(1) {
		t.Fatal("随机绑定包含删除地址", random)
	}
	call("PATCH", "/api/accounts/2/billing-address", 1, map[string]any{"billing_address_id": 1}, 200)
	checkAddress := expectTimestampUpdate(t, db, "addresses", "id=1")
	checkAccount := expectTimestampUpdate(t, db, "chatgpt_accounts", "id=1")
	call("DELETE", "/api/addresses/1", 1, nil, 204)
	checkAddress()
	checkAccount()
	if err := db.QueryRow(`SELECT count(*) FROM chatgpt_accounts WHERE billing_address_id IS NOT NULL AND deleted_at IS NULL`).Scan(&count); err != nil || count != 0 {
		t.Fatal("删除地址没有解绑账号", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM addresses WHERE id=1 AND deleted_at IS NOT NULL`).Scan(&count); err != nil || count != 1 {
		t.Fatal("没有保留地址历史", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM operation_events WHERE action='billing_address' AND after_data->>'reason'='address_deleted'`).Scan(&count); err != nil || count != 2 {
		t.Fatal("删除解绑缺少审计", count, err)
	}
	call("GET", "/api/addresses/1", 1, nil, 404)
	call("PATCH", path, 1, map[string]any{"random": true}, 409)
}
