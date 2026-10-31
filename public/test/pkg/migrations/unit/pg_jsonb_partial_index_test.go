package unit

import (
	"strings"
	"testing"

	"go_wp/public/migrations"
)

const pgIdxMigrationVersion = "068-pg-jsonb-partial-index"

type pgIdxRow struct {
	IndexName string
	IndexDef  string
	Predicate *string
}

func pgIdxOf(t *testing.T) map[string]pgIdxRow {
	t.Helper()
	db := newMigrationDB(t)
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行全部迁移失败: %v", err)
	}
	var rows []pgIdxRow
	if err := db.Raw(`SELECT c.relname AS index_name, pg_get_indexdef(i.indexrelid) AS index_def,
		pg_get_expr(i.indpred, i.indrelid) AS predicate
		FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
		WHERE i.indrelid = 'pages'::regclass AND c.relname LIKE 'idx_pages_%'
		ORDER BY c.relname`).Scan(&rows).Error; err != nil {
		t.Fatalf("查询索引失败: %v", err)
	}
	out := map[string]pgIdxRow{}
	for _, r := range rows {
		out[r.IndexName] = r
	}
	return out
}

func TestPgJSONBPartialIndexMigrationBuildsPartialIndexes(t *testing.T) {
	idx := pgIdxOf(t)

	want := []string{
		"idx_pages_blockref",
		"idx_pages_structure_header",
		"idx_pages_structure_footer",
		"idx_pages_theme_alive",
	}
	for _, name := range want {
		row, ok := idx[name]
		if !ok {
			t.Fatalf("迁移 068 未建出索引 %s（现有: %v）", name, keysOf(idx))
		}
		if row.Predicate == nil || !strings.Contains(*row.Predicate, "deleted_at IS NULL") {
			t.Fatalf("索引 %s 不是 deleted_at IS NULL 部分索引，谓词=%v", name, row.Predicate)
		}
	}

	blockDef := idx["idx_pages_blockref"].IndexDef
	if !strings.Contains(blockDef, "USING gin") || !strings.Contains(blockDef, "jsonb_path_query_array") {
		t.Fatalf("idx_pages_blockref 定义不符（应为 GIN(jsonb_path_query_array(...))）: %s", blockDef)
	}
	if !strings.Contains(blockDef, "blockId") {
		t.Fatalf("idx_pages_blockref 表达式未包含 blockId: %s", blockDef)
	}
	if !strings.Contains(idx["idx_pages_structure_header"].IndexDef, "headerBlockId") {
		t.Fatalf("idx_pages_structure_header 定义不符: %s", idx["idx_pages_structure_header"].IndexDef)
	}
	if !strings.Contains(idx["idx_pages_structure_footer"].IndexDef, "footerBlockId") {
		t.Fatalf("idx_pages_structure_footer 定义不符: %s", idx["idx_pages_structure_footer"].IndexDef)
	}

	themeDef := idx["idx_pages_theme_alive"].IndexDef
	if !strings.Contains(themeDef, "theme_id") || !strings.Contains(themeDef, "update_time DESC") {
		t.Fatalf("idx_pages_theme_alive 定义不符: %s", themeDef)
	}
}

func TestPgJSONBPartialIndexMigrationCheckSQL(t *testing.T) {
	var target *migrations.Migration
	for i := range migrations.All() {
		if migrations.All()[i].Version == pgIdxMigrationVersion {
			m := migrations.All()[i]
			target = &m
			break
		}
	}
	if target == nil {
		t.Fatalf("迁移 %s 未注册到 migrations.All()", pgIdxMigrationVersion)
	}
	if !strings.Contains(target.CheckSQL, "pg_indexes") || !strings.Contains(target.CheckSQL, "idx_pages_blockref") {
		t.Fatalf("068 的 CheckSQL 必须按索引名判定，实际: %s", target.CheckSQL)
	}

	db := newMigrationDB(t)
	if err := db.Exec(`CREATE TABLE pages (id UUID PRIMARY KEY, deleted_at TIMESTAMPTZ, theme_id UUID, updated_at TIMESTAMPTZ, draft_document JSONB NOT NULL)`).Error; err != nil {
		t.Fatalf("建 pages 表失败: %v", err)
	}
	var count int64
	if err := db.Raw(target.CheckSQL, target.TableName).Scan(&count).Error; err != nil {
		t.Fatalf("CheckSQL 执行失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("pages 存在但索引不存在时 CheckSQL 应为 0（触发执行），实际 %d", count)
	}

	if err := db.Exec(`CREATE INDEX idx_pages_blockref ON pages USING GIN ((jsonb_path_query_array(draft_document, '$.**.blockId'))) WHERE deleted_at IS NULL`).Error; err != nil {
		t.Fatalf("建索引失败: %v", err)
	}
	if err := db.Raw(target.CheckSQL, target.TableName).Scan(&count).Error; err != nil {
		t.Fatalf("CheckSQL 二次执行失败: %v", err)
	}
	if count == 0 {
		t.Fatalf("索引已存在时 CheckSQL 应 >0（跳过），实际 %d", count)
	}
}

func keysOf(m map[string]pgIdxRow) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
