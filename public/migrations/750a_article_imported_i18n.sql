-- 750 · 文章正文导入画布成功的两条结论词条。
--
-- 导入成功改为渲染结论片段（admin/content/article_imported.html），不再后端发 302：
--   · admin.article.ok.imported —— 结论句，说明草稿已创建；
--   · admin.article.edit.visualOpen —— 片段里的下一步链接文案。
-- 两条与 admin.article.ok.created / admin.article.edit.visualCreate 同一分类（admin / 200）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.article.ok.imported', 'zh-CN', '页面草稿已创建。', 200, 'admin', 'admin/content/article_imported.html: 导入结论', 1, now(), now()),
    ('admin.article.ok.imported', 'en-US', 'Page draft created.', 200, 'admin', 'admin/content/article_imported.html: 导入结论', 1, now(), now()),
    ('admin.article.edit.visualOpen', 'zh-CN', '打开可视化编辑器', 200, 'admin', 'admin/content/article_imported.html: 导入后续链接', 1, now(), now()),
    ('admin.article.edit.visualOpen', 'en-US', 'Open the visual editor', 200, 'admin', 'admin/content/article_imported.html: 导入后续链接', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
