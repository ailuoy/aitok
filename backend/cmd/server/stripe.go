package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	stripe "github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/webhook"
)

// 将外部支付调用隔离，测试时不请求真实 Stripe 账户。
type stripeGateway interface {
	Create(context.Context, *stripe.CheckoutSessionCreateParams) (*stripe.CheckoutSession, error)
	RetrievePrice(context.Context, string) (*stripe.Price, error)
	RetrieveSession(context.Context, string) (*stripe.CheckoutSession, error)
}
type stripeClient struct{ client *stripe.Client }

func newStripeGateway(key string) stripeGateway { return &stripeClient{stripe.NewClient(key)} }
func (c *stripeClient) Create(ctx context.Context, p *stripe.CheckoutSessionCreateParams) (*stripe.CheckoutSession, error) {
	return c.client.V1CheckoutSessions.Create(ctx, p)
}

func (c *stripeClient) RetrievePrice(ctx context.Context, id string) (*stripe.Price, error) {
	return c.client.V1Prices.Retrieve(ctx, id, nil)
}

func (c *stripeClient) RetrieveSession(ctx context.Context, id string) (*stripe.CheckoutSession, error) {
	return c.client.V1CheckoutSessions.Retrieve(ctx, id, nil)
}

func validTopupPrice(price *stripe.Price, id string, amount int64) bool {
	return price != nil && price.ID == id && price.Active && price.Type == stripe.PriceTypeOneTime &&
		price.BillingScheme == stripe.PriceBillingSchemePerUnit && price.CustomUnitAmount == nil &&
		price.Currency == stripe.CurrencyUSD && price.UnitAmount == amount
}

func (s *Server) createCheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	id, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return
	}
	if !s.billing.stripeEnabled() {
		reply(w, map[string]string{"error": "Stripe 充值暂未开通"}, 503)
		return
	}
	var in struct {
		AmountMinor int64  `json:"amount_minor"`
		RequestKey  string `json:"request_key"`
		Quantity    *int64 `json:"quantity"`
	}
	if jsonBody(r, &in) != nil || !requestKeyPattern.MatchString(in.RequestKey) {
		reply(w, map[string]string{"error": "充值请求格式错误"}, 400)
		return
	}
	quantity := int64(1)
	if in.Quantity != nil {
		quantity = *in.Quantity
	}
	if quantity < 1 || quantity > 100 {
		reply(w, map[string]string{"error": "充值数量必须是 1 到 100 的整数"}, 400)
		return
	}
	var tokens int64
	var priceID string
	for _, option := range s.billing.options() {
		if option.AmountMinor == in.AmountMinor {
			tokens = option.Tokens
			priceID = option.PriceID
		}
	}
	if tokens == 0 {
		reply(w, map[string]string{"error": "请选择有效充值金额"}, 400)
		return
	}
	tokens *= quantity
	totalAmount := in.AmountMinor * quantity
	orderNo, err := newOrderNo()
	if err != nil {
		reply(w, map[string]string{"error": "创建订单失败"}, 500)
		return
	}
	_, err = s.db.ExecContext(r.Context(), `INSERT INTO topup_orders(order_no,user_id,request_key,amount_minor,tokens,quantity,unit_amount_minor,price_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(user_id,request_key) DO NOTHING`, orderNo, id, in.RequestKey, totalAmount, tokens, quantity, in.AmountMinor, priceID)
	if err != nil {
		reply(w, map[string]string{"error": "创建订单失败"}, 500)
		return
	}
	var amount int64
	var status string
	var checkoutURL sql.NullString
	var storedQuantity, unitAmount int64
	err = s.db.QueryRowContext(r.Context(), `SELECT order_no,amount_minor,tokens,status,checkout_url,quantity,COALESCE(unit_amount_minor,amount_minor),COALESCE(price_id,'') FROM topup_orders WHERE user_id=$1 AND request_key=$2`, id, in.RequestKey).Scan(&orderNo, &amount, &tokens, &status, &checkoutURL, &storedQuantity, &unitAmount, &priceID)
	if err != nil {
		reply(w, map[string]string{"error": "读取订单失败"}, 500)
		return
	}
	if amount != totalAmount || storedQuantity != quantity || unitAmount != in.AmountMinor || status != "pending" {
		reply(w, map[string]string{"error": "订单已处理或金额不匹配，请刷新后重新充值"}, 409)
		return
	}
	if checkoutURL.Valid {
		reply(w, map[string]string{"checkout_url": checkoutURL.String, "order_no": orderNo}, 200)
		return
	}
	price, err := s.stripe.RetrievePrice(r.Context(), priceID)
	if err != nil {
		reply(w, map[string]string{"error": "读取 Stripe 价格失败，请稍后重试"}, 502)
		return
	}
	if !validTopupPrice(price, priceID, unitAmount) {
		reply(w, map[string]string{"error": "Stripe 价格必须是启用的单次 USD 价格，且与充值金额一致"}, 503)
		return
	}
	params := &stripe.CheckoutSessionCreateParams{
		Mode:              stripe.String("payment"),
		SuccessURL:        stripe.String(s.billing.BaseURL + "/wallet?topup=success&order=" + orderNo),
		CancelURL:         stripe.String(s.billing.BaseURL + "/wallet?topup=cancelled&order=" + orderNo),
		ClientReferenceID: stripe.String(orderNo),
		Metadata:          map[string]string{"app": "aitok", "order_no": orderNo, "user_id": strconv.FormatInt(id, 10)},
		LineItems: []*stripe.CheckoutSessionCreateLineItemParams{{
			Quantity: stripe.Int64(storedQuantity),
			Price:    stripe.String(priceID),
		}},
	}
	params.IdempotencyKey = stripe.String("aitok-wallet:" + orderNo)
	session, err := s.stripe.Create(r.Context(), params)
	if err != nil || session == nil || session.ID == "" || session.URL == "" {
		reply(w, map[string]string{"error": "创建 Stripe 支付页面失败，请重试"}, 502)
		return
	}
	_, err = s.db.ExecContext(r.Context(), `UPDATE topup_orders SET session_id=$1,checkout_url=$2 WHERE order_no=$3 AND status='pending'`, session.ID, session.URL, orderNo)
	if err != nil {
		reply(w, map[string]string{"error": "保存支付订单失败，请重试"}, 500)
		return
	}
	reply(w, map[string]string{"checkout_url": session.URL, "order_no": orderNo}, 200)
}

