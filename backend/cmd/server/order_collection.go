package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"time"
)

type CollectionRate struct {
	USDPerUnit string        `json:"usd_per_unit"`
	Batch      *ExchangeRate `json:"batch,omitempty"`
}

type OrderProfit struct {
	USDMinor      int64  `json:"usd_minor"`
	ReceivedMinor int64  `json:"received_minor"`
	RatePercent   string `json:"rate_percent"`
	CostUSDMinor  int64  `json:"cost_usd_minor"`
	Estimated     bool   `json:"estimated"`
}

type CollectionQuote struct {
	Currency     string          `json:"currency"`
	AmountMinor  int64           `json:"amount_minor"`
	USDMinor     int64           `json:"usd_minor"`
	ExchangeRate *CollectionRate `json:"exchange_rate"`
	Profit       *OrderProfit    `json:"profit"`
}

// 收款采用当天已同步的数据；09:00 之后还需满足当天定时同步批次。
func collectionRateSince(now time.Time) time.Time {
	local := now.In(exchangeTimezone)
	since := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, exchangeTimezone)
	if scheduled := exchangeSchedule(now); scheduled.After(since) {
		return scheduled
	}
	return since
}

func collectionRate(currency string, rate *ExchangeRate, now time.Time) (*CollectionRate, error) {
	if currency == "USD" {
		return &CollectionRate{USDPerUnit: "1"}, nil
	}
	if currency != "CNY" {
		return nil, errors.New("收款币种仅支持 CNY 或 USD")
	}
	if !rate.fresh(now) || rate.SyncedAt.Before(collectionRateSince(now)) || rate.SyncedAt.After(now.Add(5*time.Minute)) {
		return nil, errors.New("当天汇率尚未就绪，请重新获取汇率")
	}
	usd, usdOK := new(big.Rat).SetString(rate.Rate)
	cny, cnyOK := new(big.Rat).SetString(rate.CNYRate)
	if !usdOK || !cnyOK || usd.Sign() <= 0 || cny.Sign() <= 0 {
		return nil, errors.New("人民币汇率无效")
	}
	return &CollectionRate{USDPerUnit: new(big.Rat).Quo(usd, cny).RatString(), Batch: rate}, nil
}

func (s *Server) currentCollectionRate(ctx context.Context, currency string) (*CollectionRate, error) {
	now := time.Now()
	if currency == "USD" {
		return collectionRate(currency, nil, now)
	}
	if currency != "CNY" {
		return nil, errors.New("收款币种仅支持 CNY 或 USD")
	}
	rate, err := latestExchangeRate(ctx, s.db)
	if err != nil {
		return nil, err
	}
	if result, err := collectionRate(currency, rate, now); err == nil {
		return result, nil
	}
	err = s.syncExchangeRateAfter(ctx, now, collectionRateSince(now), func(call context.Context) (*ExchangeRate, error) {
		if s.exchangeRateFetch != nil {
			return s.exchangeRateFetch(call, now)
		}
		return fetchExchangeRate(call, &http.Client{Timeout: 10 * time.Second}, exchangeRateURL, now)
	})
	if err != nil {
		return nil, errors.New("当天汇率获取失败，请稍后重新计算")
	}
	rate, err = latestExchangeRate(ctx, s.db)
	if err != nil {
		return nil, err
	}
	return collectionRate(currency, rate, time.Now())
}

// 利润使用整数分及有理数计算，毛利率的分母为实收收入，不是成本。
func calculateOrderProfit(o RechargeOrder) *OrderProfit {
	if o.ReceivedUSDMinor <= 0 || o.ReceivedAmountMinor <= 0 || o.ReceivedExchangeRate == nil || o.PaymentMethod == "wallet" || o.OrderStatus != "active" || o.RefundedUSDMinor > 0 || o.PaymentStatus == "refunded" {
		return nil
	}
	cost := o.CostUSDMinor
	estimated := cost == 0
	if estimated {
		cost = o.SaleUSDMinor
	}
	profit := o.ReceivedUSDMinor - cost
	margin := new(big.Rat).Mul(big.NewRat(profit, o.ReceivedUSDMinor), big.NewRat(100, 1)).FloatString(2)
	// 用保存的汇率将成本还原为收款币种，实收减成本，避免汇兑来回舍入。
	r, ok := new(big.Rat).SetString(o.ReceivedExchangeRate.USDPerUnit)
	if !ok || r.Sign() <= 0 {
		return nil
	}
	costInReceived, err := convertMoney(cost, new(big.Rat).Inv(r).RatString())
	if err != nil {
		return nil
	}
	return &OrderProfit{USDMinor: profit, ReceivedMinor: o.ReceivedAmountMinor - costInReceived, RatePercent: margin, CostUSDMinor: cost, Estimated: estimated}
}

