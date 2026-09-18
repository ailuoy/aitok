package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

type Address struct {
	UserID       *int64          `json:"user_id"`
	CanEdit      bool            `json:"can_edit"`
	ID           int64           `json:"id"`
	FullName     string          `json:"full_name"`
	SourceData   json.RawMessage `json:"source_data"`
	AddressLine1 string          `json:"address_line1"`
	AddressLine2 string          `json:"address_line2"`
	City         string          `json:"city"`
	State        string          `json:"state"`
	PostalCode   string          `json:"postal_code"`
	Country      string          `json:"country"`
	SourceURL    string          `json:"source_url"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

func (a *Address) normalize() bool {
	for _, field := range []*string{&a.FullName, &a.AddressLine1, &a.AddressLine2, &a.City, &a.State, &a.PostalCode, &a.Country} {
		*field = strings.Join(strings.Fields(*field), " ")
	}
	a.Country = strings.ToUpper(a.Country)
	if len(a.Country) != 2 || a.Country[0] < 'A' || a.Country[0] > 'Z' || a.Country[1] < 'A' || a.Country[1] > 'Z' {
		return false
	}
	for _, field := range []struct {
		value string
		max   int
	}{{a.AddressLine1, 200}, {a.City, 100}, {a.State, 100}, {a.PostalCode, 20}} {
		if field.value == "" || utf8.RuneCountInString(field.value) > field.max {
			return false
		}
	}
	return utf8.RuneCountInString(a.AddressLine2) <= 200 && utf8.RuneCountInString(a.FullName) <= 120
}

const addressColumns = `id,address_line1,address_line2,city,state,postal_code,country,source_url,created_at,updated_at,user_id,full_name,source_data`

func scanAddress(row interface{ Scan(...any) error }) (Address, error) {
	var a Address
	err := row.Scan(&a.ID, &a.AddressLine1, &a.AddressLine2, &a.City, &a.State, &a.PostalCode, &a.Country, &a.SourceURL, &a.CreatedAt, &a.UpdatedAt, &a.UserID, &a.FullName, &a.SourceData)
	return a, err
}

func addressError(w http.ResponseWriter, err error) {
	var pg *pgconn.PgError
	if errors.Is(err, sql.ErrNoRows) {
		reply(w, map[string]string{"error": "地址不存在"}, 404)
		return
	}
	if errors.As(err, &pg) && pg.Code == "23505" {
		reply(w, map[string]string{"error": "相同地址已存在"}, 409)
		return
	}
	reply(w, map[string]string{"error": "地址操作失败，请稍后重试"}, 500)
}

func (s *Server) addresses(w http.ResponseWriter, r *http.Request) {
	user, err := s.auth(r)
	if err != nil {
		reply(w, map[string]string{"error": "请先登录"}, 401)
		return
	}
	admin, err := s.isAdmin(r.Context(), user)
	if err != nil {
		addressError(w, err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	var id int64
	if r.URL.Path != "/api/addresses" {
		id, err = strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/addresses/"), 10, 64)
		if err != nil || id <= 0 {
			http.NotFound(w, r)
			return
		}
	}
	if r.Method == http.MethodGet && id == 0 {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if utf8.RuneCountInString(query) > 200 {
			reply(w, map[string]string{"error": "搜索内容过长"}, 400)
			return
		}
		page, size, valid := pageParameters(r)
		if !valid {
			reply(w, map[string]string{"error": "分页参数无效"}, 400)
			return
		}
		stateCodes := r.URL.Query().Get("state_codes")
		if len(stateCodes) > 200 {
			reply(w, map[string]string{"error": "搜索参数无效"}, 400)
			return
		}
		filter := ` WHERE deleted_at IS NULL AND (user_id=$1 OR user_id IS NULL OR $2) AND (strpos(lower(concat_ws(' ',full_name,address_line1,address_line2,city,state,postal_code,country,source_data->>'Telephone',source_data->>'Temporary_mail')),lower($3)) > 0 OR (upper(country)='US' AND upper(state)=ANY(string_to_array($4,','))))`
		if r.URL.Query().Get("unbound") == "true" {
			filter += ` AND NOT EXISTS(SELECT 1 FROM chatgpt_accounts a WHERE a.billing_address_id=addresses.id AND a.deleted_at IS NULL)`
		}
		var total int
		if err = s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM addresses`+filter, user, admin, query, stateCodes).Scan(&total); err != nil {
			addressError(w, err)
			return
		}
		rows, err := s.db.QueryContext(r.Context(), `SELECT `+addressColumns+` FROM addresses`+filter+` ORDER BY updated_at DESC,id DESC LIMIT $6 OFFSET $5`, user, admin, query, stateCodes, (page-1)*size, size)
		if err != nil {
			addressError(w, err)
			return
		}
		defer rows.Close()
		items := []Address{}
		for rows.Next() {
			a, err := scanAddress(rows)
			if err != nil {
				addressError(w, err)
				return
			}
			a.CanEdit = admin || (a.UserID != nil && *a.UserID == user)
			items = append(items, a)
		}
		if err = rows.Err(); err != nil {
			addressError(w, err)
			return
		}
		reply(w, map[string]any{"addresses": items, "total": total, "page": page, "page_size": size}, 200)
		return
	}
	if r.Method == http.MethodGet && id > 0 {
		a, err := scanAddress(s.db.QueryRowContext(r.Context(), `SELECT `+addressColumns+` FROM addresses WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR user_id IS NULL OR $3)`, id, user, admin))
		if err != nil {
			addressError(w, err)
			return
		}
		a.CanEdit = admin || (a.UserID != nil && *a.UserID == user)
		reply(w, map[string]any{"address": a}, 200)
		return
	}
	if (r.Method == http.MethodPost && id == 0) || (r.Method == http.MethodPatch && id > 0) {
		var a Address
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if jsonBody(r, &a) != nil || !a.normalize() {
			reply(w, map[string]string{"error": "请填写有效的街道、城市、州、邮编及两位国家代码，姓名不能超过 120 字"}, 400)
			return
		}
		args := []any{a.AddressLine1, a.AddressLine2, a.City, a.State, a.PostalCode, a.Country, user, a.FullName}
		query := `INSERT INTO addresses(address_line1,address_line2,city,state,postal_code,country,user_id,full_name) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING ` + addressColumns
		status := 201
		if id > 0 {
			query = `UPDATE addresses SET address_line1=$1,address_line2=$2,city=$3,state=$4,postal_code=$5,country=$6,full_name=$8,updated_at=NOW() WHERE deleted_at IS NULL AND (user_id=$7 OR $10) AND id=$9 RETURNING ` + addressColumns
			args = append(args, id, admin)
			status = 200
		}
		a, err = scanAddress(s.db.QueryRowContext(r.Context(), query, args...))
		if err != nil {
			addressError(w, err)
			return
		}
		a.CanEdit = true
		reply(w, map[string]any{"address": a}, status)
		return
	}
	if r.Method == http.MethodDelete && id > 0 {
		tx, err := s.db.BeginTx(r.Context(), nil)
		if err != nil {
			addressError(w, err)
			return
		}
		defer tx.Rollback()
		result, err := tx.ExecContext(r.Context(), `UPDATE addresses SET updated_at=NOW(),deleted_at=NOW() WHERE id=$1 AND deleted_at IS NULL AND (user_id=$2 OR $3)`, id, user, admin)
		if err != nil {
			addressError(w, err)
			return
		}
		count, err := result.RowsAffected()
		if err != nil {
			addressError(w, err)
			return
		}
		if count == 0 {
			addressError(w, sql.ErrNoRows)
			return
		}
		rows, err := tx.QueryContext(r.Context(), `UPDATE chatgpt_accounts SET billing_address_id=NULL,updated_at=NOW() WHERE billing_address_id=$1 AND deleted_at IS NULL RETURNING id`, id)
		if err != nil {
			addressError(w, err)
			return
		}
		var accounts []int64
		for rows.Next() {
			var account int64
			if err = rows.Scan(&account); err != nil {
				rows.Close()
				addressError(w, err)
				return
			}
			accounts = append(accounts, account)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			addressError(w, err)
			return
		}
		for _, account := range accounts {
			if err = recordEvent(r.Context(), tx, user, account, "account", "billing_address", eventKey(), map[string]any{"billing_address_id": id}, map[string]any{"billing_address_id": nil, "reason": "address_deleted"}); err != nil {
				addressError(w, err)
				return
			}
		}
		if err = recordEvent(r.Context(), tx, user, id, "address", "delete", eventKey(), map[string]any{}, map[string]any{"unbound_accounts": len(accounts)}); err == nil {
			err = tx.Commit()
		}
		if err != nil {
			addressError(w, err)
			return
		}
		w.WriteHeader(204)
		return
	}
	w.WriteHeader(405)
}
