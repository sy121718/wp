// product_pricing.go — 定价工具（issue #13）。
//
// 本票五条验收的落点：
//
//  1. 四种定价规则（成本乘倍数 / 成本加价 / 目标毛利率 / 统一售价）+ 四种尾数处理，
//     规则与尾数都只有一份内置注册表（product_pricing_rule.go），不接受自由表达式；
//  2. 作用范围三种：单个 SKU（变体 id）/ 单个商品的全部变体（商品 id）/ 筛选集
//     （工程 + 状态 / 关键词 / 分类 / 品牌 / 标签，命中商品的**全部**变体）；
//  3. 结果**落库**：算出的售价写回 product_variants.price，不是运行时计算 ——
//     构建期（实体类型解析器 / 集合源）读到的是落库后的确定值；
//  4. 应用前可预览（PreviewPricing 与 ApplyPricing 共用同一份算价逻辑，预览不写库），
//     应用有留痕（批次 + 逐变体「原价 → 新价」明细，含规则、尾数、范围、操作人、备注）；
//  5. 不进构建管线：本文件只依赖本模块 model，不导入 builder / pipeline / artifact，
//     算价结果对构建期而言就是一次普通的商品数据变更（由发布流程决定何时重建）。
//
// 边界：定价只改售价（price）。成本价（cost_price）与划线价（compare_price）都不动 ——
// 它们分别是规则的输入与营销展示字段，改价工具替用户改它们是越权。
package productservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
)

const (
	// defaultPricingAdjustments 留痕列表默认条数；maxPricingAdjustments 为其上限。
	defaultPricingAdjustments = 20
	maxPricingAdjustments     = 200
	// maxPricingAdjustmentItems 单个批次的明细展示上限（只有详情接口会用到）。
	maxPricingAdjustmentItems = 500
	// maxPricingProducts 筛选集单批命中的商品数上限。
	// 批量改价是不可逆的经营动作（虽然有留痕），一次改几百个商品已经足够，
	// 超过就要求调用方把筛选条件收紧 —— 比「默默改了一万个 SKU」诚实。
	maxPricingProducts = 500
)

// pricingTarget 参与试算的最小单元：一个变体 + 它所属的商品。
type pricingTarget struct {
	product *productmodel.ProductEntity
	variant *productmodel.VariantEntity
}

// pricingRequest 归一后的定价请求（预览与应用共用）。
type pricingRequest struct {
	rule       *pricingRule
	ruleParams json.RawMessage
	rounding   string
	scope      string
	targetID   string
	projectID  string
	filter     productmodel.PricingFilter
	filterJSON json.RawMessage
}

