package inventoryservice

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

// 本文件是库存模块与 masterdata 模块之间的**唯一适配面**：货源资料（编码 / 名称 /
// 类型 / 关联方 / 结算价 / 状态 / 对接配置）的字段级留痕在这里定义白名单与格式化，
// masterdata 只做 diff 与落库。
//
// 与库存流水的分工（验收 5）：货源资料是**配置**，改动进主数据变更记录；
// 数量增减进库存流水（inventory_stock_movements）。同一张货源的「停用」与
// 「入库 +10」分别落在两处，谁都不替代谁。

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"go_wp/internal/module/inventory/contract"
	"go_wp/internal/module/inventory/dto"
	"go_wp/internal/module/inventory/enums"
	"go_wp/internal/module/inventory/model"
	"go_wp/internal/module/masterdata/contract"
	"go_wp/internal/module/masterdata/enums"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/module/project/contract"
	"go_wp/pkg/utils"
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
	//
	// 成本的主载体自批次 A 起是**仓库侧**（inventory_stocks.cost_price，(仓库, SKU)
	// 的当前值，迁移 244），它在 ChangeStock 的事务里随库存一起写好、不依赖本端口；
	// 本端口是商品侧的兼容写回（未注入不影响仓库侧成本）。
	variantCost productcontract.VariantCostPort
	// changes 主数据变更记录端口（issue #19，由 masterdata 模块实现）。
	// 货源资料的字段级变更（编码 / 类型 / 关联方 / 结算价 / 状态 / 对接配置）经它留痕；
	// 未注入时静默跳过（纯库存单测路径），生产装配恒注入。
	changes masterdatacontract.MasterDataService
	// cipherSecret 敏感配置加密密钥（仓库第三方对接凭据，迁移 240）。
	//
	// 装配期由 inbound 从 config.yaml 的 app.secret 读入后注入（模块自己不读配置，
	// 与 mail / webhook 同一模式）。为空时不写明文凭据，而是明确报错 ——
	// 「没有密钥」是配置问题，不该降级成「凭据明文落库」。
	cipherSecret string
}

// NewService 构造。
func NewService(m *inventorymodel.Model, project projectcontract.ProjectService) *Service {
	return &Service{m: m, project: project}
}

// SetStockCache 注入商品侧库存缓存端口（issue #16，装配期调用）。
//
// 注入时机在商品模块装配之后（缓存端口的实现属商品模块），
// 与 product.SetVariantStock 同一模式：可选依赖不进构造参数。

// SetCipherSecret 注入敏感配置加密密钥（装配期从 config 的 app.secret 读入后调用）。
//
// 未注入时第三方仓的凭据无法加密：写入明文凭据会被拒绝（ErrWarehouseCredentialKeyMissing），
// 走引用名（secretRef）的路径不受影响 —— 系统只记名字，不记值。
func (s *Service) SetCipherSecret(secret string) { s.cipherSecret = secret }

// SetVariantCost 注入商品侧成本价写回端口（issue #18，装配期调用；**必须注入**）。
//
// 装配自检（审计 CQ-019）：判为 required-port —— 为空时采购收货 / 生产入库的单价
// 不回写 cost_price，入库单行只记一条 cost_error，库存真源照常变动（不报错）。
// 与 SetStockCache 同一模式：端口实现属商品模块，故在商品模块装配之后注入。
func (s *Service) SetVariantCost(port productcontract.VariantCostPort) {
	s.variantCost = port
}

// SetMasterDataChanges 注入主数据变更记录端口（issue #19，装配期调用；**必须注入**）。
//
// 装配自检（审计 CQ-019）：判为 required-port —— 为空时货源资料的字段级变更
// 静默跳过留痕（recordChanges 直接 return nil），审计缺记录且不报错。
// 依赖方向 inventory → masterdata（本模块只把货源资料的前后快照递过去）；
// 装配方 routes.go 对 product / inventory 两处 setter 同一轮断言 + 注入。
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

// sourceChangeSnapshot 货源主数据的字段白名单快照。
//
// 只有结构化列进快照：类型 / 关联方 / 结算价 / 状态都是报表与采购单要按它们取数或
// 校验的字段，改一次必须留痕；对接配置（config）是自由形状的 JSON，
// 用压缩后的文本参与 diff（等价 JSON 不产生假记录）。
func sourceChangeSnapshot(e *inventorymodel.SourceEntity) masterdatacontract.FieldSnapshot {
	if e == nil {
		return nil
	}
	return masterdatacontract.NewSnapshot(
		"code", e.Code,
		"name", e.Name,
		"type", e.Type,
		"related_party", masterdatacontract.FormatBool(e.RelatedParty),
		"settle_price", masterdatacontract.FormatPricePtr(e.SettlePrice),
		"status", e.Status,
		"config", masterdatacontract.FormatJSON(e.Config),
		"sort", masterdatacontract.FormatInt(e.Sort),
	)
}

// sourceChangeInput 组装货源级变更输入。
func sourceChangeInput(e *inventorymodel.SourceEntity, action, operator string,
	before, after masterdatacontract.FieldSnapshot) *masterdatacontract.ChangeInput {
	if e == nil {
		return nil
	}
	return &masterdatacontract.ChangeInput{
		ProjectID: e.ProjectID, EntityType: masterdataenums.EntityInventorySource,
		EntityID: e.ID, EntityLabel: e.Name,
		Action: action, Origin: masterdataenums.OriginSource, OperatorID: operator,
		Before: before, After: after,
	}
}

// recordChanges 记录主数据变更（端口未注入时空转：纯库存单测路径）。
//
// **新代码优先用 recordChangesTx**：业务行与留痕必须同事务（见下）。本方法留给
// 「这次写操作只有留痕一处写入」、或确实无法进事务的路径。
func (s *Service) recordChanges(ctx context.Context, inputs ...*masterdatacontract.ChangeInput) (err error) {
	if s.changes == nil || len(inputs) == 0 {
		return nil
	}
	return s.changes.RecordChanges(ctx, inputs)
}

// recordChangesTx 在**调用方事务内**记录主数据变更（端口未注入时空转，与上面同口径）。
//
// 与 recordChanges 只差一个参数：写的是调用方的 tx。这个差别是本质的 ——
// master_data_changes 是 append-only（迁移 111 的触发器拒绝 UPDATE / DELETE）：
// 业务行提交了而留痕没提交，等于**永久缺一条审计**，事后只能靠人工补一条「说明性」记录；
// 反过来则是「审计里有、业务没变」。所以只要有主实体写，留痕就必须在同一事务里。
// 先例：masterdata.RecordChangesTx（product 模块的三处写路径同此形状）。
func (s *Service) recordChangesTx(ctx context.Context, tx *gorm.DB, inputs ...*masterdatacontract.ChangeInput) (err error) {
	if s.changes == nil || len(inputs) == 0 {
		return nil
	}
	return s.changes.RecordChangesTx(ctx, tx, inputs)
}
