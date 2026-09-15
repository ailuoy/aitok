package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExchangeRateRoundingAndSchedule(t *testing.T) {
	for _, test := range []struct {
		amount int64
		rate   string
		want   int64
	}{{99900, "0.01589", 1587}, {649000, "0.01589", 10313}, {999000, "0.01589", 15874}, {100, "0.015", 2}, {100, "0.014999999999", 1}} {
		got, err := convertMoney(test.amount, test.rate)
		if err != nil || got != test.want {
			t.Fatalf("%+v: %d %v", test, got, err)
		}
	}
	for _, rate := range []string{"0", "-1", "NaN", "1e99"} {
		if _, err := convertMoney(99900, rate); err == nil {
			t.Fatal("accepted invalid conversion", rate)
		}
	}
	now := time.Date(2026, 9, 15, 8, 59, 0, 0, exchangeTimezone)
	if exchangeSchedule(now).Day() != 14 || exchangeSchedule(now.Add(time.Minute)).Day() != 15 {
		t.Fatal("daily UTC+8 schedule boundary")
	}
}

func TestExchangeRateProviderValidation(t *testing.T) {
	now := time.Now().UTC()
	valid := fmt.Sprintf(`{"result":"success","base_code":"PHP","time_last_update_unix":%d,"rates":{"USD":0.01589,"CNY":0.11234}}`, now.Unix())
	for _, tc := range []struct {
		name, body string
		status     int
		ok         bool
	}{
		{"valid", valid, 200, true},
		{"wrong currency", strings.Replace(valid, `"PHP"`, `"USD"`, 1), 200, false},
		{"inverse rate", strings.Replace(valid, "0.01589", "62.9", 1), 200, false},
		{"negative", strings.Replace(valid, "0.01589", "-1", 1), 200, false},
		{"missing CNY", strings.Replace(valid, `,"CNY":0.11234`, "", 1), 200, false},
		{"invalid CNY", strings.Replace(valid, "0.11234", "0", 1), 200, false},
		{"stale", strings.Replace(valid, fmt.Sprint(now.Unix()), fmt.Sprint(now.Add(-49*time.Hour).Unix()), 1), 200, false},
		{"future", strings.Replace(valid, fmt.Sprint(now.Unix()), fmt.Sprint(now.Add(time.Hour).Unix()), 1), 200, false},
		{"trailing", valid + `{}`, 200, false},
		{"unavailable", valid, 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer server.Close()
			rate, err := fetchExchangeRate(context.Background(), server.Client(), server.URL, now)
			if (err == nil) != tc.ok {
				t.Fatalf("unexpected result: %v", err)
			}
			if tc.ok && (rate.Rate != "0.015890000000" || rate.CNYRate != "0.112340000000") {
				t.Fatal(rate.Rate)
			}
		})
	}
}

func TestCNYConversionAndMissingBatch(t *testing.T) {
	now := time.Now()
	rate := &ExchangeRate{Rate: "0.01589", CNYRate: "0.1067", EffectiveAt: now, SyncedAt: now}
	for _, tc := range []struct{ php, want int64 }{{89196, 9517}, {579464, 61829}, {891964, 95173}} {
		pkg := RechargePackage{AutoUSD: true, Currency: "PHP", OriginalAmountMinor: tc.php}
		pricePackage(&pkg, rate, now)
		if pkg.SaleCNYMinor == nil || *pkg.SaleCNYMinor != tc.want || !pkg.CNYPriceReady {
			t.Fatalf("CNY conversion: %+v", pkg)
		}
		pricePackage(&pkg, rate, now.Add(49*time.Hour))
		if pkg.CNYPriceReady || pkg.PriceReady {
			t.Fatal("stale currency reported fresh")
		}
		pricePackage(&pkg, nil, now)
		if pkg.SaleCNYMinor != nil {
			t.Fatal("missing rate must clear price")
		}
	}
	manual := RechargePackage{SaleUSDMinor: 2000}
	rate.Rate = "0.02"
	rate.CNYRate = "0.14"
	pricePackage(&manual, rate, now)
	if manual.SaleUSDMinor != 2000 || manual.SaleCNYMinor == nil || *manual.SaleCNYMinor != 14000 {
		t.Fatal("manual USD must use USD/CNY cross rate")
	}
	db := walletTestDB(t)
	// 历史 USD 单币记录不能配上不同批次的 CNY。
	_, err := db.Exec(`INSERT INTO exchange_rates(base_currency,quote_currency,rate,source,effective_at,created_at) VALUES('PHP','USD',0.01589,'test',NOW(),NOW()),('PHP','CNY',0.1067,'test',NOW(),NOW()-INTERVAL '1 day')`)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := latestExchangeRate(context.Background(), db)
	if err != nil || latest.CNYRate != "" {
		t.Fatal("mixed different rate batches")
	}
	s := &Server{db: db}
	// 使用同一时间基准，避免 Docker 与宿主机时钟偏差使测试误判汇率倒退。
	now = latest.EffectiveAt.Add(time.Second)
	if err = s.syncExchangeRate(context.Background(), now, func(context.Context) (*ExchangeRate, error) {
		return &ExchangeRate{Rate: "0.01589", CNYRate: "0.1067", Source: "test", EffectiveAt: now, SyncedAt: now}, nil
	}); err != nil {
		t.Fatal(err)
	}
	latest, err = latestExchangeRate(context.Background(), db)
	if err != nil || latest.CNYRate != "0.106700000000" {
		t.Fatal("missing CNY not supplemented on startup", err)
	}
	if _, err = db.Exec(`UPDATE exchange_rates SET deleted_at=NOW(),updated_at=NOW() WHERE quote_currency='CNY'`); err != nil {
		t.Fatal(err)
	}
	latest, err = latestExchangeRate(context.Background(), db)
	if err != nil || latest.CNYRate != "" {
		t.Fatal("soft-deleted CNY leaked")
	}
}

