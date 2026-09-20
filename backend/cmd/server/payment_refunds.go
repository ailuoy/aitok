package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
)

type refundLookup interface {
	FindRefund(context.Context, string, string) (*stripe.Refund, error)
}

func (c *stripeClient) FindRefund(ctx context.Context, intent, requestID string) (*stripe.Refund, error) {
	p := &stripe.RefundListParams{PaymentIntent: stripe.String(intent)}
	var found *stripe.Refund
	var listErr error
	c.client.V1Refunds.List(ctx, p)(func(result *stripe.Refund, err error) bool {
		if err != nil {
			listErr = err
			return false
		}
		if result.Metadata["aitok_request"] == requestID {
			found = result
			return false
		}
		return true
	})
	return found, listErr
}

type refundReader interface {
	RetrieveRefund(context.Context, string) (*stripe.Refund, error)
}

func (c *stripeClient) RetrieveRefund(ctx context.Context, id string) (*stripe.Refund, error) {
	return c.client.V1Refunds.Retrieve(ctx, id, nil)
}

type refundGateway interface {
	Refund(context.Context, string, int64, string) (*stripe.Refund, error)
}

func (c *stripeClient) Refund(ctx context.Context, intent string, amount int64, key string) (*stripe.Refund, error) {
	p := &stripe.RefundCreateParams{PaymentIntent: stripe.String(intent), Amount: stripe.Int64(amount)}
	p.SetIdempotencyKey(key)
	p.AddMetadata("aitok_request", strings.TrimPrefix(key, "aitok-refund:"))
	return c.client.V1Refunds.Create(ctx, p)
}

// 使用整数比例累计计算，部分退款不累计舍入误差，避免大额乘法溢出。
func proportional(total, part, whole int64) int64 {
	if total <= 0 || part <= 0 || whole <= 0 {
		return 0
	}
	if part >= whole {
		return total
	}
	return new(big.Int).Quo(new(big.Int).Mul(big.NewInt(total), big.NewInt(part)), big.NewInt(whole)).Int64()
}

