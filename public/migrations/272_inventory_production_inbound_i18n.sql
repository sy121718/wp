-- 272 · 采购入库页「生产入库」表单的文案词条（中英成对，11 个 key / 22 行）。
--
-- 背景（本批收口）：/admin/inventory/purchases/production 的路由、权限点
-- （inventory:purchase_production）与 handler（InventoryPurchaseProduction）一直在，
-- 但两处模板互相指向对方而**谁都没有渲染表单** —— 生产入库从 UI 上不可达
-- （inventory.html 说「生产入库在采购入库页」，inventory_purchases.html 说「已移到库存页」）。
-- 本批把表单补回采购入库页，新增的文案槽位因此必须同批 seed 中英各一行：
-- sys_i18n 是文案的真相来源，模板里的中文只是取词失败时的兜底（缺 en-US 只会让
-- 英文界面显示中文，任何断言都不会红 —— 所以按批次登记并在账本里对账）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.inventory_purchases.production.title', 'zh-CN', '生产入库（自家工厂）', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.title', 'en-US', 'Production inbound (own factory)', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.open', 'zh-CN', '生产入库', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.open', 'en-US', 'Production inbound', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.noInternal', 'zh-CN', '这个工程还没有启用中的内部货源（自家工厂 / 集团内关联公司）—— 先去货源管理把来源类型设成「内部」。生产入库只认内部货源：外部供应商走采购单。', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.noInternal', 'en-US', 'This project has no enabled internal source (own factory / affiliated company) yet — set a source type to "internal" in source management first. Production inbound only accepts internal sources; external suppliers go through a purchase order.', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.labelSource', 'zh-CN', '生产来源', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.labelSource', 'en-US', 'Production source', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.optionSource', 'zh-CN', '（选择内部货源）', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.optionSource', 'en-US', '(select an internal source)', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.labelSku', 'zh-CN', 'SKU 编码', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.labelSku', 'en-US', 'SKU code', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.optionSku', 'zh-CN', '（选择 SKU）', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.optionSku', 'en-US', '(select a SKU)', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.labelQuantity', 'zh-CN', '本次入库数量', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.labelQuantity', 'en-US', 'Quantity received', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.labelCost', 'zh-CN', '单元成本价', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.labelCost', 'en-US', 'Unit cost', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.hintCost', 'zh-CN', '自家工厂没有采购单价可引用，成本价必须手工填：它写进该 SKU 在当前仓的当前成本，同时回写商品侧成本价。', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.hintCost', 'en-US', 'An own factory has no purchase unit price to fall back on, so the cost must be typed by hand: it is written to the current cost of this SKU in this warehouse and written back to the product-side cost price.', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.submit', 'zh-CN', '登记生产入库', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now()),
    ('admin.inventory_purchases.production.submit', 'en-US', 'Record production inbound', 200, 'admin', 'admin/inventory_purchases.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
