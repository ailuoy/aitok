package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 独立作用域的短期只读凭证，仅交给本机进程，不注入 ChatGPT 页面。
func (s *Server) assistantToken(user, account int64) string {
	body := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("assistant:%d:%d:%d", user, account, time.Now().Add(12*time.Hour).Unix())))
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) browserAssistant(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
	unauthorized := func() { reply(w, map[string]string{"error": "助手授权已过期，请重新打开账号"}, 401) }
	if len(parts) != 2 {
		unauthorized()
		return
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(parts[0]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		unauthorized()
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		unauthorized()
		return
	}
	var user, account, expiry int64
	if _, err = fmt.Sscanf(string(raw), "assistant:%d:%d:%d", &user, &account, &expiry); err != nil || time.Now().Unix() > expiry {
		unauthorized()
		return
	}
	var exists bool
	if err = s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM chatgpt_accounts WHERE id=$1 AND deleted_at IS NULL AND user_id=$2 AND EXISTS(SELECT 1 FROM users WHERE id=$2 AND deleted_at IS NULL))`, account, user).Scan(&exists); err != nil || !exists {
		unauthorized()
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/browser-assistant/cards/") {
		id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/browser-assistant/cards/"), 10, 64)
		if err != nil || id < 1 {
			http.NotFound(w, r)
			return
		}
		c, err := s.readCard(r, user, id, false)
		if err != nil {
			cardError(w, err)
			return
		}
		reply(w, map[string]any{"card": c}, 200)
		return
	}
	if r.URL.Path != "/api/browser-assistant" {
		http.NotFound(w, r)
		return
	}
	cards := []BankCard{}
	rows, err := s.db.QueryContext(r.Context(), `SELECT `+bankCardColumns+` FROM bank_cards WHERE deleted_at IS NULL AND user_id=$1 ORDER BY id DESC LIMIT 500`, user)
	if err != nil {
		cardError(w, err)
		return
	}
	for rows.Next() {
		c, e := scanBankCard(rows)
		if e != nil {
			rows.Close()
			cardError(w, e)
			return
		}
		cards = append(cards, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		cardError(w, err)
		return
	}
	addresses := []Address{}
	rows, err = s.db.QueryContext(r.Context(), `SELECT `+addressColumns+` FROM addresses WHERE deleted_at IS NULL AND (user_id=$1 OR user_id IS NULL) ORDER BY id DESC LIMIT 1000`, user)
	if err != nil {
		addressError(w, err)
		return
	}
	for rows.Next() {
		a, e := scanAddress(rows)
		if e != nil {
			rows.Close()
			addressError(w, e)
			return
		}
		addresses = append(addresses, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		addressError(w, err)
		return
	}
	reply(w, map[string]any{"cards": cards, "addresses": addresses}, 200)
}
