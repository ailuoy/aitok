package main

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAddressValidation(t *testing.T) {
	a := Address{FullName: "  Test   User ", AddressLine1: " 100   Test Road ", City: "Portland", State: "OR", PostalCode: "97201", Country: "us"}
	if !a.normalize() || a.FullName != "Test User" || a.AddressLine1 != "100 Test Road" || a.Country != "US" {
		t.Fatal("地址未规范化")
	}
	a.FullName = strings.Repeat("名", 121)
	if a.normalize() {
		t.Fatal("超长姓名被接受")
	}
	a.FullName = ""
	if !a.normalize() {
		t.Fatal("旧地址应允许空姓名")
	}
	for _, country := range []string{"", "U", "USA", "12"} {
		a.Country = country
		if a.normalize() {
			t.Fatal("无效国家代码被接受")
		}
	}
	a.Country = "US"
	a.AddressLine1 = ""
	if a.normalize() {
		t.Fatal("空街道地址被接受")
	}
}

func TestAddressCRUDIntegration(t *testing.T) {
	db := walletTestDB(t)
	body, err := os.ReadFile("../../migrations/003_addresses.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(strings.ReplaceAll(string(body), "CREATE TABLE IF NOT EXISTS", "CREATE TEMP TABLE IF NOT EXISTS")); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(1,'owner@example.com',''),(3,'__superadmin__','')`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("address-test-secret")}
	call := func(method, path string, user int64, input any, status int) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		if user != 0 {
			r.Header.Set("Authorization", "Bearer "+s.token(user))
		}
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d，预期 %d；%s", method, path, w.Code, status, w.Body.String())
		}
		return w
	}
	input := map[string]string{"full_name": "Test User", "address_line1": "100 Test Road", "address_line2": "", "city": "Portland", "state": "OR", "postal_code": "97201", "country": "US", "source_url": "https://untrusted.example/"}
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		for _, path := range []string{"/api/addresses", "/api/addresses/1"} {
			call(method, path, 0, input, 401)
		}
	}
	w := call("POST", "/api/addresses", 3, input, 201)
	var created struct{ Address Address }
	json.Unmarshal(w.Body.Bytes(), &created)
	if created.Address.SourceURL != "" {
		t.Fatal("手动添加不能伪造采集来源")
	}
	if created.Address.FullName != "Test User" {
		t.Fatal("姓名未保存")
	}
	path := fmt.Sprintf("/api/addresses/%d", created.Address.ID)
	if _, err := db.Exec(`UPDATE addresses SET source_data=$1 WHERE id=$2`, `{"Full_Name":"Test User","Telephone":"555-0100","Extra_Field":"kept","CVV2":"123"}`, created.Address.ID); err != nil {
		t.Fatal(err)
	}
	call("GET", path, 1, nil, 403)
	call("PATCH", path, 1, input, 403)
	call("DELETE", path, 1, nil, 403)
	call("GET", "/api/addresses", 1, nil, 403)

	call("POST", "/api/addresses", 3, input, 409)
	input["address_line1"] = "  100  test road "
	call("POST", "/api/addresses", 3, input, 409)
	w = call("GET", "/api/addresses?q=Portland", 3, nil, 200)
	var list struct {
		Addresses []Address
		Total     int
	}
	json.Unmarshal(w.Body.Bytes(), &list)
	if list.Total != 1 || len(list.Addresses) != 1 {
		t.Fatal("地址搜索结果错误")
	}
	w = call("GET", "/api/addresses?q=test%20user", 3, nil, 200)
	json.Unmarshal(w.Body.Bytes(), &list)
	if list.Total != 1 || len(list.Addresses) != 1 || list.Addresses[0].FullName != "Test User" {
		t.Fatal("姓名搜索结果错误")
	}
	w = call("GET", "/api/addresses?q=%27%20OR%201%3D1--", 3, nil, 200)
	json.Unmarshal(w.Body.Bytes(), &list)
	if list.Total != 0 {
		t.Fatal("搜索必须按字面值查询")
	}
	call("GET", "/api/addresses?page=-1", 3, nil, 400)
	call("GET", "/api/addresses?page=2", 3, nil, 200)
	input["city"] = "Salem"
	input["full_name"] = "New Name"
	call("PATCH", path, 3, input, 200)
	w = call("GET", path, 3, nil, 200)
	if !strings.Contains(w.Body.String(), "Salem") || !strings.Contains(w.Body.String(), "New Name") {
		t.Fatal("编辑未保存")
	}
	var updated struct{ Address Address }
	json.Unmarshal(w.Body.Bytes(), &updated)
	var source map[string]string
	json.Unmarshal(updated.Address.SourceData, &source)
	if source["Extra_Field"] != "kept" || source["CVV2"] != "123" {
		t.Fatal("编辑地址不应丢失完整来源字段")
	}
	w = call("GET", "/api/addresses?q=555-0100", 3, nil, 200)
	json.Unmarshal(w.Body.Bytes(), &list)
	if list.Total != 1 {
		t.Fatal("来源电话搜索结果错误")
	}
	input["address_line1"] = ""
	call("PATCH", path, 3, input, 400)
	call("DELETE", path, 3, nil, 204)
	call("GET", path, 3, nil, 404)
	call("DELETE", path, 3, nil, 404)
	call("GET", "/api/addresses/invalid", 3, nil, 404)
	call("DELETE", "/api/addresses", 3, nil, 405)
}
