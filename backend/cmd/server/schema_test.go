package main

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// 比较 PostgreSQL 实际解析的结构，避免只匹配 SQL 文本而漏掉约束或索引差异。
const schemaDefinitionQuery = `
SELECT 'table', c.relname, COALESCE(obj_description(c.oid,'pg_class'),'')
FROM pg_class c WHERE c.relnamespace=pg_my_temp_schema() AND c.relkind='r'
UNION ALL
SELECT 'column', c.relname||'.'||a.attname,
  format_type(a.atttypid,a.atttypmod)||'|'||a.attnotnull::text||'|'||COALESCE(pg_get_expr(d.adbin,d.adrelid),'')
FROM pg_class c JOIN pg_attribute a ON a.attrelid=c.oid
LEFT JOIN pg_attrdef d ON d.adrelid=c.oid AND d.adnum=a.attnum
WHERE c.relnamespace=pg_my_temp_schema() AND c.relkind='r' AND a.attnum>0 AND NOT a.attisdropped
UNION ALL
SELECT 'constraint', c.relname||'.'||con.conname, pg_get_constraintdef(con.oid)
FROM pg_class c JOIN pg_constraint con ON con.conrelid=c.oid
WHERE c.relnamespace=pg_my_temp_schema() AND c.relkind='r'
UNION ALL
SELECT 'index', c.relname||'.'||idx.relname, pg_get_indexdef(i.indexrelid)
FROM pg_class c JOIN pg_index i ON i.indrelid=c.oid JOIN pg_class idx ON idx.oid=i.indexrelid
WHERE c.relnamespace=pg_my_temp_schema() AND c.relkind='r'
UNION ALL
SELECT 'trigger', c.relname||'.'||tr.tgname, pg_get_triggerdef(tr.oid)
FROM pg_class c JOIN pg_trigger tr ON tr.tgrelid=c.oid
WHERE c.relnamespace=pg_my_temp_schema() AND c.relkind='r' AND NOT tr.tgisinternal
UNION ALL
SELECT 'function', p.proname, pg_get_functiondef(p.oid)
FROM pg_proc p WHERE p.pronamespace=pg_my_temp_schema()
ORDER BY 1,2`

// 函数和触发器也必须隔离在临时 schema，不能替换实际业务库中的函数。
func temporarySchemaSQL(script string) string {
	script = strings.ReplaceAll(script, "CREATE TABLE IF NOT EXISTS", "CREATE TEMP TABLE IF NOT EXISTS")
	for _, name := range []string{"aitok_touch_timestamps", "aitok_prevent_hard_delete"} {
		script = strings.ReplaceAll(script, "FUNCTION "+name+"(", "FUNCTION pg_temp."+name+"(")
		script = strings.ReplaceAll(script, "FUNCTION IF EXISTS "+name+"(", "FUNCTION IF EXISTS pg_temp."+name+"(")
	}
	return script
}

