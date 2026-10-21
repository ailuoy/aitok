package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAccountSessionExpiryMetadata(t *testing.T) {
	db := walletTestDB(t)
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("e", 32))))
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(1,'member@test.local',''),(2,'__superadmin__','')`); err != nil {
		t.Fatal(err)
	}
	claims := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":2000000000}`))
	fixtures := []struct {
		raw     string
		wrapped bool
		want    string
	}{
		{`{"accessToken":"header.` + claims + `.sig","expires":"2099-01-01T00:00:00Z","sessionToken":"private-cookie"}`, false, time.Unix(2000000000, 0).UTC().Format(time.RFC3339)},
		{`{"accessToken":"header.` + claims + `.sig","expires":"2020-01-01T00:00:00Z"}`, true, "2020-01-01T00:00:00Z"},
		{`{"accessToken":"opaque-token"}`, false, ""},
		{`invalid session`, false, ""},
	}
	for i, fixture := range fixtures {
		encrypted, err := encryptSession(fixture.raw)
		if fixture.wrapped {
			encrypted, err = encodeAccountCredentials(accountCredentials{SessionJSON: fixture.raw, ProxyURL: "socks5://user:private-proxy@host:1080"})
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO chatgpt_accounts(id,user_id,label,email,session_ciphertext) VALUES($1,1,'fixture',$2,$3)`, i+1, "account"+string(rune('a'+i))+"@test.local", encrypted); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(5,1,'empty','empty@test.local'),(6,1,'corrupt','corrupt@test.local'); UPDATE chatgpt_accounts SET session_ciphertext='broken-ciphertext' WHERE id=6`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("metadata-tests")}
	for _, user := range []int64{1, 2} {
		for _, path := range []string{"/api/accounts", "/api/accounts?paged=1", "/api/me"} {
			r := httptest.NewRequest("GET", path, nil)
			r.Header.Set("Authorization", "Bearer "+s.token(user))
			w := httptest.NewRecorder()
			s.routes().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("%s: got %d", path, w.Code)
			}
			for _, secret := range []string{"accessToken", "private-cookie", "private-proxy", "session_ciphertext", "broken-ciphertext"} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatal("账号列表泄露 Session 凭据")
				}
			}
			var result struct {
				Accounts []map[string]json.RawMessage `json:"accounts"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Accounts) != 6 {
				t.Fatal("账号列表读取失败", err)
			}
			for _, account := range result.Accounts {
				field, exists := account["session_expires_at"]
				if user == 1 {
					if exists {
						t.Fatal("普通用户不应收到 Session 管理字段")
					}
					continue
				}
				if !exists {
					t.Fatal("管理员列表缺少到期时间")
				}
				var id int
				_ = json.Unmarshal(account["id"], &id)
				var got *string
				if err := json.Unmarshal(field, &got); err != nil {
					t.Fatal(err)
				}
				if id <= 2 {
					if got == nil || *got != fixtures[id-1].want {
						t.Fatal("到期时间应取 JWT 与 expires 的较早值")
					}
				} else if got != nil {
					t.Fatal("无法解析的到期时间应返回 null")
				}
			}
		}
	}
}
