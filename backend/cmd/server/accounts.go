package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Account struct {
	BillingAddress       json.RawMessage `json:"billing_address"`
	BillingAddressID     *int64          `json:"billing_address_id"`
	BillingAddressLabel  string          `json:"billing_address_label"`
	ID                   int64           `json:"id"`
	UserID               int64           `json:"user_id"`
	Label                string          `json:"label"`
	Notes                string          `json:"notes"`
	Email                string          `json:"email"`
	OwnerEmail           string          `json:"owner_email"`
	CreatedAt            time.Time       `json:"created_at"`
	RenewalDate          *string         `json:"renewal_date"`
	HasSession           bool            `json:"has_session"`
	GroupID              *int64          `json:"group_id"`
	VerifiedPlan         string          `json:"verified_plan"`
	VerifiedAt           *time.Time      `json:"verified_at"`
	SubscriptionEndsAt   *string         `json:"subscription_ends_at"`
	RenewalEnabled       bool            `json:"renewal_enabled"`
	PaymentCardID        *int64          `json:"payment_card_id"`
	PaymentCardLabel     string          `json:"payment_card_label"`
	PaymentCardLast4     string          `json:"payment_card_last4"`
	PaymentCardAvailable bool            `json:"payment_card_available"`
	LastLoginAt          *time.Time      `json:"last_login_at"`
}

