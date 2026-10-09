package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type cardRenewalSummary struct {
	Month        string `json:"month"`
	BoundCount   int64  `json:"bound_count"`
	RenewalCount int64  `json:"renewal_count"`
	Required     int64  `json:"required_usd_minor"`
	UnknownCount int64  `json:"unknown_count"`
	Difference   *int64 `json:"difference_usd_minor"`
}

// 只读汇总独立于银行卡编辑模型，不接受客户端传入预算。
type bankCardWithRenewal struct {
	BankCard
	Renewal *cardRenewalSummary `json:"renewal,omitempty"`
}

// 统一使用当前套餐与持久化汇率估算下次续费，不回写历史订单金额。
func renewalPackages(ctx context.Context, tx *sql.Tx, now time.Time) (map[int64]RechargePackage, error) {
	rate, err := latestExchangeRate(ctx, tx)
	if err != nil {
		return nil, err
	}
	raw, err := jsonRows(ctx, tx, `SELECT to_jsonb(p) FROM recharge_packages p WHERE p.deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	packages := make(map[int64]RechargePackage, len(raw))
	for _, value := range raw {
		var pkg RechargePackage
		if err := json.Unmarshal(value, &pkg); err != nil {
			return nil, err
		}
		pricePackage(&pkg, rate, now)
		pkg.PriceReady = pkg.Enabled && pkg.PriceReady && pkg.SaleUSDMinor > 0
		packages[pkg.ID] = pkg
	}
	return packages, nil
}

func cardRenewalMonth(now time.Time) (string, string) {
	local := now.In(exchangeTimezone)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, exchangeTimezone)
	return start.Format("2006-01-02"), start.AddDate(0, 1, 0).Format("2006-01-02")
}

const boundCardAccounts = ` FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id AND u.deleted_at IS NULL WHERE a.deleted_at IS NULL AND a.payment_card_id=ANY($1::bigint[])`

// 按当前页卡片批量汇总，绑定数量包含关闭续订及未设置日期的账号。
func fillCardRenewals(ctx context.Context, tx *sql.Tx, cards []bankCardWithRenewal, packages map[int64]RechargePackage, now time.Time) error {
	start, end := cardRenewalMonth(now)
	ids := []int64{}
	indexes := map[int64]int{}
	for i := range cards {
		if cards[i].DeletedAt != nil {
			continue
		}
		ids = append(ids, cards[i].ID)
		indexes[cards[i].ID] = i
		cards[i].Renewal = &cardRenewalSummary{Month: start[:7]}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT a.payment_card_id,COALESCE(a.subscription_package_id,0),count(*),count(*) FILTER (WHERE a.renewal_enabled AND a.renewal_date >= $2::date AND a.renewal_date < $3::date)`+boundCardAccounts+` GROUP BY a.payment_card_id,a.subscription_package_id`, ids, start, end)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cardID, packageID, count, due int64
		if err := rows.Scan(&cardID, &packageID, &count, &due); err != nil {
			return err
		}
		summary := cards[indexes[cardID]].Renewal
		summary.BoundCount += count
		summary.RenewalCount += due
		pkg := packages[packageID]
		if !pkg.PriceReady {
			summary.UnknownCount += due
			continue
		}
		const maxSafeMinor int64 = 1<<53 - 1
		if due > (maxSafeMinor-summary.Required)/pkg.SaleUSDMinor {
			return fmt.Errorf("本月续费合计金额超出范围")
		}
		summary.Required += due * pkg.SaleUSDMinor
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range cards {
		card := &cards[i]
		if card.Renewal != nil && card.Renewal.UnknownCount == 0 {
			difference := card.BalanceUSDMinor - card.Reserved - card.Renewal.Required
			const maxSafeMinor int64 = 1<<53 - 1
			if difference < -maxSafeMinor || difference > maxSafeMinor {
				return fmt.Errorf("本月续费余额差超出范围")
			}
			card.Renewal.Difference = &difference
		}
	}
	return nil
}

type cardBoundAccount struct {
	ID             int64   `json:"id"`
	Label          string  `json:"label"`
	Email          string  `json:"email"`
	PackageID      int64   `json:"subscription_package_id"`
	Plan           string  `json:"subscription_plan"`
	PackageName    string  `json:"package_name"`
	RenewalDate    *string `json:"renewal_date"`
	RenewalEnabled bool    `json:"renewal_enabled"`
	RenewalAmount  *int64  `json:"renewal_usd_minor"`
	InMonth        bool    `json:"in_month"`
}

func (s *Server) bankCardAccounts(w http.ResponseWriter, r *http.Request, user, id int64, admin bool) {
	if !admin {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	page, size, valid := pageParameters(r)
	if !valid {
		reply(w, map[string]string{"error": "页码无效"}, 400)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		cardError(w, err)
		return
	}
	defer tx.Rollback()
	card, err := scanBankCard(tx.QueryRowContext(r.Context(), `SELECT `+bankCardColumns+` FROM bank_cards WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR $3)`, id, user, admin))
	if err != nil {
		cardError(w, err)
		return
	}
	now := time.Now()
	packages, err := renewalPackages(r.Context(), tx, now)
	cards := []bankCardWithRenewal{{BankCard: card}}
	if err == nil {
		err = fillCardRenewals(r.Context(), tx, cards, packages, now)
	}
	if err != nil {
		cardError(w, err)
		return
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT a.id,a.label,a.email,COALESCE(a.subscription_package_id,0),a.renewal_date::text,a.renewal_enabled`+boundCardAccounts+` ORDER BY a.renewal_date ASC NULLS LAST,a.id DESC LIMIT $2 OFFSET $3`, []int64{id}, size, (page-1)*size)
	if err != nil {
		cardError(w, err)
		return
	}
	defer rows.Close()
	accounts := []cardBoundAccount{}
	start, end := cardRenewalMonth(now)
	for rows.Next() {
		var account cardBoundAccount
		if err := rows.Scan(&account.ID, &account.Label, &account.Email, &account.PackageID, &account.RenewalDate, &account.RenewalEnabled); err != nil {
			cardError(w, err)
			return
		}
		pkg := packages[account.PackageID]
		account.Plan, account.PackageName = pkg.Plan, pkg.Name
		if pkg.PriceReady {
			amount := pkg.SaleUSDMinor
			account.RenewalAmount = &amount
		}
		account.InMonth = account.RenewalEnabled && account.RenewalDate != nil && *account.RenewalDate >= start && *account.RenewalDate < end
		accounts = append(accounts, account)
	}
	err = rows.Err()
	rows.Close()
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		cardError(w, err)
		return
	}
	reply(w, map[string]any{"accounts": accounts, "total": cards[0].Renewal.BoundCount, "page": page, "page_size": size, "renewal": cards[0].Renewal, "available_usd_minor": card.BalanceUSDMinor - card.Reserved}, 200)
}
