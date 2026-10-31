package inventorymodel

import (
	"context"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

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

type StockCandidatesRow struct {
	ID               string  `gorm:"column:id"`
	VariantID        *string `gorm:"column:variant_id"`
	WarehouseID      string  `gorm:"column:warehouse_id"`
	SKUCode          string  `gorm:"column:sku_code"`
	WarehouseCode    string  `gorm:"column:warehouse_code"`
	DeductPriority   int     `gorm:"column:deduct_priority"`
	Quantity         int     `gorm:"column:quantity"`
	ReservedQuantity int     `gorm:"column:reserved_quantity"`
	TrackQuantity    bool    `gorm:"column:track_quantity"`
}

func (m *Model) ListStockCandidatesByIDs(ctx context.Context, projectID string, stockIDs []string) (list []*StockCandidatesRow, err error) {
	if len(stockIDs) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Table("inventory_stocks AS s").
			Select("s.id, s.variant_id, s.warehouse_id, s.quantity, s.reserved_quantity, "+
				"s.track_quantity, s.sku_code, w.code AS warehouse_code, w.deduct_priority AS deduct_priority").
			Joins("JOIN inventory_warehouses AS w ON w.id = s.warehouse_id").
			Where("w.status = ?", "active").
			Where("s.id IN ?", stockIDs).
			Order("w.deduct_priority ASC, w.code ASC, w.id ASC, s.sku_code ASC, s.id ASC").
			Scan(&list).Error
	})
	return list, err
}

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

func (m *Model) ListStocksByWarehouseTx(ctx context.Context, tx *gorm.DB, projectID, warehouseID string) (list []*StockEntity, err error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	err = tx.WithContext(ctx).Model(&StockEntity{}).
		Where("warehouse_id = ?", warehouseID).Find(&list).Error
	return list, err
}

func (m *Model) CreateStockRowTx(ctx context.Context, tx *gorm.DB, e *StockEntity) (err error) {
	if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
		return serr
	}

	normalizeStockTracking(e)
	return tx.WithContext(ctx).Create(e).Error
}

type StockAlertRow struct {
	Total      int64 `gorm:"column:total"`
	OutOfStock int64 `gorm:"column:out_of_stock"`
	LowStock   int64 `gorm:"column:low_stock"`
}

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

func (m *Model) FindStockIDByVariantWarehouseTx(ctx context.Context, tx *gorm.DB, projectID, variantID, warehouseID string) (*string, error) {
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return nil, serr
	}
	var ids []string
	if err := tx.WithContext(ctx).Model(&StockEntity{}).
		Where("variant_id = ? AND warehouse_id = ?", variantID, warehouseID).
		Limit(1).Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return &ids[0], nil
}
