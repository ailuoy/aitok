package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
)

const walletDebitFixture = `INSERT INTO users(id,email,password_hash,role) VALUES(1,'admin@test.local','','admin'),(2,'payer@test.local','','user'),(3,'other@test.local','','user');
INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,2,'工作账号','account@test.local');
INSERT INTO wallets(user_id,balance) VALUES(2,300),(3,500);
INSERT INTO recharge_orders(id,order_no,user_id,account_id,account_email,package_id,package_snapshot,period_start,period_end,sale_usd_minor,wallet_tokens,payment_method,payment_status,fulfillment_status,received_currency,received_amount_minor,received_usd_minor,received_exchange_rate,received_at,request_key,version)
VALUES(1,'wallet-order-1',2,1,'account@test.local',1,'{}','2030-01-01','2030-02-01',14208,45,'manual','paid','completed','CNY',140000,20881,'{}','2026-01-01','wallet-order-create-1',1);`

func walletDebitTest(t *testing.T) (*Server, func(string, string, int64, any, int) map[string]any) {
	t.Helper()
	db := walletTestDB(t)
	_, err := db.Exec(walletDebitFixture)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("wallet-debit-test"), billing: billingConfig{TokensPerUSD: 1}}
	call := func(method, path string, user int64, input any, status int) map[string]any {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+s.token(user))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		var result map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &result)
		return result
	}
	return s, call
}

func walletDebitInput() orderCommand {
	return orderCommand{Action: "wallet_debit", RequestKey: "wallet-debit-request-1", Version: 1, ExpectedUserID: 2, ExpectedReceivedUSDMinor: 20881, ExpectedTokensPerUSD: 1}
}

func mustWalletSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestOrderWalletDebitPrecisionAndHistory(t *testing.T) {
	s, call := walletDebitTest(t)
	q := call("GET", "/api/orders/1/wallet-quote", 1, nil, 200)
	if q["amount_usd_minor"] != float64(20881) || q["balance_after_minor"] != float64(9119) || q["user_id"] != float64(2) {
		t.Fatal("应使用历史实收折美元，而不是官网成本或套餐代币", q)
	}
	in := walletDebitInput()
	call("POST", "/api/orders/1", 1, in, 200)
	call("POST", "/api/orders/1", 1, in, 200)
	changed := in
	changed.ExpectedReceivedUSDMinor++
	call("POST", "/api/orders/1", 1, changed, 409)
	in.RequestKey = "wallet-debit-request-2"
	in.Version = 2
	call("POST", "/api/orders/1", 1, in, 409)
	var whole, fraction, debits, ledger, events int64
	err := s.db.QueryRow(`SELECT balance,balance_subunit,(SELECT count(*) FROM order_wallet_debits),(SELECT count(*) FROM wallet_ledger),(SELECT count(*) FROM operation_events WHERE action='wallet_debit') FROM wallets WHERE user_id=2`).Scan(&whole, &fraction, &debits, &ledger, &events)
	if err != nil || whole != 91 || fraction != 19 || debits != 1 || ledger != 1 || events != 1 {
		t.Fatal("扣款不精确或重复扣款", whole, fraction, debits, ledger, events, err)
	}
	wallet := call("GET", "/api/wallet", 2, nil, 200)
	entry := wallet["ledger"].([]any)[0].(map[string]any)
	if wallet["balance"] != 91.19 || entry["amount"] != -208.81 || entry["balance_after"] != 91.19 {
		t.Fatal("小数余额或流水不正确", wallet)
	}
	for _, path := range []string{"/api/accounts", "/api/accounts?paged=1"} {
		for _, user := range []int64{1, 2} {
			a := call("GET", path, user, nil, 200)["accounts"].([]any)[0].(map[string]any)
			if a["spent_usd_minor"] != float64(20881) {
				t.Fatal("账号累计消费遗漏", a)
			}
		}
	}
	order := call("GET", "/api/orders/1", 1, nil, 200)["order"].(map[string]any)
	if order["wallet_debit"] == nil {
		t.Fatal("详情缺少已扣款状态")
	}
	order = call("GET", "/api/orders", 1, nil, 200)["orders"].([]any)[0].(map[string]any)
	if order["wallet_debit"] == nil {
		t.Fatal("列表缺少已扣款状态")
	}
	data := call("GET", "/api/consumption-orders?user_id=3&page_size=1", 2, nil, 200)
	if data["total"] != float64(1) || data["balance"] != 91.19 {
		t.Fatal(data)
	}
	row := data["orders"].([]any)[0].(map[string]any)
	for _, field := range []string{"cost_usd_minor", "profit", "actor_id", "card_id", "user_id", "payment_reference", "order_no"} {
		if _, ok := row[field]; ok {
			t.Fatal("用户账单泄露字段", field)
		}
	}
	if call("GET", "/api/consumption-orders?user_id=2", 3, nil, 200)["total"] != float64(0) {
		t.Fatal("可读取其他用户消费")
	}
	call("POST", "/api/orders/1", 1, orderCommand{Action: "discard", RequestKey: "discard-after-wallet", Version: 2, Reason: "结束订单"}, 200)
	if call("GET", "/api/consumption-orders", 2, nil, 200)["total"] != float64(1) {
		t.Fatal("废弃订单丢失真实消费")
	}
	// 换绑不会转移历史消费或泄露给新用户；管理端仍可核对总消费。
	call("PATCH", "/api/accounts/1/owner", 1, map[string]any{"email": "other@test.local", "user_id": 3, "expected_owner_id": 2}, 200)
	for _, path := range []string{"/api/accounts", "/api/accounts?paged=1"} {
		if call("GET", path, 3, nil, 200)["accounts"].([]any)[0].(map[string]any)["spent_usd_minor"] != float64(0) {
			t.Fatal("换绑泄露原用户消费")
		}
		if call("GET", path, 1, nil, 200)["accounts"].([]any)[0].(map[string]any)["spent_usd_minor"] != float64(20881) {
			t.Fatal("管理端累计丢失")
		}
	}
	if call("GET", "/api/consumption-orders", 2, nil, 200)["total"] != float64(1) {
		t.Fatal("换绑丢失原用户账单")
	}
}

