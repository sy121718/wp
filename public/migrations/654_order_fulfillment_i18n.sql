-- 654 · 履约状态的 12 条文案（B7，迁移 652 建列之后的词条）。
--
-- 为什么必须 seed：enums.FulfillmentLabel 给的是 (key, 中文兜底)，词条缺失时页面
-- 回落到中文 —— 中文界面完全看不出来，只有切到 en-US 才暴露，而那时看到的还是中文。
--
-- 12 个取值与 ordermodel.Fulfillment* 常量一一对应（见迁移 652 的取值清单）。
-- 去掉了 dy_erp 的 synced / syncing 两态：那是多平台订单同步作业的状态，
-- go_wp 是单团队多工程模型、没有同步实体，造一个永远走不到的状态只会让筛选条多两个恒为 0 的徽章。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.orders.fulfillment.pending', 'zh-CN', '待履约', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.pending', 'en-US', 'Pending fulfilment', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.paid', 'zh-CN', '待发货', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.paid', 'en-US', 'Awaiting shipment', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.partial', 'zh-CN', '部分发货', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.partial', 'en-US', 'Partially shipped', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.shipped', 'zh-CN', '已发货', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.shipped', 'en-US', 'Shipped', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.received', 'zh-CN', '已收货', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.received', 'en-US', 'Received', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.returned', 'zh-CN', '已退货', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.returned', 'en-US', 'Returned', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.return_done', 'zh-CN', '退货完成', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.return_done', 'en-US', 'Return closed', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.reship', 'zh-CN', '换货补发', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.reship', 'en-US', 'Replacement shipped', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.exception', 'zh-CN', '履约异常', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.exception', 'en-US', 'Fulfilment exception', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.failed', 'zh-CN', '履约失败', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.failed', 'en-US', 'Fulfilment failed', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.cancelled', 'zh-CN', '已取消', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.cancelled', 'en-US', 'Cancelled', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.refunded', 'zh-CN', '已退款', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now()),
('admin.orders.fulfillment.refunded', 'en-US', 'Refunded', 200, 'admin', 'internal/module/order/enums/order_enums.go · FulfillmentLabel', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
