-- PostgreSQL 12+：只读导出结构，用于与 schema.sql 比较；不读取业务表数据。
-- 默认导出 public；如应用使用其他 schema，修改 requested_schemas 中的数组。
-- SQL 客户端执行后导出 schema_report 单元格的完整文本，避免截断。
-- psql -X -qAt -v ON_ERROR_STOP=1 -f backend/migrations/inspect.sql > online-schema.json
BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '60s';
SET LOCAL lock_timeout = '5s';
SET LOCAL search_path = pg_catalog;

WITH requested_schemas AS (
  SELECT ARRAY['public']::text[] AS names
), namespaces AS (
  SELECT n.oid, n.nspname
  FROM pg_namespace n, requested_schemas r
  WHERE n.nspname = ANY(r.names)
), relations AS (
  SELECT c.*, n.nspname
  FROM pg_class c JOIN namespaces n ON n.oid = c.relnamespace
  WHERE c.relkind IN ('r', 'p')
), tables AS (
  SELECT c.nspname AS schema_name, c.relname AS table_name,
    obj_description(c.oid, 'pg_class') AS comment,
    c.relkind AS kind, c.relpersistence AS persistence,
    c.relrowsecurity AS row_security, c.relforcerowsecurity AS force_row_security,
    c.reloptions AS options,
    CASE WHEN c.relkind = 'p' THEN pg_get_partkeydef(c.oid) END AS partition_key,
    pg_get_expr(c.relpartbound, c.oid) AS partition_bound,
    (SELECT COALESCE(jsonb_agg(format('%I.%I', pn.nspname, p.relname) ORDER BY i.inhseqno), '[]'::jsonb)
     FROM pg_inherits i JOIN pg_class p ON p.oid = i.inhparent
     JOIN pg_namespace pn ON pn.oid = p.relnamespace WHERE i.inhrelid = c.oid) AS parents,
    (SELECT COALESCE(jsonb_agg(jsonb_build_object(
      'position', a.attnum, 'name', a.attname,
      'type', format_type(a.atttypid, a.atttypmod), 'not_null', a.attnotnull,
      'default', pg_get_expr(d.adbin, d.adrelid),
      'identity', a.attidentity, 'generated', a.attgenerated,
      'collation', CASE WHEN a.attcollation <> 0 THEN format('%I.%I', cn.nspname, co.collname) END,
      'comment', col_description(c.oid, a.attnum)
    ) ORDER BY a.attnum), '[]'::jsonb)
     FROM pg_attribute a LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
     LEFT JOIN pg_collation co ON co.oid = a.attcollation
     LEFT JOIN pg_namespace cn ON cn.oid = co.collnamespace
     WHERE a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped) AS columns,
    (SELECT COALESCE(jsonb_agg(jsonb_build_object(
      'name', k.conname, 'type', k.contype, 'definition', pg_get_constraintdef(k.oid, false),
      'validated', k.convalidated, 'deferrable', k.condeferrable,
      'initially_deferred', k.condeferred, 'comment', obj_description(k.oid, 'pg_constraint')
    ) ORDER BY k.conname), '[]'::jsonb)
     FROM pg_constraint k WHERE k.conrelid = c.oid) AS constraints,
    (SELECT COALESCE(jsonb_agg(jsonb_build_object(
      'name', ic.relname, 'definition', pg_get_indexdef(i.indexrelid),
      'valid', i.indisvalid, 'ready', i.indisready, 'primary', i.indisprimary,
      'unique', i.indisunique, 'comment', obj_description(i.indexrelid, 'pg_class')
    ) ORDER BY ic.relname), '[]'::jsonb)
     FROM pg_index i JOIN pg_class ic ON ic.oid = i.indexrelid WHERE i.indrelid = c.oid) AS indexes,
    (SELECT COALESCE(jsonb_agg(jsonb_build_object(
      'name', t.tgname, 'definition', pg_get_triggerdef(t.oid, false),
      'enabled', t.tgenabled, 'function', t.tgfoid::regprocedure::text,
      'comment', obj_description(t.oid, 'pg_trigger')
    ) ORDER BY t.tgname), '[]'::jsonb)
     FROM pg_trigger t WHERE t.tgrelid = c.oid AND NOT t.tgisinternal) AS triggers,
    (SELECT COALESCE(jsonb_agg(jsonb_build_object(
      'name', p.polname, 'command', p.polcmd, 'permissive', p.polpermissive,
      'roles', (SELECT jsonb_agg(CASE WHEN role_id = 0 THEN 'PUBLIC' ELSE pg_get_userbyid(role_id) END ORDER BY role_id)
                FROM unnest(p.polroles) AS roles(role_id)),
      'using', pg_get_expr(p.polqual, p.polrelid), 'check', pg_get_expr(p.polwithcheck, p.polrelid)
    ) ORDER BY p.polname), '[]'::jsonb)
     FROM pg_policy p WHERE p.polrelid = c.oid) AS policies
  FROM relations c
), sequences AS (
  SELECT n.nspname AS schema_name, c.relname AS sequence_name,
    format_type(s.seqtypid, NULL) AS data_type, s.seqstart AS start_value,
    s.seqincrement AS increment_by, s.seqmin AS min_value, s.seqmax AS max_value,
    s.seqcache AS cache_size, s.seqcycle AS cycle,
    obj_description(c.oid, 'pg_class') AS comment,
    (SELECT jsonb_build_object('schema', tn.nspname, 'table', tc.relname, 'column', a.attname)
     FROM pg_depend d JOIN pg_class tc ON tc.oid = d.refobjid
     JOIN pg_namespace tn ON tn.oid = tc.relnamespace
     JOIN pg_attribute a ON a.attrelid = tc.oid AND a.attnum = d.refobjsubid
     WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid
       AND d.refclassid = 'pg_class'::regclass AND d.deptype IN ('a', 'i') LIMIT 1) AS owned_by
  FROM pg_sequence s JOIN pg_class c ON c.oid = s.seqrelid
  JOIN namespaces n ON n.oid = c.relnamespace
), routines AS (
  SELECT n.nspname AS schema_name, p.proname AS name, p.prokind AS kind,
    pg_get_function_identity_arguments(p.oid) AS arguments,
    pg_get_functiondef(p.oid) AS definition, obj_description(p.oid, 'pg_proc') AS comment
  FROM pg_proc p JOIN namespaces n ON n.oid = p.pronamespace
  WHERE p.prokind IN ('f', 'p') AND NOT EXISTS (
    SELECT 1 FROM pg_depend d WHERE d.classid = 'pg_proc'::regclass
      AND d.objid = p.oid AND d.deptype = 'e'
  )
), views AS (
  SELECT n.nspname AS schema_name, c.relname AS name, c.relkind AS kind,
    pg_get_viewdef(c.oid, false) AS definition, obj_description(c.oid, 'pg_class') AS comment
  FROM pg_class c JOIN namespaces n ON n.oid = c.relnamespace WHERE c.relkind IN ('v', 'm')
), custom_types AS (
  SELECT n.nspname AS schema_name, t.typname AS name, t.typtype AS kind,
    obj_description(t.oid, 'pg_type') AS comment,
    CASE WHEN t.typtype = 'd' THEN format_type(t.typbasetype, t.typtypmod) END AS base_type,
    t.typnotnull AS not_null, t.typdefault AS default_value,
    (SELECT COALESCE(jsonb_agg(e.enumlabel ORDER BY e.enumsortorder), '[]'::jsonb)
     FROM pg_enum e WHERE e.enumtypid = t.oid) AS enum_values,
    (SELECT COALESCE(jsonb_agg(jsonb_build_object('name', k.conname,
      'definition', pg_get_constraintdef(k.oid, false), 'validated', k.convalidated) ORDER BY k.conname), '[]'::jsonb)
     FROM pg_constraint k WHERE k.contypid = t.oid) AS constraints
  FROM pg_type t JOIN namespaces n ON n.oid = t.typnamespace WHERE t.typtype IN ('e', 'd')
)
SELECT jsonb_pretty(jsonb_build_object(
  'format_version', 1,
  'database', current_database(),
  'server_version', current_setting('server_version'),
  'exported_at', CURRENT_TIMESTAMP,
  'requested_schemas', (SELECT to_jsonb(names) FROM requested_schemas),
  'schemas', (SELECT COALESCE(jsonb_agg(nspname ORDER BY nspname), '[]'::jsonb) FROM namespaces),
  'table_count', (SELECT count(*) FROM tables),
  'tables', (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY schema_name, table_name), '[]'::jsonb) FROM tables t),
  'sequences', (SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY schema_name, sequence_name), '[]'::jsonb) FROM sequences s),
  'routines', (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY schema_name, name, arguments), '[]'::jsonb) FROM routines r),
  'views', (SELECT COALESCE(jsonb_agg(to_jsonb(v) ORDER BY schema_name, name), '[]'::jsonb) FROM views v),
  'custom_types', (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY schema_name, name), '[]'::jsonb) FROM custom_types t),
  'extensions', (SELECT COALESCE(jsonb_agg(jsonb_build_object('name', e.extname,
    'version', e.extversion, 'schema', n.nspname) ORDER BY e.extname), '[]'::jsonb)
    FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace),
  'incoming_foreign_keys', (SELECT COALESCE(jsonb_agg(jsonb_build_object(
    'schema', n.nspname, 'table', c.relname, 'name', k.conname,
    'definition', pg_get_constraintdef(k.oid, false)) ORDER BY n.nspname, c.relname, k.conname), '[]'::jsonb)
    FROM pg_constraint k JOIN pg_class c ON c.oid = k.conrelid
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE k.contype = 'f' AND k.confrelid IN (SELECT oid FROM relations))
)) AS schema_report;

COMMIT;
