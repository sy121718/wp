-- 750a（现行执行版本）· 文章正文导入结论词条，直接播种规范短 key。
--
-- 历史文件 750a_article_imported_i18n.sql 保留原样、不再注册执行：它播种的是
-- 点分长 key（admin.article.ok.imported / admin.article.edit.visualOpen），
-- 完整 seed 重跑会把这些退役 key 重新补回。这里改播短 key，与 753 的修复目标一致。
--
-- 幂等：只补缺失的 (item_key, lang)，不覆盖运营已修改的译文。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('artImportOk', 'zh-CN', '页面草稿已创建。', 200, 'admin', 'admin/content/article_imported.html: 导入结论', 1, now(), now()),
    ('artImportOk', 'en-US', 'Page draft created.', 200, 'admin', 'admin/content/article_imported.html: 导入结论', 1, now(), now()),
    ('artVisualOpen', 'zh-CN', '打开可视化编辑器', 200, 'admin', 'admin/content/article_imported.html: 导入后续链接', 1, now(), now()),
    ('artVisualOpen', 'en-US', 'Open the visual editor', 200, 'admin', 'admin/content/article_imported.html: 导入后续链接', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
