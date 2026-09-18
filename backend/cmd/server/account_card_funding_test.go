package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestAccountCardFundingScopeAndBalance(t *testing.T) {
	db, call := accountSettingsTest(t)
	exec := func(query string) {
		t.Helper()
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO recharge_packages(id,name,plan,region,currency,original_amount_minor,sale_usd_minor,months,enabled) VALUES(1,'Plus','plus','US','USD',2000,2000,1,true);
UPDATE chatgpt_accounts SET payment_card_id=1,subscription_package_id=1,renewal_date=(NOW() AT TIME ZONE 'Asia/Shanghai')::date+CASE id WHEN 1 THEN 0 WHEN 4 THEN 10 ELSE 5 END;
UPDATE chatgpt_accounts SET renewal_date=NULL WHERE id=3;
INSERT INTO chatgpt_accounts(id,user_id,label,email,payment_card_id,subscription_package_id,renewal_date,renewal_enabled,deleted_at) VALUES
(6,2,'Future','future@test.local',1,1,(NOW() AT TIME ZONE 'Asia/Shanghai')::date+11,true,NULL),
(7,2,'Past','past@test.local',1,1,(NOW() AT TIME ZONE 'Asia/Shanghai')::date-1,true,NULL),
(8,2,'Deleted','deleted@test.local',1,1,(NOW() AT TIME ZONE 'Asia/Shanghai')::date,true,NOW());`)
	check := func(query string, count, required, unknown int, status string) {
		t.Helper()
		cards := call("GET", "/api/accounts/payment-cards?"+query, 1, nil, 200)["cards"].([]any)
		if len(cards) != 1 {
			t.Fatal("不可用卡不得进入预算", cards)
		}
		card := cards[0].(map[string]any)
		if card["renewal_count"] != float64(count) || card["required_usd_minor"] != float64(required) || card["unknown_count"] != float64(unknown) || card["funding_status"] != status {
			t.Fatalf("%s 预算错误: %v", query, card)
		}
	}
	// 同一卡的两个近期账号合计，排除关闭续订、无日期、已删账号及已删用户。
	check("renewal_status=soon", 2, 4000, 0, "sufficient")
	check("renewal_status=safe", 1, 2000, 0, "sufficient")
	check("renewal_status=overdue", 1, 2000, 0, "sufficient")
	check("", 4, 8000, 0, "insufficient")
	for page := 1; page <= 2; page++ {
		check(fmt.Sprintf("renewal_status=soon&page_size=1&page=%d", page), 2, 4000, 0, "sufficient")
	}
	check("renewal_status=soon&group=1&q=Alpha", 1, 2000, 0, "sufficient")
	check("group=none", 2, 4000, 0, "sufficient")
	check("q=missing", 0, 0, 0, "sufficient")
	exec(`UPDATE bank_cards SET reserved_usd_minor=1000 WHERE id=1`)
	check("renewal_status=soon", 2, 4000, 0, "sufficient")
	exec(`UPDATE bank_cards SET reserved_usd_minor=1001 WHERE id=1`)
	check("renewal_status=soon", 2, 4000, 0, "insufficient")
	card := call("GET", "/api/accounts/payment-cards", 1, nil, 200)["cards"].([]any)[0].(map[string]any)
	if card["balance_usd_minor"] != float64(5000) || card["reserved_usd_minor"] != float64(1001) {
		t.Fatal("应返回系统余额及占用", card)
	}
	// 绑定、是否续订、套餐变更后重算。
	call("PATCH", "/api/accounts/1/subscription", 1, map[string]any{"renewal_enabled": false}, 200)
	check("renewal_status=soon", 1, 2000, 0, "sufficient")
	call("PATCH", "/api/accounts/4/payment-card", 1, map[string]any{"payment_card_id": nil}, 200)
	check("renewal_status=soon", 0, 0, 0, "sufficient")
	for _, query := range []string{"renewal_status=invalid", "group=invalid", "q=" + strings.Repeat("a", 201)} {
		call("GET", "/api/accounts/payment-cards?"+query, 1, nil, 400)
	}
	call("GET", "/api/accounts/payment-cards?renewal_status=soon", 2, nil, 403)
}

func TestAccountCardFundingUnknownAndExchangeRate(t *testing.T) {
	db, call := accountSettingsTest(t)
	exec := func(query string) {
		t.Helper()
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE chatgpt_accounts SET payment_card_id=1 WHERE id IN(1,4);
INSERT INTO recharge_packages(id,name,plan,region,currency,original_amount_minor,sale_usd_minor,months,enabled,auto_usd) VALUES
(1,'Fixed','plus','US','USD',2000,2000,3,true,false),
(2,'PHP','plus','PH','PHP',100001,1,1,true,true);
UPDATE chatgpt_accounts SET subscription_package_id=1 WHERE id=1;`)
	check := func(required, unknown int, status string) {
		t.Helper()
		card := call("GET", "/api/accounts/payment-cards", 1, nil, 200)["cards"].([]any)[0].(map[string]any)
		if card["required_usd_minor"] != float64(required) || card["unknown_count"] != float64(unknown) || card["funding_status"] != status {
			t.Fatal("缺价或汇率预算错误", card)
		}
	}
	check(2000, 1, "unknown") // 未选套餐；多月套餐按一次完整扣款。
	exec(`UPDATE chatgpt_accounts SET subscription_package_id=2 WHERE id=4`)
	check(2000, 1, "unknown") // 无汇率，不使用自动定价套餐的旧 USD 值。
	exec(`INSERT INTO exchange_rates(base_currency,quote_currency,rate,source,effective_at) VALUES('PHP','USD',0.018,'test',NOW())`)
	check(3800, 0, "sufficient")
	exec(`UPDATE exchange_rates SET effective_at=NOW()-INTERVAL '49 hours'`)
	check(2000, 1, "unknown")
	exec(`UPDATE recharge_packages SET enabled=false WHERE id=1`)
	check(0, 2, "unknown")
	exec(`UPDATE recharge_packages SET enabled=true,deleted_at=NOW() WHERE id=1`)
	check(0, 2, "unknown")
	exec(`UPDATE recharge_packages SET deleted_at=NULL WHERE id=1; UPDATE bank_cards SET balance_usd_minor=1999 WHERE id=1`)
	check(2000, 1, "insufficient") // 已知部分已不足，即使其余价格未知也应标红。
}