func (s *Server) stripeWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	if s.billing.WebhookSecret == "" {
		w.WriteHeader(503)
		return
	}
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(400)
		return
	}
	// 与 google-maps 一样仅忽略 API 版本差异，签名与时间容差仍严格校验。
	event, err := webhook.ConstructEventWithOptions(payload, r.Header.Get("Stripe-Signature"), s.billing.WebhookSecret, webhook.ConstructEventOptions{IgnoreAPIVersionMismatch: true})
	if err != nil {
		w.WriteHeader(400)
		return
	}
	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted, stripe.EventTypeCheckoutSessionAsyncPaymentSucceeded, stripe.EventTypeCheckoutSessionExpired, stripe.EventTypeCheckoutSessionAsyncPaymentFailed:
		var session stripe.CheckoutSession
		if err = json.Unmarshal(event.Data.Raw, &session); err != nil {
			w.WriteHeader(400)
			return
		}
		if session.Metadata["app"] != "aitok" {
			w.WriteHeader(200)
			return
		}
		if event.Type == stripe.EventTypeCheckoutSessionCompleted && session.PaymentStatus != stripe.CheckoutSessionPaymentStatusPaid {
			w.WriteHeader(200)
			return
		}
		err = s.applyStripeSession(r.Context(), event.Type, &session)
		if err != nil {
			reply(w, map[string]string{"error": "订单校验或入账失败"}, 500)
			return
		}
	}
	w.WriteHeader(200)
}

func (s *Server) applyStripeSession(ctx context.Context, eventType stripe.EventType, session *stripe.CheckoutSession) error {
	if session == nil || session.Metadata["app"] != "aitok" || session.ClientReferenceID == "" || session.ID == "" || session.Metadata["order_no"] != session.ClientReferenceID {
		return errors.New("missing order reference")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id, amount, tokens int64
	var status, currency string
	var sessionID sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT user_id,amount_minor,tokens,status,currency,session_id FROM topup_orders WHERE order_no=$1 FOR UPDATE`, session.ClientReferenceID).Scan(&id, &amount, &tokens, &status, &currency, &sessionID)
	if err != nil {
		return err
	}
	if !sessionID.Valid || sessionID.String != session.ID || session.Metadata["user_id"] != strconv.FormatInt(id, 10) {
		return errors.New("session mismatch")
	}
	// 订单状态与唯一流水标识共同防止不同事件对同一支付重复入账。
	if status == "paid" {
		return nil
	}
	if eventType == stripe.EventTypeCheckoutSessionExpired || eventType == stripe.EventTypeCheckoutSessionAsyncPaymentFailed {
		next := "expired"
		if eventType == stripe.EventTypeCheckoutSessionAsyncPaymentFailed {
			next = "failed"
		}
		_, err = tx.ExecContext(ctx, `UPDATE topup_orders SET status=$1 WHERE order_no=$2 AND status='pending'`, next, session.ClientReferenceID)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	if status != "pending" || session.Mode != stripe.CheckoutSessionModePayment || session.PaymentStatus != stripe.CheckoutSessionPaymentStatusPaid || string(session.Currency) != currency || session.AmountTotal != amount || session.AmountSubtotal != amount {
		return errors.New("payment amount, currency or status mismatch")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO wallets(user_id) VALUES($1) ON CONFLICT DO NOTHING`, id); err != nil {
		return err
	}
	var balance int64
	err = tx.QueryRowContext(ctx, `UPDATE wallets SET balance=balance+$1 WHERE user_id=$2 RETURNING balance`, tokens, id).Scan(&balance)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO wallet_ledger(user_id,amount,balance_after,kind,reference,description) VALUES($1,$2,$3,'stripe_topup',$4,'Stripe 充值')`, id, tokens, balance, "stripe:"+session.ClientReferenceID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE topup_orders SET status='paid' WHERE order_no=$1`, session.ClientReferenceID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
