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

	"github.com/jackc/pgx/v5/pgconn"
)

const maxCardMoneyMinor int64 = 1000000000000
const subscriptionPHPMinor int64 = 891964

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

const cardLedgerColumns = `id,kind,amount_usd_minor,balance_after_usd_minor,COALESCE(account_id,0),account_label,account_email,COALESCE(original_php_minor,0),notes,created_at`

func scanCardLedger(row interface{ Scan(...any) error }) (cardLedgerEntry, error) {
	var entry cardLedgerEntry
	err := row.Scan(&entry.ID, &entry.Kind, &entry.AmountUSDMinor, &entry.BalanceAfterUSDMinor, &entry.AccountID, &entry.AccountLabel, &entry.AccountEmail, &entry.OriginalPHPMinor, &entry.Notes, &entry.CreatedAt)
	return entry, err
}

func (s *Server) bankCardLedger(w http.ResponseWriter, r *http.Request, user, cardID int64, admin bool) {
	if r.Method == http.MethodGet {
		s.readCardLedger(w, r, user, cardID, admin)
		return
	}
	if r.Method != http.MethodPost {
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
	}
	if jsonBody(r, &in) != nil {
		reply(w, map[string]string{"error": "记账参数无效"}, 400)
		return
	}
	amount, valid := parseCardUSD(in.AmountUSD)
	in.Notes = strings.TrimSpace(in.Notes)
	if !valid || !ledgerKeyPattern.MatchString(in.RequestKey) || utf8.RuneCountInString(in.Notes) > 1000 || (in.Kind != "deposit" && in.Kind != "subscription") || (in.Kind == "subscription" && in.AccountID < 1) || (in.Kind == "deposit" && in.AccountID != 0) {
		reply(w, map[string]string{"error": "请输入大于 0、最多两位小数的 USD 金额；开通扣款必须选择账号"}, 400)
		return
	}
	if in.Kind == "subscription" {
		amount = -amount
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		cardError(w, err)
		return
	}
	defer tx.Rollback()
	var balance, owner int64
	// 同一卡的所有收支串行记账，余额与流水在同一事务提交。
	err = tx.QueryRowContext(r.Context(), `SELECT balance_usd_minor,user_id FROM bank_cards WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR $3) FOR UPDATE`, cardID, user, admin).Scan(&balance, &owner)
	if err != nil {
		cardError(w, err)
		return
	}
	// 幂等与开通判重必须涵盖历史记录，软删除不能成为重复入账的途径。
	existing, err := scanCardLedger(tx.QueryRowContext(r.Context(), `SELECT `+cardLedgerColumns+` FROM bank_card_ledger WHERE card_id=$1 AND request_key=$2`, cardID, in.RequestKey))
	if err == nil {
		kind := existing.Kind
		if kind == "opening" {
			kind = "deposit"
		}
		if kind != in.Kind || existing.AmountUSDMinor != amount || existing.AccountID != in.AccountID || existing.Notes != in.Notes {
			reply(w, map[string]string{"error": "此请求已记账，请刷新对账单后重新操作"}, 409)
			return
		}
		reply(w, map[string]any{"entry": existing, "replayed": true}, 200)
		return
	}
	if !errors.Is(err, sql.ErrNoRows) {
		cardError(w, err)
		return
	}
	var accountID, php any
	var label, email string
	if in.Kind == "subscription" {
		// 普通用户仅可关联本人账号；管理员可跨所属用户核对业务付款。
		err = tx.QueryRowContext(r.Context(), `SELECT label,email FROM chatgpt_accounts WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR $3) FOR SHARE`, in.AccountID, owner, admin).Scan(&label, &email)
		if errors.Is(err, sql.ErrNoRows) {
			reply(w, map[string]string{"error": "账号不存在或无权为此账号记账"}, 404)
			return
		}
		if err != nil {
			cardError(w, err)
			return
		}
		var paid bool
		if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM bank_card_ledger WHERE kind='subscription' AND account_id=$1)`, in.AccountID).Scan(&paid); err != nil {
			cardError(w, err)
			return
		}
		if paid {
			reply(w, map[string]string{"error": "该账号已记录开通扣款，请勿重复记账"}, 409)
			return
		}
		accountID, php = in.AccountID, subscriptionPHPMinor
	} else {
		var hasEntries bool
		if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM bank_card_ledger WHERE card_id=$1)`, cardID).Scan(&hasEntries); err != nil {
			cardError(w, err)
			return
		}
		if !hasEntries {
			in.Kind = "opening"
		}
	}
	next := balance + amount
	if next < 0 {
		reply(w, map[string]string{"error": "卡内记账余额不足，请核对或先记录存入款项"}, 409)
		return
	}
	if next > maxCardMoneyMinor {
		reply(w, map[string]string{"error": "余额超出允许范围"}, 400)
		return
	}
	entry, err := scanCardLedger(tx.QueryRowContext(r.Context(), `INSERT INTO bank_card_ledger(card_id,actor_id,request_key,kind,amount_usd_minor,balance_after_usd_minor,account_id,account_label,account_email,original_php_minor,notes) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+cardLedgerColumns, cardID, user, in.RequestKey, in.Kind, amount, next, accountID, label, email, php, in.Notes))
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			reply(w, map[string]string{"error": "该账号或请求已记账，请刷新对账单，勿重复扣款"}, 409)
			return
		}
		cardError(w, err)
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE bank_cards SET balance_usd_minor=$2,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, cardID, next); err != nil {
		cardError(w, err)
		return
	}
	if err = tx.Commit(); err != nil {
		cardError(w, err)
		return
	}
	reply(w, map[string]any{"entry": entry, "replayed": false}, 201)
}

func (s *Server) readCardLedger(w http.ResponseWriter, r *http.Request, user, cardID int64, admin bool) {
	page := 1
	var err error
	if raw := r.URL.Query().Get("page"); raw != "" {
		page, err = strconv.Atoi(raw)
	}
	if err != nil || page < 1 || page > 100000 {
		reply(w, map[string]string{"error": "页码无效"}, 400)
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
	rows, err := tx.QueryContext(r.Context(), `SELECT `+cardLedgerColumns+` FROM bank_card_ledger WHERE card_id=$1 ORDER BY id DESC LIMIT 20 OFFSET $2`, cardID, (page-1)*20)
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
	reply(w, map[string]any{"entries": entries, "total": total, "page": page, "page_size": 20, "balance_usd_minor": balance, "deposited_usd_minor": deposit, "spent_usd_minor": spent}, 200)
}