// PreviewPricing 按规则试算并返回逐变体明细（**不落库、不留痕**）。
//
// 预览与应用走同一份算价逻辑（normalizePricingRequest → resolvePricingTargets →
// computePricingLines），因此预览里看到的每个数字就是应用后会写进库的数字。
func (s *Service) PreviewPricing(ctx context.Context, req *productdto.PricingPreviewReq) (res *productdto.PricingPreviewResp, err error) {
	if req == nil {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	pr, err := normalizePricingRequest(&req.PricingRuleReq)
	if err != nil {
		return nil, err
	}
	targets, err := s.resolvePricingTargets(ctx, pr)
	if err != nil {
		return nil, err
	}
	lines, counts, err := s.computePricingLines(ctx, pr, targets)
	if err != nil {
		return nil, err
	}
	return &productdto.PricingPreviewResp{
		RuleType:       pr.rule.Type,
		RuleParams:     pr.ruleParams,
		RuleLabel:      pr.rule.Describe(pr.ruleParams),
		Rounding:       pr.rounding,
		RoundingLabel:  describePricingRounding(pr.rounding),
		Scope:          pr.scope,
		ScopeLabel:     describePricingScope(pr.scope),
		ProjectID:      pr.projectID,
		TargetCount:    len(lines),
		ChangedCount:   counts.changed,
		UnchangedCount: counts.unchanged,
		SkippedCount:   counts.skipped,
		Lines:          lines,
	}, nil
}

// ApplyPricing 应用调价：算价 → 写回 product_variants.price → 写留痕（同一事务）。
//
// 三个决定性的语义：
//   - 只有**真正发生变化**的变体才写库、才留痕：无改动的行写一遍等于伪造「改动记录」；
//   - 一条改动都没有时整体报错（ErrPricingNothingChanged），不写空批次 —— 空台账比没有台账更误导；
//   - 价格写回后立刻重算本工程的自动标签（变体写操作后的重算时机，见 #11）：
//     价格区间 / 促销规则的归属由价格决定，不重算就会立刻变成坏数据。
func (s *Service) ApplyPricing(ctx context.Context, req *productdto.PricingApplyReq) (res *productdto.PricingApplyResp, err error) {
	if req == nil {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	pr, err := normalizePricingRequest(&req.PricingRuleReq)
	if err != nil {
		return nil, err
	}
	targets, err := s.resolvePricingTargets(ctx, pr)
	if err != nil {
		return nil, err
	}
	lines, counts, err := s.computePricingLines(ctx, pr, targets)
	if err != nil {
		return nil, err
	}
	if counts.changed == 0 {
		return nil, errors.New(productenums.ErrPricingNothingChanged)
	}

	now := time.Now().UTC()
	byVariant := make(map[string]*pricingTarget, len(targets))
	for _, t := range targets {
		byVariant[t.variant.ID] = t
	}
	updated := make([]*productmodel.VariantEntity, 0, counts.changed)
	changeInputs := make([]*masterdatacontract.ChangeInput, 0, counts.changed)
	items := make([]*productmodel.PriceAdjustmentItemEntity, 0, counts.changed)
	changedLines := make([]*productdto.PricingLineResp, 0, counts.changed)
	for _, line := range lines {
		if line.Status != productenums.PricingLineChanged {
			continue
		}
		t := byVariant[line.VariantID]
		if t == nil {
			continue
		}
		t.variant.Price = line.NewPrice
		t.variant.UpdatedAt = now
		updated = append(updated, t.variant)
		changedLines = append(changedLines, line)
		// issue #19：定价工具改的是售价这一列主数据，逐变体留痕。
		// 改前快照从内存里的变体复制一份并把售价换回原价即可 —— 本次只动 price 一列。
		beforeVariant := *t.variant
		beforeVariant.Price = line.OldPrice
		changeInputs = append(changeInputs, variantChangeInput(pr.projectID, t.variant,
			masterdataenums.ActionUpdate, masterdataenums.OriginPricing, req.OperatorID,
			variantChangeSnapshot(&beforeVariant, nil), variantChangeSnapshot(t.variant, nil)))
		items = append(items, &productmodel.PriceAdjustmentItemEntity{
			ID: uuid.NewString(), ProductID: line.ProductID, VariantID: line.VariantID,
			SKUCode: line.SKUCode, OldPrice: line.OldPrice, NewPrice: line.NewPrice,
			CreatedAt: now,
		})
	}
	adj := &productmodel.PriceAdjustmentEntity{
		ID: uuid.NewString(), ProjectID: pr.projectID,
		RuleType: pr.rule.Type, RuleParams: pr.ruleParams,
		Rounding: pr.rounding, Scope: pr.scope, Filter: pr.filterJSON,
		VariantCount: len(lines), ChangedCount: counts.changed,
		Note: strings.TrimSpace(req.Note), OperatorID: strings.TrimSpace(req.OperatorID),
		CreatedAt: now,
	}
	if pr.targetID != "" {
		targetID := pr.targetID
		adj.TargetID = &targetID
	}
	for _, it := range items {
		it.AdjustmentID = adj.ID
	}
	// 价格写入与留痕写入是两个聚合：事务边界由 service 决定（model 只提供 tx 透传），
	// 半截状态（改了价却没有记录）是留痕功能的致命伤，必须原子。
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if uerr := s.m.SaveVariantsTx(tx, updated, nil); uerr != nil {
			return uerr
		}
		if cerr := s.m.CreateAdjustmentWithItemsTx(tx, adj, items); cerr != nil {
			return cerr
		}
		// issue #19：售价留痕与改价同事务，避免「价已改、审计没记」的半截状态（CQ-026）。
		return s.recordChangesTx(ctx, tx, changeInputs...)
	}); err != nil {
		return nil, err
	}
	s.bumpFragmentCache(ctx, pr.projectID)
	// 变体写操作后的重算时机（#11）：价格变了，价格区间 / 促销规则的归属可能跟着变。
	recalc, rerr := s.recalcPricingAutoTags(ctx, pr.projectID)
	if rerr != nil {
		return nil, rerr
	}
	return &productdto.PricingApplyResp{
		AdjustmentID:     adj.ID,
		RuleType:         pr.rule.Type,
		RuleParams:       pr.ruleParams,
		RuleLabel:        pr.rule.Describe(pr.ruleParams),
		Rounding:         pr.rounding,
		RoundingLabel:    describePricingRounding(pr.rounding),
		Scope:            pr.scope,
		ScopeLabel:       describePricingScope(pr.scope),
		ProjectID:        pr.projectID,
		TargetCount:      len(lines),
		ChangedCount:     counts.changed,
		SkippedCount:     counts.skipped,
		RecalculatedTags: recalc,
		Lines:            changedLines,
	}, nil
}

