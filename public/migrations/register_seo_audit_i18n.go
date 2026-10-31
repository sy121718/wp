package migrations

func init() {
	registerSeed(Seed{
		Version:   "697-seo-audit-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `SELECT CASE WHEN COUNT(*) = 42 THEN 1 ELSE 0 END FROM sys_i18n
			WHERE lang IN ('zh-CN','en-US') AND item_key IN (
			'admin.seo.audit.bad_contract',
			'admin.seo.audit.detail.item',
			'admin.seo.audit.detail.title',
			'admin.seo.audit.detail.src',
			'admin.seo.audit.detail.href',
			'admin.seo.audit.issue.title_missing',
			'admin.seo.audit.issue.title_duplicate',
			'admin.seo.audit.issue.description_missing',
			'admin.seo.audit.issue.canonical_missing',
			'admin.seo.audit.issue.image_alt_missing',
			'admin.seo.audit.issue.internal_link_broken',
			'admin.seo.audit.issue.hreflang_incomplete',
			'admin.seo.audit.issue.unknown',
			'admin.seo.audit.issue.paths',
			'admin.seo.audit.issues_fmt',
			'admin.seo.audit.level.error',
			'admin.seo.audit.level.warning',
			'admin.seo.audit.level.info',
			'admin.seo.audit.level.unknown',
			'admin.seo.audit.no_artifacts',
			'admin.seo.audit.no_artifacts_desc')`,
		SQL: mustSQL("697_seo_audit_i18n.sql"),
	})
}
