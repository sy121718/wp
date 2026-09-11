// Package inventoryservice inventory 模块业务实现（issue #15 / #16）。
//
// 边界：本模块管仓库实体与库存**真源**，以及围绕真源的全部变动能力：
//
//	#15  仓库实体 + 「SKU × 仓库」库存记录（新建变体自动生成初始 0 的一行）；
//	#16  按 SKU 增减（真源行锁）、库存流水、变动原因字典、物料清单展开扣减、
//	     商品侧缓存同步与对账。
//
// 死线：一切影响可用量的判断只读 inventory_stocks（在行锁之内），绝不读
// product_variants.stock_total 那个列表展示缓存 —— 缓存只被同步 / 对账。
package inventoryservice

import (
	"context"
	"errors"
	"strings"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

const (
	// defaultPageSize / maxPageSize 库存记录列表分页。
	defaultPageSize = 20
	maxPageSize     = 200
	// maxWarehouseCodeLen 仓库短码长度上限（SKU 编码前缀，过长会把编码挤爆）。
	maxWarehouseCodeLen = 8
)

// Service 仓库与库存业务实现。
//
// 只持有本模块 model 与外部契约；不持有 *gorm.DB。
type Service struct {
	m       *inventorymodel.Model
	project projectcontract.ProjectService
	// stockCache 商品侧库存缓存的读写端口（issue #16，由 product 模块实现）。
	// 未注入时变动的缓存同步记失败台账（真源仍然成功），对账则显式报错。
	// 依赖方向 inventory → product：本模块调商品模块的缓存端口，
	// 商品模块实现的库存记录端口则由顶层反向注入（两端口互不干扰）。
	stockCache productcontract.VariantStockCachePort
}

// NewService 构造。
func NewService(m *inventorymodel.Model, project projectcontract.ProjectService) *Service {
	return &Service{m: m, project: project}
}

// SetStockCache 注入商品侧库存缓存端口（issue #16，装配期调用）。
//
// 注入时机在商品模块装配之后（缓存端口的实现属商品模块），
// 与 product.SetVariantStock 同一模式：可选依赖不进构造参数。
func (s *Service) SetStockCache(port productcontract.VariantStockCachePort) {
	s.stockCache = port
}

// 编译期断言：本模块契约 + 商品模块定义的变体库存端口（依赖方向 inventory → product）。
var (
	_ inventorycontract.InventoryService = (*Service)(nil)
	_ productcontract.VariantStockPort   = (*Service)(nil)
)

// resolveProjectID 解析工程：显式指定优先，否则取唯一工程。
func (s *Service) resolveProjectID(ctx context.Context, projectID string) (id string, err error) {
	if strings.TrimSpace(projectID) != "" {
		return projectID, nil
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", errors.New(inventoryenums.ErrInvalidParam)
	}
	return list[0].ID, nil
}

// pageArgs 归一化分页参数。
func pageArgs(req *inventorydto.ListStockReq) (page, size int) {
	page, size = 1, defaultPageSize
	if req == nil {
		return page, size
	}
	if req.Page > 0 {
		page = req.Page
	}
	if req.Size > 0 {
		size = req.Size
		if size > maxPageSize {
			size = maxPageSize
		}
	}
	return page, size
}
