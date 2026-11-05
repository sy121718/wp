-- 当前词条回补源；历史原文 696_product_mvc_facts_i18n.sql 保持归档，不再执行。
-- 商品 JSON 评分覆盖范围与详情模板错误事实。
INSERT INTO sys_i18n (item_key, lang, item_value) 
SELECT d.item_key, d.lang, d.item_value
FROM (VALUES
('admin.seo.profile.landing', 'zh-CN', '落地页'),
('admin.seo.profile.landing', 'en-US', 'Landing page'),
('admin.seo.profile.guide', 'zh-CN', '长文指南'),
('admin.seo.profile.guide', 'en-US', 'Long-form guide'),
('admin.seo.coverage.incomplete', 'zh-CN', '重复标题检查未覆盖以下来源：'),
('admin.seo.coverage.incomplete', 'en-US', 'Duplicate-title checks do not cover:'),
('pTplNameReq', 'zh-CN', '模板名不能为空'),
('pTplNameReq', 'en-US', 'Template name is required'),
('pTplPathReq', 'zh-CN', '请填写新的访问路径。'),
('pTplPathReq', 'en-US', 'Enter the new URL path.'),
('pTplUnavailable', 'zh-CN', '详情页模板能力尚未就绪，请稍后重试。'),
('pTplUnavailable', 'en-US', 'Product detail templates are unavailable. Please try again later.'),
('pTplPreviewNil', 'zh-CN', '预览未生成有效内容，请检查模板后重试。'),
('pTplPreviewNil', 'en-US', 'The preview produced no content. Check the template and try again.')
) AS d(item_key, lang, item_value)
WHERE NOT EXISTS (SELECT 1 FROM (VALUES ('pTplNameReq', 'product.err.detailTemplateNameRequired'), ('pTplPathReq', 'product.err.detailTemplatePathRequired'), ('pTplPreviewNil', 'product.err.detailTemplatePreviewEmpty'), ('pTplUnavailable', 'product.err.detailTemplateUnavailable')) AS renamed(new_key, old_key)
    JOIN sys_i18n source ON source.item_key = renamed.old_key AND source.lang = d.lang
    WHERE renamed.new_key = d.item_key)
ON CONFLICT (item_key, lang) DO NOTHING;
