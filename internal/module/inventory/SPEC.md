---
id: go-wp-mod-inventory
type: module-design
title: inventory — 库存真源
status: active
parent: go-wp-modules
---

# inventory — 库存真源

## 职责

库存的唯一真源：仓库、仓库商品 SKU（库存行）、变动流水、变动原因、物料清单、货源、采购单与收货入库、**调拨单**、**库存预警**。

## 边界

- **可用量判断只读库存行锁内**：绝不读商品上的缓存库存列；缓存只被同步与对账写。
- **入库不得旁路**：采购收货与手工调整走同一变动契约。
- 库存变动与主数据配置变更严格分工：数量变动在这里，改前改后 diff 在 `masterdata`。
- 订单扣减由本模块的 `outbound/orderstock` 实现 `order` 的 `StockOperator` 契约——方向是「实现方依赖调用方契约」，不是 order 依赖 inventory。

## 仓库商品 SKU（迁移 621）

库存行 `inventory_stocks` 就是仓库侧 SKU 主档，**`variant_id` / `product_id` 可空**：

- NULL = 仓库里已有货、还没对应到商品（客户寄卖 / 清关到仓 / 手工补录）；
- 「认领」把两列填上（单向、**不自动合并**：目标 (变体, 仓) 已有行时拒绝，两行库存是两份事实）；
- 「解除认领」仅当 `quantity = 0 且 reserved_quantity = 0`。

**编码永远是裸码**（`DRAWERSMOKE_001`），仓码前缀只属于商品侧（迁移 262 / 246 + 两个巡检脚本）。

## 三量恒等式与预占

```
on_hand   = quantity
reserved  = reserved_quantity          （0 <= reserved <= quantity，两条 DDL CHECK）
available = quantity - reserved_quantity
```

**预占 / 释放只改 `reserved_quantity`，不改 `quantity`、不写流水。** 流水只在真实出入库时写。

两段式：下单预占（`ReserveStockTx`）→ 发货扣减（`DeductStockTx`，同一条 SQL 里 `quantity -= n` 且 `reserved -= LEAST(reserved, n)`）→ 取消 / 退款 / 超时释放（`ReleaseStockTx`）。

## 出库跨仓分配（迁移 623）

只在「出库且行 / 请求都没写仓」时启用，用**可售**（不是 quantity）按 `deduct_priority → 仓短码 → 仓 id` 分配。显式指定的仓永远优先。可售不足**整单拒绝**。

## 调拨（迁移 627）

状态：`pending → in_transit → completed`，或 `pending → cancelled`。

- 明细指向**库存行**（`stock_id`）而不是变体：未认领的货同样可以调拨；
- **出货与签收各需至少一张凭证附件**（缺即拒绝、整体回滚）；
- 调拨用可售；建单即定两个仓（`from <> to` 由 DDL 约束）。

## 库存预警

只统计 `track_quantity = true` 的行：`OutOfStock`（quantity ≤ 0）/ `LowStock`（quantity > 0 且可售 ≤ 0）/ `Alert = 前两者之和`。