// ListPricingRuleTypes 内置定价规则清单（后台下拉与参数说明的唯一来源）。
func (s *Service) ListPricingRuleTypes(_ context.Context) (list []*productdto.PricingRuleTypeResp) {
	return pricingRuleTypeOptions()
}

// ListPricingRoundingOptions 尾数处理清单（后台下拉的唯一来源）。
func (s *Service) ListPricingRoundingOptions(_ context.Context) (list []*productdto.PricingRoundingOptionResp) {
	return pricingRoundingOptions()
}

// ListPriceAdjustments 调价留痕列表（新的在前；不含逐变体明细）。
func (s *Service) ListPriceAdjustments(ctx context.Context, req *productdto.ListPriceAdjustmentReq) (list []*productdto.PriceAdjustmentResp, err error) {
	var projectID string
	limit := defaultPricingAdjustments
	if req != nil {
		projectID = strings.TrimSpace(req.ProjectID)
		if req.Limit > 0 {
			limit = req.Limit
		}
	}
	if limit > maxPricingAdjustments {
		limit = maxPricingAdjustments
	}
	// 工程作用域：留痕列表是**工程内**视图（DB-009）。project_id 为空时走唯一工程
	// 兜底，多工程部署下明确报错而不是退化成「所有工程的批次」。
	if projectID, err = s.resolveProjectID(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := s.m.ListAdjustments(ctx, projectID, limit)
	if err != nil {
		return nil, err
	}
	list = make([]*productdto.PriceAdjustmentResp, 0, len(rows))
	for _, row := range rows {
		list = append(list, toAdjustmentResp(row, nil))
	}
	return list, nil
}

// GetPriceAdjustment 单批次留痕详情（含逐变体「原价 → 新价」明细）。
//
// 明细里的商品名按当前数据补全：商品被改名或删除时，SKU 编码快照仍然可读
// （明细不建外键的用意就在这里）。
func (s *Service) GetPriceAdjustment(ctx context.Context, req *productdto.GetPriceAdjustmentReq) (res *productdto.PriceAdjustmentResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	row, err := s.m.GetAdjustment(ctx, req.ID, projectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(productenums.ErrPricingAdjustmentNotFound)
		}
		return nil, err
	}
	details, err := s.m.ListAdjustmentItems(ctx, row.ID, projectID, maxPricingAdjustmentItems)
	if err != nil {
		return nil, err
	}
	names, err := s.adjustmentProductNames(ctx, projectID, details)
	if err != nil {
		return nil, err
	}
	return toAdjustmentResp(row, adjustmentItemResps(details, names)), nil
}

