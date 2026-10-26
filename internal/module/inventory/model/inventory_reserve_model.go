package inventorymodel

// inventory_reserve_model.go — 预占 / 释放的行级写入（迁移 624）。
//
// 以**库存行 id** 为锁对象，用**原子 SQL** 完成读-改-写：
//
//	预占 / 释放 —— 只改 reserved_quantity（可售 = quantity - reserved_quantity）
//
// 为什么走原子 SQL 而不是「读出来 → 改结构体 → 写回」：预占是并发最激烈的那一段
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
