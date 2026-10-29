package migrations

func init() {
	register(Migration{
		Version: "692-retired-site-slots", TableName: "page_dependencies",
		CheckSQL: `SELECT CASE WHEN to_regclass('page_site_slots') IS NULL AND NOT EXISTS (SELECT 1 FROM page_dependencies WHERE dependency_kind='site_slot') AND NOT EXISTS (SELECT 1 FROM presentation_dependencies WHERE dependency_kind='site_slot') AND (SELECT COUNT(*) FROM pg_constraint WHERE conname IN ('page_dependencies_dependency_kind_check','presentation_dependencies_dependency_kind_check') AND conrelid IN (to_regclass('page_dependencies'),to_regclass('presentation_dependencies')) AND POSITION('site_slot' IN pg_get_constraintdef(oid))=0 AND POSITION('page_target' IN pg_get_constraintdef(oid))>0 AND POSITION('presentation_target' IN pg_get_constraintdef(oid))>0)=2 THEN 1 ELSE 0 END`,
		SQL:      mustSQL("692_retired_site_slots.sql"),
	})
}
