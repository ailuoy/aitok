package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseCardUSD(t *testing.T) {
	for _, tt := range []struct {
		value string
		cents int64
		valid bool
	}{
		{"0.01", 1, true}, {"12.3", 1230, true}, {"150.25", 15025, true}, {"10000000000", maxCardMoneyMinor, true},
		{"0", 0, false}, {"-1", 0, false}, {"1.001", 0, false}, {"1e3", 0, false}, {"NaN", 0, false}, {"01", 0, false}, {" 1 ", 0, false}, {"10000000000.01", 0, false}, {"999999999999999999999", 0, false},
	} {
		t.Run(tt.value, func(t *testing.T) {
			value, valid := parseCardUSD(tt.value)
			if valid != tt.valid || valid && value != tt.cents {
				t.Fatalf("金额解析错误：%q => %d %t", tt.value, value, valid)
			}
		})
	}
}

func TestBankCardLedgerIntegration(t *testing.T) {
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	db := walletTestDB(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'owner@test.local','','user'),(2,'other@test.local','','user'),(3,'admin@test.local','','admin');INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,1,'Account One','one@test.local'),(2,2,'Other Account','two@test.local'),(3,1,'Account Three','three@test.local')`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("ledger-test")}
	routes := s.routes()
	call := func(method, path string, user int64, body any, status int) map[string]any {
		t.Helper()
		encoded, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
		if user > 0 {
			r.Header.Set("Authorization", "Bearer "+s.token(user))
		}
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		return result
	}
	cardInput := map[string]any{"label": "Test card", "cardholder": "Test User", "number": "4242424242424242", "exp_month": 12, "exp_year": 2035, "balance_usd_minor": 999999}
	created := call("POST", "/api/bank-cards", 1, cardInput, 201)["card"].(map[string]any)
	if created["balance_usd_minor"] != float64(0) {
		t.Fatal("不能通过卡片编辑篡改余额")
	}
	cardID := int64(created["id"].(float64))
	cardPath := fmt.Sprintf("/api/bank-cards/%d", cardID)
	path := cardPath + "/ledger"
	deposit := map[string]any{"kind": "deposit", "amount_usd": "500.00", "request_key": "initial-deposit-0001"}
	call("GET", path, 0, nil, 401)
	call("GET", path, 2, nil, 404)
	call("POST", path, 2, deposit, 404)
	call("POST", path, 1, map[string]any{"kind": "deposit", "amount_usd": "1.111", "request_key": "invalid-amount-0001"}, 400)
	opening := call("POST", path, 1, deposit, 201)["entry"].(map[string]any)
	if opening["kind"] != "opening" || opening["balance_after_usd_minor"] != float64(50000) {
		t.Fatal("首次存入未记录初始余额")
	}
	if call("POST", path, 1, deposit, 200)["replayed"] != true {
		t.Fatal("同一请求未去重")
	}
	deposit["amount_usd"] = "600.00"
	call("POST", path, 1, deposit, 409)
	charge := map[string]any{"kind": "subscription", "amount_usd": "150.25", "account_id": 2, "request_key": "account-charge-0001", "notes": "卡平台实际扣款"}
	call("POST", path, 1, charge, 404)
	charge["account_id"] = 1
	charge["amount_usd"] = "501.00"
	call("POST", path, 1, charge, 409)
	charge["amount_usd"] = "150.25"
	entry := call("POST", path, 1, charge, 201)["entry"].(map[string]any)
	if entry["amount_usd_minor"] != float64(-15025) || entry["balance_after_usd_minor"] != float64(34975) || entry["original_php_minor"] != float64(891964) || entry["account_email"] != "one@test.local" {
		t.Fatal("扣款、余额或原币快照错误")
	}
	call("POST", path, 1, charge, 200)
	charge["request_key"] = "account-charge-0002"
	call("POST", path, 1, charge, 409)
	otherCard := call("POST", "/api/bank-cards", 2, cardInput, 201)["card"].(map[string]any)
	otherPath := fmt.Sprintf("/api/bank-cards/%.0f/ledger", otherCard["id"])
	deposit["amount_usd"] = "500.00"
	call("POST", otherPath, 2, deposit, 201)
	call("POST", otherPath, 3, charge, 409)
	charge["account_id"] = 2
	charge["request_key"] = "admin-charge-00001"
	call("POST", otherPath, 3, charge, 201)
	statement := call("GET", path, 1, nil, 200)
	if statement["total"] != float64(2) || statement["balance_usd_minor"] != float64(34975) || statement["spent_usd_minor"] != float64(15025) || statement["deposited_usd_minor"] != float64(50000) {
		t.Fatal("对账汇总错误")
	}
	call("GET", path, 3, nil, 200)
	call("GET", path+"?page=0", 1, nil, 400)
	call("DELETE", path, 1, nil, 405)
	cardInput["number"] = "5555555555554444"
	call("PATCH", cardPath, 1, cardInput, 409)
	cardInput["number"] = "4242424242424242"
	cardInput["label"] = "New label"
	updated := call("PATCH", cardPath, 1, cardInput, 200)["card"].(map[string]any)
	if updated["balance_usd_minor"] != float64(34975) {
		t.Fatal("编辑银行卡影响余额")
	}
	call("DELETE", "/api/accounts/1", 1, nil, 204)
	statement = call("GET", path, 1, nil, 200)
	if statement["entries"].([]any)[0].(map[string]any)["account_email"] != "one@test.local" {
		t.Fatal("删除账号丢失流水快照")
	}
	for index := 0; index < 20; index++ {
		call("POST", path, 1, map[string]any{"kind": "deposit", "amount_usd": "0.01", "request_key": fmt.Sprintf("pagination-deposit-%04d", index)}, 201)
	}
	statement = call("GET", path+"?page=2", 1, nil, 200)
	if statement["total"] != float64(22) || len(statement["entries"].([]any)) != 2 || statement["balance_usd_minor"] != float64(34995) {
		t.Fatal("分页或连续记账余额错误")
	}
	call("DELETE", cardPath, 1, nil, 204)
	call("GET", cardPath, 1, nil, 404)
	call("GET", path, 1, nil, 200)
	call("POST", path, 1, deposit, 404)
	var sum, balance int64
	if err := db.QueryRow(`SELECT sum(amount_usd_minor),max(c.balance_usd_minor) FROM bank_card_ledger l JOIN bank_cards c ON c.id=l.card_id WHERE c.id=$1`, cardID).Scan(&sum, &balance); err != nil || sum != balance {
		t.Fatal("余额与流水合计不一致", err)
	}
}
