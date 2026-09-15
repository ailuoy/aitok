package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSoftDeleteLifecycle(t *testing.T) {
	db := walletTestDB(t)
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(1,'soft@test.local',''),(2,'other@test.local',''),(3,'__superadmin__','');INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,1,'Account','account@test.local')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE users SET role='admin' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("soft-delete-test")}
	call := func(method, path string, user int64, body any, status int) map[string]any {
		t.Helper()
		encoded, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
		r.Header.Set("Authorization", "Bearer "+s.token(user))
		if method == "POST" && (strings.HasSuffix(path, "/browser") || strings.HasSuffix(path, "/browser-session")) && s.permitted(r.Context(), user, "accounts") {
			r.Header.Set("X-Aitok-TOTP", browserTestOTP(t, s, user))
		}
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
	checkGroup := expectTimestampUpdate(t, db, "account_groups", "id=$1", groupID)
	call("PATCH", groupPath, 1, map[string]any{"name": "Renamed"}, 200)
	checkGroup()
	checkAccount := expectTimestampUpdate(t, db, "chatgpt_accounts", "id=1")
	call("PATCH", "/api/accounts/1/group", 1, map[string]any{"group_id": groupID}, 200)
	checkAccount()
	call("DELETE", groupPath, 2, nil, 403)
	checkGroup = expectTimestampUpdate(t, db, "account_groups", "id=$1", groupID)
	checkAccount = expectTimestampUpdate(t, db, "chatgpt_accounts", "id=1")
	call("DELETE", groupPath, 1, nil, 204)
	checkGroup()
	checkAccount()
	assertDeleted("account_groups", groupID)
	var bound sql.NullInt64
	if err := db.QueryRow(`SELECT group_id FROM chatgpt_accounts WHERE id=1`).Scan(&bound); err != nil || bound.Valid {
		t.Fatal("软删除分组未解除绑定", err)
	}
	call("PATCH", "/api/accounts/1/group", 1, map[string]any{"group_id": groupID}, 404)
	call("POST", "/api/account-groups", 1, map[string]any{"name": "Renamed"}, 201)
	if len(call("GET", "/api/account-groups", 1, nil, 200)["groups"].([]any)) != 1 {
		t.Fatal("分组列表包含已删除数据")
	}
	checkAccount = expectTimestampUpdate(t, db, "chatgpt_accounts", "id=1")
	call("DELETE", "/api/accounts/1", 1, nil, 204)
	checkAccount()
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
	checkAddress := expectTimestampUpdate(t, db, "addresses", "id=$1", aid)
	call("DELETE", ap, 1, nil, 204)
	checkAddress()
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
	checkCard := expectTimestampUpdate(t, db, "bank_cards", "id=$1", cid)
	call("DELETE", cp, 1, nil, 204)
	checkCard()
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
	if _, err := db.Exec(`UPDATE users SET deleted_at=NOW(),updated_at=NOW() WHERE id=1 AND deleted_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	assertDeleted("users", 1)
	for _, path := range []string{"/api/me", "/api/accounts", "/api/wallet", "/api/bank-cards", "/api/addresses"} {
		call("GET", path, 1, nil, 401)
	}
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(10,'soft@test.local','')`); err != nil {
		t.Fatal(err)
	}
	call("GET", "/api/bank-cards", 10, nil, 403)
	var inherited int
	db.QueryRow("SELECT count(*) FROM bank_cards WHERE user_id=10").Scan(&inherited)
	if inherited != 0 {
		t.Fatal("新用户继承了旧用户卡片")
	}
}

func TestAllTablesHaveTimestampsWithoutDatabaseLogic(t *testing.T) {
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
	if len(tables) != 21 {
		t.Fatalf("应有21张表，实际%d", len(tables))
	}
	for _, table := range tables {
		var count int
		if err = db.QueryRow(`SELECT count(*) FROM pg_attribute WHERE attrelid=$1::regclass AND attname IN ('created_at','updated_at','deleted_at') AND atttypid='timestamptz'::regtype`, table).Scan(&count); err != nil || count != 3 {
			t.Fatalf("%s 缺少统一时间字段", table)
		}
	}
	var triggers int
	if err = db.QueryRow(`SELECT count(*) FROM pg_trigger tr JOIN pg_proc p ON p.oid=tr.tgfoid JOIN pg_class c ON c.oid=tr.tgrelid WHERE c.relnamespace=pg_my_temp_schema() AND NOT tr.tgisinternal`).Scan(&triggers); err != nil || triggers != 0 {
		t.Fatal("时间维护不能依赖数据库触发器", err)
	}
}

