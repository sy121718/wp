package migrations

func registerMediaAndUniqueConstraints() {

	registerSeed(Seed{
		Version:      "072-media-replace-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code IN ('media:replace','media:references')",
		SQL:          mustSQL("072_media_replace_permissions.sql"),
	})

	register(Migration{
		Version:   "074-attachment-md5-unique",
		TableName: "uq_attachment_md5_type_active",
		CheckSQL:  "SELECT COUNT(*) FROM pg_indexes WHERE indexname = ?",
		SQL:       mustSQL("074_attachment_md5_unique.sql"),
	})
	register(Migration{
		Version:   "075-navigation-path-unique",
		TableName: "uq_navigation_project_kind_path",
		CheckSQL:  "SELECT COUNT(*) FROM pg_indexes WHERE indexname = ?",
		SQL:       mustSQL("075_navigation_path_unique.sql"),
	})

	registerSeed(Seed{
		Version:      "077-recovery-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code IN ('page:artifact_rebuild','page:publication_audit')",
		SQL:          mustSQL("077_recovery_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "078-artifact-gc-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'page:artifact_gc'",
		SQL:          mustSQL("078_artifact_gc_permissions.sql"),
	})

	registerSeed(Seed{
		Version:      "079-content-collections-permission",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'content:collections'",
		SQL:          mustSQL("079_content_collections_permission.sql"),
	})
}
