package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
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
	call("PATCH", "/api/accounts/2/billing-address", 1, map[string]any{"billing_address_id": 1}, 409)
	call("PATCH", "/api/accounts/2/billing-address", 1, map[string]any{"random": true}, 409)
	if available := call("GET", "/api/addresses?unbound=true", 1, nil, 200); available["total"] != float64(0) {
		t.Fatal("可绑定列表仍包含占用地址", available)
	}
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
	if err := db.QueryRow(`SELECT count(*) FROM operation_events WHERE action='billing_address' AND after_data->>'reason'='address_deleted'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("删除解绑缺少审计", count, err)
	}
	call("GET", "/api/addresses/1", 1, nil, 404)
	call("PATCH", path, 1, map[string]any{"random": true}, 409)
}

func TestAccountBillingAddressCanBeReassigned(t *testing.T) {
	db, call := accountSettingsTest(t)
	if _, err := db.Exec(`INSERT INTO addresses(id,address_line1,city,state,postal_code,country,user_id) VALUES(1,'One Road','Portland','OR','97201','US',1),(2,'Two Road','Portland','OR','97201','US',1)`); err != nil {
		t.Fatal(err)
	}
	call("PATCH", "/api/accounts/1/billing-address", 1, map[string]any{"billing_address_id": 1}, 200)
	available := call("GET", "/api/addresses?unbound=true&q=Road&page_size=1", 1, nil, 200)
	if available["total"] != float64(1) || available["addresses"].([]any)[0].(map[string]any)["id"] != float64(2) {
		t.Fatal("搜索或分页包含其他账号占用地址", available)
	}
	if result := call("PATCH", "/api/accounts/2/billing-address", 1, map[string]any{"random": true}, 200); result["billing_address_id"] != float64(2) {
		t.Fatal("随机绑定选中占用地址", result)
	}
	call("PATCH", "/api/accounts/1/billing-address", 1, map[string]any{"billing_address_id": nil}, 200)
	call("PATCH", "/api/accounts/2/billing-address", 1, map[string]any{"billing_address_id": 1}, 200)
	call("PATCH", "/api/accounts/1/billing-address", 1, map[string]any{"billing_address_id": 2}, 200)
	call("DELETE", "/api/accounts/1", 1, nil, 204)
	call("PATCH", "/api/accounts/3/billing-address", 1, map[string]any{"billing_address_id": 2}, 200)
}

func TestAccountBillingAddressConcurrentBinding(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("设置 TEST_DATABASE_URL 验证并发绑定")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	// 独立 schema 允许多连接并发，所有数据和清理均限定在本次测试内。
	admin := stdlib.OpenDB(*config)
	defer admin.Close()
	schema := fmt.Sprintf("aitok_binding_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	config.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*config)
	defer db.Close()
	body, err := os.ReadFile("../../migrations/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'binding@test.local','','admin'); INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,1,'One','one@test.local'),(2,1,'Two','two@test.local'),(3,1,'Three','three@test.local'),(4,1,'Four','four@test.local'); INSERT INTO addresses(id,address_line1,city,state,postal_code,country) VALUES(1,'One Road','Portland','OR','97201','US'),(2,'Two Road','Portland','OR','97201','US')`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db}
	for _, scenario := range []struct {
		name, body string
		accounts   []int64
		address    int
	}{
		{"explicit", `{"billing_address_id":1}`, []int64{1, 2}, 1},
		{"random", `{"random":true}`, []int64{3, 4}, 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			start := make(chan struct{})
			results := make(chan int, 2)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			for _, account := range scenario.accounts {
				go func(id int64) {
					<-start
					r := httptest.NewRequest("PATCH", "/", strings.NewReader(scenario.body)).WithContext(ctx)
					w := httptest.NewRecorder()
					s.setAccountBillingAddress(w, r, 1, id)
					results <- w.Code
				}(account)
			}
			close(start)
			statuses := map[int]int{}
			for range scenario.accounts {
				statuses[<-results]++
			}
			if statuses[200] != 1 || statuses[409] != 1 {
				t.Fatalf("同一地址应仅一个请求成功: %v", statuses)
			}
			var owners, events int
			if err := db.QueryRow(`SELECT count(*) FROM chatgpt_accounts WHERE billing_address_id=$1 AND deleted_at IS NULL`, scenario.address).Scan(&owners); err != nil || owners != 1 {
				t.Fatal("地址被重复绑定", owners, err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM operation_events WHERE action='billing_address' AND after_data->>'billing_address_id'=$1`, fmt.Sprint(scenario.address)).Scan(&events); err != nil || events != 1 {
				t.Fatal("失败绑定产生了审计或成功审计缺失", events, err)
			}
		})
	}
}
