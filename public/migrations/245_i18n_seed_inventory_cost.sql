-- 245 · 仓库侧成本（批次 A）的新文案词条：中英成对。
--
-- 本批新增两个取词位：
--   · ErrStockCostInvalid —— 新增的 enums 常量（常量值就是 i18n key）。
--     显式传入的成本价不合法（负数 / NaN）时的业务错误；缺词条时接口与页面会原样
--     显示 ErrStockCostInvalid 这个裸 key；
--   · admin.inventory.cost.unknown —— 库存行的成本展示兜底（NULL = 尚未核算）。
--     成本列可空表示「还没核算」，页面上必须能与 0（合法的显式成本）区分开，
--     所以空值不能显示成 0，也不能显示成空白。
--
-- 幂等：ON CONFLICT DO NOTHING —— seed 是默认值来源，后台是真相来源（运营改过的不覆盖）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrStockCostInvalid', 'zh-CN', '成本价不合法（显式传入的成本必须是不小于 0 的数值）', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('ErrStockCostInvalid', 'en-US', 'Invalid cost price (an explicit cost must be a number not less than 0)', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
('admin.inventory.cost.unknown', 'zh-CN', '未核算', 200, 'inventory', 'internal/module/product/inventory/inbound/http/inventory_page_handle.go', 1, now(), now()),
('admin.inventory.cost.unknown', 'en-US', 'Not costed', 200, 'inventory', 'internal/module/product/inventory/inbound/http/inventory_page_handle.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
