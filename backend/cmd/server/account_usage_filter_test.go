package main

import (
	"encoding/csv"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountUsageFilter(t *testing.T) {
	db, call := accountSettingsTest(t)
	if _, err := db.Exec(`UPDATE chatgpt_accounts SET usage_status=CASE WHEN id IN(2,4,5) THEN 'discarded' ELSE 'in_use' END,payment_card_id=1,renewal_date=(NOW() AT TIME ZONE 'Asia/Shanghai')::date;
INSERT INTO chatgpt_accounts(id,user_id,label,email,usage_status,deleted_at) VALUES(6,2,'Deleted','deleted@usage.test','discarded',NOW());`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query string
		ids   []int
	}{
		{"", []int{3, 1}},
		{"usage_status=in_use", []int{3, 1}},
		{"usage_status=discarded", []int{4, 2}},
		{"usage_status=discarded&group=1&q=Alpha&payment_card_id=1&renewal_status=soon", []int{4}},
		{"usage_status=in_use&group=1", nil},
	} {
		for page := 1; page <= len(tc.ids)+1; page++ {
			data := call("GET", fmt.Sprintf("/api/accounts?paged=1&page_size=1&page=%d&%s", page, tc.query), 1, nil, 200)
			rows := data["accounts"].([]any)
			if data["total"] != float64(len(tc.ids)) {
				t.Fatal("使用状态筛选总数错误", tc.query, data)
			}
			if page <= len(tc.ids) {
				if len(rows) != 1 || rows[0].(map[string]any)["id"] != float64(tc.ids[page-1]) {
					t.Fatal("跨页状态筛选错误", tc.query, page, rows)
				}
			} else if len(rows) != 0 {
				t.Fatal("超出范围应返回空页", rows)
			}
		}
		s := &Server{db: db, secret: []byte("account-settings-test")}
		r := httptest.NewRequest("GET", "/api/accounts/export?"+tc.query, nil)
		r.Header.Set("Authorization", "Bearer "+s.token(1))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\ufeff"))).ReadAll()
		if w.Code != 200 || err != nil || len(records) != len(tc.ids)+1 {
			t.Fatal("导出未遵循状态筛选", tc.query, w.Code, records, err)
		}
		for i, id := range tc.ids {
			if records[i+1][0] != fmt.Sprint(id) {
				t.Fatal("导出账号错误", tc.query, records)
			}
		}
	}
	for _, tc := range []struct {
		status string
		count  float64
	}{{"in_use", 2}, {"discarded", 1}} {
		cards := call("GET", "/api/accounts/payment-cards?usage_status="+tc.status, 1, nil, 200)["cards"].([]any)
		if len(cards) != 1 || cards[0].(map[string]any)["renewal_count"] != tc.count {
			t.Fatal("预算未遵循状态筛选", tc.status, cards)
		}
	}
	for _, path := range []string{"/api/accounts?paged=1&", "/api/accounts/export?", "/api/accounts/payment-cards?"} {
		call("GET", path+"usage_status=invalid", 1, nil, 400)
	}
	// 普通用户不能通过管理筛选推断账号标记，仍只能看到自己的账号。
	for _, status := range []string{"in_use", "discarded"} {
		data := call("GET", "/api/accounts?paged=1&usage_status="+status, 2, nil, 200)
		if data["total"] != float64(3) {
			t.Fatal("管理筛选影响普通用户范围", data)
		}
	}
}
