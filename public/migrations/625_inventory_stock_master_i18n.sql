-- 622 · 仓库商品 SKU 预占 / 释放的文案词条：中英成对。
--
-- 2026-10 收敛：认领概念下线（迁移 638）后，本批只剩两类取词位：
--
--   ① 业务错误（enums 常量值即 i18n key，不 seed 就会在页面上原样显示裸 key）：
--      · ErrReserveUnderflow —— 释放的预占量超过已预占量（不静默夹到 0）；
--   ② 库存页筛选条的「（全部）」项（admin.inventory.filter.all，库存 / 调拨两页共用）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
--
-- 判定阈值见 register_catalog.go 的注册项：逐条枚举本批 (item_key, lang) 组合数，
-- 不用全库总量、也不用前缀 LIKE（前者会被同期其它批次的行满足而静默跳过，
-- 后者会被将来新增的同前缀 key 带跑 —— 仓库里 226/277/283/592 都踩过）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrReserveUnderflow', 'zh-CN', '要释放的预占数量超过了这一行已预占的数量', 409, 'inventory', 'internal/module/inventory/service/inventory_stock_reserve.go', 1, now(), now()),
('ErrReserveUnderflow', 'en-US', 'The quantity to release exceeds what this row has reserved', 409, 'inventory', 'internal/module/inventory/service/inventory_stock_reserve.go', 1, now(), now()),
('admin.inventory.filter.all', 'zh-CN', '（全部）', 200, 'inventory', 'internal/templates/admin/inventory/inventory_transfers.html', 1, now(), now()),
('admin.inventory.filter.all', 'en-US', '(all)', 200, 'inventory', 'internal/templates/admin/inventory/inventory_transfers.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
