package main

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestAuditAccountEmailSearch(t *testing.T) {
	db := walletTestDB(t)
	_, err := db.Exec(`
INSERT INTO users(id,email,password_hash,role) VALUES(1,'actor@test.local','','admin'),(2,'member@test.local','','user');
INSERT INTO chatgpt_accounts(id,user_id,label,email,deleted_at) VALUES
 (10,2,'目标账号','Target+tag@test.local',NULL),(20,2,'其他账号','other@test.local',NULL),
 (30,2,'已删除账号','deleted@test.local',NOW());
INSERT INTO recharge_orders(id,order_no,user_id,account_id,account_email,package_id,package_snapshot,period_start,period_end,sale_usd_minor,request_key)
 VALUES(100,'audit-order',2,10,'Target+tag@test.local',1,'{}','2030-01-01','2030-02-01',100,'audit-order-key');
INSERT INTO operation_events(id,actor_id,entity_type,entity_id,action,request_key,after_data,deleted_at) VALUES
 (1,1,'account',10,'subscription','audit-1','{}',NULL),
 (2,1,'order',100,'record','audit-2','{}',NULL),
 (3,1,'admin_request',0,'POST /api/accounts/10/browser-session','audit-3','{"resource":"/api/accounts/10/browser-session","status":403}',NULL),
 (4,1,'admin_request',0,'POST /api/orders/100/refund','audit-4','{"resource":"/api/orders/100/refund"}',NULL),
 (5,1,'admin_ui',1,'打开浏览器','audit-5','{"account_id":10,"source":"browser"}',NULL),
 (6,1,'admin_ui',1,'访问页面','audit-6','{"page":"/admin/accounts"}',NULL),
 (7,1,'account',20,'subscription','audit-7','{"owner_email":"Target+tag@test.local"}',NULL),
 (8,1,'account',30,'subscription','audit-8','{}',NULL),
 (9,1,'account',10,'deleted event','audit-9','{}',NOW()),
 (10,1,'order',999,'create','audit-10','{"account_email":"snapshot@test.local"}',NULL),
 (11,1,'admin_ui',1,'malformed','audit-11','{"account_id":"999999999999999999999999999999999"}',NULL),
 (12,1,'account',999,'create','audit-12','{"email":"historical@test.local"}',NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("audit-email-test")}
	type auditEvent struct {
		ID           int    `json:"id"`
		AccountEmail string `json:"account_email"`
	}
	search := func(email, query string, page, size, user, status int) (int, []auditEvent) {
		t.Helper()
		params := url.Values{"account_email": {email}, "q": {query}, "page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(size)}}
		r := httptest.NewRequest("GET", "/api/audit?"+params.Encode(), nil)
		r.Header.Set("Authorization", "Bearer "+s.token(int64(user)))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("status %d want %d: %s", w.Code, status, w.Body.String())
		}
		var out struct {
			Total  int          `json:"total"`
			Events []auditEvent `json:"events"`
		}
		if status == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
		}
		return out.Total, out.Events
	}
	total, rows := search("  TARGET+TAG@TEST.LOCAL  ", "", 2, 2, 1, 200)
	if total != 5 || len(rows) != 2 || rows[0].ID != 3 || rows[1].ID != 2 {
		t.Fatal("账号关联筛选或分页总数错误", total, rows)
	}
	for _, row := range rows {
		if row.AccountEmail != "Target+tag@test.local" {
			t.Fatal("缺少关联账号邮箱", row)
		}
	}
	for _, tc := range []struct {
		email, query string
		total        int
	}{
		{"target+", "record", 1}, {"", "target+tag", 5}, {"actor@test.local", "", 0},
		{"deleted@test.local", "", 0}, {"snapshot@test.local", "", 1}, {"historical@test.local", "", 1},
		{"%", "", 0}, {"_", "", 0}, {"' OR 1=1 --", "", 0},
	} {
		if total, _ := search(tc.email, tc.query, 1, 20, 1, 200); total != tc.total {
			t.Fatalf("email=%q query=%q: got %d want %d", tc.email, tc.query, total, tc.total)
		}
	}
	search(strings.Repeat("a", 201), "", 1, 20, 1, 400)
	search("target", "", 1, 20, 2, 403)
}

func TestAdminActivityAccountAssociation(t *testing.T) {
	db := walletTestDB(t)
	_, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'owner@test.local','','user'),(2,'other@test.local','','user');
INSERT INTO chatgpt_accounts(id,user_id,label,email,deleted_at) VALUES(10,1,'own','own@test.local',NULL),(20,2,'other','other@test.local',NULL),(30,1,'deleted','deleted@test.local',NOW());`)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("audit-association-test")}
	call := func(id int, kind string, status int) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"request_key": "account-audit-key", "page": "/admin/accounts", "kind": kind, "control": "open_browser", "result": "triggered", "account_id": id})
		r := httptest.NewRequest("POST", "/api/admin-activity", strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+s.token(1))
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("account %d: got %d want %d: %s", id, w.Code, status, w.Body.String())
		}
	}
	call(-1, "click", 400)
	call(20, "click", 403)
	call(30, "click", 403)
	call(999, "click", 403)
	call(10, "click", 204)
	call(10, "click", 204)
	call(0, "click", 409)
	var count, account int
	if err := db.QueryRow(`SELECT count(*),min((after_data->>'account_id')::int) FROM operation_events`).Scan(&count, &account); err != nil || count != 1 || account != 10 {
		t.Fatal("账号关联未保存或重试重复写入", count, account, err)
	}
}
