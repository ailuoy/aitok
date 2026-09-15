package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
)

func TestListPaginationIntegration(t *testing.T) {
	// walletTestDB 将迁移应用到当前连接的临时表，测试数据不会写入业务表。
	db := walletTestDB(t)
	_, err := db.Exec(`
 INSERT INTO users(id,email,password_hash) SELECT n,'member-'||n||'@test.local','' FROM generate_series(1,61) n;
 INSERT INTO users(id,email,password_hash) VALUES(1000,'__superadmin__','');
 UPDATE users SET deleted_at=NOW(),updated_at=NOW() WHERE id=61;
 INSERT INTO chatgpt_accounts(id,user_id,label,email,deleted_at) SELECT n,CASE WHEN n<=30 THEN 1 ELSE 2 END,'fixture','account-'||n||'@test.local',CASE WHEN n=61 THEN NOW() END FROM generate_series(1,61) n;
 INSERT INTO addresses(user_id,full_name,address_line1,city,state,postal_code,country,deleted_at) SELECT 1,'fixture',n||' Street','Portland','OR','97201','US',CASE WHEN n=61 THEN NOW() END FROM generate_series(1,61) n;
 INSERT INTO bank_cards(id,user_id,label,cardholder,number_ciphertext,number_fingerprint,last4,brand,exp_month,exp_year,deleted_at) SELECT n,1,'fixture','User','encrypted','fingerprint-'||n,'4242','Visa',12,2035,CASE WHEN n=61 THEN NOW() END FROM generate_series(1,61) n;
 INSERT INTO bank_card_ledger(card_id,actor_id,request_key,kind,amount_usd_minor,balance_after_usd_minor,external_reference,deleted_at) SELECT 1,1,'key-'||n,'deposit',1,n,'reference-'||n,CASE WHEN n=61 THEN NOW() END FROM generate_series(1,61) n;
 INSERT INTO card_statement_rows(card_id,external_reference,amount_usd_minor,occurred_at,actor_id,deleted_at) SELECT 1,'reference-'||n,1,NOW(),1,CASE WHEN n=61 THEN NOW() END FROM generate_series(1,61) n;
 INSERT INTO recharge_orders(order_no,user_id,account_id,account_email,package_id,package_snapshot,period_start,period_end,sale_usd_minor,request_key,deleted_at) SELECT 'order-'||n,1,n,'account-'||n||'@test.local',1,'{}','2030-01-01','2030-02-01',100,'key-'||n,CASE WHEN n=61 THEN NOW() END FROM generate_series(1,61) n;
 INSERT INTO operation_events(actor_id,entity_type,entity_id,action,request_key,deleted_at) SELECT 1,'fixture',n,'fixture','key-'||n,CASE WHEN n=61 THEN NOW() END FROM generate_series(1,61) n;
 INSERT INTO proxy_activity(user_id,device_id,event_id,data,deleted_at) SELECT 1,'fixture','event-'||n,'{}',CASE WHEN n=61 THEN NOW() END FROM generate_series(1,61) n;
 INSERT INTO payment_exceptions(event_id,order_no,kind,amount_minor,deleted_at) SELECT 'event-'||n,'order-'||n,'refund',100,CASE WHEN n=61 THEN NOW() END FROM generate_series(1,61) n;
 `)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("pagination-test-key")}
	call := func(path string, user int64, status int) map[string]json.RawMessage {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer "+s.token(user))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: got %d want %d: %s", path, w.Code, status, w.Body.String())
		}
		out := map[string]json.RawMessage{}
		if status == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return out
	}
	for _, endpoint := range []struct {
		path, key string
		total     int
	}{
		{"/api/accounts?paged=1", "accounts", 60}, {"/api/addresses?", "addresses", 60},
		{"/api/bank-cards?", "cards", 60}, {"/api/users?q=member", "users", 60},
		{"/api/bank-cards/1/ledger?", "entries", 61}, {"/api/orders?", "orders", 60},
		{"/api/audit?q=fixture", "events", 60}, {"/api/proxy-activity?", "activities", 60},
		{"/api/payment-exceptions?", "exceptions", 60}, {"/api/card-operations/1?", "statements", 61},
	} {
		t.Run(endpoint.key, func(t *testing.T) {
			for _, tc := range []struct {
				query             string
				page, size, count int
			}{
				{"", 1, 20, 20}, {"&page=2&page_size=20", 2, 20, 20},
				{"&page=2&page_size=50", 2, 50, endpoint.total - 50}, {"&page=2&page_size=100", 2, 100, 0},
			} {
				out := call(endpoint.path+tc.query, 1000, 200)
				var rows []map[string]any
				if err := json.Unmarshal(out[endpoint.key], &rows); err != nil {
					t.Fatal(err)
				}
				for key, want := range map[string]int{"total": endpoint.total, "page": tc.page, "page_size": tc.size} {
					var got int
					_ = json.Unmarshal(out[key], &got)
					if got != want {
						t.Fatalf("%s %s: got %d want %d", endpoint.path, key, got, want)
					}
				}
				if len(rows) != tc.count {
					t.Fatalf("%s: got %d rows want %d", endpoint.path, len(rows), tc.count)
				}
				if tc.page == 2 && tc.size == 20 && rows[0]["id"] != float64(endpoint.total-20) {
					t.Fatal("分页偏移错误", rows[0]["id"])
				}
			}
			for _, invalid := range []string{"0", "-1", "101", "x", "1.5"} {
				call(endpoint.path+"&page_size="+invalid, 1000, 400)
			}
			if endpoint.key != "accounts" {
				call(endpoint.path+"&page_size=100", 1, 403)
			}
		})
	}
	own := call("/api/accounts?paged=1&page_size=100", 1, 200)
	if string(own["total"]) != "30" {
		t.Fatal("分页不能扩大普通用户的数据范围", string(own["total"]))
	}
	for _, query := range []string{"q=Oregon&state_codes=OR", "q=rego&state_codes=OR"} {
		result := call(fmt.Sprintf("/api/addresses?%s&page_size=50", query), 1000, 200)
		if string(result["total"]) != "60" {
			t.Fatal("完整州名搜索未匹配缩写")
		}
	}
}
