package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
)

const accountBillingAddressValue = `CASE WHEN b.id IS NULL THEN NULL ELSE jsonb_build_object('address_line1',b.address_line1,'address_line2',b.address_line2,'city',b.city,'state',b.state,'postal_code',b.postal_code,'country',b.country,'full_name',b.full_name) END`
const accountBillingAddressJSON = `'billing_address_id',a.billing_address_id,'billing_address_label',COALESCE(concat_ws(', ',NULLIF(b.address_line1,''),NULLIF(b.city,''),NULLIF(b.state,''),NULLIF(b.postal_code,'')),''),'billing_address',` + accountBillingAddressValue

func billingAddressSummary(address *Address) map[string]string {
	if address == nil {
		return nil
	}
	return map[string]string{"address_line1": address.AddressLine1, "address_line2": address.AddressLine2, "city": address.City, "state": address.State, "postal_code": address.PostalCode, "country": address.Country, "full_name": address.FullName}
}

func addressLabel(address *Address) string {
	if address == nil {
		return ""
	}
	parts := []string{}
	for _, part := range []string{address.AddressLine1, address.City, address.State, address.PostalCode} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, ", ")
}

func (s *Server) setAccountBillingAddress(w http.ResponseWriter, r *http.Request, user, id int64) {
	if r.Method != http.MethodPatch {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var input struct {
		AddressID json.RawMessage `json:"billing_address_id"`
		Random    bool            `json:"random"`
	}
	var addressID *int64
	if jsonBody(r, &input) != nil || (input.Random && len(input.AddressID) != 0) || (!input.Random && (len(input.AddressID) == 0 || json.Unmarshal(input.AddressID, &addressID) != nil || (addressID != nil && *addressID < 1))) {
		reply(w, map[string]string{"error": "请选择有效账单地址，解绑请传 null"}, 400)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	// 与删除地址采用相同的地址→账号锁顺序，避免绑定已删除地址。
	var address *Address
	if input.Random {
		var chosen int64
		err = tx.QueryRowContext(r.Context(), `SELECT id FROM addresses WHERE deleted_at IS NULL ORDER BY random() LIMIT 1 FOR SHARE`).Scan(&chosen)
		if err == sql.ErrNoRows {
			reply(w, map[string]string{"error": "暂无可绑定地址，请先添加地址"}, 409)
			return
		}
		if err != nil {
			operationError(w, err)
			return
		}
		addressID = &chosen
	}
	if addressID != nil {
		selected, readErr := scanAddress(tx.QueryRowContext(r.Context(), `SELECT `+addressColumns+` FROM addresses WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, *addressID))
		err = readErr
		address = &selected
		if err == sql.ErrNoRows {
			reply(w, map[string]string{"error": "地址不存在或已删除，请重新选择"}, 409)
			return
		}
		if err != nil {
			operationError(w, err)
			return
		}
	}
	var previous *int64
	err = tx.QueryRowContext(r.Context(), `SELECT a.billing_address_id FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id AND u.deleted_at IS NULL WHERE a.id=$1 AND a.deleted_at IS NULL FOR UPDATE OF a`, id).Scan(&previous)
	if err != nil {
		operationError(w, err)
		return
	}
	changed := (previous == nil) != (addressID == nil) || (previous != nil && addressID != nil && *previous != *addressID)
	if changed {
		_, err = tx.ExecContext(r.Context(), `UPDATE chatgpt_accounts SET billing_address_id=$2,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, id, addressID)
		if err == nil {
			err = recordEvent(r.Context(), tx, user, id, "account", "billing_address", eventKey(), map[string]any{"billing_address_id": previous}, map[string]any{"billing_address_id": addressID})
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"billing_address_id": addressID, "billing_address_label": addressLabel(address), "billing_address": billingAddressSummary(address)}, 200)
}
