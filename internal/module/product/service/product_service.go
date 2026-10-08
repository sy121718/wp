package productservice

// Package productservice 商品模块业务实现（issue #5 / T3a）。
//
// 边界：本模块只管商品与变体。库存事实仅通过 inventory 契约访问；
// 订单、购物车、客户等仍由各自模块负责，跨模块只走 contract，不 import 对方 service/model。
//
// 两条已定语义在本文件落地：
//  1. 商品主体不存价格 —— 价格全在变体上，商品侧的「价格区间」是从变体派生的只读结果；
//  2. 商品级字段是「新增变体时的默认值模板」，仅新增路径逐字段判空后填充，
//     编辑路径一个字都不动（含调用方主动清空字段）；空值以 NULL 判定，0 与 false 视为已填。

// 为什么要把「有没有自己的 SKU / 价格放在哪」显式成一个类型：
// 捆绑品此前只是「bundle_items 非空的商品」，系统分不清主体卖自己的 SKU 与容器卖组合。
// 后果是创建路径无差别地给捆绑品生成了一个价 0 的首个变体，而价格区间只从变体派生 ——
// 捆绑商品在列表里显示 0.00（运营看到的现象）。
//
// 定了类型之后的三条不变量：
//   · bundle 不生成首个变体（容器没有自己的 SKU），库存由成员变体按 BOM 扣减；
//   · bundle 只有一个对外价格 = 容器价（products.default_price），创建时必须 > 0；
//   · 成员价不参与任何对外金额与展示（成本合计保留在后台口径）。

// 本文件是商品模块与 masterdata 模块之间的**唯一适配面**：
//
//   - 商品侧定义自己的字段白名单（商品 / 变体各一份快照），masterdata 只做 diff 与落库；
//   - 端口（masterdatacontract.MasterDataService）在装配期注入，未注入时静默跳过
//     （纯商品单测路径，与 variantStock 端口同一手法）——生产装配恒注入。
//
// 覆盖的验收字段（issue #19 验收 2）：
//
//	变体默认发货仓 —— 建变体时解析出的归属仓（home_warehouse_id / home_warehouse_code）；
//	SKU 编码       —— 建 / 改 / 删变体都会落行；
//	商品价格       —— 变体售价（含定价工具批量改价与入库成本价回写）；
//	上下架状态     —— 商品 status（draft / published / …）；
//	货源资料       —— 见 inventory 模块的 source 快照。

// 要修的口径差：商品主写路径（Create/Update/Delete + 变体 + 定价 + 分类/品牌/标签）
// 此前只调 bumpFragmentCache —— 那只是 Redis 里运行时片段 HTML 的版本号，与静态产物
// 字节毫无关系。于是「改标题 / 改价 / 上下架 / 分类 / 增删商品」之后，详情页与集合
// 列表页的静态产物可以一直是旧字节，且任何日志都不会提示。
//
// 链路（与 content 模块的同名链路逐字对齐）：
//
//	商品写事务内写 outbox 行（project + entity + revision + 依赖键）
//	  → 消费者按批领取（租约 + SKIP LOCKED）
//	  → 窄端口 DependencyInvalidator（装配层注入 pipeline.Fanout）
//	  → page / presentation 的 MarkStaleByDependency → RebuildStale
//
// 三个不变量的落点：
//  1. **事务回滚不产生事件**：行与商品聚合写在同一个事务里（enqueueInvalidationTx 只
//     接受调用方的事务句柄），没有第二处写入、也没有「写完再补偿」；
//  2. **消费者崩溃可重放**：领取只推进 claimed_time / attempts，租约到期后同一批会被
//     重新领取；事件直到被成功消费才写 processed_time；
//  3. **幂等**：失效动作本身幂等（标记 stale 是幂等 UPDATE；重建按当前数据重算），
//     同一批被投递两次不会累积副作用 —— 回归见 public/test/product/feature 的
//     product_outbox_dependency_test.go。