// adjustmentProductNames 批量取明细涉及的当前商品名（失败不阻断留痕读取）。
//
// projectID 由调用方给出（读留痕时已经解析过）：读的是 products（迁移 215 名单），
// 缺作用域时名字全取不到、返回空 map —— 留痕详情里的商品名会整列变空。
func (s *Service) adjustmentProductNames(ctx context.Context, projectID string, details []*productmodel.PriceAdjustmentItemEntity) (names map[string]string, err error) {
	names = map[string]string{}
	ids := make([]string, 0, len(details))
	seen := map[string]bool{}
	for _, d := range details {
		if d == nil || seen[d.ProductID] {
			continue
		}
		seen[d.ProductID] = true
		ids = append(ids, d.ProductID)
	}
	if len(ids) == 0 {
		return names, nil
	}
	rows, lerr := s.m.ListProductsByIDs(ctx, ids, projectID)
	if lerr != nil {
		return nil, lerr
	}
	for _, p := range rows {
		if p != nil {
			names[p.ID] = p.Name
		}
	}
	return names, nil
}

// toAdjustmentResp 批次实体 → 响应（items 为 nil 时只回批次本身）。
func toAdjustmentResp(e *productmodel.PriceAdjustmentEntity, items []*productdto.PriceAdjustmentItemResp) *productdto.PriceAdjustmentResp {
	resp := &productdto.PriceAdjustmentResp{
		ID: e.ID, ProjectID: e.ProjectID,
		RuleType: e.RuleType, RuleParams: orJSON(e.RuleParams, "{}"),
		RuleLabel:     describePricingRule(e.RuleType, e.RuleParams),
		Rounding:      e.Rounding,
		RoundingLabel: describePricingRounding(e.Rounding),
		Scope:         e.Scope,
		ScopeLabel:    describePricingScope(e.Scope),
		Filter:        orJSON(e.Filter, "{}"),
		FilterLabel:   describePricingFilter(e.Filter),
		VariantCount:  e.VariantCount,
		ChangedCount:  e.ChangedCount,
		Note:          e.Note,
		OperatorID:    e.OperatorID,
		CreatedAt:     e.CreatedAt.Format(time.RFC3339),
	}
	if e.TargetID != nil {
		resp.TargetID = *e.TargetID
	}
	if items != nil {
		resp.Items = items
	}
	return resp
}

// adjustmentItemResps 明细实体 → 响应（商品名取自当前数据，缺失时留空）。
func adjustmentItemResps(rows []*productmodel.PriceAdjustmentItemEntity, names map[string]string) []*productdto.PriceAdjustmentItemResp {
	out := make([]*productdto.PriceAdjustmentItemResp, 0, len(rows))
	for _, r := range rows {
		if r == nil {
			continue
		}
		out = append(out, &productdto.PriceAdjustmentItemResp{
			ID: r.ID, ProductID: r.ProductID, ProductName: names[r.ProductID],
			VariantID: r.VariantID, SKUCode: r.SKUCode,
			OldPrice: r.OldPrice, NewPrice: r.NewPrice, Diff: r.NewPrice - r.OldPrice,
			CreatedAt: r.CreatedAt.Format(time.RFC3339),
		})
	}
	return out
}

