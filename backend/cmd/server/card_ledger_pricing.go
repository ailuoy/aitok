package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

type cardLedgerPricing struct {
	Package           RechargePackage `json:"package"`
	Mode              string          `json:"mode"`
	ChargeCurrency    string          `json:"charge_currency"`
	ChargeAmountMinor int64           `json:"charge_amount_minor"`
	AmountUSDMinor    int64           `json:"amount_usd_minor"`
	ExchangeRate      *CollectionRate `json:"exchange_rate,omitempty"`
	ConfirmedAt       time.Time       `json:"confirmed_at"`
}

func priceCardSubscription(ctx context.Context, db exchangeQuery, packageID int64, mode, amount string) (*cardLedgerPricing, error) {
	var raw json.RawMessage
	if err := db.QueryRowContext(ctx, `SELECT to_jsonb(p) FROM recharge_packages p WHERE id=$1 AND enabled AND deleted_at IS NULL FOR SHARE`, packageID).Scan(&raw); err != nil {
		return nil, err
	}
	var pkg RechargePackage
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return nil, err
	}
	now := time.Now()
	var batch *ExchangeRate
	if pkg.AutoUSD || mode == "CNY" {
		var err error
		batch, err = latestExchangeRate(ctx, db)
		if err != nil {
			return nil, err
		}
	}
	pricePackage(&pkg, batch, now)
	pricing := &cardLedgerPricing{Package: pkg, Mode: mode, ChargeCurrency: "USD", ConfirmedAt: now}
	if mode == "package" {
		if !pkg.PriceReady || pkg.SaleUSDMinor <= 0 || pkg.SaleUSDMinor > maxCardMoneyMinor {
			return nil, operationConflict("套餐价格尚未就绪，请更新汇率后重试")
		}
		pricing.ChargeAmountMinor, pricing.AmountUSDMinor = pkg.SaleUSDMinor, pkg.SaleUSDMinor
		return pricing, nil
	}
	minor, ok := parseCardUSD(amount)
	if !ok || (mode != "USD" && mode != "CNY") {
		return nil, operationConflict("请选择扣款方式并输入大于零、最多两位小数的金额")
	}
	rate, err := collectionRate(mode, batch, now)
	if err != nil {
		return nil, operationConflict(err.Error())
	}
	usd, err := convertMoney(minor, rate.USDPerUnit)
	if err != nil {
		return nil, err
	}
	pricing.ChargeCurrency, pricing.ChargeAmountMinor, pricing.AmountUSDMinor, pricing.ExchangeRate = mode, minor, usd, rate
	return pricing, nil
}

func (p *cardLedgerPricing) rateID() int64 {
	if p.ExchangeRate != nil && p.ExchangeRate.Batch != nil {
		return p.ExchangeRate.Batch.ID
	}
	return 0
}

func (s *Server) cardLedgerQuote(w http.ResponseWriter, r *http.Request, user, cardID int64, admin bool) {
	var exists bool
	if err := s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM bank_cards WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR $3))`, cardID, user, admin).Scan(&exists); err != nil {
		cardError(w, err)
		return
	}
	if !exists {
		http.NotFound(w, r)
		return
	}
	query := r.URL.Query()
	packageID, err := strconv.ParseInt(query.Get("package_id"), 10, 64)
	mode, amount := query.Get("charge_mode"), query.Get("charge_amount")
	_, validAmount := parseCardUSD(amount)
	if err != nil || packageID < 1 || (mode != "package" && mode != "USD" && mode != "CNY") || (mode == "package" && amount != "") || (mode != "package" && !validAmount) {
		reply(w, map[string]string{"error": "请选择套餐和有效扣款金额"}, 400)
		return
	}
	if mode == "CNY" {
		if _, err := s.currentCollectionRate(r.Context(), mode); err != nil {
			reply(w, map[string]string{"error": err.Error()}, 503)
			return
		}
	}
	pricing, err := priceCardSubscription(r.Context(), s.db, packageID, mode, amount)
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, pricing, 200)
}
