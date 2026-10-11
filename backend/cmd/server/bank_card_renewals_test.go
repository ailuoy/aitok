package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCardRenewalNext10DaysUTC8(t *testing.T) {
	for _, tc := range []struct{ now, start, end string }{
		{"2026-12-31T15:59:59Z", "2026-12-31", "2027-01-10"},
		{"2026-12-31T16:00:00Z", "2027-01-01", "2027-01-11"},
		{"2028-02-28T16:00:00Z", "2028-02-29", "2028-03-10"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		start, end := cardRenewalNext10Days(now)
		if start != tc.start || end != tc.end {
			t.Fatalf("北京时间十天边界错误: %s %s %s", tc.now, start, end)
		}
	}
}

func TestCardUpcomingRenewals(t *testing.T) {
	db, _ := accountSettingsTest(t)
	// 固定跨年窗口，覆盖起止边界、关闭续订、空日期、软删除及未知价格。
	_, err := db.Exec(`INSERT INTO chatgpt_accounts(id,user_id,label,email,payment_card_id,subscription_package_id,renewal_date,renewal_enabled,deleted_at) VALUES
(6,2,'Today','today@test.local',1,1,'2026-12-27',true,NULL),
(7,2,'Last day','last@test.local',1,2,'2027-01-05',true,NULL),
(8,2,'Outside','outside@test.local',1,1,'2027-01-06',true,NULL),
(9,2,'Overdue','overdue@test.local',1,1,'2026-12-26',true,NULL),
(10,2,'Disabled','disabled@test.local',1,1,'2026-12-28',false,NULL),
(11,2,'No date','no-date@test.local',1,1,NULL,true,NULL),
(12,2,'Deleted','deleted@test.local',1,1,'2026-12-28',true,NOW()),
(13,3,'Deleted owner','owner@test.local',1,1,'2026-12-28',true,NULL),
(14,2,'Unknown','unknown@test.local',1,NULL,'2026-12-28',true,NULL),
(15,2,'Other card','other@test.local',2,1,'2026-12-28',true,NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 12, 27, 12, 0, 0, 0, exchangeTimezone)
	packages := map[int64]RechargePackage{
		1: {PriceReady: true, SaleUSDMinor: 2000},
		2: {PriceReady: true, SaleUSDMinor: 7000},
	}
	summarize := func() []bankCardWithRenewal {
		t.Helper()
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		cards := []bankCardWithRenewal{{BankCard: BankCard{ID: 1}}, {BankCard: BankCard{ID: 2}}, {BankCard: BankCard{ID: 3}}}
		if err := fillCardRenewals(context.Background(), tx, cards, packages, now); err != nil {
			t.Fatal(err)
		}
		return cards
	}
	cards := summarize()
	summary := cards[0].Renewal
	if summary.BoundCount != 7 || summary.Next10Days.RenewalCount != 3 || summary.Next10Days.Required != 9000 || summary.Next10Days.UnknownCount != 1 {
		t.Fatalf("十天续费统计错误: %+v", summary)
	}
	if summary.RenewalCount != 3 || summary.Required != 4000 || summary.UnknownCount != 1 {
		t.Fatalf("本月统计口径改变: %+v", summary)
	}
	if cards[1].Renewal.Next10Days.Required != 2000 || cards[2].Renewal.Next10Days != (cardRenewalTotals{}) {
		t.Fatal("卡片统计未隔离或空卡统计错误", cards)
	}
	packages[2] = RechargePackage{}
	if got := summarize()[0].Renewal.Next10Days; got.Required != 2000 || got.UnknownCount != 2 || got.RenewalCount != 3 {
		t.Fatalf("不可用套餐应计入待核算: %+v", got)
	}
	packages[1] = RechargePackage{PriceReady: true, SaleUSDMinor: 1 << 53}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := fillCardRenewals(context.Background(), tx, cards, packages, now); err == nil {
		t.Fatal("续费金额超出安全整数范围应拒绝")
	}
}

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

func TestBankCardUpcomingAccountDetails(t *testing.T) {
	db, call := accountSettingsTest(t)
	_, err := db.Exec(`INSERT INTO recharge_packages(id,name,plan,region,currency,original_amount_minor,sale_usd_minor,months,enabled) VALUES
(1,'Plus monthly','plus','US','USD',2000,2000,1,true),
(2,'Pro unavailable','pro_5x','US','USD',7000,7000,1,false);
UPDATE chatgpt_accounts SET payment_card_id=1,subscription_package_id=1,renewal_date=(NOW() AT TIME ZONE 'Asia/Shanghai')::date;
UPDATE chatgpt_accounts SET renewal_date=NULL WHERE id=3;
UPDATE chatgpt_accounts SET renewal_date=renewal_date+9 WHERE id=4;
INSERT INTO chatgpt_accounts(id,user_id,label,email,payment_card_id,subscription_package_id,renewal_date,renewal_enabled,deleted_at) VALUES
(6,2,'Unknown','unknown@test.local',1,NULL,(NOW() AT TIME ZONE 'Asia/Shanghai')::date+1,true,NULL),
(7,2,'Outside','outside@test.local',1,1,(NOW() AT TIME ZONE 'Asia/Shanghai')::date+10,true,NULL),
(8,2,'Deleted','deleted@test.local',1,1,(NOW() AT TIME ZONE 'Asia/Shanghai')::date,true,NOW()),
(9,2,'Other card','other@test.local',2,1,(NOW() AT TIME ZONE 'Asia/Shanghai')::date,true,NULL),
(10,2,'Overdue','past@test.local',1,1,(NOW() AT TIME ZONE 'Asia/Shanghai')::date-1,true,NULL),
(11,2,'Unavailable','unavailable@test.local',1,2,(NOW() AT TIME ZONE 'Asia/Shanghai')::date+5,true,NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/bank-cards/1/accounts?renewal_window=next_10_days"
	wantIDs, wantDays := []int{1, 6, 11, 4}, []int{0, 1, 5, 9}
	for i, id := range wantIDs {
		data := call("GET", fmt.Sprintf("%s&page_size=1&page=%d", path, i+1), 1, nil, 200)
		summary := data["renewal"].(map[string]any)["next_10_days"].(map[string]any)
		rows := data["accounts"].([]any)
		if data["total"] != float64(4) || summary["renewal_count"] != data["total"] || len(rows) != 1 || summary["required_usd_minor"] != float64(4000) || summary["unknown_count"] != float64(2) {
			t.Fatal("续费明细分页与统计范围不一致", data)
		}
		account := rows[0].(map[string]any)
		today, err := time.Parse("2006-01-02", data["today"].(string))
		if err != nil || account["id"] != float64(id) || account["renewal_date"] != today.AddDate(0, 0, wantDays[i]).Format("2006-01-02") || account["has_session"] != (id == 1 || id == 4) {
			t.Fatal("明细排序、北京时间倒计时或浏览器可用标记错误", account, err)
		}
		if id == 1 && (account["subscription_plan"] != "plus" || account["package_name"] != "Plus monthly" || account["renewal_usd_minor"] != float64(2000)) {
			t.Fatal("套餐或续费金额错误", account)
		}
		if (id == 6 || id == 11) && account["renewal_usd_minor"] != nil {
			t.Fatal("未知价格不应显示为零金额", account)
		}
		encoded, _ := json.Marshal(data)
		for _, secret := range []string{"session_ciphertext", "test-session", "secret-number", "secret-cvc"} {
			if strings.Contains(string(encoded), secret) {
				t.Fatal("续费明细泄露敏感数据")
			}
		}
	}
	if rows := call("GET", path+"&page=5&page_size=1", 1, nil, 200)["accounts"].([]any); len(rows) != 0 {
		t.Fatal("超出页码应返回空列表")
	}
	call("GET", path, 2, nil, 403)
	call("GET", "/api/bank-cards/1/accounts?renewal_window=invalid", 1, nil, 400)
	call("GET", "/api/bank-cards/4/accounts?renewal_window=next_10_days", 1, nil, 404)
	if _, err := db.Exec(`UPDATE chatgpt_accounts SET renewal_enabled=false WHERE payment_card_id=1`); err != nil {
		t.Fatal(err)
	}
	data := call("GET", path, 1, nil, 200)
	if data["total"] != float64(0) || len(data["accounts"].([]any)) != 0 {
		t.Fatal("关闭续订后应更新明细与数量", data)
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
