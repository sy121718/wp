-- 当前词条回补源；历史原文 438_i18n_product_rating_count_header.sql 保持归档，不再执行。
-- 商品列表评分平均值与评分条数分列：条数表头须独立翻译。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)

SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, d.create_time, d.update_time
FROM (VALUES
('pRatingHeader', 'zh-CN', '评分数', 200, 'admin', '商品列表评分明细条数列', 1, now(), now()),
('pRatingHeader', 'en-US', 'Ratings', 200, 'admin', '商品列表评分明细条数列', 1, now(), now())
) AS d(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
WHERE NOT EXISTS (SELECT 1 FROM (VALUES ('pRatingHeader', 'admin.products.rating.countHeader')) AS renamed(new_key, old_key)
    JOIN sys_i18n source ON source.item_key = renamed.old_key AND source.lang = d.lang
    WHERE renamed.new_key = d.item_key)
ON CONFLICT (item_key, lang) DO NOTHING;
