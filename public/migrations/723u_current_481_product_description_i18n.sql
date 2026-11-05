-- 当前词条回补源；历史原文 481_product_description_i18n.sql 保持归档，不再执行。
-- 481 · 商品描述字段的标签文案。
--
-- 背景：products.description 一直存在（jsonb，形态 {"html": "..."}，构建期拿它出详情页正文、
--   评分拿它算内容质量），但**后台编辑页从来没有对应控件** —— 运营只能通过导入或接口写它。
--   本批补上富文本字段，这是它的标签词条。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据枚举本批自己的 1 个 key。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)

SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, d.create_time, d.update_time
FROM (VALUES
('pDesc', 'zh-CN', '商品描述', 200, 'admin', '商品编辑页：描述富文本字段', 1, now(), now()),
('pDesc', 'en-US', 'Product description', 200, 'admin', '商品编辑页：描述富文本字段', 1, now(), now())
) AS d(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
WHERE NOT EXISTS (SELECT 1 FROM (VALUES ('pDesc', 'admin.products.label.description')) AS renamed(new_key, old_key)
    JOIN sys_i18n source ON source.item_key = renamed.old_key AND source.lang = d.lang
    WHERE renamed.new_key = d.item_key)
ON CONFLICT (item_key, lang) DO NOTHING;
