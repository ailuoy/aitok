package main

import (
	"context"
	"encoding/json"
	stripe "github.com/stripe/stripe-go/v82"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOrderPermissionAndMonthEnd(t *testing.T) {
	for _, pair := range [][2]string{{"2030-01-31", "2030-02-28"}, {"2028-01-31", "2028-02-29"}, {"2030-12-31", "2031-01-31"}} {
		date, _ := time.Parse("2006-01-02", pair[0])
		if actual := addMonthsClamped(date, 1).Format("2006-01-02"); actual != pair[1] {
			t.Fatal(actual)
		}
	}
	db := walletTestDB(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash,role,permissions) VALUES(1,'finance@test.local','','admin',ARRAY['finance']);`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("test")}
	for _, action := range []string{"verify", "assign", "retry", "refund"} {
		r := httptest.NewRequest("POST", "/api/orders/1", strings.NewReader(`{"action":"`+action+`","request_key":"permission-test-key"}`))
		r.Header.Set("Authorization", "Bearer "+s.token(1))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatalf("%s: %d %s", action, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest("GET", "/api/orders", nil)
	r.Header.Set("Authorization", "Bearer "+s.token(1))
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	var data map[string]any
	json.Unmarshal(w.Body.Bytes(), &data)
	if w.Code != 200 || data["can_manage"] != true || data["can_finance"] != true {
		t.Fatal(w.Body.String())
	}
}

type refundTestGateway struct {
	fakeStripe
	result *stripe.Refund
	before func()
	keys   []string
}

func (g *refundTestGateway) Refund(_ context.Context, _ string, _ int64, key string) (*stripe.Refund, error) {
	g.keys = append(g.keys, key)
	if g.before != nil {
		g.before()
	}
	return g.result, nil
}

func TestRefundApprovalCallbackOrderAndRemainingAmount(t *testing.T) {
	db := walletTestDB(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'customer@test.local','','admin'),(2,'finance@test.local','','admin');INSERT INTO wallets(user_id,balance) VALUES(1,100);INSERT INTO topup_orders(order_no,user_id,request_key,amount_minor,tokens,status,payment_intent) VALUES('paid-1',1,'payment-key',10000,100,'paid','pi_1')`); err != nil {
		t.Fatal(err)
	}
	gateway := &refundTestGateway{}
	s := &Server{db: db, secret: []byte("test"), stripe: gateway, billing: billingConfig{SecretKey: "sk_test_mock"}}
	call := func(path, body string, user int64, want int) {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+s.token(user))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	call("/api/wallet/topups/paid-1/refund", `{"amount_usd":"40.00","reason":"test","request_key":"refund-request-key"}`, 1, 201)
	gateway.result = &stripe.Refund{ID: "re_1", Amount: 4000, Status: stripe.RefundStatusSucceeded, PaymentIntent: &stripe.PaymentIntent{ID: "pi_1"}, Metadata: map[string]string{"aitok_request": "request:refund-request-key"}}
	// 模拟渠道回调先提交成功，随后批准接口收到相同成功结果。
	gateway.before = func() {
		raw, _ := json.Marshal(gateway.result)
		if err := s.applyPaymentAdjustment(context.Background(), stripe.Event{ID: "evt-refund-first", Type: stripe.EventTypeRefundUpdated, Data: &stripe.EventData{Raw: raw}}); err != nil {
			t.Fatal(err)
		}
	}
	call("/api/payment-exceptions", `{"id":1,"action":"approve"}`, 2, 200)
	var refunded, balance int64
	var status string
	db.QueryRow(`SELECT refunded_minor FROM topup_orders WHERE order_no='paid-1'`).Scan(&refunded)
	db.QueryRow(`SELECT balance FROM wallets WHERE user_id=1`).Scan(&balance)
	db.QueryRow(`SELECT status FROM payment_exceptions WHERE id=1`).Scan(&status)
	if refunded != 4000 || balance != 60 || status != "resolved" {
		t.Fatal(refunded, balance, status)
	}
	// 申请已完成，不应再次从剩余额度中扣减同一笔申请。
	call("/api/wallet/topups/paid-1/refund", `{"amount_usd":"60.00","reason":"rest","request_key":"refund-request-rest"}`, 1, 201)
	call("/api/wallet/topups/paid-1/refund", `{"amount_usd":"0.01","reason":"too much","request_key":"refund-request-over"}`, 1, 409)
	// 较早的 pending 通知不能把已成功退款降级为等待状态。
	gateway.result.Status = stripe.RefundStatusPending
	raw, _ := json.Marshal(gateway.result)
	if err := s.applyPaymentAdjustment(context.Background(), stripe.Event{ID: "evt-refund-late", Type: stripe.EventTypeRefundUpdated, Data: &stripe.EventData{Raw: raw}}); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT status FROM payment_exceptions WHERE id=1`).Scan(&status)
	if status != "resolved" {
		t.Fatal(status)
	}
}

func TestDisputeLossRecoveryAndLateCreated(t *testing.T) {
	db := walletTestDB(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(1,'dispute@test.local','');INSERT INTO wallets(user_id,balance) VALUES(1,100);INSERT INTO topup_orders(order_no,user_id,request_key,amount_minor,tokens,status,payment_intent) VALUES('dispute-1',1,'payment-key',10000,100,'paid','pi_dispute')`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db}
	for i, state := range []string{"lost", "needs_response", "won"} {
		kind := stripe.EventTypeChargeDisputeClosed
		if i == 1 {
			kind = stripe.EventTypeChargeDisputeCreated
		}
		raw, _ := json.Marshal(map[string]any{"id": "dp_1", "amount": 10000, "payment_intent": "pi_dispute", "status": state})
		if err := s.applyPaymentAdjustment(context.Background(), stripe.Event{ID: "dispute-" + state, Type: kind, Data: &stripe.EventData{Raw: raw}}); err != nil {
			t.Fatal(err)
		}
		var balance int64
		db.QueryRow(`SELECT balance FROM wallets WHERE user_id=1`).Scan(&balance)
		want := int64(0)
		if state == "won" {
			want = 100
		}
		if balance != want {
			t.Fatal(state, balance)
		}
	}
	var pending int
	db.QueryRow(`SELECT count(*) FROM payment_exceptions WHERE status='pending'`).Scan(&pending)
	if pending != 0 {
		t.Fatal("stale dispute pending", pending)
	}
}