// describePricingFilter 筛选集条件的可读文本（后台回看用；只列真正生效的条件）。
func describePricingFilter(raw json.RawMessage) string {
	m, err := decodeRuleParams(raw)
	if err != nil || len(m) == 0 {
		return ""
	}
	keys := []string{"status", "keyword", "categoryId", "brandId", "tagId"}
	labels := map[string]string{
		"status": "状态", "keyword": "关键词", "categoryId": "分类",
		"brandId": "品牌", "tagId": "标签",
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		rawValue, ok := m[k]
		if !ok {
			continue
		}
		var value string
		if jerr := json.Unmarshal(rawValue, &value); jerr != nil {
			// 非字符串形态（旧数据）：原样展示，不因为一行坏数据让留痕列表打不开。
			value = strings.TrimSpace(string(rawValue))
		}
		if value == "" {
			continue
		}
		parts = append(parts, labels[k]+"："+value)
	}
	return strings.Join(parts, " · ")
}

// normalizePricingRequest 归一化定价请求并做第一层校验。
func normalizePricingRequest(req *productdto.PricingRuleReq) (pr *pricingRequest, err error) {
	if req == nil {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	rule := lookupPricingRule(req.RuleType)
	if rule == nil {
		return nil, pricingRuleTypeErr(req.RuleType)
	}
	params, err := rule.Normalize(req.RuleParams)
	if err != nil {
		return nil, err
	}
	rounding, err := normalizePricingRounding(req.Rounding)
	if err != nil {
		return nil, err
	}
	scope, err := normalizePricingScope(req.Scope)
	if err != nil {
		return nil, err
	}
	pr = &pricingRequest{
		rule: rule, ruleParams: params, rounding: rounding, scope: scope,
		targetID:  strings.TrimSpace(req.TargetID),
		projectID: strings.TrimSpace(req.ProjectID),
		filter: productmodel.PricingFilter{
			Status:     strings.TrimSpace(req.Status),
			Keyword:    strings.TrimSpace(req.Keyword),
			CategoryID: strings.TrimSpace(req.CategoryID),
			BrandID:    strings.TrimSpace(req.BrandID),
			TagID:      strings.TrimSpace(req.TagID),
		},
	}
	switch scope {
	case productenums.PricingScopeSKU, productenums.PricingScopeProduct:
		if pr.targetID == "" {
			return nil, errors.New(productenums.ErrPricingTargetRequired)
		}
	case productenums.PricingScopeFilter:
		// 筛选集必须指定工程：没有工程条件的批量改价会跨工程改到别人的商品上。
		if pr.projectID == "" {
			return nil, fmt.Errorf("%s：筛选集范围必须指定工程", productenums.ErrInvalidParam)
		}
		pr.filter.ProjectID = pr.projectID
		// 空筛选（只有工程）也不算有效范围：「本工程全部商品」是改价里最危险的一种，
		// 必须由调用方明确写出条件（状态 / 关键词 / 分类 / 品牌 / 标签至少一个）。
		if pr.filter.Status == "" && pr.filter.Keyword == "" && pr.filter.CategoryID == "" &&
			pr.filter.BrandID == "" && pr.filter.TagID == "" {
			return nil, fmt.Errorf("%s：筛选集至少要给一个筛选条件", productenums.ErrPricingFilterEmpty)
		}
	}
	pr.filterJSON = pricingFilterJSON(pr.filter)
	return pr, nil
}

// pricingFilterJSON 筛选条件 → JSONB 落库形态（只保留真正生效的键）。
//
// 手工构造对象而不是 json.Marshal 结构体：空条件不该在台账里留下一堆空串键。
func pricingFilterJSON(f productmodel.PricingFilter) json.RawMessage {
	m := map[string]string{}
	if f.Status != "" {
		m["status"] = f.Status
	}
	if f.Keyword != "" {
		m["keyword"] = f.Keyword
	}
	if f.CategoryID != "" {
		m["categoryId"] = f.CategoryID
	}
	if f.BrandID != "" {
		m["brandId"] = f.BrandID
	}
	if f.TagID != "" {
		m["tagId"] = f.TagID
	}
	b, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage("{}")
	}
	return b
}

