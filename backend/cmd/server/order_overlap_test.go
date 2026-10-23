package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRecordedOrdersAllowOverlappingPeriods(t *testing.T) {
	db := walletTestDB(t)
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'overlap@test.local','','admin');
INSERT INTO chatgpt_accounts(id,user_id,label,email,payment_card_id) VALUES(1,1,'Account','account@test.local',1);
INSERT INTO recharge_packages(id,name,plan,region,currency,original_amount_minor,sale_usd_minor,months,enabled) VALUES(1,'Monthly','plus','US','USD',1000,1000,1,true),(2,'Two months','plus','US','USD',1000,1000,2,true);
INSERT INTO bank_cards(id,user_id,label,cardholder,number_ciphertext,number_fingerprint,last4,brand,exp_month,exp_year,balance_usd_minor) VALUES(1,1,'Card','Tester','encrypted','fingerprint','4242','Visa',12,2099,10000);`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("test")}
	call := func(method, path string, input any, status int) map[string]any {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+s.token(1))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d want %d %s", method, path, w.Code, status, w.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	start, end := chargedOrderPeriod(time.Now(), 1)
	pending := call("POST", "/api/orders", map[string]any{"account_id": 1, "package_id": 1, "period_start": start, "request_key": "pending-overlap-order"}, 201)
	var firstID any
	for i, packageID := range []int{1, 1, 2} {
		input := map[string]any{"account_id": 1, "package_id": packageID, "reference": fmt.Sprintf("overlap-payment-%d", i), "evidence": "独立交易凭据", "received_currency": "USD", "received_amount": "15.00", "request_key": fmt.Sprintf("overlap-record-%d", i), "period_start": "2099-01-01"}
		created := call("POST", "/api/orders/record", input, 201)
		if i == 0 {
			firstID = created["id"]
		}
		// 同周期和部分重叠周期分别扣款，但重放同一请求只返回原订单。
		replay := call("POST", "/api/orders/record", input, 200)["order"].(map[string]any)
		if replay["id"] != created["id"] || replay["period_start"] != start || replay["cost_usd_minor"] != float64(1000) || replay["fulfillment_status"] != "completed" {
			t.Fatal("独立订单未完成扣款或重放创建了新订单", replay)
		}
		if packageID == 1 && replay["period_end"] != end {
			t.Fatal("重叠订单不应被改为其他周期", replay)
		}
		input["request_key"] = fmt.Sprintf("duplicate-payment-%d", i)
		failed := call("POST", "/api/orders/record", input, 409)
		if !strings.Contains(failed["error"].(string), "同一笔交易不能重复录入") {
			t.Fatal("同一账号重复交易号应拒绝", failed)
		}
	}
	// 新请求也不能对已扣款订单再次补录。
	call("POST", fmt.Sprintf("/api/orders/%.0f", firstID), map[string]any{"action": "record", "version": 0, "reference": "same-order-second-charge", "evidence": "重复扣款", "request_key": "same-order-second-charge"}, 409)
	// 允许订单重叠不影响无订单的直接记账防重。
	failed := call("POST", "/api/bank-cards/1/ledger", map[string]any{"kind": "subscription", "account_id": 1, "package_id": 1, "charge_mode": "package", "expected_amount_usd_minor": 1000, "period_start": start, "period_end": end, "reference": "direct-overlap-charge", "request_key": "direct-overlap-charge"}, 409)
	if !strings.Contains(failed["error"].(string), "该周期已有扣款流水") {
		t.Fatal("无订单的直接扣款必须保留周期防重", failed)
	}
	preserved := call("GET", fmt.Sprintf("/api/orders/%.0f", pending["id"]), nil, 200)["order"].(map[string]any)
	if preserved["order_status"] != "active" || preserved["cost_usd_minor"] != float64(0) || preserved["payment_status"] != "unpaid" {
		t.Fatal("新交易不能修改已有订单", preserved)
	}
	var orders, ledger, audits, events, wallets int
	var balance int64
	err = db.QueryRow(`SELECT (SELECT count(*) FROM recharge_orders),(SELECT count(*) FROM bank_card_ledger),(SELECT balance_usd_minor FROM bank_cards WHERE id=1),(SELECT count(*) FROM renewal_date_audit),(SELECT count(*) FROM operation_events WHERE entity_type='order' AND action='record'),(SELECT count(*) FROM wallet_ledger)`).Scan(&orders, &ledger, &balance, &audits, &events, &wallets)
	if err != nil || orders != 4 || ledger != 3 || balance != 7000 || audits != 3 || events != 3 || wallets != 0 {
		t.Fatal("重叠交易必须各扣一次，失败或重放不产生订单、扣款、续期或审计", orders, ledger, balance, audits, events, wallets, err)
	}
	var renewalDate string
	firstEnd, _ := time.Parse("2006-01-02", end)
	wantRenewal := addMonthsClamped(addMonthsClamped(firstEnd, 1), 2).Format("2006-01-02")
	if err = db.QueryRow(`SELECT renewal_date::text FROM chatgpt_accounts WHERE id=1`).Scan(&renewalDate); err != nil || renewalDate != wantRenewal {
		t.Fatal("各笔成功订单应依次顺延续订日期，重放不得重复顺延", renewalDate, wantRenewal, err)
	}
}
