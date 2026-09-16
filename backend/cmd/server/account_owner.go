package main

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// POST 仅按完整注册邮箱查找，PATCH 在确认目标用户后变更账号归属。
// 管理权限由 accountAction 统一校验，不提供用户列表或模糊搜索入口。
func (s *Server) accountOwner(w http.ResponseWriter, r *http.Request, actor, accountID int64) {
	if r.Method != http.MethodPost && r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var input struct {
		Email           string `json:"email"`
		UserID          int64  `json:"user_id"`
		ExpectedOwnerID int64  `json:"expected_owner_id"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		reply(w, map[string]string{"error": "请求格式错误"}, 400)
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if len(input.Email) > 254 || !validEmail(input.Email) {
		reply(w, map[string]string{"error": "请输入完整的注册邮箱，必须精确匹配"}, 400)
		return
	}
	binding := r.Method == http.MethodPatch
	if binding && (input.UserID < 1 || input.ExpectedOwnerID < 1) {
		reply(w, map[string]string{"error": "请先按完整邮箱查找用户，再确认绑定"}, 400)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	userLock, accountLock := "", ""
	if binding {
		// 与新增账号保持用户→账号的锁顺序，串行校验同一用户下的重复邮箱。
		userLock, accountLock = " FOR UPDATE", " FOR UPDATE OF a"
	}
	var target struct {
		ID    int64  `json:"id"`
		Email string `json:"email"`
	}
	err = tx.QueryRowContext(r.Context(), `SELECT id,email FROM users WHERE email=$1 AND deleted_at IS NULL AND NOT disabled`+userLock, input.Email).Scan(&target.ID, &target.Email)
	if err == sql.ErrNoRows {
		reply(w, map[string]string{"error": "未找到该邮箱对应的可用注册用户，请核对完整邮箱"}, 404)
		return
	}
	if err != nil {
		operationError(w, err)
		return
	}
	var previousOwner int64
	var previousEmail, accountEmail string
	var groupID *int64
	err = tx.QueryRowContext(r.Context(), `SELECT a.user_id,u.email,a.email,a.group_id FROM chatgpt_accounts a JOIN users u ON u.id=a.user_id AND u.deleted_at IS NULL WHERE a.id=$1 AND a.deleted_at IS NULL`+accountLock, accountID).Scan(&previousOwner, &previousEmail, &accountEmail, &groupID)
	if err != nil {
		operationError(w, err)
		return
	}
	if !binding {
		reply(w, map[string]any{"user": target}, 200)
		return
	}
	// 邮箱被注销后重新注册时也不能绑定到未经本次确认的新身份。
	if target.ID != input.UserID {
		reply(w, map[string]string{"error": "目标用户已变更，请重新查找并确认"}, 409)
		return
	}
	if previousOwner != target.ID {
		if previousOwner != input.ExpectedOwnerID {
			reply(w, map[string]string{"error": "账号所属用户已变更，请刷新账号列表后重试"}, 409)
			return
		}
		var duplicate bool
		err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM chatgpt_accounts WHERE user_id=$1 AND lower(trim(email))=lower(trim($2)) AND id<>$3 AND deleted_at IS NULL)`, target.ID, accountEmail, accountID).Scan(&duplicate)
		if err != nil {
			operationError(w, err)
			return
		}
		if duplicate {
			reply(w, map[string]string{"error": "该用户已拥有相同邮箱的账号，请先核对原账号"}, 409)
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE chatgpt_accounts SET user_id=$2,group_id=NULL,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, accountID, target.ID)
		if err == nil {
			err = recordEvent(r.Context(), tx, actor, accountID, "account", "owner", eventKey(), map[string]any{"user_id": previousOwner, "owner_email": previousEmail, "group_id": groupID}, map[string]any{"user_id": target.ID, "owner_email": target.Email, "group_id": nil})
		}
		groupID = nil
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"user_id": target.ID, "owner_email": target.Email, "group_id": groupID}, 200)
}
