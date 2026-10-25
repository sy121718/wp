---
id: go-wp-mod-order
type: module-design
title: order — 订单与退货
status: active
parent: go-wp-modules
---

# order — 订单与退货

## 职责

订单、订单明细、状态流水、退货与退货明细、优惠码与核销、**发货单与承运商**、**汇率快照**，以及基于订单派生的客户分析物化表（同期群 / 增长 / RFM / 分群 / 日汇总 / 热销）。

订单是交易链路的唯一落账点。

## 边界

- **不 import 库存模块**：需要库存能力时用本模块收窄声明的契约（`StockOperator` / `WarehouseResolver` / `StockAlertReader`），由 `inventory` 侧实现并注入。grep 不到 inventory 是设计而非缺失。
- 订单表带 FORCE 策略：只给 id 的入口必须先逐工程探测归属再进事务。
- 状态流转必须经同一状态机，不旁路写状态列（`allowedTransitions` 是唯一真源，不新增状态值）。
- 分析物化表只由订单数据重建，不接受外部写入。

## 两段式库存（迁移 621 / 625）

下单**只预占**（`reserveStockTx`），发货才扣减（`shipStockTx`）；取消 / 未发货退款**释放预占**（`releaseOrderReservationTx`）。

`order_items.warehouse_id` 在下单时解析并固化（空 = 存量行，首次发货时解析后回写）：预占与扣减必须落在同一个仓，否则默认仓一改就分裂成两批货。成本快照按**同一个仓**取（`VariantSnapshotsInWarehouse`）。

## 发货（迁移 624，S2）

一张发货单 = 一次发货事实（承运商 + 运单号 + 明细 + 库存扣减 + 状态流水），十步全在一个事务里。

- `request_id` 幂等：同一 (工程, request_id) 只产生一张单，重复提交直接返回那张；
- **部分发货不推进状态**（留在 `paid`），全部发完才 `shipped` —— 否则第二次发货会被「只有已付款可发货」挡住；
- 已发数量真源是**明细求和**（`SumShippedQuantityTx`），不是缓存列；
- 承运商停用不删（历史单据的 `carrier_id` 还指着它）。

## 退货入库的两支

按「发过货没有」分：已发货才把货加回 `quantity`（那是真的入库）；未发货只释放预占（加回数量会凭空多货）。

## 销售概览毛利（迁移 629）

`毛利 = Σ 行实付 − Σ 行成本 − Σ 运费`。税不扣（代收）、优惠已含在 `line_total` 里不重扣。

`cost_price IS NULL` 的行按 0 计入，同时标 `CostIncomplete` / `CostIncompleteRows` —— **绝不用 0 冒充**未知成本造成毛利虚高。

运费是订单级的，必须单独一条查询按订单去重（JOIN 会按明细行放大订单头）。

## 汇率快照（迁移 630）

`currency_rates` 是**币种对**的事实（不带 `project_id`），覆盖式（同一币对只留最新一条）。

`orders.fx_rate` / `fx_base_code` 是下单那一刻的快照：**落地即固化**，之后汇率怎么变都不影响历史订单。NULL = 汇率未固化（不推算、不回填）。

折算全程 `big.Rat`（`numeric(18,8)` 读成 float64 会丢精度）；`UnconvertedOrders` 必须显示 —— 否则偏小的折算金额会被当成全量。
