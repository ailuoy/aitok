package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
)

func TestSessionEncryption(t *testing.T) {
	key := []byte(strings.Repeat("x", 32))
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	raw := `{"accessToken":"sensitive-test-token"}`
	encoded, err := encryptSession(raw)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := encryptSession(raw)
	if encoded == second || strings.Contains(encoded, "sensitive") {
		t.Fatal("加密应使用随机 nonce")
	}
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	data, _ := base64.StdEncoding.DecodeString(encoded)
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil || string(plain) != raw {
		t.Fatal("Session 加密内容不完整")
	}

}

type fakeStripe struct {
	params    *stripe.CheckoutSessionCreateParams
	calls     int
	session   *stripe.CheckoutSession
	readCalls int
}

func (f *fakeStripe) Create(_ context.Context, p *stripe.CheckoutSessionCreateParams) (*stripe.CheckoutSession, error) {
	f.params = p
	f.calls++
	return &stripe.CheckoutSession{ID: "cs_test_aitok", URL: "https://checkout.stripe.com/test"}, nil
}

func (f *fakeStripe) RetrievePrice(_ context.Context, id string) (*stripe.Price, error) {
	amount := int64(100)
	if id == "price_test_100" {
		amount = 10000
	}
	return &stripe.Price{ID: id, Active: true, Currency: stripe.CurrencyUSD, Type: stripe.PriceTypeOneTime, BillingScheme: stripe.PriceBillingSchemePerUnit, UnitAmount: amount}, nil
}

func (f *fakeStripe) RetrieveSession(_ context.Context, id string) (*stripe.CheckoutSession, error) {
	f.readCalls++
	return f.session, nil
}

