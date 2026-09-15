package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeAccountBrowser struct {
	methods []string
	params  []map[string]any
}

func (b *fakeAccountBrowser) Call(_ context.Context, method string, params any) (json.RawMessage, error) {
	b.methods = append(b.methods, method)
	b.params = append(b.params, params.(map[string]any))
	state := "closed"
	if method == "start" {
		state = "opened"
	}
	return json.Marshal(map[string]string{"state": state})
}

func TestBrowserProxyValidationAndMasking(t *testing.T) {
	for _, value := range []string{"", "socks5://host:1080", "socks5://user:p%40ss@[::1]:1080"} {
		if err := validateBrowserProxy(value); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"http://host:1080", "socks5://host", "socks5://host:0", "socks5://host:65536", "socks5://host:1080/path", "socks5://user@host:1080", "socks5://host:1080?q=secret"} {
		if validateBrowserProxy(value) == nil {
			t.Fatal("无效代理被接受")
		}
	}
	settings, _ := json.Marshal(browserProxySettings("socks5://username:password@proxy.example:1080"))
	if strings.Contains(string(settings), "username") || strings.Contains(string(settings), "password") || !strings.Contains(string(settings), "socks5://proxy.example:1080") {
		t.Fatal("代理配置未正确脱敏")
	}
}

func TestManagedAccountBrowserIntegration(t *testing.T) {
	db := walletTestDB(t)
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(1,'owner@example.com',''),(2,'other@example.com',''),(3,'__superadmin__','')`); err != nil {
		t.Fatal(err)
	}
	worker := &fakeAccountBrowser{}
	s := &Server{db: db, secret: []byte("browser-test-secret"), browser: worker}
	call := func(method, path string, user int64, input any, status int) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		if user != 0 {
			r.Header.Set("Authorization", "Bearer "+s.token(user))
		}
		if method == "POST" && (strings.HasSuffix(path, "/browser") || strings.HasSuffix(path, "/browser-session")) && s.permitted(r.Context(), user, "accounts") {
			r.Header.Set("X-Aitok-TOTP", browserTestOTP(t, s, user))
		}
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d，预期 %d", method, path, w.Code, status)
		}
		return w
	}
	raw := `{"accessToken":"original-secret","refreshToken":"refresh-secret","user":{"email":"chat@example.com"}}`
	w := call("POST", "/api/accounts", 1, map[string]string{"session_json": raw}, 201)
	var result struct{ Account Account }
	json.Unmarshal(w.Body.Bytes(), &result)
	path := fmt.Sprintf("/api/accounts/%d", result.Account.ID)
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		call(method, path+"/browser", 0, map[string]any{}, 401)
		call(method, path+"/browser", 1, map[string]any{}, 403)
		call(method, path+"/browser", 2, map[string]any{}, 403)
	}
	if len(worker.methods) != 0 {
		t.Fatal("未授权请求触发了后台浏览器")
	}
	proxy := "socks5://proxy-user:proxy-password@proxy.example:1080"
	w = call("PATCH", path+"/browser", 3, map[string]string{"proxy_url": proxy}, 200)
	if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "proxy-password") || strings.Contains(w.Body.String(), "original-secret") {
		t.Fatal("浏览器配置接口泄露凭据或未禁止缓存")
	}
	var encrypted string
	db.QueryRow(`SELECT session_ciphertext FROM chatgpt_accounts WHERE id=$1`, result.Account.ID).Scan(&encrypted)
	credentials, err := decodeAccountCredentials(encrypted)
	if err != nil || credentials.SessionJSON != raw || credentials.ProxyURL != proxy || strings.Contains(encrypted, "proxy-password") {
		t.Fatal("代理与原始 Session 未正确加密保存")
	}
	updated := strings.ReplaceAll(raw, "original-secret", "updated-secret")
	call("PATCH", path+"/session", 3, map[string]string{"session_json": updated}, 200)
	call("GET", path+"/browser", 3, nil, 200)
	w = call("POST", path+"/browser", 3, map[string]any{"environment_id": "tampered"}, 200)
	params := worker.params[len(worker.params)-1]
	if params["environment_id"] != fmt.Sprintf("account:%d", result.Account.ID) || params["proxy_url"] != proxy || params["expected_email"] != "chat@example.com" {
		t.Fatal("后台浏览器未使用服务端账号标识或保存的代理")
	}
	session := params["session"].(map[string]any)
	if session["accessToken"] != "updated-secret" || session["refreshToken"] != nil || strings.Contains(w.Body.String(), "secret") {
		t.Fatal("启动凭据范围错误或凭据被返回前端")
	}
	call("DELETE", path+"/browser", 3, nil, 200)
	call("PATCH", path+"/browser", 3, map[string]string{"proxy_url": ""}, 200)
	call("POST", path+"/browser", 3, map[string]any{}, 200)
	if worker.params[len(worker.params)-1]["proxy_url"] != "" {
		t.Fatal("清空代理后仍使用旧代理")
	}
	call("PATCH", path+"/browser", 3, map[string]string{"proxy_url": "https://invalid"}, 400)
	call("POST", "/api/accounts/999/browser", 3, map[string]any{}, 404)
	call("POST", path+"/browser-session", 3, nil, 200)
	call("POST", path+"/browser-session", 1, nil, 403)
}

func TestBrowserRuntimeCancelledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	browser := &browserRuntime{}
	if _, err := browser.Call(ctx, "start", map[string]any{}); err == nil || browser.command != nil {
		t.Fatal("取消的请求不应启动子进程")
	}
}

func TestBrowserRuntimeIntegration(t *testing.T) {
	if os.Getenv("AITOK_BROWSER_SMOKE") == "" {
		t.Skip("设置 AITOK_BROWSER_SMOKE 验证真实 Node 子进程通信")
	}
	t.Setenv("AITOK_BROWSER_SCRIPT", "../../../scripts/session-browser.mjs")
	browser := &browserRuntime{}
	t.Cleanup(browser.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := browser.Call(ctx, "status", map[string]any{"environment_id": fmt.Sprintf("test:%d", i)})
			if err != nil || !strings.Contains(string(result), `"closed"`) {
				t.Errorf("后台浏览器通信失败: %v", err)
			}
		}(i)
	}
	wg.Wait()
}
