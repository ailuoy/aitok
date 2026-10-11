package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func proxyTestInput() map[string]any {
	return map[string]any{"name": "共享代理", "host": "203.0.113.10", "port": 1080, "username": "proxy-user", "password": "proxy-secret", "last_test": map[string]any{"ok": true, "exit_ip": "203.0.113.10", "proxy_ips": []string{"203.0.113.10"}, "matches": true, "tested_at": "2026-10-11T00:00:00Z"}}
}

func TestSharedProxyConfigAndBinding(t *testing.T) {
	db, call := accountSettingsTest(t)
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("p", 32))))
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(6,'other-admin@test.local','','admin')`); err != nil {
		t.Fatal(err)
	}
	input := proxyTestInput()
	call("GET", "/api/proxies", 2, nil, 403)
	call("POST", "/api/proxies", 2, input, 403)
	config := call("POST", "/api/proxies", 1, input, 200)
	p := config["proxies"].([]any)[0].(map[string]any)
	id := p["id"].(string)
	if _, ok := p["password"]; ok {
		t.Fatal("列表泄露密码")
	}
	if p["has_password"] != true {
		t.Fatal("列表丢失认证状态")
	}
	var encrypted string
	if err := db.QueryRow(`SELECT password_ciphertext FROM socks5_proxies WHERE id=$1`, id).Scan(&encrypted); err != nil || encrypted == "proxy-secret" {
		t.Fatal("密码未加密", err)
	}
	call("PATCH", "/api/accounts/1/proxy", 2, map[string]any{"proxy_id": id}, 403)
	call("PATCH", "/api/accounts/1/proxy", 1, map[string]any{}, 400)
	call("PATCH", "/api/accounts/1/proxy", 1, map[string]any{"proxy_id": "99999"}, 404)
	call("PATCH", "/api/accounts/1/proxy", 1, map[string]any{"proxy_id": id}, 200)
	other := call("GET", "/api/proxies", 6, nil, 200)
	if other["bindings"].(map[string]any)["1"] != id || len(other["proxies"].([]any)) != 1 {
		t.Fatal("其他管理员未读取相同配置", other)
	}
	detail := call("GET", "/api/proxies/"+id, 6, nil, 200)
	if detail["password"] != "proxy-secret" {
		t.Fatal("编辑详情未解密密码")
	}
	s := &Server{db: db}
	selected, err := s.accountSharedProxy(context.Background(), 1)
	if err != nil || selected == nil || selected.ID != id || *selected.Password != "proxy-secret" {
		t.Fatal("账号启动未使用共享代理", err)
	}
	call("DELETE", "/api/proxies/"+id, 6, nil, 409)
	input["name"] = "另一管理员修改"
	call("PATCH", "/api/proxies/"+id, 6, input, 200)
	input["host"] = "203.0.113.99"
	call("POST", "/api/proxies/"+id+"/test-result", 1, input, 409)
	input["host"] = "203.0.113.10"
	call("POST", "/api/proxies/"+id+"/test-result", 1, input, 200)
	call("PATCH", "/api/accounts/1/proxy", 6, map[string]any{"proxy_id": nil}, 200)
	selected, err = s.accountSharedProxy(context.Background(), 1)
	if err != nil || selected != nil {
		t.Fatal("清空共享绑定未生效", err)
	}
	// 已软删除用户的历史账号不阻塞活跃代理管理，也不能再导出凭据。
	if _, err = db.Exec(`UPDATE chatgpt_accounts SET socks5_proxy_id=$1 WHERE id=5`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.accountSharedProxy(context.Background(), 5); err == nil {
		t.Fatal("已删除用户的账号仍可读取代理凭据")
	}
	call("DELETE", "/api/proxies/"+id, 6, nil, 200)
	call("GET", "/api/proxies/"+id, 1, nil, 404)
	call("PATCH", "/api/accounts/1/proxy", 1, map[string]any{"proxy_id": id}, 404)
	var retained bool
	if err = db.QueryRow(`SELECT deleted_at IS NOT NULL AND updated_at>=created_at FROM socks5_proxies WHERE id=$1`, id).Scan(&retained); err != nil || !retained {
		t.Fatal("删除未保留原行", err)
	}
	input["host"] = "bad/host"
	call("POST", "/api/proxies", 1, input, 409)
	var leaked bool
	if err = db.QueryRow(`SELECT EXISTS(SELECT 1 FROM operation_events WHERE before_data::text LIKE '%proxy-secret%' OR after_data::text LIKE '%proxy-secret%')`).Scan(&leaked); err != nil || leaked {
		t.Fatal("审计泄露密码", err)
	}
}

func TestImportSharedProxiesAtomic(t *testing.T) {
	db, call := accountSettingsTest(t)
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("p", 32))))
	input := proxyTestInput()
	input["id"] = "legacy-uuid"
	payload := map[string]any{"proxies": []any{input}, "bindings": map[string]string{"1": "unknown"}}
	call("POST", "/api/proxies/import", 1, payload, 409)
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM socks5_proxies`).Scan(&count); err != nil || count != 0 {
		t.Fatal("导入失败未回滚", err)
	}
	payload["bindings"] = map[string]string{"1": "legacy-uuid", "5": "legacy-uuid"}
	config := call("POST", "/api/proxies/import", 1, payload, 200)
	id := config["proxies"].([]any)[0].(map[string]any)["id"]
	if config["bindings"].(map[string]any)["1"] != id {
		t.Fatal("迁移未重建绑定")
	}
	if _, ok := config["bindings"].(map[string]any)["5"]; ok {
		t.Fatal("导入修改了已删除用户账号")
	}
	call("POST", "/api/proxies/import", 1, payload, 409)
	if err := db.QueryRow(`SELECT count(*) FROM socks5_proxies`).Scan(&count); err != nil || count != 1 {
		t.Fatal("重复导入改写配置", err)
	}
}

func TestSharedProxyRequiresLogin(t *testing.T) {
	db := walletTestDB(t)
	s := &Server{db: db, secret: []byte("shared-proxy-test")}
	for _, path := range []string{"/api/proxies", "/api/proxies/1"} {
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatal("未登录读取代理", w.Code)
		}
	}
	var p sharedProxy
	if err := json.Unmarshal([]byte(`{"name":"代理","host":"203.0.113.1","port":"1080","username":"","password":""}`), &p); err != nil || p.normalize() != nil {
		t.Fatal("表单字符串端口无法解析", err)
	}
}
