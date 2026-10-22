---
id: go-wp-mod-order
type: module-design
title: order — 订单与退货
status: active
parent: go-wp-modules
---

# order — 订单与退货

## 职责

订单、订单明细、状态流水、退货与退货明细、优惠码与核销，以及基于订单派生的客户分析物化表（同期群 / 增长 / RFM / 分群 / 日汇总 / 热销）。

订单是交易链路的唯一落账点。

## 边界

- **不 import 库存模块**：需要扣库存时用本模块收窄声明的 `StockOperator` 契约，由 `inventory` 侧实现并注入。grep 不到 inventory 是设计而非缺失。
- 订单表带 FORCE 策略：只给 id 的入口必须先逐工程探测归属再进事务。
- 状态流转必须经同一状态机，不旁路写状态列。
- 分析物化表只由订单数据重建，不接受外部写入。
