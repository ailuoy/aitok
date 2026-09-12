package main

import (
	"context"
	"testing"

	stripe "github.com/stripe/stripe-go/v82"
)

func TestConfiguredTopupPrices(t *testing.T) {
	t.Setenv("STRIPE_1_PRICE_ID", "price_test_1")
	t.Setenv("STRIPE_100_PRICE_ID", "price_test_100")
	t.Setenv("TOKENS_PER_USD", "1")
	c, err := loadBillingConfig()
	if err != nil {
		t.Fatal(err)
	}
	options := c.options()
	if len(options) != 2 || options[0].PriceID != "price_test_1" || options[0].AmountMinor != 100 || options[0].Tokens != 1 || options[1].PriceID != "price_test_100" || options[1].AmountMinor != 10000 || options[1].Tokens != 100 {
		t.Fatal("充值配置与两档 Price 不匹配")
	}
	f := &fakeStripe{}
	for _, option := range options {
		price, _ := f.RetrievePrice(context.Background(), option.PriceID)
		if !validTopupPrice(price, option.PriceID, option.AmountMinor) {
			t.Fatal("正确价格被拒绝")
		}
		price.UnitAmount++
		if validTopupPrice(price, option.PriceID, option.AmountMinor) {
			t.Fatal("错误金额未拒绝")
		}
		price.UnitAmount = option.AmountMinor
		price.Active = false
		if validTopupPrice(price, option.PriceID, option.AmountMinor) {
			t.Fatal("已归档价格未拒绝")
		}
		price.Active = true
		price.Currency = stripe.CurrencyEUR
		if validTopupPrice(price, option.PriceID, option.AmountMinor) {
			t.Fatal("错误币种未拒绝")
		}
		price.Currency = stripe.CurrencyUSD
		price.Type = stripe.PriceTypeRecurring
		if validTopupPrice(price, option.PriceID, option.AmountMinor) {
			t.Fatal("周期订阅价格未拒绝")
		}
	}
}
