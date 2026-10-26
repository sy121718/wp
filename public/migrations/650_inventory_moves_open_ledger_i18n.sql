-- 650 · 库存管理页的流水卡：拆页入口 + 两句过期提示的措辞收口（B4 收尾）。
--
-- 三件事：
--
--   1. 新增 admin.inventory.moves.openLedger =「查看全部流水」。流水已拆成独立页
--      （/admin/inventory/movements），库存管理页那张卡只剩「这个 SKU 最近发生了什么」
--      的预览。不给入口的话，想看全局流水的人会去清空 SKU 筛选 —— 那同时清掉了上面
--      库存卡的上下文，等于用一个副作用换另一个视图。
--   2. admin.inventory.moves.hint 里「按物料清单展开扣减产生的子项流水会标出它由哪个父 SKU
--      引出」这句已不成立：BOM 概念随捆绑品一起下线（迁移 636 删表、删 parent_variant_id 的写入）。
--   3. admin.inventory.sku.empty 里「库存以 SKU × 仓库 为维度」已不成立：迁移 641 之后
--      一条货的身份是「仓库 × 仓库 SKU」，同一变体在同一个仓可以有多条货。
--
-- 幂等：前两条是改值（DO UPDATE），第三条是新增（DO NOTHING 语义由 DO UPDATE 一并覆盖）。
INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('admin.inventory.moves.openLedger', 'zh-CN', '查看全部流水'),
('admin.inventory.moves.openLedger', 'en-US', 'All movements'),
('admin.inventory.moves.hint', 'zh-CN', '每一次真源变动都留痕：方向、数量、变动前后值、原因、来源引用与批次号。'),
('admin.inventory.moves.hint', 'en-US', 'Every change to the stock of record leaves a trace: direction, quantity, values before and after, reason, source reference and batch id.'),
('admin.inventory.sku.empty', 'zh-CN', '选一个 SKU 后，这里列出它在各仓的库存记录（一条货的身份是「仓库 + 仓库 SKU」，同一个仓里可以有多条货）。'),
('admin.inventory.sku.empty', 'en-US', 'Pick a SKU and this section lists its stock records per warehouse (a stock row is identified by warehouse + warehouse SKU, and one warehouse can hold several rows for the same product).')
ON CONFLICT (item_key, lang) DO UPDATE SET item_value = EXCLUDED.item_value, update_time = now();
