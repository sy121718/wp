-- 482 · 文章 SEO 字段合并的说明文案。
--
-- 背景：文章的 seoTitle / seoDescription 已与 title / excerpt 合并（2026-09-30）——
--   编辑页不再有这两个输入框（留一个框只会逼编辑者把同一个标题抄两遍），
--   合并后的口径说明挂在 SEO 卡标题的 .help 上（admin/content/article_edit.html）。
--   本批登记这一条取词；随合并退役的旧标签词条（seoTitleLabel 等）按约定留在库里
--   不再取用，不回改历史迁移。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据枚举本批自己的 1 个 key。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.article.edit.seoMergedHint', 'zh-CN', '搜索引擎结果页显示的标题就是上面的「标题」、描述就是「摘要」—— 不用再填一遍（这两栏已合并）。这里只剩主关键词这一项。', 200, 'admin', '文章编辑页：SEO 字段合并说明', 1, now(), now()),
('admin.article.edit.seoMergedHint', 'en-US', 'The title and description shown in search results are the Title and Excerpt above — no need to fill them twice (the two fields are merged). Only the focus keyword remains here.', 200, 'admin', '文章编辑页：SEO 字段合并说明', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
