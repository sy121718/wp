-- 481 · 商品描述字段的标签文案。
--
-- 背景：products.description 一直存在（jsonb，形态 {"html": "..."}，构建期拿它出详情页正文、
--   评分拿它算内容质量），但**后台编辑页从来没有对应控件** —— 运营只能通过导入或接口写它。
--   本批补上富文本字段，这是它的标签词条。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据枚举本批自己的 1 个 key。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.products.label.description', 'zh-CN', '商品描述', 200, 'admin', '商品编辑页：描述富文本字段', 1, now(), now()),
('admin.products.label.description', 'en-US', 'Product description', 200, 'admin', '商品编辑页：描述富文本字段', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
