// product_port.go — 实现 product 契约定义的变体库存端口（依赖方向 inventory → product）。
//
// 商品模块建变体时需要两件本模块才知道的事：
//  1. 归属仓的短码 —— SKU 编码形如 {仓短码}_{商品码}_{序号}，未指定仓时用默认仓；
//  2. 在归属仓生成一条初始 0 的库存记录。
//
// 商品模块只依赖本端口（端口定义在 product/contract），实现留在这里 ——
// 商品模块不认识仓库模块，装配期由顶层把本服务注入。
package inventoryservice

import (
	"context"
	"errors"
	"strings"

	inventoryenums "go_wp/internal/module/inventory/enums"
	productcontract "go_wp/internal/module/product/contract"
)

// ResolveWarehouse 解析归属仓（warehouseID 为空 → 该工程的默认仓），返回只读引用。
func (s *Service) ResolveWarehouse(ctx context.Context, projectID, warehouseID string) (ref *productcontract.WarehouseRef, err error) {
	wh, err := s.resolveWarehouse(ctx, projectID, warehouseID)
	if err != nil {
		return nil, err
	}
	return &productcontract.WarehouseRef{
		ID: wh.ID, ProjectID: wh.ProjectID, Code: wh.Code, Name: wh.Name,
	}, nil
}

// EnsureVariantStock 在归属仓为该 SKU 生成库存记录（已存在则复用，初始 0）。
func (s *Service) EnsureVariantStock(ctx context.Context, ref *productcontract.WarehouseRef, productID, variantID, skuCode string) (err error) {
	if ref == nil || strings.TrimSpace(ref.ID) == "" {
		return errors.New(inventoryenums.ErrStockWarehouseNeeded)
	}
	if strings.TrimSpace(variantID) == "" {
		return errors.New(inventoryenums.ErrStockVariantRequired)
	}
	_, err = s.ensureStockRow(ctx, ref.ProjectID, ref.ID, productID, variantID, skuCode)
	return err
}
