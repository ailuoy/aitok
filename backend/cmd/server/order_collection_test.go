package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCollectionProfitAndFreshness(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, exchangeTimezone)
	batch := &ExchangeRate{ID: 1, Rate: "0.02", CNYRate: "0.14", Source: "test", EffectiveAt: now, SyncedAt: now}
	order := RechargeOrder{OrderStatus: "active", SaleUSDMinor: 10000}
	for _, sample := range []struct {
		currency, amount            string
		usd, profit, receivedProfit int64
		margin                      string
	}{
		{"CNY", "840.00", 12000, 2000, 14000, "16.67"},
		{"CNY", "700.00", 10000, 0, 0, "0.00"},
		{"USD", "80.00", 8000, -2000, -2000, "-25.00"},
		{"USD", "100.01", 10001, 1, 1, "0.01"},
	} {
		rate, err := collectionRate(sample.currency, batch, now)
		if err != nil {
			t.Fatal(err)
		}
		quote, err := quoteCollection(order, sample.currency, sample.amount, rate)
		if err != nil || quote.USDMinor != sample.usd || quote.Profit.USDMinor != sample.profit || quote.Profit.ReceivedMinor != sample.receivedProfit || quote.Profit.RatePercent != sample.margin || !quote.Profit.Estimated {
			t.Fatalf("quote %s %s: %+v %v", sample.currency, sample.amount, quote, err)
		}
	}
	for _, amount := range []string{"0", "-1", "1.001", "1e2", "10000000000.01"} {
		if _, err := quoteCollection(order, "USD", amount, &CollectionRate{USDPerUnit: "1"}); err == nil {
			t.Fatal("接受了非法金额", amount)
		}
	}
	if _, err := collectionRate("PHP", batch, now); err == nil {
		t.Fatal("接受了不支持的收款币种")
	}
	for _, when := range []time.Time{now.Add(-24 * time.Hour), time.Date(2026, 9, 16, 8, 59, 0, 0, exchangeTimezone)} {
		old := *batch
		old.SyncedAt = when
		if _, err := collectionRate("CNY", &old, now); err == nil {
			t.Fatal("接受了旧同步批次")
		}
	}
	early := time.Date(2026, 9, 16, 0, 1, 0, 0, exchangeTimezone)
	if collectionRateSince(early).Day() != 16 || collectionRateSince(early).Hour() != 0 {
		t.Fatal("凌晨必须补同步当日汇率")
	}
	if collectionRateSince(now).Hour() != 9 {
		t.Fatal("九点后需满足当天定时同步")
	}
}