// 这个文件只做三件事：
//
//	① 取工程清单（跨工程扫描的枚举源）；
//	② 把一次扫描结论编成**可定位**的明细文案（哪张表 / 命中多少 / 涉及哪些工程与商品）；
//	③ 命中时的统一错误出口。
//
// 扫描本身在 model（productmodel.CrossProjectRef / ProductRefsByXxx），
// 它按工程逐个设置 RLS 作用域取并集 —— 为什么这样写、换非超级角色后是否仍然成立，
// 见 model/product_ref_scan.go 的文件头（结论：成立，与连接身份无关）。
//
// 口径（AGENTS.md「冲突与数据不一致一律打回给人」）：
//   - **禁止自动清理**：命中即拒绝删除，绝不替调用方解绑；
//   - **禁止静默放过**：拿不到工程清单 / 扫描出错一律向上返回，不用空清单当「没有引用」；
//   - **禁止自动改名 / 加后缀**：错误里列出可定位的数据，由人决定怎么处置。
//
// 为什么明细要卡字节预算：错误经提示页（shell.RenderJump）整页呈现，正文太长会挤掉
// 「立即前往」的链接与其它提示 —— 明细写太长反而让可定位信息读不到。因此明细必须有硬上限。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/inventory/contract"
	"go_wp/internal/module/masterdata/contract"
	"go_wp/internal/module/masterdata/enums"
	"go_wp/internal/module/presentation/contract"
	"go_wp/internal/module/product/contract"
	"go_wp/internal/module/product/dto"
	"go_wp/internal/module/product/enums"
	"go_wp/internal/module/product/model"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/rls"
	"go_wp/pkg/upload"
	"go_wp/pkg/utils"
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
	// purchases 购买事实只读端口（「买过才能评」的输入，由 order 模块实现；可缺）。
	//
	// 未注入 = 该规则未启用（放行），判据见 product_comment_policy.go 与
	// productcontract.PurchaseChecker 的注释。刻意是**收窄的是非题端口**而不是
	// OrderService：商品只需要一个答案，拿不到订单的读写能力。
	purchases productcontract.PurchaseChecker
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

// pageArgs 归一化分页参数。
func pageArgs(req *productdto.ListReq) (page, size int) {
	inPage, inSize := 0, 0
	if req != nil {
		inPage, inSize = req.Page, req.Size
	}
	paging := utils.NormalizePaging(inPage, inSize, defaultPageSize, maxPageSize)
	return paging.Page, paging.Size
}

// optionalPaging 归一「可选分页」参数，返回 model 层接受的 (limit, offset)。
//
// 与 pageArgs 的关键差别：**两个入参都是零值时返回 (0, 0) = 不分页**。
// 品牌 / 标签的 Page、Size 是后加的可选字段，同一个请求类型上并存两种调用形态：
//
//	· 显式分页 —— 后台列表页给出 Page/Size，要的是「当页 + 总数」；
//	· 全量取数 —— 集合源筛选选项、内容翻译候选、规则重算的取数来源都传零值，
//	  它们要的是全部行。这里若按 pageArgs 那样兜底成「第 1 页 20 条」，
//	  表现为品牌筛选下拉少了一批选项、重算回执少了一批标签 —— 不报错、只是少了。
//
// 所以「分页」必须是调用方的显式意图，而不是模型层的默认行为。
func optionalPaging(page, size int) (limit, offset int) {
	if page <= 0 && size <= 0 {
		return 0, 0
	}
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}
	return size, (page - 1) * size
}

// normalizeSlug 规范化传入 slug（小写 + 去首尾空白）。
func normalizeSlug(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, " ", "-")
	return strings.ToLower(s)
}

// deriveSlug 由商品名派生 slug：保留字母数字、其余转连字符；中文名派生为空，由调用方兜底。
func deriveSlug(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// orJSON jsonb 列的兜底值。
func orJSON(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(fallback)
	}
	return raw
}

// orJSONList 字符串数组转 jsonb（空数组写 []）。
func orJSONList(items []string) json.RawMessage {
	if items == nil {
		return json.RawMessage("[]")
	}
	b, err := json.Marshal(items)
	if err != nil {
		return json.RawMessage("[]")
	}
	return b
}

// orIDList id 数组转 jsonb。
func orIDList(items []string) json.RawMessage { return orJSONList(items) }

