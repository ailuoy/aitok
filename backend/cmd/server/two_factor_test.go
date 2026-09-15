package main

import (
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTOTPStandardAndWindow(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	for _, v := range []struct {
		at   int64
		want string
	}{{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"}, {1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"}} {
		if got := totpCode(secret, v.at/30); got != v.want {
			t.Fatalf("RFC 6238 vector %d: %s", v.at, got)
		}
	}
	now := time.Unix(1234567890, 0)
	step := now.Unix() / 30
	for _, offset := range []int64{-1, 0, 1} {
		if matchTOTP(secret, totpCode(secret, step+offset), now, -1) != step+offset {
			t.Fatal("时钟窗口不匹配")
		}
	}
	for _, code := range []string{"", "12345", "1234567", "abcdef", totpCode(secret, step-2), totpCode(secret, step+2)} {
		if matchTOTP(secret, code, now, -1) != -1 {
			t.Fatal("无效验证码被接受")
		}
	}
	if matchTOTP(secret, totpCode(secret, step), now, step) != -1 {
		t.Fatal("验证码重放未拒绝")
	}
}

func TestRoleBoundaryAndTwoFactor(t *testing.T) {
	db := walletTestDB(t)
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("t", 32))))
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,role,permissions) VALUES(1,'user@test.local','','',NULL),(2,'admin@test.local','','admin',ARRAY[]::text[]),(3,'__superadmin__','','',NULL);INSERT INTO chatgpt_accounts(id,user_id,label,email,renewal_date) VALUES(1,1,'own','own@test.local','2030-01-01'),(2,2,'other','other@test.local','2030-02-01')`)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`SELECT setval(pg_get_serial_sequence('chatgpt_accounts','id'),2)`)
	s := &Server{db: db, secret: []byte("role-tests"), admin: adminConfig{"admin", "test-admin-password"}}
	handler := s.routes()
	call := func(user int64, method, path string, body any, code string) (int, map[string]any) {
		t.Helper()
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(data)))
		r.Header.Set("Authorization", "Bearer "+s.token(user))
		r.Header.Set("X-Aitok-TOTP", code)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out
	}
	for _, path := range []string{"/api/accounts", "/api/accounts?paged=1", "/api/me"} {
		status, out := call(1, "GET", path, nil, "")
		if status != 200 {
			t.Fatalf("用户列表 %s: %d", path, status)
		}
		accounts := out["accounts"].([]any)
		if len(accounts) != 1 {
			t.Fatal("用户可读取他人账号")
		}
		a := accounts[0].(map[string]any)
		if len(a) != 4 || a["email"] != "own@test.local" {
			t.Fatal("用户账号响应含管理字段")
		}
	}
	for _, v := range []struct{ method, path string }{{"DELETE", "/api/accounts/1"}, {"PATCH", "/api/accounts/1/session"}, {"POST", "/api/accounts/1/browser-session"}, {"POST", "/api/accounts/1/browser"}, {"POST", "/api/accounts/import"}, {"GET", "/api/accounts/export"}, {"GET", "/api/wallet"}, {"GET", "/api/packages"}, {"GET", "/api/bank-cards"}, {"GET", "/api/addresses"}, {"GET", "/api/account-groups"}, {"GET", "/api/users"}, {"GET", "/api/proxy-activity"}, {"GET", "/api/two-factor"}, {"POST", "/api/orders"}, {"POST", "/api/unknown-management"}} {
		if status, _ := call(1, v.method, v.path, nil, ""); status != 403 {
			t.Fatalf("用户越权 %s %s: %d", v.method, v.path, status)
		}
	}
	if status, _ := call(1, "POST", "/api/accounts", map[string]string{"email": "new@test.local", "session_json": `{"accessToken":"test-token"}`}, ""); status != 201 {
		t.Fatalf("用户添加账号: %d", status)
	}
	if status, out := call(2, "GET", "/api/accounts", nil, ""); status != 200 || len(out["accounts"].([]any)) != 3 {
		t.Fatal("管理员没有全量访问")
	}
	if status, _ := call(2, "POST", "/api/accounts/import", map[string]any{"accounts": []any{}}, ""); status != 404 {
		t.Fatal("已移除的批量导入接口仍可访问")
	}
	for _, path := range []string{"/api/accounts/1/browser-session", "/api/accounts/1/browser"} {
		if status, _ := call(2, "POST", path, nil, ""); status != 403 {
			t.Fatal("未绑定验证器启动未拦截")
		}
	}
	if status, _ := call(3, "POST", "/api/two-factor", map[string]string{"action": "setup", "password": "wrong"}, ""); status != 403 {
		t.Fatal("绑定未校验密码")
	}
	status, setup := call(3, "POST", "/api/two-factor", map[string]string{"action": "setup", "password": "test-admin-password"}, "")
	if status != 200 {
		t.Fatalf("生成二维码: %d", status)
	}
	secret := setup["secret"].(string)
	if !strings.HasPrefix(setup["otpauth_url"].(string), "otpauth://totp/") {
		t.Fatal("二维码 URI 无效")
	}
	var encrypted string
	var enabled bool
	db.QueryRow(`SELECT totp_pending_ciphertext,totp_enabled_at IS NOT NULL FROM users WHERE id=3`).Scan(&encrypted, &enabled)
	if enabled || strings.Contains(encrypted, secret) {
		t.Fatal("绑定未确认即启用或明文持久化")
	}
	if _, err := readTOTP(encrypted, 2); err == nil {
		t.Fatal("可跨用户使用密钥")
	}
	// 过期绑定不能激活，重新生成后仅新二维码有效。
	db.Exec(`UPDATE users SET totp_pending_expires_at=NOW()-INTERVAL '1 second' WHERE id=3`)
	if status, _ = call(3, "POST", "/api/two-factor", map[string]string{"action": "confirm", "code": totpCode(secret, time.Now().Unix()/30)}, ""); status != 409 {
		t.Fatal("过期二维码被激活")
	}
	status, setup = call(3, "POST", "/api/two-factor", map[string]string{"action": "setup", "password": "test-admin-password"}, "")
	if status != 200 {
		t.Fatal("重新生成绑定失败")
	}
	secret = setup["secret"].(string)
	now := time.Now()
	previous := totpCode(secret, now.Unix()/30-1)
	if status, _ = call(3, "POST", "/api/two-factor", map[string]string{"action": "confirm", "code": previous}, ""); status != 200 {
		t.Fatalf("确认绑定: %d", status)
	}
	if status, _ = call(3, "POST", "/api/accounts/1/browser-session", nil, previous); status != 403 {
		t.Fatal("绑定验证码可重放")
	}
	if status, _ = call(3, "POST", "/api/two-factor", map[string]string{"action": "setup", "password": "test-admin-password"}, ""); status != 409 {
		t.Fatal("已绑定密钥可被覆盖")
	}
	credentials, _ := encryptSession(`{"accessToken":"test-token","user":{"email":"own@test.local"}}`)
	db.Exec(`UPDATE chatgpt_accounts SET session_ciphertext=$1 WHERE id=1`, credentials)
	current := totpCode(secret, now.Unix()/30)
	status, out := call(3, "POST", "/api/accounts/1/browser-session", nil, current)
	if status != 200 || out["session"] == nil {
		t.Fatalf("管理员打开他人账号: %d", status)
	}
	if status, _ = call(3, "POST", "/api/accounts/1/browser", nil, current); status != 403 {
		t.Fatal("可在另一入口重放验证码")
	}
	// 并发请求只能一个消费成功；不连接任何真实浏览器。
	code := totpCode(secret, now.Unix()/30+1)
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, _ := call(3, "POST", "/api/accounts/1/browser-session", nil, code)
			results <- status
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for status := range results {
		if status == 200 {
			success++
		} else if status != 403 {
			t.Fatalf("并发验证: %d", status)
		}
	}
	if success != 1 {
		t.Fatal("并发重复启动")
	}
	// 旧助手凭证在角色降级后失效，即使没有重新登录。
	assistant := s.assistantToken(2, 2)
	db.Exec(`UPDATE users SET role='user' WHERE id=2`)
	r := httptest.NewRequest("GET", "/api/browser-assistant", nil)
	r.Header.Set("Authorization", "Bearer "+assistant)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("降权后旧助手仍可用")
	}
	for i := 0; i < 12; i++ {
		status, _ = call(3, "POST", "/api/accounts/1/browser-session", nil, "000000")
	}
	if status != 429 {
		t.Fatal("验证码暴力尝试未限流")
	}
	var leak bool
	db.QueryRow(`SELECT EXISTS(SELECT 1 FROM operation_events WHERE after_data::text LIKE $1 OR before_data::text LIKE $1)`, "%"+secret+"%").Scan(&leak)
	if leak {
		t.Fatal("审计泄露验证器种子")
	}
	var count int
	db.QueryRow(`SELECT count(*) FROM chatgpt_accounts WHERE id=1 AND deleted_at IS NULL`).Scan(&count)
	if count != 1 {
		t.Fatal("用户删除请求修改了数据")
	}
}

// 旧浏览器功能用例每次独立配置验证器，重放和并发保护由安全用例独立验证。
func browserTestOTP(t *testing.T, s *Server, user int64) string {
	t.Helper()
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte(strings.Repeat("b", 20)))
	encrypted, err := encryptSession(totpEnvelope(user) + secret)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec("UPDATE users SET totp_ciphertext=$2,totp_enabled_at=NOW(),totp_last_step=-1 WHERE id=$1", user, encrypted)
	if err != nil {
		t.Fatal(err)
	}
	return totpCode(secret, time.Now().Unix()/30)
}
