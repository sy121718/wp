// Package productservice 商品模块业务实现（issue #5 / T3a）。
//
// 边界：本模块只管商品与变体。库存事实仅通过 inventory 契约访问；
// 订单、购物车、客户等仍由各自模块负责，跨模块只走 contract，不 import 对方 service/model。
//
// 两条已定语义在本文件落地：
//  1. 商品主体不存价格 —— 价格全在变体上，商品侧的「价格区间」是从变体派生的只读结果；
//  2. 商品级字段是「新增变体时的默认值模板」，仅新增路径逐字段判空后填充，
//     编辑路径一个字都不动（含调用方主动清空字段）；空值以 NULL 判定，0 与 false 视为已填。
package productservice

import (
	"context"
	"errors"
	"strings"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
	inventorycontract "go_wp/internal/module/inventory/contract"
	productmodel "go_wp/internal/module/product/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/rls"

	"gorm.io/gorm"
)

const (
	defaultPageSize = 20
	maxPageSize     = 200
)

// Service 商品模块业务实现。
//
// 只持有本模块 model 与 project 契约；不持有 *gorm.DB。
type Service struct {
	m       *productmodel.Model
	project projectcontract.ProjectService
	// inv 只读库存事实：查询期投影与删除守卫共用同一份库存真源。
	// 生产装配必需；未注入时保留纯商品单测的零值路径。
	inv inventorycontract.ProductStockReader
	// contentStore 内容译文读取端口（装配期注入，可空）。
	// 构建期商品可翻译字段（name/subtitle/description）按构建语言取译文；
	// 未注入 / 语言为空 / 查询失败一律回退原文（兜底铁律，绝不报错）。
	contentStore i18n.ContentStore
	// invSvc 是商品创建/查询所需的有限库存能力；Tx 写入透传同一数据库事务。
	// 生产装配必需；未注入时保留纯商品单测的零值路径。
	invSvc inventorycontract.ProductStockPort
	// availability 库存真源可用量端口（issue #20，由 inventory 模块实现）。
	// 捆绑品的数量上限与整单下限都受可用量约束，且只看真源、绝不读展示缓存；
	// 未注入时整单校验 fail-closed（返回 ErrBundleStockUnavailable），不按「无限制」放行。
	availability productcontract.VariantAvailabilityPort
	// changes 主数据变更记录端口（issue #19，由 masterdata 模块实现）。
	// 商品 / 变体的关键字段变更经它留痕（append-only）；
	// 未注入时静默跳过（纯商品单测路径），生产装配恒注入。
	changes masterdatacontract.MasterDataService
	// publishedLocator 实体 → 已上线详情页路径（BIZ-2 / EDT-012）。
	// 集合项 url 字段只填真实已发布路径；未注入时留空（预览无站点上下文时正常）。
	publishedLocator presentationcontract.PublishedEntityLocator
	// archiveEnsurer 归档页按需创建（审计 EDT-004）：新建 / 改名分类时让归档页跟上。
	// 未注入时分类照常保存（归档页是派生视图，不是分类的一部分）。
	archiveEnsurer presentationcontract.ArchiveInstanceEnsurer
	// fragmentCacheBumper 商品写操作后使运行时片段 HTML 缓存失效（PERF-002）。
	fragmentCacheBumper func(context.Context, string)
	// invalidator 静态产物失效端口（审计 ARCH-01，装配期注入，可空）。
	//
	// 与 fragmentCacheBumper 是**两条不同的链**：那个刷的是运行时片段 HTML 的
	// Redis 版本号，这个把事件交给发布内核去标记静态产物 stale 并重建。
	// 只接前者正是 ARCH-01 的口径差（片段刷新了 ≠ 详情页与列表页的静态产物更新了）。
	invalidator productcontract.DependencyInvalidator
}

// SetInventory 注入库存真源只读契约；漏接会静默跳过投影和删除守卫，装配期必须自检。
func (s *Service) SetInventory(reader inventorycontract.ProductStockReader) { s.inv = reader }

// NewService 构造商品用例。
func NewService(m *productmodel.Model, project projectcontract.ProjectService) *Service {
	return &Service{m: m, project: project}
}

// SetContentStore 注入内容译文读取端口（装配期调用，与其它模块的
// SetDependencyInvalidator / SetSourceResolver 同模式：可选依赖不进构造参数）。
// 传入 nil 表示不翻译（构建期商品字段输出原文）。
func (s *Service) SetContentStore(store i18n.ContentStore) {
	s.contentStore = store
}

