package migrations

func init() {
	registerSeed(Seed{
		Version:   "726-seo-audit-result-i18n",
		TableName: "sys_i18n",
		ConditionSQL: `WITH keys(old_key, new_key, retire) AS (VALUES
    ('admin.seo.audit.issue.title_missing', 'seoAuditIssueTitleMiss', true),
    ('admin.seo.audit.issue.title_duplicate', 'seoAuditIssueTitleDup', true),
    ('admin.seo.audit.issue.description_missing', 'seoAuditIssueDescMiss', true),
    ('admin.seo.audit.issue.canonical_missing', 'seoAuditIssueCanonMiss', true),
    ('admin.seo.audit.issue.image_alt_missing', 'seoAuditIssueAltMiss', true),
    ('admin.seo.audit.issue.internal_link_broken', 'seoAuditIssueLinkBad', true),
    ('admin.seo.audit.issue.hreflang_incomplete', 'seoAuditIssueHreflang', true),
    ('admin.seo.audit.issue.unknown', 'seoAuditIssueUnknown', true),
    ('admin.seo.audit.issue.paths', 'seoAuditPaths', true),
    ('admin.seo.audit.detail.title', 'seoAuditDetailTitle', true),
    ('admin.seo.audit.detail.src', 'seoAuditDetailSrc', true),
    ('admin.seo.audit.detail.href', 'seoAuditDetailHref', true),
    ('admin.seo.audit.col.level', 'seoAuditColLevel', true),
    ('admin.seo.audit.col.path', 'seoAuditColPath', true),
    ('admin.seo.audit.col.issue', 'seoAuditColIssue', true),
    ('admin.seo.audit.level.error', 'seoAuditLevelError', true),
    ('admin.seo.audit.level.warning', 'seoAuditLevelWarning', true),
    ('admin.seo.audit.level.info', 'seoAuditLevelInfo', true),
    ('admin.seo.audit.level.unknown', 'seoAuditLevelUnknown', true),
    ('admin.seo.audit.issues_fmt', 'seoAuditIssues', true),
    ('admin.seo.audit.no_artifacts', 'seoAuditNoArtifacts', true),
    ('admin.seo.audit.no_artifacts_desc', 'seoAuditNoArtifactsDesc', true),
    ('admin.seo.audit.clean_lead', 'seoAuditClean', true)
)
SELECT CASE WHEN
 NOT EXISTS (SELECT 1 FROM keys k CROSS JOIN (VALUES ('zh-CN'), ('en-US')) AS langs(lang)
 WHERE NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = langs.lang))
 AND NOT EXISTS (SELECT 1 FROM keys k JOIN sys_i18n s ON s.item_key = k.old_key
 WHERE k.retire OR NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang))
THEN 1 ELSE 0 END`,
		SQL: mustSQL("726_seo_audit_result_i18n.sql"),
	})
}
