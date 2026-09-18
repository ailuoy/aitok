package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
)

// 产品选型仅记录账号当前套餐，不修改订单凭据、资金或开通日期。
func (s *Server) setAccountSubscriptionPackage(w http.ResponseWriter, r *http.Request, user, id int64) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	var in struct {
		PackageID json.RawMessage `json:"subscription_package_id"`
	}
	var packageID *int64
	if jsonBody(r, &in) != nil || len(in.PackageID) == 0 || json.Unmarshal(in.PackageID, &packageID) != nil || (packageID != nil && *packageID <= 0) {
		reply(w, map[string]string{"error": "请选择有效套餐，可选择未设置以清空"}, 400)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	if packageID != nil {
		// 下架套餐仍可记录为当前产品；与套餐删除互斥，防止绑定已删除套餐。
		var existing int64
		if err = tx.QueryRowContext(r.Context(), `SELECT id FROM recharge_packages WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, *packageID).Scan(&existing); err != nil {
			if err == sql.ErrNoRows {
				reply(w, map[string]string{"error": "套餐不存在或已删除，请刷新后重试"}, 400)
			} else {
				operationError(w, err)
			}
			return
		}
	}
	var previous *int64
	err = tx.QueryRowContext(r.Context(), `SELECT a.subscription_package_id FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id AND u.deleted_at IS NULL WHERE a.id=$1 AND a.deleted_at IS NULL FOR UPDATE OF a`, id).Scan(&previous)
	changed := (previous == nil) != (packageID == nil) || (previous != nil && packageID != nil && *previous != *packageID)
	if err == nil && changed {
		_, err = tx.ExecContext(r.Context(), `UPDATE chatgpt_accounts SET subscription_package_id=$2,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, id, packageID)
		if err == nil {
			err = recordEvent(r.Context(), tx, user, id, "account", "subscription_package", eventKey(), map[string]any{"subscription_package_id": previous}, map[string]any{"subscription_package_id": packageID})
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"subscription_package_id": packageID}, 200)
}
