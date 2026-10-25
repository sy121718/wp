package inventorymodel

// inventory_claim_model.go — 仓库商品 SKU 主档化的行级写入（迁移 621）。
//
// 两组都以**库存行 id** 为锁对象，并一律用**原子 SQL** 完成读-改-写：
//
//	认领 / 解除   —— 只改 product_id / variant_id（认领是单向动作，不做自动合并）
//	预占 / 释放   —— 只改 reserved_quantity（可售 = quantity - reserved_quantity）
//
// 为什么全部走原子 SQL 而不是「读出来 → 改结构体 → 写回」：预占是并发最激烈的那一段
//（同一件货被多个订单同时抢），读-改-写会让两个请求都读到旧值、写成两个「加一次」相撞
// —— 表现为少卖（reserved 只加了一次）或超卖（守卫读到旧值放行）。
// DDL 的两条 CHECK 只是最后一道兜底，不是判据。
//
// 加锁顺序：调用方在事务内先按 (variant_id, warehouse_id) 升序 `FOR UPDATE` 锁定目标行，
// 再调这里的 UPDATE —— 与 applyStockChangesTx 同一锁序，出入库与预占两条路径因此共享
// 一个全局顺序，等待链不可能成环。

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// ClaimStockTx 把一条未认领的库存行认领到某个商品变体（product_id + variant_id 一起写）。
//
// 只改这两列，不动 quantity / reserved_quantity / sku_code / cost_price：
// 认领回答的是「这条货是哪件商品的」，不是「这条货有多少」。
//
// **不做自动合并**：目标 (变体, 仓) 已有行时由 service 拒绝（ErrStockAlreadyClaimed），
// 因为两行库存是两份事实（成本可能不同、数量各自独立），合并要么丢数量、要么丢成本。
//
// `AND variant_id IS NULL` 是并发下的第二道闸：判定与写入之间若已被别的请求认领，
// 这条 UPDATE 匹配 0 行，调用方据此报错而不是把已认领的行改挂到另一个变体上。
func (m *Model) ClaimStockTx(ctx context.Context, tx *gorm.DB, id, productID, variantID string, at time.Time) (affected int64, err error) {
	res := tx.WithContext(ctx).Model(&StockEntity{}).Where("id = ? AND variant_id IS NULL", id).
		Updates(map[string]any{
			"product_id":  productID,
			"variant_id":  variantID,
			"update_time": at,
		})
	return res.RowsAffected, res.Error
}

// UnclaimStockTx 解除认领（商品下线 / 认领错了）：把 variant_id / product_id 置回 NULL。
//
// 只允许在 quantity = 0 且 reserved_quantity = 0 的行上执行 —— 判据由 service 在锁内判
// （它要把「为什么不允许」说清楚）；这里再带一次 `quantity = 0 AND reserved_quantity = 0`
// 作为**并发下的第二道闸**：判定与写入之间若有别的请求入了货，这条 UPDATE 会匹配 0 行，
// 调用方据此报错而不是把带数量的行悄悄解除认领（那会让货从「商品库存」里消失）。
func (m *Model) UnclaimStockTx(ctx context.Context, tx *gorm.DB, id string, at time.Time) (affected int64, err error) {
	res := tx.WithContext(ctx).Model(&StockEntity{}).
		Where("id = ? AND variant_id IS NOT NULL AND quantity = 0 AND reserved_quantity = 0", id).
		Updates(map[string]any{
			"product_id":  nil,
			"variant_id":  nil,
			"update_time": at,
		})
	return res.RowsAffected, res.Error
}

// UpdateStockReservedTx 预占 / 释放：只改 reserved_quantity（delta 可正可负）。
//
// 两条守卫写在同一句 SQL 里（返回影响行数，受影响 0 行 = 守卫拦下）：
//
//	reserved_quantity + delta >= 0         —— 释放不得把预占打到负数（不静默夹到 0）
//	reserved_quantity + delta <= quantity  —— 预占不得超在库（DDL 的 CHECK 同一口径）
//
// 为什么不用 `LEAST/GREATEST` 夹一下了事：夹值会让「预占比货还多」这种真错误变成
// 一次静默的部分成功 —— 调用方以为预占成功了，实际只预占到在库数，发货时才发现少货。
func (m *Model) UpdateStockReservedTx(ctx context.Context, tx *gorm.DB, id string, delta int, at time.Time) (affected int64, err error) {
	res := tx.WithContext(ctx).Model(&StockEntity{}).
		Where("id = ?", id).
		Where("reserved_quantity + ? >= 0", delta).
		Where("reserved_quantity + ? <= quantity", delta).
		Updates(map[string]any{
			"reserved_quantity": gorm.Expr("reserved_quantity + ?", delta),
			"update_time":       at,
		})
	return res.RowsAffected, res.Error
}