// SetInventoryService 注入库存受限契约；生产装配必须确认端口与实现均已接入。
func (s *Service) SetInventoryService(port inventorycontract.ProductStockPort) {
	s.invSvc = port
}

// SetAvailabilityPort 注入库存真源可用量端口（issue #20，装配期调用）。
//
// 与 SetVariantStock 同一模式：端口定义在本模块契约、实现在 inventory 模块，
// 由顶层装配注入（依赖方向 inventory → product）。
func (s *Service) SetAvailabilityPort(port productcontract.VariantAvailabilityPort) {
	s.availability = port
}

// SetMasterDataChanges 注入主数据变更记录端口（issue #19，装配期调用）。
//
// 与 SetVariantStock 同一模式：可选依赖不进构造参数，装配期由顶层注入
// masterdata 模块的实现（依赖方向 product → masterdata）。
func (s *Service) SetMasterDataChanges(port masterdatacontract.MasterDataService) {
	s.changes = port
}

// SetArchiveInstanceEnsurer 注入归档页创建端口（装配期调用，审计 EDT-004）。
//
// **必须注入**（审计 CQ-019 判为 required-port）：为空时分类新建 / 改名后归档页
// 不跟上，线上仍是旧路径 —— 静默，没有日志也没有报错。装配方 routes.go 断言失败即 panic。
func (s *Service) SetArchiveInstanceEnsurer(port presentationcontract.ArchiveInstanceEnsurer) {
	s.archiveEnsurer = port
}

// SetPublishedEntityLocator 注入已上线详情页路径解析端口（装配期调用）。
//
// **必须注入**（审计 CQ-019 判为 required-port）：为空时商品集合项的 url 字段全空，
// 列表页商品没有链接 —— 页面能渲染、也不报错，只是导出不去。
// 同一个端口注入给片段层（runtimefragment）那处一直是 fail-fast，这里对齐。
func (s *Service) SetPublishedEntityLocator(port presentationcontract.PublishedEntityLocator) {
	s.publishedLocator = port
}

// SetFragmentCacheBumper 注入片段缓存失效回调（装配期调用）。
//
// **必须注入**（审计 CQ-019 判为 required-port）：为空时商品写操作后片段 HTML 缓存
// 不失效，前台继续显示**过期价** —— 缓存里那份 HTML 看起来完全正常。
// 注入源是包级函数 runtimefragment.BumpFragmentCacheVersion，恒可得，故断言成本为零。
func (s *Service) SetFragmentCacheBumper(fn func(context.Context, string)) {
	s.fragmentCacheBumper = fn
}

func (s *Service) bumpFragmentCache(ctx context.Context, projectID string) {
	if s.fragmentCacheBumper != nil && strings.TrimSpace(projectID) != "" {
		s.fragmentCacheBumper(ctx, projectID)
	}
}

// 编译期契约断言。
var (
	_ productcontract.ProductService = (*Service)(nil)
	// 构建期数据源（issue #35）：组件取商品数据只走这个受限接口，
	// 写方法不在它上面。
	_ productcontract.ProductDataSource = (*Service)(nil)
	// 成本价写回端口（issue #18）：库存模块经它把入库单价写进 product_variants.cost_price。
	_ productcontract.VariantCostPort = (*Service)(nil)
)

// transactionScope 在**调用方已开启的事务**上设置工程作用域（工程 id 为空时跳过）。
//
// 为什么允许空 id 跳过：纯商品单测路径（未注入留痕端口）拿不到工程上下文，
// 而 product_variants 本就没有 RLS 策略 —— 为它编一个工程 id 只会把「没作用域」
// 伪装成「有作用域」。涉及 products（有 FORCE 策略）的路径工程 id 必非空，
// 空串在那里是编程错误，由 rls 直接拒掉，不在这里兜。
func transactionScope(tx *gorm.DB, projectID string) error {
	if strings.TrimSpace(projectID) == "" {
		return nil
	}
	return rls.ScopeTx(tx, projectID)
}

// resolveProjectID 解析工程：显式指定优先，否则取唯一工程。
func (s *Service) resolveProjectID(ctx context.Context, projectID string) (id string, err error) {
	if projectID != "" {
		return projectID, nil
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", errors.New(productenums.ErrInvalidParam)
	}
	return list[0].ID, nil
}
