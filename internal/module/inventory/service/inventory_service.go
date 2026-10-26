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

import (
	"context"
	"strings"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
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
	cipherSecret string
	// refs 变体 → 仓库货行的引用读口（迁移 637 / 641）。
	//
	// 641 之后「这条库存行属于哪个变体」的真源是 product_variants.warehouse_skus（JSON 数组），
	// inventory_stocks.variant_id 退化成写入当时留下的线索列 —— 同一变体在同一仓可以有多条
	// 货行，一条货行也可以被多个变体引用。所以按变体汇总（可用量 / 成本 / 出库分配）
	// 必须经这个口取 id 清单，不能再用 variant_id 过滤。
	//
	// 允许为 nil：纯库存单测路径下没有商品域。生产装配必注入（缺它就答不出「这个变体
	// 有哪些货」）—— 汇总方法在 nil 时返回空结果，与「没有任何货行」同形，
	// 而那只影响展示投影，可用量判定仍由写路径的行锁说了算。
	refs productcontract.VariantWarehouseStockPort
	// attachments 附件读口（调拨凭证的存在性校验与展示，迁移 630 W3）。
	//
	// 允许为 nil：纯库存单测路径下不校验存在性（只保留 id）。生产装配必注入 ——
	// 缺它时「凭证引用的附件是否存在」就无从判起，而那是凭证要求的全部意义。
	attachments inventorycontract.AttachmentResolver
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

// SetAttachmentResolver 注入附件读口（迁移 630 W3，装配期调用；生产必注入）。
//
// 未注入时调拨的凭证校验退化成「只保留 id、不校验存在性」——
// 那是纯库存单测的形态，不是生产可接受的降级。
func (s *Service) SetAttachmentResolver(resolver inventorycontract.AttachmentResolver) {
	if s == nil {
		return
	}
	s.attachments = resolver
}

// SetVariantWarehouseStocks 注入变体 → 货行的引用读口（装配期调用，生产必注入）。
//
// 未注入时汇总类方法返回空结果（与「没有任何货行」同形），只影响展示投影；
// 可用量判定仍由写路径的行锁说了算，不靠这个口兜底。
func (s *Service) SetVariantWarehouseStocks(refs productcontract.VariantWarehouseStockPort) {
	if s == nil {
		return
	}
	s.refs = refs
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
		return "", inventoryenums.Err(inventoryenums.ErrInvalidParam)
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
