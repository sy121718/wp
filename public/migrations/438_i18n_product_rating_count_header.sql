-- 商品列表评分平均值与评分条数分列：条数表头须独立翻译。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.products.rating.countHeader', 'zh-CN', '评分数', 200, 'admin', '商品列表评分明细条数列', 1, now(), now()),
('admin.products.rating.countHeader', 'en-US', 'Ratings', 200, 'admin', '商品列表评分明细条数列', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
