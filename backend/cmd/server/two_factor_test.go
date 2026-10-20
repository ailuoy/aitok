package main

import (
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestTwoFactorQRReveal(t *testing.T) {
	db := walletTestDB(t)
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("q", 32))))
	password := "current-admin-password"
	encoded, err := passwordHash(password)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES
(1,'admin+qr@test.local',$1,'admin'),(2,'__superadmin__','',''),
(3,'unbound@test.local',$1,'admin'),(4,'member@test.local',$1,'user'),
(5,'disabled@test.local',$1,'admin'),(6,'deleted@test.local',$1,'admin'),
(7,'broken@test.local',$1,'admin'),(8,'no-password@test.local','','admin')`, encoded)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("qr-reveal-tests"), admin: adminConfig{"operator", "super-admin-password"}}
	tokens := map[int64]string{}
	for _, id := range []int64{1, 2, 3, 4, 5, 6, 7, 8} {
		if id != 3 {
			browserTestOTP(t, s, id)
		}
		tokens[id] = s.token(id)
	}
	_, err = db.Exec(`UPDATE users SET disabled=true WHERE id=5;
UPDATE users SET deleted_at=NOW() WHERE id=6;
UPDATE users SET totp_ciphertext=(SELECT totp_ciphertext FROM users WHERE id=1) WHERE id=7;
UPDATE users SET totp_last_step=42 WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	var original string
	if err := db.QueryRow(`SELECT to_jsonb(u)::text FROM users u WHERE id=1`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte(strings.Repeat("b", 20)))
	call := func(user int64, method, action, suppliedPassword string, status int) map[string]any {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"action": action, "password": suppliedPassword})
		r := httptest.NewRequest(method, "/api/two-factor", strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+tokens[user])
		r.Header.Set("X-Aitok-Page", "/admin/accounts")
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status || (status == 200 && w.Header().Get("Cache-Control") != "no-store") {
			t.Fatalf("%s %s: got %d want %d，敏感响应应禁止缓存", method, action, w.Code, status)
		}
		if status != 200 || method == "GET" {
			if strings.Contains(w.Body.String(), secret) || strings.Contains(w.Body.String(), "otpauth://") {
				t.Fatal("状态查询或失败响应泄露密钥")
			}
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	if out := call(1, "GET", "", "", 200); out["enabled"] != true || len(out) != 1 {
		t.Fatal("状态接口只能返回是否启用")
	}
	call(0, "POST", "reveal", password, 401)
	call(4, "POST", "reveal", password, 403)
	call(5, "POST", "reveal", password, 401)
	call(6, "POST", "reveal", password, 401)
	call(3, "POST", "reveal", password, 409)
	call(7, "POST", "reveal", password, 503)
	call(8, "POST", "reveal", "", 403)
	call(1, "POST", "reveal", "wrong", 403)
	call(1, "POST", "reveal", "", 403)
	for _, tc := range []struct {
		user     int64
		password string
		label    string
	}{{1, password, "admin+qr@test.local"}, {2, s.admin.Password, "operator"}} {
		out := call(tc.user, "POST", "reveal", tc.password, 200)
		uri, err := url.Parse(out["otpauth_url"].(string))
		if err != nil || uri.Scheme != "otpauth" || uri.Host != "totp" || uri.Path != "/AiTok:"+tc.label || uri.Query().Get("secret") != secret || uri.Query().Get("issuer") != "AiTok" || out["secret"] != secret {
			t.Fatal("重新扫码必须返回当前用户原有验证器配置")
		}
	}
	// 再次查看仍需密码，成功查看不能重置限流计数。
	call(1, "POST", "reveal", "wrong", 403)
	call(1, "POST", "reveal", password, 200)
	call(1, "POST", "reveal", password, 429)
	var after string
	if err := db.QueryRow(`SELECT to_jsonb(u)::text FROM users u WHERE id=1`).Scan(&after); err != nil || after != original {
		t.Fatal("重新扫码不应修改密钥、启用时间或验证码消费状态", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM operation_events WHERE entity_type='user' AND entity_id=1 AND actor_id=1 AND action='two_factor_qr_read' AND after_data->>'qr_viewed'='true'`).Scan(&count); err != nil || count != 2 {
		t.Fatal("每次成功读取必须记录本人查看审计", count, err)
	}
	var audit string
	if err := db.QueryRow(`SELECT string_agg(to_jsonb(e)::text,' ') FROM operation_events e`).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	for _, sensitive := range []string{secret, password, s.admin.Password, encoded, "otpauth://"} {
		if strings.Contains(audit, sensitive) {
			t.Fatal("审计中包含密码、密钥或二维码内容")
		}
	}
}

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
		if len(a) != 8 || a["email"] != "own@test.local" {
			t.Fatal("用户账号响应含管理字段")
		}
	}
	for _, v := range []struct{ method, path string }{{"DELETE", "/api/accounts/1"}, {"PATCH", "/api/accounts/1/session"}, {"POST", "/api/accounts/1/browser-session"}, {"POST", "/api/accounts/1/browser"}, {"POST", "/api/accounts/import"}, {"GET", "/api/accounts/export"}, {"GET", "/api/packages"}, {"GET", "/api/bank-cards"}, {"GET", "/api/addresses"}, {"GET", "/api/account-groups"}, {"GET", "/api/users"}, {"GET", "/api/proxy-activity"}, {"GET", "/api/two-factor"}, {"POST", "/api/orders"}, {"POST", "/api/unknown-management"}} {
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
		if status, _ := call(2, "POST", path, map[string]any{}, ""); status != 400 && status != 422 {
			t.Fatal("浏览器启动不应要求绑定验证器")
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
	if status, _ = call(3, "POST", "/api/two-factor", map[string]string{"action": "confirm", "code": previous}, ""); status != 409 {
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
	if status, _ = call(3, "POST", "/api/accounts/1/browser", map[string]any{}, ""); status != 503 {
		t.Fatal("后台启动不应再次校验验证码")
	}
	// 浏览器导出不会消费或限流 TOTP，连续打开多个账号无需等待新验证码。
	for i := 0; i < 12; i++ {
		if status, _ := call(3, "POST", "/api/accounts/1/browser-session", nil, ""); status != 200 {
			t.Fatalf("连续打开账号不应受 2FA 限流影响: %d", status)
		}
	}
	var last int64
	if err := db.QueryRow(`SELECT totp_last_step FROM users WHERE id=3`).Scan(&last); err != nil || last != now.Unix()/30-1 {
		t.Fatal("浏览器打开不应消费验证码", err)
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

// 敏感操作用例独立配置验证器，重放和并发保护由登录安全用例验证。
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
