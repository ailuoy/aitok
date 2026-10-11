package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// 配置为全站共享；列表不下发密码，明文仅用于管理员编辑及本机执行。
type sharedProxy struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Host        string           `json:"host"`
	Port        json.Number      `json:"port"`
	Username    string           `json:"username"`
	Password    *string          `json:"password,omitempty"`
	HasPassword bool             `json:"has_password"`
	LastTest    *sharedProxyTest `json:"last_test"`
}

type sharedProxyTest struct {
	OK        bool     `json:"ok"`
	ExitIP    string   `json:"exit_ip"`
	ProxyIPs  []string `json:"proxy_ips"`
	Matches   bool     `json:"matches"`
	LatencyMS int64    `json:"latency_ms"`
	TestedAt  string   `json:"tested_at"`
	Error     string   `json:"error,omitempty"`
}

func (p *sharedProxy) normalize() error {
	p.Name = strings.TrimSpace(p.Name)
	p.Host = strings.Trim(strings.TrimSpace(p.Host), "[]")
	port, err := strconv.Atoi(p.Port.String())
	if err != nil || port < 1 || port > 65535 {
		return operationConflict("代理端口必须介于 1 和 65535")
	}
	if p.Name == "" || utf8.RuneCountInString(p.Name) > 80 || p.Host == "" || len(p.Host) > 253 || strings.ContainsAny(p.Host, "/@?#\\") || strings.IndexFunc(p.Host, unicode.IsSpace) >= 0 || strings.ContainsRune(p.Name, '\x00') || strings.ContainsRune(p.Host, '\x00') {
		return operationConflict("请填写有效的代理名称和主机")
	}
	if p.Password == nil {
		return operationConflict("请提供代理密码字段，无认证时留空")
	}
	if (p.Username == "") != (*p.Password == "") || len(p.Username) > 255 || len(*p.Password) > 255 || strings.ContainsRune(p.Username, '\x00') || strings.ContainsRune(*p.Password, '\x00') {
		return operationConflict("用户名和密码需同时填写或同时清空，且各不超过 255 字节")
	}
	if p.LastTest != nil {
		result := p.LastTest
		if result.ProxyIPs == nil {
			result.ProxyIPs = []string{}
		}
		if _, err := time.Parse(time.RFC3339Nano, result.TestedAt); err != nil || result.LatencyMS < 0 || result.LatencyMS > 600000 || len(result.ProxyIPs) > 100 || len(result.Error) > 2000 {
			return operationConflict("本机测试结果格式无效，请重新测试")
		}
		if result.OK && net.ParseIP(result.ExitIP) == nil {
			return operationConflict("测试结果缺少有效出口 IP")
		}
		for _, ip := range result.ProxyIPs {
			if net.ParseIP(ip) == nil {
				return operationConflict("测试结果的代理 IP 无效")
			}
		}
		if result.OK {
			result.Error = ""
		} else if *p.Password != "" {
			result.Error = strings.ReplaceAll(result.Error, *p.Password, "***")
		}
	}
	address := &url.URL{Scheme: "socks5", Host: net.JoinHostPort(p.Host, p.Port.String())}
	if p.Username != "" {
		address.User = url.UserPassword(p.Username, *p.Password)
	}
	return validateBrowserProxy(address.String())
}

func scanSharedProxy(row interface{ Scan(...any) error }, secret bool) (sharedProxy, error) {
	var p sharedProxy
	var port int
	var encrypted string
	var result []byte
	err := row.Scan(&p.ID, &p.Name, &p.Host, &port, &p.Username, &encrypted, &result)
	if err != nil {
		return p, err
	}
	p.Port = json.Number(strconv.Itoa(port))
	p.HasPassword = encrypted != ""
	if len(result) > 0 {
		if err = json.Unmarshal(result, &p.LastTest); err != nil {
			return p, err
		}
	}
	if secret {
		password := ""
		if encrypted != "" {
			password, err = decryptSession(encrypted)
		}
		p.Password = &password
	}
	return p, err
}

const sharedProxyColumns = `id::text,name,host,port,username,password_ciphertext,last_test`

func (s *Server) sharedProxyConfig(ctx context.Context) (map[string]any, error) {
	// 同一快照读取列表和绑定，避免并发删除/重绑产生不一致视图。
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT `+sharedProxyColumns+` FROM socks5_proxies WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, err
	}
	proxies := []sharedProxy{}
	for rows.Next() {
		p, e := scanSharedProxy(rows, false)
		if e != nil {
			rows.Close()
			return nil, e
		}
		proxies = append(proxies, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT a.id::text,a.socks5_proxy_id::text FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id AND u.deleted_at IS NULL JOIN socks5_proxies p ON p.id=a.socks5_proxy_id AND p.deleted_at IS NULL WHERE a.deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	bindings := map[string]string{}
	for rows.Next() {
		var account, proxy string
		if err = rows.Scan(&account, &proxy); err != nil {
			rows.Close()
			return nil, err
		}
		bindings[account] = proxy
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return map[string]any{"proxies": proxies, "bindings": bindings}, nil
}

func (s *Server) replySharedProxies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	config, err := s.sharedProxyConfig(r.Context())
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, config, 200)
}

