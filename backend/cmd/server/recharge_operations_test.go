package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
)

func TestRechargeOrderLifecycle(t *testing.T) {
	db := walletTestDB(t)
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'owner@test.local','','user'),(2,'manager@test.local','','admin'),(3,'other@test.local','','user');INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,1,'Account','chat@test.local'),(2,3,'Other','other-chat@test.local'),(3,1,'Legacy duplicate','chat@test.local');INSERT INTO wallets(user_id,balance) VALUES(1,1000);`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("test-signing-key")}
	call := func(method, path string, user int64, input any, status int) map[string]any {
		t.Helper()
		b, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, strings.NewReader(string(b)))
		r.Header.Set("Authorization", "Bearer "+s.token(user))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d want %d %s", method, path, w.Code, status, w.Body.String())
		}
		out := map[string]any{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	pkg := map[string]any{"name": "Plus Test", "plan": "plus", "region": "PH", "currency": "PHP", "original_amount_minor": 100000, "sale_usd_minor": 20000, "wallet_tokens": 100, "months": 1, "enabled": true}
	call("POST", "/api/packages", 1, pkg, 403)
	pid := call("POST", "/api/packages", 2, pkg, 200)["id"]
	db.Exec("UPDATE users SET role='admin' WHERE id=1")
	card := call("POST", "/api/bank-cards", 1, map[string]any{"label": "Test", "cardholder": "User", "number": "4242424242424242", "exp_month": 12, "exp_year": 2035}, 201)["card"].(map[string]any)
	cid := int64(card["id"].(float64))
	cp := fmt.Sprintf("/api/bank-cards/%d/ledger", cid)
	call("POST", cp, 1, map[string]any{"kind": "deposit", "amount_usd": "1000.00", "request_key": "operation-deposit-001", "reference": "external-deposit-001"}, 201)
	create := map[string]any{"account_id": 1, "package_id": pid, "period_start": "2030-01-01", "request_key": "operation-create-001"}
	id := int64(call("POST", "/api/orders", 1, create, 201)["id"].(float64))
	op := fmt.Sprintf("/api/orders/%d", id)
	call("GET", op, 3, nil, 403)
	call("POST", "/api/orders", 1, create, 200)
	create["account_id"] = 3
	create["request_key"] = "operation-create-dup"
	call("POST", "/api/orders", 1, create, 409)
	collect := map[string]any{"action": "collect", "method": "wallet", "request_key": "operation-collect-01", "version": 0}
	call("POST", op, 2, collect, 409)
	call("POST", op, 1, collect, 200)
	call("POST", op, 1, collect, 200)
	var balance int64
	db.QueryRow(`SELECT balance FROM wallets WHERE user_id=1`).Scan(&balance)
	if balance != 900 {
		t.Fatal("wallet debit duplicated", balance)
	}
	purchase := map[string]any{"action": "purchase", "card_id": cid, "amount_usd": "150.00", "reference": "official-purchase-1", "evidence": "official receipt", "request_key": "operation-purchase-1", "version": 1}
	call("POST", op, 3, purchase, 403)
	call("POST", op, 2, purchase, 200)
	call("POST", op, 2, purchase, 200)
	verify := map[string]any{"action": "verify", "success": true, "plan": "pro_20x", "period_end": "2030-02-01", "evidence": "official subscription", "request_key": "operation-verify-001", "version": 2}
	call("POST", op, 2, verify, 409)
	verify["plan"] = "plus"
	call("POST", op, 2, verify, 200)
	var plan string
	var end string
	if err = db.QueryRow(`SELECT verified_plan,subscription_ends_at::text FROM chatgpt_accounts WHERE id=1`).Scan(&plan, &end); err != nil || plan != "plus" || end != "2030-02-01" {
		t.Fatal("verification not persisted", err)
	}
	refund := map[string]any{"action": "refund", "amount_usd": "50.00", "reference": "customer-refund-1", "evidence": "confirmed", "reason": "partial refund", "request_key": "operation-refund-001", "version": 3}
	call("POST", op, 3, refund, 403)
	call("POST", op, 2, refund, 200)
	call("POST", op, 2, refund, 200)
	db.QueryRow(`SELECT balance FROM wallets WHERE user_id=1`).Scan(&balance)
	if balance != 925 {
		t.Fatal("partial token refund", balance)
	}
	refund["version"] = 4
	refund["request_key"] = "operation-refund-002"
	refund["amount_usd"] = "151.00"
	call("POST", op, 2, refund, 409)
	refund["amount_usd"] = "150.00"
	refund["reference"] = "customer-refund-2"
	call("POST", op, 2, refund, 200)
	db.QueryRow(`SELECT balance FROM wallets WHERE user_id=1`).Scan(&balance)
	if balance != 1000 {
		t.Fatal("full refund must restore exact tokens", balance)
	}
	create["account_id"] = 1
	create["request_key"] = "operation-create-next"
	create["period_start"] = "2030-02-01"
	call("POST", "/api/orders", 1, create, 201)
	// 套餐编辑不能改变已有订单价格快照。
	pkg["sale_usd_minor"] = 30000
	call("PATCH", fmt.Sprintf("/api/packages/%.0f", pid), 2, pkg, 200)
	detail := call("GET", op, 1, nil, 200)["order"].(map[string]any)
	if detail["sale_usd_minor"] != float64(20000) {
		t.Fatal("historical price changed")
	}
	var sum, cardBalance int64
	db.QueryRow(`SELECT COALESCE(sum(l.amount_usd_minor),0),max(c.balance_usd_minor) FROM bank_card_ledger l JOIN bank_cards c ON c.id=l.card_id WHERE c.id=$1`, cid).Scan(&sum, &cardBalance)
	if sum != cardBalance || sum != 85000 {
		t.Fatal("card balance mismatch", sum, cardBalance)
	}
}

func TestCardAdjustmentsAndStatementImport(t *testing.T) {
	db := walletTestDB(t)
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'finance@test.local','','admin');INSERT INTO bank_cards(id,user_id,label,cardholder,number_ciphertext,number_fingerprint,last4,brand,exp_month,exp_year,balance_usd_minor) VALUES(1,1,'Test','User','encrypted','fingerprint','4242','Visa',12,2035,10000);INSERT INTO bank_card_ledger(card_id,actor_id,request_key,kind,amount_usd_minor,balance_after_usd_minor,external_reference) VALUES(1,1,'initial-card-operation','opening',10000,10000,'opening-1')`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("key")}
	call := func(input any, status int) map[string]any {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest("POST", "/api/card-operations/1", strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+s.token(1))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("got %d want %d: %s", w.Code, status, w.Body.String())
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	hold := map[string]any{"action": "hold", "amount_usd": "90.00", "reference": "hold-1", "request_key": "hold-operation-001"}
	call(hold, 200)
	call(hold, 200)
	fee := map[string]any{"action": "posting", "kind": "fee", "amount_usd": "-20.00", "reference": "fee-1", "notes": "fee receipt", "request_key": "fee-operation-0001"}
	call(fee, 409)
	call(map[string]any{"action": "release", "hold_id": 1, "request_key": "release-operation-1"}, 200)
	call(fee, 200)
	refund := map[string]any{"action": "posting", "kind": "refund", "amount_usd": "10.00", "reference_id": 2, "reference": "refund-1", "notes": "refund receipt", "request_key": "refund-operation-01"}
	call(refund, 200)
	call(refund, 200)
	refund["request_key"] = "refund-operation-02"
	refund["reference"] = "refund-2"
	refund["amount_usd"] = "11.00"
	call(refund, 409)
	statement := "transaction_id,amount_usd,occurred_at,description\nfee-1,-20.00,2030-01-01T00:00:00Z,fee\n"
	call(map[string]any{"action": "import", "csv": statement, "request_key": "import-operation-01"}, 200)
	call(map[string]any{"action": "import", "csv": strings.ReplaceAll(statement, "-20.00", "-30.00"), "request_key": "import-operation-02"}, 409)
	var count int
	db.QueryRow(`SELECT count(*) FROM card_statement_rows`).Scan(&count)
	if count != 1 {
		t.Fatal("import not idempotent")
	}
	var balance, reserved int64
	db.QueryRow(`SELECT balance_usd_minor,reserved_usd_minor FROM bank_cards WHERE id=1`).Scan(&balance, &reserved)
	if balance != 9000 || reserved != 0 {
		t.Fatal("incorrect balance", balance, reserved)
	}
}

