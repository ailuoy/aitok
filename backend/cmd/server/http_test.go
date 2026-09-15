package main

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/transport"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

func TestHTTPRouteCompatibility(t *testing.T) {
	s := &Server{secret: []byte("test-secret"), admin: adminConfig{Username: "admin", Password: "test-password"}}
	server := s.routes()
	for _, route := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/api/me", 401},
		{"GET", "/api/users", 401},
		{"PATCH", "/api/users/1/role", 401},
		{"GET", "/api/accounts", 401},
		{"POST", "/api/accounts/1/renew", 401},
		{"GET", "/api/bank-cards", 401},
		{"GET", "/api/bank-cards/1/ledger", 401},
		{"GET", "/api/browser-assistant", 401},
		{"GET", "/api/browser-assistant/cards/1", 401},
		{"GET", "/api/addresses", 401},
		{"PATCH", "/api/addresses/1", 401},
		{"GET", "/api/account-groups", 401},
		{"DELETE", "/api/account-groups/1", 401},
		{"GET", "/api/wallet", 401},
		{"POST", "/api/wallet/topups", 401},
		{"POST", "/api/wallet/topups/order/sync", 401},
		{"POST", "/api/register", 400},
		{"POST", "/api/login", 401},
		{"POST", "/api/login-code", 401},
		{"POST", "/api/send-code", 400},
		{"POST", "/api/forgot-password", 400},
		{"POST", "/api/reset-password", 400},
		{"POST", "/api/stripe/webhook", 503},
		{"GET", "/api/login", 405},
		{"GET", "/api/stripe/webhook", 405},
		{"POST", "/healthz", 405},
		{"GET", "/healthz/", 404},
		{"GET", "/api/login/", 404},
		{"GET", "/api/accounts-extra", 404},
		{"GET", "/missing", 404},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			server.ServeHTTP(w, httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`)))
			if w.Code != route.status {
				t.Fatalf("状态码 %d，预期 %d；响应 %s", w.Code, route.status, w.Body.String())
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "*" {
				t.Fatal("缺少 CORS 响应头")
			}
			if route.status == 401 {
				var body map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["error"] == "" {
					t.Fatal("鉴权错误必须保留原有 JSON error 字段")
				}
			}
		})
	}
}

func TestHTTPPreflight(t *testing.T) {
	server := (&Server{}).routes()
	for _, path := range []string{"/api/accounts/1/session", "/healthz", "/missing"} {
		r := httptest.NewRequest(http.MethodOptions, path, nil)
		r.Header.Set("Origin", "https://example.com")
		r.Header.Set("Access-Control-Request-Method", "PATCH")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
			t.Fatalf("%s 预检请求未返回空的 204 响应", path)
		}
		for name, want := range map[string]string{
			"Access-Control-Allow-Origin":  "*",
			"Access-Control-Allow-Headers": "Content-Type, Authorization",
			"Access-Control-Allow-Methods": "GET,POST,PATCH,DELETE,OPTIONS",
		} {
			if w.Header().Get(name) != want {
				t.Fatalf("%s: %s = %q，预期 %q", path, name, w.Header().Get(name), want)
			}
		}
	}
}

func TestKratosRequestContext(t *testing.T) {
	server := (&Server{}).routes()
	server.HandleFunc("/test-context", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := transport.FromServerContext(r.Context()); !ok {
			t.Error("请求未经过 Kratos HTTP 传输层")
		}
		if _, ok := r.Context().Deadline(); ok {
			t.Error("框架不应额外限制支付或浏览器请求的超时时间")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test-context", nil))
}

func TestKratosAppLifecycle(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	server := (&Server{}).routes(khttp.Listener(listener))
	app := newApp(server)
	done := make(chan error, 1)
	go func() { done <- app.Run() }()
	t.Cleanup(func() {
		if err := app.Stop(); err != nil {
			t.Error(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(6 * time.Second):
			t.Error("Kratos 应用未在停机期限内退出")
		}
	})
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req, err := http.NewRequest(method, "http://"+listener.Addr().String()+"/healthz", nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("%s 健康检查失败: 状态码 %d，错误 %v", method, response.StatusCode, err)
		}
		if method == http.MethodHead && len(body) != 0 {
			t.Fatal("HEAD 响应不应携带正文")
		}
		if method == http.MethodGet && string(body) != "{\"status\":\"ok\"}\n" {
			t.Fatalf("健康检查响应不兼容: %s", body)
		}
	}
}
