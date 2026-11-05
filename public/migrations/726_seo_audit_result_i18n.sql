-- SEO 体检结果改为完整短词条与具名参数句；旧碎片来源退役。
WITH keys(old_key, new_key, retire) AS (VALUES
    ('admin.seo.audit.issue.title_missing', 'seoMissTitle', true),
    ('admin.seo.audit.issue.title_duplicate', 'seoDupTitle', true),
    ('admin.seo.audit.issue.description_missing', 'seoMissDesc', true),
    ('admin.seo.audit.issue.canonical_missing', 'seoMissCanon', true),
    ('admin.seo.audit.issue.image_alt_missing', 'seoMissAlt', true),
    ('admin.seo.audit.issue.internal_link_broken', 'seoBadLink', true),
    ('admin.seo.audit.issue.hreflang_incomplete', 'seoHreflang', true),
    ('admin.seo.audit.issue.unknown', 'seoIssueUnknown', true),
    ('admin.seo.audit.issue.paths', 'seoIssuePaths', true),
    ('admin.seo.audit.detail.title', 'seoDetailTitle', true),
    ('admin.seo.audit.detail.src', 'seoDetailSrc', true),
    ('admin.seo.audit.detail.href', 'seoDetailHref', true),
    ('admin.seo.audit.col.level', 'seoColLevel', true),
    ('admin.seo.audit.col.path', 'seoColPath', true),
    ('admin.seo.audit.col.issue', 'seoColIssue', true),
    ('admin.seo.audit.level.error', 'seoLevelErr', true),
    ('admin.seo.audit.level.warning', 'seoLevelWarn', true),
    ('admin.seo.audit.level.info', 'seoLevelInfo', true),
    ('admin.seo.audit.level.unknown', 'seoLevelNone', true),
    ('admin.seo.audit.issues_fmt', 'seoIssues', true),
    ('admin.seo.audit.no_artifacts', 'seoNoArtifacts', true),
    ('admin.seo.audit.no_artifacts_desc', 'seoNoArtHelp', true),
    ('admin.seo.audit.clean_lead', 'seoClean', true)
), guard AS MATERIALIZED (
    SELECT CASE WHEN COUNT(*) = 0 THEN 1
        ELSE ('商品短词条冲突: ' || string_agg(k.old_key || ' -> ' || k.new_key || '/' || s.lang, ', '))::integer END AS ok
    FROM keys k
    JOIN sys_i18n s ON s.item_key = k.old_key
    JOIN sys_i18n t ON t.item_key = k.new_key AND t.lang = s.lang
    WHERE ROW(t.item_value, t.http_code, t.category, t.remark, t.status)
        IS DISTINCT FROM ROW(s.item_value, s.http_code, s.category, s.remark, s.status)
), moved AS (
    UPDATE sys_i18n s SET item_key = k.new_key
    FROM keys k, guard g
    WHERE g.ok = 1 AND k.retire AND s.item_key = k.old_key
      AND NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang)
    RETURNING s.id
), copied AS (
    INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
    SELECT k.new_key, s.lang, s.item_value, s.http_code, s.category, s.remark, s.status, s.create_time, s.update_time
    FROM keys k JOIN sys_i18n s ON s.item_key = k.old_key CROSS JOIN guard g
    WHERE g.ok = 1 AND NOT k.retire
      AND NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang)
    RETURNING id
), retired AS (
    DELETE FROM sys_i18n s USING keys k, guard g
    WHERE g.ok = 1 AND k.retire AND s.item_key = k.old_key
      AND EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang)
    RETURNING s.id
)
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, d.create_time, d.update_time
FROM (VALUES
    ('seoMissTitle', 'zh-CN', '页面没有标题', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoMissTitle', 'en-US', 'The page has no title', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoDupTitle', 'zh-CN', '多个页面使用了同一个标题', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoDupTitle', 'en-US', 'Multiple pages share the same title', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoMissDesc', 'zh-CN', '页面没有 meta description', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoMissDesc', 'en-US', 'The page has no meta description', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoMissCanon', 'zh-CN', '页面没有 canonical', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoMissCanon', 'en-US', 'The page has no canonical URL', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoMissAlt', 'zh-CN', '图片缺少替代文本', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoMissAlt', 'en-US', 'An image is missing alt text', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoBadLink', 'zh-CN', '站内链接指向不存在的路径', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoBadLink', 'en-US', 'An internal link points to a path that does not exist', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoHreflang', 'zh-CN', 'hreflang 只有一条，缺少互指', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoHreflang', 'en-US', 'Only one hreflang is declared, with no cross-reference', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoIssueUnknown', 'zh-CN', '未识别的检查项（后端返回了界面尚未覆盖的标识）', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoIssueUnknown', 'en-US', 'Unrecognized check (the backend returned an identifier this page does not cover yet)', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoIssuePaths', 'zh-CN', '受影响的路径', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoIssuePaths', 'en-US', 'Affected paths', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoDetailTitle', 'zh-CN', '标题：', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoDetailTitle', 'en-US', 'Title: ', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoDetailSrc', 'zh-CN', '图片：', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoDetailSrc', 'en-US', 'Image: ', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoDetailHref', 'zh-CN', '链接：', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoDetailHref', 'en-US', 'Link: ', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoColLevel', 'zh-CN', '级别', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('seoColLevel', 'en-US', 'Severity', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('seoColPath', 'zh-CN', '路径', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('seoColPath', 'en-US', 'Path', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('seoColIssue', 'zh-CN', '问题', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('seoColIssue', 'en-US', 'Issue', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('seoLevelErr', 'zh-CN', '严重', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoLevelErr', 'en-US', 'Error', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoLevelWarn', 'zh-CN', '警告', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoLevelWarn', 'en-US', 'Warning', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoLevelInfo', 'zh-CN', '提示', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoLevelInfo', 'en-US', 'Info', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoLevelNone', 'zh-CN', '未标注级别', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoLevelNone', 'en-US', 'Level not provided', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoIssues', 'zh-CN', '已扫描 {scanned} 个产物，发现 {count} 个问题。', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoIssues', 'en-US', 'Scanned {scanned} artifacts and found {count} issues.', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoNoArtifacts', 'zh-CN', '当前站点还没有可检查的产物。', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoNoArtifacts', 'en-US', 'This site has no artifacts to check yet.', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoNoArtHelp', 'zh-CN', '发布页面后再运行体检；没有产物时体检不会给出结论。', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoNoArtHelp', 'en-US', 'Publish a page and run the audit again; without artifacts the audit draws no conclusion.', 0, NULL, NULL, 1, NULL::timestamptz, NULL::timestamptz),
    ('seoIssues', 'zh-CN', '已扫描 {scanned} 个产物，发现 {count} 个问题。', 200, 'admin', 'admin/analytics/seo_audit_result.html', 1, now(), now()),
    ('seoIssues', 'en-US', 'Scanned {scanned} artifacts and found {count} issues.', 200, 'admin', 'admin/analytics/seo_audit_result.html', 1, now(), now()),
    ('seoClean', 'zh-CN', '已扫描 {scanned} 个产物，未发现 SEO 问题。', 200, 'admin', 'admin/analytics/seo_audit_result.html', 1, now(), now()),
    ('seoClean', 'en-US', 'Scanned {scanned} artifacts; no SEO issues found.', 200, 'admin', 'admin/analytics/seo_audit_result.html', 1, now(), now())
) AS d(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
LEFT JOIN keys k ON k.new_key = d.item_key CROSS JOIN guard g
WHERE g.ok = 1
  AND NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = d.item_key AND t.lang = d.lang)
  AND NOT EXISTS (SELECT 1 FROM sys_i18n s WHERE s.item_key = k.old_key AND s.lang = d.lang);