func quoteCollection(o RechargeOrder, currency, amount string, rate *CollectionRate) (*CollectionQuote, error) {
	minor, ok := parseCardUSD(amount)
	if !ok {
		return nil, errors.New("请输入大于零、最多两位小数的实收金额")
	}
	usd, err := convertMoney(minor, rate.USDPerUnit)
	if err != nil {
		return nil, err
	}
	o.ReceivedCurrency, o.ReceivedAmountMinor, o.ReceivedUSDMinor = currency, minor, usd
	o.ReceivedExchangeRate, o.PaymentMethod = rate, "manual"
	profit := calculateOrderProfit(o)
	if profit == nil {
		return nil, errors.New("订单成本无法折算，请核对订单金额")
	}
	return &CollectionQuote{Currency: currency, AmountMinor: minor, USDMinor: usd, ExchangeRate: rate, Profit: profit}, nil
}

func decimalMoney(minor int64) string {
	sign := ""
	if minor < 0 {
		sign, minor = "-", -minor
	}
	return fmt.Sprintf("%s%d.%02d", sign, minor/100, minor%100)
}

func orderWithProfit(raw json.RawMessage) (json.RawMessage, error) {
	var o RechargeOrder
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, err
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	profit, err := json.Marshal(calculateOrderProfit(o))
	if err != nil {
		return nil, err
	}
	result["profit"] = profit
	return json.Marshal(result)
}

func (s *Server) orderCollectionQuote(w http.ResponseWriter, r *http.Request, user, id int64) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	if !s.permitted(r.Context(), user, "finance") {
		w.WriteHeader(403)
		return
	}
	currency, amount := r.URL.Query().Get("currency"), r.URL.Query().Get("amount")
	if _, ok := parseCardUSD(amount); !ok || (currency != "CNY" && currency != "USD") {
		reply(w, map[string]string{"error": "请选择 CNY / USD 并输入有效实收金额"}, 400)
		return
	}
	var raw json.RawMessage
	var o RechargeOrder
	if id == 0 {
		packageID, err := strconv.ParseInt(r.URL.Query().Get("package_id"), 10, 64)
		if err != nil || packageID < 1 {
			reply(w, map[string]string{"error": "请选择套餐"}, 400)
			return
		}
		err = s.db.QueryRowContext(r.Context(), `SELECT to_jsonb(p) FROM recharge_packages p WHERE id=$1 AND enabled AND deleted_at IS NULL`, packageID).Scan(&raw)
		if err != nil {
			operationError(w, err)
			return
		}
		var pkg RechargePackage
		if err = json.Unmarshal(raw, &pkg); err != nil {
			operationError(w, err)
			return
		}
		var batch *ExchangeRate
		if pkg.AutoUSD {
			batch, err = latestExchangeRate(r.Context(), s.db)
			if err != nil {
				operationError(w, err)
				return
			}
		}
		pricePackage(&pkg, batch, time.Now())
		if !pkg.PriceReady {
			reply(w, map[string]string{"error": "套餐价格尚未就绪，请刷新后重试"}, 503)
			return
		}
		o = RechargeOrder{OrderStatus: "active", PaymentStatus: "unpaid", SaleUSDMinor: pkg.SaleUSDMinor}
	} else {
		err := s.db.QueryRowContext(r.Context(), `SELECT to_jsonb(o)-'evidence' FROM recharge_orders o WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&raw)
		if err != nil {
			operationError(w, err)
			return
		}
		if err = json.Unmarshal(raw, &o); err != nil {
			operationError(w, err)
			return
		}
	}
	if o.OrderStatus != "active" || o.PaymentStatus != "unpaid" || o.FulfillmentStatus == "cancelled" {
		reply(w, map[string]string{"error": "订单已收款或结束，不能再次确认收款"}, 409)
		return
	}
	rate, err := s.currentCollectionRate(r.Context(), currency)
	if err != nil {
		reply(w, map[string]string{"error": err.Error()}, 503)
		return
	}
	quote, err := quoteCollection(o, currency, amount, rate)
	if err != nil {
		reply(w, map[string]string{"error": err.Error()}, 400)
		return
	}
	reply(w, quote, 200)
}