func TestCredentialUpgradeRevocationAndLimits(t *testing.T) {
	db := walletTestDB(t)
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(1,'legacy@test.local',$1)`, hash("legacy-password"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("secure-key")}
	old := s.token(1)
	r := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"email":"legacy@test.local","password":"legacy-password"}`))
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("legacy login failed", w.Body.String())
	}
	var stored string
	db.QueryRow(`SELECT password_hash FROM users WHERE id=1`).Scan(&stored)
	if !strings.HasPrefix(stored, "$2") || !passwordMatches(stored, "legacy-password") {
		t.Fatal("password not upgraded")
	}
	r = httptest.NewRequest("GET", "/api/me", nil)
	r.Header.Set("Authorization", "Bearer "+old)
	if _, err = s.auth(r); err == nil {
		t.Fatal("old credential stamp accepted")
	}
	current := s.token(1)
	r = httptest.NewRequest("POST", "/api/logout", nil)
	r.Header.Set("Authorization", "Bearer "+current)
	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatal("logout failed")
	}
	r.Header.Set("Authorization", "Bearer "+current)
	if _, err = s.auth(r); err == nil {
		t.Fatal("logout did not revoke token")
	}
	if err = s.storeEmailCode(context.Background(), "legacy@test.local", "login", hash("123456"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if s.verifyCode("legacy@test.local", "login", "000000") {
			t.Fatal("wrong code accepted")
		}
	}
	if s.verifyCode("legacy@test.local", "login", "123456") {
		t.Fatal("exhausted code accepted")
	}
	for i := 0; i < 3; i++ {
		if !s.allowAttempt(context.Background(), "test-limit", 3, time.Minute) {
			t.Fatal("early rejection")
		}
	}
	if s.allowAttempt(context.Background(), "test-limit", 3, time.Minute) {
		t.Fatal("limit missing")
	}
}

