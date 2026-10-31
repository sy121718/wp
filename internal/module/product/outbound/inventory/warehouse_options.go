package inventory

import (
	"context"
	"strings"

	inventorycontract "go_wp/internal/module/inventory/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productservice "go_wp/internal/module/product/service"
)

const warehouseSKUOptionSize = 100

type Options struct {
	svc inventorycontract.InventoryService
}

func New(svc inventorycontract.InventoryService) *Options {
	return &Options{svc: svc}
}

var _ productcontract.WarehouseOptionsPort = (*Options)(nil)

func (o *Options) WarehouseOptions(ctx context.Context, projectID string) (out []productdto.WarehouseOptionResp, err error) {
	out = []productdto.WarehouseOptionResp{}
	if o.svc == nil || strings.TrimSpace(projectID) == "" {
		return out, nil
	}
	rows, lerr := o.svc.ListWarehouses(ctx, &inventorycontract.ListWarehouseReq{ProjectID: projectID})
	if lerr != nil {
		return nil, lerr
	}
	for _, w := range rows {
		if w == nil {
			continue
		}
		out = append(out, productdto.WarehouseOptionResp{
			ID:        w.ID,
			Code:      w.Code,
			Name:      w.Name,
			IsDefault: w.IsDefault,
		})
	}
	return out, nil
}

func (o *Options) WarehouseSKUOptions(ctx context.Context, projectID string, warehouses []productdto.WarehouseOptionResp) (out []productdto.WarehouseSKUGroupResp, err error) {
	out = []productdto.WarehouseSKUGroupResp{}
	if o.svc == nil || strings.TrimSpace(projectID) == "" {
		return out, nil
	}
	for _, w := range warehouses {
		if strings.TrimSpace(w.ID) == "" {
			continue
		}
		rows, lerr := o.svc.ListWarehouseSKUs(ctx, &inventorycontract.ListWarehouseSKUReq{
			ProjectID: projectID, WarehouseID: w.ID, Size: warehouseSKUOptionSize,
		})
		if lerr != nil {
			return nil, lerr
		}
		if len(rows) == 0 {
			continue
		}
		items := make([]productdto.WarehouseSKUItemResp, 0, len(rows))
		warehouseCode := ""
		for _, r := range rows {

			code := productservice.StripWarehousePrefix(r.SKUCode, r.WarehouseCode)
			if warehouseCode == "" {
				warehouseCode = r.WarehouseCode
			}
			items = append(items, productdto.WarehouseSKUItemResp{
				SKUCode: code, ExternalSKU: r.ExternalSKU,
				StockID: r.ID, Name: r.Name, ImageURL: r.ImageURL,
				Quantity: r.Quantity, TrackQuantity: r.TrackQuantity,
			})
		}
		out = append(out, productdto.WarehouseSKUGroupResp{
			WarehouseID:   w.ID,
			WarehouseCode: warehouseCode,
			WarehouseName: w.Name,
			IsDefault:     w.IsDefault,
			Items:         items,
		})
	}
	return out, nil
}
