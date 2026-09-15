package main

import (
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const maxCardMoneyMinor int64 = 1000000000000

var cardMoneyPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,10})(\.[0-9]{1,2})?$`)
var ledgerKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,80}$`)

// 金额以十进制字符串输入，避免浮点数换算造成美分误差。
func parseCardUSD(value string) (int64, bool) {
	if !cardMoneyPattern.MatchString(value) {
		return 0, false
	}
	parts := strings.SplitN(value, ".", 2)
	whole, _ := strconv.ParseInt(parts[0], 10, 64)
	cents := int64(0)
	if len(parts) == 2 {
		cents, _ = strconv.ParseInt(parts[1]+strings.Repeat("0", 2-len(parts[1])), 10, 64)
	}
	amount := whole*100 + cents
	return amount, amount > 0 && amount <= maxCardMoneyMinor
}

type cardLedgerEntry struct {
	ActorID              int64     `json:"actor_id"`
	ExternalReference    string    `json:"external_reference"`
	Currency             string    `json:"currency"`
	OriginalAmountMinor  int64     `json:"original_amount_minor"`
	PeriodStart          string    `json:"period_start"`
	PeriodEnd            string    `json:"period_end"`
	ID                   int64     `json:"id"`
	Kind                 string    `json:"kind"`
	AmountUSDMinor       int64     `json:"amount_usd_minor"`
	BalanceAfterUSDMinor int64     `json:"balance_after_usd_minor"`
	AccountID            int64     `json:"account_id"`
	AccountLabel         string    `json:"account_label"`
	AccountEmail         string    `json:"account_email"`
	OriginalPHPMinor     int64     `json:"original_php_minor"`
	Notes                string    `json:"notes"`
	CreatedAt            time.Time `json:"created_at"`
}

const cardLedgerColumns = `id,kind,amount_usd_minor,balance_after_usd_minor,COALESCE(account_id,0),account_label,account_email,COALESCE(original_php_minor,0),notes,created_at,actor_id,external_reference,currency,COALESCE(original_amount_minor,0),COALESCE(period_start::text,''),COALESCE(period_end::text,'')`

func scanCardLedger(row interface{ Scan(...any) error }) (cardLedgerEntry, error) {
	var entry cardLedgerEntry
	err := row.Scan(&entry.ID, &entry.Kind, &entry.AmountUSDMinor, &entry.BalanceAfterUSDMinor, &entry.AccountID, &entry.AccountLabel, &entry.AccountEmail, &entry.OriginalPHPMinor, &entry.Notes, &entry.CreatedAt, &entry.ActorID, &entry.ExternalReference, &entry.Currency, &entry.OriginalAmountMinor, &entry.PeriodStart, &entry.PeriodEnd)
	return entry, err
}

