package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCardRenewalMonthUTC8(t *testing.T) {
	for _, tc := range []struct{ now, start, end string }{
		{"2026-12-31T15:59:59Z", "2026-12-01", "2027-01-01"},
		{"2026-12-31T16:00:00Z", "2027-01-01", "2027-02-01"},
		{"2028-02-29T15:59:59Z", "2028-02-01", "2028-03-01"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		start, end := cardRenewalMonth(now)
		if start != tc.start || end != tc.end {
			t.Fatalf("北京时间月份边界错误: %s %s %s", tc.now, start, end)
		}
	}
}

func TestBankCardRenewalsAndBoundAccounts(t *testing.T) {
	db, call := accountSettingsTest(t)
	exec := func(query string) {
		t.Helper()
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO recharge_packages(id,name,plan,region,currency,original_amount_minor,sale_usd_minor,months,enabled) VALUES
(1,'Plus monthly','plus','US','USD',2000,2000,1,true),
(2,'Pro monthly','pro_5x','US','USD',7000,7000,1,true);
UPDATE bank_cards SET reserved_usd_minor=500 WHERE id=1;
UPDATE chatgpt_accounts SET payment_card_id=1,subscription_package_id=1,renewal_date=date_trunc('month',NOW() AT TIME ZONE 'Asia/Shanghai')::date;
UPDATE chatgpt_accounts SET renewal_date=NULL WHERE id=3;
INSERT INTO chatgpt_accounts(id,user_id,label,email,payment_card_id,subscription_package_id,renewal_date,deleted_at) VALUES
(6,2,'Last day','last@test.local',1,1,(date_trunc('month',NOW() AT TIME ZONE 'Asia/Shanghai')+interval '1 month -1 day')::date,NULL),
(7,2,'Next month','next@test.local',1,1,(date_trunc('month',NOW() AT TIME ZONE 'Asia/Shanghai')+interval '1 month')::date,NULL),
(8,2,'Past month','past@test.local',1,1,(date_trunc('month',NOW() AT TIME ZONE 'Asia/Shanghai')-interval '1 day')::date,NULL),
(9,2,'Deleted','deleted@test.local',1,1,(NOW() AT TIME ZONE 'Asia/Shanghai')::date,NOW()),
(10,2,'Frozen card account','frozen@test.local',2,2,(NOW() AT TIME ZONE 'Asia/Shanghai')::date,NULL),
(11,2,'Expired card account','expired@test.local',3,1,(NOW() AT TIME ZONE 'Asia/Shanghai')::date,NULL);
UPDATE chatgpt_accounts SET renewal_enabled=true WHERE id>=6;`)
	cardSummary := func(id int) map[string]any {
		t.Helper()
		// 搜索和卡片分页只影响展示，不缩小单卡的账号统计范围。
		data := call("GET", fmt.Sprintf("/api/bank-cards?page_size=1&page=%d", 4-id), 1, nil, 200)
		card := data["cards"].([]any)[0].(map[string]any)
		if card["id"] != float64(id) {
			t.Fatal("银行卡分页错误", data)
		}
		return card["renewal"].(map[string]any)
	}
	summary := cardSummary(1)
	if summary["bound_count"] != float64(7) || summary["renewal_count"] != float64(3) || summary["required_usd_minor"] != float64(6000) || summary["difference_usd_minor"] != float64(-1500) {
		t.Fatal("本月预算未正确处理关闭续订、空日期、月份边界及软删除", summary)
	}
	if cardSummary(2)["difference_usd_minor"] != float64(-1000) || cardSummary(3)["difference_usd_minor"] != float64(-2000) {
		t.Fatal("冻结或过期卡仍需展示续费预算")
	}
	exec(`UPDATE bank_cards SET balance_usd_minor=8000 WHERE id=2`)
	if cardSummary(2)["difference_usd_minor"] != float64(1000) {
		t.Fatal("余额充足时应返回正差额")
	}
	exec(`UPDATE bank_cards SET balance_usd_minor=7000 WHERE id=2`)
	if cardSummary(2)["difference_usd_minor"] != float64(0) {
		t.Fatal("余额恰好覆盖续费时应返回零")
	}
	seen := map[float64]bool{}
	for page := 1; page <= 4; page++ {
		data := call("GET", fmt.Sprintf("/api/bank-cards/1/accounts?page_size=2&page=%d", page), 1, nil, 200)
		if data["total"] != float64(7) || data["available_usd_minor"] != float64(4500) || data["renewal"].(map[string]any)["required_usd_minor"] != float64(6000) {
			t.Fatal("明细分页不能影响总数及本月预算", data)
		}
		for _, raw := range data["accounts"].([]any) {
			a := raw.(map[string]any)
			id := a["id"].(float64)
			if seen[id] || id == 5 || id == 9 || a["subscription_plan"] != "plus" || a["renewal_usd_minor"] != float64(2000) || a["in_month"] != (id == 1 || id == 4 || id == 6) {
				t.Fatal("绑定明细套餐、金额或本月标记错误", a)
			}
			seen[id] = true
		}
		encoded, _ := json.Marshal(data)
		for _, secret := range []string{"session_ciphertext", "api_key", "number_ciphertext", "cvc_ciphertext", "secret-number"} {
			if strings.Contains(string(encoded), secret) {
				t.Fatal("绑定明细不能包含敏感字段", secret)
			}
		}
	}
	if len(seen) != 7 {
		t.Fatal("明细遗漏绑定账号", seen)
	}
	exec(`UPDATE chatgpt_accounts SET subscription_package_id=NULL WHERE id=1`)
	summary = cardSummary(1)
	if summary["unknown_count"] != float64(1) || summary["difference_usd_minor"] != nil || summary["required_usd_minor"] != float64(4000) {
		t.Fatal("套餐缺失不能误报余额充足", summary)
	}
	exec(`UPDATE recharge_packages SET enabled=false WHERE id=1`)
	if cardSummary(1)["unknown_count"] != float64(3) {
		t.Fatal("下架套餐需标明待核算")
	}
	exec(`UPDATE recharge_packages SET enabled=true,auto_usd=true,currency='PHP',original_amount_minor=100000 WHERE id=1`)
	if cardSummary(1)["unknown_count"] != float64(3) {
		t.Fatal("自动定价无汇率时需标明待核算")
	}
	exec(`INSERT INTO exchange_rates(base_currency,quote_currency,rate,source,effective_at) VALUES('PHP','USD',0.02,'test',NOW())`)
	summary = cardSummary(1)
	if summary["unknown_count"] != float64(1) || summary["required_usd_minor"] != float64(4000) {
		t.Fatal("自动定价应使用持久化有效汇率", summary)
	}
	exec(`UPDATE exchange_rates SET effective_at=NOW()-interval '3 days'`)
	if cardSummary(1)["unknown_count"] != float64(3) {
		t.Fatal("过期汇率不能计算本月预算")
	}
	exec(`UPDATE chatgpt_accounts SET payment_card_id=NULL WHERE payment_card_id=1`)
	data := call("GET", "/api/bank-cards/1/accounts", 1, nil, 200)
	if data["total"] != float64(0) || len(data["accounts"].([]any)) != 0 || data["renewal"].(map[string]any)["difference_usd_minor"] != float64(4500) {
		t.Fatal("解绑后应更新数量与预算", data)
	}
	call("GET", "/api/bank-cards/1/accounts", 2, nil, 403)
	call("POST", "/api/bank-cards/1/accounts", 1, nil, 405)
	call("GET", "/api/bank-cards/1/accounts?page_size=0", 1, nil, 400)
	call("GET", "/api/bank-cards/4/accounts", 1, nil, 404)
	call("GET", "/api/bank-cards/999/accounts", 1, nil, 404)
	archived := call("GET", "/api/bank-cards?archived=1", 1, nil, 200)["cards"].([]any)[0].(map[string]any)
	if _, ok := archived["renewal"]; ok {
		t.Fatal("已删除卡不能展示当前预算", archived)
	}
}
