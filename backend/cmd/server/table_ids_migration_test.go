package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestTableIDsMigrationPreservesHistory(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("设置 TEST_DATABASE_URL 验证存量表 ID 迁移")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	boundaries := regexp.MustCompile(`(?m)^[ \t]*(BEGIN|COMMIT);[ \t]*$`)
	read := func(path string) string {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return temporarySchemaSQL(boundaries.ReplaceAllString(string(body), ""))
	}
	exec := func(query string) {
		t.Helper()
		if _, err := tx.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	files, err := filepath.Glob("../../migrations/[0-9][0-9][0-9]_*.sql")
	if err != nil || len(files) == 0 {
		t.Fatal("未找到编号迁移", err)
	}
	for _, file := range files {
		if filepath.Base(file) >= "025_table_ids.sql" {
			break
		}
		exec(read(file))
	}
	exec(`SET LOCAL search_path TO pg_temp`)
	// 同时保留正常和已软删除的旧行，用历史时间检测迁移是否误改业务时间。
	exec(`INSERT INTO wallets(user_id,balance,created_at,updated_at,deleted_at) VALUES
(1,120,'2020-01-01','2021-01-01',NULL),(2,45,'2020-01-01','2021-01-01','2021-01-01');
INSERT INTO topup_orders(order_no,user_id,request_key,amount_minor,tokens,created_at,updated_at,deleted_at) VALUES
('old-1',1,'request-1',100,10,'2020-01-01','2021-01-01',NULL),('old-2',2,'request-2',200,20,'2020-01-01','2021-01-01','2021-01-01');
INSERT INTO account_renewals(user_id,request_key,account_id,tokens,renewal_date,created_at,updated_at,deleted_at) VALUES
(1,'request-1',1,10,'2021-02-01','2020-01-01','2021-01-01',NULL),(2,'request-2',2,20,'2021-03-01','2020-01-01','2021-01-01','2021-01-01');
INSERT INTO auth_limits(key,count,created_at,updated_at,deleted_at) VALUES
('old-1',3,'2020-01-01','2021-01-01',NULL),('old-2',5,'2020-01-01','2021-01-01','2021-01-01');`)
	cases := []struct {
		table, columns, conflict, values string
	}{
		{"wallets", "user_id,balance", "user_id", "3,75"},
		{"topup_orders", "order_no,user_id,request_key,amount_minor,tokens", "order_no", "'new-3',3,'request-3',300,30"},
		{"account_renewals", "user_id,request_key,account_id,tokens,renewal_date", "user_id,request_key", "3,'request-3',3,30,'2021-04-01'"},
		{"auth_limits", "key,count", "key", "'new-3',7"},
	}
	state := func(table string, withID bool) string {
		t.Helper()
		payload := "to_jsonb(t)"
		if !withID {
			payload += "-'id'"
		}
		var result string
		if err := tx.QueryRow(`SELECT jsonb_agg(payload ORDER BY payload::text)::text FROM (SELECT ` + payload + ` AS payload FROM pg_temp.` + table + ` t) rows`).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before, migrated := map[string]string{}, map[string]string{}
	for _, c := range cases {
		before[c.table] = state(c.table, false)
	}
	migration := read("../../migrations/025_table_ids.sql")
	for attempt := 0; attempt < 2; attempt++ {
		exec(migration)
		for _, c := range cases {
			if state(c.table, false) != before[c.table] {
				t.Fatalf("%s 迁移改变了原业务数据", c.table)
			}
			var count, distinct int
			if err := tx.QueryRow("SELECT count(*),count(DISTINCT id) FROM pg_temp."+c.table).Scan(&count, &distinct); err != nil || count != 2 || distinct != count {
				t.Fatalf("%s 存量行 ID 未补齐或不唯一: %d %d %v", c.table, count, distinct, err)
			}
			if attempt == 0 {
				migrated[c.table] = state(c.table, true)
			} else if state(c.table, true) != migrated[c.table] {
				t.Fatalf("%s 重放改变了 ID", c.table)
			}
		}
	}
	for _, c := range cases {
		// 显式冲突目标验证业务键仍有唯一约束，包括已删除行，防止历史重复入账。
		result, err := tx.Exec("INSERT INTO pg_temp." + c.table + "(" + c.columns + ") SELECT " + c.columns + " FROM pg_temp." + c.table + " ON CONFLICT(" + c.conflict + ") DO NOTHING")
		if err != nil {
			t.Fatal(err)
		}
		if n, err := result.RowsAffected(); err != nil || n != 0 {
			t.Fatalf("%s 业务唯一性丢失: %d %v", c.table, n, err)
		}
		var previous, next int64
		if err := tx.QueryRow("SELECT max(id) FROM pg_temp." + c.table).Scan(&previous); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow("INSERT INTO pg_temp." + c.table + "(" + c.columns + ") VALUES(" + c.values + ") RETURNING id").Scan(&next); err != nil || next <= previous {
			t.Fatalf("%s 新行 ID 未正常自增: %d %d %v", c.table, previous, next, err)
		}
		// 软删除仍保留原 ID 和创建时间，并维护更新时间；普通查询只看到新行。
		exec("UPDATE pg_temp." + c.table + " SET deleted_at=NOW(),updated_at=NOW() WHERE id<=(SELECT max(id) FROM pg_temp." + c.table + " WHERE created_at='2020-01-01') AND deleted_at IS NULL")
		var total, active, preserved int
		err = tx.QueryRow("SELECT count(*),count(*) FILTER(WHERE deleted_at IS NULL),count(*) FILTER(WHERE created_at='2020-01-01' AND updated_at=NOW() AND deleted_at=NOW()) FROM pg_temp."+c.table).Scan(&total, &active, &preserved)
		if err != nil || total != 3 || active != 1 || preserved != 1 {
			t.Fatalf("%s 软删除未保留原行和时间语义: %d %d %d %v", c.table, total, active, preserved, err)
		}
	}
}

func TestSchemaVerifyRejectsInvalidID(t *testing.T) {
	db := walletTestDB(t)
	verify, err := os.ReadFile("../../migrations/verify.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, change string }{
		{"缺少主键", "ALTER TABLE wallets DROP CONSTRAINT wallets_pkey"},
		{"联合主键", "ALTER TABLE wallets DROP CONSTRAINT wallets_pkey; ALTER TABLE wallets ADD PRIMARY KEY(id,user_id)"},
		{"缺少自增默认值", "ALTER TABLE wallets ALTER COLUMN id DROP DEFAULT"},
		{"错误类型", "ALTER TABLE wallets ALTER COLUMN id TYPE INTEGER"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if _, err := tx.Exec("SET LOCAL search_path TO pg_temp; " + c.change); err != nil {
				t.Fatal(err)
			}
			rows, err := tx.Query(string(verify))
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			invalidTables := 0
			for rows.Next() {
				var database, schema, table, status, missing, invalid, triggers string
				var invalidID bool
				if err := rows.Scan(&database, &schema, &table, &status, &missing, &invalid, &triggers, &invalidID); err != nil {
					t.Fatal(err)
				}
				if status != "OK" {
					invalidTables++
					if table != "wallets" || status != "INVALID" || !invalidID {
						t.Fatalf("核对异常: %s %s invalid_id=%t", table, status, invalidID)
					}
				}
			}
			if err := rows.Err(); err != nil || invalidTables != 1 {
				t.Fatalf("应只识别钱包 ID 定义错误: %d %v", invalidTables, err)
			}
		})
	}
}
