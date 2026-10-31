package migrations

import "sync"

func registerBuildJobIntent() {
	registerBuildJobIntentOnce.Do(registerBuildJobIntentSQL)
}

var registerBuildJobIntentOnce sync.Once

func registerBuildJobIntentSQL() {

	register(Migration{
		Version:   "307-build-jobs-intent",
		TableName: "build_jobs",
		CheckSQL: "SELECT CASE WHEN COUNT(*) = 5 THEN 1 ELSE 0 END " +
			"FROM pg_index x " +
			"JOIN pg_class c ON c.oid = x.indrelid " +
			"JOIN pg_namespace n ON n.oid = c.relnamespace " +
			"JOIN pg_class ic ON ic.oid = x.indexrelid " +
			"JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = ANY (x.indkey) " +
			"WHERE n.nspname = current_schema() AND c.relname = ? " +
			"AND ic.relname = 'uq_build_jobs_pending' " +
			"AND x.indisunique " +
			"AND x.indpred IS NOT NULL " +
			"AND a.attname IN ('source_type', 'source_id', 'build_input_hash', 'lang', 'intent')",
		SQL: mustSQL("307_build_jobs_intent.sql"),
	})
}
