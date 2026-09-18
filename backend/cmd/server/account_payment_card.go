package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// 只展示可用卡的摘要；完整卡号及安全码继续走已有的受审计敏感读取入口。
const usablePaymentCard = `c.deleted_at IS NULL AND c.status='active' AND (c.exp_year,c.exp_month)>=(EXTRACT(YEAR FROM NOW() AT TIME ZONE 'UTC')::int,EXTRACT(MONTH FROM NOW() AT TIME ZONE 'UTC')::int)`
const accountPaymentCardJSON = `'payment_card_id',c.id,'payment_card_label',COALESCE(c.label,''),'payment_card_last4',COALESCE(c.last4,''),'payment_card_available',COALESCE(` + usablePaymentCard + `,false)`

type accountFundingCard struct {
	ID            int64  `json:"id"`
	Label         string `json:"label"`
	Last4         string `json:"last4"`
	Brand         string `json:"brand"`
	Balance       int64  `json:"balance_usd_minor"`
	Reserved      int64  `json:"reserved_usd_minor"`
	Required      int64  `json:"required_usd_minor"`
	RenewalCount  int64  `json:"renewal_count"`
	UnknownCount  int64  `json:"unknown_count"`
	FundingStatus string `json:"funding_status"`
}

func (s *Server) accountPaymentCards(w http.ResponseWriter, r *http.Request, user int64) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	filter, args, err := accountFilter(r, user, true)
	if err != nil {
		reply(w, map[string]string{"error": err.Error()}, 400)
		return
	}
	cards, err := s.accountCardFunding(r.Context(), filter, args)
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"cards": cards}, 200)
}

func (s *Server) accountCardFunding(ctx context.Context, filter string, args []any) ([]accountFundingCard, error) {
	// 余额、账号绑定、套餐及汇率来自同一快照，仅预估，不产生任何扣款。
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rawCards, err := jsonRows(ctx, tx, `SELECT jsonb_build_object('id',c.id,'label',c.label,'last4',c.last4,'brand',c.brand,'balance_usd_minor',c.balance_usd_minor,'reserved_usd_minor',c.reserved_usd_minor) FROM bank_cards c WHERE `+usablePaymentCard+` ORDER BY lower(c.label),c.id`)
	if err != nil {
		return nil, err
	}
	cards := make([]accountFundingCard, 0, len(rawCards))
	cardIndexes := make(map[int64]int, len(rawCards))
	for _, raw := range rawCards {
		var card accountFundingCard
		if err := json.Unmarshal(raw, &card); err != nil {
			return nil, err
		}
		cardIndexes[card.ID] = len(cards)
		cards = append(cards, card)
	}
	rate, err := latestExchangeRate(ctx, tx)
	if err != nil {
		return nil, err
	}
	rawPackages, err := jsonRows(ctx, tx, `SELECT to_jsonb(p) FROM recharge_packages p WHERE p.deleted_at IS NULL AND p.enabled`)
	if err != nil {
		return nil, err
	}
	prices := make(map[int64]int64, len(rawPackages))
	now := time.Now()
	for _, raw := range rawPackages {
		var pkg RechargePackage
		if err := json.Unmarshal(raw, &pkg); err != nil {
			return nil, err
		}
		pricePackage(&pkg, rate, now)
		if pkg.PriceReady && pkg.SaleUSDMinor > 0 {
			prices[pkg.ID] = pkg.SaleUSDMinor
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.id,COALESCE(a.subscription_package_id,0),count(*)`+filter+` AND `+usablePaymentCard+` AND a.renewal_enabled AND a.renewal_date IS NOT NULL GROUP BY c.id,a.subscription_package_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cardID, packageID, count int64
		if err := rows.Scan(&cardID, &packageID, &count); err != nil {
			return nil, err
		}
		card := &cards[cardIndexes[cardID]]
		card.RenewalCount += count
		price, ready := prices[packageID]
		if !ready {
			card.UnknownCount += count
			continue
		}
		// 保证累计金额既不溢出，也能被前端安全表示为整数美分。
		const maxSafeMinor int64 = 1<<53 - 1
		if count > (maxSafeMinor-card.Required)/price {
			return nil, fmt.Errorf("续费合计金额超出范围")
		}
		card.Required += count * price
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range cards {
		card := &cards[i]
		switch {
		case card.Required > card.Balance-card.Reserved:
			card.FundingStatus = "insufficient"
		case card.UnknownCount > 0:
			card.FundingStatus = "unknown"
		default:
			card.FundingStatus = "sufficient"
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return cards, nil
}

func (s *Server) setAccountPaymentCard(w http.ResponseWriter, r *http.Request, user, id int64) {
	if r.Method != http.MethodPatch {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var input struct {
		CardID json.RawMessage `json:"payment_card_id"`
	}
	var cardID *int64
	if jsonBody(r, &input) != nil || len(input.CardID) == 0 || json.Unmarshal(input.CardID, &cardID) != nil || (cardID != nil && *cardID < 1) {
		reply(w, map[string]string{"error": "请选择有效付款卡，解绑请传 null"}, 400)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	// 与删卡采用相同的卡片→账号锁顺序，防止并发绑定已删除或已冻结的卡。
	if cardID != nil {
		var locked int64
		err = tx.QueryRowContext(r.Context(), `SELECT c.id FROM bank_cards c WHERE c.id=$1 AND `+usablePaymentCard+` FOR SHARE`, *cardID).Scan(&locked)
		if err == sql.ErrNoRows {
			reply(w, map[string]string{"error": "付款卡已删除、冻结或过期，请重新选择"}, 409)
			return
		}
		if err != nil {
			operationError(w, err)
			return
		}
	}
	var previous *int64
	err = tx.QueryRowContext(r.Context(), `SELECT a.payment_card_id FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id AND u.deleted_at IS NULL WHERE a.id=$1 AND a.deleted_at IS NULL FOR UPDATE OF a`, id).Scan(&previous)
	if err != nil {
		operationError(w, err)
		return
	}
	changed := (previous == nil) != (cardID == nil) || (previous != nil && cardID != nil && *previous != *cardID)
	if changed {
		_, err = tx.ExecContext(r.Context(), `UPDATE chatgpt_accounts SET payment_card_id=$2,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, id, cardID)
		if err == nil {
			err = recordEvent(r.Context(), tx, user, id, "account", "payment_card", eventKey(), map[string]any{"payment_card_id": previous}, map[string]any{"payment_card_id": cardID})
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"payment_card_id": cardID}, 200)
}
