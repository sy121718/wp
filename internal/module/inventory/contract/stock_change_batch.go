package inventorycontract

import (
	"context"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
)

type BatchStockWriter interface {
	ChangeStockTxWithBatch(ctx context.Context, tx *gorm.DB, req *inventorydto.ChangeStockReq) (batchID string, err error)
}
