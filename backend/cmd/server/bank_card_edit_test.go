package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 仅在隔离测试库准备验证器，业务测试无需等待真实的 30 秒时间步。
func prepareCardEditTOTP(t *testing.T, s *Server, user int64) string {
	t.Helper()
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	encrypted, err := encryptSession(totpEnvelope(user) + secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE users SET totp_ciphertext=$2,totp_enabled_at=NOW(),totp_last_step=-1,updated_at=NOW() WHERE id=$1`, user, encrypted); err != nil {
		t.Fatal(err)
	}
	return totpCode(secret, time.Now().Unix()/30)
}

func cardEditTestToken(t *testing.T, s *Server, user, card int64, expires time.Time) string {
	t.Helper()
	token, err := s.bankCardEditToken(httptest.NewRequest("GET", "/", nil), user, card, expires)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestBankCardEditSecurityAndPreservation(t *testing.T) {
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("e", 32))))
	db := walletTestDB(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'edit@test.local','','admin'),(2,'other@test.local','','admin'),(3,'member@test.local','','user')`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("card-edit-test")}
	routes := s.routes()
	call := func(method, path string, user int64, code string, input any, status int) map[string]any {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+s.token(user))
		r.Header.Set("X-Aitok-TOTP", code)
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d want %d", method, path, w.Code, status)
		}
		if strings.Contains(w.Body.String(), `"cvc":`) || strings.Contains(w.Body.String(), "4242424242424242") {
			t.Fatal("编辑接口泄露完整卡号或 CVC")
		}
		if user != 3 && w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("银行卡响应允许缓存")
		}
		var data map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &data)
		return data
	}
	input := map[string]any{"label": "Test", "cardholder": "Test User", "number": "4242424242424242", "cvc": "0042", "exp_month": 12, "exp_year": 2035}
	call("POST", "/api/bank-cards", 1, "", input, 201)
	const path = "/api/bank-cards/1"
	call("GET", path, 1, "", nil, 403)
	code := prepareCardEditTOTP(t, s, 1)
	call("GET", path, 1, "invalid", nil, 403)
	call("GET", path, 3, code, nil, 403)
	call("PATCH", path, 1, "", input, 403)
	data := call("GET", path, 1, code, nil, 200)
	card := data["card"].(map[string]any)
	if card["number"] != "************4242" || card["has_cvc"] != true {
		t.Fatal("编辑详情未正确脱敏")
	}
	call("GET", path, 1, code, nil, 403)
	input["edit_token"] = data["edit_token"]
	var numberBefore, cvcBefore string
	if err := db.QueryRow(`SELECT number_ciphertext,cvc_ciphertext FROM bank_cards WHERE id=1`).Scan(&numberBefore, &cvcBefore); err != nil {
		t.Fatal(err)
	}
	for _, number := range []any{nil, "", "  ", "4242 4242 4242 4242"} {
		delete(input, "number")
		if number != nil {
			input["number"] = number
		}
		input["cvc"], input["notes"] = " ", "Updated notes"
		call("PATCH", path, 1, "", input, 200)
		var numberAfter, cvcAfter, notes string
		if err := db.QueryRow(`SELECT number_ciphertext,cvc_ciphertext,notes FROM bank_cards WHERE id=1`).Scan(&numberAfter, &cvcAfter, &notes); err != nil || numberAfter != numberBefore || cvcAfter != cvcBefore || notes != "Updated notes" {
			t.Fatal("未修改的卡号或留空的 CVC 被覆盖", err)
		}
	}
	call("PATCH", path, 2, "", input, 403)
	call("PATCH", "/api/bank-cards/2", 1, "", input, 403)
	input["edit_token"] = data["edit_token"].(string) + "tampered"
	call("PATCH", path, 1, "", input, 403)
	input["edit_token"] = cardEditTestToken(t, s, 1, 1, time.Now().Add(-time.Minute))
	call("PATCH", path, 1, "", input, 403)
	input["edit_token"] = data["edit_token"]
	input["number"], input["cvc"] = "5555555555554444", "007"
	call("PATCH", path, 1, "", input, 200)
	var numberAfter, cvcAfter string
	if err := db.QueryRow(`SELECT number_ciphertext,cvc_ciphertext FROM bank_cards WHERE id=1`).Scan(&numberAfter, &cvcAfter); err != nil {
		t.Fatal(err)
	}
	if number, err := decryptSession(numberAfter); err != nil || number != "5555555555554444" {
		t.Fatal("新卡号未保存")
	}
	if cvc, err := decryptSession(cvcAfter); err != nil || cvc != "007" {
		t.Fatal("新 CVC 未保留前导零")
	}
	if _, err := db.Exec(`UPDATE users SET session_version=session_version+1,updated_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	call("PATCH", path, 1, "", input, 403)
	var leaked bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM operation_events WHERE after_data::text LIKE '%edit_token%' OR after_data::text LIKE '%0042%' OR after_data::text LIKE '%4242424242424242%')`).Scan(&leaked); err != nil || leaked {
		t.Fatal("审计泄露编辑凭证或卡片敏感信息", err)
	}
	// 编辑凭证不能替代登录凭证。
	r := httptest.NewRequest(http.MethodGet, "/api/bank-cards", nil)
	r.Header.Set("Authorization", "Bearer "+data["edit_token"].(string))
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("编辑凭证被当作登录凭证")
	}
}
