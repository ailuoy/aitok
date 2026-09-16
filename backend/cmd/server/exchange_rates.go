package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"time"
)

const exchangeRateURL = "https://open.er-api.com/v6/latest/PHP"
const exchangeRateSource = "https://www.exchangerate-api.com"

var exchangeTimezone = time.FixedZone("UTC+8", 8*60*60)

type ExchangeRate struct {
	ID          int64     `json:"id"`
	Rate        string    `json:"rate"`
	CNYRate     string    `json:"cny_rate,omitempty"`
	Source      string    `json:"source"`
	EffectiveAt time.Time `json:"effective_at"`
	SyncedAt    time.Time `json:"synced_at"`
}

type exchangeQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func latestExchangeRate(ctx context.Context, db exchangeQuery) (*ExchangeRate, error) {
	var rate ExchangeRate
	// 只组合相同同步批次的汇率，不能把今日 USD 和旧 CNY 混在一起。
	err := db.QueryRowContext(ctx, `SELECT u.id,u.rate::text,COALESCE(c.rate::text,''),u.source,u.effective_at,u.created_at FROM exchange_rates u LEFT JOIN LATERAL (SELECT rate FROM exchange_rates WHERE base_currency=u.base_currency AND quote_currency='CNY' AND source=u.source AND effective_at=u.effective_at AND created_at=u.created_at AND deleted_at IS NULL ORDER BY id DESC LIMIT 1) c ON TRUE WHERE u.base_currency='PHP' AND u.quote_currency='USD' AND u.deleted_at IS NULL ORDER BY u.created_at DESC,u.id DESC LIMIT 1`).Scan(&rate.ID, &rate.Rate, &rate.CNYRate, &rate.Source, &rate.EffectiveAt, &rate.SyncedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rate, nil
}

func (rate *ExchangeRate) fresh(now time.Time) bool {
	return rate != nil && !rate.EffectiveAt.After(now.Add(5*time.Minute)) && now.Sub(rate.EffectiveAt) <= 48*time.Hour && now.Sub(rate.SyncedAt) <= 48*time.Hour
}

// 金额和汇率用有理数计算，PHP/USD/CNY 均以分为最小单位，仅在最终金额四舍五入。
func convertMoney(amount int64, rate string) (int64, error) {
	r, ok := new(big.Rat).SetString(rate)
	if !ok || r.Sign() <= 0 || amount <= 0 || amount > maxCardMoneyMinor {
		return 0, errors.New("无效金额或汇率")
	}
	r.Mul(r, new(big.Rat).SetInt64(amount))
	numerator := new(big.Int).Mul(r.Num(), big.NewInt(2))
	numerator.Add(numerator, r.Denom())
	denominator := new(big.Int).Mul(r.Denom(), big.NewInt(2))
	rounded := new(big.Int).Quo(numerator, denominator)
	if !rounded.IsInt64() || rounded.Sign() <= 0 || rounded.Int64() > maxCardMoneyMinor {
		return 0, errors.New("折算金额超出范围")
	}
	return rounded.Int64(), nil
}

func pricePackage(pkg *RechargePackage, rate *ExchangeRate, now time.Time) {
	pkg.PriceReady = true
	pkg.ExchangeRate = nil
	pkg.SaleCNYMinor = nil
	pkg.CNYPriceReady = false
	if rate != nil && rate.CNYRate != "" {
		base, conversion := pkg.OriginalAmountMinor, rate.CNYRate
		if !pkg.AutoUSD {
			base = pkg.SaleUSDMinor
			cny, cnyOK := new(big.Rat).SetString(rate.CNYRate)
			usd, usdOK := new(big.Rat).SetString(rate.Rate)
			if cnyOK && usdOK && usd.Sign() > 0 {
				conversion = cny.Quo(cny, usd).RatString()
			} else {
				conversion = ""
			}
		}
		if !pkg.AutoUSD || pkg.Currency == "PHP" {
			if amount, err := convertMoney(base, conversion); err == nil {
				pkg.SaleCNYMinor = &amount
				pkg.CNYPriceReady = rate.fresh(now)
				pkg.ExchangeRate = rate
			}
		}
	}
	if !pkg.AutoUSD {
		return
	}
	pkg.SaleUSDMinor = 0
	pkg.PriceReady = false
	if rate == nil || pkg.Currency != "PHP" {
		return
	}
	amount, err := convertMoney(pkg.OriginalAmountMinor, rate.Rate)
	if err != nil {
		return
	}
	pkg.SaleUSDMinor = amount
	pkg.ExchangeRate = rate
	pkg.PriceReady = rate.fresh(now)
}

func fetchExchangeRate(ctx context.Context, client *http.Client, url string, now time.Time) (*ExchangeRate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("汇率接口状态 %d", response.StatusCode)
	}
	var data struct {
		Result  string                 `json:"result"`
		Base    string                 `json:"base_code"`
		Updated int64                  `json:"time_last_update_unix"`
		Rates   map[string]json.Number `json:"rates"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 128*1024))
	if err = decoder.Decode(&data); err != nil {
		return nil, errors.New("汇率响应格式无效")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("汇率响应包含额外数据")
	}
	r, ok := new(big.Rat).SetString(data.Rates["USD"].String())
	if data.Result != "success" || data.Base != "PHP" || !ok || r.Cmp(big.NewRat(1, 1000)) < 0 || r.Cmp(big.NewRat(1, 10)) > 0 {
		return nil, errors.New("汇率币种或数值无效")
	}
	cny, cnyOK := new(big.Rat).SetString(data.Rates["CNY"].String())
	if !cnyOK || cny.Cmp(big.NewRat(1, 100)) < 0 || cny.Cmp(big.NewRat(1, 1)) > 0 {
		return nil, errors.New("人民币汇率缺失或数值无效")
	}
	rate := &ExchangeRate{Rate: r.FloatString(12), CNYRate: cny.FloatString(12), Source: exchangeRateSource, EffectiveAt: time.Unix(data.Updated, 0).UTC(), SyncedAt: now}
	if !rate.fresh(now) {
		return nil, errors.New("汇率数据已过期或时间异常")
	}
	return rate, nil
}

func exchangeSchedule(now time.Time) time.Time {
	local := now.In(exchangeTimezone)
	scheduled := time.Date(local.Year(), local.Month(), local.Day(), 9, 0, 0, 0, exchangeTimezone)
	if now.Before(scheduled) {
		scheduled = scheduled.AddDate(0, 0, -1)
	}
	return scheduled
}

func (s *Server) syncExchangeRate(ctx context.Context, now time.Time, fetch func(context.Context) (*ExchangeRate, error)) error {
	return s.syncExchangeRateAfter(ctx, now, exchangeSchedule(now), fetch)
}

func (s *Server) syncExchangeRateAfter(ctx context.Context, now, since time.Time, fetch func(context.Context) (*ExchangeRate, error)) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtext('daily-php-usd-rate'))`).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return errors.New("其他实例正在同步汇率，稍后复查")
	}
	last, err := latestExchangeRate(ctx, tx)
	if err != nil {
		return err
	}
	if last.fresh(now) && last.CNYRate != "" && !last.SyncedAt.Before(since) {
		return nil
	}
	rate, err := fetch(ctx)
	if err != nil {
		return err
	}
	if rate == nil || rate.CNYRate == "" || !rate.fresh(now) || (last != nil && rate.EffectiveAt.Before(last.EffectiveAt)) {
		return errors.New("拒绝无效或倒退的汇率")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO exchange_rates(base_currency,quote_currency,rate,source,effective_at,created_at,updated_at) VALUES('PHP','USD',$1,$3,$4,$5,$5),('PHP','CNY',$2,$3,$4,$5,$5)`, rate.Rate, rate.CNYRate, rate.Source, rate.EffectiveAt, now)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// 启动时补同步，之后每天 UTC+8 09:00 同步；失败每 30 分钟重试，保留上一条有效记录。
func (s *Server) runExchangeRates(ctx context.Context) {
	client := &http.Client{Timeout: 10 * time.Second}
	for ctx.Err() == nil {
		now := time.Now()
		batch, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := s.syncExchangeRate(batch, now, func(call context.Context) (*ExchangeRate, error) {
			return fetchExchangeRate(call, client, exchangeRateURL, now)
		})
		cancel()
		delay := time.Until(exchangeSchedule(now).AddDate(0, 0, 1))
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("exchange rate sync failed: %v", err)
			}
			delay = 30 * time.Minute
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
