package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	stripe "github.com/stripe/stripe-go/v82"
)

func TestRestartPreservesToken(t *testing.T) {
	db := walletTestDB(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(42,'restart@test.local','')`); err != nil {
		t.Fatal(err)
	}
	key := []byte("same-persisted-key-before-and-after-restart")
	before := &Server{secret: key}
	token := before.token(42)
	after := &Server{db: db, secret: append([]byte(nil), key...)}
	r := httptest.NewRequest("GET", "/api/me", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	id, err := after.auth(r)
	if err != nil || id != 42 {
		t.Fatal("固定密钥下重启不应使有效 token 失效")
	}
}

func TestPaymentQuantityAndReconciliation(t *testing.T) {
	db := walletTestDB(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(1,'payer@example.com',''),(2,'other@example.com',''),(3,'__superadmin__','')`); err != nil {
		t.Fatal(err)
	}
	provider := &fakeStripe{}
	s := &Server{db: db, secret: []byte("test-key"), stripe: provider, billing: billingConfig{SecretKey: "sk_test_fake", WebhookSecret: "whsec_test", BaseURL: "http://localhost:15680", TokensPerUSD: 1, RenewalCost: 20, RenewalMonths: 1, Price1ID: "price_test_1", Price100ID: "price_test_100"}}
	routes := s.routes()
	call := func(method, path, body string, id int64, status int) map[string]any {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+s.token(id))
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		var data map[string]any
		json.Unmarshal(w.Body.Bytes(), &data)
		return data
	}
	for _, quantity := range []string{"0", "-1", "101", "1.5", `"7"`} {
		call("POST", "/api/wallet/topups", `{"amount_minor":100,"quantity":`+quantity+`,"request_key":"quantity-test-001"}`, 1, 400)
	}
	body := `{"amount_minor":100,"quantity":7,"request_key":"quantity-test-001"}`
	order := call("POST", "/api/wallet/topups", body, 1, 200)["order_no"].(string)
	call("POST", "/api/wallet/topups", body, 1, 200)
	call("POST", "/api/wallet/topups", `{"amount_minor":100,"quantity":8,"request_key":"quantity-test-001"}`, 1, 409)
	if provider.calls != 1 || *provider.params.LineItems[0].Quantity != 7 || *provider.params.LineItems[0].Price != "price_test_1" {
		t.Fatal("数量或支付幂等处理错误")
	}
	if *provider.params.SuccessURL != s.billing.BaseURL+"/admin/wallet?topup=success&order="+order || *provider.params.CancelURL != s.billing.BaseURL+"/admin/wallet?topup=cancelled&order="+order {
		t.Fatal("支付返回地址必须指向钱包并保留订单号")
	}
	wallet := call("GET", "/api/wallet", "", 1, 200)
	orders := wallet["orders"].([]any)
	stored := orders[0].(map[string]any)
	if stored["quantity"] != float64(7) || stored["amount_minor"] != float64(700) || stored["tokens"] != float64(7) || stored["unit_amount_minor"] != float64(100) {
		t.Fatal("订单金额和数量记录错误")
	}
	path := "/api/wallet/topups/" + order + "/sync"
	call("POST", path, "", 2, 404)
	if provider.readCalls != 0 {
		t.Fatal("越权请求不应访问支付供应商")
	}
	provider.session = &stripe.CheckoutSession{ID: "cs_test_aitok", ClientReferenceID: order, Mode: stripe.CheckoutSessionModePayment, PaymentStatus: stripe.CheckoutSessionPaymentStatusUnpaid, Status: stripe.CheckoutSessionStatusOpen, Currency: stripe.CurrencyUSD, AmountSubtotal: 700, AmountTotal: 700, Metadata: map[string]string{"app": "aitok", "order_no": order, "user_id": "1"}}
	call("POST", path, "", 1, 200)
	if call("GET", "/api/wallet", "", 1, 200)["balance"] != float64(0) {
		t.Fatal("未付款订单不得入账")
	}
	provider.session.PaymentStatus = stripe.CheckoutSessionPaymentStatusPaid
	provider.session.Status = stripe.CheckoutSessionStatusComplete
	provider.session.AmountTotal = 1
	call("POST", path, "", 1, 409)
	provider.session.AmountTotal = 700
	call("POST", path, "", 3, 200)
	call("POST", path, "", 1, 200)
	if err := s.applyStripeSession(context.Background(), stripe.EventTypeCheckoutSessionCompleted, provider.session); err != nil {
		t.Fatal(err)
	}
	wallet = call("GET", "/api/wallet", "", 1, 200)
	if wallet["balance"] != float64(7) || len(wallet["ledger"].([]any)) != 1 || wallet["orders"].([]any)[0].(map[string]any)["status"] != "paid" {
		t.Fatal("主动核账与延迟回调应只入账一次")
	}
}