func TestSchemaSnapshotMatchesMigrations(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("设置 TEST_DATABASE_URL 验证完整 schema 与历史迁移一致")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	read := func(path string) string {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	files, err := filepath.Glob("../../migrations/[0-9][0-9][0-9]_*.sql")
	if err != nil || len(files) == 0 {
		t.Fatal("未找到编号迁移", err)
	}
	var chain strings.Builder
	for _, file := range files {
		chain.WriteString(read(file))
		chain.WriteByte('\n')
	}
	snapshot := read("../../migrations/schema.sql")
	release := read("../../migrations/release.sql")
	legacyUpgrade := strings.ReplaceAll(read("../../migrations/upgrade-from-002.sql"), "SET LOCAL search_path TO public, pg_catalog;", "SET LOCAL search_path TO pg_temp;")
	// 截图升级包固定到 025；后续结构通过新增迁移继续升级，历史包保持不变。
	for _, file := range files {
		if filepath.Base(file) > "025_table_ids.sql" {
			legacyUpgrade += "\n" + read(file)
		}
	}
	generated, err := exec.Command("bash", "../../../scripts/build-release-sql.sh").Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(generated) != release {
		t.Fatal("release.sql 未同步生成")
	}
	boundaries := regexp.MustCompile(`(?m)^[ \t]*(BEGIN|COMMIT);[ \t]*$`)
	temporarySchema := regexp.MustCompile(`pg_temp(?:_[0-9]+)?\.`)
	inspect := func(t *testing.T, scripts ...string) map[string]string {
		t.Helper()
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for _, script := range scripts {
			// 只建立会话临时表，事务结束回滚，不修改现有业务表及其注释。
			query := temporarySchemaSQL(boundaries.ReplaceAllString(script, ""))
			if _, err = tx.Exec(query); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = tx.Exec(`SET LOCAL search_path TO pg_temp`); err != nil {
			t.Fatal(err)
		}
		// 快照控制新表的展示顺序；历史迁移保留原有物理列顺序。
		if len(scripts) == 1 && scripts[0] == snapshot {
			var invalid int
			err = tx.QueryRow(`SELECT count(*) FROM pg_class c WHERE c.relnamespace=pg_my_temp_schema() AND c.relkind='r'
AND ARRAY(SELECT a.attname::text FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped ORDER BY a.attnum DESC LIMIT 3)
<> ARRAY['deleted_at','updated_at','created_at']`).Scan(&invalid)
			if err != nil || invalid != 0 {
				t.Fatalf("快照时间字段必须按 created_at、updated_at、deleted_at 位于末尾：不符合的表数=%d，错误=%v", invalid, err)
			}
		}
		// information_schema 将隔离测试表标为 LOCAL TEMPORARY，生产表为 BASE TABLE。
		verify := strings.ReplaceAll(read("../../migrations/verify.sql"), "t.table_type = 'BASE TABLE'", "t.table_type = 'LOCAL TEMPORARY'")
		checks, err := tx.Query(verify)
		if err != nil {
			t.Fatal(err)
		}
		checkedTables := 0
		for checks.Next() {
			var database, schema, table, status, missing, invalid, triggers string
			var invalidID bool
			if err = checks.Scan(&database, &schema, &table, &status, &missing, &invalid, &triggers, &invalidID); err != nil {
				t.Fatal(err)
			}
			if status != "OK" {
				t.Errorf("%s 结构核对失败: %s %s %s %s invalid_id=%t", table, status, missing, invalid, triggers, invalidID)
			}
			checkedTables++
		}
		if err = checks.Err(); err != nil {
			t.Fatal(err)
		}
		checks.Close()
		if checkedTables != 22 {
			t.Fatalf("结构核对应覆盖全部22张表，实际%d", checkedTables)
		}
		rows, err := tx.Query(schemaDefinitionQuery)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		result := map[string]string{}
		tableCount := 0
		for rows.Next() {
			var kind, name, definition string
			if err = rows.Scan(&kind, &name, &definition); err != nil {
				t.Fatal(err)
			}
			if kind == "table" {
				tableCount++
				if strings.TrimSpace(definition) == "" {
					t.Errorf("表 %s 缺少注释", name)
				}
			}
			result[kind+":"+name] = temporarySchema.ReplaceAllString(definition, "")
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		if tableCount == 0 {
			t.Fatal("没有创建任何业务临时表")
		}
		return result
	}
	want := inspect(t, snapshot)
	duplicateHistory := `INSERT INTO users(id,email,password_hash,deleted_at) VALUES(9001,'schema@test.local','',NOW()),(9002,'schema@test.local','',NULL);
INSERT INTO account_groups(user_id,name,deleted_at) VALUES(9002,'same name',NOW()),(9002,'same name',NULL);
INSERT INTO addresses(address_line1,city,state,postal_code,deleted_at) VALUES('1 Test Road','Portland','OR','97201',NOW()),('1 Test Road','Portland','OR','97201',NULL);
INSERT INTO bank_cards(user_id,label,cardholder,number_ciphertext,number_fingerprint,last4,brand,exp_month,exp_year,deleted_at) VALUES(9002,'Card','Test','test','test','4242','Visa',12,2030,NOW()),(9002,'Card','Test','test','test','4242','Visa',12,2030,NULL);`
	multiCycleHistory := duplicateHistory + `
INSERT INTO chatgpt_accounts(id,user_id,label,email) VALUES(9900,9002,'History','history@test.local');
INSERT INTO bank_card_ledger(card_id,actor_id,request_key,kind,amount_usd_minor,balance_after_usd_minor,account_id,account_email,period_start,period_end,reversed_at,external_reference) VALUES
(2,9002,'schema-cycle-reversed','subscription',-100,900,9900,'history@test.local','2030-01-01','2030-02-01',NOW(),'schema-original'),
(2,9002,'schema-cycle-correct','subscription',-100,900,9900,'history@test.local','2030-01-01','2030-02-01',NULL,'schema-correct'),
(2,9002,'schema-cycle-next','subscription',-100,800,9900,'history@test.local','2030-02-01','2030-03-01',NULL,'schema-next');`
	closedOrders := duplicateHistory + `
INSERT INTO recharge_orders(id,order_no,user_id,account_id,account_email,package_id,package_snapshot,period_start,period_end,sale_usd_minor,request_key,order_status) VALUES
(101,'closed-refund',9002,9900,'closed@test.local',1,'{}','2030-01-01','2030-02-01',100,'closed-refund','refunded'),
(102,'closed-discard',9002,9900,'closed@test.local',1,'{}','2030-01-01','2030-02-01',100,'closed-discard','discarded'),
(103,'closed-new',9002,9900,'closed@test.local',1,'{}','2030-01-01','2030-02-01',100,'closed-new','active');
INSERT INTO bank_card_ledger(card_id,actor_id,request_key,kind,amount_usd_minor,balance_after_usd_minor,order_id,account_email,period_start,period_end,external_reference) VALUES
(2,9002,'closed-charge-1','subscription',-100,900,101,'closed@test.local','2030-01-01','2030-02-01','closed-charge-1'),
(2,9002,'closed-charge-2','subscription',-100,800,102,'closed@test.local','2030-01-01','2030-02-01','closed-charge-2'),
(2,9002,'closed-charge-3','subscription',-100,700,103,'closed@test.local','2030-01-01','2030-02-01','closed-charge-3');`
	for _, scenario := range []struct {
		name    string
		scripts []string
	}{
		{"截图旧版八表增量升级", []string{read(files[0]), read(files[1]), read(files[2]), legacyUpgrade}},
		{"截图旧版八表增量重复升级", []string{read(files[0]), read(files[1]), read(files[2]), legacyUpgrade, legacyUpgrade}},
		{"退款废弃后同周期重建并重复升级", []string{snapshot, closedOrders, release, release}},
		{"人民币汇率历史后重复升级", []string{snapshot, `INSERT INTO exchange_rates(base_currency,quote_currency,rate,source,effective_at) VALUES('PHP','USD',0.01589,'test',NOW()),('PHP','CNY',0.1067,'test',NOW());`, release, release}},
		{"多周期与冲正历史后重复升级", []string{snapshot, multiCycleHistory, release, release}},
		{"编号迁移", []string{chain.String()}},
		{"上线 SQL", []string{release}},
		{"快照后重复升级", []string{snapshot, release, release}},
		{"旧基线升级", []string{read(files[0]), release}},
		{"软删除同名历史后重复升级", []string{snapshot, duplicateHistory, release, release}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			actual := inspect(t, scenario.scripts...)
			if !reflect.DeepEqual(actual, want) {
				for key, value := range want {
					if actual[key] != value {
						t.Errorf("%s 不一致：实际 %q，快照 %q", key, actual[key], value)
					}
				}
				for key := range actual {
					if _, ok := want[key]; !ok {
						t.Errorf("快照缺少 %s", key)
					}
				}
			}
		})
	}
}