func (s *Server) applyPaymentAdjustment(ctx context.Context, event stripe.Event) error {
	var intent string
	var refundResults []*stripe.Refund
	var refunded, charged int64
	kind := "refund"
	closed := false
	won := false
	switch event.Type {
	case stripe.EventTypeRefundUpdated, stripe.EventTypeRefundCreated, stripe.EventTypeRefundFailed:
		var result stripe.Refund
		if json.Unmarshal(event.Data.Raw, &result) != nil {
			return fmt.Errorf("invalid refund")
		}
		if result.PaymentIntent != nil {
			intent = result.PaymentIntent.ID
		}
		kind = "refund_result"
		refundResults = []*stripe.Refund{&result}
	case stripe.EventTypeChargeRefunded:
		var charge stripe.Charge
		if json.Unmarshal(event.Data.Raw, &charge) != nil {
			return fmt.Errorf("invalid charge")
		}
		if charge.PaymentIntent != nil {
			intent = charge.PaymentIntent.ID
		}
		if charge.Refunds != nil {
			refundResults = charge.Refunds.Data
		}
		refunded = charge.AmountRefunded
		charged = charge.Amount
	case stripe.EventTypeChargeDisputeCreated, stripe.EventTypeChargeDisputeClosed:
		var dispute stripe.Dispute
		if json.Unmarshal(event.Data.Raw, &dispute) != nil {
			return fmt.Errorf("invalid dispute")
		}
		if dispute.PaymentIntent != nil {
			intent = dispute.PaymentIntent.ID
		}
		refunded = dispute.Amount
		kind = "dispute"
		closed = event.Type == stripe.EventTypeChargeDisputeClosed
		won = string(dispute.Status) == "won"
	default:
		return nil
	}
	if intent == "" {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var order, status string
	var user, amount, prior int64
	// 支付审计覆盖历史订单，软删除不允许绕过退款/拒付入账。
	err = tx.QueryRowContext(ctx, `SELECT order_no,user_id,amount_minor,refunded_minor,status FROM topup_orders WHERE payment_intent=$1 FOR UPDATE`, intent).Scan(&order, &user, &amount, &prior, &status)
	if err == sql.ErrNoRows {
		return fmt.Errorf("payment identity not yet recorded; retry after checkout synchronization")
	}
	if err != nil {
		return err
	}
	if refunded < 0 || refunded > amount || (charged > 0 && charged != amount) || status != "paid" {
		return fmt.Errorf("adjustment mismatch")
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM payment_exceptions WHERE event_id=$1)`, event.ID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	if kind == "refund" {
		if refunded > prior {
			_, err = tx.ExecContext(ctx, `UPDATE topup_orders SET refunded_minor=$2,updated_at=NOW() WHERE order_no=$1`, order, refunded)
		}
	} else if kind == "dispute" {
		next := "open"
		if closed {
			next = "lost"
			if won {
				next = "won"
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE topup_orders SET dispute_status=CASE WHEN dispute_status IN ('won','lost') AND $2='open' THEN dispute_status ELSE $2 END,updated_at=NOW() WHERE order_no=$1`, order, next)
	}
	if err != nil {
		return err
	}
	detail := "支付渠道退款，待回收对应代币"
	if kind == "dispute" {
		detail = "支付发生拒付，请核对支付渠道；争议期间停止新增钱包消费"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO payment_exceptions(event_id,order_no,kind,amount_minor,detail) VALUES($1,$2,$3,$4,$5)`, event.ID, order, kind, refunded, detail)
	if err != nil {
		return err
	}
	for _, result := range refundResults {
		if err = applyRefundResult(ctx, tx, order, result); err != nil {
			return err
		}
	}
	if kind == "refund_result" {
		_, err = tx.ExecContext(ctx, `UPDATE payment_exceptions SET status='resolved',updated_at=NOW() WHERE event_id=$1`, event.ID)
		if err != nil {
			return err
		}
	}
	var currentDispute string
	if err = tx.QueryRowContext(ctx, `SELECT dispute_status FROM topup_orders WHERE order_no=$1`, order).Scan(&currentDispute); err != nil {
		return err
	}
	if kind == "refund" || (kind == "dispute" && currentDispute == "lost") {
		if err = reconcileWalletRefund(ctx, tx, order); err != nil {
			return err
		}
	}
	if kind == "dispute" && currentDispute == "won" {
		if err = reconcileWalletRefund(ctx, tx, order); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE payment_exceptions SET status='resolved',updated_at=NOW() WHERE order_no=$1 AND kind='dispute'`, order)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func reconcileWalletRefund(ctx context.Context, tx *sql.Tx, order string) error {
	var user, tokens, amount, refunded, reversed, disputed int64
	var disputeStatus string
	err := tx.QueryRowContext(ctx, `SELECT user_id,tokens,amount_minor,refunded_minor,reversed_tokens,dispute_status FROM topup_orders WHERE order_no=$1 FOR UPDATE`, order).Scan(&user, &tokens, &amount, &refunded, &reversed, &disputeStatus)
	if err != nil {
		return err
	}
	if disputeStatus == "lost" {
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(amount_minor),0) FROM payment_exceptions WHERE order_no=$1 AND kind='dispute'`, order).Scan(&disputed)
		if err != nil {
			return err
		}
	}
	target := proportional(tokens, refunded+disputed, amount)
	delta := target - reversed
	if delta > 0 {
		var balance int64
		err = tx.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE user_id=$1 FOR UPDATE`, user).Scan(&balance)
		if err != nil {
			return err
		}
		take := delta
		if take > balance {
			take = balance
		}
		if take > 0 {
			_, err = tx.ExecContext(ctx, `UPDATE wallets SET balance=balance-$2,updated_at=NOW() WHERE user_id=$1`, user, take)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO wallet_ledger(user_id,amount,balance_after,kind,reference,description,balance_after_subunit) VALUES($1,$2,$3,'stripe_refund',$4,'Stripe 退款代币回收',(SELECT balance_subunit FROM wallets WHERE user_id=$1))`, user, -take, balance-take, "stripe-refund:"+order+":"+strconv.FormatInt(reversed+take, 10))
			if err != nil {
				return err
			}
			reversed += take
			_, err = tx.ExecContext(ctx, `UPDATE topup_orders SET reversed_tokens=$2,updated_at=NOW() WHERE order_no=$1`, order, reversed)
			if err != nil {
				return err
			}
		}
	}
	if delta < 0 {
		var balance int64
		err = tx.QueryRowContext(ctx, `UPDATE wallets SET balance=balance+$2,updated_at=NOW() WHERE user_id=$1 RETURNING balance`, user, -delta).Scan(&balance)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO wallet_ledger(user_id,amount,balance_after,kind,reference,description,balance_after_subunit) VALUES($1,$2,$3,'dispute_reversal',$4,'争议胜诉返还代币',(SELECT balance_subunit FROM wallets WHERE user_id=$1))`, user, -delta, balance, "dispute-return:"+order+":"+eventKey())
		if err != nil {
			return err
		}
		reversed = target
		_, err = tx.ExecContext(ctx, `UPDATE topup_orders SET reversed_tokens=$2,updated_at=NOW() WHERE order_no=$1`, order, reversed)
		if err != nil {
			return err
		}
	}
	status := "pending"
	detail := "余额不足，剩余待回收代币 " + strconv.FormatInt(target-reversed, 10)
	if target <= reversed {
		status = "resolved"
		detail = "对应代币已回收"
	}
	_, err = tx.ExecContext(ctx, `UPDATE payment_exceptions SET status=$2,detail=$3,updated_at=NOW() WHERE order_no=$1 AND (kind='refund' OR (kind='dispute' AND $4='lost'))`, order, status, detail, disputeStatus)
	return err
}

