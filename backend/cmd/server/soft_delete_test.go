package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSoftDeleteLifecycle(t *testing.T) {
	db := walletTestDB(t)
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(1,'soft@test.local',''),(2,'other@test.local',''),(3,'__superadmin__','');INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,1,'Account','account@test.local')`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("soft-delete-test")}
	call := func(method, path string, user int64, body any, status int) map[string]any {
		t.Helper()
		encoded, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
		r.Header.Set("Authorization", "Bearer "+s.token(user))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		var result map[string]any
		json.Unmarshal(w.Body.Bytes(), &result)
		return result
	}
	assertDeleted := func(table string, id int64) {
		t.Helper()
		var created, updated time.Time
		var deleted sql.NullTime
		if err := db.QueryRow(`SELECT created_at,updated_at,deleted_at FROM `+table+` WHERE id=$1`, id).Scan(&created, &updated, &deleted); err != nil || !deleted.Valid || updated.Before(created) || updated.Before(deleted.Time) {
			t.Fatalf("%s 未保留删除记录或时间无效: %v", table, err)
		}
	}
	g := call("POST", "/api/account-groups", 1, map[string]any{"name": "Batch"}, 201)["group"].(map[string]any)
	groupID := int64(g["id"].(float64))
	groupPath := fmt.Sprintf("/api/account-groups/%d", groupID)
	call("PATCH", "/api/accounts/1/group", 1, map[string]any{"group_id": groupID}, 200)
	call("DELETE", groupPath, 2, nil, 404)
	call("DELETE", groupPath, 1, nil, 204)
	assertDeleted("account_groups", groupID)
	var bound sql.NullInt64
	if err := db.QueryRow(`SELECT group_id FROM chatgpt_accounts WHERE id=1`).Scan(&bound); err != nil || bound.Valid {
		t.Fatal("软删除分组未解除绑定", err)
	}
	call("PATCH", "/api/accounts/1/group", 1, map[string]any{"group_id": groupID}, 404)
	call("POST", "/api/account-groups", 1, map[string]any{"name": "Batch"}, 201)
	if len(call("GET", "/api/account-groups", 1, nil, 200)["groups"].([]any)) != 1 {
		t.Fatal("分组列表包含已删除数据")
	}
	call("DELETE", "/api/accounts/1", 1, nil, 204)
	assertDeleted("chatgpt_accounts", 1)
	call("DELETE", "/api/accounts/1", 1, nil, 404)
	call("POST", "/api/accounts/1/browser-session", 1, nil, 404)
	call("PATCH", "/api/accounts/1/session", 1, map[string]any{"session_json": "{}"}, 404)
	call("GET", "/api/accounts/1/browser", 3, nil, 404)
	call("POST", "/api/accounts/1/login", 1, map[string]any{"logged_in_at": time.Now().UTC()}, 404)
	call("PATCH", "/api/accounts/1/renewal-date", 3, map[string]any{"renewal_date": "2030-01-01"}, 404)
	if len(call("GET", "/api/accounts", 3, nil, 200)["accounts"].([]any)) != 0 {
		t.Fatal("账号列表包含已删除账号")
	}
	r := httptest.NewRequest("GET", "/api/browser-assistant", nil)
	r.Header.Set("Authorization", "Bearer "+s.assistantToken(1, 1))
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("已删除账号的助手凭据仍可用")
	}
	address := map[string]any{"full_name": "Test", "address_line1": "1 Road", "city": "Portland", "state": "OR", "postal_code": "97201", "country": "US"}
	a := call("POST", "/api/addresses", 1, address, 201)["address"].(map[string]any)
	aid := int64(a["id"].(float64))
	ap := fmt.Sprintf("/api/addresses/%d", aid)
	call("DELETE", ap, 1, nil, 204)
	assertDeleted("addresses", aid)
	call("GET", ap, 1, nil, 404)
	call("PATCH", ap, 1, address, 404)
	call("POST", "/api/addresses", 1, address, 201)
	if call("GET", "/api/addresses", 1, nil, 200)["total"] != float64(1) {
		t.Fatal("地址列表包含已删除数据")
	}
	card := map[string]any{"label": "Card", "number": "4242424242424242", "cardholder": "Test User", "exp_month": 12, "exp_year": 2035, "platform": "Archived platform"}
	c := call("POST", "/api/bank-cards", 1, card, 201)["card"].(map[string]any)
	cid := int64(c["id"].(float64))
	cp := fmt.Sprintf("/api/bank-cards/%d", cid)
	call("POST", cp+"/ledger", 1, map[string]any{"kind": "deposit", "amount_usd": "100.00", "request_key": "soft-delete-deposit-1"}, 201)
	call("DELETE", cp, 1, nil, 204)
	assertDeleted("bank_cards", cid)
	call("GET", cp, 1, nil, 404)
	if call("GET", cp+"/ledger", 1, nil, 200)["balance_usd_minor"] != float64(10000) {
		t.Fatal("删除卡片丢失历史账本")
	}
	call("POST", cp+"/ledger", 1, map[string]any{"kind": "deposit", "amount_usd": "100.00", "request_key": "soft-delete-deposit-2"}, 404)
	listing := call("GET", "/api/bank-cards", 1, nil, 200)
	if listing["total"] != float64(0) || len(listing["platforms"].([]any)) != 0 {
		t.Fatal("已删除卡或卡平台仍在列表中")
	}
	call("POST", "/api/bank-cards", 1, card, 201)
	// 删除用户后旧令牌失效；相同邮箱的新用户不会继承旧用户资产。
	if _, err := db.Exec(`UPDATE users SET deleted_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	assertDeleted("users", 1)
	for _, path := range []string{"/api/me", "/api/accounts", "/api/wallet", "/api/bank-cards", "/api/addresses"} {
		call("GET", path, 1, nil, 401)
	}
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(10,'soft@test.local','')`); err != nil {
		t.Fatal(err)
	}
	if call("GET", "/api/bank-cards", 10, nil, 200)["total"] != float64(0) {
		t.Fatal("重复邮箱错误继承原资产")
	}
}

func TestAllTablesMaintainTimestampsAndRejectHardDelete(t *testing.T) {
	db := walletTestDB(t)
	rows, err := db.Query(`SELECT relname FROM pg_class WHERE relnamespace=pg_my_temp_schema() AND relkind='r' ORDER BY relname`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	if len(tables) != 12 {
		t.Fatalf("应有12张表，实际%d", len(tables))
	}
	for _, table := range tables {
		var count int
		if err = db.QueryRow(`SELECT count(*) FROM pg_attribute WHERE attrelid=$1::regclass AND attname IN ('created_at','updated_at','deleted_at') AND atttypid='timestamptz'::regtype`, table).Scan(&count); err != nil || count != 3 {
			t.Fatalf("%s 缺少统一时间字段", table)
		}
		for _, query := range []string{`DELETE FROM ` + table, `TRUNCATE ` + table + ` CASCADE`} {
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			_, err = tx.Exec(query)
			tx.Rollback()
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "23514" {
				t.Fatalf("%s 未阻止物理删除: %v", table, err)
			}
		}
	}
	var created, updated time.Time
	if err = db.QueryRow(`INSERT INTO users(email,password_hash,created_at,updated_at) VALUES('timestamps@test.local','','2000-01-01','2000-01-01') RETURNING created_at,updated_at`).Scan(&created, &updated); err != nil {
		t.Fatal(err)
	}
	var afterCreated, afterUpdated time.Time
	if err = db.QueryRow(`UPDATE users SET password_hash='new',created_at=NOW() WHERE email='timestamps@test.local' RETURNING created_at,updated_at`).Scan(&afterCreated, &afterUpdated); err != nil {
		t.Fatal(err)
	}
	if !created.Equal(afterCreated) || !afterUpdated.After(updated) {
		t.Fatal("创建时间应保持不变，更新时间应自动推进")
	}
}

func TestConsumedEmailCodeIsSoftDeleted(t *testing.T) {
	db := walletTestDB(t)
	s := &Server{db: db}
	if _, err := db.Exec(`INSERT INTO email_codes(email,purpose,code,expires_at) VALUES('code@test.local','login',$1,NOW()+INTERVAL '1 hour')`, hash("123456")); err != nil {
		t.Fatal(err)
	}
	if !s.verifyCode("code@test.local", "login", "123456") || s.verifyCode("code@test.local", "login", "123456") {
		t.Fatal("验证码应只能消费一次")
	}
	var total, deleted int
	if err := db.QueryRow(`SELECT count(*),count(deleted_at) FROM email_codes`).Scan(&total, &deleted); err != nil || total != 1 || deleted != 1 {
		t.Fatal("验证码消费没有保留原记录", err)
	}
	if _, err := db.Exec(`INSERT INTO email_codes(email,purpose,code,expires_at) VALUES('code@test.local','login',$1,NOW()+INTERVAL '1 hour') ON CONFLICT(email,purpose) WHERE deleted_at IS NULL DO UPDATE SET code=EXCLUDED.code,expires_at=EXCLUDED.expires_at`, hash("654321")); err != nil {
		t.Fatal(err)
	}
	if !s.verifyCode("code@test.local", "login", "654321") {
		t.Fatal("软删除后的新验证码不可用")
	}
	if err := db.QueryRow(`SELECT count(*),count(deleted_at) FROM email_codes`).Scan(&total, &deleted); err != nil || total != 2 || deleted != 2 {
		t.Fatal("新验证码覆盖了历史记录", err)
	}
}
