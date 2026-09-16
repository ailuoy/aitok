package main

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// 复现上线截图的八张旧表，在会话临时表验证，绝不迁移业务库。
func TestLegacyScreenshotUpgrade(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("设置 TEST_DATABASE_URL 验证截图旧库升级")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	boundaries := regexp.MustCompile(`(?m)^[ \t]*(BEGIN|COMMIT);[ \t]*$`)
	read := func(name string) string {
		t.Helper()
		body, err := os.ReadFile("../../migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		script := strings.ReplaceAll(string(body), "SET LOCAL search_path TO public, pg_catalog;", "SET LOCAL search_path TO pg_temp;")
		return temporarySchemaSQL(boundaries.ReplaceAllString(script, ""))
	}
	upgrade := read("upgrade-from-002.sql")
	for _, name := range []string{"保留数据及重复执行", "拒绝不匹配的旧字段", "拒绝未识别的外键"} {
		t.Run(name, func(t *testing.T) {
			tx, err := db.BeginTx(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			exec := func(query string) {
				t.Helper()
				if _, err := tx.Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			for _, file := range []string{"000_initial_schema.sql", "001_wallet_and_renewals.sql", "002_payment_quantity_and_history.sql"} {
				exec(read(file))
			}
			exec(`SET LOCAL search_path TO pg_temp`)
			exec(`INSERT INTO users(id,email,password_hash,created_at) VALUES(1,'legacy@test.local','test-hash','2020-01-01');
INSERT INTO chatgpt_accounts(id,user_id,label,email,api_key,session_ciphertext,renewal_date,created_at) VALUES(1,1,'Legacy','account@test.local','test-key','test-ciphertext','2027-01-31','2020-02-01');
INSERT INTO email_codes(email,purpose,code,expires_at) VALUES('legacy@test.local','register','test-code','2020-03-01');
INSERT INTO wallets(user_id,balance) VALUES(1,250);
INSERT INTO topup_orders(order_no,user_id,request_key,amount_minor,tokens,status,session_id,checkout_url,quantity,unit_amount_minor,price_id,created_at) VALUES('legacy-order',1,'legacy-request',1000,300,'paid','test-session','https://example.invalid',2,500,'test-price','2020-04-01');
INSERT INTO wallet_ledger(user_id,amount,balance_after,kind,reference,description,created_at) VALUES(1,300,300,'topup','test-topup','历史入账','2020-04-01'),(1,-50,250,'renewal','test-renewal','历史支出','2020-05-01');
INSERT INTO account_renewals(user_id,request_key,account_id,tokens,renewal_date,account_label,months,created_at) VALUES(1,'legacy-renewal',1,50,'2027-01-31','Legacy',1,'2020-05-01');
INSERT INTO renewal_date_audit(account_id,admin_id,previous_date,renewal_date,created_at) VALUES(1,1,'2026-12-31','2027-01-31','2020-05-01');`)
			if name != "保留数据及重复执行" {
				if name == "拒绝不匹配的旧字段" {
					exec(`ALTER TABLE chatgpt_accounts ALTER COLUMN api_key TYPE VARCHAR(255)`)
				} else {
					// 截图未展示的自定义约束不能静默保留或擅自删除。
					exec(`ALTER TABLE wallets RENAME CONSTRAINT wallets_user_id_fkey TO custom_wallet_owner_fkey`)
				}
				exec(`SAVEPOINT before_upgrade`)
				if _, err := tx.Exec(upgrade); err == nil || !strings.Contains(err.Error(), "aitok_upgrade_schema_check") {
					t.Fatalf("不匹配结构应由提交保护阻止: %v", err)
				}
				exec(`ROLLBACK TO SAVEPOINT before_upgrade`)
				var count int
				if err := tx.QueryRow(`SELECT count(*) FROM pg_class WHERE relnamespace=pg_my_temp_schema() AND relkind='r'`).Scan(&count); err != nil || count != 8 {
					t.Fatalf("失败应恢复八张旧表: %d %v", count, err)
				}
				return
			}
			tables := []string{"users", "chatgpt_accounts", "email_codes", "wallets", "topup_orders", "wallet_ledger", "account_renewals", "renewal_date_audit"}
			columns, before, upgraded := map[string]string{}, map[string]string{}, map[string]string{}
			state := func(table, columns string) string {
				t.Helper()
				var result string
				if err := tx.QueryRow(`SELECT jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text)::text FROM (SELECT ` + columns + ` FROM pg_temp.` + table + `) r`).Scan(&result); err != nil {
					t.Fatal(err)
				}
				return result
			}
			for _, table := range tables {
				var list string
				if err := tx.QueryRow(`SELECT string_agg(quote_ident(a.attname),',' ORDER BY a.attnum) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid WHERE c.relnamespace=pg_my_temp_schema() AND c.relname=$1 AND a.attnum>0 AND NOT a.attisdropped`, table).Scan(&list); err != nil {
					t.Fatal(err)
				}
				columns[table] = list
				before[table] = state(table, list)
			}
			for attempt := 0; attempt < 2; attempt++ {
				exec(upgrade)
				for _, table := range tables {
					if state(table, columns[table]) != before[table] {
						t.Fatalf("%s 旧数据被改动", table)
					}
					if attempt == 0 {
						upgraded[table] = state(table, "*")
					} else if state(table, "*") != upgraded[table] {
						t.Fatalf("%s 重复升级改变 ID 或数据", table)
					}
				}
			}
		})
	}
}

// 部分 SQL 客户端按更新命令执行整段迁移，任何行结果集都会导致客户端报错。
func TestLegacyUpgradeReturnsNoResultSets(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("设置 TEST_DATABASE_URL 验证迁移结果协议")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "BEGIN"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "ROLLBACK")
	boundaries := regexp.MustCompile(`(?m)^[ \t]*(BEGIN|COMMIT);[ \t]*$`)
	for _, file := range []string{"000_initial_schema.sql", "001_wallet_and_renewals.sql", "002_payment_quantity_and_history.sql", "upgrade-from-002.sql", "upgrade-from-002.sql"} {
		body, err := os.ReadFile("../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		script := strings.ReplaceAll(string(body), "SET LOCAL search_path TO public, pg_catalog;", "SET LOCAL search_path TO pg_temp;")
		script = temporarySchemaSQL(boundaries.ReplaceAllString(script, ""))
		results, err := conn.PgConn().Exec(ctx, script).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		for _, result := range results {
			if len(result.FieldDescriptions) != 0 || len(result.Rows) != 0 {
				t.Fatalf("%s 返回了客户端不期望的查询结果集", file)
			}
		}
	}
}
