-- 商品标签批量条的「未勾选」提示：模板已在取词，词库缺中英两行。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('tagBulkNoneSel', 'zh-CN', '没有勾选任何标签，列表未改动。', 400, 'product', 'admin/product/product_tags.html: 批量条空选提示', 1, now(), now()),
('tagBulkNoneSel', 'en-US', 'No tags selected; the list is unchanged.', 400, 'product', 'admin/product/product_tags.html: 批量条空选提示', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
