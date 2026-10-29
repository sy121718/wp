-- 商品 JSON 评分覆盖范围与详情模板错误事实。
INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.seo.profile.landing', 'zh-CN', '落地页'),
('admin.seo.profile.landing', 'en-US', 'Landing page'),
('admin.seo.profile.guide', 'zh-CN', '长文指南'),
('admin.seo.profile.guide', 'en-US', 'Long-form guide'),
('admin.seo.coverage.incomplete', 'zh-CN', '重复标题检查未覆盖以下来源：'),
('admin.seo.coverage.incomplete', 'en-US', 'Duplicate-title checks do not cover:'),
('product.err.detailTemplateNameRequired', 'zh-CN', '模板名不能为空'),
('product.err.detailTemplateNameRequired', 'en-US', 'Template name is required'),
('product.err.detailTemplatePathRequired', 'zh-CN', '请填写新的访问路径。'),
('product.err.detailTemplatePathRequired', 'en-US', 'Enter the new URL path.'),
('product.err.detailTemplateUnavailable', 'zh-CN', '详情页模板能力尚未就绪，请稍后重试。'),
('product.err.detailTemplateUnavailable', 'en-US', 'Product detail templates are unavailable. Please try again later.'),
('product.err.detailTemplatePreviewEmpty', 'zh-CN', '预览未生成有效内容，请检查模板后重试。'),
('product.err.detailTemplatePreviewEmpty', 'en-US', 'The preview produced no content. Check the template and try again.')
ON CONFLICT (item_key, lang) DO NOTHING;
