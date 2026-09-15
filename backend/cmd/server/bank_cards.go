package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

type BankCard struct {
	ID              int64  `json:"id"`
	Label           string `json:"label"`
	Platform        string `json:"platform"`
	Notes           string `json:"notes"`
	Cardholder      string `json:"cardholder"`
	Number          string `json:"number,omitempty"`
	Last4           string `json:"last4"`
	Brand           string `json:"brand"`
	ExpMonth        int    `json:"exp_month"`
	ExpYear         int    `json:"exp_year"`
	BalanceUSDMinor int64  `json:"balance_usd_minor"`
}

func (c *BankCard) normalize() bool {
	c.Label = strings.Join(strings.Fields(c.Label), " ")
	c.Platform = strings.Join(strings.Fields(c.Platform), " ")
	c.Notes = strings.TrimSpace(c.Notes)
	if utf8.RuneCountInString(c.Platform) > 80 || utf8.RuneCountInString(c.Notes) > 1000 {
		return false
	}
	c.Cardholder = strings.Join(strings.Fields(c.Cardholder), " ")
	c.Number = strings.NewReplacer(" ", "", "-", "").Replace(c.Number)
	if c.Label == "" || utf8.RuneCountInString(c.Label) > 80 || c.Cardholder == "" || utf8.RuneCountInString(c.Cardholder) > 120 || len(c.Number) < 13 || len(c.Number) > 19 {
		return false
	}
	sum := 0
	double := false
	for i := len(c.Number) - 1; i >= 0; i-- {
		if c.Number[i] < '0' || c.Number[i] > '9' {
			return false
		}
		n := int(c.Number[i] - '0')
		if double {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		double = !double
	}
	now := time.Now().UTC()
	if sum == 0 || sum%10 != 0 || c.ExpMonth < 1 || c.ExpMonth > 12 || c.ExpYear < now.Year() || c.ExpYear > now.Year()+25 || (c.ExpYear == now.Year() && c.ExpMonth < int(now.Month())) {
		return false
	}
	c.Last4 = c.Number[len(c.Number)-4:]
	c.Brand = "银行卡"
	switch {
	case strings.HasPrefix(c.Number, "4"):
		c.Brand = "Visa"
	case strings.HasPrefix(c.Number, "34") || strings.HasPrefix(c.Number, "37"):
		c.Brand = "Amex"
	case strings.HasPrefix(c.Number, "5") || strings.HasPrefix(c.Number, "2"):
		c.Brand = "Mastercard"
	case strings.HasPrefix(c.Number, "62"):
		c.Brand = "UnionPay"
	}
	return true
}

const bankCardColumns = `id,label,cardholder,last4,brand,exp_month,exp_year,platform,notes,balance_usd_minor`

func scanBankCard(row interface{ Scan(...any) error }) (BankCard, error) {
	var c BankCard
	err := row.Scan(&c.ID, &c.Label, &c.Cardholder, &c.Last4, &c.Brand, &c.ExpMonth, &c.ExpYear, &c.Platform, &c.Notes, &c.BalanceUSDMinor)
	return c, err
}
func cardError(w http.ResponseWriter, err error) {
	var pg *pgconn.PgError
	if errors.Is(err, sql.ErrNoRows) {
		reply(w, map[string]string{"error": "银行卡不存在"}, 404)
		return
	}
	if errors.As(err, &pg) && pg.Code == "23505" {
		reply(w, map[string]string{"error": "这张银行卡已添加"}, 409)
		return
	}
	if errors.As(err, &pg) && pg.Code == "23503" {
		reply(w, map[string]string{"error": "此卡已有对账记录，不能删除，请保留用于核对历史余额"}, 409)
		return
	}
	reply(w, map[string]string{"error": "银行卡操作失败，请检查服务配置或稍后重试"}, 500)
}

func (s *Server) readCard(r *http.Request, user, id int64, admin bool) (BankCard, error) {
	var c BankCard
	var encrypted string
	err := s.db.QueryRowContext(r.Context(), `SELECT `+bankCardColumns+`,number_ciphertext FROM bank_cards WHERE deleted_at IS NULL AND (user_id=$1 OR $3) AND id=$2`, user, id, admin).Scan(&c.ID, &c.Label, &c.Cardholder, &c.Last4, &c.Brand, &c.ExpMonth, &c.ExpYear, &c.Platform, &c.Notes, &c.BalanceUSDMinor, &encrypted)
	if err == nil {
		c.Number, err = decryptSession(encrypted)
	}
	return c, err
}

func (s *Server) bankCards(w http.ResponseWriter, r *http.Request) {
	user, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return
	}
	admin, err := s.isAdmin(r.Context(), user)
	if err != nil {
		reply(w, map[string]string{"error": "用户不存在"}, 401)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasSuffix(r.URL.Path, "/ledger") {
		id, parseErr := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/bank-cards/"), "/ledger"), 10, 64)
		if parseErr != nil || id < 1 {
			http.NotFound(w, r)
			return
		}
		s.bankCardLedger(w, r, user, id, admin)
		return
	}
	var id int64
	if r.URL.Path != "/api/bank-cards" {
		id, err = strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/bank-cards/"), 10, 64)
		if err != nil || id < 1 {
			http.NotFound(w, r)
			return
		}
	}
	if r.Method == "GET" && id == 0 {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		page := 1
		if value := r.URL.Query().Get("page"); value != "" {
			page, err = strconv.Atoi(value)
		}
		if err != nil || page < 1 || page > 100000 || utf8.RuneCountInString(query) > 200 {
			reply(w, map[string]string{"error": "搜索或页码无效"}, 400)
			return
		}
		const filter = ` WHERE deleted_at IS NULL AND (user_id=$1 OR $3) AND strpos(lower(concat_ws(' ',label,cardholder,last4,brand,platform,notes)),lower($2))>0`
		var total int
		if err = s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM bank_cards`+filter, user, query, admin).Scan(&total); err != nil {
			cardError(w, err)
			return
		}
		rows, err := s.db.QueryContext(r.Context(), `SELECT `+bankCardColumns+` FROM bank_cards`+filter+` ORDER BY id DESC LIMIT 20 OFFSET $4`, user, query, admin, (page-1)*20)
		if err != nil {
			cardError(w, err)
			return
		}
		defer rows.Close()
		cards := []BankCard{}
		for rows.Next() {
			c, err := scanBankCard(rows)
			if err != nil {
				cardError(w, err)
				return
			}
			cards = append(cards, c)
		}
		if err = rows.Err(); err != nil {
			cardError(w, err)
			return
		}
		rows.Close()
		// 平台选项来自当前用户有权管理的全部银行卡，不受列表搜索和分页影响。
		platformRows, err := s.db.QueryContext(r.Context(), `SELECT DISTINCT platform FROM bank_cards WHERE deleted_at IS NULL AND (user_id=$1 OR $2) AND platform<>'' ORDER BY platform`, user, admin)
		if err != nil {
			cardError(w, err)
			return
		}
		defer platformRows.Close()
		platforms := []string{}
		for platformRows.Next() {
			var platform string
			if err = platformRows.Scan(&platform); err != nil {
				cardError(w, err)
				return
			}
			platforms = append(platforms, platform)
		}
		if err = platformRows.Err(); err != nil {
			cardError(w, err)
			return
		}
		reply(w, map[string]any{"cards": cards, "platforms": platforms, "total": total, "page": page, "page_size": 20}, 200)
		return
	}
	if r.Method == "GET" && id > 0 {
		c, err := s.readCard(r, user, id, admin)
		if err != nil {
			cardError(w, err)
			return
		}
		reply(w, map[string]any{"card": c}, 200)
		return
	}
	if (r.Method == "POST" && id == 0) || (r.Method == "PATCH" && id > 0) {
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		var c BankCard
		if jsonBody(r, &c) != nil || !c.normalize() {
			reply(w, map[string]string{"error": "请填写名称、持卡人、有效卡号及未过期的有效期；卡平台最多 80 字，备注最多 1000 字"}, 400)
			return
		}
		encrypted, err := encryptSession(c.Number)
		if err != nil {
			reply(w, map[string]string{"error": "银行卡加密未配置"}, 503)
			return
		}
		owner := user
		tx, err := s.db.BeginTx(r.Context(), nil)
		if err != nil {
			cardError(w, err)
			return
		}
		defer tx.Rollback()
		if id > 0 {
			if err = tx.QueryRowContext(r.Context(), `SELECT user_id FROM bank_cards WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR $3) FOR UPDATE`, id, user, admin).Scan(&owner); err != nil {
				cardError(w, err)
				return
			}
		}
		mac := hmac.New(sha256.New, s.secret)
		mac.Write([]byte("bank-card:" + strconv.FormatInt(owner, 10) + ":" + c.Number))
		fingerprint := hex.EncodeToString(mac.Sum(nil))
		args := []any{c.Label, c.Cardholder, encrypted, fingerprint, c.Last4, c.Brand, c.ExpMonth, c.ExpYear, owner, c.Platform, c.Notes}
		query := `INSERT INTO bank_cards(label,cardholder,number_ciphertext,number_fingerprint,last4,brand,exp_month,exp_year,user_id,platform,notes) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING ` + bankCardColumns
		status := 201
		if id > 0 {
			query = `UPDATE bank_cards SET label=$1,cardholder=$2,number_ciphertext=$3,number_fingerprint=$4,last4=$5,brand=$6,exp_month=$7,exp_year=$8,platform=$10,notes=$11,updated_at=NOW() WHERE deleted_at IS NULL AND user_id=$9 AND id=$12 AND (number_fingerprint=$4 OR NOT EXISTS(SELECT 1 FROM bank_card_ledger WHERE card_id=$12)) RETURNING ` + bankCardColumns
			args = append(args, id)
			status = 200
		}
		saved, err := scanBankCard(tx.QueryRowContext(r.Context(), query, args...))
		if id > 0 && errors.Is(err, sql.ErrNoRows) {
			reply(w, map[string]string{"error": "银行卡已不存在，或已有对账记录不能更换卡号；新卡请单独添加"}, 409)
			return
		}
		if err != nil {
			cardError(w, err)
			return
		}
		if err = tx.Commit(); err != nil {
			cardError(w, err)
			return
		}
		reply(w, map[string]any{"card": saved}, status)
		return
	}
	if r.Method == "DELETE" && id > 0 {
		result, err := s.db.ExecContext(r.Context(), `UPDATE bank_cards SET deleted_at=NOW() WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR $3)`, id, user, admin)
		if err != nil {
			cardError(w, err)
			return
		}
		n, err := result.RowsAffected()
		if err != nil {
			cardError(w, err)
			return
		}
		if n == 0 {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(204)
		return
	}
	w.WriteHeader(405)
}