// decodeStrings jsonb 数组 → 字符串切片（失败返回空切片，不阻断读取）。
// mediaURL 媒体地址 → 对外可用的完整链接。
//
// 商品域里凡是「取自媒体库」的字段（images / defaultImage / 变体 image /
// 分类 image / 品牌 logo）都要过它：媒体库给的是 upload.base_url 前缀下的地址，
// 未配置时是 "/storage/<id>.<ext>"。写入口归一（新数据天然完整），读出口再归一
// （存量相对值也显示对）—— 两侧共用 pkg/upload 的同一个实现，避免各写一份前缀拼接。
//
// 非媒体地址（外链 CDN / data: URI）由 StorageURL 原样返回，不会被改写。
func mediaURL(u string) string { return upload.StorageURL(u) }

// mediaURLs 逐个归一图片数组；空数组返回空数组（不是 nil），
// 免得调用方在 JSON 里拿到 null 又要各自兜一次。
func mediaURLs(list []string) []string {
	out := make([]string, 0, len(list))
	for _, u := range list {
		if s := mediaURL(u); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func decodeStrings(raw json.RawMessage) (out []string) {
	out = []string{}
	if len(raw) == 0 {
		return out
	}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []string{}
	}
	return out
}

// normalizeProductType 归一化商品类型：空默认 variant，其余仅接受 variant / bundle。
func normalizeProductType(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "", productmodel.TypeVariant:
		return productmodel.TypeVariant, nil
	case productmodel.TypeBundle:
		return productmodel.TypeBundle, nil
	default:
		return "", errors.New(productenums.ErrProductTypeInvalid)
	}
}

// product_translate.go — service 层取词函数的传递通道（后台展示文案专用）。
//
// 背景：本模块的 service 要产出**展示文案**（内置定价 / 标签规则的展示名与描述、
// 筛选条件与状态的可读标签），而它不是纯数据 —— 英文界面上必须显示英文。
// service 层没有语言上下文（语言来自后台 Cookie / Accept-Language，只有 gin.Context
// 知道），因此取词函数只能由 inbound 传进来。
//
// 为什么用 ctx 而不是给每个方法加参数：
//   - 这些文案散布在多层内部函数里（pricingStatusLabel → setPricingLineStatus →
//     computePricingLines → PreviewPricing），逐层加到 contract 方法签名会把
//     `tr func(...)` 写进一堆与展示无关的接口（预览、应用、留痕列表、抽屉数据…）；
//   - 传播方向是单向的（inbound → service → 内部函数），与 ctx 的语义一致；
//   - 未注入时的行为是**返回中文兜底**（不是裸 key），与 i18n 未初始化时的降级一致，
//     所以漏注入不会把页面打坏，只是不翻译。
//
// 写入侧只有一处：inbound 的 productTranslateMiddleware（路由组中间件），
// 它把 shell.TranslateFor(c) 放进 Request 的 Context —— handler 里
// `ctx := c.Request.Context()` 拿到的就是带取词函数的 ctx，无需逐调用点改动。
type translateCtxKey struct{}

// TranslateFunc 取词函数签名（与 shell.TranslateFor(c) 返回的函数同型）。
type TranslateFunc func(key, fallback string) string

// WithTranslate 把取词函数放进 ctx（inbound 中间件调用）。
//
// tr 为 nil 时原样返回 ctx（不写入坏值）。
func WithTranslate(ctx context.Context, tr TranslateFunc) context.Context {
	if ctx == nil || tr == nil {
		return ctx
	}
	return context.WithValue(ctx, translateCtxKey{}, tr)
}

// translateFrom 取当前请求的取词函数；未注入时返回**恒等函数**。
//
// 恒等函数的语义与 pkg/i18n.Translate 在词条缺失时一致：返回调用点给的中文兜底
// （兜底为空才退回 key）—— service 的每条文案都在调用点带中文兜底，因此未注入时
// 页面显示的就是中文原文，而不是 `admin.product_pricing.rule.costMultiple.name`
// 这种裸 key。
func translateFrom(ctx context.Context) TranslateFunc {
	if ctx != nil {
		if tr, ok := ctx.Value(translateCtxKey{}).(TranslateFunc); ok && tr != nil {
			return tr
		}
	}
	return func(key, fallback string) string {
		if fallback != "" {
			return fallback
		}
		return key
	}
}

