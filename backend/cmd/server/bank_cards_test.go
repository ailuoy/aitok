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

func TestBankCardPlatformAndNotesValidation(t *testing.T) {
	valid := BankCard{Label: "Work", Cardholder: "Test User", Number: "4242424242424242", ExpMonth: 12, ExpYear: time.Now().Year() + 1}
	for _, tt := range []struct {
		name, platform, notes string
		valid                 bool
	}{
		{"optional", "", "", true},
		{"limits", strings.Repeat("台", 80), strings.Repeat("注", 1000), true},
		{"platform too long", strings.Repeat("台", 81), "", false},
		{"notes too long", "", strings.Repeat("注", 1001), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			card := valid
			card.Platform, card.Notes = tt.platform, tt.notes
			if card.normalize() != tt.valid {
				t.Fatal("平台或备注长度校验错误")
			}
		})
	}
	valid.Platform, valid.Notes = "  自定义   平台  ", "  第一行\n第二行  "
	if !valid.normalize() || valid.Platform != "自定义 平台" || valid.Notes != "第一行\n第二行" {
		t.Fatal("平台空格或备注换行处理错误")
	}
}

func TestBankCardsAndAssistantScope(t *testing.T) {
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32))))
	db := walletTestDB(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(1,'one@example.com',''),(2,'two@example.com',''),(3,'__superadmin__','');INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,1,'One','one@example.com'),(2,2,'Two','two@example.com');INSERT INTO addresses(address_line1,city,state,postal_code,country,user_id) VALUES('1 Shared Road','Portland','OR','97201','US',NULL),('2 Private Road','Portland','OR','97201','US',2)`); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("card-test")}
	call := func(method, path, token string, input any, status int) map[string]any {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.routes().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		var out map[string]any
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	token := s.token(1)
	other := s.token(2)
	input := map[string]any{"label": "Work", "cardholder": "Test User", "number": "4242 4242 4242 4242", "exp_month": 12, "exp_year": 2035, "cvc": "123", "platform": "  自定义   平台 ", "notes": " 月度订阅\n仅工作用途 "}
	call("POST", "/api/bank-cards", "", input, 401)
	created := call("POST", "/api/bank-cards", token, input, 201)["card"].(map[string]any)
	if _, ok := created["number"]; ok {
		t.Fatal("写入响应泄露卡号")
	}
	if created["platform"] != "自定义 平台" || created["notes"] != "月度订阅\n仅工作用途" {
		t.Fatal("卡平台或备注未正确保存")
	}
	id := int64(created["id"].(float64))
	path := fmt.Sprintf("/api/bank-cards/%d", id)
	call("POST", "/api/bank-cards", token, input, 409)
	var ciphertext string
	db.QueryRow(`SELECT number_ciphertext FROM bank_cards WHERE id=$1`, id).Scan(&ciphertext)
	if strings.Contains(ciphertext, "4242424242424242") {
		t.Fatal("卡号没有加密")
	}
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		call(method, path, other, input, 404)
		call(method, path, s.token(3), input, 404)
	}
	list := call("GET", "/api/bank-cards?q=4242", token, nil, 200)
	if list["total"] != float64(1) || strings.Contains(fmt.Sprint(list), "4242424242424242") {
		t.Fatal("银行卡列表不正确")
	}
	for _, query := range []string{"自定义", "仅工作用途"} {
		if call("GET", "/api/bank-cards?q="+query, token, nil, 200)["total"] != float64(1) {
			t.Fatal("搜索没有包含平台或备注")
		}
	}
	otherInput := map[string]any{"label": "Other", "cardholder": "Other User", "number": "4242424242424242", "exp_month": 12, "exp_year": 2035, "platform": "Other platform"}
	call("POST", "/api/bank-cards", other, otherInput, 201)
	options := call("GET", "/api/bank-cards?q=unmatched&page=2", token, nil, 200)["platforms"].([]any)
	if len(options) != 1 || options[0] != "自定义 平台" {
		t.Fatal("平台选项受到搜索分页影响或泄露其他用户数据")
	}
	input["number"] = "4242424242424241"
	call("PATCH", path, token, input, 400)
	input["number"] = "4242424242424242"
	input["label"] = "Updated"
	input["platform"], input["notes"] = "新平台", "已修改备注"
	call("PATCH", path, token, input, 200)
	full := call("GET", path, token, nil, 200)["card"].(map[string]any)
	if full["number"] != "4242424242424242" || full["label"] != "Updated" || full["platform"] != "新平台" || full["notes"] != "已修改备注" {
		t.Fatal("卡片编辑失败")
	}
	limited := s.assistantToken(1, 1)
	call("GET", "/api/bank-cards", limited, nil, 401)
	call("GET", "/api/browser-assistant", token, nil, 401)
	call("POST", "/api/browser-assistant", limited, nil, 405)
	call("GET", "/api/browser-assistant", s.assistantToken(1, 2), nil, 401)
	snapshot := call("GET", "/api/browser-assistant", limited, nil, 200)
	if len(snapshot["cards"].([]any)) != 1 || len(snapshot["addresses"].([]any)) != 1 || strings.Contains(fmt.Sprint(snapshot), "4242424242424242") {
		t.Fatal("助手数据范围或脱敏错误")
	}
	if snapshot["cards"].([]any)[0].(map[string]any)["platform"] != "新平台" {
		t.Fatal("助手缺少卡平台")
	}
	call("GET", fmt.Sprintf("/api/browser-assistant/cards/%d", id), s.assistantToken(2, 2), nil, 404)
	call("GET", fmt.Sprintf("/api/browser-assistant/cards/%d", id), limited, nil, 200)
	call("DELETE", "/api/addresses/1", token, nil, 404)
	call("GET", "/api/addresses/1", token, nil, 200)
	call("GET", "/api/addresses/2", token, nil, 404)
	input["platform"], input["notes"] = "", ""
	cleared := call("PATCH", path, token, input, 200)["card"].(map[string]any)
	if cleared["platform"] != "" || cleared["notes"] != "" || len(call("GET", "/api/bank-cards", token, nil, 200)["platforms"].([]any)) != 0 {
		t.Fatal("卡平台或备注清空失败")
	}
	call("DELETE", path, token, nil, 204)
	call("GET", path, token, nil, 404)
}
