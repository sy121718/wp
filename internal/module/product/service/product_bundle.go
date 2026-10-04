// product_bundle.go — 捆绑品选项配置（issue #20）。
//
// 本文件是捆绑配置的**读 / 写 / 配置期校验**；整单选择校验在 product_bundle_validate.go。
//
// 三条刻意的语义：
//
//  1. **配置期校验与选择期校验分离**：配置期的错误（自相矛盾的区间、永远无法满足的整单下限）
//     是运营填错了，保存时就拒；选择期的错误（漏填必选、超可用量）是买家选错了，
//     在前台即时反馈。两套错误码分开，后台与前台才都能给出可读原因。
//  2. **配置里引用的是跨商品的已存在 SKU**（spec 第 53/54 条）：保存时逐个校验存在性、
//     同工程、非自引用；已删除的 SKU 在读取时以 Enabled=false 呈现（不静默丢项，
//     否则运营看不出自己配的东西已经失效）。
//  3. **可用量只读 inventory 真源**：经 VariantAvailabilityPort（inventory 实现、装配注入）
//     批量取数；端口未注入时整单校验 fail-closed（拿不到权威可用量就不放行），
//     绝不回退读 product_variants.stock_total 缓存。
package productservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventoryenums "go_wp/internal/module/inventory/enums"
	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/i18n"
	"go_wp/pkg/rls"
)

// bundleOrigin 捆绑配置变更的记录来源（issue #19 的 origin 口径）。
const bundleOrigin = "bundle"

// defaultBundleItemsJSON 空配置的序列化形式（与迁移 114 的列默认值同形状）。
var defaultBundleItemsJSON = mustMarshalBundleConfig(productdto.NewEmptyBundleConfig())

// mustMarshalBundleConfig 序列化配置（结构固定，失败即结构被改坏，直接暴露）。
func mustMarshalBundleConfig(cfg productdto.BundleConfig) json.RawMessage {
	raw, err := json.Marshal(cfg)
	if err != nil {
		panic("捆绑配置序列化失败: " + err.Error())
	}
	return raw
}

// normalizeBundleItems 把商品创建 / 更新路径上的 bundle_items 规范化成迁移 114 要求的对象形状。
//
// 商品主体的写入路径不承担选项规则的语义校验（那是 SetBundleConfig 的职责），
// 但必须保证**形状**合法：数据库的 CHECK 会拒绝形状不对的值，与其让整次写入
// 以 23514 失败，不如在这里给出明确结论。
//
//	空 / [] / null → 空配置（历史列默认值就是 '[]'，读取侧一直按「无捆绑」处理）
//	JSON 对象      → 补齐缺省键后重新序列化（列里始终是完整形状）
//	其它形状       → 拒绝（裸数组、字符串、数字……不静默写成脏数据）
func normalizeBundleItems(raw json.RawMessage) (out json.RawMessage, err error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == "[]" {
		return defaultBundleItemsJSON, nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return nil, errors.New(productenums.ErrBundleShapeInvalid)
	}
	var cfg productdto.BundleConfig
	if uerr := json.Unmarshal(raw, &cfg); uerr != nil {
		return nil, errors.New(productenums.ErrBundleShapeInvalid)
	}
	if cfg.MaxOptions <= 0 {
		cfg.MaxOptions = productdto.BundleDefaultMaxOptions
	}
	if cfg.Options == nil {
		cfg.Options = []productdto.BundleOption{}
	}
	return mustMarshalBundleConfig(cfg), nil
}

// decodeBundleConfig 把 products.bundle_items 解析为配置对象。
//
// 迁移 114 已把该列的形状收紧（CHECK：必须是对象且 options 是数组），因此这里的
// 兜底分支只在「手工改库」或「更早的历史行」上生效：形状不认识时按空配置处理，
// 读取侧不因脏数据报错（配置为空 = 不是捆绑品，是安全且可解释的结论）。
func decodeBundleConfig(raw json.RawMessage) (cfg productdto.BundleConfig) {
	cfg = productdto.NewEmptyBundleConfig()
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" || trimmed == "[]" {
		return cfg
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return productdto.NewEmptyBundleConfig()
	}
	if cfg.MaxOptions <= 0 {
		cfg.MaxOptions = productdto.BundleDefaultMaxOptions
	}
	if cfg.Options == nil {
		cfg.Options = []productdto.BundleOption{}
	}
	return cfg
}