// productChangeSnapshot 商品主数据的字段白名单快照。
//
// 只收「主数据」：名称 / URL 段 / 上下架状态 / 商品级默认售价 / 品牌。
// 版本元数据（update_time、sort）与派生值（价格区间）一律不进快照 ——
// 进了就会每次写操作都产生一串没有信息量的记录。
func productChangeSnapshot(e *productmodel.ProductEntity) masterdatacontract.FieldSnapshot {
	if e == nil {
		return nil
	}
	return masterdatacontract.NewSnapshot(
		"name", e.Name,
		"slug", e.Slug,
		"status", e.Status,
		"default_price", masterdatacontract.FormatPricePtr(e.DefaultPrice),
		"brand_id", masterdatacontract.FormatStringPtr(e.BrandID),
	)
}

// variantChangeSnapshot 变体主数据的字段白名单快照。
//
// ref 非 nil 时才写「默认发货仓」两个字段：变体的默认发货仓在商品侧没有独立列
// （由 SKU 编码前缀与库存记录行表达），取值只能来自**建变体时**解析出的归属仓。
// 编辑路径拿不到它，于是不写这两个键 —— 否则会把「这次操作根本没涉及该字段」
// 误记成「默认发货仓被清空了」。
func variantChangeSnapshot(v *productmodel.VariantEntity, ref *productcontract.WarehouseRef) masterdatacontract.FieldSnapshot {
	if v == nil {
		return nil
	}
	snap := masterdatacontract.NewSnapshot(
		"sku_code", v.SKUCode,
		"barcode", v.Barcode,
		"price", masterdatacontract.FormatPrice(v.Price),
		"compare_price", masterdatacontract.FormatPricePtr(v.ComparePrice),
		"cost_price", masterdatacontract.FormatPricePtr(v.CostPrice),
		"enabled", masterdatacontract.FormatBool(v.Enabled),
		"option_values", masterdatacontract.FormatJSON(v.OptionValues),
	)
	if ref != nil {
		snap["home_warehouse_id"] = ref.ID
		snap["home_warehouse_code"] = ref.Code
	}
	return snap
}

// variantProjectID 变体所属工程（变更记录按工程隔离，变体行只有 product_id）。
//
// 只在留痕端口已注入时才多查一次商品：未注入的纯商品单测路径一个多余的查询都不发。
// scopeID 是调用方给的工程作用域：products 表有 RLS 策略，反查本身也需要作用域
// （详见 pkg/rls 与 AGENTS.md 的 DB-009 段）。
func (s *Service) variantProjectID(ctx context.Context, v *productmodel.VariantEntity, scopeID string) (projectID string, err error) {
	if s.changes == nil || v == nil {
		return "", nil
	}
	p, gerr := s.m.Get(ctx, v.ProductID, scopeID)
	if gerr != nil {
		return "", mapNotFound(gerr)
	}
	return p.ProjectID, nil
}

// recordChanges 记录主数据变更（端口未注入时空转：纯商品单测路径）。
//
// 写入发生在业务写操作**之后**：跨模块写不进同一个事务（表隔离约定），
// 端口失败即把错误透出给调用方，不静默丢记录。
func (s *Service) recordChanges(ctx context.Context, inputs ...*masterdatacontract.ChangeInput) (err error) {
	if s.changes == nil || len(inputs) == 0 {
		return nil
	}
	return s.changes.RecordChanges(ctx, inputs)
}

func (s *Service) recordChangesTx(ctx context.Context, tx *gorm.DB, inputs ...*masterdatacontract.ChangeInput) (err error) {
	if s.changes == nil || len(inputs) == 0 {
		return nil
	}
	return s.changes.RecordChangesTx(ctx, tx, inputs)
}

