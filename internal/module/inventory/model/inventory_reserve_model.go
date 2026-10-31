package inventorymodel

import (
	"context"
	"time"

	"gorm.io/gorm"
)

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
