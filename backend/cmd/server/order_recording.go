package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// 一次录入：未收款订单必须填写实收，订单、卡片扣款和审计共用调用方事务。
type orderRecording struct {
	OrderSource      string `json:"order_source,omitempty"`
	CardID           int64  `json:"card_id"`
	Reference        string `json:"reference"`
	Evidence         string `json:"evidence"`
	ReceivedCurrency string `json:"received_currency"`
	ReceivedAmount   string `json:"received_amount"`
	CollectionRateID int64  `json:"collection_rate_id"`
}

func (in orderRecording) validate() error {
	if utf8.RuneCountInString(in.OrderSource) > 80 {
		return errors.New("订单来源不能超过 80 字")
	}
	if in.CardID < 1 || strings.TrimSpace(in.Reference) == "" || len(in.Reference) > 200 || strings.TrimSpace(in.Evidence) == "" {
		return errors.New("请选择付款卡，填写交易号及凭据")
	}
	if err := validateEvidence(in.Evidence); err != nil {
		return err
	}
	if in.ReceivedAmount == "" {
		if in.ReceivedCurrency != "" || in.CollectionRateID != 0 {
			return errors.New("请填写实收金额")
		}
	} else {
		if _, ok := parseCardUSD(in.ReceivedAmount); !ok || (in.ReceivedCurrency != "CNY" && in.ReceivedCurrency != "USD") {
			return errors.New("请填写有效的 CNY / USD 实收金额")
		}
	}
	return nil
}

