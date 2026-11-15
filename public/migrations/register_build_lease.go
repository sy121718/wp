package migrations

import "sync"

func registerBuildJobLease() {
	registerBuildJobLeaseOnce.Do(registerBuildJobLeaseSQL)
}

var registerBuildJobLeaseOnce sync.Once

func registerBuildJobLeaseSQL() {

	register(Migration{
		Version:   "295-build-jobs-lease",
		TableName: "build_jobs",
		CheckSQL: allChecks(
			"SELECT CASE WHEN COUNT(*) = 6 THEN 1 ELSE 0 END "+
				"FROM information_schema.columns c "+
				"WHERE c.table_schema = current_schema() AND c.table_name = ? "+
				"AND c.column_name IN ('project_id', 'lang', 'intent', 'lease_token', 'lease_expires_time', 'attempt') "+
				"AND EXISTS (SELECT 1 FROM pg_indexes i WHERE i.schemaname = current_schema() "+
				"AND i.tablename = c.table_name AND i.indexname = 'uq_build_jobs_running_source')",
			coverObjects("uq_build_jobs_running_source", "idx_build_jobs_lease_expires"),
		),
		SQL: mustSQL("295_build_jobs_lease.sql"),
	})
}