// 临时表遮蔽同名业务表，不改变已有用户、余额和账号。
func walletTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("设置 TEST_DATABASE_URL 运行钱包集成测试")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	files, err := filepath.Glob("../../migrations/[0-9][0-9][0-9]_*.sql")
	if err != nil || len(files) == 0 {
		t.Fatal("未找到编号迁移", err)
	}
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		query := temporarySchemaSQL(string(body))
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestWalletAndRenewalIntegration(t *testing.T) {
	db := walletTestDB(t)
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(1,'owner@example.com',''),(2,'other@example.com',''),(3,'__superadmin__','')`)
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeStripe{}
	s := &Server{db: db, secret: []byte("wallet-test-secret"), admin: adminConfig{"admin", "123456"}, billing: billingConfig{SecretKey: "sk_test_fake", WebhookSecret: "whsec_test", BaseURL: "http://localhost:15680", TokensPerUSD: 1, RenewalCost: 45, RenewalMonths: 1, Price1ID: "price_test_1", Price100ID: "price_test_100"}, stripe: provider}
	routes := s.routes()
	call := func(method, path string, body any, user int64, status int) map[string]any {
		t.Helper()
		encoded, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
		req.Header.Set("Authorization", "Bearer "+s.token(user))
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s %s: %d，预期 %d；%s", method, path, w.Code, status, w.Body.String())
		}
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		return result
	}
	accountBody := map[string]any{"label": "工作账号", "email": "chat@example.com", "session_json": `{"accessToken":"secret-value"}`}
	a := call("POST", "/api/accounts", accountBody, 1, 201)["account"].(map[string]any)
	aid := int64(a["id"].(float64))
	path := fmt.Sprintf("/api/accounts/%d", aid)
	var stored string
	db.QueryRow(`SELECT session_ciphertext FROM chatgpt_accounts WHERE id=$1`, aid).Scan(&stored)
	if stored == "" || strings.Contains(stored, "secret-value") {
		t.Fatal("Session 未加密")
	}
	accountBody["session_json"] = "[]"
	call("POST", "/api/accounts", accountBody, 1, 400)
	list := call("GET", "/api/accounts", nil, 2, 200)["accounts"].([]any)
	if len(list) != 0 {
		t.Fatal("普通用户看到了他人账号")
	}
	list = call("GET", "/api/accounts", nil, 3, 200)["accounts"].([]any)
	if len(list) != 1 {
		t.Fatal("超管应能查看全部账号")
	}
	encodedList, _ := json.Marshal(list)
	if strings.Contains(string(encodedList), "secret-value") || strings.Contains(string(encodedList), "session_ciphertext") {
		t.Fatal("列表泄露 Session")
	}
	call("PATCH", path+"/renewal-date", map[string]string{"renewal_date": "2030-01-31"}, 1, 403)
	call("PATCH", path+"/renewal-date", map[string]string{"renewal_date": "2030-02-30"}, 3, 400)
	checkAccount := expectTimestampUpdate(t, db, "chatgpt_accounts", "id=$1", aid)
	call("PATCH", path+"/renewal-date", map[string]string{"renewal_date": "2030-01-31"}, 3, 200)
	checkAccount()
	var auditCount int
	db.QueryRow(`SELECT count(*) FROM renewal_date_audit`).Scan(&auditCount)
	if auditCount != 1 {
		t.Fatal("管理员日期修改未记录")
	}
	renew := map[string]any{"request_key": "renewal-request-001", "expected_cost": 45, "expected_months": 1}
	call("POST", path+"/renew", renew, 2, 403)
	db.Exec("UPDATE users SET role='admin' WHERE id=1")
	call("POST", path+"/renew", renew, 1, 410)
	topup := map[string]any{"amount_minor": 10000, "request_key": "topup-request-001"}
	result := call("POST", "/api/wallet/topups", topup, 1, 200)
	orderNo := result["order_no"].(string)
	call("POST", "/api/wallet/topups", topup, 1, 200)
	if provider.calls != 1 || provider.params.LineItems[0].PriceData != nil || *provider.params.LineItems[0].Price != "price_test_100" || *provider.params.Mode != "payment" {
		t.Fatal("重复创建 Checkout 或价格不正确")
	}
	if *provider.params.IdempotencyKey != "aitok-wallet:"+orderNo || provider.params.Metadata["user_id"] != "1" {
		t.Fatal("缺少支付幂等键或归属")
	}
	wallet := call("GET", "/api/wallet", nil, 1, 200)
	if wallet["balance"] != float64(0) {
		t.Fatal("未支付订单提前入账")
	}
	session := &stripe.CheckoutSession{ID: "cs_test_aitok", ClientReferenceID: orderNo, Mode: stripe.CheckoutSessionModePayment, PaymentStatus: stripe.CheckoutSessionPaymentStatusPaid, Currency: stripe.CurrencyUSD, AmountSubtotal: 10000, AmountTotal: 10000, Metadata: map[string]string{"app": "aitok", "order_no": orderNo, "user_id": "1"}}
	session.AmountTotal = 1
	if s.applyStripeSession(context.Background(), stripe.EventTypeCheckoutSessionCompleted, session) == nil {
		t.Fatal("篡改金额应拒绝入账")
	}
	session.AmountTotal = 10000
	session.Currency = stripe.CurrencyEUR
	if s.applyStripeSession(context.Background(), stripe.EventTypeCheckoutSessionCompleted, session) == nil {
		t.Fatal("币种错误应拒绝入账")
	}
	session.Currency = stripe.CurrencyUSD
	// 验证 Webhook 实际路由的签名校验和业务处理。
	event := map[string]any{"id": "evt_aitok_test", "object": "event", "type": "checkout.session.completed", "created": time.Now().Unix(), "data": map[string]any{"object": session}}
	payload, _ := json.Marshal(event)
	webhookRequest := func(signature string) int {
		request := httptest.NewRequest("POST", "/api/stripe/webhook", strings.NewReader(string(payload)))
		request.Header.Set("Stripe-Signature", signature)
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, request)
		return w.Code
	}
	if webhookRequest("invalid") != 400 {
		t.Fatal("伪造 Webhook 未拒绝")
	}
	checkWallet := expectTimestampUpdate(t, db, "wallets", "user_id=1")
	checkOrder := expectTimestampUpdate(t, db, "topup_orders", "order_no=$1", orderNo)
	stamp := fmt.Sprint(time.Now().Unix())
	mac := hmac.New(sha256.New, []byte(s.billing.WebhookSecret))
	mac.Write([]byte(stamp + "." + string(payload)))
	signature := "t=" + stamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
	if webhookRequest(signature) != 200 || webhookRequest(signature) != 200 {
		t.Fatal("合法 Webhook 或重复回调失败")
	}
	checkWallet()
	checkOrder()
	// 不同事件 ID 或异步成功事件仍不能对同一订单重复入账。
	if err := s.applyStripeSession(context.Background(), stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded, session); err != nil {
		t.Fatal(err)
	}
	wallet = call("GET", "/api/wallet", nil, 1, 200)
	if wallet["balance"] != float64(100) || len(wallet["ledger"].([]any)) != 1 {
		t.Fatal("充值未正确幂等入账")
	}
	db.Exec("UPDATE users SET role='admin' WHERE id=1")
	call("POST", path+"/renew", renew, 1, 410)
	wallet = call("GET", "/api/wallet", nil, 1, 200)
	if wallet["balance"] != float64(100) || len(wallet["ledger"].([]any)) != 1 {
		t.Fatal("停用续订后不得扣除代币")
	}
	// 历史扣款记录仍可读取，删除账号不影响已保存的名称快照。
	if _, err = db.Exec(`INSERT INTO account_renewals(user_id,request_key,account_id,tokens,renewal_date,account_label,months) VALUES(1,'legacy-renewal',$1,45,'2030-02-28','工作账号',1)`, aid); err != nil {
		t.Fatal(err)
	}
	call("DELETE", path, nil, 1, 204)
	wallet = call("GET", "/api/wallet", nil, 1, 200)
	if wallet["renewals"].([]any)[0].(map[string]any)["account_label"] != "工作账号" {
		t.Fatal("删除账号不应丢失扣款记录")
	}
}
