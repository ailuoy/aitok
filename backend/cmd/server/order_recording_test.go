package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestUnifiedOrderRecording(t *testing.T) {
	db := walletTestDB(t)
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'record@test.local','','admin'),(2,'user@test.local','','user');
INSERT INTO chatgpt_accounts(id,user_id,label,email,payment_card_id) VALUES(1,1,'Account','account@test.local',1),(2,1,'Second','second@test.local',1),(3,1,'Third','third@test.local',1);
INSERT INTO recharge_packages(id,name,plan,region,currency,original_amount_minor,sale_usd_minor,months,enabled) VALUES(1,'Plus','plus','PH','PHP',100000,20000,1,true);
INSERT INTO bank_cards(id,user_id,label,cardholder,number_ciphertext,number_fingerprint,last4,brand,exp_month,exp_year,balance_usd_minor) VALUES(1,1,'Card','Tester','encrypted','fingerprint','4242','Visa',12,2035,10000);
INSERT INTO exchange_rates(base_currency,quote_currency,rate,source,effective_at,created_at,updated_at) VALUES('PHP','USD',0.02,'test',NOW(),NOW(),NOW()),('PHP','CNY',0.14,'test',NOW(),NOW(),NOW());`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("test")}
	call := func(method, path string, user int64, input any, status int) map[string]any {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+s.token(user))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d want %d %s", method, path, w.Code, status, w.Body.String())
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	input := map[string]any{"account_id": 1, "package_id": 1, "period_start": "2030-01-01", "card_id": 1, "reference": "unified-payment-1", "evidence": testRichEvidence(t), "expected_sale_usd_minor": 20000, "request_key": "unified-create-0001", "order_source": "  微信   老客户  "}
	call("POST", "/api/orders/record", 2, input, 403)
	if failed := call("POST", "/api/orders/record", 1, input, 409); failed["error"] != "请填写实收金额" {
		t.Fatal("空金额应拒绝录入", failed)
	}
	input["received_currency"] = "USD"
	for _, amount := range []string{"", "0", "-1", "abc", "1.001"} {
		input["received_amount"] = amount
		call("POST", "/api/orders/record", 1, input, 400)
	}
	input["received_amount"] = "250.00"
	input["received_currency"] = "PHP"
	call("POST", "/api/orders/record", 1, input, 400)
	input["received_currency"] = "USD"
	// 绑定缺失、卡片不可用或客户端指定其他卡时，录入必须整笔回滚。
	for _, cardState := range []struct{ query, message string }{
		{`UPDATE chatgpt_accounts SET payment_card_id=NULL,updated_at=NOW() WHERE id=1`, "未绑定付款卡"},
		{`UPDATE chatgpt_accounts SET payment_card_id=1,updated_at=NOW() WHERE id=1; UPDATE bank_cards SET status='frozen',updated_at=NOW() WHERE id=1`, "已停用"},
		{`UPDATE bank_cards SET status='active',exp_year=2020,updated_at=NOW() WHERE id=1`, "过期"},
		{`UPDATE bank_cards SET exp_year=2035,deleted_at=NOW(),updated_at=NOW() WHERE id=1`, "删除"},
	} {
		if _, err = db.Exec(cardState.query); err != nil {
			t.Fatal(err)
		}
		if failed := call("POST", "/api/orders/record", 1, input, 409); !strings.Contains(failed["error"].(string), cardState.message) {
			t.Fatal("应提示绑定卡问题", failed)
		}
	}
	if _, err = db.Exec(`UPDATE bank_cards SET deleted_at=NULL,updated_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	input["card_id"] = 999
	if failed := call("POST", "/api/orders/record", 1, input, 409); !strings.Contains(failed["error"].(string), "已变更") {
		t.Fatal("不能指定非绑定卡", failed)
	}
	input["card_id"] = 1
	if failed := call("POST", "/api/orders/record", 1, input, 409); !strings.Contains(failed["error"].(string), "余额不足") {
		t.Fatal("应提示余额不足", failed)
	}
	if sources := call("GET", "/api/orders", 1, nil, 200)["sources"].([]any); len(sources) != 0 {
		t.Fatal("扣款失败不能保存新来源", sources)
	}
	input["order_source"] = strings.Repeat("源", 81)
	call("POST", "/api/orders/record", 1, input, 400)
	input["order_source"] = "  微信   老客户  "
	var orders, ledger int
	var balance int64
	err = db.QueryRow(`SELECT (SELECT count(*) FROM recharge_orders),(SELECT count(*) FROM bank_card_ledger),(SELECT balance_usd_minor FROM bank_cards WHERE id=1)`).Scan(&orders, &ledger, &balance)
	if err != nil || orders != 0 || ledger != 0 || balance != 10000 {
		t.Fatal("余额不足未整体回滚", orders, ledger, balance, err)
	}
	var activated, audits int
	if err = db.QueryRow(`SELECT (SELECT count(*) FROM chatgpt_accounts WHERE verified_at IS NOT NULL OR subscription_ends_at IS NOT NULL),(SELECT count(*) FROM renewal_date_audit)`).Scan(&activated, &audits); err != nil || activated != 0 || audits != 0 {
		t.Fatal("扣款失败不能开通账号或写入续费审计", activated, audits, err)
	}
	_, err = db.Exec(`UPDATE bank_cards SET balance_usd_minor=100000,updated_at=NOW() WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	first := call("POST", "/api/orders/record", 1, input, 201)
	op := fmt.Sprintf("/api/orders/%.0f", first["id"])
	// 已完成请求重放不受后续解绑影响，仍返回原结果且不再次扣款。
	if _, err = db.Exec(`UPDATE chatgpt_accounts SET payment_card_id=NULL,updated_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/orders/record", 1, input, 200)
	if _, err = db.Exec(`UPDATE chatgpt_accounts SET payment_card_id=1,updated_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	// 不同账号使用同一真实交易号必须拒绝，整笔订单和扣款回滚。
	input["account_id"], input["request_key"] = 2, "duplicate-transaction-record"
	if failed := call("POST", "/api/orders/record", 1, input, 409); !strings.Contains(failed["error"].(string), "同一笔交易不能重复录入") {
		t.Fatal("应提示交易号重复", failed)
	}
	if err = db.QueryRow(`SELECT (SELECT count(*) FROM recharge_orders),(SELECT count(*) FROM bank_card_ledger),(SELECT balance_usd_minor FROM bank_cards WHERE id=1)`).Scan(&orders, &ledger, &balance); err != nil || orders != 1 || ledger != 1 || balance != 80000 {
		t.Fatal("交易号重复未整体回滚", orders, ledger, balance, err)
	}
	if err = db.QueryRow(`SELECT (SELECT count(*) FROM chatgpt_accounts WHERE verified_at IS NOT NULL),(SELECT count(*) FROM renewal_date_audit)`).Scan(&activated, &audits); err != nil || activated != 1 || audits != 1 {
		t.Fatal("交易号重复不能开通其他账号", activated, audits, err)
	}
	input["account_id"], input["request_key"] = 1, "unified-create-0001"
	input["reference"] = "changed-reference"
	call("POST", "/api/orders/record", 1, input, 409)
	input["reference"] = "unified-payment-1"
	input["order_source"] = "修改来源"
	call("POST", "/api/orders/record", 1, input, 409)
	input["order_source"] = "微信 老客户"
	call("POST", "/api/orders/record", 1, input, 200)
	order := call("GET", op, 1, nil, 200)["order"].(map[string]any)
	if order["order_source"] != "微信 老客户" {
		t.Fatal("订单来源未规范化保存", order["order_source"])
	}
	if order["cost_usd_minor"] != float64(20000) || order["fulfillment_status"] != "completed" || order["received_amount_minor"] != float64(25000) || order["profit"].(map[string]any)["usd_minor"] != float64(5000) {
		t.Fatal("录入应保存实收并计算毛利", order)
	}
	if _, err = db.Exec(`UPDATE recharge_orders SET updated_at=NOW()-INTERVAL '2 days' WHERE id=$1`, first["id"]); err != nil {
		t.Fatal(err)
	}
	notices := call("GET", "/api/notices", 1, nil, 200)["notices"].([]any)
	for _, notice := range notices {
		if notice.(map[string]any)["kind"] == "order" {
			t.Fatal("扣款完成不应再提醒开通核验", notices)
		}
	}
	call("POST", op, 1, map[string]any{"action": "verify", "success": true, "plan": "plus", "period_end": "2030-02-01", "evidence": "旧核验请求", "request_key": "verify-without-receipt", "version": 0}, 409)
	input["request_key"] = "overlapping-new-record"
	input["reference"] = "overlapping-new-payment"
	input["period_start"] = "2099-01-01"
	call("POST", "/api/orders/record", 1, input, 409)
	quote := call("GET", "/api/orders/collection-quote?package_id=1&currency=CNY&amount=1680.00", 1, nil, 200)
	delete(input, "period_start")
	input["account_id"] = 2
	input["request_key"] = "unified-create-0002"
	input["reference"] = "unified-payment-2"
	input["received_currency"] = "CNY"
	input["received_amount"] = "1680.00"
	input["collection_rate_id"] = 0
	call("POST", "/api/orders/record", 1, input, 409)
	input["collection_rate_id"] = quote["exchange_rate"].(map[string]any)["batch"].(map[string]any)["id"]
	delete(input, "card_id") // 不传卡片也必须由后端使用账号绑定卡。
	second := call("POST", "/api/orders/record", 1, input, 201)
	secondPath := fmt.Sprintf("/api/orders/%.0f", second["id"])
	order = call("GET", secondPath, 1, nil, 200)["order"].(map[string]any)
	if order["card_id"] != float64(1) || order["received_usd_minor"] != float64(24000) || order["profit"].(map[string]any)["usd_minor"] != float64(4000) || order["profit"].(map[string]any)["estimated"] != false {
		t.Fatal("合并录入实收及毛利错误", order)
	}
	// 旧已收款未扣款订单只补录卡片支出，保留旧实收。
	legacy := map[string]any{"account_id": 3, "package_id": 1, "period_start": "2030-03-01", "request_key": "legacy-order-create"}
	third := call("POST", "/api/orders", 1, legacy, 201)
	thirdPath := fmt.Sprintf("/api/orders/%.0f", third["id"])
	if failed := call("POST", thirdPath, 1, map[string]any{"action": "record", "card_id": 1, "reference": "legacy-missing-receipt", "evidence": "proof", "request_key": "legacy-missing-receipt", "version": 0}, 409); failed["error"] != "请填写实收金额" {
		t.Fatal("未收款订单补录必须填写实收", failed)
	}
	call("POST", thirdPath, 1, map[string]any{"action": "collect", "method": "manual", "received_currency": "USD", "received_amount": "250.00", "reference": "legacy-receipt", "evidence": "old receipt", "request_key": "legacy-collect-1", "version": 0}, 200)
	record := map[string]any{"action": "record", "card_id": 1, "reference": "legacy-purchase", "evidence": "new proof", "request_key": "legacy-record-0001", "version": 1, "received_currency": "USD", "received_amount": "999.00", "order_source": "合作渠道"}
	call("POST", thirdPath, 1, record, 409)
	delete(record, "received_currency")
	delete(record, "received_amount")
	record["card_id"] = 999
	if failed := call("POST", thirdPath, 1, record, 409); !strings.Contains(failed["error"].(string), "已变更") {
		t.Fatal("补录不能指定非绑定卡", failed)
	}
	delete(record, "card_id")
	// 旧订单补录改为今日生效时，也必须重新检查订单周期，不能覆盖未扣款的有效订单。
	blocker := call("POST", "/api/orders", 1, map[string]any{"account_id": 3, "package_id": 1, "period_start": order["period_start"], "request_key": "legacy-current-blocker"}, 201)
	call("POST", thirdPath, 1, record, 409)
	call("POST", fmt.Sprintf("/api/orders/%.0f", blocker["id"]), 1, map[string]any{"action": "discard", "reason": "重复订单", "request_key": "discard-current-blocker", "version": 0}, 200)
	call("POST", thirdPath, 1, record, 200)
	call("POST", thirdPath, 1, record, 200)
	order = call("GET", thirdPath, 1, nil, 200)["order"].(map[string]any)
	if order["received_usd_minor"] != float64(25000) || order["payment_reference"] != "legacy-receipt" {
		t.Fatal("补录不应覆盖原收款")
	}
	err = db.QueryRow(`SELECT (SELECT count(*) FROM recharge_orders),(SELECT count(*) FROM bank_card_ledger),(SELECT balance_usd_minor FROM bank_cards WHERE id=1)`).Scan(&orders, &ledger, &balance)
	if err != nil || orders != 4 || ledger != 3 || balance != 40000 {
		t.Fatal("订单和扣款重复或缺失", orders, ledger, balance, err)
	}
	var consistent int
	err = db.QueryRow(`SELECT count(*) FROM recharge_orders o JOIN bank_card_ledger l ON l.order_id=o.id JOIN chatgpt_accounts a ON a.id=o.account_id WHERE o.fulfillment_status='completed' AND o.period_start=(l.created_at AT TIME ZONE 'Asia/Shanghai')::date AND o.period_end=(o.period_start+INTERVAL '1 month')::date AND l.period_start=o.period_start AND l.period_end=o.period_end AND o.verified_at=l.created_at AND a.verified_at=o.verified_at AND a.verified_plan='plus' AND a.subscription_package_id=o.package_id AND a.subscription_ends_at=o.period_end AND a.renewal_date=o.period_end`).Scan(&consistent)
	if err != nil || consistent != 3 {
		t.Fatal("订单、流水和账号须按实际扣款时间同步开通", consistent, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM renewal_date_audit`).Scan(&audits); err != nil || audits != 3 {
		t.Fatal("重复请求不能重复开通或写入续费审计", audits, err)
	}
	var wallets int
	if err = db.QueryRow(`SELECT count(*) FROM wallet_ledger`).Scan(&wallets); err != nil || wallets != 0 {
		t.Fatal("合并录入不能扣钱包代币")
	}
	// 来源去重且不受分页/搜索影响；来源本身也可以搜索和导出。
	listed := call("GET", "/api/orders?q=nomatch&page_size=1", 1, nil, 200)
	if listed["total"] != float64(0) || len(listed["sources"].([]any)) != 2 {
		t.Fatal("来源选项不能被当前分页或筛选截断", listed["sources"])
	}
	listed = call("GET", "/api/orders?q="+url.QueryEscape("微信 老客户"), 1, nil, 200)
	if listed["total"] != float64(2) {
		t.Fatal("无法按来源搜索订单", listed["total"])
	}
	request := httptest.NewRequest("GET", "/api/orders/export", nil)
	request.Header.Set("Authorization", "Bearer "+s.token(1))
	response := httptest.NewRecorder()
	s.routes().ServeHTTP(response, request)
	if response.Code != 200 || !strings.Contains(response.Body.String(), "订单来源,订单号") || !strings.Contains(response.Body.String(), "合作渠道,") {
		t.Fatal("导出未包含订单来源")
	}
	if _, err = db.Exec(`UPDATE recharge_orders SET deleted_at=NOW(),updated_at=NOW() WHERE id=$1`, third["id"]); err != nil {
		t.Fatal(err)
	}
	sources := call("GET", "/api/orders", 1, nil, 200)["sources"].([]any)
	if len(sources) != 1 || sources[0] != "微信 老客户" {
		t.Fatal("来源必须排除软删除订单并去重", sources)
	}
}

func TestChargedOrderPeriodUTC8(t *testing.T) {
	for _, c := range []struct {
		at, start, end string
		months         int
	}{
		{"2030-01-30T15:59:59Z", "2030-01-30", "2030-02-28", 1},
		{"2030-01-30T16:00:00Z", "2030-01-31", "2030-02-28", 1},
		{"2028-01-30T16:00:00Z", "2028-01-31", "2028-02-29", 1},
		{"2030-12-31T16:00:00Z", "2031-01-01", "2032-01-01", 12},
	} {
		at, err := time.Parse(time.RFC3339, c.at)
		if err != nil {
			t.Fatal(err)
		}
		start, end := chargedOrderPeriod(at, c.months)
		if start != c.start || end != c.end {
			t.Fatalf("%s: got %s to %s, want %s to %s", c.at, start, end, c.start, c.end)
		}
	}
}
