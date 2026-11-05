-- 当前词条回补源；历史原文 479_product_default_image_i18n.sql 保持归档，不再执行。
-- 479 · 商品主图字段的文案。
--
-- 背景：products.default_image 一直是契约里的字段（创建/更新 DTO 都有，构建期还拿它当
--   图集为空时的兜底图），但后台编辑页从来没有对应控件 —— 运营只能靠导入或接口设它。
--   本批补上单图媒体字段，这是它的一个标签词条。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据枚举本批自己的 1 个 key。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)

SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, d.create_time, d.update_time
FROM (VALUES
('pDefImage', 'zh-CN', '主图', 200, 'admin', '商品主图字段', 1, now(), now()),
('pDefImage', 'en-US', 'Main image', 200, 'admin', '商品主图字段', 1, now(), now())
) AS d(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
WHERE NOT EXISTS (SELECT 1 FROM (VALUES ('pDefImage', 'admin.products.label.defaultImage')) AS renamed(new_key, old_key)
    JOIN sys_i18n source ON source.item_key = renamed.old_key AND source.lang = d.lang
    WHERE renamed.new_key = d.item_key)
ON CONFLICT (item_key, lang) DO NOTHING;