func applyReceipt(r *http.Request, tx *sql.Tx, o *RechargeOrder, currency, amount string, rateID int64) error {
	var batch *ExchangeRate
	var err error
	if currency == "CNY" {
		batch, err = latestExchangeRate(r.Context(), tx)
		if err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	rate, err := collectionRate(currency, batch, now)
	if err != nil {
		return err
	}
	if (currency == "CNY" && (batch == nil || batch.ID != rateID)) || (currency == "USD" && rateID != 0) {
		return errors.New("汇率已更新，请重新计算并确认实收金额")
	}
	quote, err := quoteCollection(*o, currency, amount, rate)
	if err != nil {
		return err
	}
	o.ReceivedCurrency, o.ReceivedAmountMinor, o.ReceivedUSDMinor = quote.Currency, quote.AmountMinor, quote.USDMinor
	o.ReceivedExchangeRate, o.ReceivedAt = rate, &now
	o.PaymentMethod, o.PaymentStatus = "manual", "paid"
	return nil
}

func recordOrderPosting(r *http.Request, tx *sql.Tx, user int64, o *RechargeOrder, in orderRecording, key string) error {
	if o.PaymentStatus == "unpaid" && strings.TrimSpace(in.ReceivedAmount) == "" {
		return operationConflict("请填写实收金额")
	}
	if err := in.validate(); err != nil {
		return err
	}
	if o.OrderStatus != "active" || o.CostUSDMinor != 0 || o.PaymentStatus == "refunded" || o.FulfillmentStatus == "cancelled" {
		return errors.New("订单已记账或结束")
	}
	if in.ReceivedAmount != "" {
		if o.PaymentStatus != "unpaid" || o.ReceivedAmountMinor != 0 {
			return errors.New("订单已有收款记录，不能覆盖")
		}
		if err := applyReceipt(r, tx, o, in.ReceivedCurrency, in.ReceivedAmount, in.CollectionRateID); err != nil {
			return err
		}
	}
	// 与流水使用同一事务时间；旧订单补录也从实际扣款日开始。
	var postedAt time.Time
	var previous *time.Time
	if err := tx.QueryRowContext(r.Context(), `SELECT renewal_date,NOW() FROM chatgpt_accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, o.AccountID).Scan(&previous, &postedAt); err != nil {
		return err
	}
	start, end := chargedOrderPeriod(postedAt, o.Package.Months)
	if _, err := tx.ExecContext(r.Context(), `SELECT pg_advisory_xact_lock(hashtext('recharge-cycle'),hashtext($1))`, strconv.FormatInt(o.UserID, 10)+":"+o.AccountEmail); err != nil {
		return err
	}
	var overlaps bool
	// 保留完整历史防重语义，排除本单，防止补录改期覆盖其他有效订单。
	if err := tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM recharge_orders WHERE user_id=$1 AND account_email=$2 AND id<>$3 AND order_status='active' AND payment_status<>'refunded' AND fulfillment_status<>'cancelled' AND period_start<$5::date AND period_end>$4::date)`, o.UserID, o.AccountEmail, o.ID, start, end).Scan(&overlaps); err != nil {
		return err
	}
	if overlaps {
		return errors.New("扣款日期与已有订单周期重叠")
	}
	o.PeriodStart, o.PeriodEnd = start, end
	if err := postCardEntry(r, tx, user, in.CardID, cardPosting{Kind: "subscription", Amount: -o.SaleUSDMinor, OrderID: &o.ID, AccountID: &o.AccountID, AccountEmail: o.AccountEmail, AccountLabel: o.AccountEmail, PeriodStart: o.PeriodStart, PeriodEnd: o.PeriodEnd, Currency: o.Package.Currency, OriginalAmount: o.Package.OriginalAmountMinor, Reference: strings.TrimSpace(in.Reference), Notes: evidenceSummary(in.Evidence), Key: key}, true); err != nil {
		return err
	}
	o.CardID = &in.CardID
	o.CostUSDMinor = o.SaleUSDMinor
	o.PurchaseReference = strings.TrimSpace(in.Reference)
	o.Evidence = strings.TrimSpace(in.Evidence)
	if in.OrderSource != "" {
		o.OrderSource = in.OrderSource
	}
	// 复用历史订阅字段保存开通结果，账号有效期与扣款同事务提交。
	if _, err := tx.ExecContext(r.Context(), `UPDATE chatgpt_accounts SET verified_plan=$2,verified_at=$3,subscription_ends_at=$4,renewal_date=$4,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, o.AccountID, o.Package.Plan, postedAt, end); err != nil {
		return err
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO renewal_date_audit(account_id,admin_id,previous_date,renewal_date) VALUES($1,$2,$3,$4)`, o.AccountID, user, previous, end); err != nil {
		return err
	}
	o.VerifiedAt = &postedAt
	o.FulfillmentStatus = "completed"
	o.FailureReason = ""
	return nil
}

func saveRecordedOrder(r *http.Request, tx *sql.Tx, o RechargeOrder) error {
	var rate any
	if o.ReceivedExchangeRate != nil {
		encoded, err := json.Marshal(o.ReceivedExchangeRate)
		if err != nil {
			return err
		}
		rate = string(encoded)
	}
	_, err := tx.ExecContext(r.Context(), `UPDATE recharge_orders SET payment_method=$2,payment_status=$3,card_id=$4,cost_usd_minor=$5,purchase_reference=$6,evidence=$7,fulfillment_status=$8,received_currency=$9,received_amount_minor=$10,received_usd_minor=$11,received_exchange_rate=$12,received_at=$13,period_start=$14,period_end=$15,verified_at=$16,failure_reason=$17,order_source=$18,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, o.ID, o.PaymentMethod, o.PaymentStatus, o.CardID, o.CostUSDMinor, o.PurchaseReference, o.Evidence, o.FulfillmentStatus, o.ReceivedCurrency, o.ReceivedAmountMinor, o.ReceivedUSDMinor, rate, o.ReceivedAt, o.PeriodStart, o.PeriodEnd, o.VerifiedAt, o.FailureReason, o.OrderSource)
	return err
}

// 生效日按 UTC+8 的扣款日期，到期日按套餐月数并截断月末。
func chargedOrderPeriod(postedAt time.Time, months int) (string, string) {
	local := postedAt.In(time.FixedZone("UTC+8", 8*60*60))
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	return start.Format("2006-01-02"), addMonthsClamped(start, months).Format("2006-01-02")
}