// GetBundleConfig 读某商品的捆绑配置（含每项的 SKU 快照与真源可用量）。
func (s *Service) GetBundleConfig(ctx context.Context, req *productdto.GetBundleConfigReq) (res *productdto.BundleConfigResp, err error) {
	if req == nil || strings.TrimSpace(req.ProductID) == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, gerr := s.m.Get(ctx, strings.TrimSpace(req.ProductID), projectID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	return s.bundleConfigResp(ctx, e, decodeBundleConfig(e.BundleItems))
}

// SetBundleConfig 保存捆绑配置（整体替换）+ 落主数据变更记录。
func (s *Service) SetBundleConfig(ctx context.Context, req *productdto.SetBundleConfigReq) (res *productdto.BundleConfigResp, err error) {
	if req == nil || strings.TrimSpace(req.ProductID) == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, gerr := s.m.Get(ctx, strings.TrimSpace(req.ProductID), projectID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	before := decodeBundleConfig(e.BundleItems)
	cfg, verr := s.validateBundleConfig(ctx, e, req.Config)
	if verr != nil {
		return nil, verr
	}
	raw, merr := json.Marshal(cfg)
	if merr != nil {
		return nil, merr
	}
	e.BundleItems = raw
	e.UpdatedAt = time.Now().UTC()
	// 捆绑配置与它的主数据变更记录**同事务**（AGENTS.md「写操作的事务与回滚」）。
	// 留痕不再是「写完之后另起一次写」：分开提交时配置改了而时间线上没有这一笔，
	// 事后没人能说清这份配置是谁在什么时候改成这样的。
	if uerr := s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, e.ProjectID); serr != nil {
			return serr
		}
		if xerr := s.m.UpdateTx(ctx, tx, e); xerr != nil {
			return xerr
		}
		return s.recordChangesTx(ctx, tx, bundleChangeInput(e, req.OperatorID, before, cfg))
	}); uerr != nil {
		return nil, uerr
	}
	return s.bundleConfigResp(ctx, e, cfg)
}

