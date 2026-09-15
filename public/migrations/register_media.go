package migrations

// registerMediaAndUniqueConstraints 注册「媒体域与唯一约束/权限点补全（072–079）」。
//
// 由 register.go 的 init() 按主题拆出：本文件只做注册，不含任何 SQL 文本，
// 注册项引用的 SQL 一律经 mustSQL 从嵌入的 .sql 取。
func registerMediaAndUniqueConstraints() {
	// 072：补 media 换图 / 引用来源权限点（此前从未 seed，接口对全员 403 死链）。
	registerSeed(Seed{
		Version:      "072-media-replace-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code IN ('media:replace','media:references')",
		SQL:          mustSQL("072_media_replace_permissions.sql"),
	})

	// 074 / 075：把两处「先查后写」的判断（媒体上传去重、导航路径唯一）落到
	// 数据库层唯一约束 —— 并发下两次请求都能通过检查，只有约束能真正兜住。
	// CheckSQL 用 pg_indexes 判断索引是否已存在（默认的「表是否存在」对加索引无意义）。
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

	// 076：按工程修正 061 回填的语言（原实现取全局第一个工程的 defaultLang，多工程库全被标成同一语言）。
	// TableName 故意用不存在的名字：默认 CheckSQL（表是否存在）恒为 0，等价于「每次启动都跑一遍」。
	// 该 UPDATE 幂等且带 IS DISTINCT FROM 条件，无差异时零行更新。
	register(Migration{
		Version:   "076-lang-backfill-per-project",
		TableName: "page_artifacts_lang_backfill_always",
		SQL:       mustSQL("076_lang_backfill_per_project.sql"),
	})

	// 077：灾难恢复接口权限点（产物重建 + 激活面巡检）。
	registerSeed(Seed{
		Version:      "077-recovery-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code IN ('page:artifact_rebuild','page:publication_audit')",
		SQL:          mustSQL("077_recovery_permissions.sql"),
	})

	// 078：产物回收接口权限点。
	registerSeed(Seed{
		Version:      "078-artifact-gc-permissions",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'page:artifact_gc'",
		SQL:          mustSQL("078_artifact_gc_permissions.sql"),
	})

	// 079：内容集合元数据接口权限点（工作台集合字段下拉 + 内置组件字段校验）。
	registerSeed(Seed{
		Version:      "079-content-collections-permission",
		TableName:    "sys_permission",
		ConditionSQL: "SELECT COUNT(*) FROM sys_permission WHERE permission_code = 'content:collections'",
		SQL:          mustSQL("079_content_collections_permission.sql"),
	})
}
