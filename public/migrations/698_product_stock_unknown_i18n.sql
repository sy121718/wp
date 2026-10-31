-- 698 · PRD-017：商品列表与 MCP 的「库存读取失败」词条（故障事实如实显示）。
--
-- 背景：商品列表的库存列是**查询期聚合**（真源在 inventory_stocks）。库存一侧读失败时，
-- 三态保持零值、行上打 StockUnknown —— 旧模板按 "none" 兜底显示「未入库」，
-- 等于把「这次没查到」当成「真的没入库」告诉运营。裁决（PRD-017）是故障如实显示：
-- 后台列表显示「库存读取失败」并不展开分仓（那一屏的每个仓都是伪「未入库」）。
--
-- 只增一条键：admin.products.stock.unknown（中 / 英）。
-- 已有的 stock.infinite / infiniteShort / none / expand 不动（运营可能已改过译文，
-- 这里也不复制它们 —— 复制会用旧 seed 覆盖线上运营值）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.products.stock.unknown', 'zh-CN', '库存读取失败', 200, 'product', 'internal/templates/admin/product/products.html', 1, now(), now()),
('admin.products.stock.unknown', 'en-US', 'Stock unavailable', 200, 'product', 'internal/templates/admin/product/products.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
