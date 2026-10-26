-- 645 · 主档字段长度上限的明细词条（迁移 642 / B3）。
--
-- 642 给 inventory_stocks 加了名称 / 图 / 条码 / 备注四个展示字段，service 侧按
-- 「maxStockXxxLen」限长并在超限时给出带占位的明细句（ErrDetail + i18n.ErrorDetail）：
--
--   · admin.inventory.err.stockNameLimit    名称超 200 字
--   · admin.inventory.err.stockBarcodeLimit 条码超 64 字
--   · admin.inventory.err.stockRemarkLimit  备注超 500 字
--   · admin.inventory.err.stockImageLimit   图片地址超 512 字
--
-- 四条共用一个句式（"上限 {max} 个字符"），占位符由调用点填；词条缺失时读侧回落
-- inventoryenums.ErrDetailFallbacks 里的中文兜底，但那只是兜底，不是翻译。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.inventory.err.stockNameLimit', 'zh-CN', '上限 {max} 个字符', 200, 'inventory', 'internal/module/inventory/service/inventory_warehouse_sku.go', 1, now(), now()),
('admin.inventory.err.stockNameLimit', 'en-US', 'at most {max} characters', 200, 'inventory', 'internal/module/inventory/service/inventory_warehouse_sku.go', 1, now(), now()),
('admin.inventory.err.stockBarcodeLimit', 'zh-CN', '上限 {max} 个字符', 200, 'inventory', 'internal/module/inventory/service/inventory_warehouse_sku.go', 1, now(), now()),
('admin.inventory.err.stockBarcodeLimit', 'en-US', 'at most {max} characters', 200, 'inventory', 'internal/module/inventory/service/inventory_warehouse_sku.go', 1, now(), now()),
('admin.inventory.err.stockRemarkLimit', 'zh-CN', '上限 {max} 个字符', 200, 'inventory', 'internal/module/inventory/service/inventory_warehouse_sku.go', 1, now(), now()),
('admin.inventory.err.stockRemarkLimit', 'en-US', 'at most {max} characters', 200, 'inventory', 'internal/module/inventory/service/inventory_warehouse_sku.go', 1, now(), now()),
('admin.inventory.err.stockImageLimit', 'zh-CN', '上限 {max} 个字符', 200, 'inventory', 'internal/module/inventory/service/inventory_warehouse_sku.go', 1, now(), now()),
('admin.inventory.err.stockImageLimit', 'en-US', 'at most {max} characters', 200, 'inventory', 'internal/module/inventory/service/inventory_warehouse_sku.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
