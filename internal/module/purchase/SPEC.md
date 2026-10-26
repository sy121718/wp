---
id: go-wp-mod-purchase
type: module-design
title: purchase — 货源与采购入库
status: active
parent: go-wp-modules
---

# purchase — 货源与采购入库

## 职责

货源（供应商 / 集团内关联公司 / 自家工厂）、采购单与采购明细、收货与生产入库单及其明细，以及基于入库派生的进货历史投影。

自 2026-10 从 `inventory` 拆出：库存数量变动仍只有一个真源，采购只是**它的一个写入口**。

## 边界

- **不 import 库存的 dto / model / service**：库存能力经本模块的 `contract.StockPort`（自有形状的 `StockChange` / `StockLine`）与 `contract.VariantCatalog`；由 `inventory/outbound/purchasestock` 实现并注入（反向实现，不算反向依赖）。
- **不 join 库存表**：列表要显示的仓库名走 `StockPort.WarehouseNames`，不在 SQL 里连 `inventory_warehouses`。
- **表名保留 `inventory_` 前缀**是历史命名（迁移 196 / 206 / 626 的 plpgsql 函数体写死了表名），模块归属跟着 `internal/module/purchase` 走，不跟着表名。
- 权限点字符串保留 `inventory:source_*` / `inventory:purchase_*`：`sys_permission` 已有行、Casbin 按字符串匹配，改名会让策略全失配。

## 单据的语义

- **单据身份**：单号工程内唯一、货源必须启用、收货仓为空时兜底默认仓。
- **状态是推导值，不是输入**：`pending → partial → received`，全量收满才 `received`；`finished` / `cancelled` 只能从终态规则允许的位置推进（`assertPurchaseTransition`）。
- **行是原子的**：明细全量替换，且仅在**没有任何已入库行**时允许替换（`ErrPurchaseLinesLocked`）。
- SKU 一律先归一（剥本仓短码前缀）再落库 —— 仓库里的编码永远是裸码，与 `inventory` 同一条口径（`scripts/check-inventory-sku-format.sh` 兜两处实现漂移）。

## 入库事务（收货 / 生产两支同形）

一个事务里：锁单头 → 原子递增已入库（返回 0 即超收）→ 写入库单与入库行 → **经 `StockPort.ChangeStockTx` 写库存变动** → 置 `posted` 并记批次号 → 重算单据状态。

- 批次号必须与入库单**同一事务**落行：`BatchStockWriter.ChangeStockTxWithBatch` 返回 batchID 就是为这个；提交后回读会留下「单据已提交、流水查不到」的窗口。
- `request_id` 幂等：唯一键冲突即经 `idempotentReceipt` 重放已存在的那张单。
- 成本两条写路径：仓库侧 `inventory_stocks.cost_price` 随变动同事务；商品侧 `product_variants.cost_price` 提交后 best-effort（失败写 `cost_error`，不回滚单据）。
- 生产入库没有采购单：货源类型必须是 `internal`（`ErrProductionSourceNotInternal`），单价必填。

## 货源

三类靠 `type` + `related_party` 区分（外部 / 集团内关联公司 / 自家工厂）。

- 内部类型恒为关联方；`settle_price` 只属内部，外部带结算价直接拒绝。
- `config` 必须是 JSON 对象且压缩重写，体积上限 64 KiB。
- 变更留痕走 `masterdata` 契约（`EntityInventorySource`），改前快照与动作由本模块提供。
