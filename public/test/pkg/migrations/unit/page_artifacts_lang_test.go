package unit

import (
	"testing"

	migrations "go_wp/public/migrations"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

const langMigrationVersion = "061-page-artifacts-lang"

const oldSchemaPageArtifacts = `CREATE TABLE page_artifacts (
    id uuid PRIMARY KEY,
    page_id uuid NOT NULL,
    version bigint NOT NULL,
    artifact_key text NOT NULL,
    created_at timestamptz NOT NULL,
    UNIQUE (page_id, version),
    UNIQUE (id, page_id)
)`

func newMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
	}
	return db
}

func langMigrationSQL(t *testing.T) string {
	t.Helper()
	for _, m := range migrations.All() {
		if m.Version == langMigrationVersion {
			return m.SQL
		}
	}
	t.Fatalf("迁移 %s 未注册到 migrations.All()", langMigrationVersion)
	return ""
}

func applyLangMigration(t *testing.T, db *gorm.DB) int {
	t.Helper()
	statements := migrations.SplitStatements(langMigrationSQL(t))
	n := 0
	for _, stmt := range statements {
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

func TestPageArtifactsLangMigrationBackfillsExistingRows(t *testing.T) {
	db := newMigrationDB(t)

	if err := db.Exec(oldSchemaPageArtifacts).Error; err != nil {
		t.Fatalf("建旧 schema 失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO page_artifacts (id, page_id, version, artifact_key, created_at) VALUES
		('aaaaaaaa-0000-0000-0000-000000000001','bbbbbbbb-0000-0000-0000-000000000001',1,'artifacts/h1', now()),
		('aaaaaaaa-0000-0000-0000-000000000002','bbbbbbbb-0000-0000-0000-000000000001',2,'artifacts/h2', now())`).Error; err != nil {
		t.Fatalf("写入存量行失败: %v", err)
	}

	if n := applyLangMigration(t, db); n == 0 {
		t.Fatal("迁移 061 未产生任何语句")
	}

	var nullCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM page_artifacts WHERE lang IS NULL OR lang = ''`).Scan(&nullCount).Error; err != nil {
		t.Fatalf("统计空 lang 失败: %v", err)
	}
	if nullCount != 0 {
		t.Fatalf("存量行 lang 不应为空，实际空行 %d", nullCount)
	}
	var langs []string
	if err := db.Raw(`SELECT lang FROM page_artifacts ORDER BY version`).Scan(&langs).Error; err != nil {
		t.Fatalf("读取 lang 失败: %v", err)
	}
	if len(langs) != 2 || langs[0] != "zh-CN" || langs[1] != "zh-CN" {
		t.Fatalf("存量行应回填默认语言 zh-CN，实际 %v", langs)
	}

	var nullable, columnDefault string
	if err := db.Raw(`SELECT is_nullable, COALESCE(column_default, '') FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'page_artifacts' AND column_name = 'lang'`).
		Row().Scan(&nullable, &columnDefault); err != nil {
		t.Fatalf("读取 lang 列属性失败: %v", err)
	}
	if nullable != "NO" || columnDefault != "'zh-CN'::text" {
		t.Fatalf("lang 列应为 NOT NULL DEFAULT 'zh-CN'，实际 nullable=%s default=%s", nullable, columnDefault)
	}

	var idxCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema()
		AND tablename = 'page_artifacts' AND indexname = 'uk_page_artifacts_page_version_lang'`).Scan(&idxCount).Error; err != nil {
		t.Fatalf("查询新唯一键失败: %v", err)
	}
	if idxCount != 1 {
		t.Fatalf("应存在唯一键 uk_page_artifacts_page_version_lang，实际 %d", idxCount)
	}

	if err := db.Exec(`INSERT INTO page_artifacts (id, page_id, version, artifact_key, created_at, lang)
		VALUES ('aaaaaaaa-0000-0000-0000-000000000003','bbbbbbbb-0000-0000-0000-000000000001',1,'artifacts/h1-en', now(), 'en-US')`).Error; err != nil {
		t.Fatalf("同页多语言并存失败（旧 UNIQUE(page_id, version) 未被移除）: %v", err)
	}

	if err := db.Exec(`INSERT INTO page_artifacts (id, page_id, version, artifact_key, created_at, lang)
		VALUES ('aaaaaaaa-0000-0000-0000-000000000004','bbbbbbbb-0000-0000-0000-000000000001',1,'artifacts/h1-dup', now(), 'zh-CN')`).Error; err == nil {
		t.Fatal("同 (page_id, version, lang) 的重复插入应被唯一键拒绝")
	}
}

func TestPageArtifactsLangMigrationIdempotent(t *testing.T) {
	db := newMigrationDB(t)

	if err := db.Exec(oldSchemaPageArtifacts).Error; err != nil {
		t.Fatalf("建旧 schema 失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO page_artifacts (id, page_id, version, artifact_key, created_at)
		VALUES ('aaaaaaaa-0000-0000-0000-000000000001','bbbbbbbb-0000-0000-0000-000000000001',1,'artifacts/h1', now())`).Error; err != nil {
		t.Fatalf("写入存量行失败: %v", err)
	}

	first := applyLangMigration(t, db)

	second := applyLangMigration(t, db)
	if first == 0 || second == 0 {
		t.Fatalf("迁移语句数异常: first=%d second=%d", first, second)
	}

	var rowCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM page_artifacts`).Scan(&rowCount).Error; err != nil {
		t.Fatalf("统计行数失败: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("重复迁移不应改变行数，实际 %d", rowCount)
	}
	var lang string
	if err := db.Raw(`SELECT lang FROM page_artifacts LIMIT 1`).Scan(&lang).Error; err != nil {
		t.Fatalf("读取 lang 失败: %v", err)
	}
	if lang != "zh-CN" {
		t.Fatalf("重复迁移不应改写 lang，实际 %q", lang)
	}
	var idxCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema()
		AND tablename = 'page_artifacts' AND indexname = 'uk_page_artifacts_page_version_lang'`).Scan(&idxCount).Error; err != nil {
		t.Fatalf("查询新唯一键失败: %v", err)
	}
	if idxCount != 1 {
		t.Fatalf("重复迁移后唯一键应恰好一个，实际 %d", idxCount)
	}
}

func TestPageArtifactsLangMigrationUsesProjectDefaultLang(t *testing.T) {
	db := newMigrationDB(t)

	if err := db.Exec(`CREATE TABLE projects (
		id uuid PRIMARY KEY, name text NOT NULL, settings jsonb NOT NULL,
		created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL)`).Error; err != nil {
		t.Fatalf("建 projects 失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO projects (id, name, settings, created_at, updated_at)
		VALUES ('cccccccc-0000-0000-0000-000000000001','站点','{"defaultLang":"en-US"}', now(), now())`).Error; err != nil {
		t.Fatalf("写入 projects 失败: %v", err)
	}
	if err := db.Exec(oldSchemaPageArtifacts).Error; err != nil {
		t.Fatalf("建旧 schema 失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO page_artifacts (id, page_id, version, artifact_key, created_at)
		VALUES ('aaaaaaaa-0000-0000-0000-000000000001','bbbbbbbb-0000-0000-0000-000000000001',1,'artifacts/h1', now())`).Error; err != nil {
		t.Fatalf("写入存量行失败: %v", err)
	}

	applyLangMigration(t, db)

	var lang string
	if err := db.Raw(`SELECT lang FROM page_artifacts LIMIT 1`).Scan(&lang).Error; err != nil {
		t.Fatalf("读取 lang 失败: %v", err)
	}
	if lang != "en-US" {
		t.Fatalf("存量行应按项目设置回填 en-US，实际 %q", lang)
	}
}

func TestMigrationsRunTwiceIsIdempotent(t *testing.T) {
	db := newMigrationDB(t)

	if err := migrations.Run(db); err != nil {
		t.Fatalf("首次执行全部迁移失败: %v", err)
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("重复执行全部迁移失败（幂等性被破坏）: %v", err)
	}

	var idxCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema()
		AND tablename = 'page_artifacts' AND indexname = 'uk_page_artifacts_page_hash'`).Scan(&idxCount).Error; err != nil {
		t.Fatalf("查询新唯一键失败: %v", err)
	}
	if idxCount != 1 {
		t.Fatalf("全量迁移后应存在内容唯一键 uk_page_artifacts_page_hash，实际 %d", idxCount)
	}
}
