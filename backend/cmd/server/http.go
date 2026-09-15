package main

import (
	"net/http"
	"time"

	"github.com/go-kratos/kratos/v2"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

func newApp(server *khttp.Server) *kratos.App {
	return kratos.New(
		kratos.Name("aitok-api"),
		kratos.Server(server),
		kratos.StopTimeout(5*time.Second),
	)
}

func (s *Server) routes(options ...khttp.ServerOption) *khttp.Server {
	options = append([]khttp.ServerOption{
		// 保留原有请求时限，由支付、浏览器等业务自行控制超时。
		khttp.Timeout(0),
		khttp.StrictSlash(false),
		khttp.Filter(cors, s.securityFilter, s.auditFilter, s.accessFilter),
		khttp.NotFoundHandler(http.NotFoundHandler()),
	}, options...)
	server := khttp.NewServer(options...)
	// 仅检查 HTTP 服务存活，不访问数据库或执行迁移。
	server.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		reply(w, map[string]string{"status": "ok"}, http.StatusOK)
	})
	server.HandleFunc("/api/packages", s.packages)
	server.HandlePrefix("/api/packages/", http.HandlerFunc(s.packages))
	server.HandleFunc("/api/orders", s.rechargeOrders)
	server.HandlePrefix("/api/orders/", http.HandlerFunc(s.rechargeOrders))
	server.HandlePrefix("/api/card-operations/", http.HandlerFunc(s.cardOperations))
	server.HandleFunc("/api/audit", s.audit)
	server.HandleFunc("/api/admin-activity", s.adminActivity)
	server.HandleFunc("/api/proxy-activity", s.proxyActivity)
	server.HandleFunc("/api/payment-exceptions", s.paymentExceptions)
	server.HandleFunc("/api/notices", s.notices)
	server.HandleFunc("/api/order-operators", s.orderOperators)
	server.HandleFunc("/api/logout", s.logout)
	server.HandleFunc("/api/register", s.register)
	server.HandleFunc("/api/login", s.login)
	server.HandleFunc("/api/send-code", s.sendCode)
	server.HandleFunc("/api/login-code", s.loginCode)
	server.HandleFunc("/api/forgot-password", s.forgotPassword)
	server.HandleFunc("/api/reset-password", s.resetPassword)
	server.HandleFunc("/api/me", s.me)
	server.HandleFunc("/api/two-factor", s.twoFactor)
	server.HandleFunc("/api/users", s.users)
	server.HandlePrefix("/api/users/", http.HandlerFunc(s.users))
	server.HandleFunc("/api/accounts", s.accounts)
	server.HandlePrefix("/api/accounts/", http.HandlerFunc(s.accountAction))
	server.HandleFunc("/api/bank-cards", s.bankCards)
	server.HandlePrefix("/api/bank-cards/", http.HandlerFunc(s.bankCards))
	server.HandleFunc("/api/browser-assistant", s.browserAssistant)
	server.HandlePrefix("/api/browser-assistant/", http.HandlerFunc(s.browserAssistant))
	server.HandleFunc("/api/addresses", s.addresses)
	server.HandlePrefix("/api/addresses/", http.HandlerFunc(s.addresses))
	server.HandleFunc("/api/account-groups", s.accountGroups)
	server.HandlePrefix("/api/account-groups/", http.HandlerFunc(s.accountGroups))
	server.HandleFunc("/api/wallet", s.walletDashboard)
	server.HandleFunc("/api/wallet/topups", s.createCheckout)
	server.HandlePrefix("/api/wallet/topups/", http.HandlerFunc(s.syncPayment))
	server.HandleFunc("/api/stripe/webhook", s.stripeWebhook)
	return server
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Aitok-Page, X-Aitok-TOTP")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
