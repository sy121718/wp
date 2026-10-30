-- 产物 SEO 体检结果区的 21 个词条（四档状态、结论行模板、级别标签、明细前缀）。
--
-- 背景：seo.html 的结果区改由 ui/seo-audit.js 按 service 的**事实**渲染后，
-- 页面新增了 21 个 key 只有中文兜底、没有 seed —— 换到 en-US 时这些位置会回落成中文。
-- 217 已 seed 过该页早先的 10 个 admin.seo.audit.* 词条，本批只补差集。
--
-- 与 217 的差异：217 用了三组拼接词条（issues_lead/mid/tail）表达「已扫描 N 个产物」，
-- 那套在结果区重写后已无消费者；本批用单条带占位符的 issues_fmt（{scanned}/{count}），
-- 占位符由 JS 替换，中英文各自成句，不再依赖词条首尾空格对齐。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。

INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
-- 契约不符时的重试提示（data-seo-audit-message 与 alert 共用同一 key）
('admin.seo.audit.bad_contract', 'zh-CN', '体检结果格式异常，本次未更新结论，请重试。'),
('admin.seo.audit.bad_contract', 'en-US', 'The audit result had an unexpected format. The previous result was kept — please try again.'),
-- 明细前缀：结论里带参数时才出现
('admin.seo.audit.detail.item', 'zh-CN', '检查项标识：'),
('admin.seo.audit.detail.item', 'en-US', 'Check identifier: '),
('admin.seo.audit.detail.title', 'zh-CN', '标题：'),
('admin.seo.audit.detail.title', 'en-US', 'Title: '),
('admin.seo.audit.detail.src', 'zh-CN', '图片：'),
('admin.seo.audit.detail.src', 'en-US', 'Image: '),
('admin.seo.audit.detail.href', 'zh-CN', '链接：'),
('admin.seo.audit.detail.href', 'en-US', 'Link: '),
-- 检查项标识（service 的稳定常量，改名等于丢历史）
('admin.seo.audit.issue.title_missing', 'zh-CN', '页面没有标题'),
('admin.seo.audit.issue.title_missing', 'en-US', 'The page has no title'),
('admin.seo.audit.issue.title_duplicate', 'zh-CN', '多个页面使用了同一个标题'),
('admin.seo.audit.issue.title_duplicate', 'en-US', 'Multiple pages share the same title'),
('admin.seo.audit.issue.description_missing', 'zh-CN', '页面没有 meta description'),
('admin.seo.audit.issue.description_missing', 'en-US', 'The page has no meta description'),
('admin.seo.audit.issue.canonical_missing', 'zh-CN', '页面没有 canonical'),
('admin.seo.audit.issue.canonical_missing', 'en-US', 'The page has no canonical URL'),
('admin.seo.audit.issue.image_alt_missing', 'zh-CN', '图片缺少替代文本'),
('admin.seo.audit.issue.image_alt_missing', 'en-US', 'An image is missing alt text'),
('admin.seo.audit.issue.internal_link_broken', 'zh-CN', '站内链接指向不存在的路径'),
('admin.seo.audit.issue.internal_link_broken', 'en-US', 'An internal link points to a path that does not exist'),
('admin.seo.audit.issue.hreflang_incomplete', 'zh-CN', 'hreflang 只有一条，缺少互指'),
('admin.seo.audit.issue.hreflang_incomplete', 'en-US', 'Only one hreflang is declared, with no cross-reference'),
('admin.seo.audit.issue.unknown', 'zh-CN', '未识别的检查项（后端返回了界面尚未覆盖的标识）'),
('admin.seo.audit.issue.unknown', 'en-US', 'Unrecognized check (the backend returned an identifier this page does not cover yet)'),
-- 重复标题的受影响路径标题
('admin.seo.audit.issue.paths', 'zh-CN', '受影响的路径'),
('admin.seo.audit.issue.paths', 'en-US', 'Affected paths'),
-- 有问题的总结句（占位符由 JS 填）
('admin.seo.audit.issues_fmt', 'zh-CN', '已扫描 {scanned} 个产物，发现 {count} 个问题。'),
('admin.seo.audit.issues_fmt', 'en-US', 'Scanned {scanned} artifacts and found {count} issues.'),
-- 级别标签
('admin.seo.audit.level.error', 'zh-CN', '严重'),
('admin.seo.audit.level.error', 'en-US', 'Error'),
('admin.seo.audit.level.warning', 'zh-CN', '警告'),
('admin.seo.audit.level.warning', 'en-US', 'Warning'),
('admin.seo.audit.level.info', 'zh-CN', '提示'),
('admin.seo.audit.level.info', 'en-US', 'Info'),
('admin.seo.audit.level.unknown', 'zh-CN', '未标注级别'),
('admin.seo.audit.level.unknown', 'en-US', 'Level not provided'),
-- 没有产物（与「没有发现问题」刻意分档）
('admin.seo.audit.no_artifacts', 'zh-CN', '当前站点还没有可检查的产物。'),
('admin.seo.audit.no_artifacts', 'en-US', 'This site has no artifacts to check yet.'),
('admin.seo.audit.no_artifacts_desc', 'zh-CN', '发布页面后再运行体检；没有产物时体检不会给出结论。'),
('admin.seo.audit.no_artifacts_desc', 'en-US', 'Publish a page and run the audit again; without artifacts the audit draws no conclusion.')
ON CONFLICT (item_key, lang) DO NOTHING;
