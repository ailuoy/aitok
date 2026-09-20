package main

import (
	"fmt"
	"net/http"
	"time"
)

// 仅由已校验超级管理员身份的 users 路由调用，允许直接设置目标用户的新密码。
func (s *Server) changeUserPassword(w http.ResponseWriter, r *http.Request, actor, target int64) {
	if r.Method != http.MethodPatch {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var input struct {
		NewPassword     string `json:"new_password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if jsonBody(r, &input) != nil {
		reply(w, map[string]string{"error": "请求格式错误"}, 400)
		return
	}
	if len(input.NewPassword) < 8 || len(input.NewPassword) > 72 {
		reply(w, map[string]string{"error": "新密码需为 8 至 72 字节"}, 400)
		return
	}
	if input.NewPassword != input.ConfirmPassword {
		reply(w, map[string]string{"error": "两次输入的新密码不一致"}, 400)
		return
	}
	if !s.allowAttempt(r.Context(), fmt.Sprintf("user-password:%d:%d", actor, target), 10, 15*time.Minute) {
		w.Header().Set("Retry-After", "900")
		reply(w, map[string]string{"error": "尝试过于频繁，请稍后重试"}, 429)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	var locked int64
	err = tx.QueryRowContext(r.Context(), `SELECT id FROM users WHERE id=$1 AND email<>$2 AND deleted_at IS NULL FOR UPDATE`, target, adminIdentity).Scan(&locked)
	if err != nil {
		operationError(w, err)
		return
	}
	encoded, err := passwordHash(input.NewPassword)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE users SET password_hash=$2,session_version=session_version+1,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, target, encoded)
	}
	if err == nil {
		// 审计仅记录结果，不保存任何密码或密码哈希。
		err = recordEvent(r.Context(), tx, actor, target, "user", "password", eventKey(), map[string]any{}, map[string]any{"password_changed": true, "sessions_revoked": true})
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]string{"message": "密码已修改，该用户需要重新登录"}, 200)
}
