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
	"strings"
	"time"

	"github.com/google/uuid"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
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
	if uerr := s.m.Update(ctx, e); uerr != nil {
		return nil, uerr
	}
	// 留痕在写操作之后（跨模块写不进同一事务）：配置变了就在商品的时间线上留一行。
	if rerr := s.recordChanges(ctx, bundleChangeInput(e, req.OperatorID, before, cfg)); rerr != nil {
		return nil, rerr
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

		item := productdto.BundleOption{VariantID: vid, Required: o.Required}
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
		variants, lerr := s.m.ListVariantsByIDs(ctx, ids)
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
		owners, oerr := s.m.ListProductsByIDs(ctx, productIDs)
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
	variants, verr := s.m.ListVariantsByIDs(ctx, ids)
	if verr != nil {
		return nil, verr
	}
	byID := make(map[string]*productmodel.VariantEntity, len(variants))
	productIDs := make([]string, 0, len(variants))
	for _, v := range variants {
		byID[v.ID] = v
		productIDs = append(productIDs, v.ProductID)
	}
	owners, oerr := s.m.ListProductsByIDs(ctx, productIDs)
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
