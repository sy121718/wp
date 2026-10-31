package migrations

func init() {
	register(Migration{
		Version:   "698-navigation-menu-stale-sequence",
		TableName: "build_jobs",

		CheckSQL: `SELECT CASE WHEN (
			(SELECT COUNT(*) FROM information_schema.columns c
			  WHERE c.table_schema = current_schema() AND c.table_name = 'build_jobs'
			    AND c.column_name = 'source_id' AND c.data_type = 'text') = 1
			AND (SELECT COUNT(*) FROM information_schema.columns c
			  WHERE c.table_schema = current_schema() AND c.table_name = 'build_jobs'
			    AND c.column_name = 'input_version' AND c.data_type = 'bigint') = 1
			AND (SELECT COUNT(*) FROM information_schema.columns c
			  WHERE c.table_schema = current_schema() AND c.table_name = 'build_jobs'
			    AND c.column_name = 'context_key' AND c.data_type = 'text') = 1
			AND (SELECT COUNT(DISTINCT a.attname) FROM pg_index x
			        JOIN pg_class c ON c.oid = x.indrelid
			        JOIN pg_class ic ON ic.oid = x.indexrelid
			        JOIN pg_namespace n ON n.oid = c.relnamespace
			        JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = ANY (x.indkey)
			        WHERE n.nspname = current_schema() AND c.relname = 'build_jobs'
			          AND ic.relname = 'uq_build_jobs_context_version'
			          AND x.indisunique AND x.indpred IS NOT NULL
			          AND x.indnkeyatts = 2
			          AND a.attname IN ('context_key', 'input_version')) = 2
			AND (SELECT COUNT(*) FROM pg_constraint k
			      JOIN pg_class c ON c.oid = k.conrelid
			      JOIN pg_namespace n ON n.oid = c.relnamespace
			      WHERE n.nspname = current_schema() AND c.relname = 'build_jobs'
			        AND k.conname = 'build_jobs_source_type_check'
			        AND pg_get_constraintdef(k.oid) LIKE '%menu_stale%') = 1
			AND (SELECT COUNT(*) FROM pg_constraint k
			      JOIN pg_class c ON c.oid = k.conrelid
			      JOIN pg_namespace n ON n.oid = c.relnamespace
			      WHERE n.nspname = current_schema() AND c.relname = 'build_jobs'
			        AND k.conname = 'build_jobs_menu_stale_shape_check'
			        AND k.contype = 'c'
			        AND pg_get_constraintdef(k.oid) LIKE '%menu_stale%'
			        AND pg_get_constraintdef(k.oid) LIKE '%context_key%') = 1
			AND to_regclass(current_schema() || '.build_job_seq') IS NOT NULL
			AND (SELECT COUNT(*) FROM pg_constraint k
			      JOIN pg_class c ON c.oid = k.conrelid
			      JOIN pg_namespace n ON n.oid = c.relnamespace
			      WHERE n.nspname = current_schema() AND c.relname = 'build_job_seq'
			        AND k.conname = 'ck_build_job_seq_kind' AND k.contype = 'c') = 1
			AND (SELECT COUNT(*) FROM pg_class c
			      JOIN pg_namespace n ON n.oid = c.relnamespace
			      WHERE n.nspname = current_schema() AND c.relname = 'build_job_seq'
			        AND c.relrowsecurity AND c.relforcerowsecurity) = 1
			AND (SELECT COUNT(*) FROM pg_policy p
			      JOIN pg_class c ON c.oid = p.polrelid
			      JOIN pg_namespace n ON n.oid = c.relnamespace
			      WHERE n.nspname = current_schema() AND c.relname = 'build_job_seq'
			        AND p.polname = 'project_isolation') = 1
		) THEN 1 ELSE 0 END`,
		SQL: mustSQL("698_navigation_menu_stale_sequence.sql"),
	})
}
