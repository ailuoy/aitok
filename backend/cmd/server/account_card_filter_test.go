package main

import (
	"encoding/csv"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountPaymentCardFilter(t *testing.T) {
	db, call := accountSettingsTest(t)
	_, err := db.Exec(`UPDATE chatgpt_accounts SET payment_card_id=CASE WHEN id IN(1,4,5) THEN 1 WHEN id=2 THEN 2 END,renewal_date=(NOW() AT TIME ZONE 'Asia/Shanghai')::date;
INSERT INTO chatgpt_accounts(id,user_id,label,email,payment_card_id,deleted_at) VALUES
(6,2,'Deleted account','deleted-card-account@test.local',1,NOW()),
(7,2,'Deleted card','deleted-card@test.local',4,NULL),
(8,2,'Expired card','expired-card@test.local',3,NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		filter string
		ids    []int
	}{
		{"payment_card_id=1", []int{4, 1}},
		{"payment_card_id=none", []int{7, 3}},
		{"payment_card_id=2", []int{2}},
		{"payment_card_id=3", []int{8}},
		{"payment_card_id=1&group=1&q=Alpha&renewal_status=soon", []int{4}},
		{"", []int{8, 7, 4, 3, 2, 1}},
	} {
		for i, id := range tc.ids {
			data := call("GET", fmt.Sprintf("/api/accounts?paged=1&page_size=1&page=%d&%s", i+1, tc.filter), 1, nil, 200)
			rows := data["accounts"].([]any)
			if data["total"] != float64(len(tc.ids)) || len(rows) != 1 || rows[0].(map[string]any)["id"] != float64(id) {
				t.Fatalf("付款卡筛选或跨页结果错误: %s %v", tc.filter, data)
			}
		}
	}
	for _, value := range []string{"4", "999"} {
		if data := call("GET", "/api/accounts?paged=1&payment_card_id="+value, 1, nil, 200); data["total"] != float64(0) {
			t.Fatal("已删除或不存在的卡不应返回账号", data)
		}
	}
	for _, value := range []string{"0", "-1", "invalid", "1.5", "9223372036854775808"} {
		call("GET", "/api/accounts?paged=1&payment_card_id="+value, 1, nil, 400)
		call("GET", "/api/accounts/payment-cards?payment_card_id="+value, 1, nil, 400)
	}
	// 普通用户忽略管理筛选，不能通过筛选结果推断绑卡信息。
	if data := call("GET", "/api/accounts?paged=1&payment_card_id=999", 2, nil, 200); data["total"] != float64(5) {
		t.Fatal("付款卡筛选不应影响普通用户", data)
	}
	for _, tc := range []struct {
		card  string
		count float64
	}{{"1", 2}, {"2", 0}, {"none", 0}} {
		cards := call("GET", "/api/accounts/payment-cards?payment_card_id="+tc.card, 1, nil, 200)["cards"].([]any)
		if len(cards) != 1 || cards[0].(map[string]any)["renewal_count"] != tc.count {
			t.Fatal("付款卡预算应遵循筛选且保留可选卡", cards)
		}
	}
	s := &Server{db: db, secret: []byte("account-settings-test")}
	r := httptest.NewRequest("GET", "/api/accounts/export?payment_card_id=1&group=1&q=Alpha&renewal_status=soon", nil)
	r.Header.Set("Authorization", "Bearer "+s.token(1))
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
	if w.Code != 200 || err != nil || len(records) != 2 || records[1][0] != "4" {
		t.Fatalf("导出未遵循付款卡组合筛选: %d %v %v", w.Code, records, err)
	}
}