// ListBundleSKUs 可挑选的 SKU 清单（后台配置器的下拉数据源，跨商品）。
//
// 只列启用中的变体：捆绑套餐里挂一个已停用的 SKU，前台会立刻变成「选不了又躲不开」的
// 必选项。停用要挂进套餐，得先在商品页把它启用回来。
func (s *Service) ListBundleSKUs(ctx context.Context, req *productdto.ListBundleSKUReq) (list []*productdto.BundleSKUResp, err error) {
	if req == nil {
		req = &productdto.ListBundleSKUReq{}
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	products, lerr := s.m.List(ctx, projectID, strings.TrimSpace(req.Keyword), "", maxPageSize, 0)
	if lerr != nil {
		return nil, lerr
	}
	if len(products) == 0 {
		return []*productdto.BundleSKUResp{}, nil
	}
	ids := make([]string, 0, len(products))
	nameOf := make(map[string]string, len(products))
	for _, p := range products {
		ids = append(ids, p.ID)
		nameOf[p.ID] = p.Name
	}
	variants, verr := s.m.ListVariantsByProducts(ctx, ids)
	if verr != nil {
		return nil, verr
	}
	list = make([]*productdto.BundleSKUResp, 0, len(variants))
	for _, v := range variants {
		if !v.Enabled {
			continue
		}
		list = append(list, &productdto.BundleSKUResp{
			VariantID: v.ID, SKUCode: v.SKUCode, ProductID: v.ProductID,
			ProductName: nameOf[v.ProductID], Price: v.Price, CostPrice: v.CostPrice, Enabled: v.Enabled,
		})
	}
	return list, nil
}

// validateBundleConfig 配置期校验并归一化（返回可落库的配置）。
//
// 拒绝的都是「运营填错」：区间自相矛盾、超过服务端护栏、引用不存在的 SKU、自引用，
// 以及**永远无法满足的整单下限**（下限高于所有选项能加到的最大总量 —— 这种配置一旦保存，
// 前台任何选择都会被拒，等于把商品锁死）。
func (s *Service) validateBundleConfig(ctx context.Context, product *productmodel.ProductEntity, in productdto.BundleConfig) (out productdto.BundleConfig, err error) {
	out = productdto.NewEmptyBundleConfig()
	if in.MaxOptions < 0 || in.MaxOptions > productdto.BundleMaxOptionsLimit {
		return out, errors.New(productenums.ErrBundleMaxOptionsInvalid)
	}
	if in.MaxOptions > 0 {
		out.MaxOptions = in.MaxOptions
	}
	if len(in.Options) > out.MaxOptions {
		return out, errors.New(productenums.ErrBundleOptionsExceeded)
	}
	if in.MinTotalQty < 0 || in.MinTotalQty > productdto.BundleMaxQtyLimit ||
		in.MaxTotalQty < 0 || in.MaxTotalQty > productdto.BundleMaxQtyLimit {
		return out, errors.New(productenums.ErrBundleQtyInvalid)
	}
	if in.MaxTotalQty > 0 && in.MinTotalQty > in.MaxTotalQty {
		return out, errors.New(productenums.ErrBundleTotalRangeInvalid)
	}
	out.MinTotalQty, out.MaxTotalQty = in.MinTotalQty, in.MaxTotalQty

	seen := make(map[string]bool, len(in.Options))
	for _, o := range in.Options {
		vid := strings.TrimSpace(o.VariantID)
		if vid == "" {
			return out, errors.New(productenums.ErrBundleVariantRequired)
		}
		if _, perr := uuid.Parse(vid); perr != nil {
			return out, errors.New(productenums.ErrInvalidParam)
		}
		if seen[vid] {
			return out, errors.New(productenums.ErrBundleVariantDuplicated)
		}
		seen[vid] = true

		// 来源快照随行保存（docs/14 §1.2）：只作溯源与展示，**不是身份** ——
		// 因此这里只做形状归一（未知来源拒绝、非仓库来源清空仓库字段），
		// 不回查「那条仓库 SKU 现在还在不在」：仓库侧改码不该让成员配置保存不了
		//（成员的身份是 variantId，已经在上面校验过存在性与工程归属）。
		src, serr := normalizeBundleMemberSource(o)
		if serr != nil {
			return out, serr
		}
		item := productdto.BundleOption{
			VariantID: vid, Required: o.Required,
			SourceKind: src.Kind, WarehouseID: src.WarehouseID,
			WarehouseSKU: src.WarehouseSKU, ExternalSKU: src.ExternalSKU,
		}
		if o.DefaultQty < 0 || o.MinQty < 0 || o.MaxQty < 0 ||
			o.DefaultQty > productdto.BundleMaxQtyLimit || o.MaxQty > productdto.BundleMaxQtyLimit ||
			o.MinQty > productdto.BundleMaxQtyLimit {
			return out, errors.New(productenums.ErrBundleQtyInvalid)
		}
		item.MinQty = o.MinQty
		if o.Required && item.MinQty < 1 {
			// 必选项的语义就是「至少买一件」：minQty 写 0 与必选自相矛盾，按 1 归一。
			item.MinQty = 1
		}
		item.DefaultQty = o.DefaultQty
		if item.DefaultQty < item.MinQty {
			return out, errors.New(productenums.ErrBundleQtyRangeInvalid)
		}
		item.MaxQty = o.MaxQty
		if item.MaxQty > 0 && (item.MaxQty < item.MinQty || item.MaxQty < item.DefaultQty) {
			return out, errors.New(productenums.ErrBundleQtyRangeInvalid)
		}
		out.Options = append(out.Options, item)
	}

	// 引用的 SKU 必须存在、属于同工程、且不是捆绑主体自己的 SKU（自引用）。
	if len(out.Options) > 0 {
		ids := make([]string, 0, len(out.Options))
		for _, o := range out.Options {
			ids = append(ids, o.VariantID)
		}
		variants, lerr := s.m.ListVariantsByIDs(ctx, ids, product.ProjectID)
		if lerr != nil {
			return out, lerr
		}
		if len(variants) != len(ids) {
			return out, errors.New(productenums.ErrBundleVariantNotFound)
		}
		productIDs := make([]string, 0, len(variants))
		for _, v := range variants {
			productIDs = append(productIDs, v.ProductID)
		}
		// 作用域取宿主商品的工程：跨工程的 SKU 这里本来就该「读不到」，
		// projectOf 缺失即落到下面的 ProjectMismatch 分支（与旧语义一致，只是不再靠全表读）。
		owners, oerr := s.m.ListProductsByIDs(ctx, productIDs, product.ProjectID)
		if oerr != nil {
			return out, oerr
		}
		projectOf := make(map[string]string, len(owners))
		for _, p := range owners {
			projectOf[p.ID] = p.ProjectID
		}
		for _, v := range variants {
			if v.ProductID == product.ID {
				return out, errors.New(productenums.ErrBundleSelfReference)
			}
			if projectOf[v.ProductID] != product.ProjectID {
				return out, errors.New(productenums.ErrBundleVariantProjectMismatch)
			}
		}
	}

	// 整单下限必须可达：下限 > 所有选项能加到的最大总量时永远无法满足。
	if out.MinTotalQty > 0 {
		reachable := 0
		for _, o := range out.Options {
			if o.MaxQty > 0 {
				reachable += o.MaxQty
			} else {
				// 留空 = 只受库存约束：可达性无法先验判定，按「不受配置限制」处理。
				reachable = -1
				break
			}
		}
		if reachable >= 0 && reachable < out.MinTotalQty {
			return out, errors.New(productenums.ErrBundleTotalUnreachable)
		}
	}
	// 整单上限不得低于必选项的最小量之和：低于时任何合法选择都超上限。
	if out.MaxTotalQty > 0 {
		requiredMin := 0
		for _, o := range out.Options {
			if o.Required {
				requiredMin += o.MinQty
			}
		}
		if requiredMin > out.MaxTotalQty {
			return out, errors.New(productenums.ErrBundleTotalRangeInvalid)
		}
	}
	return out, nil
}

// normalizeBundleMemberSource 归一成员来源快照（服务端不信任前端提交的来源字段）。
//
// 三值白名单：unknown → ErrBundleSourceInvalid（不静默降级成「手工指定」——
// 那会让一个拼错的来源看起来像一条正常的历史配置）。
// 非仓库来源的仓库字段一律清空：来源快照要能自解释，不能留半截字段让人误读。
func normalizeBundleMemberSource(o productdto.BundleOption) (src productdto.BundleMemberSource, err error) {
	kind := strings.TrimSpace(o.SourceKind)
	switch kind {
	case "":
		return src, nil
	case productenums.BundleSourceProduct, productenums.BundleSourceWarehouse, productenums.BundleSourceAttributes:
	default:
		return src, errors.New(productenums.ErrBundleSourceInvalid)
	}
	src.Kind = kind
	if kind != productenums.BundleSourceWarehouse {
		return src, nil
	}
	src.WarehouseID = strings.TrimSpace(o.WarehouseID)
	src.WarehouseSKU = strings.TrimSpace(o.WarehouseSKU)
	src.ExternalSKU = strings.TrimSpace(o.ExternalSKU)
	return src, nil
}

// bundleConfigResp 组装读模型：配置 + 每项 SKU 快照 + 真源可用量 + 套餐主体价。
func (s *Service) bundleConfigResp(ctx context.Context, e *productmodel.ProductEntity, cfg productdto.BundleConfig) (res *productdto.BundleConfigResp, err error) {
	res = &productdto.BundleConfigResp{
		ProductID:   e.ID,
		ProductName: e.Name,
		BasePrice:   s.bundleBasePrice(ctx, e),
		Config:      cfg,
		Options:     make([]*productdto.BundleOptionDetail, 0, len(cfg.Options)),
	}
	if len(cfg.Options) == 0 {
		return res, nil
	}
	ids := make([]string, 0, len(cfg.Options))
	for _, o := range cfg.Options {
		ids = append(ids, o.VariantID)
	}
	variants, verr := s.m.ListVariantsByIDs(ctx, ids, e.ProjectID)
	if verr != nil {
		return nil, verr
	}
	byID := make(map[string]*productmodel.VariantEntity, len(variants))
	productIDs := make([]string, 0, len(variants))
	for _, v := range variants {
		byID[v.ID] = v
		productIDs = append(productIDs, v.ProductID)
	}
	owners, oerr := s.m.ListProductsByIDs(ctx, productIDs, e.ProjectID)
	if oerr != nil {
		return nil, oerr
	}
	nameOf := make(map[string]string, len(owners))
	for _, p := range owners {
		nameOf[p.ID] = p.Name
	}
	avail, aerr := s.availableQuantities(ctx, e.ProjectID, ids)
	if aerr != nil {
		return nil, aerr
	}
	for _, o := range cfg.Options {
		d := &productdto.BundleOptionDetail{BundleOption: o}
		if v := byID[o.VariantID]; v != nil {
			d.SKUCode, d.ProductID, d.ProductName = v.SKUCode, v.ProductID, nameOf[v.ProductID]
			d.ItemPrice, d.CostPrice, d.Enabled = v.Price, v.CostPrice, v.Enabled
		}
		d.Available = avail[o.VariantID]
		res.Options = append(res.Options, d)
	}
	return res, nil
}

// availableQuantities 批量取真源可用量（端口未注入即 fail-closed）。
//
// 失败不降级、不返回 0、更不回退读缓存：可用量是「能不能卖」的依据，
// 拿不到权威值时的正确行为是拒绝，而不是猜。
func (s *Service) availableQuantities(ctx context.Context, projectID string, variantIDs []string) (out map[string]int, err error) {
	if s.availability == nil {
		return nil, errors.New(productenums.ErrBundleStockUnavailable)
	}
	if len(variantIDs) == 0 {
		return map[string]int{}, nil
	}
	return s.availability.AvailableQuantities(ctx, projectID, variantIDs)
}

// bundleBasePrice 套餐价 = 主体自定价：商品级默认价优先，缺失时退回最低启用变体价。
//
// 「子项价格不参与前台展示」是套装语义的一半：另一半是套餐自身的价格必须确定。
func (s *Service) bundleBasePrice(ctx context.Context, e *productmodel.ProductEntity) float64 {
	if e.DefaultPrice != nil {
		return *e.DefaultPrice
	}
	variants, err := s.m.ListVariants(ctx, e.ID)
	if err != nil {
		return 0
	}
	best, found := 0.0, false
	for _, v := range variants {
		if !v.Enabled {
			continue
		}
		if !found || v.Price < best {
			best, found = v.Price, true
		}
	}
	return best
}

// —— 捆绑成员的三种来源（docs/14 §1.2，批次 C）——
//
// 与变体清单的「预览—保存」模型同一形态（docs/14 §8）：本段**一个字节都不写库**，
// 只把候选行交回前端清单；写入的唯一入口仍是 SetBundleConfig。
//
// 三条共用的口径（三者必须一致，否则同一个成员会因来路不同而行为不同）：
//
//	· **去重**：同一变体在成员清单里只出现一次（库里的既有成员 + 本批已解析的 + 前端清单里的）；
//	· **单条失败不整批失败**：某一条解析不出来（组合在商品侧没有变体 / 该仓没有这条 SKU /
//	  变体已停用 / 超上限）只跳过它，逐条回带原因（沿用本模块的「成功 N / 跳过 M」口径）；
//	· **服务端不信任前端**：组合由服务端按属性组固定顺序重算，仓库 SKU 一律回该仓复核，
//	  前端提交的 variantId 只在「去重」这一件事上被当输入。
//
// 成员的身份恒为 variantId（uuid）：来源快照（warehouseId / warehouseSku / externalSku）
// 只作溯源与展示 —— 仓库换码、商品换仓都不影响成员引用。

// BundleMemberResolveLimit 单次解析的候选上限（仓库来源一次提交的仓库 SKU 条数上限）。
//
// 商品来源与属性来源的上限另有出处：前者受变体总数约束，后者复用 MaxVariantCombinations；
// 这一条管的是「前端一次能交上来多少条仓库 SKU」—— 没有上限就等于把循环次数交给请求方。
const BundleMemberResolveLimit = 200

// ResolveBundleMembers 解析一批候选成员（不落库；写入只发生在 SetBundleConfig）。
func (s *Service) ResolveBundleMembers(ctx context.Context, req *productdto.ResolveBundleMembersReq) (res *productdto.ResolveBundleMembersResp, err error) {
	if req == nil || strings.TrimSpace(req.ProductID) == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	container, cerr := s.m.Get(ctx, strings.TrimSpace(req.ProductID), projectID)
	if cerr != nil {
		return nil, mapNotFound(cerr)
	}
	source := strings.TrimSpace(req.Source)
	switch source {
	case productenums.BundleSourceProduct, productenums.BundleSourceWarehouse, productenums.BundleSourceAttributes:
	default:
		return nil, errors.New(productenums.ErrBundleSourceInvalid)
	}
	// 上限取该容器自己的配置（运营设的 maxOptions）：解析时就把超出的候选逐条列出来，
	// 而不是先追加一屏、等保存时再整批报「选项数超过上限」。
	maxOptions := decodeBundleConfig(container.BundleItems).MaxOptions
	if maxOptions <= 0 || maxOptions > productdto.BundleMaxOptionsLimit {
		maxOptions = productdto.BundleDefaultMaxOptions
	}
	res = &productdto.ResolveBundleMembersResp{
		ProductID: container.ID, Source: source,
		Members: []*productdto.BundleMemberDraft{}, Skipped: []productdto.BundleMemberSkip{},
	}
	// 去重集合：前端清单里已有的 + 本批已解析出来的（服务端自己维护，前端传的形状不作数）。
	seen := make(map[string]bool, len(req.ExistingVariantIDs)+maxOptions)
	for _, id := range req.ExistingVariantIDs {
		if id = strings.TrimSpace(id); id != "" {
			seen[id] = true
		}
	}
	// add 收编一条候选：去重 / 上限 / 停用都在这里判，跳过的逐条记原因（不静默）。
	// v 为 nil 表示「这一组规格在商品侧没有对应变体」（属性来源的唯一形态）。
	add := func(v *productmodel.VariantEntity, src productdto.BundleMemberSource, optRaw json.RawMessage) {
		res.Total++
		if v == nil {
			res.Skipped = append(res.Skipped, productdto.BundleMemberSkip{
				Reason: productenums.BundleMemberNotOnProduct, OptionValues: optRaw,
			})
			return
		}
		switch {
		case seen[v.ID]:
			res.Skipped = append(res.Skipped, bundleMemberSkipOf(v, optRaw, productenums.BundleMemberSkippedInList))
			return
		case len(res.Members) >= maxOptions:
			res.Skipped = append(res.Skipped, bundleMemberSkipOf(v, optRaw, productenums.BundleMemberOptionsExceeded))
			return
		case !v.Enabled:
			res.Skipped = append(res.Skipped, bundleMemberSkipOf(v, optRaw, productenums.BundleMemberVariantDisabled))
			return
		}
		seen[v.ID] = true
		res.Members = append(res.Members, &productdto.BundleMemberDraft{
			VariantID: v.ID, SKUCode: v.SKUCode, ProductID: v.ProductID, Enabled: v.Enabled,
			OptionValues: orJSON(v.OptionValues, "{}"),
			Source:       src,
		})
	}

	switch source {
	case productenums.BundleSourceProduct:
		// ① 从商品导入：选中一个商品 → 其全部（启用）变体一次导入为成员。
		src, serr := s.bundleSourceProduct(ctx, projectID, container.ID, req.SourceProductID)
		if serr != nil {
			return nil, serr
		}
		variants, verr := s.m.ListVariants(ctx, src.ID)
		if verr != nil {
			return nil, verr
		}
		for _, v := range variants {
			if v == nil {
				continue
			}
			add(v, productdto.BundleMemberSource{Kind: source}, orJSON(v.OptionValues, "{}"))
		}

	case productenums.BundleSourceWarehouse:
		// ② 从仓库选：按 (仓库, 仓库 SKU) 定位那条库存行 → 它的 variant_id 就是成员身份。
		// inventory_stocks.variant_id 是 NOT NULL，所以这条来路一定能定位到变体 ——
		// 本实现刻意不造「无变体的成员」。
		if s.invSvc == nil {
			return nil, errors.New(productenums.ErrBundleStockUnavailable)
		}
		warehouseID := strings.TrimSpace(req.WarehouseID)
		skus := normalizeBundleWarehouseSKUs(req.WarehouseSKUs)
		if warehouseID == "" || len(skus) == 0 {
			return nil, errors.New(productenums.ErrBundleSourceWarehouseRequired)
		}
		if len(skus) > BundleMemberResolveLimit {
			skus = skus[:BundleMemberResolveLimit]
		}
		for _, code := range skus {
			picked, perr := s.invSvc.GetWarehouseSKU(ctx, &inventorycontract.GetWarehouseSKUReq{
				ProjectID: projectID, WarehouseID: warehouseID, SKUCode: code,
			})
			if perr != nil {
				// 只有「这个仓里没有这条货」算逐条跳过；其余错误（仓库不存在等）原样抛出，
				// 不能把它们一起吞成「这条 SKU 没有」。
				if strings.TrimSpace(perr.Error()) != inventoryenums.ErrWarehouseSKUNotFound {
					return nil, perr
				}
				res.Total++
				res.Skipped = append(res.Skipped, productdto.BundleMemberSkip{
					WarehouseID: warehouseID, WarehouseSKU: code,
					Reason: productenums.BundleMemberWarehouseSKUMissing,
				})
				continue
			}
			src := productdto.BundleMemberSource{
				Kind: source, WarehouseID: warehouseID,
				WarehouseSKU: picked.SKUCode, ExternalSKU: picked.ExternalSKU,
			}
			v, gerr := s.m.GetVariant(ctx, picked.VariantID)
			if gerr != nil {
				res.Total++
				res.Skipped = append(res.Skipped, productdto.BundleMemberSkip{
					WarehouseID: warehouseID, WarehouseSKU: code,
					Reason: productenums.BundleMemberWarehouseSKUMissing,
				})
				continue
			}
			// 自引用：那条货就是我们自己这个捆绑容器的变体（同 SetBundleConfig 的口径）。
			if v.ProductID == container.ID {
				res.Total++
				res.Skipped = append(res.Skipped, bundleMemberSkipOf(v, nil, productenums.ErrBundleSelfReference))
				continue
			}
			add(v, src, orJSON(v.OptionValues, "{}"))
		}

	case productenums.BundleSourceAttributes:
		// ③ 自选属性值笛卡尔积：勾选进服务端，组合由服务端按**属性组固定顺序**重算，
		// 且只接受「商品侧确实存在对应变体」的组合 —— 不存在的逐条拒绝并说明，
		// 绝不静默丢弃、也不造无变体成员。
		src, serr := s.bundleSourceProduct(ctx, projectID, container.ID, req.SourceProductID)
		if serr != nil {
			return nil, serr
		}
		attrs, aerr := s.m.ListAttributesByIDs(ctx, decodeStrings(src.AttributeIDs), projectID)
		if aerr != nil {
			return nil, aerr
		}
		// 与变体生成 / 预览共用同一套维度归一与上限保护（同一个组合在两边必须是同一个结论）。
		dims, derr := buildVariationDimensions(src, attrs, req.Selections)
		if derr != nil {
			return nil, derr
		}
		total := combinationCount(dims)
		if total > MaxVariantCombinations {
			return nil, fmt.Errorf("%s：%s", productenums.ErrVariationCountLimit,
				i18n.ErrorDetail(productenums.DetailVariationCountExceed,
					"n", strconv.Itoa(total), "max", strconv.Itoa(MaxVariantCombinations)))
		}
		variants, verr := s.m.ListVariants(ctx, src.ID)
		if verr != nil {
			return nil, verr
		}
		byKey := make(map[string]*productmodel.VariantEntity, len(variants))
		for _, v := range variants {
			byKey[optionKey(decodeOptionPairs(v.OptionValues))] = v
		}
		for _, pairs := range expandCombinations(dims) {
			raw := encodeOptionPairs(pairs)
			add(byKey[optionKey(pairs)], productdto.BundleMemberSource{Kind: source}, raw)
		}
	}

	if err = s.fillBundleMemberProductNames(ctx, projectID, res.Members); err != nil {
		return nil, err
	}
	return res, nil
}

// bundleSourceProduct 解析来源商品（product / attributes 两种来源共用）。
//
// 自引用在请求层面就拒（来源商品就是捆绑容器自己）—— 与 SetBundleConfig 的自引用判定同一口径，
// 只是这里能更早地给出结论，不必等保存。
func (s *Service) bundleSourceProduct(ctx context.Context, projectID, containerID, sourceProductID string) (p *productmodel.ProductEntity, err error) {
	id := strings.TrimSpace(sourceProductID)
	if id == "" {
		return nil, errors.New(productenums.ErrBundleSourceProductRequired)
	}
	if id == containerID {
		return nil, errors.New(productenums.ErrBundleSelfReference)
	}
	p, err = s.m.Get(ctx, id, projectID)
	if err != nil {
		return nil, mapNotFound(err)
	}
	return p, nil
}

// normalizeBundleWarehouseSKUs 归一仓库 SKU 编码：去空白、去重、保持提交顺序。
//
// 空串一律丢掉（表单里的空选项不是一条货）；重复的只在解析层留一条，
// 去重后的「重复」由上层按「已在清单里」逐条回带，不在这里静默吞掉。
func normalizeBundleWarehouseSKUs(codes []string) (out []string) {
	out = make([]string, 0, len(codes))
	seen := make(map[string]bool, len(codes))
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, code)
	}
	return out
}

