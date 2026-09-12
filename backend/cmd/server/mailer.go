package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type cloudflareMailer struct {
	provider, accountID, apiToken, apiBaseURL, fromAddress, fromName string
	client                                                           *http.Client
}

func newMailer() *cloudflareMailer {
	return &cloudflareMailer{
		provider:    envDefault("MAIL_PROVIDER", "cloudflare"),
		accountID:   strings.TrimSpace(os.Getenv("CLOUDFLARE_ACCOUNT_ID")),
		apiToken:    strings.TrimSpace(os.Getenv("CLOUDFLARE_EMAIL_API_TOKEN")),
		apiBaseURL:  strings.TrimRight(envDefault("CLOUDFLARE_EMAIL_API_BASE_URL", "https://api.cloudflare.com/client/v4"), "/"),
		fromAddress: envDefault("MAIL_FROM_ADDRESS", "no-reply@toktopup.com"),
		fromName:    envDefault("MAIL_FROM_NAME", "AiTok"),
		client:      &http.Client{Timeout: 10 * time.Second},
	}
}

func codeExpiryMinutes() int {
	minutes, err := strconv.Atoi(envDefault("MAIL_CODE_EXPIRE_MINUTES", "10"))
	if err != nil || minutes < 1 || minutes > 60 {
		return 10
	}
	return minutes
}

func (m *cloudflareMailer) sendCode(ctx context.Context, to, code string, minutes int) error {
	if m.provider != "cloudflare" || m.accountID == "" || m.apiToken == "" || !validEmail(m.fromAddress) {
		return errors.New("mail configuration is incomplete")
	}
	// 与 google-maps 保持相同的 Cloudflare 请求结构。
	var from any = m.fromAddress
	if m.fromName != "" {
		from = map[string]string{"address": m.fromAddress, "name": m.fromName}
	}
	message := fmt.Sprintf("你的验证码是 %s，%d 分钟内有效。如非本人操作，请忽略。", code, minutes)
	body, err := json.Marshal(map[string]any{"to": to, "from": from, "subject": "AiTok 验证码", "text": message, "html": "<p>" + message + "</p>"})
	if err != nil {
		return err
	}
	endpoint := m.apiBaseURL + "/accounts/" + url.PathEscape(m.accountID) + "/email/sending/send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.apiToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !result.Success {
		return fmt.Errorf("cloudflare email send failed: status %d", resp.StatusCode)
	}
	return nil
}
