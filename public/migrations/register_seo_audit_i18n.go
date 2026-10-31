package migrations

// 697：产物 SEO 体检结果区的 21 个词条（见 697_seo_audit_i18n.sql）。
//
// 门槛逐条枚举本批的 42 行（21 个 key × 2 语言），不写 LIKE、也不比全库总量：
// 「表里有多少行」与「本批这几条在不在」是两件事，后者才是这条 seed 的语义。
// 21 这个数来自 seo.html 实际使用的 admin.seo.audit.* key 与 217 已 seed 的差集，
// 不是估计值 —— 页面加 key 就要在这里同步加一条。
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
