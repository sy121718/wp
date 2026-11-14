-- 755 · 内容模板列表页的提示词条。
--
-- 三处提示从「控制器拼成品句」改为「模板按 key 取词」：
--   · 引用面能力未装配 / 部分文档无法解析；
--   · 样例实体选取失败（缺少商品/文章、模块未装配、实体类型不支持）。
--
-- key 按根规则：首字符字母、仅 ASCII 字母数字、1–15 字符、不含点。
-- 幂等：只补缺失的 (item_key, lang)，不覆盖运营已修改的译文。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('ctImpUnavail', 'zh-CN', '引用反查能力未装配：本页无法列出引用这套模板的页面与实例，删除前请人工确认。', 200, 'admin', 'admin/contenttemplate: 引用面不可用', 1, now(), now()),
    ('ctImpUnavail', 'en-US', 'Reference lookup is not wired up: this page cannot list the pages and instances that use this template; please confirm manually before deleting.', 200, 'admin', 'admin/contenttemplate: 引用面不可用', 1, now(), now()),
    ('ctImpUnpars', 'zh-CN', '有 %s 份文档无法解析，影响面可能不完整（这些文档仍可能引用本模板）。', 200, 'admin', 'admin/contenttemplate: 引用面可能不完整', 1, now(), now()),
    ('ctImpUnpars', 'en-US', '%s document(s) could not be parsed, so the impact may be incomplete (they may still reference this template).', 200, 'admin', 'admin/contenttemplate: 引用面可能不完整', 1, now(), now()),
    ('ctHintProdMod', 'zh-CN', '商品模块未装配，无法自动选取预览样例', 200, 'admin', 'admin/contenttemplate: 样例实体提示', 1, now(), now()),
    ('ctHintProdMod', 'en-US', 'The product module is not wired up, so a preview sample cannot be picked automatically.', 200, 'admin', 'admin/contenttemplate: 样例实体提示', 1, now(), now()),
    ('ctHintCntMod', 'zh-CN', '内容模块未装配，无法自动选取预览样例', 200, 'admin', 'admin/contenttemplate: 样例实体提示', 1, now(), now()),
    ('ctHintCntMod', 'en-US', 'The content module is not wired up, so a preview sample cannot be picked automatically.', 200, 'admin', 'admin/contenttemplate: 样例实体提示', 1, now(), now()),
    ('ctHintNoProd', 'zh-CN', '工程内还没有商品，无法预览商品详情模板', 200, 'admin', 'admin/contenttemplate: 样例实体提示', 1, now(), now()),
    ('ctHintNoProd', 'en-US', 'This project has no products yet, so the product detail template cannot be previewed.', 200, 'admin', 'admin/contenttemplate: 样例实体提示', 1, now(), now()),
    ('ctHintNoArt', 'zh-CN', '工程内还没有文章，无法预览文章详情模板', 200, 'admin', 'admin/contenttemplate: 样例实体提示', 1, now(), now()),
    ('ctHintNoArt', 'en-US', 'This project has no articles yet, so the article detail template cannot be previewed.', 200, 'admin', 'admin/contenttemplate: 样例实体提示', 1, now(), now()),
    ('ctHintEntMiss', 'zh-CN', '暂不支持该实体类型的预览样例自动选取（本页只支持商品与文章）', 200, 'admin', 'admin/contenttemplate: 样例实体提示', 1, now(), now()),
    ('ctHintEntMiss', 'en-US', 'Automatic preview samples are not supported for this entity type (only products and articles are supported on this page).', 200, 'admin', 'admin/contenttemplate: 样例实体提示', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
