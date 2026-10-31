package unit

import (
	"testing"

	migrations "go_wp/public/migrations"

	"gorm.io/gorm"
)

const mediaCenterMigrationVersion = "067-media-center"

func mediaCenterMigrationSQL(t *testing.T) string {
	t.Helper()
	for _, m := range migrations.All() {
		if m.Version == mediaCenterMigrationVersion {
			return m.SQL
		}
	}
	t.Fatalf("迁移 %s 未注册到 migrations.All()", mediaCenterMigrationVersion)
	return ""
}

func applyMediaCenterMigration(t *testing.T, db *gorm.DB) int {
	t.Helper()
	n := 0
	for _, stmt := range migrations.SplitStatements(mediaCenterMigrationSQL(t)) {
		if stmt == "" {
			continue
		}
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("执行迁移语句失败: %v\nSQL: %s", err, stmt)
		}
		n++
	}
	return n
}

const oldSchemaAttachmentJSON = `CREATE TABLE sys_attachment (
    id BIGSERIAL PRIMARY KEY,
    file_path text NOT NULL,
    md5 text,
    file_type text NOT NULL DEFAULT 'image',
    extra_info json,
    status smallint NOT NULL DEFAULT 1
)`

func TestMediaCenterMigrationJSONToJSONB(t *testing.T) {
	db := newMigrationDB(t)

	if err := db.Exec(oldSchemaAttachmentJSON).Error; err != nil {
		t.Fatalf("建旧 schema 失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO sys_attachment (file_path, md5, file_type, extra_info) VALUES
        ('legacy/a.png', 'd41d8cd98f00b204e9800998ecf8427e', 'image', '{"alt":"Logo","title":"品牌"}'),
        ('legacy/b.png', NULL, 'image', NULL),
        ('legacy/c.png', 'abc', 'image', '[1,2,3]')`).Error; err != nil {
		t.Fatalf("插入存量数据失败: %v", err)
	}

	applyMediaCenterMigration(t, db)

	var dataType string
	if err := db.Raw(`SELECT data_type FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'sys_attachment' AND column_name = 'extra_info'`).
		Scan(&dataType).Error; err != nil {
		t.Fatalf("查询列类型失败: %v", err)
	}
	if dataType != "jsonb" {
		t.Fatalf("extra_info 应为 jsonb，实际 %s", dataType)
	}

	var genType, genNullable string
	var genDefault *string
	if err := db.Raw(`SELECT data_type, is_nullable, column_default FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'sys_attachment' AND column_name = 'generation'`).
		Row().Scan(&genType, &genNullable, &genDefault); err != nil {
		t.Fatalf("查询 generation 列失败: %v", err)
	}
	if genType != "integer" || genNullable != "NO" {
		t.Fatalf("generation 列定义不符: type=%s nullable=%s", genType, genNullable)
	}
	var gens []int
	if err := db.Raw(`SELECT generation FROM sys_attachment ORDER BY id`).Scan(&gens).Error; err != nil {
		t.Fatalf("查询 generation 值失败: %v", err)
	}
	if len(gens) != 3 {
		t.Fatalf("存量行数不符: %d", len(gens))
	}
	for i, g := range gens {
		if g != 1 {
			t.Fatalf("存量行 generation 应回填为 1，第 %d 行=%d", i+1, g)
		}
	}

	var nullCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM sys_attachment WHERE id = 2 AND extra_info IS NULL`).Scan(&nullCount).Error; err != nil {
		t.Fatalf("查询 NULL 行失败: %v", err)
	}
	if nullCount != 1 {
		t.Fatalf("extra_info NULL 应保持 NULL")
	}

	var alt string
	if err := db.Raw(`SELECT extra_info ->> 'alt' FROM sys_attachment WHERE id = 1`).Scan(&alt).Error; err != nil {
		t.Fatalf("读取 alt 失败: %v", err)
	}
	if alt != "Logo" {
		t.Fatalf("存量 JSON 内容丢失: alt=%q", alt)
	}

	var idxCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema()
        AND tablename = 'sys_attachment' AND indexname IN ('idx_att_extra_info_gin','idx_att_md5_type')`).
		Scan(&idxCount).Error; err != nil {
		t.Fatalf("查询索引失败: %v", err)
	}
	if idxCount != 2 {
		t.Fatalf("迁移应建 2 个索引，实际 %d", idxCount)
	}

	if err := db.Exec(`UPDATE sys_attachment SET extra_info = jsonb_set(coalesce(extra_info,'{}'::jsonb), '{refs}',
        '[{"kind":"page","id":"p-1","title":"首页"}]'::jsonb, true) WHERE id = 1`).Error; err != nil {
		t.Fatalf("写入 refs 失败: %v", err)
	}
	var hitID uint64
	if err := db.Raw(`SELECT id FROM sys_attachment WHERE extra_info @> '{"refs":[{"kind":"page","id":"p-1"}]}'::jsonb`).
		Scan(&hitID).Error; err != nil {
		t.Fatalf("@> 查询失败: %v", err)
	}
	if hitID != 1 {
		t.Fatalf("@> 查询应命中 id=1，实际 %d", hitID)
	}

	applyMediaCenterMigration(t, db)
	applyMediaCenterMigration(t, db)
	var rows, genSum int64
	if err := db.Raw(`SELECT COUNT(*), COALESCE(SUM(generation),0) FROM sys_attachment`).
		Row().Scan(&rows, &genSum); err != nil {
		t.Fatalf("幂等后查询失败: %v", err)
	}
	if rows != 3 || genSum != 3 {
		t.Fatalf("重复执行后数据被破坏: rows=%d genSum=%d", rows, genSum)
	}
}

func TestMediaCenterMigrationSanitizesDirtyTextColumn(t *testing.T) {
	db := newMigrationDB(t)

	if err := db.Exec(`CREATE TABLE sys_attachment (
        id BIGSERIAL PRIMARY KEY,
        file_path text NOT NULL,
        md5 text,
        file_type text NOT NULL DEFAULT 'image',
        extra_info text,
        status smallint NOT NULL DEFAULT 1
    )`).Error; err != nil {
		t.Fatalf("建 text 列旧表失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO sys_attachment (file_path, file_type, extra_info) VALUES
        ('legacy/ok.png', 'image', '{"alt":"ok"}'),
        ('legacy/empty.png', 'image', '   '),
        ('legacy/bad.png', 'image', '{not-json'),
        ('legacy/null.png', 'image', NULL)`).Error; err != nil {
		t.Fatalf("插入脏数据失败: %v", err)
	}

	applyMediaCenterMigration(t, db)

	var dataType string
	if err := db.Raw(`SELECT data_type FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'sys_attachment' AND column_name = 'extra_info'`).
		Scan(&dataType).Error; err != nil {
		t.Fatalf("查询列类型失败: %v", err)
	}
	if dataType != "jsonb" {
		t.Fatalf("extra_info 应为 jsonb，实际 %s", dataType)
	}

	var okCount, nullCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM sys_attachment WHERE id = 1 AND extra_info ->> 'alt' = 'ok'`).Scan(&okCount).Error; err != nil {
		t.Fatalf("查询合法行失败: %v", err)
	}
	if okCount != 1 {
		t.Fatalf("合法 JSON 行应完整保留")
	}
	if err := db.Raw(`SELECT COUNT(*) FROM sys_attachment WHERE id IN (2,3,4) AND extra_info IS NULL`).Scan(&nullCount).Error; err != nil {
		t.Fatalf("查询脏数据行失败: %v", err)
	}
	if nullCount != 3 {
		t.Fatalf("空串/非法 JSON/NULL 应全部归零，实际 %d 行为 NULL", nullCount)
	}
}
