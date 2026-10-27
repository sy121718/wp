-- 655 · B6 / B7 新增界面词条（待发货页筛选与可发守卫、退货页两个仓库视角列、订单列表两列与来源筛选）。
--
-- 分两批而不是一个迁移：654 是**枚举文案**（履约状态，与数据层的 12 个常量一一对应），
-- 655 是**页面列与提示**（散落在三个模板里）。两者的失效表现不同 ——
-- 枚举文案缺失会让筛选条出现裸 key，页面列缺失只是列头显示中文兜底 —— 排障时分开更好定位。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.order.pendingShip.filter.keyword', 'zh-CN', '订单号 / 客户名', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.filter.keyword', 'en-US', 'Order no. / customer', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.filter.keywordPh', 'zh-CN', '订单号 / 客户名', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.filter.keywordPh', 'en-US', 'Order no. / customer', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.col.source', 'zh-CN', '来源', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.col.source', 'en-US', 'Source', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.col.lines', 'zh-CN', '商品行数', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.col.lines', 'en-US', 'Lines', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.blocked.hint', 'zh-CN', '这张单现在发不出去，先按下面的说明处理。', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.blocked.hint', 'en-US', 'This order cannot ship yet; see the note below.', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.blocked.variantMissing', 'zh-CN', '有订单行没有关联商品规格，发货会被拒绝。', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.blocked.variantMissing', 'en-US', 'An order line has no product variant; shipping would be rejected.', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.blocked.unmatched', 'zh-CN', '有订单行的商品规格在库存里找不到可扣减的货行，发货会被拒绝。', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.blocked.unmatched', 'en-US', 'A line with a variant that has no deductible stock row; shipping would be rejected.', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.blocked.warehouseMissing', 'zh-CN', '有订单行没定发货仓，且这个工程没有可用的默认仓。', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.blocked.warehouseMissing', 'en-US', 'A line has no warehouse and this project has no default warehouse.', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.blocked.stockInsufficient', 'zh-CN', '发货仓的库存不够本次要发的件数，先补货或改发货仓。', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.blocked.stockInsufficient', 'en-US', 'The shipping warehouse does not hold enough stock; restock or pick another warehouse.', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.blocked.unknown', 'zh-CN', '这张单现在发不出去。', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.order.pendingShip.blocked.unknown', 'en-US', 'This order cannot ship right now.', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.returns.col.warehouse', 'zh-CN', '原发货仓', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.returns.col.warehouse', 'en-US', 'Shipped from', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.returns.col.pending_lines', 'zh-CN', '待入库行数', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.returns.col.pending_lines', 'en-US', 'Pending lines', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.returns.detail.warehouse', 'zh-CN', '原发货仓', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.returns.detail.warehouse', 'en-US', 'Shipped from', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.returns.detail.pending_lines', 'zh-CN', '待入库行数', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.returns.detail.pending_lines', 'en-US', 'Pending lines', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.orders.col.fulfillment', 'zh-CN', '履约', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.orders.col.fulfillment', 'en-US', 'Fulfilment', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.orders.col.created_via', 'zh-CN', '来源', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.orders.col.created_via', 'en-US', 'Source', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.orders.filter.created_via_all', 'zh-CN', '全部来源', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now()),
('admin.orders.filter.created_via_all', 'en-US', 'All sources', 200, 'admin', 'B6/B7 订单与退货页新增列与提示', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