// productChangeInput 组装商品级变更输入。
func productChangeInput(e *productmodel.ProductEntity, action, origin, operator string,
	before, after masterdatacontract.FieldSnapshot) *masterdatacontract.ChangeInput {
	if e == nil {
		return nil
	}
	return &masterdatacontract.ChangeInput{
		ProjectID: e.ProjectID, EntityType: masterdataenums.EntityProduct,
		EntityID: e.ID, EntityLabel: e.Name,
		Action: action, Origin: origin, OperatorID: operator,
		Before: before, After: after,
	}
}

// variantChangeInput 组装变体级变更输入。
func variantChangeInput(projectID string, v *productmodel.VariantEntity, action, origin, operator string,
	before, after masterdatacontract.FieldSnapshot) *masterdatacontract.ChangeInput {
	if v == nil {
		return nil
	}
	return &masterdatacontract.ChangeInput{
		ProjectID: projectID, EntityType: masterdataenums.EntityProductVariant,
		EntityID: v.ID, EntityLabel: v.SKUCode,
		Action: action, Origin: origin, OperatorID: operator,
		Before: before, After: after,
	}
}

// 消费者参数。
const (
	// outboxBatchMax 单次领取上限：批太大时一次扇出会拖住消费者协程，
	// 批太小则空转；200 与依赖表写入的批大小（CreateInBatches 200）同一量级。
	outboxBatchMax = 200
	// outboxLease 领取租约：超过它未完成即视为消费者已崩溃，可被重新领取。
	// 取 5 分钟：正常消费是「标记 + 触发重建（入队或同步）」的秒级动作，
	// 5 分钟足够覆盖一次慢重建，又不至于让崩溃后的事件等太久。
	outboxLease = 5 * time.Minute
	// outboxDispatchInterval 消费者轮询间隔（装配层默认值）。
	outboxDispatchInterval = 15 * time.Second
)

// SetDependencyInvalidator 注入依赖失效扇出窄端口（装配期调用；可空）。
//
// 为空时：事件照常落库（不丢事实），消费者**不标记处理**，等装配补齐后可重放 ——
// 这正是「静默降级」要防的形态：少了它，商品改了而站点永不更新，且没有报错。
func (s *Service) SetDependencyInvalidator(inv productcontract.DependencyInvalidator) {
	if s == nil {
		return
	}
	s.invalidator = inv
}

// invalidationTarget 一次变更涉及的实体（商品 / 分类 / 品牌 / 标签 / 属性）。
type invalidationTarget struct {
	EntityType string
	EntityID   string
}

// productInvalidationTarget 商品实体的失效目标（最常用的一条）。
func productInvalidationTarget(productID string) invalidationTarget {
	return invalidationTarget{EntityType: productcontract.EntityTypeProduct, EntityID: productID}
}

// membershipDiff 两个成员集合的**对称差**（升序）——「归属变了」的那些实体。
//
// 用在自动标签归属重算上：只重建归属真的变过的商品，而不是整集合都重建一遍。
func membershipDiff(before, after []string) []string {
	inBefore := make(map[string]bool, len(before))
	for _, id := range before {
		if id = strings.TrimSpace(id); id != "" {
			inBefore[id] = true
		}
	}
	inAfter := make(map[string]bool, len(after))
	changed := make([]string, 0, len(after))
	for _, id := range after {
		if id = strings.TrimSpace(id); id != "" {
			inAfter[id] = true
			if !inBefore[id] {
				changed = append(changed, id)
			}
		}
	}
	for _, id := range before {
		if id = strings.TrimSpace(id); id != "" && !inAfter[id] {
			changed = append(changed, id)
		}
	}
	sort.Strings(changed)
	return changed
}

