-- Shared placeholder for the sole bundle member SKU selector.
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.product_bundle.option.none', 'zh-CN', '— 不选 —', 200, 'admin', 'admin/product/product_bundle_form.html: SKU selector placeholder', 1, now(), now()),
('admin.product_bundle.option.none', 'en-US', '— None —', 200, 'admin', 'admin/product/product_bundle_form.html: SKU selector placeholder', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