func TestPurchaseReversalAndFollowingCycle(t *testing.T) {
	db := walletTestDB(t)
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'operator@test.local','','admin');
 INSERT INTO chatgpt_accounts(id,user_id,label,email,verified_plan,verified_at,subscription_ends_at) VALUES(1,1,'Account','chat@test.local','plus',NOW(),'2030-02-01');
 INSERT INTO bank_cards(id,user_id,label,cardholder,number_ciphertext,number_fingerprint,last4,brand,exp_month,exp_year,balance_usd_minor) VALUES(1,1,'Card','User','encrypted','fingerprint','4242','Visa',12,2035,8500);
 INSERT INTO recharge_orders(id,order_no,user_id,account_id,account_email,package_id,package_snapshot,period_start,period_end,sale_usd_minor,request_key,payment_status,fulfillment_status,card_id,cost_usd_minor,purchase_reference,verified_at) VALUES(1,'order-1',1,1,'chat@test.local',1,'{}','2030-01-01','2030-02-01',2000,'order-key-1','paid','completed',1,1500,'original-purchase',NOW()),(2,'order-2',1,1,'chat@test.local',1,'{}','2030-02-01','2030-03-01',2000,'order-key-2','paid','pending',NULL,0,'',NULL);
 INSERT INTO bank_card_ledger(card_id,actor_id,request_key,kind,amount_usd_minor,balance_after_usd_minor) VALUES(1,1,'opening-test-key','opening',10000,10000);
 INSERT INTO bank_card_ledger(card_id,actor_id,request_key,kind,amount_usd_minor,balance_after_usd_minor,order_id,account_id,account_email,period_start,period_end,external_reference) VALUES(1,1,'purchase-test-key','subscription',-1500,8500,1,1,'chat@test.local','2030-01-01','2030-02-01','original-purchase');`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("test")}
	call := func(path, body string, want int) {
		t.Helper()
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+s.token(1))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	reversal := `{"action":"posting","kind":"reversal","amount_usd":"15.00","reference_id":2,"reference":"reversal-1","notes":"incorrect purchase","request_key":"reversal-test-key"}`
	call("/api/card-operations/1", reversal, 200)
	call("/api/card-operations/1", reversal, 200)
	var cost int64
	var verified bool
	db.QueryRow(`SELECT cost_usd_minor FROM recharge_orders WHERE id=1`).Scan(&cost)
	db.QueryRow(`SELECT verified_at IS NOT NULL FROM chatgpt_accounts WHERE id=1`).Scan(&verified)
	if cost != 0 || verified {
		t.Fatal("reversal did not clear order verification", cost, verified)
	}
	purchase := `{"action":"purchase","card_id":1,"amount_usd":"15.00","reference":"original-purchase","evidence":"receipt","version":1,"request_key":"correct-purchase-key"}`
	call("/api/orders/1", purchase, 409)
	purchase = strings.ReplaceAll(purchase, "original-purchase", "corrected-purchase")
	call("/api/orders/1", purchase, 200)
	purchase = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(purchase, `"version":1`, `"version":0`), "correct-purchase-key", "next-purchase-key"), "corrected-purchase", "next-purchase")
	call("/api/orders/2", purchase, 200)
	var balance, sum int64
	db.QueryRow(`SELECT balance_usd_minor FROM bank_cards WHERE id=1`).Scan(&balance)
	db.QueryRow(`SELECT sum(amount_usd_minor) FROM bank_card_ledger WHERE card_id=1`).Scan(&sum)
	if balance != 7000 || sum != balance {
		t.Fatal(balance, sum)
	}
}