// enqueueInvalidationTx 在**调用方的事务内**写 outbox 行。
//
// 每个目标实体写两类键：
//   - direct_content:{type}:{id} —— 直接引用该实体的产物（商品详情页 / 归档详情）；
//   - content_collection:collection:content:product —— 商品集合（列表页 / 归档列表页）
//     的成员或成员可见字段变化。**任何商品域实体的变更都要发这条键**：列表项里
//     内嵌了商品的名称 / 价格 / 分类品牌标签展示名，改其中任何一个都会改列表字节；
//     而新增 / 删除成员时旧产物里根本没有新实体，只能靠集合键失效
//     （与 content_service.go 的 notifyContentChanged 同一口径）。
//
// projectID 为空（纯单测路径未解析出工程）时跳过：product_outbox_events.project_id
// 是 NOT NULL，编一个工程 id 比跳过更难排查。
func (s *Service) enqueueInvalidationTx(ctx context.Context, tx *gorm.DB, projectID string, targets ...invalidationTarget) error {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" || tx == nil || len(targets) == 0 {
		return nil
	}
	now := time.Now().UTC()
	rows := make([]productmodel.OutboxEventEntity, 0, len(targets)*2)
	seen := map[[2]string]bool{}
	collectionEmitted := false
	for _, t := range targets {
		t.EntityType = strings.TrimSpace(t.EntityType)
		t.EntityID = strings.TrimSpace(t.EntityID)
		key := [2]string{t.EntityType, t.EntityID}
		if t.EntityType == "" || t.EntityID == "" || seen[key] {
			continue
		}
		seen[key] = true
		rev, err := s.m.NextOutboxRevisionTx(ctx, tx, t.EntityType, t.EntityID)
		if err != nil {
			return err
		}
		direct := pipeline.DirectContentKey(t.EntityType, t.EntityID)
		keys := []pipeline.DepKey{direct}
		// 集合键按批去重：它表达的是「商品集合整体变了」，与具体是哪个商品无关，
		// 一批变更发一次即可（消费者按键去重，多发只是多几行无意义的行）。
		if !collectionEmitted {
			collectionEmitted = true
			keys = append(keys, pipeline.ContentCollectionKey(productcontract.EntityTypeProduct))
		}
		for _, k := range keys {
			rows = append(rows, productmodel.OutboxEventEntity{
				ProjectID:      projectID,
				EntityType:     t.EntityType,
				EntityID:       t.EntityID,
				EntityRevision: rev,
				DependencyKind: k.Kind,
				DependencyKey:  k.Key,
				CreateTime:     now,
			})
		}
	}
	return s.m.AppendOutboxTx(ctx, tx, rows)
}

// enqueueProductInvalidationTx 商品变更的快捷入口（单商品）。
func (s *Service) enqueueProductInvalidationTx(ctx context.Context, tx *gorm.DB, projectID, productID string) error {
	return s.enqueueInvalidationTx(ctx, tx, projectID, productInvalidationTarget(productID))
}

// DispatchOutbox 领取并消费一批事件，返回本批处理的条数。
//
// 单批内按 (kind,key) 去重后再交给端口：扇出本身是聚合语义，同一批里同一个键
// 发一次与发 N 次结果相同（重复标记是幂等的 UPDATE）。
func (s *Service) DispatchOutbox(ctx context.Context, limit int) (int, error) {
	if s == nil || s.m == nil {
		return 0, nil
	}
	if limit <= 0 || limit > outboxBatchMax {
		limit = outboxBatchMax
	}
	rows, err := s.m.ClaimPendingOutbox(ctx, outboxLease, limit)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	if s.invalidator == nil {
		// 端口未装配：**不标记处理**，事件留在表里等装配补齐后重放。
		return 0, nil
	}
	seen := map[[2]string]bool{}
	done := make([]int64, 0, len(rows))
	for _, row := range rows {
		k := [2]string{row.DependencyKind, row.DependencyKey}
		if !seen[k] {
			seen[k] = true
			// 端口契约：永不返回错误（失败只记日志），因此这里不需要错误分支。
			s.invalidator.Invalidate(ctx, row.DependencyKind, row.DependencyKey)
		}
		done = append(done, row.ID)
	}
	if err := s.m.MarkOutboxProcessed(ctx, done); err != nil {
		// 没标记成功 = 这批会被重新领取（幂等），不打回内容写入。
		return 0, err
	}
	return len(rows), nil
}

// StartOutboxWorker 启动 outbox 消费协程（进程内 goroutine + ticker）。
//
// 端口未注入时直接返回 —— 空转的协程除了刷日志没有任何作用。
// 循环体单独成函数（runOutboxLoop）：装配期入口只做「是否该起协程」的判断，
// 消费节奏与退出的实现细节留在循环里，读的人不必在一个函数里同时看两件事。
func (s *Service) StartOutboxWorker(ctx context.Context, interval time.Duration) {
	if s == nil || s.invalidator == nil {
		return
	}
	if interval <= 0 {
		interval = outboxDispatchInterval
	}
	go s.runOutboxLoop(ctx, interval)
}

