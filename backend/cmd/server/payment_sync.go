package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	stripe "github.com/stripe/stripe-go/v82"
)

// 用户核验自己的订单；超管可处理漏回调订单。不接收客户端提供的支付结果或 Session ID。
func (s *Server) syncPayment(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/refund") {
		s.requestTopupRefund(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	id, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/wallet/topups/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "sync" {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	var sessionID sql.NullString
	var status string
	err = s.db.QueryRowContext(ctx, `SELECT session_id,status FROM topup_orders WHERE order_no=$1 AND deleted_at IS NULL AND user_id=$2`, parts[0], id).Scan(&sessionID, &status)
	if errors.Is(err, sql.ErrNoRows) {
		admin, adminErr := s.isAdmin(ctx, id)
		if adminErr == nil && admin {
			err = s.db.QueryRowContext(ctx, `SELECT session_id,status FROM topup_orders WHERE order_no=$1 AND deleted_at IS NULL`, parts[0]).Scan(&sessionID, &status)
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		reply(w, map[string]string{"error": "订单不存在"}, 404)
		return
	}
	if err != nil {
		reply(w, map[string]string{"error": "读取订单失败"}, 500)
		return
	}
	if (status != "pending" && status != "paid") || !sessionID.Valid {
		reply(w, map[string]string{"status": status}, 200)
		return
	}
	if s.billing.SecretKey == "" {
		reply(w, map[string]string{"error": "Stripe 暂未配置"}, 503)
		return
	}
	session, err := s.stripe.RetrieveSession(ctx, sessionID.String)
	if err != nil {
		reply(w, map[string]string{"error": "Stripe 暂时无法连接，请稍后重新核对"}, 502)
		return
	}
	if session == nil || session.ID != sessionID.String || session.ClientReferenceID != parts[0] {
		reply(w, map[string]string{"error": "支付订单不匹配"}, 409)
		return
	}
	if session.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid {
		err = s.applyStripeSession(ctx, stripe.EventTypeCheckoutSessionCompleted, session)
		status = "paid"
	} else if session.Status == stripe.CheckoutSessionStatusExpired {
		err = s.applyStripeSession(ctx, stripe.EventTypeCheckoutSessionExpired, session)
		status = "expired"
	}
	if err != nil {
		reply(w, map[string]string{"error": "订单校验或入账失败，请联系管理员"}, 409)
		return
	}
	reply(w, map[string]string{"status": status}, 200)
}
