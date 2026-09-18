package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminAuditContextPreservesBusinessNumbers(t *testing.T) {
	db := walletTestDB(t)
	ctx := context.WithValue(context.Background(), requestAuditKey{}, &requestAuditContext{Page: "/admin/orders", Method: "POST", Resource: "/api/orders"})
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	const value int64 = 9007199254740993
	if err := recordEvent(ctx, tx, 1, 1, "order", "test", "audit-number-001", nil, map[string]int64{"id": value}); err != nil {
		t.Fatal(err)
	}
	var stored int64
	if err := tx.QueryRowContext(ctx, `SELECT (after_data->>'id')::bigint FROM operation_events WHERE request_key='audit-number-001'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != value {
		t.Fatalf("business number changed: got %d want %d", stored, value)
	}
}

func TestAdminActivityIdentityPrivacyAndReplay(t *testing.T) {
	db := walletTestDB(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash,role) VALUES(1,'member@test.local','','user'),(2,'audit@test.local','','admin')`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("audit-test")}
	call := func(body string, user int64, want int) {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/admin-activity", strings.NewReader(body))
		if user > 0 {
			r.Header.Set("Authorization", "Bearer "+s.token(user))
		}
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("got %d want %d: %s", w.Code, want, w.Body.String())
		}
	}
	body := `{"request_key":"activity-page-001","page":"/admin/accounts","kind":"page_view","control":"","result":"visited"}`
	call(body, 0, 401)
	call(body, 1, 204)
	call(body, 1, 204)
	call(strings.Replace(body, "/admin/accounts", "/admin/orders", 1), 1, 409)
	for _, invalid := range []string{
		strings.Replace(body, `"page":`, `"actor_id":2,"page":`, 1),
		strings.Replace(body, `"page":`, `"password":"secret","page":`, 1),
		strings.Replace(body, "/admin/accounts", "/admin/accounts?session=secret", 1),
		strings.Replace(body, "/admin/accounts", "/login", 1),
		strings.Replace(body, "page_view", "execute_anything", 1), body + `{}`,
	} {
		call(invalid, 1, 400)
	}
	call(`{"request_key":"activity-proxy-01","page":"/admin/proxies","kind":"local_request","control":"proxy_test","result":"failure"}`, 1, 204)
	var count int
	var actor int64
	var raw string
	if err := db.QueryRow(`SELECT count(*),min(actor_id),string_agg(after_data::text,'') FROM operation_events`).Scan(&count, &actor, &raw); err != nil {
		t.Fatal(err)
	}
	if count != 2 || actor != 1 || strings.Contains(raw, "secret") {
		t.Fatal("incorrect audit identity/replay/privacy", count, actor)
	}
	r := httptest.NewRequest("GET", "/api/audit", nil)
	r.Header.Set("Authorization", "Bearer "+s.token(1))
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("ordinary user can read all audit")
	}
	r = httptest.NewRequest("GET", "/api/audit?q=member%40test.local", nil)
	r.Header.Set("Authorization", "Bearer "+s.token(2))
	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	var out struct {
		Events []json.RawMessage `json:"events"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if w.Code != 200 || len(out.Events) != 2 {
		t.Fatal("actor search did not return activity", w.Body.String())
	}
}

func TestAdminRequestAuditDeniedSensitiveAndBusiness(t *testing.T) {
	db := walletTestDB(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash,role,permissions) VALUES(1,'restricted@test.local','','user',ARRAY['packages'])`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("audit-test")}
	call := func(method, path, page, body string, want int) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+s.token(1))
		r.Header.Set("X-Aitok-Page", page)
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	call("DELETE", "/api/accounts/123?token=never-log-this", "/admin/accounts", `{"session":"never-log-this"}`, 403)
	var payload string
	if err := db.QueryRow(`SELECT after_data::text FROM operation_events WHERE entity_type='admin_request'`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, "never-log-this") || !strings.Contains(payload, "403") || !strings.Contains(payload, "/admin/accounts") {
		t.Fatal("denied request not sanitized", payload)
	}
	db.Exec("UPDATE users SET role='admin' WHERE id=1")
	call("POST", "/api/packages", "/admin/packages", `{"name":"Test","plan":"plus","region":"PH","currency":"PHP","original_amount_minor":100,"sale_usd_minor":100,"months":1,"enabled":true}`, 200)
	var count int
	db.QueryRow(`SELECT count(*) FROM operation_events`).Scan(&count)
	if count != 2 {
		t.Fatal("business success audited twice", count)
	}
	db.QueryRow(`SELECT after_data::text FROM operation_events WHERE entity_type='package'`).Scan(&payload)
	if !strings.Contains(payload, `"page": "/admin/packages"`) || !strings.Contains(payload, `"source": "server"`) {
		t.Fatal("business event missing route context", payload)
	}
	call("GET", "/api/packages", "/admin/packages", "", 200)
	db.QueryRow(`SELECT count(*) FROM operation_events`).Scan(&count)
	if count != 2 {
		t.Fatal("background GET generated noise")
	}
	call("GET", "/api/bank-cards/1", "/admin/bank-cards", "", 403)
	db.QueryRow(`SELECT count(*) FROM operation_events`).Scan(&count)
	if count != 3 {
		t.Fatal("sensitive read denial missing")
	}
	if actual := safeAuditResource("/api/wallet/topups/token-secret/refund"); actual != "/api/wallet/topups/:id/refund" {
		t.Fatal(actual)
	}
}