func TestOrderCollectionSnapshots(t *testing.T) {
	db := walletTestDB(t)
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'collector@test.local','','admin'),(2,'user@test.local','','user');
INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,1,'Test','account@test.local');
INSERT INTO bank_cards(id,user_id,label,cardholder,number_ciphertext,number_fingerprint,last4,brand,exp_month,exp_year,balance_usd_minor) VALUES(1,1,'Test','Tester','encrypted','fingerprint','4242','Visa',12,2035,100000);
INSERT INTO recharge_orders(id,order_no,user_id,account_id,account_email,package_id,package_snapshot,period_start,period_end,sale_usd_minor,request_key) VALUES
(1,'collection-order-1',1,1,'account@test.local',1,'{"name":"Test","plan":"plus","months":1,"currency":"USD","original_amount_minor":10000}','2030-01-01','2030-02-01',10000,'collection-order-1'),
(2,'collection-order-2',1,1,'account@test.local',1,'{"name":"Test"}','2030-02-01','2030-03-01',10000,'collection-order-2');`)
	if err != nil {
		t.Fatal(err)
	}
	fetches := 0
	s := &Server{db: db, secret: []byte("test"), exchangeRateFetch: func(_ context.Context, now time.Time) (*ExchangeRate, error) {
		fetches++
		return &ExchangeRate{Rate: "0.02", CNYRate: "0.14", Source: "test", EffectiveAt: now, SyncedAt: now}, nil
	}}
	call := func(method, path string, user int64, input any, status int) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+s.token(user))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d want %d %s", method, path, w.Code, status, w.Body.String())
		}
		return w
	}
	decode := func(w *httptest.ResponseRecorder) map[string]any {
		t.Helper()
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	quotePath := "/api/orders/1/collection-quote?currency=CNY&amount=840.00"
	call("GET", quotePath, 2, nil, 403)
	quote := decode(call("GET", quotePath, 1, nil, 200))
	call("GET", quotePath, 1, nil, 200)
	if fetches != 1 || quote["usd_minor"] != float64(12000) {
		t.Fatal("汇率应当日复用，收入折算不正确", fetches, quote)
	}
	id := quote["exchange_rate"].(map[string]any)["batch"].(map[string]any)["id"]
	collect := map[string]any{"action": "collect", "method": "manual", "received_currency": "CNY", "received_amount": "840.00", "collection_rate_id": 0, "reference": "receipt-cny-1", "evidence": "实收凭据", "request_key": "collect-cny-0001", "version": 0}
	call("POST", "/api/orders/1", 1, collect, 409)
	collect["collection_rate_id"] = id
	result := decode(call("POST", "/api/orders/1", 1, collect, 200))["order"].(map[string]any)
	if result["received_currency"] != "CNY" || result["received_amount_minor"] != float64(84000) || result["received_usd_minor"] != float64(12000) || result["profit"].(map[string]any)["rate_percent"] != "16.67" {
		t.Fatal("实收快照未保存", result)
	}
	// 汇率变化后重放原确认必须成功，旧订单收入及毛利保持不变。
	_, err = db.Exec(`INSERT INTO exchange_rates(base_currency,quote_currency,rate,source,effective_at,created_at,updated_at) VALUES('PHP','USD',0.02,'new',NOW(),NOW(),NOW()),('PHP','CNY',0.16,'new',NOW(),NOW(),NOW())`)
	if err != nil {
		t.Fatal(err)
	}
	call("POST", "/api/orders/1", 1, collect, 200)
	collect["received_amount"] = "850.00"
	call("POST", "/api/orders/1", 1, collect, 409)
	collect["received_amount"] = "840.00"
	purchase := map[string]any{"action": "purchase", "card_id": 1, "reference": "purchase-after-cny", "evidence": "账单", "request_key": "purchase-after-cny", "version": 1}
	call("POST", "/api/orders/1", 1, purchase, 200)
	order := decode(call("GET", "/api/orders/1", 1, nil, 200))["order"].(map[string]any)
	profit := order["profit"].(map[string]any)
	if order["received_usd_minor"] != float64(12000) || profit["usd_minor"] != float64(2000) || profit["estimated"] != false {
		t.Fatal("改汇率后旧单利润漂移", profit)
	}
	list := decode(call("GET", "/api/orders", 1, nil, 200))["orders"].([]any)
	if list[0].(map[string]any)["profit"] != nil {
		t.Fatal("未收款不能凭空计算利润")
	}
	export := call("GET", "/api/orders/export", 1, nil, 200).Body.String()
	if !strings.Contains(export, "毛利率%") || !strings.Contains(export, "840.00,120.00,20.00,16.67") {
		t.Fatal("导出遗漏实收或利润", export)
	}
	collect["received_currency"] = "USD"
	collect["received_amount"] = "80.00"
	collect["collection_rate_id"] = 0
	collect["reference"] = "receipt-usd-2"
	collect["request_key"] = "collect-usd-0002"
	loss := decode(call("POST", "/api/orders/2", 1, collect, 200))["order"].(map[string]any)["profit"].(map[string]any)
	if loss["usd_minor"] != float64(-2000) || loss["rate_percent"] != "-25.00" {
		t.Fatal("亏损计算错误", loss)
	}
	call("POST", "/api/orders/1", 1, map[string]any{"action": "refund_note", "reason": "refund", "reference": "refund-receipt-1", "evidence": "退款凭据", "request_key": "refund-receipt-1", "version": 2}, 200)
	closed := decode(call("GET", "/api/orders/1", 1, nil, 200))["order"].(map[string]any)
	if closed["profit"] != nil || closed["received_usd_minor"] != float64(12000) {
		t.Fatal("退款需保留收入快照但不冒充已结算毛利")
	}
	call("GET", quotePath, 1, nil, 409)
	// 无可用汇率时，获取失败不能伪造汇率；USD 仍可直接确认。
	_, err = db.Exec(`UPDATE exchange_rates SET deleted_at=NOW(),updated_at=NOW()`)
	if err != nil {
		t.Fatal(err)
	}
	s.exchangeRateFetch = func(context.Context, time.Time) (*ExchangeRate, error) { return nil, errors.New("offline") }
	if _, err := s.currentCollectionRate(context.Background(), "CNY"); err == nil {
		t.Fatal("汇率失败应返回错误")
	}
	if _, err := s.currentCollectionRate(context.Background(), "USD"); err != nil {
		t.Fatal(err)
	}
}
