package migrations

func init() {
	register(Migration{
		Version: "691-page-artifacts-immutable", TableName: "page_artifacts",
		CheckSQL: `SELECT CASE WHEN EXISTS (SELECT 1 FROM pg_index WHERE indexrelid = to_regclass('uk_page_artifacts_page_hash') AND indisunique AND indisvalid) AND to_regclass('uk_page_artifacts_page_version_lang') IS NULL AND to_regclass('idx_page_artifacts_page_hash') IS NULL THEN 1 ELSE 0 END`,
		SQL:      mustSQL("691_page_artifacts_immutable.sql"),
	})
}
