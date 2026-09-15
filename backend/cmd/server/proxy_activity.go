package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) proxyActivity(w http.ResponseWriter, r *http.Request) {
	user, err := s.auth(r)
	if err != nil {
		w.WriteHeader(401)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == "GET" {
		page, size, valid := pageParameters(r)
		if !valid {
			w.WriteHeader(400)
			return
		}
		admin := s.permitted(r.Context(), user, "audit")
		const filter = ` FROM proxy_activity p WHERE deleted_at IS NULL AND (user_id=$1 OR $2)`
		var total int
		if err := s.db.QueryRowContext(r.Context(), `SELECT count(*)`+filter, user, admin).Scan(&total); err != nil {
			operationError(w, err)
			return
		}
		rows, err := jsonRows(r.Context(), s.db, `SELECT to_jsonb(p)`+filter+` ORDER BY id DESC LIMIT $4 OFFSET $3`, user, admin, (page-1)*size, size)
		if err != nil {
			operationError(w, err)
			return
		}
		reply(w, map[string]any{"activities": rows, "total": total, "page": page, "page_size": size}, 200)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	var in struct {
		Device string `json:"device_id"`
		Events []struct {
			ID          string    `json:"id"`
			ProxyName   string    `json:"proxy_name"`
			Action      string    `json:"action"`
			OK          bool      `json:"ok"`
			Environment string    `json:"environment_id"`
			Email       string    `json:"email"`
			IP          string    `json:"exit_ip"`
			At          time.Time `json:"created_at"`
		} `json:"events"`
	}
	if jsonBody(r, &in) != nil || len(in.Device) < 8 || len(in.Device) > 100 || len(in.Events) > 200 {
		w.WriteHeader(400)
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		operationError(w, err)
		return
	}
	defer tx.Rollback()
	for _, e := range in.Events {
		if len(e.ID) < 8 || len(e.ID) > 100 || len(e.ProxyName) > 100 || len(e.Action) > 50 || len(e.Email) > 254 || len(e.IP) > 80 || len(e.Environment) > 300 || e.At.IsZero() || e.At.After(time.Now().Add(time.Minute)) {
			w.WriteHeader(400)
			return
		}
		if e.Environment != "" && !strings.Contains(e.Environment, ":user:"+strconv.FormatInt(user, 10)+":account:") {
			w.WriteHeader(403)
			return
		}
		data, _ := json.Marshal(e)
		_, err = tx.ExecContext(r.Context(), `INSERT INTO proxy_activity(user_id,device_id,event_id,data) VALUES($1,$2,$3,$4) ON CONFLICT(user_id,device_id,event_id) DO NOTHING`, user, in.Device, e.ID, data)
		if err != nil {
			operationError(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		operationError(w, err)
		return
	}
	reply(w, map[string]bool{"ok": true}, 200)
}
