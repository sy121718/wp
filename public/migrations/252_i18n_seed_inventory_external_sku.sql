-- 252 · 仓库 SKU 外部编码（迁移 251）的词条：7 条库存域业务错误 + 10 个模板文案位。
--
-- 与本项目所有 enums 常量一致：常量值就是 i18n key，真文案在这张表里。缺词条的后果是页面上
-- 原样显示 ErrExternalSKUProductConflict 这种裸 key（既不中文也不是话），所以新增常量必须同批 seed。
--
-- 这一批的错误全部是**可行动的**：
--   · ErrExternalSKUProductConflict —— N:1 弱校验的拒绝理由（同一仓同一外码只能属于同一个商品）；
--   · ErrWarehouseSKUNotFound / ErrWarehouseSKURequired —— 「从仓库选」的选品指路；
--   · ErrWarehouseSKUBundleNotAllowed —— 捆绑商品不存在于仓库，这个入口对它不成立；
--   · ErrWarehouseSKUCodeTaken —— 仓内 SKU 唯一（迁移 244 的 UNIQUE (warehouse_id, sku_code)）；
--   · ErrExternalSKUInvalid / ErrSKUSourceInvalid —— 入参形状问题。
--
-- 模板文案位：新建商品抽屉的「SKU 来源（自己创建 / 从仓库选）」整块（8 个 key，products.html）
-- 与库存管理页「该 SKU 的各仓库存」多出的「外部编码」列（1 个 key，inventory.html）。
-- 中文站点有模板兜底看起来正常，英文站点会退回中文兜底 —— 真源是本表，所以一并补上。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrExternalSKUInvalid', 'zh-CN', '外部编码不合法：长度需在 128 个字符以内且不含控制字符', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrExternalSKUInvalid', 'en-US', 'Invalid external code: at most 128 characters and no control characters', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrExternalSKUProductConflict', 'zh-CN', '该外部编码在本仓已挂在另一个商品上：同一个外部编码在同一仓库内只能属于同一个商品（同一商品的多个口味可以共用一个外部编码）', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrExternalSKUProductConflict', 'en-US', 'This external code is already mapped to another product in this warehouse: one external code may only belong to one product per warehouse (variants of the same product may share it)', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrWarehouseSKUNotFound', 'zh-CN', '该仓库里没有这条仓库 SKU：请确认仓库选对了，或先在该仓建好这条货的库存记录', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrWarehouseSKUNotFound', 'en-US', 'No such warehouse SKU in this warehouse: check the warehouse, or create its stock row first', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrWarehouseSKURequired', 'zh-CN', '从仓库选 SKU 时必须先选仓库并指明该仓的那条仓库 SKU', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrWarehouseSKURequired', 'en-US', 'Picking an SKU from a warehouse requires both a warehouse and one of its warehouse SKUs', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrWarehouseSKUBundleNotAllowed', 'zh-CN', '捆绑商品不存在于仓库，不能用「从仓库选」建主体 SKU，请改用自己的编码', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrWarehouseSKUBundleNotAllowed', 'en-US', 'Bundles do not exist in a warehouse, so "pick from warehouse" cannot be used; enter your own container SKU', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrWarehouseSKUCodeTaken', 'zh-CN', '该仓已有同一条 SKU 编码的库存行（我们自己的 SKU 在仓内唯一），请改用另一个仓库或另一个仓库 SKU', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrWarehouseSKUCodeTaken', 'en-US', 'This warehouse already has a stock row with the same SKU code (our SKUs are unique per warehouse); pick another warehouse or warehouse SKU', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrSKUSourceInvalid', 'zh-CN', 'SKU 来源取值不合法：只支持「自己创建」与「从仓库选」', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrSKUSourceInvalid', 'en-US', 'Invalid SKU source: only "create your own" and "pick from warehouse" are supported', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('admin.products.skuSource.label', 'zh-CN', 'SKU 来源', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.skuSource.label', 'en-US', 'SKU source', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.skuSource.custom', 'zh-CN', '自己创建（自己填编码，选了仓库自动加仓库码前缀）', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.skuSource.custom', 'en-US', 'Create your own (you type the code; selecting a warehouse adds its code prefix)', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.skuSource.warehouse', 'zh-CN', '从仓库选（取仓库里那条货的编码）', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.skuSource.warehouse', 'en-US', 'Pick from warehouse (use the code of an existing warehouse SKU)', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.label.warehouseSku', 'zh-CN', '仓库 SKU', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.label.warehouseSku', 'en-US', 'Warehouse SKU', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.ph.warehouseSku', 'zh-CN', '选择所选仓库里的一条货', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.ph.warehouseSku', 'en-US', 'Pick one of the SKUs in the selected warehouse', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.label.externalSku', 'zh-CN', '外部编码', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.label.externalSku', 'en-US', 'External code', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.ph.externalSku', 'zh-CN', '该仓的第三方编码，留空即带入所选仓库 SKU 的编码', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.ph.externalSku', 'en-US', 'The third-party code in that warehouse; leave empty to reuse the picked warehouse SKU code', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.skuSource.warehouseHint', 'zh-CN', '主体 SKU 由「仓库短码_仓库 SKU」自动拼成，不用手填；同一仓库内一个外部编码只能属于同一个商品（同一商品的多个口味共用一个编码是允许的）。', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.skuSource.warehouseHint', 'en-US', 'The container SKU is built automatically as <warehouse code>_<warehouse SKU>; within one warehouse an external code may belong to one product only (variants of the same product may share it).', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.skuSource.previewLabel', 'zh-CN', '将要生成的主体 SKU：', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.products.skuSource.previewLabel', 'en-US', 'Container SKU to be generated: ', 200, 'product', 'internal/templates/admin/products.html', 1, now(), now()),
('admin.inventory.sku.col.externalSku', 'zh-CN', '外部编码', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now()),
('admin.inventory.sku.col.externalSku', 'en-US', 'External code', 200, 'inventory', 'internal/templates/admin/inventory.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