func TestOrderWalletDebitGuardsRollback(t *testing.T) {
	for name, query := range map[string]string{
		"余额不足":   `UPDATE wallets SET balance=208,balance_subunit=80 WHERE user_id=2`,
		"账号已换绑":  `UPDATE chatgpt_accounts SET user_id=3 WHERE id=1`,
		"用户停用":   `UPDATE users SET disabled=true WHERE id=2`,
		"账号删除":   `UPDATE chatgpt_accounts SET deleted_at=NOW() WHERE id=1`,
		"钱包删除":   `UPDATE wallets SET deleted_at=NOW() WHERE user_id=2`,
		"实收未知":   `UPDATE recharge_orders SET received_currency='',received_amount_minor=0,received_usd_minor=0,received_exchange_rate=NULL,received_at=NULL WHERE id=1`,
		"已退款":    `UPDATE recharge_orders SET order_status='refunded' WHERE id=1`,
		"尚未开通":   `UPDATE recharge_orders SET fulfillment_status='pending' WHERE id=1`,
		"历史钱包支付": `UPDATE recharge_orders SET payment_method='wallet' WHERE id=1`,
		"实收金额改变": `UPDATE recharge_orders SET received_usd_minor=20882 WHERE id=1`,
		"退款待处理":  `INSERT INTO topup_orders(order_no,user_id,request_key,amount_minor,tokens) VALUES('topup-pending',2,'topup-pending',30000,300); INSERT INTO payment_exceptions(event_id,order_no,kind,status,amount_minor) VALUES('pending-refund','topup-pending','refund','pending',100)`,
	} {
		t.Run(name, func(t *testing.T) {
			s, call := walletDebitTest(t)
			mustWalletSQL(t, s.db, query)
			var before string
			if err := s.db.QueryRow(`SELECT to_jsonb(w)::text FROM wallets w WHERE user_id=2`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			call("POST", "/api/orders/1", 1, walletDebitInput(), 409)
			var after string
			var count int
			err := s.db.QueryRow(`SELECT to_jsonb(w)::text,(SELECT count(*) FROM wallet_ledger)+(SELECT count(*) FROM order_wallet_debits)+(SELECT count(*) FROM operation_events WHERE action='wallet_debit') FROM wallets w WHERE user_id=2`).Scan(&after, &count)
			if err != nil || before != after || count != 0 {
				t.Fatal("失败未整体回滚", err, count)
			}
		})
	}
}

func TestOrderWalletDebitOwnershipRatioAndPermissions(t *testing.T) {
	s, call := walletDebitTest(t)
	call("GET", "/api/orders/1/wallet-quote", 2, nil, 403)
	call("POST", "/api/orders/1", 2, walletDebitInput(), 403)
	call("POST", "/api/orders/1", 3, walletDebitInput(), 403)
	call("POST", "/api/consumption-orders", 2, nil, 403)
	// 原订单属于用户 2，实际扣款使用现在绑定的用户 3。
	mustWalletSQL(t, s.db, `UPDATE chatgpt_accounts SET user_id=3 WHERE id=1`)
	s.billing.TokensPerUSD = 2
	q := call("GET", "/api/orders/1/wallet-quote", 1, nil, 200)
	if q["user_id"] != float64(3) || q["tokens_minor"] != float64(41762) {
		t.Fatal(q)
	}
	in := walletDebitInput()
	in.ExpectedUserID = 3
	call("POST", "/api/orders/1", 1, in, 409)
	in.ExpectedTokensPerUSD = 2
	call("POST", "/api/orders/1", 1, in, 200)
	if call("GET", "/api/wallet", 3, nil, 200)["balance"] != 82.38 || call("GET", "/api/wallet", 2, nil, 200)["balance"] != float64(300) {
		t.Fatal("误扣原订单用户或汇率计算错误")
	}
	if call("GET", "/api/consumption-orders", 2, nil, 200)["total"] != float64(0) {
		t.Fatal("订单归属不应决定付款人")
	}
	// 金融幂等读取完整历史，不能软删除后再次扣款。
	mustWalletSQL(t, s.db, `UPDATE order_wallet_debits SET deleted_at=NOW(),updated_at=NOW(); UPDATE wallet_ledger SET deleted_at=NOW(),updated_at=NOW()`)
	in.Version = 2
	in.RequestKey = "wallet-soft-deleted-retry"
	call("POST", "/api/orders/1", 1, in, 409)
	if call("GET", "/api/consumption-orders", 3, nil, 200)["total"] != float64(0) {
		t.Fatal("默认查询没有过滤软删除")
	}
}

func TestFractionalWalletWithTopupAndRefund(t *testing.T) {
	s, call := walletDebitTest(t)
	call("POST", "/api/orders/1", 1, walletDebitInput(), 200)
	mustWalletSQL(t, s.db, `INSERT INTO topup_orders(order_no,user_id,request_key,amount_minor,tokens,session_id) VALUES('fractional-topup',2,'fractional-topup',10000,100,'cs_fractional')`)
	session := &stripe.CheckoutSession{ID: "cs_fractional", ClientReferenceID: "fractional-topup", Mode: stripe.CheckoutSessionModePayment, PaymentStatus: stripe.CheckoutSessionPaymentStatusPaid, Currency: stripe.CurrencyUSD, AmountSubtotal: 10000, AmountTotal: 10000, Metadata: map[string]string{"app": "aitok", "order_no": "fractional-topup", "user_id": "2"}}
	if err := s.applyStripeSession(context.Background(), stripe.EventTypeCheckoutSessionCompleted, session); err != nil {
		t.Fatal(err)
	}
	check := func(balance float64, kind string) {
		t.Helper()
		w := call("GET", "/api/wallet", 2, nil, 200)
		entry := w["ledger"].([]any)[0].(map[string]any)
		if w["balance"] != balance || entry["balance_after"] != balance || entry["kind"] != kind {
			t.Fatal("整数操作丢失小数快照", w)
		}
	}
	check(191.19, "stripe_topup")
	mustWalletSQL(t, s.db, `UPDATE topup_orders SET refunded_minor=5000 WHERE order_no='fractional-topup'`)
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = reconcileWalletRefund(context.Background(), tx, "fractional-topup"); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	check(141.19, "stripe_refund")
}

func TestOrderWalletTinyAmounts(t *testing.T) {
	for _, amount := range []int64{1, 99, 100, 30000} {
		t.Run(fmt.Sprint(amount), func(t *testing.T) {
			s, call := walletDebitTest(t)
			mustWalletSQL(t, s.db, `UPDATE recharge_orders SET received_usd_minor=$1 WHERE id=1`, amount)
			in := walletDebitInput()
			in.ExpectedReceivedUSDMinor = amount
			call("POST", "/api/orders/1", 1, in, 200)
			var balance, spent int64
			err := s.db.QueryRow(`SELECT balance*100+balance_subunit,(SELECT -amount*100-amount_subunit FROM wallet_ledger LIMIT 1) FROM wallets WHERE user_id=2`).Scan(&balance, &spent)
			if err != nil || balance != 30000-amount || spent != amount {
				t.Fatal(balance, spent, err)
			}
		})
	}
}

func TestOrderWalletConcurrentDebits(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("设置 TEST_DATABASE_URL 验证并发扣款")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*config)
	defer admin.Close()
	for _, scenario := range []string{"同请求重放", "不同请求同订单", "不同订单同钱包"} {
		t.Run(scenario, func(t *testing.T) {
			// 多连接隔离 schema，仅清理本测试创建的对象。
			schema := fmt.Sprintf("aitok_wallet_test_%d", time.Now().UnixNano())
			mustWalletSQL(t, admin, `CREATE SCHEMA `+schema)
			defer admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
			cfg := config.Copy()
			cfg.RuntimeParams["search_path"] = schema
			db := stdlib.OpenDB(*cfg)
			defer db.Close()
			body, err := os.ReadFile("../../migrations/schema.sql")
			if err != nil {
				t.Fatal(err)
			}
			mustWalletSQL(t, db, string(body))
			mustWalletSQL(t, db, walletDebitFixture)
			if scenario == "不同订单同钱包" {
				mustWalletSQL(t, db, `INSERT INTO recharge_orders(id,order_no,user_id,account_id,account_email,package_id,package_snapshot,period_start,period_end,sale_usd_minor,wallet_tokens,payment_method,payment_status,fulfillment_status,received_currency,received_amount_minor,received_usd_minor,received_exchange_rate,received_at,request_key,version) SELECT 2,'wallet-order-2',user_id,account_id,account_email,package_id,package_snapshot,'2030-02-01','2030-03-01',sale_usd_minor,wallet_tokens,payment_method,payment_status,fulfillment_status,received_currency,received_amount_minor,received_usd_minor,received_exchange_rate,received_at,'wallet-create-2',version FROM recharge_orders WHERE id=1`)
			}
			s := &Server{db: db, billing: billingConfig{TokensPerUSD: 1}}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			start := make(chan struct{})
			results := make(chan int, 2)
			for i := 0; i < 2; i++ {
				in := walletDebitInput()
				orderID := int64(1)
				if i == 1 && scenario != "同请求重放" {
					in.RequestKey = "concurrent-second-key"
				}
				if i == 1 && scenario == "不同订单同钱包" {
					orderID = 2
				}
				go func(in orderCommand, id int64) {
					<-start
					body, _ := json.Marshal(in)
					r := httptest.NewRequest("POST", "/", strings.NewReader(string(body))).WithContext(ctx)
					w := httptest.NewRecorder()
					s.orderAction(w, r, 1, id, true)
					results <- w.Code
				}(in, orderID)
			}
			close(start)
			statuses := map[int]int{}
			for i := 0; i < 2; i++ {
				select {
				case status := <-results:
					statuses[status]++
				case <-ctx.Done():
					t.Fatal("并发请求超时")
				}
			}
			wantSuccess := 1
			if scenario == "同请求重放" {
				wantSuccess = 2
			}
			if statuses[200] != wantSuccess || statuses[409] != 2-wantSuccess {
				t.Fatal("并发结果错误", statuses)
			}
			var balance, count int64
			err = db.QueryRow(`SELECT balance*100+balance_subunit,(SELECT count(*) FROM order_wallet_debits) FROM wallets WHERE user_id=2`).Scan(&balance, &count)
			if err != nil || balance != 9119 || count != 1 {
				t.Fatal("重复扣款或余额透支", balance, count, err)
			}
		})
	}
}

func TestOrderWalletWriteFailureRollsBack(t *testing.T) {
	s, call := walletDebitTest(t)
	// 隔离临时表故障注入：消费记录失败时，已执行的钱包扣款及流水必须一并回滚。
	mustWalletSQL(t, s.db, `ALTER TABLE order_wallet_debits ADD CONSTRAINT test_reject_debit CHECK (actor_id<>1)`)
	call("POST", "/api/orders/1", 1, walletDebitInput(), 409)
	var balance, count, version int64
	err := s.db.QueryRow(`SELECT balance*100+balance_subunit,(SELECT count(*) FROM wallet_ledger),(SELECT version FROM recharge_orders WHERE id=1) FROM wallets WHERE user_id=2`).Scan(&balance, &count, &version)
	if err != nil || balance != 30000 || count != 0 || version != 1 {
		t.Fatal("消费记录写入失败未回滚", balance, count, version, err)
	}
}
