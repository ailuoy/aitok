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

// 编辑凭证只用于验证后的当前卡片保存，不得替代登录或助手凭证。
func (s *Server) bankCardEditToken(r *http.Request, user, card int64, expires time.Time) (string, error) {
	stamp, err := s.sessionStamp(r.Context(), user)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("card-edit:%d:%d:%d:%s", user, card, expires.Unix(), stamp)))
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Server) validBankCardEditToken(r *http.Request, user, card int64, token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	fields := strings.Split(string(raw), ":")
	if len(fields) != 5 || fields[0] != "card-edit" || fields[1] != strconv.FormatInt(user, 10) || fields[2] != strconv.FormatInt(card, 10) {
		return false
	}
	expires, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || time.Now().Unix() >= expires {
		return false
	}
	expected, err := s.bankCardEditToken(r, user, card, time.Unix(expires, 0))
	return err == nil && hmac.Equal([]byte(token), []byte(expected))
}
