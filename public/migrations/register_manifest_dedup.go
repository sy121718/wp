package migrations

import "sync"

func registerManifestDedup() {
	registerManifestDedupOnce.Do(registerManifestDedupSQL)
}

var registerManifestDedupOnce sync.Once

func registerManifestDedupSQL() {

	register(Migration{
		Version:   "305-manifest-dedup",
		TableName: "presentation_artifacts",
		CheckSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM information_schema.columns " +
			" WHERE table_schema = current_schema() AND table_name = ? " +
			" AND column_name = 'build_input_manifest') = 0 " +
			"AND (SELECT COUNT(*) FROM information_schema.columns " +
			" WHERE table_schema = current_schema() AND table_name = 'page_artifacts' " +
			" AND column_name = 'build_input_manifest' AND is_nullable = 'NO') = 0 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("305_manifest_dedup.sql"),
	})
}