// resolvePricingTargets 解析作用范围，返回参与试算的「商品 + 变体」列表。
//
// 顺序稳定（商品排序 → 变体排序），保证同一份数据每次预览输出同样的行序。
func (s *Service) resolvePricingTargets(ctx context.Context, pr *pricingRequest) (targets []*pricingTarget, err error) {
	switch pr.scope {
	case productenums.PricingScopeSKU:
		v, gerr := s.m.GetVariant(ctx, pr.targetID)
		if gerr != nil {
			if errors.Is(gerr, gorm.ErrRecordNotFound) {
				return nil, errors.New(productenums.ErrPricingTargetNotFound)
			}
			return nil, gerr
		}
		p, perr := s.m.Get(ctx, v.ProductID, pr.projectID)
		if perr != nil {
			if errors.Is(perr, gorm.ErrRecordNotFound) {
				return nil, errors.New(productenums.ErrPricingTargetNotFound)
			}
			return nil, perr
		}
		return []*pricingTarget{{product: p, variant: v}}, nil

	case productenums.PricingScopeProduct:
		p, perr := s.m.Get(ctx, pr.targetID, pr.projectID)
		if perr != nil {
			if errors.Is(perr, gorm.ErrRecordNotFound) {
				return nil, errors.New(productenums.ErrPricingTargetNotFound)
			}
			return nil, perr
		}
		variants, verr := s.m.ListVariants(ctx, p.ID)
		if verr != nil {
			return nil, verr
		}
		if len(variants) == 0 {
			return nil, errors.New(productenums.ErrPricingTargetNotFound)
		}
		targets = make([]*pricingTarget, 0, len(variants))
		for _, v := range variants {
			targets = append(targets, &pricingTarget{product: p, variant: v})
		}
		return targets, nil

	default:
		products, lerr := s.m.ListProductsForPricing(ctx, pr.filter)
		if lerr != nil {
			return nil, lerr
		}
		if len(products) == 0 {
			return nil, errors.New(productenums.ErrPricingFilterEmpty)
		}
		if len(products) > maxPricingProducts {
			return nil, fmt.Errorf("%s：%d 个商品超过单批上限 %d，请收紧筛选条件",
				productenums.ErrPricingTargetTooMany, len(products), maxPricingProducts)
		}
		ids := make([]string, 0, len(products))
		for _, p := range products {
			ids = append(ids, p.ID)
		}
		variants, verr := s.m.ListVariantsByProducts(ctx, ids)
		if verr != nil {
			return nil, verr
		}
		byProduct := make(map[string][]*productmodel.VariantEntity, len(products))
		for _, v := range variants {
			byProduct[v.ProductID] = append(byProduct[v.ProductID], v)
		}
		targets = make([]*pricingTarget, 0, len(variants))
		for _, p := range products {
			for _, v := range byProduct[p.ID] {
				targets = append(targets, &pricingTarget{product: p, variant: v})
			}
		}
		if len(targets) == 0 {
			return nil, errors.New(productenums.ErrPricingFilterEmpty)
		}
		return targets, nil
	}
}

// pricingCounts 试算结果的三类计数。
type pricingCounts struct {
	changed   int
	unchanged int
	skipped   int
}

