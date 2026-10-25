package inventorymodel

// inventory_shipping_model.go — 发货出库与跨仓扣减候选（迁移 621 / 623）。
//
// 发货出库一条 SQL 完成两列写回（quantity 与 reserved_quantity），
// 跨仓扣减候选一次 IN 取全再内存分配 —— 两者都是为了不在并发路径上留下中间态。

import (
	"context"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// DeductStockOnShipTx 发货出库：**同一句**里 quantity -= n 且 reserved -= min(reserved, n)。
//
// 为什么必须同一句：分成两句会有「数量扣了、预占没放」的中间态（或反之），
// 而中间态在并发下会被别的请求读到 —— 表现为「预占还在、货已经少了」，
// 可售数量凭空变成负数。同一句 UPDATE 里两列一起写，中间态不存在。
//
// reserved 释放量取 min(reserved, n) 而不是 n：
//
//   - 下单预占过 → reserved >= n，释放 n（精确对应）；
//   - 历史订单（预占引入之前建的）没有预占 → reserved = 0，释放 0，
//     不能让 reserved 变负（DDL 的 CHECK 会拒），也不能不扣货；
//   - 部分预占（人工调整过预占）→ 按实际预占量释放，不多不少。
//
// 返回影响行数：0 行 = quantity < n（可用量不足）或行不存在，调用方据此整体回滚。
func (m *Model) DeductStockOnShipTx(ctx context.Context, tx *gorm.DB, id string, quantity int, at time.Time) (affected int64, err error) {
	res := tx.WithContext(ctx).Model(&StockEntity{}).
		Where("id = ? AND quantity >= ?", id, quantity).
		Updates(map[string]any{
			"quantity":          gorm.Expr("quantity - ?", quantity),
			"reserved_quantity": gorm.Expr("reserved_quantity - LEAST(reserved_quantity, ?)", quantity),
			"update_time":       at,
		})
	return res.RowsAffected, res.Error
}

// StockCandidatesRow 跨仓扣减的候选行（只读投影：一次取全，内存里按优先级分配）。
//
// 对齐 ERP 的 listDeductCandidates：避免「逐行 ORDER BY LIMIT 1 + 更新」把行锁范围
// 扩大并制造多次往返。候选先全拿到、再统一下传，锁序才是确定的。
type StockCandidatesRow struct {
	ID               string  `gorm:"column:id"`
	VariantID        *string `gorm:"column:variant_id"`
	WarehouseID      string  `gorm:"column:warehouse_id"`
	WarehouseCode    string  `gorm:"column:warehouse_code"`
	DeductPriority   int     `gorm:"column:deduct_priority"`
	Quantity         int     `gorm:"column:quantity"`
	ReservedQuantity int     `gorm:"column:reserved_quantity"`
	TrackQuantity    bool    `gorm:"column:track_quantity"`
}

// ListDeductCandidates 取若干变体在**各活跃仓**的库存行，按扣减优先级排好序（只读、不加锁）。
//
// 排序口径（迁移 623）：deduct_priority 升序 → 仓短码升序 → 仓 id 升序。
// 同优先级时按短码而不是随机顺序：跨环境重放同一批数据必须扣在同一个仓上，
// 否则「为什么这个仓少了 3 件」在每个环境给出不同答案。
func (m *Model) ListDeductCandidates(ctx context.Context, projectID string, variantIDs []string) (list []*StockCandidatesRow, err error) {
	if len(variantIDs) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Table("inventory_stocks AS s").
			Select("s.id, s.variant_id, s.warehouse_id, s.quantity, s.reserved_quantity, "+
				"s.track_quantity, w.code AS warehouse_code, w.deduct_priority AS deduct_priority").
			Joins("JOIN inventory_warehouses AS w ON w.id = s.warehouse_id").
			Where("w.status = ?", "active").
			Where("s.variant_id IN ?", variantIDs).
			Order("w.deduct_priority ASC, w.code ASC, w.id ASC, s.id ASC").
			Scan(&list).Error
	})
	return list, err
}

// ListStocksByWarehouseTx 取某仓的全部库存行（**不加锁**，供调拨 / 签收前的目标行查找）。
func (m *Model) ListStocksByWarehouseTx(ctx context.Context, tx *gorm.DB, projectID, warehouseID string) (list []*StockEntity, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	err = tx.WithContext(ctx).Model(&StockEntity{}).
		Where("warehouse_id = ?", warehouseID).Find(&list).Error
	return list, err
}

// CreateUnclaimedStockTx 建一条**未认领**库存行（product_id / variant_id 均为 NULL，迁移 621）。
//
// 与 EnsureStockTx 的区别是幂等性：这里**不幂等**（同仓同码重复插入应该撞
// 244 的 UNIQUE (warehouse_id, sku_code)），因为「人工建档」是一个显式的建货动作，
// 重复提交必须报错而不是静默返回旧行 —— 否则运营会以为补录成功了。
func (m *Model) CreateUnclaimedStockTx(ctx context.Context, tx *gorm.DB, e *StockEntity) (err error) {
	if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
		return serr
	}
	// 与 EnsureStockTx 同一道兜底（见 normalizeStockTracking）。
	normalizeStockTracking(e)
	return tx.WithContext(ctx).Create(e).Error
}

// StockVariantExistsTx 该仓里是否**已经**有某变体的库存行（认领前的冲突判定）。
//
// 判据是 (variant_id, warehouse_id) 而不是 (warehouse_id, sku_code)：认领不改编码，
// 撞的必须是「同一个变体在该仓已有行」这件事 —— 那才是两份事实。
func (m *Model) StockVariantExistsTx(ctx context.Context, tx *gorm.DB, projectID, warehouseID, variantID string) (exists bool, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return false, serr
	}
	var n int64
	err = tx.WithContext(ctx).Model(&StockEntity{}).
		Where("warehouse_id = ? AND variant_id = ?", warehouseID, variantID).
		Count(&n).Error
	return n > 0, err
}

// StockAlertRow 库存预警的四个计数（口径见 StockAlertCounts）。
type StockAlertRow struct {
	Total      int64 `gorm:"column:total"`
	OutOfStock int64 `gorm:"column:out_of_stock"`
	LowStock   int64 `gorm:"column:low_stock"`
}

// StockAlertCounts 库存预警（迁移 621 / 623，W4）：一条查询数三个档。
//
// 口径照抄 ERP 的 WarehouseSKUStockAlert，但**只统计 track_quantity = true 的行**：
// 无限库存永远「有货」，不该进预警。
//
//	out  —— quantity <= 0（跟踪且卖光）
//	low  —— quantity > 0 且 quantity - reserved_quantity <= 0（货在，但全被订单占住）
//
// Alert = out + low 由 service 算（两个数字相加是展示口径，不是 SQL 的分组）。
func (m *Model) StockAlertCounts(ctx context.Context, projectID string) (row StockAlertRow, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&StockEntity{}).
			Select("COUNT(*) AS total, "+
				"COUNT(*) FILTER (WHERE quantity <= 0) AS out_of_stock, "+
				"COUNT(*) FILTER (WHERE quantity > 0 AND quantity - reserved_quantity <= 0) AS low_stock").
			Where("project_id = ? AND track_quantity", projectID).
			Scan(&row).Error
	})
	return row, err
}