func (s *Server) listAccounts(ctx context.Context, id int64, admin bool) ([]Account, error) {
	// 增量迁移执行前仍可读取原账号列表；备注保存要求已完成 030 迁移。
	rows, err := s.db.QueryContext(ctx, `SELECT a.id,a.user_id,a.label,COALESCE(to_jsonb(a)->>'notes',''),a.email,u.email,a.created_at,a.renewal_date::text,COALESCE(a.session_ciphertext,'')<>'',a.group_id,a.last_login_at,a.verified_plan,a.verified_at,a.subscription_ends_at::text,a.renewal_enabled,c.id,COALESCE(c.label,''),COALESCE(c.last4,''),COALESCE(`+usablePaymentCard+`,false),a.billing_address_id,COALESCE(concat_ws(', ',NULLIF(b.address_line1,''),NULLIF(b.city,''),NULLIF(b.state,''),NULLIF(b.postal_code,'')),''),`+accountBillingAddressValue+` FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id AND u.deleted_at IS NULL LEFT JOIN bank_cards c ON c.id=a.payment_card_id AND c.deleted_at IS NULL LEFT JOIN addresses b ON b.id=a.billing_address_id AND b.deleted_at IS NULL WHERE a.deleted_at IS NULL AND (a.user_id=$1 OR $2) ORDER BY a.id DESC`, id, admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Account{}
	for rows.Next() {
		var a Account
		var billingAddress []byte
		if err := rows.Scan(&a.ID, &a.UserID, &a.Label, &a.Notes, &a.Email, &a.OwnerEmail, &a.CreatedAt, &a.RenewalDate, &a.HasSession, &a.GroupID, &a.LastLoginAt, &a.VerifiedPlan, &a.VerifiedAt, &a.SubscriptionEndsAt, &a.RenewalEnabled, &a.PaymentCardID, &a.PaymentCardLabel, &a.PaymentCardLast4, &a.PaymentCardAvailable, &a.BillingAddressID, &a.BillingAddressLabel, &billingAddress); err != nil {
			return nil, err
		}
		a.BillingAddress = billingAddress
		if a.OwnerEmail == adminIdentity {
			a.OwnerEmail = s.admin.Username
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func encryptSession(raw string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(os.Getenv("SESSION_ENCRYPTION_KEY"))
	if err != nil || len(key) != 32 {
		return "", errors.New("未配置 SESSION_ENCRYPTION_KEY")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(raw), nil)), nil
}

func (s *Server) accounts(w http.ResponseWriter, r *http.Request) {
	id, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return
	}
	if r.Method == http.MethodGet {
		admin, err := s.isAdmin(r.Context(), id)
		if err != nil {
			reply(w, map[string]string{"error": "用户不存在"}, 401)
			return
		}
		if r.URL.Query().Get("paged") == "1" {
			s.accountPage(w, r, id, admin)
			return
		}
		accounts, err := s.listAccounts(r.Context(), id, admin)
		if err != nil {
			reply(w, map[string]string{"error": "读取账号失败"}, 500)
			return
		}
		reply(w, map[string]any{"accounts": accountViews(accounts, admin)}, 200)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	var in struct {
		Label       string `json:"label"`
		Email       string `json:"email"`
		SessionJSON string `json:"session_json"`
	}
	if jsonBody(r, &in) != nil {
		reply(w, map[string]string{"error": "请求格式错误或 Session JSON 超过 256 KB"}, 400)
		return
	}
	in.Label = strings.TrimSpace(in.Label)
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	session, err := parseChatGPTSession(in.SessionJSON)
	if err != nil {
		reply(w, map[string]string{"error": err.Error()}, 400)
		return
	}
	if in.Email == "" {
		in.Email = session.Email
	}
	if session.Email != "" && session.Email != in.Email {
		reply(w, map[string]string{"error": "填写的邮箱与 Session 中的账号不一致"}, 400)
		return
	}
	if in.Label == "" {
		in.Label = session.Name
		if in.Label == "" {
			in.Label = in.Email
		}
	}
	if in.Label == "" || len(in.Label) > 120 || !validEmail(in.Email) {
		reply(w, map[string]string{"error": "无法识别账号名称或邮箱，请手动补充"}, 400)
		return
	}
	encrypted, err := encryptSession(in.SessionJSON)
	if err != nil {
		reply(w, map[string]string{"error": "账号加密未配置，请联系管理员"}, 503)
		return
	}
	a, err := s.insertAccount(r.Context(), id, in.Label, in.Email, encrypted)
	if err != nil {
		reply(w, map[string]string{"error": "该邮箱账号已存在或保存失败，请核对原账号"}, 409)
		return
	}
	reply(w, map[string]any{"account": accountView(a, s.permitted(r.Context(), id, "accounts"))}, 201)
}

func (s *Server) accountAction(w http.ResponseWriter, r *http.Request) {
	id, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return
	}
	if !s.permitted(r.Context(), id, "accounts") {
		reply(w, map[string]string{"error": "仅管理员可以管理账号"}, 403)
		return
	}
	if r.URL.Path == "/api/accounts/export" {
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		s.accountPage(w, r, id, s.permitted(r.Context(), id, "accounts"))
		return
	}
	if r.URL.Path == "/api/accounts/payment-cards" {
		s.accountPaymentCards(w, r)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/accounts/"), "/")
	aid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || aid <= 0 {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 2 && parts[1] == "subscription" {
		s.subscriptionSettings(w, r, id, aid)
		return
	}
	if len(parts) == 2 && parts[1] == "notes" {
		s.accountNotes(w, r, aid)
		return
	}
	if len(parts) == 2 && parts[1] == "owner" {
		s.accountOwner(w, r, id, aid)
		return
	}
	if len(parts) == 2 && parts[1] == "billing-address" {
		s.setAccountBillingAddress(w, r, id, aid)
		return
	}
	if len(parts) == 2 && parts[1] == "payment-card" {
		s.setAccountPaymentCard(w, r, id, aid)
		return
	}
	if len(parts) == 2 && parts[1] == "renew" && r.Method == http.MethodPost {
		reply(w, map[string]string{"error": "代币续订功能已停用"}, http.StatusGone)
		return
	}
	if len(parts) == 2 && parts[1] == "group" && r.Method == http.MethodPatch {
		s.setAccountGroup(w, r, id, aid)
		return
	}
	if len(parts) == 2 && parts[1] == "login" && r.Method == http.MethodPost {
		s.recordAccountLogin(w, r, id, aid)
		return
	}
	if len(parts) == 2 && parts[1] == "renewal-date" && r.Method == http.MethodPatch {
		s.setRenewalDate(w, r, id, aid)
		return
	}
	if len(parts) == 2 && ((parts[1] == "browser-session" && r.Method == http.MethodPost) || (parts[1] == "session" && r.Method == http.MethodPatch)) {
		s.accountSession(w, r, id, aid, parts[1] == "browser-session")
		return
	}
	if len(parts) == 2 && parts[1] == "browser" {
		s.accountBrowser(w, r, id, aid)
		return
	}
	if len(parts) != 1 || r.Method != http.MethodDelete {
		w.WriteHeader(405)
		return
	}
	admin, err := s.isAdmin(r.Context(), id)
	if err != nil {
		reply(w, map[string]string{"error": "用户不存在"}, 401)
		return
	}
	result, err := s.db.ExecContext(r.Context(), `UPDATE chatgpt_accounts SET updated_at=NOW(),deleted_at=NOW() WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR $3)`, aid, id, admin)
	if err != nil {
		reply(w, map[string]string{"error": "删除失败"}, 500)
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(204)
}

func (s *Server) setRenewalDate(w http.ResponseWriter, r *http.Request, id, aid int64) {
	admin, err := s.isAdmin(r.Context(), id)
	if err != nil || !admin {
		reply(w, map[string]string{"error": "仅管理员可以设置续订日期"}, 403)
		return
	}
	var in struct {
		Date string `json:"renewal_date"`
	}
	if jsonBody(r, &in) != nil {
		reply(w, map[string]string{"error": "请求格式错误"}, 400)
		return
	}
	var date any
	if in.Date != "" {
		d, err := time.Parse("2006-01-02", in.Date)
		if err != nil || d.Year() < 2000 || d.Year() > 9999 {
			reply(w, map[string]string{"error": "请输入有效续订日期"}, 400)
			return
		}
		date = in.Date
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		reply(w, map[string]string{"error": "保存失败"}, 500)
		return
	}
	defer tx.Rollback()
	var previous sql.NullTime
	err = tx.QueryRowContext(r.Context(), `SELECT renewal_date FROM chatgpt_accounts WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, aid).Scan(&previous)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE chatgpt_accounts SET updated_at=NOW(),renewal_date=$1 WHERE id=$2 AND deleted_at IS NULL`, date, aid)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `INSERT INTO renewal_date_audit(account_id,admin_id,previous_date,renewal_date) VALUES($1,$2,$3,$4)`, aid, id, previous, date)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		reply(w, map[string]string{"error": "保存续订日期失败"}, 500)
		return
	}
	reply(w, map[string]string{"message": "续订日期已更新"}, 200)
}

// 用户列表只返回允许展示的字段，管理字段不下发到客户端。
func accountView(a Account, admin bool) any {
	if admin {
		return a
	}
	return map[string]any{"id": a.ID, "label": a.Label, "email": a.Email, "last_login_at": a.LastLoginAt}
}
func accountViews(accounts []Account, admin bool) []any {
	result := make([]any, 0, len(accounts))
	for _, a := range accounts {
		result = append(result, accountView(a, admin))
	}
	return result
}
