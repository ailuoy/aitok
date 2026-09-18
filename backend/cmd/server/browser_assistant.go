package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 独立作用域的短期凭证，只读卡片/地址并更新绑定账号的 Session，仅交给本机进程。
func (s *Server) assistantToken(user, account int64) string {
	stamp, err := s.sessionStamp(context.Background(), user)
	if err != nil {
		return ""
	}
	body := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("assistant-v2:%d:%d:%d:%s", user, account, time.Now().Add(12*time.Hour).Unix(), stamp)))
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *Server) browserAssistant(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	updateSession := r.URL.Path == "/api/browser-assistant/session" && r.Method == http.MethodPost
	if r.Method != "GET" && !updateSession {
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
	var stamp string
	scope := "assistant"
	if strings.HasPrefix(string(raw), "assistant-v2:") {
		scope = "assistant-v2"
	}
	if _, err = fmt.Sscanf(string(raw), scope+":%d:%d:%d:%s", &user, &account, &expiry, &stamp); err != nil || time.Now().Unix() > expiry {
		unauthorized()
		return
	}
	if updateSession {
		aw := &auditWriter{ResponseWriter: w}
		w = aw
		defer func() {
			if aw.status < 400 {
				return
			}
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
			defer cancel()
			_, err := s.db.ExecContext(ctx, `INSERT INTO operation_events(actor_id,entity_type,entity_id,action,request_key,after_data) VALUES($1,'account',$2,'assistant_session_update',$3,jsonb_build_object('source','browser_assistant','result','failure','status',$4::int))`, user, account, eventKey(), aw.status)
			if err != nil {
				log.Print("assistant session audit write failed")
			}
		}()
	}
	current, e := s.sessionStamp(r.Context(), user)
	if e != nil || !hmac.Equal([]byte(stamp), []byte(current)) {
		unauthorized()
		return
	}
	if !s.permitted(r.Context(), user, "accounts") {
		unauthorized()
		return
	}
	if updateSession {
		if scope != "assistant-v2" {
			reply(w, map[string]string{"error": "请从后台重新打开账号以启用 Session 更新"}, 403)
			return
		}
		// 账号 ID 只取自已签名凭证，不接受页面指定其他账号。
		s.accountSession(w, r, user, account, false)
		return
	}
	var paymentCardID, billingAddressID *int64
	var notes string
	if err = s.db.QueryRowContext(r.Context(), `SELECT payment_card_id,billing_address_id,COALESCE(to_jsonb(a)->>'notes','') FROM chatgpt_accounts a WHERE id=$1 AND deleted_at IS NULL AND EXISTS(SELECT 1 FROM users WHERE id=$2 AND deleted_at IS NULL)`, account, user).Scan(&paymentCardID, &billingAddressID, &notes); err != nil {
		unauthorized()
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/browser-assistant/cards/") {
		id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/browser-assistant/cards/"), 10, 64)
		if err != nil || id < 1 {
			http.NotFound(w, r)
			return
		}
		if paymentCardID != nil && id != *paymentCardID {
			reply(w, map[string]string{"error": "请使用账号已绑定的付款卡"}, 409)
			return
		}
		c, err := s.readCard(r, user, id, true)
		if err != nil {
			cardError(w, err)
			return
		}
		var usable bool
		err = s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM bank_cards WHERE id=$1 AND deleted_at IS NULL AND status='active' AND (exp_year,exp_month)>=(EXTRACT(YEAR FROM NOW())::int,EXTRACT(MONTH FROM NOW())::int))`, id).Scan(&usable)
		if err != nil || !usable {
			reply(w, map[string]string{"error": "卡片已冻结、失效或过期，请选择其他卡片"}, 409)
			return
		}
		role, roleErr := s.role(r.Context(), user)
		if roleErr != nil || (role == "admin" && !s.permitted(r.Context(), user, "card_numbers")) {
			w.WriteHeader(403)
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
	rows, err := s.db.QueryContext(r.Context(), `SELECT `+bankCardColumns+` FROM bank_cards WHERE deleted_at IS NULL AND status='active' AND (exp_year,exp_month)>=(EXTRACT(YEAR FROM NOW())::int,EXTRACT(MONTH FROM NOW())::int) AND ($1::bigint IS NULL OR id=$1) ORDER BY id DESC LIMIT 500`, paymentCardID)
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
	rows, err = s.db.QueryContext(r.Context(), `SELECT `+addressColumns+` FROM addresses WHERE deleted_at IS NULL AND ($1::bigint IS NULL OR id=$1) AND NOT EXISTS(SELECT 1 FROM chatgpt_accounts a WHERE a.billing_address_id=addresses.id AND a.deleted_at IS NULL AND a.id<>$2) ORDER BY id DESC LIMIT 1000`, billingAddressID, account)
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
	reply(w, map[string]any{"cards": cards, "addresses": addresses, "payment_card_id": paymentCardID, "billing_address_id": billingAddressID, "notes": notes}, 200)
}
