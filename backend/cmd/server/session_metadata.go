package main

import (
	"context"
	"encoding/json"
	"time"
)

// 仅在服务端解密并复用浏览器启动的到期判断，不向列表返回 Session 或 Cookie。
func accountSessionExpiry(encrypted string) *time.Time {
	credentials, err := decodeAccountCredentials(encrypted)
	if err != nil {
		return nil
	}
	session, err := parseChatGPTSession(credentials.SessionJSON)
	if err != nil {
		return nil
	}
	return session.ExpiresAt
}

func accountJSONRows(ctx context.Context, db queryer, admin bool, query string, args ...any) ([]json.RawMessage, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		var encrypted string
		if err = rows.Scan(&raw, &encrypted); err != nil {
			return nil, err
		}
		if admin {
			var account map[string]json.RawMessage
			if err = json.Unmarshal(raw, &account); err != nil {
				return nil, err
			}
			account["session_expires_at"], err = json.Marshal(accountSessionExpiry(encrypted))
			if err != nil {
				return nil, err
			}
			raw, err = json.Marshal(account)
			if err != nil {
				return nil, err
			}
		}
		items = append(items, raw)
	}
	return items, rows.Err()
}