func (s *Server) bankCardLedger(w http.ResponseWriter, r *http.Request, user, cardID int64, admin bool) {
	if r.Method == "GET" {
		s.readCardLedger(w, r, user, cardID, admin)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	var in struct {
		Kind       string `json:"kind"`
		AmountUSD  string `json:"amount_usd"`
		AccountID  int64  `json:"account_id"`
		Notes      string `json:"notes"`
		RequestKey string `json:"request_key"`
		Reference  string `json:"reference"`
		Currency   string `json:"currency"`
		Original   int64  `json:"original_amount_minor"`
		Start      string `json:"period_start"`
		End        string `json:"period_end"`
	}
	if jsonBody(r, &in) != nil {
		w.WriteHeader(400)
		return
	}
	amount, valid := parseCardUSD(in.AmountUSD)
	if !valid || !ledgerKeyPattern.MatchString(in.RequestKey) || utf8.RuneCountInString(in.Notes) > 1000 || (in.Kind != "deposit" && in.Kind != "subscription") || (in.Kind == "deposit" && in.AccountID != 0) {
		reply(w, map[string]string{"error": "记账参数无效"}, 400)
		return
	}
	if in.Kind == "subscription" {
		if _, _, err := parsePeriod(in.Start, in.End); err != nil || in.AccountID < 1 || in.Notes == "" || in.Reference == "" || in.Original <= 0 || in.Original > maxCardMoneyMinor || len(in.Currency) != 3 {
			reply(w, map[string]string{"error": "补录开通扣款需填写账号、周期、原币种金额、交易号和核对依据；新业务请使用充值订单"}, 400)
			return
		}
		amount = -amount
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		cardError(w, err)
		return
	}
	defer tx.Rollback()
	var owner int64
	err = tx.QueryRowContext(r.Context(), `SELECT user_id FROM bank_cards WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR $3) FOR UPDATE`, cardID, user, admin).Scan(&owner)
	if err != nil {
		cardError(w, err)
		return
	}
	existing, err := scanCardLedger(tx.QueryRowContext(r.Context(), `SELECT `+cardLedgerColumns+` FROM bank_card_ledger WHERE card_id=$1 AND request_key=$2`, cardID, in.RequestKey))
	if err == nil {
		kind := existing.Kind
		if kind == "opening" {
			kind = "deposit"
		}
		if kind != in.Kind || existing.AmountUSDMinor != amount || existing.AccountID != in.AccountID || existing.Notes != in.Notes || existing.ExternalReference != in.Reference || existing.PeriodStart != in.Start || existing.PeriodEnd != in.End || (in.Kind == "subscription" && (existing.Currency != in.Currency || existing.OriginalAmountMinor != in.Original)) {
			reply(w, map[string]string{"error": "同一请求的记账内容不能改变"}, 409)
			return
		}
		reply(w, map[string]any{"entry": existing, "replayed": true}, 200)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		cardError(w, err)
		return
	}
	p := cardPosting{Kind: in.Kind, Amount: amount, Notes: in.Notes, Reference: in.Reference, Currency: in.Currency, OriginalAmount: in.Original, PeriodStart: in.Start, PeriodEnd: in.End, Key: in.RequestKey}
	if in.Kind == "subscription" {
		p.AccountID = &in.AccountID
		err = tx.QueryRowContext(r.Context(), `SELECT label,lower(trim(email)) FROM chatgpt_accounts WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR $3) FOR SHARE`, in.AccountID, owner, admin).Scan(&p.AccountLabel, &p.AccountEmail)
		if err != nil {
			cardError(w, err)
			return
		}
	} else {
		var exists bool
		if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM bank_card_ledger WHERE card_id=$1)`, cardID).Scan(&exists); err != nil {
			cardError(w, err)
			return
		}
		if !exists {
			p.Kind = "opening"
		}
	}
	if err = postCardEntry(r, tx, user, cardID, p, admin); err != nil {
		operationError(w, err)
		return
	}
	entry, err := scanCardLedger(tx.QueryRowContext(r.Context(), `SELECT `+cardLedgerColumns+` FROM bank_card_ledger WHERE card_id=$1 AND request_key=$2`, cardID, in.RequestKey))
	if err == nil {
		err = recordEvent(r.Context(), tx, user, cardID, "card", p.Kind, in.RequestKey, map[string]any{}, entry)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"entry": entry, "replayed": false}, 201)
}

func (s *Server) readCardLedger(w http.ResponseWriter, r *http.Request, user, cardID int64, admin bool) {
	page, size, valid := pageParameters(r)
	if !valid {
		reply(w, map[string]string{"error": "分页参数无效"}, 400)
		return
	}

	// 同一快照读取余额、总数和流水，避免并发记账使对账单不一致。
	tx, err := s.db.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		cardError(w, err)
		return
	}
	defer tx.Rollback()
	var balance, total, deposit, spent int64
	// 此接口为只读历史对账：允许读取已软删除卡片及其完整流水，写入入口仍只接受活跃卡片。
	if err = tx.QueryRowContext(r.Context(), `SELECT balance_usd_minor FROM bank_cards WHERE id=$1 AND (user_id=$2 OR $3)`, cardID, user, admin).Scan(&balance); err != nil {
		cardError(w, err)
		return
	}
	if err = tx.QueryRowContext(r.Context(), `SELECT count(*),COALESCE(sum(amount_usd_minor) FILTER(WHERE amount_usd_minor>0),0),COALESCE(-sum(amount_usd_minor) FILTER(WHERE amount_usd_minor<0),0) FROM bank_card_ledger WHERE card_id=$1`, cardID).Scan(&total, &deposit, &spent); err != nil {
		cardError(w, err)
		return
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT `+cardLedgerColumns+` FROM bank_card_ledger WHERE card_id=$1 ORDER BY id DESC LIMIT $3 OFFSET $2`, cardID, (page-1)*size, size)
	if err != nil {
		cardError(w, err)
		return
	}
	defer rows.Close()
	entries := []cardLedgerEntry{}
	for rows.Next() {
		entry, err := scanCardLedger(rows)
		if err != nil {
			cardError(w, err)
			return
		}
		entries = append(entries, entry)
	}
	if err = rows.Err(); err != nil {
		cardError(w, err)
		return
	}
	rows.Close()
	if err = tx.Commit(); err != nil {
		cardError(w, err)
		return
	}
	reply(w, map[string]any{"entries": entries, "total": total, "page": page, "page_size": size, "balance_usd_minor": balance, "deposited_usd_minor": deposit, "spent_usd_minor": spent}, 200)
}
