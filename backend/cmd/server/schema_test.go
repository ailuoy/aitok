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
	for _, scenario := range []struct {
		name    string
		scripts []string
	}{
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
