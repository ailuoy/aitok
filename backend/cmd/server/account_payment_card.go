package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

// 只展示可用卡的摘要；完整卡号及安全码继续走已有的受审计敏感读取入口。
const usablePaymentCard = `c.deleted_at IS NULL AND c.status='active' AND (c.exp_year,c.exp_month)>=(EXTRACT(YEAR FROM NOW() AT TIME ZONE 'UTC')::int,EXTRACT(MONTH FROM NOW() AT TIME ZONE 'UTC')::int)`
const accountPaymentCardJSON = `'payment_card_id',c.id,'payment_card_label',COALESCE(c.label,''),'payment_card_last4',COALESCE(c.last4,''),'payment_card_available',COALESCE(` + usablePaymentCard + `,false)`

func (s *Server) accountPaymentCards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	cards, err := jsonRows(r.Context(), s.db, `SELECT jsonb_build_object('id',c.id,'label',c.label,'last4',c.last4,'brand',c.brand) FROM bank_cards c WHERE `+usablePaymentCard+` ORDER BY lower(c.label),c.id`)
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"cards": cards}, 200)
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
