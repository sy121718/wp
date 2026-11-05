-- 采购页面业务错误的完整词条：HTML 失败区只从词库取词，缺词会退成归口文案。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrReceiptOrderDone', 'zh-CN', '采购单已全部入库，无需再收。', 409, 'purchase', 'internal/module/purchase/enums', 1, now(), now()),
('ErrReceiptOrderDone', 'en-US', 'This purchase order is fully received; nothing left to receive.', 409, 'purchase', 'internal/module/purchase/enums', 1, now(), now()),
('ErrPurchaseLinesRequired', 'zh-CN', '采购单至少要有一行。', 400, 'purchase', 'internal/module/purchase/enums', 1, now(), now()),
('ErrPurchaseLinesRequired', 'en-US', 'A purchase order needs at least one line.', 400, 'purchase', 'internal/module/purchase/enums', 1, now(), now()),
('ErrStockSKURequired', 'zh-CN', '缺少仓库侧 SKU 编码：请传裸码（不带仓码前缀）。', 400, 'purchase', 'internal/module/inventory/enums', 1, now(), now()),
('ErrStockSKURequired', 'en-US', 'Warehouse-side SKU code is required: pass the bare code without a warehouse prefix.', 400, 'purchase', 'internal/module/inventory/enums', 1, now(), now()),
('ErrReceiptLinesRequired', 'zh-CN', '入库清单不能为空。', 400, 'purchase', 'internal/module/purchase/enums', 1, now(), now()),
('ErrReceiptLinesRequired', 'en-US', 'The receipt list cannot be empty.', 400, 'purchase', 'internal/module/purchase/enums', 1, now(), now()),
('ErrProductionVariantRequired', 'zh-CN', '生产入库必须给出 SKU 变体。', 400, 'purchase', 'internal/module/purchase/enums', 1, now(), now()),
('ErrProductionVariantRequired', 'en-US', 'Production inbound requires an SKU variant.', 400, 'purchase', 'internal/module/purchase/enums', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
