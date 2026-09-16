package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func accountSettingsTest(t *testing.T) (*sql.DB, func(string, string, int64, any, int) map[string]any) {
	t.Helper()
	db := walletTestDB(t)
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'admin@test.local','','admin'),(2,'member@test.local','','user'),(3,'gone@test.local','','user');
INSERT INTO account_groups(id,user_id,name) VALUES(1,2,'Alpha'),(2,2,'Zulu');
INSERT INTO chatgpt_accounts(id,user_id,label,email,group_id,renewal_date,last_login_at,session_ciphertext,renewal_enabled) VALUES
(1,2,'Alpha','a@test.local',2,'2030-01-01','2026-01-01','test-session',true),
(2,2,'Beta','b@test.local',1,'2030-02-01','2026-02-01','',false),
(3,1,'Gamma','c@test.local',NULL,NULL,NULL,'',true),
(4,2,'Alpha','d@test.local',1,'2030-01-01','2026-01-01','test-session',true),
(5,3,'Hidden','hidden@test.local',NULL,NULL,NULL,'',true);
UPDATE users SET deleted_at=NOW() WHERE id=3;
INSERT INTO bank_cards(id,user_id,label,cardholder,number_ciphertext,number_fingerprint,last4,brand,exp_month,exp_year,status,balance_usd_minor,cvc_ciphertext) VALUES
(1,1,'Default card','Test','secret-number','one','4242','Visa',12,2099,'active',5000,'secret-cvc'),
(2,1,'Frozen card','Test','secret-number','two','4444','Mastercard',12,2099,'frozen',6000,''),
(3,1,'Expired card','Test','secret-number','three','0005','Amex',1,2000,'active',0,''),
(4,1,'Deleted card','Test','secret-number','four','1111','Visa',12,2099,'active',7000,'');
UPDATE bank_cards SET deleted_at=NOW() WHERE id=4;`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("account-settings-test")}
	call := func(method, path string, user int64, body any, status int) map[string]any {
		t.Helper()
		encoded, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
		r.Header.Set("Authorization", "Bearer "+s.token(user))
		r.Header.Set("X-Aitok-Page", "/admin/accounts")
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		var data map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &data)
		return data
	}
	return db, call
}

func TestAccountSortingAcrossPages(t *testing.T) {
	_, call := accountSettingsTest(t)
	for _, c := range []struct {
		key, direction string
		ids            []int
	}{
		{"account", "asc", []int{4, 1, 2, 3}}, {"account", "desc", []int{3, 2, 4, 1}},
		{"renewal_date", "asc", []int{4, 1, 2, 3}}, {"renewal_date", "desc", []int{2, 4, 1, 3}},
		{"last_login_at", "asc", []int{4, 1, 2, 3}}, {"last_login_at", "desc", []int{2, 4, 1, 3}},
		{"group", "asc", []int{4, 2, 1, 3}}, {"session", "asc", []int{3, 2, 4, 1}},
		{"renewal_enabled", "asc", []int{2, 4, 3, 1}}, {"owner", "asc", []int{3, 4, 2, 1}},
	} {
		t.Run(c.key+"_"+c.direction, func(t *testing.T) {
			for page := 1; page <= 2; page++ {
				data := call("GET", fmt.Sprintf("/api/accounts?paged=1&page_size=2&page=%d&sort=%s&direction=%s", page, c.key, c.direction), 1, nil, 200)
				if data["total"] != float64(4) {
					t.Fatal("分页总数不正确", data)
				}
				rows := data["accounts"].([]any)
				for i, row := range rows {
					if row.(map[string]any)["id"] != float64(c.ids[(page-1)*2+i]) {
						t.Fatalf("跨页排序错误: %v", data)
					}
				}
			}
		})
	}
	for _, query := range []string{"sort=unknown", "sort=id%3BDROP%20TABLE%20users", "sort=account&direction=sideways"} {
		call("GET", "/api/accounts?paged=1&"+query, 1, nil, 400)
	}
	filtered := call("GET", "/api/accounts?paged=1&sort=account&direction=asc&group=1&q=Alpha", 1, nil, 200)
	if filtered["total"] != float64(1) || filtered["accounts"].([]any)[0].(map[string]any)["id"] != float64(4) {
		t.Fatal("筛选后排序错误", filtered)
	}
	call("GET", "/api/accounts?paged=1&sort=payment_card", 2, nil, 400)
	for _, row := range call("GET", "/api/accounts?paged=1&sort=account&direction=asc", 2, nil, 200)["accounts"].([]any) {
		data := row.(map[string]any)
		for _, field := range []string{"payment_card_id", "renewal_enabled", "session_ciphertext", "owner_email"} {
			if _, ok := data[field]; ok {
				t.Fatal("普通用户收到管理字段", field)
			}
		}
	}
}

func TestAccountRenewalSettingPersistsAndAudits(t *testing.T) {
	db, call := accountSettingsTest(t)
	check := expectTimestampUpdate(t, db, "chatgpt_accounts", "id=1")
	call("PATCH", "/api/accounts/1/subscription", 1, map[string]any{"renewal_enabled": false}, 200)
	check()
	call("PATCH", "/api/accounts/1/subscription", 1, map[string]any{"renewal_enabled": false}, 200)
	for _, input := range []any{map[string]any{}, map[string]any{"renewal_enabled": nil}, map[string]any{"renewal_enabled": "false"}} {
		call("PATCH", "/api/accounts/1/subscription", 1, input, 400)
	}
	call("PATCH", "/api/accounts/1/subscription", 2, map[string]any{"renewal_enabled": true}, 403)
	call("PATCH", "/api/accounts/5/subscription", 1, map[string]any{"renewal_enabled": true}, 404)
	var enabled bool
	var audits int
	if err := db.QueryRow(`SELECT renewal_enabled FROM chatgpt_accounts WHERE id=1`).Scan(&enabled); err != nil || enabled {
		t.Fatal("设置未保存", err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM operation_events WHERE entity_type='account' AND entity_id=1 AND action='subscription' AND before_data->>'renewal_enabled'='true' AND after_data->>'renewal_enabled'='false'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("续订审计不完整或重复", audits, err)
	}
}

func TestAccountPaymentCardBindingLifecycle(t *testing.T) {
	db, call := accountSettingsTest(t)
	options := call("GET", "/api/accounts/payment-cards", 1, nil, 200)["cards"].([]any)
	if len(options) != 1 || options[0].(map[string]any)["id"] != float64(1) {
		t.Fatal("候选包含不可用卡", options)
	}
	encoded, _ := json.Marshal(options)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "cardholder") {
		t.Fatal("候选泄露敏感字段")
	}
	call("GET", "/api/accounts/payment-cards", 2, nil, 403)
	call("PATCH", "/api/accounts/1/payment-card", 2, map[string]any{"payment_card_id": 1}, 403)
	for _, input := range []any{map[string]any{}, map[string]any{"payment_card_id": -1}, map[string]any{"payment_card_id": true}, map[string]any{"payment_card_id": "1"}} {
		call("PATCH", "/api/accounts/1/payment-card", 1, input, 400)
	}
	for _, id := range []int{2, 3, 4, 999} {
		call("PATCH", "/api/accounts/1/payment-card", 1, map[string]any{"payment_card_id": id}, 409)
	}
	call("PATCH", "/api/accounts/5/payment-card", 1, map[string]any{"payment_card_id": 1}, 404)
	check := expectTimestampUpdate(t, db, "chatgpt_accounts", "id=1")
	call("PATCH", "/api/accounts/1/payment-card", 1, map[string]any{"payment_card_id": 1}, 200)
	check()
	call("PATCH", "/api/accounts/1/payment-card", 1, map[string]any{"payment_card_id": 1}, 200)
	for _, path := range []string{"/api/accounts", "/api/accounts?paged=1&sort=payment_card&direction=asc"} {
		data := call("GET", path, 1, nil, 200)
		for _, row := range data["accounts"].([]any) {
			a := row.(map[string]any)
			if a["id"] == float64(1) && (a["payment_card_id"] != float64(1) || a["payment_card_last4"] != "4242" || a["payment_card_available"] != true) {
				t.Fatal("账号未返回已保存绑定", a)
			}
		}
	}
	if _, err := db.Exec(`UPDATE bank_cards SET status='frozen' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	rows := call("GET", "/api/accounts?paged=1&sort=payment_card&direction=asc", 1, nil, 200)["accounts"].([]any)
	if rows[0].(map[string]any)["payment_card_available"] != false {
		t.Fatal("冻结卡仍标记可用")
	}
	call("PATCH", "/api/accounts/1/payment-card", 1, map[string]any{"payment_card_id": nil}, 200)
	if _, err := db.Exec(`UPDATE bank_cards SET status='active' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	call("PATCH", "/api/accounts/1/payment-card", 1, map[string]any{"payment_card_id": 1}, 200)
	check = expectTimestampUpdate(t, db, "chatgpt_accounts", "id=1")
	call("DELETE", "/api/bank-cards/1", 1, nil, 204)
	check()
	var bound sql.NullInt64
	if err := db.QueryRow(`SELECT payment_card_id FROM chatgpt_accounts WHERE id=1`).Scan(&bound); err != nil || bound.Valid {
		t.Fatal("删卡未解除绑定", err)
	}
	var balance int64
	var deleted bool
	if err := db.QueryRow(`SELECT balance_usd_minor,deleted_at IS NOT NULL FROM bank_cards WHERE id=1`).Scan(&balance, &deleted); err != nil || balance != 5000 || !deleted {
		t.Fatal("删卡未保留余额和原行", balance, deleted, err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM operation_events WHERE entity_type='account' AND entity_id=1 AND action='payment_card'`).Scan(&count); err != nil || count != 4 {
		t.Fatal("绑卡、解绑、删卡审计不正确", count, err)
	}
}
