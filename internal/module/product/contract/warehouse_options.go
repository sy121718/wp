package productcontract

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

type (
	WarehouseOptionResp = productdto.WarehouseOptionResp

	WarehouseSKUGroupResp = productdto.WarehouseSKUGroupResp

	WarehouseSKUItemResp = productdto.WarehouseSKUItemResp
)

type WarehouseOptionsPort interface {
	WarehouseOptions(ctx context.Context, projectID string) (out []WarehouseOptionResp, err error)

	WarehouseSKUOptions(ctx context.Context, projectID string, warehouses []WarehouseOptionResp) (out []WarehouseSKUGroupResp, err error)
}
