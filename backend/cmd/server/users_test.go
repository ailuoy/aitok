package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAdminEnvironmentPriority(t *testing.T) {
	t.Setenv("ADMIN_USERNAME", "new-admin")
	t.Setenv("ADMIN_PASSWORD", "new-password")
	t.Setenv("SUPER_ADMIN_USERNAME", "legacy-admin")
	t.Setenv("SUPER_ADMIN_PASSWORD", "legacy-password")
	if config := loadAdminConfig(); config.Username != "new-admin" || config.Password != "new-password" {
		t.Fatal("ADMIN 配置应优先于兼容配置")
	}
}

func TestUserRolesAndManagementPermissions(t *testing.T) {
	db := walletTestDB(t)
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32))))
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES (1,'owner@example.com','private-hash','user'),(2,'manager@example.com','','admin'),(3,'__superadmin__','',''),(4,'empty@example.com','',''),(5,'null@example.com','',NULL);
INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,1,'Owned','chat@example.com'),(2,4,'Other','other@example.com');
SELECT setval(pg_get_serial_sequence('users','id'),100);`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("roles-test-secret"), admin: adminConfig{"operator", "password"}}
	tokens := map[int64]string{}
	call := func(method, path string, user int64, input any, status int) map[string]any {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		if user > 0 {
			if tokens[user] == "" {
				tokens[user] = s.token(user)
			}
			r.Header.Set("Authorization", "Bearer "+tokens[user])
		}
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private-hash") || strings.Contains(w.Body.String(), "password_hash") {
			t.Fatal("接口泄露密码哈希")
		}
		out := map[string]any{}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	call("GET", "/api/users", 0, nil, 401)
	for _, user := range []int64{1, 2, 4, 5} {
		call("GET", "/api/users", user, nil, 403)
		call("PATCH", "/api/users/1/role", user, map[string]string{"role": "admin"}, 403)
	}
	for _, user := range []int64{1, 4, 5} {
		me := call("GET", "/api/me", user, nil, 200)["user"].(map[string]any)
		if me["role"] != "user" {
			t.Fatal("空角色必须视为用户")
		}
	}
	list := call("GET", "/api/users", 3, nil, 200)
	if list["total"] != float64(5) {
		t.Fatal("用户总数错误")
	}
	for _, item := range list["users"].([]any) {
		u := item.(map[string]any)
		if u["id"] == float64(3) && (u["role"] != "super_admin" || u["username"] != "operator") {
			t.Fatal("固定超管身份错误")
		}
	}
	call("PATCH", "/api/users/3/role", 3, map[string]string{"role": "user"}, 404)
	call("PATCH", "/api/users/999/role", 3, map[string]string{"role": "admin"}, 404)
	for _, role := range []string{"super_admin", "", "invalid"} {
		call("PATCH", "/api/users/1/role", 3, map[string]string{"role": role}, 400)
	}
	call("PATCH", "/api/users/1/role", 3, map[string]string{"role": "admin"}, 200)
	if call("GET", "/api/me", 1, nil, 200)["user"].(map[string]any)["role"] != "admin" {
		t.Fatal("角色修改未生效")
	}
	if len(call("GET", "/api/accounts", 1, nil, 200)["accounts"].([]any)) != 2 {
		t.Fatal("管理员未获得业务权限")
	}
	call("PATCH", "/api/users/1/role", 3, map[string]string{"role": "user"}, 200)
	if len(call("GET", "/api/accounts", 1, nil, 200)["accounts"].([]any)) != 1 {
		t.Fatal("降级后旧登录凭据仍保留管理权限")
	}
	call("PATCH", "/api/accounts/2/renewal-date", 1, map[string]any{"renewal_date": "2030-01-01"}, 403)
	call("PATCH", "/api/accounts/2/renewal-date", 2, map[string]any{"renewal_date": "2030-01-01"}, 200)
	created := call("POST", "/api/register", 0, map[string]any{"email": "new@example.com", "password": "test-password", "role": "admin"}, 201)
	if created["token"] == nil {
		t.Fatal("注册失败")
	}
	var registeredRole string
	if err := db.QueryRow(`SELECT role FROM users WHERE email='new@example.com'`).Scan(&registeredRole); err != nil || registeredRole != "user" {
		t.Fatal("注册接口允许角色提权")
	}
	if _, err := db.Exec(`INSERT INTO users(email,password_hash) SELECT 'page-'||n||'@example.com','' FROM generate_series(1,21) n`); err != nil {
		t.Fatal(err)
	}
	page := call("GET", "/api/users?q=page-&page=2", 3, nil, 200)
	if page["total"] != float64(21) || len(page["users"].([]any)) != 1 {
		t.Fatal("用户搜索分页错误")
	}
	call("GET", "/api/users?page=0", 3, nil, 400)
	card := map[string]any{"label": "Owned card", "cardholder": "Test User", "number": "4242424242424242", "exp_month": 12, "exp_year": time.Now().Year() + 1}
	id := call("POST", "/api/bank-cards", 1, card, 201)["card"].(map[string]any)["id"].(float64)
	path := fmt.Sprintf("/api/bank-cards/%.0f", id)
	var before string
	db.QueryRow(`SELECT number_fingerprint FROM bank_cards WHERE id=$1`, id).Scan(&before)
	call("GET", path, 4, nil, 404)
	call("GET", path, 2, nil, 200)
	card["notes"] = "管理员编辑"
	call("PATCH", path, 2, card, 200)
	var owner int64
	var after string
	if err := db.QueryRow(`SELECT user_id,number_fingerprint FROM bank_cards WHERE id=$1`, id).Scan(&owner, &after); err != nil || owner != 1 || before != after {
		t.Fatal("管理员编辑改变了银行卡归属或指纹")
	}
	if call("GET", "/api/bank-cards", 2, nil, 200)["total"] != float64(1) {
		t.Fatal("管理员看不到银行卡")
	}
	call("DELETE", path, 3, nil, 204)
	call("DELETE", "/api/accounts/2", 2, nil, 204)
}
