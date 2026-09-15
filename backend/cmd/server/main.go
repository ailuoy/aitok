package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"

	khttp "github.com/go-kratos/kratos/v2/transport/http"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Server struct {
	db      *sql.DB
	secret  []byte
	admin   adminConfig
	mailer  *cloudflareMailer
	billing billingConfig
	stripe  stripeGateway
	browser browserService
}
type User struct {
	ID       int64  `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username,omitempty"`
	Role     string `json:"role"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:15682/getgpt?sslmode=disable"
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	config, err := loadBillingConfig()
	if err != nil {
		return err
	}
	browser := &browserRuntime{}
	defer browser.Close()
	s := &Server{db: db, secret: secret(), admin: loadAdminConfig(), mailer: newMailer(), billing: config, stripe: newStripeGateway(config.SecretKey), browser: browser}
	if err := db.Ping(); err != nil {
		return fmt.Errorf("数据库连接失败: %w", err)
	}
	addr := envDefault("BACKEND_ADDR", ":15681")
	return newApp(s.routes(khttp.Address(addr))).Run()
}

func secret() []byte {
	v := os.Getenv("JWT_SECRET")
	if len(v) < 32 {
		log.Fatal("请在 .env 配置 JWT_SECRET（至少 32 个字符）")
	}
	return []byte(v)
}
func jsonBody(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }
func reply(w http.ResponseWriter, v any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func hash(p string) string { b := sha256.Sum256([]byte(p)); return fmt.Sprintf("%x", b) }
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Password string }
	if jsonBody(r, &in) != nil || !validEmail(in.Email) || len(in.Password) < 6 {
		reply(w, map[string]string{"error": "请输入有效邮箱和至少 6 位密码"}, 400)
		return
	}
	var id int64
	err := s.db.QueryRowContext(r.Context(), `INSERT INTO users(email,password_hash,role) VALUES($1,$2,'user') RETURNING id`, strings.ToLower(in.Email), hash(in.Password)).Scan(&id)
	if err != nil {
		reply(w, map[string]string{"error": "邮箱已注册或数据库不可用"}, 409)
		return
	}
	reply(w, map[string]string{"token": s.token(id)}, 201)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct{ Email, Username, Password string }
	if jsonBody(r, &in) != nil {
		reply(w, map[string]string{"error": "请求格式错误"}, 400)
		return
	}
	identity := strings.TrimSpace(in.Username)
	if identity == "" {
		identity = strings.TrimSpace(in.Email)
	}
	if identity == s.admin.Username {
		s.loginAdmin(w, r, in.Password)
		return
	}
	if !validEmail(identity) {
		reply(w, map[string]string{"error": "用户名、邮箱或密码错误"}, 401)
		return
	}
	var id int64
	if err := s.db.QueryRowContext(r.Context(), `SELECT id FROM users WHERE email=$1 AND deleted_at IS NULL AND password_hash=$2`, strings.ToLower(identity), hash(in.Password)).Scan(&id); err != nil {
		reply(w, map[string]string{"error": "用户名、邮箱或密码错误"}, 401)
		return
	}
	reply(w, map[string]string{"token": s.token(id)}, 200)
}

func validEmail(value string) bool {
	value = strings.TrimSpace(value)
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Address == value && strings.Contains(value, "@")
}

func (s *Server) sendCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var in struct{ Email, Purpose string }
	if jsonBody(r, &in) != nil {
		reply(w, map[string]string{"error": "请求格式错误"}, 400)
		return
	}
	if in.Purpose == "" {
		in.Purpose = "login"
	}
	s.issueCode(w, r, in.Email, in.Purpose)
}

func (s *Server) issueCode(w http.ResponseWriter, r *http.Request, email, purpose string) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !validEmail(email) || (purpose != "login" && purpose != "reset") {
		reply(w, map[string]string{"error": "请输入有效邮箱和验证码用途"}, 400)
		return
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		reply(w, map[string]string{"error": "验证码生成失败"}, 500)
		return
	}
	code := fmt.Sprintf("%06d", n.Int64())
	minutes := codeExpiryMinutes()
	// 邮件发送失败时不保存可用验证码，也不向客户端返回验证码。
	if err := s.mailer.sendCode(r.Context(), email, code, minutes); err != nil {
		reply(w, map[string]string{"error": "邮件发送失败，请检查 Cloudflare 邮件配置及发件域名"}, 502)
		return
	}
	_, err = s.db.ExecContext(r.Context(), `INSERT INTO email_codes(email,purpose,code,expires_at) VALUES($1,$2,$3,$4) ON CONFLICT(email,purpose) WHERE deleted_at IS NULL DO UPDATE SET code=EXCLUDED.code,expires_at=EXCLUDED.expires_at`, email, purpose, hash(code), time.Now().Add(time.Duration(minutes)*time.Minute))
	if err != nil {
		reply(w, map[string]string{"error": "验证码保存失败，请稍后重试"}, 500)
		return
	}
	reply(w, map[string]string{"message": "验证码已发送"}, 200)
}
func (s *Server) verifyCode(email, purpose, code string) bool {
	if !validEmail(email) || len(code) != 6 {
		return false
	}
	// 一次性消费，避免同一验证码被重复使用。
	result, err := s.db.Exec(`UPDATE email_codes SET deleted_at=NOW() WHERE email=$1 AND purpose=$2 AND code=$3 AND deleted_at IS NULL AND expires_at>NOW()`, strings.ToLower(strings.TrimSpace(email)), purpose, hash(code))
	if err != nil {
		return false
	}
	count, err := result.RowsAffected()
	return err == nil && count == 1
}
func (s *Server) loginCode(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Code string }
	if jsonBody(r, &in) != nil || !s.verifyCode(strings.ToLower(in.Email), "login", in.Code) {
		reply(w, map[string]string{"error": "验证码无效或已过期"}, 401)
		return
	}
	var id int64
	err := s.db.QueryRow(`SELECT id FROM users WHERE email=$1 AND deleted_at IS NULL`, strings.ToLower(in.Email)).Scan(&id)
	if err == sql.ErrNoRows {
		err = s.db.QueryRow(`INSERT INTO users(email,password_hash,role) VALUES($1,$2,'user') RETURNING id`, strings.ToLower(in.Email), "").Scan(&id)
	}
	if err != nil {
		reply(w, map[string]string{"error": "登录失败"}, 500)
		return
	}
	reply(w, map[string]string{"token": s.token(id)}, 200)
}
func (s *Server) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Code, Password string }
	if jsonBody(r, &in) != nil {
		reply(w, map[string]string{"error": "请求格式错误"}, 400)
		return
	}
	if in.Code == "" {
		s.issueCode(w, r, in.Email, "reset")
		return
	}
	if len(in.Password) < 6 || !s.verifyCode(strings.ToLower(in.Email), "reset", in.Code) {
		reply(w, map[string]string{"error": "验证码无效或密码不符合要求"}, 400)
		return
	}
	_, e := s.db.Exec(`UPDATE users SET password_hash=$1 WHERE email=$2 AND deleted_at IS NULL`, hash(in.Password), strings.ToLower(in.Email))
	if e != nil {
		reply(w, map[string]string{"error": "重置失败"}, 500)
		return
	}
	reply(w, map[string]string{"message": "密码已重置"}, 200)
}
func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) { s.forgotPassword(w, r) }
func (s *Server) token(id int64) string {
	p := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10) + ":" + strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10)))
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(p))
	return p + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
func (s *Server) auth(r *http.Request) (int64, error) {
	v := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(v, ".")
	if len(parts) != 2 {
		return 0, errors.New("unauthorized")
	}
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(parts[0]))
	sig, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil || !hmac.Equal(sig, m.Sum(nil)) {
		return 0, errors.New("unauthorized")
	}
	b, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return 0, e
	}
	var id, exp int64
	if _, e = fmt.Sscanf(string(b), "%d:%d", &id, &exp); e != nil || time.Now().Unix() > exp {
		return 0, errors.New("expired")
	}
	// 软删除用户的旧令牌立即失效，所有受保护接口统一校验。
	var active bool
	if e = s.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND deleted_at IS NULL)`, id).Scan(&active); e != nil || !active {
		return 0, errors.New("unauthorized")
	}
	return id, nil
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	id, e := s.auth(r)
	if e != nil {
		reply(w, map[string]string{"error": "未登录"}, 401)
		return
	}
	var u User
	if e = s.db.QueryRow(`SELECT id,email,COALESCE(role,'') FROM users WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&u.ID, &u.Email, &u.Role); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			reply(w, map[string]string{"error": "用户不存在，请重新登录"}, 401)
			return
		}
		reply(w, map[string]string{"error": "服务暂时不可用，请稍后重试"}, 503)
		return
	}
	u.Role = userRole(u.Email, u.Role)
	if u.Email == adminIdentity {
		u.Email = ""
		u.Username = s.admin.Username
		u.Role = "super_admin"
	}

	as, err := s.listAccounts(r.Context(), id, u.Role == "super_admin" || u.Role == "admin")
	if err != nil {
		reply(w, map[string]string{"error": "读取账号失败，请确认数据库迁移已执行"}, 500)
		return
	}
	reply(w, map[string]any{"user": u, "accounts": as}, 200)
}
