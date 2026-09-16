package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminConfigAndRejectedCredentials(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "")
	t.Setenv("ADMIN_PASSWORD", "")
	if got := loadAdminConfig(); got.Username != "admin" || got.Password != "" {
		t.Fatal("默认超管配置错误")
	}
	t.Setenv("ADMIN_USERNAME", "operator")
	t.Setenv("ADMIN_PASSWORD", "custom-password")
	s := &Server{admin: loadAdminConfig()}
	if s.admin.Username != "operator" || s.admin.Password != "custom-password" {
		t.Fatal("未读取环境配置")
	}
	for _, input := range []string{
		`{"username":"operator","password":"wrong"}`,
		`{"email":"admin","password":"123456"}`,
		`{"email":"__superadmin__","password":"123456"}`,
	} {
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(input)))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("应拒绝无效凭据，实际 %d", w.Code)
		}
	}
	for _, path := range []string{"/api/register", "/api/send-code", "/api/reset-password"} {
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"email":"__superadmin__","password":"123456","code":"123456"}`)))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("内部超管标识应被拒绝: %s %d", path, w.Code)
		}
	}
}

func TestCloudflareMailer(t *testing.T) {
	t.Setenv("MAIL_PROVIDER", "cloudflare")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "test-account")
	t.Setenv("CLOUDFLARE_EMAIL_API_TOKEN", "test-token")
	t.Setenv("MAIL_FROM_ADDRESS", "no-reply@toktopup.com")
	t.Setenv("MAIL_FROM_NAME", "AiTok")
	response := `{"success":true}`
	status := http.StatusOK
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/accounts/test-account/email/sending/send" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("Cloudflare 请求参数不匹配")
		}
		var payload struct {
			To   string
			From struct{ Address, Name string }
			Text string
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload.To != "test@example.com" || payload.From.Address != "no-reply@toktopup.com" || payload.From.Name != "AiTok" || !strings.Contains(payload.Text, "123456") {
			t.Error("邮件内容或发件人错误")
		}
		w.WriteHeader(status)
		io.WriteString(w, response)
	}))
	defer mock.Close()
	t.Setenv("CLOUDFLARE_EMAIL_API_BASE_URL", mock.URL)
	m := newMailer()
	if err := m.sendCode(context.Background(), "test@example.com", "123456", 10); err != nil {
		t.Fatal(err)
	}
	response = `{"success":false}`
	if err := m.sendCode(context.Background(), "test@example.com", "123456", 10); err == nil {
		t.Fatal("未识别供应商失败响应")
	}
	response = `{"success":true}`
	status = http.StatusBadGateway
	if err := m.sendCode(context.Background(), "test@example.com", "123456", 10); err == nil {
		t.Fatal("未识别 HTTP 失败响应")
	}
	m.apiToken = ""
	if err := m.sendCode(context.Background(), "test@example.com", "123456", 10); err == nil {
		t.Fatal("缺少配置应失败")
	}
}

// 仅创建当前连接的临时表，连接关闭后自动回收，不修改项目持久化数据。
func TestAuthIntegration(t *testing.T) {
	db := walletTestDB(t)
	var deliveredCode string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct{ Text string }
		json.NewDecoder(r.Body).Decode(&payload)
		text := strings.TrimPrefix(payload.Text, "你的验证码是 ")
		deliveredCode = strings.Split(text, "，")[0]
		io.WriteString(w, `{"success":true}`)
	}))
	defer mock.Close()
	s := &Server{db: db, secret: []byte("test-signing-secret"), admin: adminConfig{"admin", "123456"}, mailer: &cloudflareMailer{provider: "cloudflare", accountID: "test", apiToken: "test", apiBaseURL: mock.URL, fromAddress: "no-reply@toktopup.com", client: mock.Client()}}
	routes := s.routes()
	call := func(path, body, token string, status int) map[string]any {
		t.Helper()
		method := http.MethodPost
		if path == "/api/me" {
			method = http.MethodGet
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s 状态码 %d，预期 %d", path, w.Code, status)
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	adminLogin := call("/api/login", `{"username":"admin","password":"123456"}`, "", 200)
	adminMe := call("/api/me", "", adminLogin["token"].(string), 200)["user"].(map[string]any)
	if adminMe["role"] != "super_admin" || adminMe["username"] != "admin" {
		t.Fatal("超管身份错误")
	}
	s.admin = adminConfig{"operator", "custom-password"}
	call("/api/login", `{"username":"admin","password":"123456"}`, "", 401)
	customLogin := call("/api/login", `{"email":"operator","password":"custom-password"}`, "", 200)
	customMe := call("/api/me", "", customLogin["token"].(string), 200)["user"].(map[string]any)
	if customMe["id"] != adminMe["id"] {
		t.Fatal("更改超管用户名后 ID 不应改变")
	}
	result := call("/api/send-code", `{"email":"member@example.com","purpose":"login"}`, "", 200)
	if result["dev_code"] != nil || len(deliveredCode) != 6 {
		t.Fatal("验证码处理错误")
	}
	codeBody := `{"email":"member@example.com","code":"` + deliveredCode + `"}`
	memberLogin := call("/api/login-code", codeBody, "", 200)
	memberMe := call("/api/me", "", memberLogin["token"].(string), 200)["user"].(map[string]any)
	if memberMe["role"] != "user" {
		t.Fatal("普通用户权限错误")
	}
	call("/api/login-code", codeBody, "", 401)
	call("/api/login", `{"email":"member@example.com","password":"`+deliveredCode+`"}`, "", 401)
	call("/api/forgot-password", `{"email":"member@example.com"}`, "", 200)
	check := expectTimestampUpdate(t, db, "users", "email='member@example.com'")
	call("/api/reset-password", `{"email":"member@example.com","code":"`+deliveredCode+`","password":"new-password"}`, "", 200)
	check()
	call("/api/login", `{"email":"member@example.com","password":"new-password"}`, "", 200)
}