func (s *Server) requestTopupRefund(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	user, err := s.auth(r)
	if err != nil {
		w.WriteHeader(401)
		return
	}
	order := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/wallet/topups/"), "/refund")
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var in struct {
		Amount string `json:"amount_usd"`
		Reason string `json:"reason"`
		Key    string `json:"request_key"`
	}
	if jsonBody(r, &in) != nil || !ledgerKeyPattern.MatchString(in.Key) || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 1000 {
		w.WriteHeader(400)
		return
	}
	amount, ok := parseCardUSD(in.Amount)
	if !ok {
		w.WriteHeader(400)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	var paid, refunded int64
	var intent string
	err = tx.QueryRowContext(r.Context(), `SELECT amount_minor,refunded_minor,payment_intent FROM topup_orders WHERE order_no=$1 AND user_id=$2 AND status='paid' AND deleted_at IS NULL FOR UPDATE`, order, user).Scan(&paid, &refunded, &intent)
	if err != nil {
		operationError(w, err)
		return
	}
	if intent == "" {
		reply(w, map[string]string{"error": "历史订单缺少支付标识，请先由管理员核对 Stripe 账单"}, 409)
		return
	}
	var oldAmount int64
	var oldReason string
	err = tx.QueryRowContext(r.Context(), `SELECT amount_minor,detail FROM payment_exceptions WHERE event_id=$1 AND order_no=$2`, "request:"+in.Key, order).Scan(&oldAmount, &oldReason)
	if err == nil {
		if oldAmount != amount || oldReason != in.Reason {
			operationError(w, fmt.Errorf("request mismatch"))
			return
		}
		reply(w, map[string]bool{"ok": true}, 200)
		return
	}
	if err != sql.ErrNoRows {
		operationError(w, err)
		return
	}
	var pending int64
	err = tx.QueryRowContext(r.Context(), `SELECT COALESCE(sum(amount_minor),0) FROM payment_exceptions WHERE order_no=$1 AND kind='refund_request' AND status IN ('pending','processing','submitted')`, order).Scan(&pending)
	if err != nil || amount > paid-refunded-pending {
		reply(w, map[string]string{"error": "退款金额超过可退金额，或已有申请待处理"}, 409)
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO payment_exceptions(event_id,order_no,kind,amount_minor,detail) VALUES($1,$2,'refund_request',$3,$4)`, "request:"+in.Key, order, amount, in.Reason)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]bool{"ok": true}, 201)
}

func (s *Server) paymentExceptions(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requirePermission(w, r, "refunds")
	if !ok {
		return
	}
	if r.Method == "GET" {
		p, size, valid := pageParameters(r)
		if !valid {
			w.WriteHeader(400)
			return
		}
		var total int
		if err := s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM payment_exceptions WHERE deleted_at IS NULL`).Scan(&total); err != nil {
			operationError(w, err)
			return
		}
		rows, err := jsonRows(r.Context(), s.db, `SELECT to_jsonb(e) FROM payment_exceptions e WHERE deleted_at IS NULL ORDER BY id DESC LIMIT $2 OFFSET $1`, (p-1)*size, size)
		if err != nil {
			operationError(w, err)
			return
		}
		reply(w, map[string]any{"exceptions": rows, "total": total, "page": p, "page_size": size}, 200)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var in struct {
		ID     int64  `json:"id"`
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	if jsonBody(r, &in) != nil || in.ID < 1 || len(in.Reason) > 1000 {
		w.WriteHeader(400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	var order string
	err = tx.QueryRowContext(ctx, `SELECT order_no FROM payment_exceptions WHERE id=$1`, in.ID).Scan(&order)
	if err != nil {
		operationError(w, err)
		return
	}
	var intent string
	var amount, refunded int64
	err = tx.QueryRowContext(ctx, `SELECT payment_intent,amount_minor,refunded_minor FROM topup_orders WHERE order_no=$1 FOR UPDATE`, order).Scan(&intent, &amount, &refunded)
	if err != nil {
		operationError(w, err)
		return
	}
	var kind, status, eventID string
	var requested int64
	var createdAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT kind,status,event_id,amount_minor,created_at FROM payment_exceptions WHERE id=$1 FOR UPDATE`, in.ID).Scan(&kind, &status, &eventID, &requested, &createdAt)
	if err != nil {
		operationError(w, err)
		return
	}
	if in.Action == "reconcile" && (kind == "refund" || kind == "dispute") {
		err = reconcileWalletRefund(ctx, tx, order)
	} else if in.Action == "reject" && kind == "refund_request" && status == "pending" && strings.TrimSpace(in.Reason) != "" {
		_, err = tx.ExecContext(ctx, `UPDATE payment_exceptions SET status='rejected',updated_at=NOW() WHERE id=$1`, in.ID)
	} else if in.Action == "approve" && kind == "refund_request" && (status == "pending" || status == "processing" || status == "submitted") {
		gateway, ok := s.stripe.(refundGateway)
		if !ok || intent == "" || s.billing.SecretKey == "" {
			reply(w, map[string]string{"error": "退款渠道未配置或历史订单缺少支付标识"}, 503)
			return
		}
		if status == "pending" && requested > amount-refunded {
			operationError(w, fmt.Errorf("refund limit"))
			return
		}
		// 先持久化处理中状态；超时后只能使用同一渠道幂等键重试。
		_, err = tx.ExecContext(ctx, `UPDATE payment_exceptions SET status='processing',updated_at=NOW() WHERE id=$1`, in.ID)
		if err == nil {
			err = tx.Commit()
		}
		if err != nil {
			operationError(w, err)
			return
		}
		var result *stripe.Refund
		var e error
		if status != "pending" {
			if lookup, supported := s.stripe.(refundLookup); supported {
				result, e = lookup.FindRefund(ctx, intent, eventID)
			}
			if e != nil {
				reply(w, map[string]string{"error": "渠道退款记录暂时无法查询，请重试原申请"}, 502)
				return
			}
			if result == nil && time.Since(createdAt) > 23*time.Hour {
				reply(w, map[string]string{"error": "此退款结果已超出安全重试窗口，请先在 Stripe 核对并等待退款回调，勿重复创建退款"}, 409)
				return
			}
		}
		if result == nil {
			result, e = gateway.Refund(ctx, intent, requested, "aitok-refund:"+eventID)
		}
		if e == nil && result != nil && result.Status != stripe.RefundStatusSucceeded {
			if reader, supported := s.stripe.(refundReader); supported {
				result, e = reader.RetrieveRefund(ctx, result.ID)
			}
		}
		if e != nil || result == nil {
			reply(w, map[string]string{"error": "退款结果待确认，请使用此申请重试，不要另建退款"}, 502)
			return
		}
		tx, err = s.db.BeginTx(ctx, nil)
		if err != nil {
			operationError(w, err)
			return
		}
		defer tx.Rollback()
		// 回调可能先于 HTTP 响应到达；以同一退款 ID 合并，不覆盖已经确认的结果。
		var locked string
		err = tx.QueryRowContext(ctx, `SELECT order_no FROM topup_orders WHERE order_no=$1 FOR UPDATE`, order).Scan(&locked)
		if result.Metadata == nil {
			result.Metadata = map[string]string{}
		}
		result.Metadata["aitok_request"] = eventID
		if err == nil {
			err = applyRefundResult(ctx, tx, order, result)
		}
	} else {
		reply(w, map[string]string{"error": "当前记录不支持此操作"}, 409)
		return
	}
	if err == nil {
		err = recordEvent(ctx, tx, user, in.ID, "payment", in.Action, eventKey(), map[string]any{"status": status}, map[string]any{"reason": in.Reason})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]bool{"ok": true}, 200)
}

// 退款 ID 独立去重，同时兼容 webhook 先到、响应重放及渠道部分退款。
// 成功退款明细之和与 charge 的累计退款取较大值，防止重复加总。
func applyRefundResult(ctx context.Context, tx *sql.Tx, order string, result *stripe.Refund) error {
	if result == nil || result.ID == "" || result.Amount <= 0 {
		return fmt.Errorf("invalid refund result")
	}
	var paid int64
	var intent string
	if err := tx.QueryRowContext(ctx, `SELECT amount_minor,payment_intent FROM topup_orders WHERE order_no=$1`, order).Scan(&paid, &intent); err != nil {
		return err
	}
	if result.Amount > paid || (result.PaymentIntent != nil && result.PaymentIntent.ID != intent) {
		return fmt.Errorf("refund mismatch")
	}
	status := "submitted"
	if result.Status == stripe.RefundStatusSucceeded {
		status = "resolved"
	}
	if result.Status == stripe.RefundStatusFailed || result.Status == stripe.RefundStatusCanceled {
		status = "failed"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO payment_exceptions(event_id,order_no,kind,amount_minor,status,detail) VALUES($1,$2,'refund_result',$3,$4,'渠道退款结果') ON CONFLICT(event_id) DO UPDATE SET status=CASE WHEN payment_exceptions.status IN ('resolved','failed') THEN payment_exceptions.status ELSE EXCLUDED.status END,updated_at=NOW()`, "refund-result:"+result.ID, order, result.Amount, status)
	if err != nil {
		return err
	}
	var confirmed string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM payment_exceptions WHERE event_id=$1`, "refund-result:"+result.ID).Scan(&confirmed); err != nil {
		return err
	}
	if requestID := result.Metadata["aitok_request"]; requestID != "" {
		_, err = tx.ExecContext(ctx, `UPDATE payment_exceptions SET status=$3,updated_at=NOW() WHERE event_id=$1 AND order_no=$2 AND kind='refund_request' AND status IN ('processing','submitted','resolved')`, requestID, order, confirmed)
		if err != nil {
			return err
		}
	}
	if confirmed != "resolved" {
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE topup_orders SET refunded_minor=GREATEST(refunded_minor,(SELECT COALESCE(sum(amount_minor),0) FROM payment_exceptions WHERE order_no=$1 AND kind='refund_result' AND event_id LIKE 'refund-result:%' AND status='resolved')),updated_at=NOW() WHERE order_no=$1`, order)
	if err != nil {
		return err
	}
	// 钱包不足时保留独立待办，退款申请本身已完成，不再占用可退额度。
	_, err = tx.ExecContext(ctx, `INSERT INTO payment_exceptions(event_id,order_no,kind,amount_minor,detail) VALUES($1,$2,'refund',$3,'退款代币回收') ON CONFLICT(event_id) DO NOTHING`, "recovery:"+result.ID, order, result.Amount)
	if err != nil {
		return err
	}
	return reconcileWalletRefund(ctx, tx, order)
}