// computePricingLines 逐变体算价（纯计算，不写库）。
//
// 单条变体算不出来（缺成本价 / 结果超范围）时只跳过这一条：
// 一个没填成本的 SKU 不该让整批调价失败，预览会把跳过原因摆出来。
func (s *Service) computePricingLines(_ context.Context, pr *pricingRequest, targets []*pricingTarget) (lines []*productdto.PricingLineResp, counts pricingCounts, err error) {
	lines = make([]*productdto.PricingLineResp, 0, len(targets))
	for _, t := range targets {
		if t == nil || t.variant == nil || t.product == nil {
			continue
		}
		line := &productdto.PricingLineResp{
			ProductID: t.product.ID, ProductName: t.product.Name, ProductSlug: t.product.Slug,
			VariantID: t.variant.ID, SKUCode: t.variant.SKUCode,
			CostPrice: t.variant.CostPrice,
			OldPrice:  t.variant.Price,
			NewPrice:  t.variant.Price,
		}
		if pr.rule.RequiresCost && t.variant.CostPrice == nil {
			setPricingLineStatus(line, productenums.PricingLineSkipped, productenums.PricingSkipCostMissing)
			counts.skipped++
			lines = append(lines, line)
			continue
		}
		cost := 0.0
		if t.variant.CostPrice != nil {
			cost = *t.variant.CostPrice
		}
		raw, cerr := pr.rule.Compute(cost, pr.ruleParams)
		if cerr != nil {
			return nil, counts, cerr
		}
		rounded, rerr := applyPricingRounding(raw, pr.rounding)
		if rerr != nil {
			return nil, counts, rerr
		}
		if rounded < 0 || rounded > pricingAmountMax {
			setPricingLineStatus(line, productenums.PricingLineSkipped, productenums.PricingSkipOutOfRange)
			counts.skipped++
			lines = append(lines, line)
			continue
		}
		line.NewPrice = rounded
		// 比较用分（整数）而不是浮点：12.30 与 12.3 不该被算成「有改动」。
		if pricingCentsOf(t.variant.Price) == pricingCentsOf(rounded) {
			setPricingLineStatus(line, productenums.PricingLineUnchanged, "")
			counts.unchanged++
		} else {
			setPricingLineStatus(line, productenums.PricingLineChanged, "")
			counts.changed++
		}
		lines = append(lines, line)
	}
	return lines, counts, nil
}

// setPricingLineStatus 填状态、状态标签与跳过原因（原因文案只有一份）。
func setPricingLineStatus(line *productdto.PricingLineResp, status, reason string) {
	line.Status = status
	line.StatusLabel = pricingStatusLabel(status)
	line.Reason = reason
	line.ReasonLabel = pricingReasonLabel(reason)
}

// pricingStatusLabel 试算行状态的中文标签。
func pricingStatusLabel(status string) string {
	switch status {
	case productenums.PricingLineChanged:
		return "改价"
	case productenums.PricingLineUnchanged:
		return "价格不变"
	case productenums.PricingLineSkipped:
		return "跳过"
	}
	return status
}

// pricingReasonLabel 跳过原因的中文说明。
func pricingReasonLabel(reason string) string {
	switch reason {
	case productenums.PricingSkipCostMissing:
		return "未填成本价，按成本类规则无法计算"
	case productenums.PricingSkipOutOfRange:
		return "计算结果为负或超过 9999999999.99"
	}
	return ""
}

// pricingCentsOf 金额 → 分（比较用；四舍五入到分，规避二进制浮点尾差）。
func pricingCentsOf(amount float64) int64 { return pricingCentsRound(amount) }

// pricingCentsRound 金额四舍五入到分。
func pricingCentsRound(amount float64) int64 {
	if amount >= 0 {
		return int64(amount*100 + 0.5)
	}
	return int64(amount*100 - 0.5)
}

// recalcPricingAutoTags 定价落库后的自动标签重算（#11 的「变体写操作后」时机），
// 返回本次重算的自动标签数（后台 / 接口可核对这次调价影响了多少标签归属）。
func (s *Service) recalcPricingAutoTags(ctx context.Context, projectID string) (n int, err error) {
	if projectID == "" {
		return 0, nil
	}
	tags, err := s.m.ListTags(ctx, projectID, productenums.TagKindRule, "")
	if err != nil {
		return 0, err
	}
	for _, tag := range tags {
		if _, err = s.recalcTag(ctx, tag); err != nil {
			return 0, err
		}
	}
	return len(tags), nil
}
