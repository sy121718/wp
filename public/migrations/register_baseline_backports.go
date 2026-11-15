package migrations

// 三条补建迁移：历史迁移（068 / 111 / 171）的 CheckSQL 只判部分对象，且
// public/CLAUDE.md 要求历史 SQL 不改，所以缺失对象由现行执行源补齐 ——
// 与 447 → 758b 同一手法。每条只做一件事，CheckSQL 覆盖自己建的全部对象。
func registerBaselineBackports() {
	register(Migration{
		Version:   "762-pages-structure-indexes",
		TableName: "pages",
		CheckSQL:  coverObjects("idx_pages_theme_alive", "idx_pages_structure_header", "idx_pages_structure_footer"),
		SQL:       mustSQL("762_pages_structure_indexes.sql"),
	})

	register(Migration{
		Version:   "763-master-data-changes-time-indexes",
		TableName: "master_data_changes",
		CheckSQL:  coverObjects("idx_master_data_changes_entity", "idx_master_data_changes_time"),
		SQL:       mustSQL("763_master_data_changes_time_indexes.sql"),
	})

	register(Migration{
		Version:   "764-build-jobs-claim-index",
		TableName: "build_jobs",
		CheckSQL:  coverObjects("idx_build_jobs_claim"),
		SQL:       mustSQL("764_build_jobs_claim_index.sql"),
	})
}
