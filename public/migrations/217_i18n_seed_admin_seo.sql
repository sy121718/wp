-- 217 · SEO 控制台模板文案词条（审计 I18N-001 的补漏；页面由 436d20b 引入时未随批 key 化）。
--
-- 背景：436d20b 把 internal/templates/admin/seo.html 与 scripts/check-i18n-coverage.sh、
-- scripts/i18n-coverage-baseline.txt（基线 18）一起提交，但页面里的中文没有 key 化 ——
-- 于是这道门禁从落地第一天起就是红的（基线 18、实际 48），静态检查形同虚设。
-- 本批把该页 30 行中文抽成 38 个词条（`admin.seo.*`），门禁回到基线。
--
-- 模板侧写法：{{ .["t"]("admin.seo.<语义>", "中文原文兜底") }}。
-- JS 里的文案走 data-msg-* 属性（与 login.html 同一手法）：门禁脚本只剔 Jet 与 HTML 注释，
-- JS 里的中文行（哪怕是注释）都会被计成未 key 化，所以该页的说明用 {* *} 写。
--
-- 三组拼接文案（已扫描 N 个产物…）按中英各自断句拆成 lead/mid/tail：中文靠词条自带的
-- 首尾空格对齐量词，英文靠它对齐名词 —— I18N-001 那批的同类词条也是这个处理
-- （谁要批量 trim 词条值，英文页面会出现 Scanned12artifacts 这类粘连）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.seo.eyebrow.visibility', 'zh-CN', '站点可见性', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.eyebrow.visibility', 'en-US', 'Site visibility', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.title', 'zh-CN', 'SEO 控制台', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.title', 'en-US', 'SEO console', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.subtitle', 'zh-CN', '查看当前站点的产物体检入口、热门路径与能力接入状态。', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.subtitle', 'en-US', 'Review the artifact audit entry, top paths, and capability status for this site.', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.label.project', 'zh-CN', '站点', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.label.project', 'en-US', 'Site', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.action.view', 'zh-CN', '查看', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.action.view', 'en-US', 'View', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.err.project_list', 'zh-CN', '站点列表暂时读取失败。', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.err.project_list', 'en-US', 'The site list could not be loaded.', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.eyebrow.issues', 'zh-CN', '当前问题', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.eyebrow.issues', 'en-US', 'Current issues', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.title', 'zh-CN', '产物 SEO 体检', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.title', 'en-US', 'Artifact SEO audit', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.intro', 'zh-CN', '体检读取已激活的 HTML 产物，检查标题、描述、canonical、图片替代文本、内部链接与 hreflang。', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.intro', 'en-US', 'The audit reads the activated HTML artifacts and checks titles, descriptions, canonical, image alt text, internal links, and hreflang.', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.run', 'zh-CN', '运行当前站点体检', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.run', 'en-US', 'Run audit for this site', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.placeholder', 'zh-CN', '运行体检后，此处显示当前产物中的 SEO 问题。', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.placeholder', 'en-US', 'Run the audit to see SEO issues in the current artifacts.', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.no_permission', 'zh-CN', '需要 SEO 体检权限才能运行检查。', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.no_permission', 'en-US', 'SEO audit permission is required to run this check.', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.paths.eyebrow', 'zh-CN', '近 30 天', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.paths.eyebrow', 'en-US', 'Last 30 days', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.paths.title', 'zh-CN', '热门路径', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.paths.title', 'en-US', 'Top paths', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.paths.error', 'zh-CN', '热门路径暂时读取失败。', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.paths.error', 'en-US', 'Top paths could not be loaded.', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.paths.col.path', 'zh-CN', '路径', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.paths.col.path', 'en-US', 'Path', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.paths.col.views', 'zh-CN', '浏览', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.paths.col.views', 'en-US', 'Views', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.paths.col.visitors', 'zh-CN', '访客', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.paths.col.visitors', 'en-US', 'Visitors', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.paths.empty', 'zh-CN', '当前站点还没有可展示的访问数据。', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.paths.empty', 'en-US', 'This site has no visit data to show yet.', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.sources.eyebrow', 'zh-CN', '流量发现', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.sources.eyebrow', 'en-US', 'Traffic discovery', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.sources.title', 'zh-CN', '热门来源', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.sources.title', 'en-US', 'Top sources', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.sources.unavailable', 'zh-CN', '来源统计当前不可用：analytics 已采集来源域，但只读聚合契约尚未提供来源排行。', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.sources.unavailable', 'en-US', 'Source stats are unavailable: analytics collects source domains, but the read-only aggregation contract does not expose a source ranking yet.', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.site_files.eyebrow', 'zh-CN', '站点文件', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.site_files.eyebrow', 'en-US', 'Site files', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.site_files.title', 'zh-CN', 'Sitemap / Feed 状态', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.site_files.title', 'en-US', 'Sitemap / feed status', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.site_files.unavailable', 'zh-CN', 'sitemap / feed 实时状态当前不可用：publication 尚未提供只读状态契约。', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.site_files.unavailable', 'en-US', 'Live sitemap / feed status is unavailable: publication does not expose a read-only status contract yet.', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.external.eyebrow', 'zh-CN', '搜索平台', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.external.eyebrow', 'en-US', 'Search platforms', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.external.title', 'zh-CN', '外部搜索数据', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.external.title', 'en-US', 'External search data', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.external.unavailable', 'zh-CN', '外部搜索平台数据当前不可用：未配置 Search Console、Bing Webmaster Tools 等外部 API。', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.external.unavailable', 'en-US', 'External search platform data is unavailable: no external API (Search Console, Bing Webmaster Tools, …) is configured.', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.loading', 'zh-CN', '正在读取当前已激活产物…', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.loading', 'en-US', 'Loading the activated artifacts…', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.request_failed', 'zh-CN', '体检请求失败', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.request_failed', 'en-US', 'Audit request failed', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.clean_lead', 'zh-CN', '已扫描 ', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.clean_lead', 'en-US', 'Scanned ', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.clean_tail', 'zh-CN', ' 个产物，未发现 SEO 问题。', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.clean_tail', 'en-US', ' artifacts, no SEO issues found.', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.issues_lead', 'zh-CN', '已扫描 ', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.issues_lead', 'en-US', 'Scanned ', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.issues_mid', 'zh-CN', ' 个产物，发现 ', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.issues_mid', 'en-US', ' artifacts, ', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.issues_tail', 'zh-CN', ' 个问题。', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.issues_tail', 'en-US', ' issues found.', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.col.level', 'zh-CN', '级别', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.col.level', 'en-US', 'Severity', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.col.path', 'zh-CN', '路径', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.col.path', 'en-US', 'Path', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.col.issue', 'zh-CN', '问题', 200, 'admin', 'admin/seo.html', 1, now(), now()),
    ('admin.seo.audit.col.issue', 'en-US', 'Issue', 200, 'admin', 'admin/seo.html', 1, now(), now())

ON CONFLICT (item_key, lang) DO NOTHING;
