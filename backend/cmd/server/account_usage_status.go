package main

import (
	"net/http"
	"strings"
	"unicode/utf8"
)

// 使用状态仅作管理员标记，不联动续订、订单、资金或软删除。
func (s *Server) setAccountUsageStatus(w http.ResponseWriter, r *http.Request, user, id int64) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	var in struct {
		Status string  `json:"usage_status"`
		Reason *string `json:"discard_reason"`
	}
	if jsonBody(r, &in) != nil || (in.Status != "in_use" && in.Status != "discarded") || (in.Reason != nil && (!utf8.ValidString(*in.Reason) || strings.ContainsRune(*in.Reason, '\x00') || utf8.RuneCountInString(*in.Reason) > 2000)) {
		reply(w, map[string]string{"error": "请选择使用中或废弃，废弃理由最多 2000 字"}, 400)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	var previousStatus, previousReason string
	err = tx.QueryRowContext(r.Context(), `SELECT a.usage_status,a.discard_reason FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id AND u.deleted_at IS NULL WHERE a.id=$1 AND a.deleted_at IS NULL FOR UPDATE OF a`, id).Scan(&previousStatus, &previousReason)
	reason := previousReason
	if in.Reason != nil {
		reason = strings.TrimSpace(*in.Reason)
	}
	if err == nil && (previousStatus != in.Status || previousReason != reason) {
		_, err = tx.ExecContext(r.Context(), `UPDATE chatgpt_accounts SET usage_status=$2,discard_reason=$3,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, id, in.Status, reason)
		if err == nil {
			// 理由作为管理文本保存，审计仅记录状态和理由是否变化，不复制正文。
			err = recordEvent(r.Context(), tx, user, id, "account", "usage_status", eventKey(), map[string]any{"usage_status": previousStatus, "has_discard_reason": previousReason != ""}, map[string]any{"usage_status": in.Status, "has_discard_reason": reason != "", "reason_changed": previousReason != reason})
		}
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"usage_status": in.Status, "discard_reason": reason}, 200)
}