// runOutboxLoop 消费循环：首跑先做一次再按 ticker 等间隔（与 webhook 重放调度同一形态）——
// 进程重启后积压的事件立刻被消化，不必等一个轮询间隔。
func (s *Service) runOutboxLoop(ctx context.Context, interval time.Duration) {
	s.dispatchOutboxBatch(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.dispatchOutboxBatch(ctx)
		}
	}
}

// dispatchOutboxBatch 消费一批并记录结果（循环里的单次动作）。
func (s *Service) dispatchOutboxBatch(ctx context.Context) {
	n, err := s.DispatchOutbox(ctx, outboxBatchMax)
	if err != nil {
		logger.Scene("product").Error(err, "商品依赖事件消费失败（事件保留待重试）")
		return
	}
	if n > 0 {
		logger.Scene("product").With("count", n).Info("商品依赖事件已扇出")
	}
}

// maxRenameFanoutProducts 实体改名时「逐引用商品」扇出的单次上限。
//
// 为什么需要护栏：一个分类被几万个商品引用时，一次改名的逐商品事件会把 outbox 与随后的
// 重建量推到不可控（每个商品一条 direct_content + 一次详情页重建）。上限之内**逐条发**，
// 超限时**按上限逐个发**并记结构化日志 —— 宁可多、不可漏：
//
//	· 「多」的代价是几条多余的重建（产物字节没变时构建是幂等的，不新增产物行）；
//	· 「漏」的代价是商品详情页永远显示旧名字，且站点上没有任何信号。
//
// 上限本身也写进日志，截断这件事因此可见、可排查，不会被当成「全站都重建过了」。
const maxRenameFanoutProducts = 5000

// enqueueEntityRenameFanout 实体改名（分类 / 品牌 / 标签 / 属性组）的失效扇出。
//
// 键由两部分组成：
//  1. 实体键 direct_content:{type}:{id} —— 绑定该实体本身的产物；
//  2. 逐**引用该实体**的商品的 direct_content:product:{id} —— 商品详情页登记的是
//     商品键，只发实体键命中不到它，于是「改了分类名，商品页仍是旧名」且无报错。
//     （列表页由 enqueueInvalidationTx 内部按批补的商品集合键覆盖。）
//
// referencedProductIDs 由调用方在**同一事务内**经 ProductIDsByX 取全量 id（不是采样，
// 见 product_ref_scan.go 的注释）；本函数只做上限截断与入队。
func (s *Service) enqueueEntityRenameFanout(ctx context.Context, tx *gorm.DB, projectID, entityType, entityID string,
	referencedProductIDs []string) error {
	if len(referencedProductIDs) > maxRenameFanoutProducts {
		logger.Scene("product").With("entity_type", entityType).With("entity_id", entityID).
			With("referencing", len(referencedProductIDs)).With("limit", maxRenameFanoutProducts).
			Warn("实体改名牵涉的商品数超过单次扇出上限：按上限逐个发失效事件（超出部分不在本次失效范围内）")
		referencedProductIDs = referencedProductIDs[:maxRenameFanoutProducts]
	}
	targets := make([]invalidationTarget, 0, len(referencedProductIDs)+1)
	targets = append(targets, invalidationTarget{EntityType: entityType, EntityID: entityID})
	for _, pid := range referencedProductIDs {
		targets = append(targets, productInvalidationTarget(pid))
	}
	return s.enqueueInvalidationTx(ctx, tx, projectID, targets...)
}

// refGuardRefSamples 明细里最多列出的商品样本数。
const refGuardRefSamples = 2

// refDetailBudget 明细部分的字节预算（**当前实现不再逐级收敛**，保留常量说明上限）。
//
// 整条回执 = 业务文案（最长的一条约 60 字节）+ "：" + 明细，必须留在
// shell.NoticeMaxBytes（512）以内。改成「词条骨架 + 纯数据样本」之后不再需要收敛：
// 样本形如 `商品id(SKU)@工程id`（每条约 30 字节），中文词条 ~90 字节 + 两条样本 ~60
// ≈ 150，英文 ≈ 210 —— 都远在上限以内。
const refDetailBudget = 380

