package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestQuickMonthOrder(t *testing.T) {
	db := walletTestDB(t)
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'monthly@test.local','','admin'),(2,'member@test.local','','user');
INSERT INTO chatgpt_accounts(id,user_id,label,email,payment_card_id,subscription_package_id,renewal_date) VALUES(1,1,'Month end','month@test.local',1,1,'2030-01-31'),(2,1,'No date','undated@test.local',1,1,NULL);
INSERT INTO recharge_packages(id,name,plan,region,currency,original_amount_minor,sale_usd_minor,months,enabled) VALUES(1,'Monthly Plus','plus','PH','USD',2000,2000,1,true);
INSERT INTO bank_cards(id,user_id,label,cardholder,number_ciphertext,number_fingerprint,last4,brand,exp_month,exp_year,balance_usd_minor) VALUES(1,1,'Card','Tester','encrypted','fingerprint','4242','Visa',12,2035,0);
INSERT INTO exchange_rates(base_currency,quote_currency,rate,source,effective_at,created_at,updated_at) VALUES('PHP','USD',0.02,'test',NOW(),NOW(),NOW()),('PHP','CNY',0.14,'test',NOW(),NOW(),NOW());`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("quick-month-test")}
	call := func(method, path string, body any, user int64, status int) map[string]any {
		t.Helper()
		encoded, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
		r.Header.Set("Authorization", "Bearer "+s.token(user))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		var result map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		return result
	}
	quote := call("GET", "/api/orders/collection-quote?package_id=1&currency=CNY&amount=200.00", nil, 1, 200)
	input := map[string]any{"quick_month": true, "account_id": 1, "package_id": 1, "expected_renewal_date": "2030-01-31", "card_id": 1, "expected_sale_usd_minor": 2000, "received_currency": "CNY", "received_amount": "200.00", "collection_rate_id": quote["exchange_rate"].(map[string]any)["batch"].(map[string]any)["id"], "order_source": "月订单测试", "reference": "quick-month-transaction-1", "evidence": "测试交易凭据", "request_key": "quick-month-order-1"}
	call("POST", "/api/orders/record", input, 2, 403)
	call("POST", "/api/orders", input, 1, 400)
	delete(input, "expected_renewal_date")
	call("POST", "/api/orders/record", input, 1, 400)
	input["expected_renewal_date"] = "2030-01-30"
	call("POST", "/api/orders/record", input, 1, 409)
	input["expected_renewal_date"] = "2030-01-31"
	input["received_currency"] = "USD"
	call("POST", "/api/orders/record", input, 1, 400)
	input["received_currency"] = "CNY"
	input["package_id"] = 999
	call("POST", "/api/orders/record", input, 1, 409)
	input["package_id"] = 1
	if _, err = db.Exec(`UPDATE recharge_packages SET months=3,updated_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/orders/record", input, 1, 400)
	if _, err = db.Exec(`UPDATE recharge_packages SET months=1,updated_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/orders/record", input, 1, 409) // 余额不足，日期和订单也不得更新。
	var date string
	var orders, entries, audits int
	if err = db.QueryRow(`SELECT renewal_date::text,(SELECT count(*) FROM recharge_orders),(SELECT count(*) FROM bank_card_ledger),(SELECT count(*) FROM renewal_date_audit) FROM chatgpt_accounts WHERE id=1`).Scan(&date, &orders, &entries, &audits); err != nil || date != "2030-01-31" || orders != 0 || entries != 0 || audits != 0 {
		t.Fatal("失败必须整笔回滚", date, orders, entries, audits, err)
	}
	if _, err = db.Exec(`UPDATE bank_cards SET balance_usd_minor=10000,updated_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/orders/record", input, 1, 201)
	call("POST", "/api/orders/record", input, 1, 200)
	var start, end, accountEnd string
	var balance int64
	if err = db.QueryRow(`SELECT a.renewal_date::text,a.subscription_ends_at::text,o.period_start::text,o.period_end::text,c.balance_usd_minor FROM chatgpt_accounts a JOIN recharge_orders o ON o.account_id=a.id JOIN bank_cards c ON c.id=o.card_id WHERE a.id=1`).Scan(&date, &accountEnd, &start, &end, &balance); err != nil || date != "2030-02-28" || accountEnd != date || start != "2030-01-31" || end != date || balance != 8000 {
		t.Fatal("月订单须从原续订日期加一个月，并同步订单、账号和绑定卡", date, accountEnd, start, end, balance, err)
	}
	// 另一个旧弹框使用新请求标识，也不能再延长一个月。
	input["request_key"] = "quick-month-stale-form"
	input["reference"] = "quick-month-stale-reference"
	call("POST", "/api/orders/record", input, 1, 409)
	if err = db.QueryRow(`SELECT (SELECT count(*) FROM recharge_orders),(SELECT count(*) FROM bank_card_ledger),(SELECT count(*) FROM renewal_date_audit)`).Scan(&orders, &entries, &audits); err != nil || orders != 1 || entries != 1 || audits != 1 {
		t.Fatal("重放或旧表单不能重复记账或续期", orders, entries, audits, err)
	}
	input["account_id"], input["expected_renewal_date"] = 2, ""
	input["request_key"], input["reference"] = "quick-month-undated", "quick-month-undated-reference"
	call("POST", "/api/orders/record", input, 1, 201)
	var postedAt time.Time
	if err = db.QueryRow(`SELECT a.renewal_date::text,o.period_start::text,l.created_at FROM chatgpt_accounts a JOIN recharge_orders o ON o.account_id=a.id JOIN bank_card_ledger l ON l.order_id=o.id WHERE a.id=2`).Scan(&date, &start, &postedAt); err != nil {
		t.Fatal(err)
	}
	wantStart, wantEnd := chargedOrderPeriod(postedAt, 1)
	if start != wantStart || date != wantEnd {
		t.Fatal("无日期账号应从扣款当天续一个月", start, date)
	}
}

func TestQuickMonthPeriod(t *testing.T) {
	for _, pair := range [][2]string{{"2028-01-31", "2028-02-29"}, {"2030-01-31", "2030-02-28"}, {"2030-12-31", "2031-01-31"}, {"2020-09-18", "2020-10-18"}} {
		previous, _ := time.Parse("2006-01-02", pair[0])
		start, end := recordedOrderPeriod(time.Now(), 1, &previous, true)
		if start != pair[0] || end != pair[1] {
			t.Fatal("必须按现有续订日期加一个月并截断月末", pair, start, end)
		}
	}
}
