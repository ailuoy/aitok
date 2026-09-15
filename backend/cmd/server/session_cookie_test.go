package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSessionLoginCookieExport(t *testing.T) {
	session, err := parseChatGPTSession(`{"accessToken":"test","sessionToken":"cookie-secret","cookies":[{"name":"other","value":"private"},{"name":"__Secure-next-auth.session-token.0","value":"chunk","domain":".chatgpt.com"},{"name":"__Secure-next-auth.session-token.1","value":"foreign","domain":"example.com"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(session.BrowserJSON)
	if !strings.Contains(string(raw), "cookie-secret") || !strings.Contains(string(raw), "chunk") || strings.Contains(string(raw), "private") || strings.Contains(string(raw), "foreign") {
		t.Fatal("登录 Cookie 导出范围错误")
	}
	for _, raw := range []string{`{"accessToken":"test","sessionToken":"bad;cookie"}`, `{"accessToken":"test","sessionToken":123}`, `{"accessToken":"test","cookies":{}}`} {
		if _, err := parseChatGPTSession(raw); err == nil {
			t.Fatal("接受了无效 Cookie")
		}
	}
}