func TestConsumedEmailCodeIsSoftDeleted(t *testing.T) {
	db := walletTestDB(t)
	s := &Server{db: db}
	if err := s.storeEmailCode(context.Background(), "code@test.local", "login", hash("123456"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	check := expectTimestampUpdate(t, db, "email_codes", "email='code@test.local' AND deleted_at IS NULL")
	if !s.verifyCode("code@test.local", "login", "123456") || s.verifyCode("code@test.local", "login", "123456") {
		t.Fatal("验证码应只能消费一次")
	}
	check()
	var total, deleted int
	if err := db.QueryRow(`SELECT count(*),count(deleted_at) FROM email_codes`).Scan(&total, &deleted); err != nil || total != 1 || deleted != 1 {
		t.Fatal("验证码消费没有保留原记录", err)
	}
	if err := s.storeEmailCode(context.Background(), "code@test.local", "login", hash("654321"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if !s.verifyCode("code@test.local", "login", "654321") {
		t.Fatal("软删除后的新验证码不可用")
	}
	if err := db.QueryRow(`SELECT count(*),count(deleted_at) FROM email_codes`).Scan(&total, &deleted); err != nil || total != 2 || deleted != 2 {
		t.Fatal("新验证码覆盖了历史记录", err)
	}
}

// 仅操作 walletTestDB 创建的临时表；用固定旧时间避免依赖 sleep 和时钟精度。
func expectTimestampUpdate(t *testing.T, db *sql.DB, table, predicate string, args ...any) func() {
	t.Helper()
	var identity string
	if err := db.QueryRow(`UPDATE `+table+` SET created_at='2000-01-01',updated_at='2000-01-01' WHERE `+predicate+` RETURNING created_at::text`, args...).Scan(&identity); err != nil {
		t.Fatal(err)
	}
	// 不通过 deleted_at 定位，软删除后也检查原行。
	predicate = strings.ReplaceAll(predicate, " AND deleted_at IS NULL", "")
	return func() {
		t.Helper()
		var created string
		var updated bool
		if err := db.QueryRow(`SELECT created_at::text,updated_at>created_at FROM `+table+` WHERE `+predicate, args...).Scan(&created, &updated); err != nil || created != identity || !updated {
			t.Fatalf("%s 更新必须保留创建时间并推进更新时间: %v", table, err)
		}
	}
}

func TestReissuedEmailCodePreservesHistory(t *testing.T) {
	db := walletTestDB(t)
	s := &Server{db: db}
	ctx := context.Background()
	if err := s.storeEmailCode(ctx, "code@test.local", "login", hash("123456"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// 模拟新码插入失败，事务必须恢复旧码的活跃状态。
	if _, err := db.Exec(`ALTER TABLE email_codes ADD CONSTRAINT test_code_hash CHECK (code <> 'rejected')`); err != nil {
		t.Fatal(err)
	}
	if err := s.storeEmailCode(ctx, "code@test.local", "login", "rejected", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("新验证码插入应失败")
	}
	var stillActive bool
	if err := db.QueryRow(`SELECT deleted_at IS NULL FROM email_codes WHERE id=1`).Scan(&stillActive); err != nil || !stillActive {
		t.Fatal("保存失败不应使旧验证码失效", err)
	}
	check := expectTimestampUpdate(t, db, "email_codes", "id=1")
	if err := s.storeEmailCode(ctx, "code@test.local", "login", hash("654321"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	check()
	var original string
	var deleted, active int
	if err := db.QueryRow(`SELECT code FROM email_codes WHERE id=1 AND deleted_at IS NOT NULL`).Scan(&original); err != nil || original != hash("123456") {
		t.Fatal("重发验证码覆盖了原记录", err)
	}
	if err := db.QueryRow(`SELECT count(*) FILTER (WHERE deleted_at IS NOT NULL),count(*) FILTER (WHERE deleted_at IS NULL) FROM email_codes`).Scan(&deleted, &active); err != nil || deleted != 1 || active != 1 {
		t.Fatal("重发后应保留一个失效记录和一个活跃记录", err)
	}
	if s.verifyCode("code@test.local", "login", "123456") || !s.verifyCode("code@test.local", "login", "654321") {
		t.Fatal("重发后只能消费新验证码")
	}
}
