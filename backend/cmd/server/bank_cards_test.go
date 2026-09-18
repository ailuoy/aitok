package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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
	for _, size := range []int{0, 200, 201} {
		card := valid
		card.WalletAddress = strings.Repeat("a", size)
		if card.normalize() != (size <= 200) {
			t.Fatal("钱包地址长度校验错误", size)
		}
	}
}

func TestBankCardsAndAssistantScope(t *testing.T) {
	t.Setenv("SESSION_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32))))
	db := walletTestDB(t)
	if _, err := db.Exec(`INSERT INTO users(id,email,password_hash) VALUES(1,'one@example.com',''),(2,'two@example.com',''),(3,'__superadmin__','');INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(1,1,'One','one@example.com'),(2,2,'Two','two@example.com');INSERT INTO addresses(address_line1,city,state,postal_code,country,user_id) VALUES('1 Shared Road','Portland','OR','97201','US',NULL),('2 Private Road','Portland','OR','97201','US',2)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE users SET role='admin' WHERE id=1"); err != nil {
		t.Fatal(err)
	}
	s := &Server{db: db, secret: []byte("card-test")}
	call := func(method, path, token string, input any, status int) map[string]any {
		t.Helper()
		body, _ := json.Marshal(input)
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+token)
		if auditCardDetails.MatchString(path) {
			user, err := s.auth(r)
			if err == nil && status != 403 {
				if method == "GET" {
					r.Header.Set("X-Aitok-TOTP", prepareCardEditTOTP(t, s, user))
				} else if method == "PATCH" {
					var cardID int64
					fmt.Sscanf(path, "/api/bank-cards/%d", &cardID)
					input.(map[string]any)["edit_token"] = cardEditTestToken(t, s, user, cardID, time.Now().Add(time.Minute))
					body, _ = json.Marshal(input)
					r.Body = io.NopCloser(strings.NewReader(string(body)))
				}
			}
		}
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
	walletAddress := "0x" + strings.Repeat("aB", 20)
	input := map[string]any{"label": "Work", "cardholder": "Test User", "number": "4242 4242 4242 4242", "exp_month": 12, "exp_year": 2035, "cvc": "123", "platform": "  自定义   平台 ", "notes": " 月度订阅\n仅工作用途 ", "wallet_address": "  " + walletAddress + "  "}
	call("POST", "/api/bank-cards", "", input, 401)
	created := call("POST", "/api/bank-cards", token, input, 201)["card"].(map[string]any)
	if _, ok := created["number"]; ok {
		t.Fatal("写入响应泄露卡号")
	}
	if created["platform"] != "自定义 平台" || created["notes"] != "月度订阅\n仅工作用途" || created["wallet_address"] != walletAddress {
		t.Fatal("卡平台、备注或钱包地址未正确保存")
	}
	id := int64(created["id"].(float64))
	path := fmt.Sprintf("/api/bank-cards/%d", id)
	call("POST", "/api/bank-cards", token, input, 409)
	var ciphertext string
	db.QueryRow(`SELECT number_ciphertext FROM bank_cards WHERE id=$1`, id).Scan(&ciphertext)
	if strings.Contains(ciphertext, "4242424242424242") {
		t.Fatal("卡号没有加密")
	}
	if created["has_cvc"] != true || created["cvc"] != nil {
		t.Fatal("写入响应只返回安全码存在状态")
	}
	var cvcCipher string
	if err := db.QueryRow(`SELECT cvc_ciphertext FROM bank_cards WHERE id=$1`, id).Scan(&cvcCipher); err != nil {
		t.Fatal(err)
	}
	if plain, err := decryptSession(cvcCipher); err != nil || plain != "123" || cvcCipher == "123" {
		t.Fatal("安全码未加密保存")
	}
	detailed := call("GET", path, token, nil, 200)["card"].(map[string]any)
	if detailed["cvc"] != nil || detailed["number"] != "************4242" {
		t.Fatal("编辑详情泄露卡片敏感信息")
	}
	for _, viewer := range []string{token, s.token(3)} {
		for _, query := range []string{"", "?include_numbers=1"} {
			visible := call("GET", "/api/bank-cards"+query, viewer, nil, 200)["cards"].([]any)[0].(map[string]any)
			if visible["number"] != nil || visible["last4"] != "4242" || visible["cvc"] != nil || visible["has_cvc"] != true {
				t.Fatal("管理员和超管列表只允许返回尾号及安全码存在状态")
			}
		}
	}
	for _, invalid := range []string{"12", "12345", "1a3", "１２３"} {
		input["cvc"] = invalid
		call("PATCH", path, token, input, 400)
	}
	input["cvc"] = "0042"
	var evidence evidenceDocument
	if err := json.Unmarshal([]byte(testRichEvidence(t)), &evidence); err != nil {
		t.Fatal(err)
	}
	var qr string
	for _, block := range evidence.Blocks {
		if block.Type == "image" {
			qr = block.Src
			break
		}
	}
	if qr == "" {
		t.Fatal("测试二维码图片为空")
	}
	input["wallet_qr_image"] = qr
	call("PATCH", path, token, input, 200)
	detailed = call("GET", path, token, nil, 200)["card"].(map[string]any)
	if detailed["cvc"] != nil || detailed["has_cvc"] != true || detailed["wallet_qr_image"] != qr {
		t.Fatal("CVC 或二维码未保存")
	}
	for _, invalid := range []string{"data:image/svg+xml;base64,PHN2Zz4=", "data:image/png;base64,YmFk", strings.Repeat("x", 2800001)} {
		input["wallet_qr_image"] = invalid
		call("PATCH", path, token, input, 400)
	}
	delete(input, "wallet_qr_image")
	delete(input, "cvc")
	call("PATCH", path, token, input, 200)
	detailed = call("GET", path, token, nil, 200)["card"].(map[string]any)
	if detailed["cvc"] != nil || detailed["has_cvc"] != true || detailed["wallet_qr_image"] != qr {
		t.Fatal("旧客户端省略字段不能清空已有值")
	}
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		call(method, path, other, input, 403)
	}
	call("GET", path, s.token(3), input, 200)
	list := call("GET", "/api/bank-cards?q=4242", token, nil, 200)
	if list["total"] != float64(1) || strings.Contains(fmt.Sprint(list), "4242424242424242") {
		t.Fatal("银行卡列表不正确")
	}
	if list["cards"].([]any)[0].(map[string]any)["wallet_address"] != walletAddress {
		t.Fatal("列表缺少完整钱包地址")
	}
	for _, query := range []string{"自定义", "仅工作用途", walletAddress} {
		if call("GET", "/api/bank-cards?q="+query, token, nil, 200)["total"] != float64(1) {
			t.Fatal("搜索没有包含平台或备注")
		}
	}
	otherInput := map[string]any{"label": "Other", "cardholder": "Other User", "number": "4242424242424242", "exp_month": 12, "exp_year": 2035, "platform": "Other platform"}
	call("POST", "/api/bank-cards", other, otherInput, 403)
	db.Exec("UPDATE users SET role='admin' WHERE id=2")
	call("POST", "/api/bank-cards", other, otherInput, 201)
	options := call("GET", "/api/bank-cards?q=unmatched&page=2", token, nil, 200)["platforms"].([]any)
	if len(options) != 2 {
		t.Fatal("管理员平台选项应覆盖所有用户且不受搜索分页影响")
	}
	input["number"] = "4242424242424241"
	call("PATCH", path, token, input, 400)
	input["number"] = "4242424242424242"
	input["wallet_address"] = strings.Repeat("a", 201)
	call("PATCH", path, token, input, 400)
	if call("GET", path, token, nil, 200)["card"].(map[string]any)["wallet_address"] != walletAddress {
		t.Fatal("无效钱包地址不能覆盖原地址")
	}
	input["wallet_address"] = "0x" + strings.Repeat("12", 20)
	input["label"] = "Updated"
	input["platform"], input["notes"] = "新平台", "已修改备注"
	call("PATCH", path, token, input, 200)
	full := call("GET", path, token, nil, 200)["card"].(map[string]any)
	if full["number"] != "************4242" || full["label"] != "Updated" || full["platform"] != "新平台" || full["notes"] != "已修改备注" || full["wallet_address"] != input["wallet_address"] {
		t.Fatal("卡片编辑失败")
	}
	limited := s.assistantToken(1, 1)
	call("GET", "/api/bank-cards", limited, nil, 401)
	call("GET", "/api/browser-assistant", token, nil, 401)
	call("POST", "/api/browser-assistant", limited, nil, 405)
	call("GET", "/api/browser-assistant", s.assistantToken(1, 999), nil, 401)
	snapshot := call("GET", "/api/browser-assistant", limited, nil, 200)
	if len(snapshot["cards"].([]any)) != 2 || len(snapshot["addresses"].([]any)) != 2 || strings.Contains(fmt.Sprint(snapshot), "4242424242424242") {
		t.Fatal("助手数据范围或脱敏错误")
	}
	if snapshot["cards"].([]any)[1].(map[string]any)["platform"] != "新平台" {
		t.Fatal("助手缺少卡平台")
	}
	if _, err := db.Exec(`UPDATE chatgpt_accounts SET billing_address_id=1,updated_at=NOW() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	boundAddress := call("GET", "/api/browser-assistant", limited, nil, 200)
	if boundAddress["billing_address_id"] != float64(1) || len(boundAddress["addresses"].([]any)) != 1 || boundAddress["addresses"].([]any)[0].(map[string]any)["id"] != float64(1) {
		t.Fatal("助手未优先展示当前账号绑定地址")
	}
	otherAddress := call("GET", "/api/browser-assistant?account_id=1", s.assistantToken(2, 2), nil, 200)
	if otherAddress["billing_address_id"] != nil || len(otherAddress["addresses"].([]any)) != 1 || otherAddress["addresses"].([]any)[0].(map[string]any)["id"] == float64(1) {
		t.Fatal("助手串用其他账号地址")
	}
	if snapshot["payment_card_id"] != nil {
		t.Fatal("未绑定付款卡时应返回空值")
	}
	if _, err := db.Exec(`UPDATE chatgpt_accounts SET payment_card_id=$1,updated_at=NOW() WHERE id=1`, id); err != nil {
		t.Fatal(err)
	}
	bound := call("GET", "/api/browser-assistant", limited, nil, 200)
	if bound["payment_card_id"] != float64(id) || len(bound["cards"].([]any)) != 1 || bound["cards"].([]any)[0].(map[string]any)["id"] != float64(id) {
		t.Fatal("助手应返回当前账号绑定的付款卡并优先列出")
	}
	for _, candidate := range snapshot["cards"].([]any) {
		otherID := int64(candidate.(map[string]any)["id"].(float64))
		if otherID != id {
			call("GET", fmt.Sprintf("/api/browser-assistant/cards/%d", otherID), limited, nil, 409)
		}
	}
	if call("GET", "/api/browser-assistant", s.assistantToken(2, 2), nil, 200)["payment_card_id"] != nil {
		t.Fatal("助手不能读取其他账号的付款卡绑定")
	}
	if _, err := db.Exec(`UPDATE bank_cards SET status='frozen',updated_at=NOW() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	unavailable := call("GET", "/api/browser-assistant", limited, nil, 200)
	if unavailable["payment_card_id"] != float64(id) || len(unavailable["cards"].([]any)) != 0 {
		t.Fatal("绑定卡不可用时应保留绑定编号，但不能进入可用卡列表")
	}
	if _, err := db.Exec(`UPDATE bank_cards SET status='active',updated_at=NOW() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	call("GET", fmt.Sprintf("/api/browser-assistant/cards/%d", id), s.assistantToken(2, 2), nil, 200)
	if assistantCard := call("GET", fmt.Sprintf("/api/browser-assistant/cards/%d", id), limited, nil, 200)["card"].(map[string]any); assistantCard["cvc"] != "0042" {
		t.Fatal("助手详情未返回已保存安全码")
	}
	call("DELETE", "/api/addresses/999", token, nil, 404)
	call("GET", "/api/addresses/1", token, nil, 200)
	call("GET", "/api/addresses/2", token, nil, 200)
	input["platform"], input["notes"] = "", ""
	input["wallet_address"] = ""
	input["cvc"], input["wallet_qr_image"] = "", ""
	cleared := call("PATCH", path, token, input, 200)["card"].(map[string]any)
	if cleared["has_cvc"] != true || cleared["wallet_qr_image"] != "" || cleared["platform"] != "" || cleared["notes"] != "" || cleared["wallet_address"] != "" || len(call("GET", "/api/bank-cards", token, nil, 200)["platforms"].([]any)) != 1 {
		t.Fatal("卡平台或备注清空失败")
	}
	call("DELETE", path, token, nil, 204)
	archived := call("GET", "/api/bank-cards?archived=1&include_numbers=1", token, nil, 200)["cards"].([]any)[0].(map[string]any)
	if archived["number"] != nil || archived["cvc"] != nil || archived["last4"] != "4242" {
		t.Fatal("归档列表泄露完整卡号或安全码")
	}
	call("GET", path, token, nil, 404)
}
