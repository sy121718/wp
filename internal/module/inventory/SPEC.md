---
id: go-wp-mod-inventory
type: module-design
title: inventory — 库存真源
status: active
parent: go-wp-modules
---

# inventory — 库存真源

## 职责

库存的唯一真源：仓库、SKU×仓库存、变动流水、变动原因、物料清单、货源、采购单与收货入库。

## 边界

- **可用量判断只读库存行锁内**：绝不读商品上的缓存库存列；缓存只被同步与对账写。
- **入库不得旁路**：采购收货与手工调整走同一变动契约。
- 库存变动与主数据配置变更严格分工：数量变动在这里，改前改后 diff 在 `masterdata`。
- 订单扣减由本模块的 `outbound/orderstock` 实现 `order` 的 `StockOperator` 契约——方向是「实现方依赖调用方契约」，不是 order 依赖 inventory。
