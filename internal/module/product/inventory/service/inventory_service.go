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
// #18 采购单与入库：采购单（来源 = #17 的货源）→ 收货入库（复用 #16 的 ChangeStock）。
// 入库一律经同一套变动契约写库存真源（理由 / 流水 / 来源引用齐全），绝不旁路写库存；
// 登记入库在采购单行锁内原子递增已入库数量并重算推导状态，幂等键挡住重复入库。
package inventoryservice

import (
	"context"
	"errors"
	"strings"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	productcontract "go_wp/internal/module/product/contract"
	inventorycontract "go_wp/internal/module/product/inventory/contract"
	inventorydto "go_wp/internal/module/product/inventory/dto"
	inventoryenums "go_wp/internal/module/product/inventory/enums"
	inventorymodel "go_wp/internal/module/product/inventory/model"
	"go_wp/pkg/utils"
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
	// 未注入时变动的缓存同步记失败台账（真源仍然成功），对账则显式报错。
	// 依赖方向 inventory → product：本模块调商品模块的缓存端口，
	// 商品模块实现的库存记录端口则由顶层反向注入（两端口互不干扰）。
	// variantCost 商品侧**成本价**写回端口（issue #18，由 product 模块实现）。
	// 采购入库 / 生产入库登记后（库存变动已提交）经它把单价写进
	// product_variants.cost_price；未注入时按回写失败记在入库单行上（cost_error），
	// 不回滚已经落地的真源库存。依赖方向同样是 inventory → product。
	variantCost productcontract.VariantCostPort
	// changes 主数据变更记录端口（issue #19，由 masterdata 模块实现）。
	// 货源资料的字段级变更（编码 / 类型 / 关联方 / 结算价 / 状态 / 对接配置）经它留痕；
	// 未注入时静默跳过（纯库存单测路径），生产装配恒注入。
	changes masterdatacontract.MasterDataService
}

// NewService 构造。
func NewService(m *inventorymodel.Model, project projectcontract.ProjectService) *Service {
	return &Service{m: m, project: project}
}

// SetStockCache 注入商品侧库存缓存端口（issue #16，装配期调用）。
//
// 注入时机在商品模块装配之后（缓存端口的实现属商品模块），
// 与 product.SetVariantStock 同一模式：可选依赖不进构造参数。

// SetVariantCost 注入商品侧成本价写回端口（issue #18，装配期调用）。
//
// 与 SetStockCache 同一模式：端口实现属商品模块，故在商品模块装配之后注入。
func (s *Service) SetVariantCost(port productcontract.VariantCostPort) {
	s.variantCost = port
}

// SetMasterDataChanges 注入主数据变更记录端口（issue #19，装配期调用）。
//
// 与 SetStockCache / SetVariantCost 同一模式：可选依赖不进构造参数。
// 依赖方向 inventory → masterdata（本模块只把货源资料的前后快照递过去）。
func (s *Service) SetMasterDataChanges(port masterdatacontract.MasterDataService) {
	s.changes = port
}

// 编译期断言：本模块契约 + 仍保留的跨模块能力。
// （issue #32：归属仓解析与库存记录生成不再走端口 —— 商品与库存同模块，商品用例直接调本服务。）
var (
	_ inventorycontract.InventoryService = (*Service)(nil)
	// 库存真源可用量端口（issue #20）：捆绑品的数量上限与整单下限读它，而不是读展示缓存。
	_ productcontract.VariantAvailabilityPort = (*Service)(nil)
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
	inPage, inSize := 0, 0
	if req != nil {
		inPage, inSize = req.Page, req.Size
	}
	paging := utils.NormalizePaging(inPage, inSize, defaultPageSize, maxPageSize)
	return paging.Page, paging.Size
}