func TestDailyRateSyncFailureAndHistory(t *testing.T) {
	db := walletTestDB(t)
	s := &Server{db: db}
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, exchangeTimezone)
	calls := 0
	fetch := func(context.Context) (*ExchangeRate, error) {
		calls++
		return &ExchangeRate{Rate: "0.01589", CNYRate: "0.11234", Source: exchangeRateSource, EffectiveAt: now.Add(-time.Hour), SyncedAt: now}, nil
	}
	for i := 0; i < 2; i++ {
		if err := s.syncExchangeRate(context.Background(), now, fetch); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatal("same day refetched", calls)
	}
	next := now.AddDate(0, 0, 1)
	if err := s.syncExchangeRate(context.Background(), next, func(context.Context) (*ExchangeRate, error) {
		return &ExchangeRate{Rate: "0.01589", CNYRate: "0", Source: exchangeRateSource, EffectiveAt: next, SyncedAt: next}, nil
	}); err == nil {
		t.Fatal("invalid second currency should roll back entire batch")
	}
	if err := s.syncExchangeRate(context.Background(), next, func(context.Context) (*ExchangeRate, error) { return nil, errors.New("provider offline") }); err == nil {
		t.Fatal("failed provider ignored")
	}
	var count int
	db.QueryRow(`SELECT count(*) FROM exchange_rates`).Scan(&count)
	if count != 2 {
		t.Fatal("failed fetch changed history")
	}
	now = next
	if err := s.syncExchangeRate(context.Background(), now, fetch); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT count(*) FROM exchange_rates`).Scan(&count)
	if count != 4 {
		t.Fatal("next day must append")
	}
	if _, err := db.Exec(`UPDATE exchange_rates SET deleted_at=NOW(),updated_at=NOW() WHERE created_at=$1`, next); err != nil {
		t.Fatal(err)
	}
	latest, err := latestExchangeRate(context.Background(), db)
	if err != nil || latest.ID != 1 {
		t.Fatal("soft-deleted rate returned", err)
	}
}

func TestPackageDynamicUSDAndOrderSnapshot(t *testing.T) {
	db := walletTestDB(t)
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'fx@test.local','','admin');INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,1,'FX','fx-account@test.local');`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("fx-test")}
	call := func(method, path string, input any, want int) map[string]any {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+s.token(1))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s got %d want %d: %s", method, path, w.Code, want, w.Body.String())
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	pkg := map[string]any{"name": "Plus PHP", "plan": "plus", "region": "PH", "currency": "PHP", "original_amount_minor": 99900, "sale_usd_minor": 999999, "auto_usd": true, "wallet_tokens": 0, "months": 1, "enabled": true}
	call("POST", "/api/packages", pkg, 503)
	if _, err = db.Exec(`INSERT INTO exchange_rates(base_currency,quote_currency,rate,source,effective_at) VALUES('PHP','USD',0.01589,'test',NOW()),('PHP','CNY',0.1067,'test',NOW())`); err != nil {
		t.Fatal(err)
	}
	pid := call("POST", "/api/packages", pkg, 200)["id"]
	list := func() map[string]any {
		return call("GET", "/api/packages", nil, 200)["packages"].([]any)[0].(map[string]any)
	}
	if list()["sale_usd_minor"] != float64(1587) || list()["sale_cny_minor"] != float64(10659) {
		t.Fatal("client price was trusted")
	}
	order := map[string]any{"account_id": 1, "package_id": pid, "period_start": "2030-01-01", "request_key": "fx-order-create-01"}
	id := call("POST", "/api/orders", order, 201)["id"]
	if _, err = db.Exec(`INSERT INTO exchange_rates(base_currency,quote_currency,rate,source,effective_at) VALUES('PHP','USD',0.02,'test',NOW()),('PHP','CNY',0.14,'test',NOW())`); err != nil {
		t.Fatal(err)
	}
	if list()["sale_usd_minor"] != float64(1998) || list()["sale_cny_minor"] != float64(13986) {
		t.Fatal("price did not update from rate")
	}
	call("POST", "/api/orders", order, 200)
	call("POST", "/api/orders", map[string]any{"account_id": 1, "package_id": pid, "period_start": "2030-02-01", "request_key": "fx-order-price-race", "expected_sale_usd_minor": 1587}, 409)
	detail := call("GET", fmt.Sprintf("/api/orders/%.0f", id), nil, 200)["order"].(map[string]any)
	if detail["package_snapshot"].(map[string]any)["sale_cny_minor"] != float64(10659) || detail["sale_usd_minor"] != float64(1587) || detail["package_snapshot"].(map[string]any)["exchange_rate"].(map[string]any)["rate"] != "0.015890000000" {
		t.Fatal("historical price/rate mutated")
	}
	if _, err = db.Exec(`UPDATE exchange_rates SET effective_at=NOW()-INTERVAL '3 days',updated_at=NOW()`); err != nil {
		t.Fatal(err)
	}
	if list()["price_ready"] != false {
		t.Fatal("stale rate accepted")
	}
	order["request_key"] = "fx-order-next-001"
	order["period_start"] = "2030-02-01"
	call("POST", "/api/orders", order, 503)
	pkg["auto_usd"] = false
	pkg["sale_usd_minor"] = 2000
	call("PATCH", fmt.Sprintf("/api/packages/%.0f", pid), pkg, 200)
	if list()["sale_usd_minor"] != float64(2000) || list()["price_ready"] != true {
		t.Fatal("manual price changed")
	}
}