func TestStripeRefundRecoveryAndReplay(t *testing.T) {
	db := walletTestDB(t)
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(1,'customer@test.local','');INSERT INTO wallets(user_id,balance) VALUES(1,30);INSERT INTO topup_orders(order_no,user_id,request_key,amount_minor,tokens,status,payment_intent) VALUES('paid-1',1,'payment-key',10000,100,'paid','pi_1')`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("key")}
	payload, _ := json.Marshal(&stripe.Charge{Amount: 10000, AmountRefunded: 5000, PaymentIntent: &stripe.PaymentIntent{ID: "pi_1"}})
	event := stripe.Event{ID: "evt_refund_1", Type: stripe.EventTypeChargeRefunded, Data: &stripe.EventData{Raw: payload}}
	if err = s.applyPaymentAdjustment(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err = s.applyPaymentAdjustment(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	var balance, reversed int64
	db.QueryRow(`SELECT balance FROM wallets WHERE user_id=1`).Scan(&balance)
	db.QueryRow(`SELECT reversed_tokens FROM topup_orders WHERE order_no='paid-1'`).Scan(&reversed)
	if balance != 0 || reversed != 30 {
		t.Fatal("refund recovery failed")
	}
	var state string
	db.QueryRow(`SELECT status FROM payment_exceptions`).Scan(&state)
	if state != "pending" {
		t.Fatal("shortage must stay pending")
	}
	db.Exec(`UPDATE wallets SET balance=40,updated_at=NOW() WHERE user_id=1`)
	tx, _ := db.Begin()
	if err = reconcileWalletRefund(context.Background(), tx, "paid-1"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT balance FROM wallets WHERE user_id=1`).Scan(&balance)
	if balance != 20 {
		t.Fatal("reconciliation overcharged", balance)
	}
	db.QueryRow(`SELECT status FROM payment_exceptions`).Scan(&state)
	if state != "resolved" {
		t.Fatal("exception not resolved")
	}
	if proportional(1000000000000, 999999999999, 1000000000000) != 999999999999 {
		t.Fatal("proportional overflow")
	}
}