// 所有共享代理写入共用事务锁，保证删除检查、绑定与首次导入不会交错。
func (s *Server) sharedProxyTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('aitok:shared-proxies'))`); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func saveSharedProxy(ctx context.Context, tx *sql.Tx, id int64, p sharedProxy) (int64, error) {
	if err := p.normalize(); err != nil {
		return 0, err
	}
	encrypted := ""
	var err error
	if *p.Password != "" {
		encrypted, err = encryptSession(*p.Password)
		if err != nil {
			return 0, operationConflict("无法加密代理密码，请检查服务端加密配置")
		}
	}
	result, err := json.Marshal(p.LastTest)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM socks5_proxies WHERE deleted_at IS NULL`).Scan(&count); err != nil {
			return 0, err
		}
		if count >= 500 {
			return 0, operationConflict("最多保存 500 条共享代理")
		}
		err = tx.QueryRowContext(ctx, `INSERT INTO socks5_proxies(name,host,port,username,password_ciphertext,last_test) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, p.Name, p.Host, p.Port.String(), p.Username, encrypted, result).Scan(&id)
	} else {
		err = tx.QueryRowContext(ctx, `UPDATE socks5_proxies SET name=$2,host=$3,port=$4,username=$5,password_ciphertext=$6,last_test=$7,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL RETURNING id`, id, p.Name, p.Host, p.Port.String(), p.Username, encrypted, result).Scan(&id)
	}
	return id, err
}

func (s *Server) sharedProxies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, err := s.auth(r)
	if err != nil {
		w.WriteHeader(401)
		return
	}
	if admin, e := s.isAdmin(r.Context(), actor); e != nil || !admin {
		w.WriteHeader(403)
		return
	}
	suffix := strings.TrimPrefix(r.URL.Path, "/api/proxies")
	if suffix == "/import" && r.Method == "POST" {
		s.importSharedProxies(w, r, actor)
		return
	}
	var id int64
	testResult := strings.HasSuffix(suffix, "/test-result")
	if suffix != "" {
		id, err = strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(suffix, "/"), "/test-result"), 10, 64)
		if err != nil || id <= 0 {
			http.NotFound(w, r)
			return
		}
	}
	if r.Method == "GET" && !testResult {
		if id == 0 {
			s.replySharedProxies(w, r)
			return
		}
		p, e := scanSharedProxy(s.db.QueryRowContext(r.Context(), `SELECT `+sharedProxyColumns+` FROM socks5_proxies WHERE id=$1 AND deleted_at IS NULL`, id), true)
		if e != nil {
			operationError(w, e)
			return
		}
		reply(w, p, 200)
		return
	}
	if !(r.Method == "POST" && (id == 0 || testResult) || r.Method == "PATCH" && id > 0 && !testResult || r.Method == "DELETE" && id > 0 && !testResult) {
		w.WriteHeader(405)
		return
	}
	var p sharedProxy
	if r.Method != "DELETE" {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if jsonBody(r, &p) != nil {
			reply(w, map[string]string{"error": "代理请求格式错误"}, 400)
			return
		}
		if !testResult && (p.LastTest == nil || !p.LastTest.OK) {
			reply(w, map[string]string{"error": "请先在本机测试代理，测试通过后保存"}, 400)
			return
		}
		if err = p.normalize(); err != nil {
			operationError(w, err)
			return
		}
	}
	tx, err := s.sharedProxyTx(r.Context())
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	action := "save"
	if r.Method == "DELETE" {
		var bound bool
		err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id AND u.deleted_at IS NULL WHERE a.socks5_proxy_id=$1 AND a.deleted_at IS NULL)`, id).Scan(&bound)
		if err == nil && bound {
			err = operationConflict("此代理仍有账号使用，请先更改账号的代理选择")
		}
		if err == nil {
			err = tx.QueryRowContext(r.Context(), `UPDATE socks5_proxies SET deleted_at=NOW(),updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL RETURNING id`, id).Scan(&id)
		}
		action = "delete"
	} else if testResult {
		var current sharedProxy
		current, err = scanSharedProxy(tx.QueryRowContext(r.Context(), `SELECT `+sharedProxyColumns+` FROM socks5_proxies WHERE id=$1 AND deleted_at IS NULL`, id), true)
		if err == nil && (p.Host != current.Host || p.Port != current.Port || p.Username != current.Username || *p.Password != *current.Password) {
			err = operationConflict("代理配置已变更，请刷新后重新测试")
		}
		if err == nil {
			raw, _ := json.Marshal(p.LastTest)
			_, err = tx.ExecContext(r.Context(), `UPDATE socks5_proxies SET last_test=$2,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, id, raw)
		}
		action = "local_test_report"
	} else {
		id, err = saveSharedProxy(r.Context(), tx, id, p)
	}
	if err == nil {
		err = recordEvent(r.Context(), tx, actor, id, "proxy", action, eventKey(), nil, map[string]any{"source": "shared_config"})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	s.replySharedProxies(w, r)
}

func (s *Server) bindSharedProxy(w http.ResponseWriter, r *http.Request, actor, account int64) {
	if r.Method != "PATCH" {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var input struct {
		ProxyID json.RawMessage `json:"proxy_id"`
	}
	if jsonBody(r, &input) != nil || len(input.ProxyID) == 0 {
		w.WriteHeader(400)
		return
	}
	var proxy any
	if string(input.ProxyID) != "null" {
		var value string
		if json.Unmarshal(input.ProxyID, &value) != nil {
			w.WriteHeader(400)
			return
		}
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			w.WriteHeader(400)
			return
		}
		proxy = id
	}
	tx, err := s.sharedProxyTx(r.Context())
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	if proxy != nil {
		var exists bool
		err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM socks5_proxies WHERE id=$1 AND deleted_at IS NULL)`, proxy).Scan(&exists)
		if err == nil && !exists {
			err = sql.ErrNoRows
		}
	}
	if err == nil {
		err = tx.QueryRowContext(r.Context(), `UPDATE chatgpt_accounts a SET socks5_proxy_id=$2,updated_at=NOW() WHERE a.id=$1 AND a.deleted_at IS NULL AND EXISTS(SELECT 1 FROM users u WHERE u.id=a.user_id AND u.deleted_at IS NULL) RETURNING a.id`, account, proxy).Scan(&account)
	}
	if err == nil {
		err = recordEvent(r.Context(), tx, actor, account, "account", "proxy", eventKey(), nil, map[string]any{"proxy_id": proxy})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	s.replySharedProxies(w, r)
}

func (s *Server) accountSharedProxy(ctx context.Context, account int64) (*sharedProxy, error) {
	var proxy sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT a.socks5_proxy_id FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id AND u.deleted_at IS NULL WHERE a.id=$1 AND a.deleted_at IS NULL`, account).Scan(&proxy)
	if err != nil || !proxy.Valid {
		return nil, err
	}
	p, err := scanSharedProxy(s.db.QueryRowContext(ctx, `SELECT `+sharedProxyColumns+` FROM socks5_proxies WHERE id=$1 AND deleted_at IS NULL`, proxy.Int64), true)
	// 绑定失效时明确报错，不能静默回退直连。
	return &p, err
}

// 仅允许在共享列表为空时一次性迁移，避免其他电脑的旧配置覆盖全站选择。
func (s *Server) importSharedProxies(w http.ResponseWriter, r *http.Request, actor int64) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	var input struct {
		Proxies  []sharedProxy     `json:"proxies"`
		Bindings map[string]string `json:"bindings"`
	}
	if jsonBody(r, &input) != nil || len(input.Proxies) == 0 || len(input.Proxies) > 500 || len(input.Bindings) > 10000 {
		reply(w, map[string]string{"error": "本机配置格式无效或超过导入上限"}, 400)
		return
	}
	tx, err := s.sharedProxyTx(r.Context())
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	var count int
	err = tx.QueryRowContext(r.Context(), `SELECT count(*) FROM socks5_proxies WHERE deleted_at IS NULL`).Scan(&count)
	if err == nil && count != 0 {
		reply(w, map[string]string{"error": "共享代理已存在，不能再用本机配置覆盖"}, 409)
		return
	}
	ids := map[string]int64{}
	for _, p := range input.Proxies {
		if err != nil {
			break
		}
		if p.ID == "" || ids[p.ID] != 0 {
			err = operationConflict("本机代理标识为空或重复")
			break
		}
		// 历史测试仅用于本机展示，迁移后不作为其他设备的连通性证明。
		p.LastTest = nil
		ids[p.ID], err = saveSharedProxy(r.Context(), tx, 0, p)
	}
	for key, oldProxy := range input.Bindings {
		if err != nil {
			break
		}
		account, e := strconv.ParseInt(key, 10, 64)
		if e != nil || account <= 0 || ids[oldProxy] == 0 {
			err = operationConflict("本机账号代理绑定无效")
			break
		}
		// 已删除账号及所属用户不迁移；不恢复或修改历史对象。
		_, err = tx.ExecContext(r.Context(), `UPDATE chatgpt_accounts a SET socks5_proxy_id=$2,updated_at=NOW() WHERE a.id=$1 AND a.deleted_at IS NULL AND EXISTS(SELECT 1 FROM users u WHERE u.id=a.user_id AND u.deleted_at IS NULL)`, account, ids[oldProxy])
	}
	if err == nil {
		err = recordEvent(r.Context(), tx, actor, 0, "proxy", "import_local", eventKey(), nil, map[string]any{"count": len(ids)})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	s.replySharedProxies(w, r)
}
