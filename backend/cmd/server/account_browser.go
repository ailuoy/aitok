package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const accountCredentialsPrefix = "aitok-credentials-v1:"

type accountCredentials struct {
	SessionJSON string `json:"session_json"`
	ProxyURL    string `json:"proxy_url"`
}

func decodeAccountCredentials(encrypted string) (accountCredentials, error) {
	raw, err := decryptSession(encrypted)
	if err != nil {
		return accountCredentials{}, err
	}
	if !strings.HasPrefix(raw, accountCredentialsPrefix) {
		return accountCredentials{SessionJSON: raw}, nil
	}
	var credentials accountCredentials
	err = json.Unmarshal([]byte(strings.TrimPrefix(raw, accountCredentialsPrefix)), &credentials)
	return credentials, err
}

func encodeAccountCredentials(credentials accountCredentials) (string, error) {
	data, err := json.Marshal(credentials)
	if err != nil {
		return "", err
	}
	return encryptSession(accountCredentialsPrefix + string(data))
}

func (s *Server) managedAccount(ctx context.Context, userID, accountID int64) (encrypted, email string, err error) {
	admin, err := s.isAdmin(ctx, userID)
	if err != nil {
		return "", "", err
	}
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(session_ciphertext,''),email FROM chatgpt_accounts WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR $3)`, accountID, userID, admin).Scan(&encrypted, &email)
	return
}

func validateBrowserProxy(raw string) error {
	if raw == "" {
		return nil
	}
	proxy, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || proxy.Scheme != "socks5" || proxy.Hostname() == "" || proxy.RawQuery != "" || proxy.Fragment != "" || (proxy.Path != "" && proxy.Path != "/") {
		return errors.New("代理格式应为 socks5://主机:端口，可包含用户名和密码")
	}
	port, err := strconv.Atoi(proxy.Port())
	if err != nil || port < 1 || port > 65535 {
		return errors.New("请输入有效 SOCKS5 代理端口")
	}
	if proxy.User != nil {
		password, _ := proxy.User.Password()
		if (proxy.User.Username() == "") != (password == "") || len(proxy.User.Username()) > 255 || len(password) > 255 {
			return errors.New("代理用户名和密码需同时填写，且各不超过 255 字节")
		}
	}
	return nil
}

func browserProxySettings(proxyURL string) map[string]any {
	address := ""
	if proxy, err := url.Parse(proxyURL); err == nil && proxy.Host != "" {
		address = proxy.Scheme + "://" + proxy.Host
	}
	return map[string]any{"has_proxy": proxyURL != "", "proxy_address": address}
}

func (s *Server) accountBrowser(w http.ResponseWriter, r *http.Request, userID, accountID int64) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" && r.Method != "POST" && r.Method != "PATCH" && r.Method != "DELETE" {
		w.WriteHeader(405)
		return
	}
	admin, err := s.isAdmin(r.Context(), userID)
	if err != nil || !admin {
		reply(w, map[string]string{"error": "仅管理员可以操作后台电脑的浏览器"}, 403)
		return
	}
	if r.Method == "POST" && !s.consumeTOTP(w, r, userID, r.Header.Get("X-Aitok-TOTP"), false) {
		return
	}
	var encrypted, email string
	err = s.db.QueryRowContext(r.Context(), `SELECT COALESCE(session_ciphertext,''),email FROM chatgpt_accounts WHERE id=$1 AND deleted_at IS NULL`, accountID).Scan(&encrypted, &email)
	if errors.Is(err, sql.ErrNoRows) {
		reply(w, map[string]string{"error": "账号不存在"}, 404)
		return
	}
	if err != nil {
		reply(w, map[string]string{"error": "读取账号失败"}, 500)
		return
	}
	credentials, err := decodeAccountCredentials(encrypted)
	if err != nil && r.Method != "DELETE" {
		reply(w, map[string]string{"error": "无法读取账号凭据，请检查加密配置或更新 Session"}, 422)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	params := map[string]any{"environment_id": fmt.Sprintf("account:%d", accountID)}
	method := "status"
	if r.Method == "POST" || r.Method == "PATCH" {
		r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
		var in struct {
			ProxyURL *string `json:"proxy_url"`
		}
		if jsonBody(r, &in) != nil {
			reply(w, map[string]string{"error": "请求格式错误"}, 400)
			return
		}
		if in.ProxyURL != nil {
			credentials.ProxyURL = strings.TrimSpace(*in.ProxyURL)
			if err := validateBrowserProxy(credentials.ProxyURL); err != nil {
				reply(w, map[string]string{"error": err.Error()}, 400)
				return
			}
			encoded, err := encodeAccountCredentials(credentials)
			if err != nil {
				reply(w, map[string]string{"error": "无法加密代理配置"}, 503)
				return
			}
			result, err := s.db.ExecContext(ctx, `UPDATE chatgpt_accounts SET updated_at=NOW(),session_ciphertext=$1 WHERE id=$2 AND deleted_at IS NULL AND session_ciphertext=$3`, encoded, accountID, encrypted)
			if err != nil {
				reply(w, map[string]string{"error": "保存代理失败"}, 500)
				return
			}
			if count, _ := result.RowsAffected(); count == 0 {
				reply(w, map[string]string{"error": "账号已被更新，请刷新后重试"}, 409)
				return
			}
		}
		if r.Method == "PATCH" {
			reply(w, map[string]any{"settings": browserProxySettings(credentials.ProxyURL)}, 200)
			return
		}
		session, err := parseChatGPTSession(credentials.SessionJSON)
		if err != nil {
			reply(w, map[string]string{"error": err.Error()}, 422)
			return
		}
		if session.Email != "" && !strings.EqualFold(session.Email, email) {
			reply(w, map[string]string{"error": "Session 邮箱与账号不一致，请更新 Session"}, 409)
			return
		}
		if session.ExpiresAt != nil && !session.ExpiresAt.After(time.Now()) {
			reply(w, map[string]string{"error": "Session 已过期，请重新导入"}, 422)
			return
		}
		params["session"], params["proxy_url"], params["expected_email"] = session.BrowserJSON, credentials.ProxyURL, email
		method = "start"
	} else if r.Method == "DELETE" {
		method = "stop"
	}
	var result json.RawMessage
	if s.browser == nil {
		err = errors.New("服务器浏览器服务未配置")
	} else {
		result, err = s.browser.Call(ctx, method, params)
	}
	if err != nil {
		if method == "status" {
			reply(w, map[string]any{"browser": map[string]string{"state": "unavailable", "message": err.Error()}, "settings": browserProxySettings(credentials.ProxyURL)}, 200)
		} else {
			reply(w, map[string]string{"error": err.Error()}, 503)
		}
		return
	}
	var status struct {
		AuthenticatedAt *time.Time `json:"authenticated_at"`
	}
	if json.Unmarshal(result, &status) == nil && status.AuthenticatedAt != nil {
		if _, err = s.db.ExecContext(ctx, `UPDATE chatgpt_accounts SET updated_at=NOW(),last_login_at=GREATEST(last_login_at,$1) WHERE id=$2 AND deleted_at IS NULL`, status.AuthenticatedAt, accountID); err != nil {
			reply(w, map[string]string{"error": "登录时间保存失败，请重试"}, 500)
			return
		}
	}
	reply(w, map[string]any{"browser": result, "settings": browserProxySettings(credentials.ProxyURL)}, 200)
}
