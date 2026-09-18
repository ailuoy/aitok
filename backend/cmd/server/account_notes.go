package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"
)

// 备注仅供管理员使用，不进入操作审计的正文快照。
func (s *Server) accountNotes(w http.ResponseWriter, r *http.Request, accountID int64) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	var input struct {
		Notes *string `json:"notes"`
	}
	if jsonBody(r, &input) != nil || input.Notes == nil || !utf8.ValidString(*input.Notes) || strings.ContainsRune(*input.Notes, '\x00') || utf8.RuneCountInString(*input.Notes) > 20000 {
		reply(w, map[string]string{"error": "请输入最多 20000 字的文本备注，可留空"}, 400)
		return
	}
	var notes string
	err := s.db.QueryRowContext(r.Context(), `UPDATE chatgpt_accounts a SET notes=$1,updated_at=NOW() WHERE a.id=$2 AND a.deleted_at IS NULL AND EXISTS(SELECT 1 FROM users u WHERE u.id=a.user_id AND u.deleted_at IS NULL) RETURNING notes`, *input.Notes, accountID).Scan(&notes)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]any{"id": accountID, "notes": notes}, 200)
}
