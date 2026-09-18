package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'owner@test.local','','admin'),(2,'other@test.local','','user'),(3,'admin@test.local','','admin');INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,1,'Account One','one@test.local'),(2,2,'Other Account','two@test.local'),(3,1,'Account Three','three@test.local')`); err != nil {
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
			if method == "GET" && auditCardDetails.MatchString(path) {
				r.Header.Set("X-Aitok-TOTP", prepareCardEditTOTP(t, s, user))
			}
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
	call("GET", path, 2, nil, 403)
	call("POST", path, 2, deposit, 403)
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
	charge := map[string]any{"kind": "subscription", "amount_usd": "150.25", "account_id": 999, "request_key": "account-charge-0001", "notes": "卡平台实际扣款", "period_start": "2030-01-01", "period_end": "2030-02-01", "currency": "PHP", "original_amount_minor": 891964, "reference": "external-ledger-test-1"}
	call("POST", path, 1, charge, 404)
	charge["account_id"] = 1
	charge["amount_usd"] = "150.25"
	entry := call("POST", path, 1, charge, 201)["entry"].(map[string]any)
	if entry["amount_usd_minor"] != float64(-15025) || entry["balance_after_usd_minor"] != float64(34975) || entry["original_amount_minor"] != float64(891964) || entry["account_email"] != "one@test.local" {
		t.Fatal("扣款、余额或原币快照错误")
	}
	call("POST", path, 1, charge, 200)
	charge["request_key"] = "account-charge-0002"
	call("POST", path, 1, charge, 409)
	db.Exec("UPDATE users SET role='admin' WHERE id=2")
	otherCard := call("POST", "/api/bank-cards", 2, cardInput, 201)["card"].(map[string]any)
	otherPath := fmt.Sprintf("/api/bank-cards/%.0f/ledger", otherCard["id"])
	deposit["amount_usd"] = "500.00"
	call("POST", otherPath, 2, deposit, 201)
	call("POST", otherPath, 3, charge, 409)
	charge["account_id"] = 2
	charge["reference"] = "external-ledger-test-2"
	charge["request_key"] = "admin-charge-00001"
	call("POST", otherPath, 3, charge, 201)
	statement := call("GET", path, 1, nil, 200)
	if statement["total"] != float64(2) || statement["balance_usd_minor"] != float64(34975) || statement["spent_usd_minor"] != float64(15025) || statement["deposited_usd_minor"] != float64(50000) {
		t.Fatal("对账汇总错误")
	}
	call("GET", path, 3, nil, 200)
	call("GET", path+"?page=0", 1, nil, 400)
	call("DELETE", path, 1, nil, 405)
	cardInput["edit_token"] = cardEditTestToken(t, s, 1, cardID, time.Now().Add(time.Minute))
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

func TestHistoricalCardCharges(t *testing.T) {
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	db := walletTestDB(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'history@test.local','','admin'),(2,'member@test.local','','user');
INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,1,'One','history-one@test.local'),(2,1,'Two','history-two@test.local');
INSERT INTO recharge_packages(id,name,plan,region,currency,original_amount_minor,sale_usd_minor,months,enabled) VALUES(1,'历史套餐','pro_5x','PH','PHP',891964,14219,1,true),(2,'人民币测试','plus','US','USD',20000,20000,1,true);
INSERT INTO exchange_rates(base_currency,quote_currency,rate,source,effective_at,created_at) VALUES('PHP','USD',0.016,'test',NOW(),NOW()),('PHP','CNY',0.112,'test',NOW(),NOW());`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("history-test")}
	call := func(method, path string, user int64, body any, status int) map[string]any {
		t.Helper()
		encoded, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
		r.Header.Set("Authorization", "Bearer "+s.token(user))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	card := call("POST", "/api/bank-cards", 1, map[string]any{"label": "History", "cardholder": "Test User", "number": "4242424242424242", "exp_month": 12, "exp_year": 2035}, 201)["card"].(map[string]any)
	cardID := int64(card["id"].(float64))
	path := fmt.Sprintf("/api/bank-cards/%d/ledger", cardID)
	quotePath := path + "?quote=1&package_id=1&charge_mode=package"
	call("GET", quotePath, 2, nil, 403)
	quote := call("GET", quotePath, 1, nil, 200)
	if quote["amount_usd_minor"] != float64(14219) {
		t.Fatal("套餐自动定价错误", quote)
	}
	input := map[string]any{"kind": "subscription", "account_id": 1, "package_id": 1, "charge_mode": "package", "expected_amount_usd_minor": 14219, "period_start": "2030-01-01", "period_end": "2030-02-01", "reference": "history-package-1", "request_key": "history-package-0001"}
	entry := call("POST", path, 1, input, 201)["entry"].(map[string]any)
	if entry["balance_after_usd_minor"] != float64(-14219) || entry["notes"] != "" || entry["original_amount_minor"] != float64(891964) {
		t.Fatal("零余额补录或选填备注失败", entry)
	}
	pricing := entry["pricing_snapshot"].(map[string]any)
	if pricing["package"].(map[string]any)["name"] != "历史套餐" || pricing["confirmed_at"] == nil {
		t.Fatal("缺少套餐快照", pricing)
	}
	if _, err := db.Exec(`UPDATE recharge_packages SET sale_usd_minor=99999,enabled=false,deleted_at=NOW(),updated_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if call("POST", path, 1, input, 200)["replayed"] != true {
		t.Fatal("套餐删除后应重放原结果")
	}
	input["expected_amount_usd_minor"] = 99999
	call("POST", path, 1, input, 409)
	input["expected_amount_usd_minor"] = 14219
	input["account_id"] = 2
	call("POST", path, 1, input, 409)
	input["account_id"] = 1
	input["amount_usd"] = "0.01"
	call("POST", path, 1, input, 400)
	delete(input, "amount_usd")
	input["currency"] = "USD"
	call("POST", path, 1, input, 400)
	delete(input, "currency")
	quote = call("GET", path+"?quote=1&package_id=2&charge_mode=CNY&charge_amount=700.04", 1, nil, 200)
	if quote["amount_usd_minor"] != float64(10001) {
		t.Fatal("人民币折算应只在最终美分舍入", quote)
	}
	rateID := quote["exchange_rate"].(map[string]any)["batch"].(map[string]any)["id"]
	cny := map[string]any{"kind": "subscription", "account_id": 2, "package_id": 2, "charge_mode": "CNY", "charge_amount": "700.04", "expected_amount_usd_minor": 10001, "rate_id": rateID, "period_start": "2030-01-01", "period_end": "2030-02-01", "reference": "history-cny-1", "request_key": "history-cny-000001"}
	cny["expected_amount_usd_minor"] = 10000
	call("POST", path, 1, cny, 409)
	cny["expected_amount_usd_minor"] = 10001
	cny["rate_id"] = 999
	call("POST", path, 1, cny, 409)
	cny["rate_id"] = rateID
	entry = call("POST", path, 1, cny, 201)["entry"].(map[string]any)
	if entry["balance_after_usd_minor"] != float64(-24220) {
		t.Fatal("负余额继续补录失败", entry)
	}
	pricing = entry["pricing_snapshot"].(map[string]any)
	if pricing["charge_currency"] != "CNY" || pricing["charge_amount_minor"] != float64(70004) || pricing["exchange_rate"].(map[string]any)["usd_per_unit"] != "1/7" {
		t.Fatal("人民币扣款快照错误", pricing)
	}
	cny["charge_amount"] = "700.03"
	call("POST", path, 1, cny, 409)
	cny["charge_amount"] = "700.04"
	cny["request_key"] = "history-cny-000002"
	cny["reference"] = "history-cny-2"
	call("POST", path, 1, cny, 409)
	cny["request_key"] = "history-cny-000001"
	cny["reference"] = "history-cny-1"
	if _, err := db.Exec(`UPDATE exchange_rates SET created_at=NOW()-INTERVAL '3 days',updated_at=NOW(); UPDATE recharge_packages SET enabled=false,updated_at=NOW() WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	call("POST", path, 1, cny, 200)
	if _, err := db.Exec(`UPDATE recharge_packages SET enabled=true,updated_at=NOW() WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	cny["request_key"] = "history-cny-stale-1"
	cny["reference"] = "history-cny-stale"
	cny["period_start"] = "2030-02-01"
	cny["period_end"] = "2030-03-01"
	call("POST", path, 1, cny, 409)
	// 普通支出仍受余额校验，不能借历史补录选项绕过订单或手续费限制。
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	err = postCardEntry(httptest.NewRequest(http.MethodPost, path, nil), tx, 1, cardID, cardPosting{Kind: "fee", Amount: -1, Key: "fee-no-overdraft-1", AllowHistoricalOverdraft: true}, true)
	tx.Rollback()
	if err == nil {
		t.Fatal("普通支出不应允许负余额")
	}
	deposit := map[string]any{"kind": "deposit", "amount_usd": "100.00", "request_key": "history-deposit-0001"}
	entry = call("POST", path, 1, deposit, 201)["entry"].(map[string]any)
	if entry["balance_after_usd_minor"] != float64(-14220) {
		t.Fatal("存入应允许逐步补足负余额")
	}
	statement := call("GET", path, 1, nil, 200)
	if statement["total"] != float64(3) || statement["balance_usd_minor"] != float64(-14220) {
		t.Fatal("幂等请求重复入账", statement)
	}
	var balance, sum int64
	if err := db.QueryRow(`SELECT balance_usd_minor,(SELECT sum(amount_usd_minor) FROM bank_card_ledger WHERE card_id=$1) FROM bank_cards WHERE id=$1`, cardID).Scan(&balance, &sum); err != nil || balance != sum {
		t.Fatal("余额与流水不一致", err)
	}
}
