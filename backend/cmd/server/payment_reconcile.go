package main

import (
	"context"
	"log"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
)

// 定期只读查询渠道支付状态，漏回调仍走同一个原子入账入口。
// 多实例可能同时查询，账本唯一键和订单锁保证只入账一次。
func (s *Server) runPaymentReconciliation(ctx context.Context) {
	if s.billing.SecretKey == "" || s.stripe == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			batch, cancel := context.WithTimeout(ctx, time.Minute)
			if err := s.reconcilePendingPayments(batch); err != nil && ctx.Err() == nil {
				log.Printf("payment reconciliation failed: %v", err)
			}
			cancel()
		}
	}
}

func (s *Server) reconcilePendingPayments(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT order_no,session_id FROM topup_orders WHERE status='pending' AND session_id IS NOT NULL AND deleted_at IS NULL ORDER BY updated_at LIMIT 20`)
	if err != nil {
		return err
	}
	type payment struct{ order, session string }
	items := []payment{}
	for rows.Next() {
		var item payment
		if err = rows.Scan(&item.order, &item.session); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		call, cancel := context.WithTimeout(ctx, 10*time.Second)
		session, e := s.stripe.RetrieveSession(call, item.session)
		cancel()
		if e == nil && session != nil && session.ID == item.session && session.ClientReferenceID == item.order {
			if session.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid {
				e = s.applyStripeSession(ctx, stripe.EventTypeCheckoutSessionCompleted, session)
			} else if session.Status == stripe.CheckoutSessionStatusExpired {
				e = s.applyStripeSession(ctx, stripe.EventTypeCheckoutSessionExpired, session)
			}
		}
		// 轮转检查，单笔渠道故障不能阻塞其余订单；失败不改变支付结果。
		if _, err = s.db.ExecContext(ctx, `UPDATE topup_orders SET updated_at=NOW() WHERE order_no=$1 AND status='pending' AND deleted_at IS NULL`, item.order); err != nil {
			return err
		}
		if e != nil {
			log.Print("payment reconciliation: one order remains pending")
		}
	}
	return nil
}