// refDetailProjectIDs 明细里最多列出的工程 id 数（工程 id 只出现在商品样本的 `@` 之后，
// 清单本身不再单独列出，数量由词条里的 {projects} 参数给出）。
const refDetailProjectIDs = 2

// crossProjectRefScope 跨工程引用检查的工程枚举（全站工程 id）。
//
// 工程清单来自 project 契约的 List()：product model 不读别的模块的表，
// 这里拿到的 id 列表就是扫描的枚举源。
//
// 拿不到清单时**报错**而不是返回空：空清单会让扫描结论是「没有任何引用」——
// 那正是本次要消灭的静默放过形态。
func (s *Service) crossProjectRefScope(ctx context.Context) (projectIDs []string, err error) {
	if s.project == nil {
		return nil, errors.New("product: 跨工程引用检查需要工程清单，但 project 契约未注入")
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, p := range list {
		if id := strings.TrimSpace(p.ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("product: 跨工程引用检查拿不到任何工程（projects 为空）")
	}
	return ids, nil
}

// crossProjectRefs 跑一次跨工程引用扫描：工程清单在这里取，守卫只提供扫描函数。
func (s *Service) crossProjectRefs(ctx context.Context, scan func(projectIDs []string) (*productmodel.CrossProjectRef, error)) (*productmodel.CrossProjectRef, error) {
	scope, err := s.crossProjectRefScope(ctx)
	if err != nil {
		return nil, err
	}
	return scan(scope)
}

// crossProjectRefBlocked 命中引用时的统一出口（业务 key + 可定位明细）。
//
// 形态与 ErrRelatedInvalid 一致：key 与明细用全角冒号连接，读侧 productErrText 会把
// key 翻成当前语言、明细原样接在后面；错误识别走 productErrKey 的前缀匹配。
func crossProjectRefBlocked(key string, ref *productmodel.CrossProjectRef) error {
	if detail := refGuardDetail(ref); detail != "" {
		return fmt.Errorf("%s：%s", key, detail)
	}
	return errors.New(key)
}

// refGuardDetail 跨工程引用的可定位明细。
//
// 内容是「引用面（表.列）+ 命中商品数 + 涉及工程数 + 商品样本」，整句由词条
// （productenums.DetailRefGuardBlocked）承载、样本作为纯数据参数传入 ——
// 读侧（productErrText）取词并填 {columns} / {n} / {projects} / {refs}，
// 于是英文界面上这句也是英文。
func refGuardDetail(ref *productmodel.CrossProjectRef) string {
	if !ref.Referenced() {
		return ""
	}
	return i18n.ErrorDetail(productenums.DetailRefGuardBlocked,
		"columns", strings.Join(ref.RefColumns, " + "),
		"n", strconv.Itoa(ref.Total),
		"projects", strconv.Itoa(len(ref.ProjectIDs)),
		"refs", refGuardRefs(ref))
}

// refGuardRefs 命中的商品样本（**纯数据**，人才能定位到哪个工程去解绑）。
//
// 形态 `商品id(SKU)@工程id`：SKU 只在有值时带上（运营按编码找人），存量空串只报 id。
// 「工程」二字不在这里出现 —— 骨架文案（含样本格式的说明）在词条里。
func refGuardRefs(ref *productmodel.CrossProjectRef) string {
	if len(ref.Products) == 0 {
		return ""
	}
	listed := len(ref.Products)
	if listed > refGuardRefSamples {
		listed = refGuardRefSamples
	}
	parts := make([]string, 0, listed)
	for _, p := range ref.Products[:listed] {
		if sku := strings.TrimSpace(p.SKUCode); sku != "" {
			parts = append(parts, p.ProductID+"("+sku+")@"+p.ProjectID)
			continue
		}
		parts = append(parts, p.ProductID+"@"+p.ProjectID)
	}
	return strings.Join(parts, "、")
}
