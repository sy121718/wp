-- ========================================
-- 402 — 商品 / 库存域空态收口的新增词条（审计 02-M 的 D8 / D9 / D10）
--
-- 背景：同批在模板层修了 D1（空数据把 <table>/<thead> 一起吃掉），另外三处缺陷需要新文案：
--   · D8（/admin/inventory 流水空态）：原句「入库请从**采购入库页**收货，盘点 / 报损请走**右上角**的库存调整。」
--        两个问题一起犯 ——「采购入库页」是纯文本（同页 page-sub 里同一件事就是 <a>），
--        而「库存调整」按钮**就在这个空态里**，不在右上角。链接只能靠把句子拆成
--        lead + link + tail 三段来拼（词条值是纯文本，塞不进 <a>），故新增 3 个 key 而不是
--        UPDATE 旧句 `admin.inventory.moves.empty`（317 修正过它一次，但那句话无论如何都放不下链接）。
--   · D9（/admin/inventory/sources 空态）：原句把「筛出来的空」与「工程里真的没有货源」合成一句，
--        且「用上面的表单建一个」——「上面的表单」实际是筛选栏，新建走页头抽屉。
--        照 products.html 的双档结构拆成：筛选无结果（重置）/ 无数据（新建抽屉）。
--   · D10（库存域 4 处空态缺 .empty-actions）：warehouses 补按钮后，原描述句
--        「这个工程还没有仓库。用上面的表单建一个 —— …」与事实冲突（页面上方没有表单），
--        一并换成指向按钮的描述。reasons 的描述句没有方位指引，复用即可。
--
-- 为什么必须走迁移而不是只改模板：**模板里的中文只是 t() 兜底，词条命中时显示的是库里的值**。
--   191 用 INSERT ... ON CONFLICT (item_key, lang) DO NOTHING seed 过本域词条，
--   改 seed 对存量库是 no-op，页面继续显示旧文案（317 / 399 踩过同一件事）。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本批**只新增、不修改任何既有词条**。
-- ========================================

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.inventory.moves.emptyLead', 'zh-CN', '还没有库存流水 —— 入库请从', 200, 'admin', 'admin/inventory.html: 流水空态描述前半（后接采购入库页链接）', 1, now(), now()),
('admin.inventory.moves.emptyLead', 'en-US', 'No stock movements yet — receive stock from the ', 200, 'admin', 'admin/inventory.html: 流水空态描述前半（后接采购入库页链接）', 1, now(), now()),
('admin.inventory.moves.emptyPurchaseLink', 'zh-CN', '采购入库页', 200, 'admin', 'admin/inventory.html: 流水空态描述里的链接文案', 1, now(), now()),
('admin.inventory.moves.emptyPurchaseLink', 'en-US', 'Purchase Receiving page', 200, 'admin', 'admin/inventory.html: 流水空态描述里的链接文案', 1, now(), now()),
('admin.inventory.moves.emptyTail', 'zh-CN', '收货；盘点 / 报损请点下面的「库存调整」。', 200, 'admin', 'admin/inventory.html: 流水空态描述后半（按钮就在这个空态里，不再写「右上角」）', 1, now(), now()),
('admin.inventory.moves.emptyTail', 'en-US', '; for stock counts or write-offs, use the Stock Adjustment button below.', 200, 'admin', 'admin/inventory.html: 流水空态描述后半（按钮就在这个空态里，不再写「右上角」）', 1, now(), now()),
('admin.inventory.list.emptyNew', 'zh-CN', '这个工程还没有仓库。点下面的「新建仓库」建一个 —— 建商品与变体时的归属仓就来自这里。', 200, 'admin', 'admin/inventory_warehouses.html: 空态描述（原「用上面的表单」指向不存在的表单）', 1, now(), now()),
('admin.inventory.list.emptyNew', 'en-US', 'This project has no warehouses yet. Use the “New warehouse” button below — variants take their owning warehouse from this list.', 200, 'admin', 'admin/inventory_warehouses.html: 空态描述（原「用上面的表单」指向不存在的表单）', 1, now(), now()),
('admin.inventory_sources.list.emptyTitle', 'zh-CN', '还没有货源', 200, 'admin', 'admin/inventory_sources.html: 空态标题（无筛选条件时）', 1, now(), now()),
('admin.inventory_sources.list.emptyTitle', 'en-US', 'No sources yet', 200, 'admin', 'admin/inventory_sources.html: 空态标题（无筛选条件时）', 1, now(), now()),
('admin.inventory_sources.list.emptyNew', 'zh-CN', '建一个货源 —— 采购单的来源就来自这里。', 200, 'admin', 'admin/inventory_sources.html: 空态描述（无筛选条件时，配新建抽屉按钮）', 1, now(), now()),
('admin.inventory_sources.list.emptyNew', 'en-US', 'Create a source — purchase orders take their supplier from here.', 200, 'admin', 'admin/inventory_sources.html: 空态描述（无筛选条件时，配新建抽屉按钮）', 1, now(), now()),
('admin.inventory_sources.list.emptyFiltered', 'zh-CN', '换个类型、状态或关键词再试，也可以清掉筛选看全部货源。', 200, 'admin', 'admin/inventory_sources.html: 空态描述（有筛选条件却零结果）', 1, now(), now()),
('admin.inventory_sources.list.emptyFiltered', 'en-US', 'Try another type, status or keyword, or clear the filters to see all sources.', 200, 'admin', 'admin/inventory_sources.html: 空态描述（有筛选条件却零结果）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
