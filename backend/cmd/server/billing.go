package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type billingConfig struct {
	SecretKey, WebhookSecret, BaseURL string
	Price1ID, Price100ID              string
	TokensPerUSD, RenewalCost         int64
	RenewalMonths                     int
}

func loadBillingConfig() (billingConfig, error) {
	c := billingConfig{SecretKey: os.Getenv("STRIPE_SECRET_KEY"), WebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"), BaseURL: strings.TrimRight(envDefault("APP_BASE_URL", "http://localhost:15680"), "/")}
	c.Price1ID = strings.TrimSpace(os.Getenv("STRIPE_1_PRICE_ID"))
	c.Price100ID = strings.TrimSpace(os.Getenv("STRIPE_100_PRICE_ID"))
	for _, v := range []struct {
		key, fallback string
		target        *int64
		max           int64
	}{
		{"TOKENS_PER_USD", "1", &c.TokensPerUSD, 1000000},
		{"RENEWAL_TOKEN_COST", "20", &c.RenewalCost, 1000000000},
	} {
		n, err := strconv.ParseInt(envDefault(v.key, v.fallback), 10, 64)
		if err != nil || n <= 0 || n > v.max {
			return c, fmt.Errorf("%s 必须为 1 到 %d 的整数", v.key, v.max)
		}
		*v.target = n
	}
	months, err := strconv.Atoi(envDefault("RENEWAL_MONTHS", "1"))
	if err != nil || months < 1 || months > 12 {
		return c, fmt.Errorf("RENEWAL_MONTHS 必须在 1 到 12 之间")
	}
	c.RenewalMonths = months
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
		return c, fmt.Errorf("APP_BASE_URL 必须是有效 HTTP(S) 地址")
	}
	return c, nil
}

func (c billingConfig) stripeEnabled() bool {
	return c.SecretKey != "" && c.WebhookSecret != "" && c.Price1ID != "" && c.Price100ID != ""
}

type topupOption struct {
	PriceID     string `json:"price_id"`
	AmountMinor int64  `json:"amount_minor"`
	Tokens      int64  `json:"tokens"`
}

func (c billingConfig) options() []topupOption {
	result := []topupOption{}
	for _, item := range []struct {
		usd     int64
		priceID string
	}{{1, c.Price1ID}, {100, c.Price100ID}} {
		result = append(result, topupOption{PriceID: item.priceID, AmountMinor: item.usd * 100, Tokens: item.usd * c.TokensPerUSD})
	}
	return result
}

var requestKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{8,100}$`)

func newOrderNo() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "AITOK-" + hex.EncodeToString(b), nil
}
