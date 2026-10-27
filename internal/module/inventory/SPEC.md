---
id: go-wp-mod-inventory
type: module-design
title: inventory — 库存真源
status: active
parent: go-wp-modules
---

# inventory — 库存真源

## 职责

库存的唯一真源：仓库、仓库商品 SKU（库存行）、变动流水、变动原因、**调拨单**、**库存预警**。

采购域（货源、采购单、收货入库）自 2026-10 拆到 `internal/module/purchase`：它只经本模块的 `contract`（`ChangeStockTxWithBatch` / `ProductStockLookup`）写库存与读仓库，**不 join 库存表**。

## 边界

- **可用量判断只读库存行锁内**：绝不读商品上的缓存库存列；缓存只被同步与对账写。
- **入库不得旁路**：采购收货（purchase 模块）与手工调整走同一变动契约。
- 库存变动与主数据配置变更严格分工：数量变动在这里，改前改后 diff 在 `masterdata`。
- 订单扣减由本模块的 `outbound/orderstock` 实现 `order` 的 `StockOperator` 契约——方向是「实现方依赖调用方契约」，不是 order 依赖 inventory。

## 仓库商品 SKU（迁移 621）

库存行 `inventory_stocks` 就是仓库侧 SKU 主档（迁移 642 起带名称 / 图片 / 条码 / 运费 / 币种 / 汇率 / 状态 / 备注）。
它的身份是 **仓库 × 仓库 SKU**（`UNIQUE (warehouse_id, sku_code)`，迁移 244；迁移 641 删掉了更早的
`(variant_id, warehouse_id)` 唯一键）：同一个变体在同一仓可以有**多条**货行（自有码 / 客户码 / 不同批次）。

- `variant_id` / `product_id` 是**线索列**，可空、不唯一、不参与定位（2026-10 去认领，迁移 638）；
- 「这条货属于哪个变体」的真源在商品侧 `product_variants.warehouse_skus`（jsonb 数组，迁移 637）：
  元素 `{stockId, warehouseId, warehouseCode, skuCode}`，一个变体可引用多条货行、一条货行也可被多个变体引用；
- 认领 / 解除认领两个动作已下线：归属由商品建档时「从仓库选」那一步写入引用，不再有独立状态迁移。

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

## 调拨（迁移 630 + 审批 651）

状态：`requested → pending → in_transit → completed`，另有 `requested → rejected`（驳回）与
`requested/pending → cancelled`。

- **建单一律落 requested**：同意（`ApproveTransfer`）时才转 pending，且那一刻**重校源仓可售**
  （申请到同意之间货可能被订单预占）；同意时可改目标仓；
- 驳回（`RejectTransfer`）理由必填（纯文本 ≤500 字），不动库存；
- 明细指向**库存行**（`stock_id`）而不是变体：无归属的货（`variant_id` 为空）同样可以调拨；
- **出货与签收各需至少一张凭证附件**（缺即拒绝、整体回滚）；
- 调拨用可售；建单即定两个仓（`from <> to` 由 DDL 约束）。

## 库存预警

只统计 `track_quantity = true` 的行：`OutOfStock`（quantity ≤ 0）/ `LowStock`（quantity > 0 且可售 ≤ 0）/ `Alert = 前两者之和`。