// bundleMemberSkipOf 组装「跳过一条成员」的回带信息（原因取 enums 常量 = i18n key）。
func bundleMemberSkipOf(v *productmodel.VariantEntity, optRaw json.RawMessage, reason string) productdto.BundleMemberSkip {
	out := productdto.BundleMemberSkip{Reason: reason, OptionValues: optRaw}
	if v != nil {
		out.VariantID, out.SKUCode = v.ID, v.SKUCode
	}
	return out
}

// fillBundleMemberProductNames 回填候选成员的商品名（一次批量取，避免逐行查）。
func (s *Service) fillBundleMemberProductNames(ctx context.Context, projectID string, drafts []*productdto.BundleMemberDraft) (err error) {
	if len(drafts) == 0 {
		return nil
	}
	ids := make([]string, 0, len(drafts))
	seen := make(map[string]bool, len(drafts))
	for _, d := range drafts {
		if d == nil || d.ProductID == "" || seen[d.ProductID] {
			continue
		}
		seen[d.ProductID] = true
		ids = append(ids, d.ProductID)
	}
	owners, oerr := s.m.ListProductsByIDs(ctx, ids, projectID)
	if oerr != nil {
		return oerr
	}
	nameOf := make(map[string]string, len(owners))
	for _, p := range owners {
		nameOf[p.ID] = p.Name
	}
	for _, d := range drafts {
		if d == nil {
			continue
		}
		d.ProductName = nameOf[d.ProductID]
	}
	return nil
}

// bundleChangeInput 组装捆绑配置的变更记录输入（issue #19：origin=bundle）。
func bundleChangeInput(e *productmodel.ProductEntity, operator string, before, after productdto.BundleConfig) *masterdatacontract.ChangeInput {
	if e == nil {
		return nil
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	return &masterdatacontract.ChangeInput{
		ProjectID:   e.ProjectID,
		EntityType:  "product",
		EntityID:    e.ID,
		EntityLabel: e.Name,
		Action:      masterdataenums.ActionUpdate,
		Origin:      bundleOrigin,
		OperatorID:  operator,
		Before: masterdatacontract.NewSnapshot(
			"bundle_items", masterdatacontract.FormatJSON(beforeJSON),
		),
		After: masterdatacontract.NewSnapshot(
			"bundle_items", masterdatacontract.FormatJSON(afterJSON),
		),
	}
}
