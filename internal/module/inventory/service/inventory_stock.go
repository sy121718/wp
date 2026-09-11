// inventory_stock.go — 库存记录读写（issue #15 验收 2/3/4）。
//
// 死线（spec §库存）：一切影响可用量的判断只能读 inventory_stocks 这一张真源表，
// 绝不读 product_variants.stock_total —— 那是后台列表展示用的冗余缓存，
// 一旦被当成扣减依据，缓存滞后就会直接变成超卖。本文件的读取路径全部走真源。
//
// 「按 SKU 增减 + 写流水 + 行锁」是 issue #16 的内容；本票的写入路径只有
// 「确保库存记录存在（初始 0）」这一条，因此 quantity 恒为 0，不做任何数值变更。
package inventoryservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
)

// EnsureStock 幂等地确保某 SKU 在某仓有一条库存记录（初始 0）。
func (s *Service) EnsureStock(ctx context.Context, req *inventorydto.EnsureStockReq) (res *inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.VariantID) == "" {
		return nil, errors.New(inventoryenums.ErrStockVariantRequired)
	}
	e, err := s.ensureStockRow(ctx, req.ProjectID, req.WarehouseID,
		strings.TrimSpace(req.ProductID), strings.TrimSpace(req.VariantID), strings.TrimSpace(req.SKUCode))
	if err != nil {
		return nil, err
	}
	return s.toStockResp(ctx, e), nil
}

// ensureStockRow 幂等生成库存行的内部实现（契约入口与商品端口共用同一条路径）。
func (s *Service) ensureStockRow(ctx context.Context, projectID, warehouseID, productID, variantID, skuCode string) (e *inventorymodel.StockEntity, err error) {
	wh, err := s.resolveWarehouse(ctx, projectID, warehouseID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	row := &inventorymodel.StockEntity{
		ID: uuid.NewString(), ProjectID: wh.ProjectID, WarehouseID: wh.ID,
		ProductID: productID, VariantID: variantID, SKUCode: skuCode,
		Quantity: 0, Metadata: json.RawMessage("{}"),
		CreatedAt: now, UpdatedAt: now,
	}
	return s.m.EnsureStock(ctx, row)
}

// GetStock 单条库存记录（按 id，或按 变体 × 仓库）。
func (s *Service) GetStock(ctx context.Context, req *inventorydto.GetStockReq) (res *inventorydto.StockResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	var e *inventorymodel.StockEntity
	if strings.TrimSpace(req.ID) != "" {
		if e, err = s.m.GetStock(ctx, req.ID); err != nil {
			return nil, mapStockNotFound(err)
		}
	} else if strings.TrimSpace(req.VariantID) != "" && strings.TrimSpace(req.WarehouseID) != "" {
		if e, err = s.m.GetStockByVariantWarehouse(ctx, req.VariantID, req.WarehouseID); err != nil {
			return nil, mapStockNotFound(err)
		}
	} else {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	return s.toStockResp(ctx, e), nil
}

// ListStocksBySKU 某 SKU 在各仓的库存（验收 3/4）。
//
// 返回的每一行都是 (SKU, 仓库) 一条真源记录：本期单仓时只有一行，
// 多仓时同一个 SKU 会出现多行 —— 这正是「库存以 SKU × 仓库 为维度」的读取口径。
func (s *Service) ListStocksBySKU(ctx context.Context, req *inventorydto.ListStockBySKUReq) (list []*inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.SKUCode) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListStockRows(ctx, inventorymodel.StockFilter{
		ProjectID: projectID, SKUCode: strings.TrimSpace(req.SKUCode),
	}, 0, 0)
	if err != nil {
		return nil, err
	}
	return toStockRespList(rows), nil
}

// ListStocks 库存记录列表（按工程 / 仓 / 商品 / 变体 / SKU 过滤 + 分页）。
func (s *Service) ListStocks(ctx context.Context, req *inventorydto.ListStockReq) (list []*inventorydto.StockResp, err error) {
	page, size := pageArgs(req)
	filter := inventorymodel.StockFilter{
		WarehouseID: strings.TrimSpace(req.WarehouseID),
		ProductID:   strings.TrimSpace(req.ProductID),
		VariantID:   strings.TrimSpace(req.VariantID),
		SKUCode:     strings.TrimSpace(req.SKUCode),
	}
	if filter.ProjectID, err = s.resolveProjectID(ctx, strings.TrimSpace(req.ProjectID)); err != nil {
		return nil, err
	}
	rows, err := s.m.ListStockRows(ctx, filter, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	return toStockRespList(rows), nil
}

// toStockResp 库存实体 → 响应（补仓库展示信息，避免后台二次查询）。
func (s *Service) toStockResp(ctx context.Context, e *inventorymodel.StockEntity) *inventorydto.StockResp {
	if e == nil {
		return nil
	}
	resp := &inventorydto.StockResp{
		ID: e.ID, ProjectID: e.ProjectID, WarehouseID: e.WarehouseID,
		ProductID: e.ProductID, VariantID: e.VariantID, SKUCode: e.SKUCode,
		Quantity:  e.Quantity,
		CreatedAt: e.CreatedAt.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
	if wh, err := s.m.GetWarehouse(ctx, e.WarehouseID); err == nil {
		resp.WarehouseCode, resp.WarehouseName = wh.Code, wh.Name
	}
	return resp
}

// toStockRespList 投影行 → 响应列表（join 已带仓库信息，无需逐行回查）。
func toStockRespList(rows []*inventorymodel.StockRow) []*inventorydto.StockResp {
	list := make([]*inventorydto.StockResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, &inventorydto.StockResp{
			ID: r.ID, ProjectID: r.ProjectID, WarehouseID: r.WarehouseID,
			WarehouseCode: r.WarehouseCode, WarehouseName: r.WarehouseName,
			ProductID: r.ProductID, VariantID: r.VariantID, SKUCode: r.SKUCode,
			Quantity:  r.Quantity,
			CreatedAt: r.CreatedAt.Format(time.RFC3339), UpdatedAt: r.UpdatedAt.Format(time.RFC3339),
		})
	}
	return list
}

// mapStockNotFound 行不存在 → 业务错误，其余原样透出。
func mapStockNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(inventoryenums.ErrStockNotFound)
	}
	return err
}
