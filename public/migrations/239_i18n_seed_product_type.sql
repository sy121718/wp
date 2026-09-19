-- 239 · product 模块新错误词条（商品类型 + 捆绑容器价）。
--
-- 与本项目所有 enums 常量一致：常量值就是 i18n key，真文案在这张表里。
-- 缺词条的后果是接口原样返回 key（既不中文也不是话），所以新增常量必须同批 seed。
-- 幂等：ON CONFLICT DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrProductTypeInvalid', 'zh-CN', '商品类型不合法（仅支持变体商品或捆绑商品）', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('ErrProductTypeInvalid', 'en-US', 'Invalid product type', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('ErrProductTypeImmutable', 'zh-CN', '商品类型在创建后不可更改', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('ErrProductTypeImmutable', 'en-US', 'Product type cannot be changed after creation', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('ErrBundlePriceRequired', 'zh-CN', '捆绑商品必须自定价：容器价需大于 0', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('ErrBundlePriceRequired', 'en-US', 'A bundle must carry its own price: container price must be greater than 0', 400, 'product', 'internal/module/product/enums', 1, now(), now()),
('admin.products.label.type', 'zh-CN', '商品类型', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.label.type', 'en-US', 'Product type', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.type.variant', 'zh-CN', '变体商品（本商品自己的 SKU）', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.type.variant', 'en-US', 'Variant product (its own SKUs)', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.type.bundle', 'zh-CN', '捆绑商品（组合多个变体，只有套餐价）', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.type.bundle', 'en-US', 'Bundle (composed of variants, priced as a whole)', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
