package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoginTwoFactor(t *testing.T) {
	db := walletTestDB(t)
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	password, err := passwordHash("admin-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'admin@test.local',$1,'admin'),(2,'__superadmin__','','')`, password); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("login-two-factor-test"), admin: adminConfig{"operator", "super-password"}}
	handler := s.routes()
	call := func(path string, body any, status int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(string(raw))))
		if w.Code != status || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: got %d want %d", path, w.Code, status)
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if status != 200 && result["token"] != nil {
			t.Fatal("验证失败仍签发登录令牌")
		}
		return result
	}
	for _, input := range []struct {
		user        int64
		credentials map[string]string
	}{
		{1, map[string]string{"email": "admin@test.local", "password": "admin-password"}},
		{2, map[string]string{"username": "operator", "password": "super-password"}},
	} {
		code := browserTestOTP(t, s, input.user)
		first := call("/api/login", input.credentials, 200)
		if first["token"] != nil || first["two_factor_required"] != true {
			t.Fatal("已绑定验证器的账号不应仅凭密码获得登录令牌")
		}
		challenge := first["challenge"].(string)
		r := httptest.NewRequest("GET", "/api/me", nil)
		r.Header.Set("Authorization", "Bearer "+challenge)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatal("登录验证凭证可访问业务接口")
		}
		call("/api/login", map[string]string{"challenge": challenge + "x", "totp_code": code}, 401)
		call("/api/login", map[string]string{"challenge": challenge}, 403)
		call("/api/login", map[string]string{"challenge": challenge, "totp_code": "invalid"}, 403)
		result := call("/api/login", map[string]string{"challenge": challenge, "totp_code": code}, 200)
		r.Header.Set("Authorization", "Bearer "+result["token"].(string))
		if user, err := s.auth(r); err != nil || user != input.user {
			t.Fatal("完成两步验证后未签发有效登录身份", err)
		}
		call("/api/login", map[string]string{"challenge": challenge, "totp_code": code}, 403)
		// 即便持有其他范围的有效签名，或已过期的登录凭证，也不能完成登录。
		stamp, _ := s.sessionStamp(r.Context(), input.user)
		for _, claims := range []loginChallenge{
			{"desktop", input.user, time.Now().Add(time.Minute).Unix(), stamp},
			{"login-2fa-v1", input.user, time.Now().Add(-time.Minute).Unix(), stamp},
		} {
			body, _ := json.Marshal(claims)
			payload := base64.RawURLEncoding.EncodeToString(body)
			mac := hmac.New(sha256.New, s.secret)
			mac.Write([]byte(payload))
			call("/api/login", map[string]string{"challenge": payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), "totp_code": code}, 401)
		}
	}
	// 邮箱验证码只用于第一阶段，不能绕过已绑定的验证器。
	if err := s.storeEmailCode(context.Background(), "admin@test.local", "login", hash("123456"), time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	code := browserTestOTP(t, s, 1)
	first := call("/api/login-code", map[string]string{"email": "admin@test.local", "code": "123456"}, 200)
	if first["token"] != nil || first["two_factor_required"] != true {
		t.Fatal("邮箱验证码登录绕过了 2FA")
	}
	challenge := first["challenge"].(string)
	// 并发登录仅有一个请求可消费同一验证码，不要求重发邮箱验证码。
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, _ := json.Marshal(map[string]string{"challenge": challenge, "totp_code": code})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("POST", "/api/login", strings.NewReader(string(raw))))
			results <- w.Code
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for status := range results {
		if status == 200 {
			success++
		} else if status != 403 {
			t.Fatalf("并发验证状态异常: %d", status)
		}
	}
	if success != 1 {
		t.Fatal("同一验证码可被并发消费")
	}
	// 登录验证码的暴力尝试继续受到限流保护。
	for i := 0; i < 4; i++ {
		call("/api/login", map[string]string{"challenge": challenge, "totp_code": "invalid"}, 403)
	}
	call("/api/login", map[string]string{"challenge": challenge, "totp_code": "invalid"}, 429)
	if _, err := db.Exec(`UPDATE users SET session_version=session_version+1,role='user' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	call("/api/login", map[string]string{"challenge": challenge, "totp_code": code}, 401)
	// 降权保留验证器，普通用户仍可登录，但不会恢复管理权限。
	if _, err := db.Exec(`UPDATE auth_limits SET window_start=NOW()-INTERVAL '1 day'`); err != nil {
		t.Fatal(err)
	}
	code = browserTestOTP(t, s, 1)
	first = call("/api/login", map[string]string{"email": "admin@test.local", "password": "admin-password"}, 200)
	if first["two_factor_required"] != true || first["token"] != nil {
		t.Fatal("降权不应绕过已绑定的两步验证")
	}
	result := call("/api/login", map[string]string{"challenge": first["challenge"].(string), "totp_code": code}, 200)
	r := httptest.NewRequest("GET", "/api/bank-cards", nil)
	r.Header.Set("Authorization", "Bearer "+result["token"].(string))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("降权后登录恢复了管理权限")
	}
}
