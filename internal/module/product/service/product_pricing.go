package productservice

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

// 设计约束（验收 1）：只接受四个内置定价规则与四种尾数处理，**不接受自由表达式**。
// 因此这里是一张写死的注册表，每条规则自带四件事：
//
//	Normalize  严格校验参数（未知键 / 类型不符 / 越界一律拒绝）并归一为落库形态；
//	Describe   把参数翻成人类可读描述（后台展示，规则语义只有这一份）；
//	Compute    由成本价算出售价（纯函数，不碰数据库）；
//	FormValue  取归一后的核心参数值（后台表单回填用）。
//
// 算出的售价一律经 applyPricingRounding 处理尾数后才落库 —— 两条路径（预览与应用）
// 共用同一份实现，预览看到的数字就是应用后写进 product_variants.price 的数字。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/module/masterdata/contract"
	"go_wp/internal/module/masterdata/enums"
	"go_wp/internal/module/product/dto"
	"go_wp/internal/module/product/enums"
	"go_wp/internal/module/product/model"
	"go_wp/pkg/i18n"
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
		RuleLabel:      pr.rule.Describe(translateFrom(ctx), pr.ruleParams),
		Rounding:       pr.rounding,
		RoundingLabel:  describePricingRounding(translateFrom(ctx), pr.rounding),
		Scope:          pr.scope,
		ScopeLabel:     describePricingScope(translateFrom(ctx), pr.scope),
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
		// 静态产物失效（审计 ARCH-01）：批量改价一次可能覆盖多个商品，逐商品各发一条
		//（失效目标是商品详情页与商品集合列表页；enqueueInvalidationTx 内部按实体去重）。
		targets := make([]invalidationTarget, 0, len(updated))
		for _, uv := range updated {
			targets = append(targets, productInvalidationTarget(uv.ProductID))
		}
		if xerr := s.enqueueInvalidationTx(ctx, tx, pr.projectID, targets...); xerr != nil {
			return xerr
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
		RuleLabel:        pr.rule.Describe(translateFrom(ctx), pr.ruleParams),
		Rounding:         pr.rounding,
		RoundingLabel:    describePricingRounding(translateFrom(ctx), pr.rounding),
		Scope:            pr.scope,
		ScopeLabel:       describePricingScope(translateFrom(ctx), pr.scope),
		ProjectID:        pr.projectID,
		TargetCount:      len(lines),
		ChangedCount:     counts.changed,
		SkippedCount:     counts.skipped,
		RecalculatedTags: recalc,
		Lines:            changedLines,
	}, nil
}

// ListPricingRuleTypes 内置定价规则清单（后台下拉与参数说明的唯一来源）。
//
// ctx 只用于取词：展示名与参数说明是文案，按请求语言渲染（取词函数由 inbound 经
// productTranslateMiddleware 注入，见 product_translate.go）。
func (s *Service) ListPricingRuleTypes(ctx context.Context) (list []*productdto.PricingRuleTypeResp) {
	return pricingRuleTypeOptions(translateFrom(ctx))
}

// ListPricingRoundingOptions 尾数处理清单（后台下拉的唯一来源）。
func (s *Service) ListPricingRoundingOptions(ctx context.Context) (list []*productdto.PricingRoundingOptionResp) {
	return pricingRoundingOptions(translateFrom(ctx))
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
		list = append(list, toAdjustmentResp(translateFrom(ctx), row, nil))
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
	return toAdjustmentResp(translateFrom(ctx), row, adjustmentItemResps(details, names)), nil
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
//
// tr 由调用点给（本模块的展示文案一律按请求语言取词，见 product_translate.go）。
func toAdjustmentResp(tr TranslateFunc, e *productmodel.PriceAdjustmentEntity, items []*productdto.PriceAdjustmentItemResp) *productdto.PriceAdjustmentResp {
	resp := &productdto.PriceAdjustmentResp{
		ID: e.ID, ProjectID: e.ProjectID,
		RuleType: e.RuleType, RuleParams: orJSON(e.RuleParams, "{}"),
		RuleLabel:     describePricingRule(tr, e.RuleType, e.RuleParams),
		Rounding:      e.Rounding,
		RoundingLabel: describePricingRounding(tr, e.Rounding),
		Scope:         e.Scope,
		ScopeLabel:    describePricingScope(tr, e.Scope),
		Filter:        orJSON(e.Filter, "{}"),
		FilterLabel:   describePricingFilter(tr, e.Filter),
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
func describePricingFilter(tr TranslateFunc, raw json.RawMessage) string {
	m, err := decodeRuleParams(raw)
	if err != nil || len(m) == 0 {
		return ""
	}
	keys := []string{"status", "keyword", "categoryId", "brandId", "tagId"}
	// 条件名是文案（英文界面上要显示 Status / Keyword / Category …），词条键与中文兜底
	// 都在本表里；冒号与间隔符是排版标点，两种语言通用。
	labelKeys := map[string]string{
		"status":     productenums.ProductPricingFilterStatus,
		"keyword":    productenums.ProductPricingFilterKeyword,
		"categoryId": productenums.ProductPricingFilterCategoryID,
		"brandId":    productenums.ProductPricingFilterBrandID,
		"tagId":      productenums.ProductPricingFilterTagID,
	}
	fallbacks := map[string]string{
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
		parts = append(parts, tr(labelKeys[k], fallbacks[k])+"："+value)
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
			return nil, fmt.Errorf("%s：%s", productenums.ErrInvalidParam,
				i18n.ErrorDetail(productenums.DetailPricingFilterProjectRequired))
		}
		pr.filter.ProjectID = pr.projectID
		// 空筛选（只有工程）也不算有效范围：「本工程全部商品」是改价里最危险的一种，
		// 必须由调用方明确写出条件（状态 / 关键词 / 分类 / 品牌 / 标签至少一个）。
		if pr.filter.Status == "" && pr.filter.Keyword == "" && pr.filter.CategoryID == "" &&
			pr.filter.BrandID == "" && pr.filter.TagID == "" {
			return nil, fmt.Errorf("%s：%s", productenums.ErrPricingFilterEmpty,
				i18n.ErrorDetail(productenums.DetailPricingFilterConditionRequired))
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
			return nil, fmt.Errorf("%s：%s", productenums.ErrPricingTargetTooMany,
				i18n.ErrorDetail(productenums.DetailPricingTargetTooMany,
					"n", strconv.Itoa(len(products)), "max", strconv.Itoa(maxPricingProducts)))
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
func (s *Service) computePricingLines(ctx context.Context, pr *pricingRequest, targets []*pricingTarget) (lines []*productdto.PricingLineResp, counts pricingCounts, err error) {
	tr := translateFrom(ctx)
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
			setPricingLineStatus(tr, line, productenums.PricingLineSkipped, productenums.PricingSkipCostMissing)
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
			setPricingLineStatus(tr, line, productenums.PricingLineSkipped, productenums.PricingSkipOutOfRange)
			counts.skipped++
			lines = append(lines, line)
			continue
		}
		line.NewPrice = rounded
		// 比较用分（整数）而不是浮点：12.30 与 12.3 不该被算成「有改动」。
		if pricingCentsOf(t.variant.Price) == pricingCentsOf(rounded) {
			setPricingLineStatus(tr, line, productenums.PricingLineUnchanged, "")
			counts.unchanged++
		} else {
			setPricingLineStatus(tr, line, productenums.PricingLineChanged, "")
			counts.changed++
		}
		lines = append(lines, line)
	}
	return lines, counts, nil
}

// setPricingLineStatus 填状态、状态标签与跳过原因（原因文案只有一份）。
func setPricingLineStatus(tr TranslateFunc, line *productdto.PricingLineResp, status, reason string) {
	line.Status = status
	line.StatusLabel = pricingStatusLabel(tr, status)
	line.Reason = reason
	line.ReasonLabel = pricingReasonLabel(tr, reason)
}

// pricingStatusLabel 试算行状态标签（当前语言）。
func pricingStatusLabel(tr TranslateFunc, status string) string {
	switch status {
	case productenums.PricingLineChanged:
		return tr(productenums.ProductPricingLineChanged, "改价")
	case productenums.PricingLineUnchanged:
		return tr(productenums.ProductPricingLineUnchanged, "价格不变")
	case productenums.PricingLineSkipped:
		return tr(productenums.ProductPricingLineSkipped, "跳过")
	}
	return status
}

// pricingReasonLabel 跳过原因的说明（当前语言）。
func pricingReasonLabel(tr TranslateFunc, reason string) string {
	switch reason {
	case productenums.PricingSkipCostMissing:
		return tr(productenums.ProductPricingSkipCostMissing, "未填成本价，按成本类规则无法计算")
	case productenums.PricingSkipOutOfRange:
		return tr(productenums.ProductPricingSkipOutOfRange, "计算结果为负或超过 9999999999.99")
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
	tags, err := s.m.ListTags(ctx, projectID, productenums.TagKindRule, "", 0, 0)
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

// 定价参数与金额的取值范围（越界即拒绝，不做静默裁剪）。
const (
	// pricingMultiplierMin/Max 成本倍数：0.01~100（0 或负数会把售价算成 0 / 负数）。
	pricingMultiplierMin = 0.01
	pricingMultiplierMax = 100
	// pricingMarkupMax 成本加价金额上限。
	pricingMarkupMax = 1e9
	// pricingMarginMin/Max 目标毛利率：必须严格在 (0,1) 之间
	// （0 = 售价等于成本，1 = 售价无穷大，两端都要挡）。
	// 上限 0.95：margin 接近 1 时 cost/(1-margin) 浮点放大，审计 TX-013。
	pricingMarginMin = 0.0001
	pricingMarginMax = 0.95
	// pricingAmountMax 金额上限：product_variants.price 是 numeric(12,2)。
	pricingAmountMax = 9999999999.99
)

// pricingRule 一个内置定价规则类型。
type pricingRule struct {
	// Type 落库的 rule_type。
	Type string
	// NameKey / Name 后台展示名：NameKey 是词条真源，Name 是中文兜底
	// （i18n 未初始化 / 词条缺失时用）。展示名是**文案**（内置规则的固定说法），
	// 与用户自定义的规则名（数据）无关。
	NameKey string
	Name    string
	// ParamsKey / Params 参数说明（后台展示，也是调用方唯一的参数文档来源）。
	ParamsKey string
	Params    string
	// RequiresCost 规则是否依赖变体成本价（缺成本价的变体被跳过而不是整批失败）。
	RequiresCost bool
	// Normalize 校验并归一参数。
	Normalize func(params json.RawMessage) (json.RawMessage, error)
	// Describe 归一后的参数 → 当前语言的人类可读描述（tr 由调用点给：service 层
	// 没有语言上下文，取词函数一律从 inbound 传下来）。
	Describe func(tr func(key, fallback string) string, params json.RawMessage) string
	// Compute 由成本价算售价（未归一 / 归一后的参数都能解析，调用方只用归一后的）。
	Compute func(cost float64, params json.RawMessage) (float64, error)
	// FormValue 核心参数值（后台表单回填的字符串形态）。
	FormValue func(params json.RawMessage) string
}

// 内置定价规则 / 尾数处理 / 作用范围的展示文案 key（词条见迁移 449_i18n_*）。
//
// 这些是**内置规则的固定说法**（后台展示名、参数说明、可读描述），不是数据 ——
// 与用户自定义的规则名无关。中文兜底留在本文件的规则表与下面的描述函数里。
const (
	pricingKeyCostMultipleName     = "admin.product_pricing.rule.costMultiple.name"
	pricingKeyCostMultipleParams   = "admin.product_pricing.rule.costMultiple.params"
	pricingKeyCostMultipleDescribe = "admin.product_pricing.rule.costMultiple.describe"
	pricingKeyCostMarkupName       = "admin.product_pricing.rule.costMarkup.name"
	pricingKeyCostMarkupParams     = "admin.product_pricing.rule.costMarkup.params"
	pricingKeyCostMarkupDescribe   = "admin.product_pricing.rule.costMarkup.describe"
	pricingKeyTargetMarginName     = "admin.product_pricing.rule.targetMargin.name"
	pricingKeyTargetMarginParams   = "admin.product_pricing.rule.targetMargin.params"
	pricingKeyTargetMarginDescribe = "admin.product_pricing.rule.targetMargin.describe"
	pricingKeyFixedPriceName       = "admin.product_pricing.rule.fixedPrice.name"
	pricingKeyFixedPriceParams     = "admin.product_pricing.rule.fixedPrice.params"
	pricingKeyFixedPriceDescribe   = "admin.product_pricing.rule.fixedPrice.describe"
	pricingKeyParamsInvalid        = "admin.product_pricing.rule.paramsInvalid"
	pricingKeyUnknownRule          = "admin.product_pricing.rule.unknown"
	pricingKeyRoundingNone         = "admin.product_pricing.rounding.none"
	pricingKeyRoundingInteger      = "admin.product_pricing.rounding.integer"
	pricingKeyRoundingEnd9         = "admin.product_pricing.rounding.end9"
	pricingKeyRoundingEnd99        = "admin.product_pricing.rounding.end99"
	pricingKeyRoundingUnknown      = "admin.product_pricing.rounding.unknown"
	pricingKeyScopeSKU             = "admin.product_pricing.scope.sku"
	pricingKeyScopeProduct         = "admin.product_pricing.scope.product"
	pricingKeyScopeFilter          = "admin.product_pricing.scope.filter"
	pricingKeyScopeUnknown         = "admin.product_pricing.scope.unknown"
)

// pricingRules 内置定价规则表（顺序即后台展示顺序）。新增规则只在这里追加。
var pricingRules = []*pricingRule{
	{
		Type:         productenums.PricingRuleCostMultiple,
		NameKey:      pricingKeyCostMultipleName,
		Name:         "成本乘倍数",
		ParamsKey:    pricingKeyCostMultipleParams,
		Params:       "multiplier：必填，0.01~100（售价 = 成本 × multiplier）",
		RequiresCost: true,
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			return normalizeSinglePricingParam(params, "multiplier", pricingMultiplierMin, pricingMultiplierMax)
		},
		Describe: func(tr func(key, fallback string) string, params json.RawMessage) string {
			v, ok := pricingParamFloat(params, "multiplier")
			if !ok {
				return tr(pricingKeyParamsInvalid, "参数不合法")
			}
			return i18n.FillTranslate(tr, pricingKeyCostMultipleDescribe, "成本 × {value}",
				map[string]string{"value": formatPricingNumber(v)})
		},
		Compute: func(cost float64, params json.RawMessage) (float64, error) {
			v, ok := pricingParamFloat(params, "multiplier")
			if !ok {
				return 0, pricingParamsErr(i18n.ErrorDetail(productenums.DetailPricingParamMissingOrNaN, "field", "multiplier"))
			}
			return cost * v, nil
		},
		FormValue: func(params json.RawMessage) string {
			v, _ := pricingParamFloat(params, "multiplier")
			return formatPricingNumber(v)
		},
	},
	{
		Type:         productenums.PricingRuleCostMarkup,
		NameKey:      pricingKeyCostMarkupName,
		Name:         "成本加价",
		ParamsKey:    pricingKeyCostMarkupParams,
		Params:       "amount：必填，0~1000000000（售价 = 成本 + amount）",
		RequiresCost: true,
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			return normalizeSinglePricingParam(params, "amount", 0, pricingMarkupMax)
		},
		Describe: func(tr func(key, fallback string) string, params json.RawMessage) string {
			v, ok := pricingParamFloat(params, "amount")
			if !ok {
				return tr(pricingKeyParamsInvalid, "参数不合法")
			}
			return i18n.FillTranslate(tr, pricingKeyCostMarkupDescribe, "成本 + {value}",
				map[string]string{"value": formatPricingNumber(v)})
		},
		Compute: func(cost float64, params json.RawMessage) (float64, error) {
			v, ok := pricingParamFloat(params, "amount")
			if !ok {
				return 0, pricingParamsErr(i18n.ErrorDetail(productenums.DetailPricingParamMissingOrNaN, "field", "amount"))
			}
			return cost + v, nil
		},
		FormValue: func(params json.RawMessage) string {
			v, _ := pricingParamFloat(params, "amount")
			return formatPricingNumber(v)
		},
	},
	{
		Type:         productenums.PricingRuleTargetMargin,
		NameKey:      pricingKeyTargetMarginName,
		Name:         "目标毛利率",
		ParamsKey:    pricingKeyTargetMarginParams,
		Params:       "margin：必填，0.0001~0.95 的小数（售价 = 成本 ÷ (1 - margin)，如 0.3 表示毛利率 30%）",
		RequiresCost: true,
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			return normalizeSinglePricingParam(params, "margin", pricingMarginMin, pricingMarginMax)
		},
		Describe: func(tr func(key, fallback string) string, params json.RawMessage) string {
			v, ok := pricingParamFloat(params, "margin")
			if !ok {
				return tr(pricingKeyParamsInvalid, "参数不合法")
			}
			return i18n.FillTranslate(tr, pricingKeyTargetMarginDescribe, "目标毛利率 {value}",
				map[string]string{"value": formatPricingPercent(v)})
		},
		Compute: func(cost float64, params json.RawMessage) (float64, error) {
			v, ok := pricingParamFloat(params, "margin")
			if !ok {
				return 0, pricingParamsErr(i18n.ErrorDetail(productenums.DetailPricingParamMissingOrNaN, "field", "margin"))
			}
			if v <= 0 || v > pricingMarginMax {
				return 0, pricingParamsErr(i18n.ErrorDetail(productenums.DetailPricingParamRangePlain,
					"field", "margin", "min", formatPricingNumber(pricingMarginMin), "max", formatPricingNumber(pricingMarginMax)))
			}
			return cost / (1 - v), nil
		},
		FormValue: func(params json.RawMessage) string {
			v, _ := pricingParamFloat(params, "margin")
			return formatPricingNumber(v)
		},
	},
	{
		Type:         productenums.PricingRuleFixedPrice,
		NameKey:      pricingKeyFixedPriceName,
		Name:         "统一售价",
		ParamsKey:    pricingKeyFixedPriceParams,
		Params:       "amount：必填，0~9999999999.99（不看成本，全部改为此售价）",
		RequiresCost: false,
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			return normalizeSinglePricingParam(params, "amount", 0, pricingAmountMax)
		},
		Describe: func(tr func(key, fallback string) string, params json.RawMessage) string {
			v, ok := pricingParamFloat(params, "amount")
			if !ok {
				return tr(pricingKeyParamsInvalid, "参数不合法")
			}
			return i18n.FillTranslate(tr, pricingKeyFixedPriceDescribe, "统一售价 {value}",
				map[string]string{"value": formatPricingNumber(v)})
		},
		Compute: func(_ float64, params json.RawMessage) (float64, error) {
			v, ok := pricingParamFloat(params, "amount")
			if !ok {
				return 0, pricingParamsErr(i18n.ErrorDetail(productenums.DetailPricingParamMissingOrNaN, "field", "amount"))
			}
			return v, nil
		},
		FormValue: func(params json.RawMessage) string {
			v, _ := pricingParamFloat(params, "amount")
			return formatPricingNumber(v)
		},
	},
}

// lookupPricingRule 按类型取规则（未命中返回 nil）。
func lookupPricingRule(ruleType string) *pricingRule {
	ruleType = strings.TrimSpace(ruleType)
	for _, r := range pricingRules {
		if r.Type == ruleType {
			return r
		}
	}
	return nil
}

// normalizePricingRuleParams 校验并归一某规则的参数（未知类型按规则类型错误返回）。
func normalizePricingRuleParams(ruleType string, params json.RawMessage) (norm json.RawMessage, err error) {
	r := lookupPricingRule(ruleType)
	if r == nil {
		return nil, pricingRuleTypeErr(ruleType)
	}
	return r.Normalize(params)
}

// describePricingRule 规则的可读描述；未知类型给一句可读说明而不是报错
// （后台列表不该因为一条坏数据整页打不开）。
func describePricingRule(tr func(key, fallback string) string, ruleType string, params json.RawMessage) string {
	ruleType = strings.TrimSpace(ruleType)
	if ruleType == "" {
		return ""
	}
	r := lookupPricingRule(ruleType)
	if r == nil {
		return i18n.FillTranslate(tr, pricingKeyUnknownRule, "未知定价规则：{type}",
			map[string]string{"type": ruleType})
	}
	return r.Describe(tr, params)
}

// normalizeSinglePricingParam 单参数规则的统一校验：白名单只允许这一个键、必填、数字、范围内。
func normalizeSinglePricingParam(params json.RawMessage, key string, min, max float64) (out json.RawMessage, err error) {
	m, derr := decodeRuleParams(params)
	if derr != nil {
		return nil, pricingParamsErr(i18n.ErrorDetail(productenums.DetailPricingParamsNotObject))
	}
	if err = rejectUnknownPricingKeys(m, key); err != nil {
		return nil, err
	}
	raw, ok := m[key]
	if !ok {
		return nil, pricingParamsErr(i18n.ErrorDetail(productenums.DetailPricingParamRequired, "field", key))
	}
	v, ferr := ruleFloat(raw, key)
	if ferr != nil {
		return nil, pricingParamsErr(i18n.ErrorDetail(productenums.DetailPricingParamNotNumber, "field", key))
	}
	if v < min || v > max {
		return nil, pricingParamsErr(i18n.ErrorDetail(productenums.DetailPricingParamRange,
			"field", key, "min", formatPricingNumber(min),
			"max", formatPricingNumber(max), "value", formatPricingNumber(v)))
	}
	return json.RawMessage(fmt.Sprintf("{%q:%s}", key, formatPricingNumber(v))), nil
}

// rejectUnknownPricingKeys 拒绝白名单以外的键（定价工具只接受内置参数，不接受自由表达式）。
func rejectUnknownPricingKeys(m map[string]json.RawMessage, allowed ...string) (err error) {
	allow := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		allow[k] = true
	}
	unknown := make([]string, 0, len(m))
	for k := range m {
		if !allow[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return pricingParamsErr(i18n.ErrorDetail(productenums.DetailPricingUnknownKeys,
		"keys", strings.Join(unknown, ", ")))
}

// pricingParamFloat 取归一后参数里的数字（缺失 / 非法返回 false）。
func pricingParamFloat(params json.RawMessage, key string) (v float64, ok bool) {
	m, err := decodeRuleParams(params)
	if err != nil {
		return 0, false
	}
	raw, exists := m[key]
	if !exists {
		return 0, false
	}
	v, ferr := ruleFloat(raw, key)
	if ferr != nil {
		return 0, false
	}
	return v, true
}

// —— 尾数处理 ——

// pricingRounding 一种尾数处理方式。
type pricingRounding struct {
	// Value 落库的 rounding。
	Value string
	// NameKey / Name 后台展示名（key 是词条真源，Name 是中文兜底）。
	NameKey string
	Name    string
	// Apply 金额 → 分（整数）。除 none（四舍五入到分）外一律**向上取**：
	// 尾数处理只抬不降，保证按规则算出的售价不会因为凑尾数被压低。
	Apply func(amount float64) int64
}

// pricingRoundings 尾数处理表（顺序即后台展示顺序）。
var pricingRoundings = []*pricingRounding{
	{
		Value:   productenums.PricingRoundingNone,
		NameKey: pricingKeyRoundingNone,
		Name:    "不舍入（四舍五入到分）",
		Apply:   func(amount float64) int64 { return int64(math.Round(amount * 100)) },
	},
	{
		Value:   productenums.PricingRoundingInteger,
		NameKey: pricingKeyRoundingInteger,
		Name:    "向上取整到元",
		// 12.01 → 13.00；12.00 → 12.00。
		Apply: func(amount float64) int64 { return ceilToStep(pricingCeilCents(amount), 100) },
	},
	{
		Value:   productenums.PricingRoundingEnd9,
		NameKey: pricingKeyRoundingEnd9,
		Name:    "尾数 9（向上取到角位为 9）",
		// 12.34 → 12.90；12.90 → 12.90；12.91 → 13.90。
		Apply: func(amount float64) int64 { return ceilToCentsRemainder(pricingCeilCents(amount), 90) },
	},
	{
		Value:   productenums.PricingRoundingEnd99,
		NameKey: pricingKeyRoundingEnd99,
		Name:    "尾数 99（向上取到分为 99）",
		// 12.34 → 12.99；12.99 → 12.99；13.00 → 13.99。
		Apply: func(amount float64) int64 { return ceilToCentsRemainder(pricingCeilCents(amount), 99) },
	},
}

// lookupPricingRounding 按取值取尾数处理（空串按 none，未命中返回 nil）。
func lookupPricingRounding(rounding string) *pricingRounding {
	rounding = strings.TrimSpace(rounding)
	if rounding == "" {
		rounding = productenums.PricingRoundingNone
	}
	for _, r := range pricingRoundings {
		if r.Value == rounding {
			return r
		}
	}
	return nil
}

// normalizePricingRounding 校验尾数处理取值（空串归一为 none）。
func normalizePricingRounding(rounding string) (out string, err error) {
	r := lookupPricingRounding(rounding)
	if r == nil {
		return "", pricingRoundingErr(rounding)
	}
	return r.Value, nil
}

// describePricingRounding 尾数处理的可读名（未知取值给一句可读说明）。
//
// tr 由调用点传：本文件只产出「取词后的展示文案」，词条 key 与中文兜底都在表里。
func describePricingRounding(tr func(key, fallback string) string, rounding string) string {
	r := lookupPricingRounding(rounding)
	if r == nil {
		return i18n.FillTranslate(tr, pricingKeyRoundingUnknown, "未知尾数处理：{value}",
			map[string]string{"value": strings.TrimSpace(rounding)})
	}
	return tr(r.NameKey, r.Name)
}

// applyPricingRounding 金额 → 尾数处理后的金额。
//
// 先换算成分（整数）再过尾数规则：全程整数运算，避免 12.90 这类十进制小数
// 在二进制浮点里表示不出来导致 12.34 向上取到 12.89 的偏差。
func applyPricingRounding(amount float64, rounding string) (out float64, err error) {
	r := lookupPricingRounding(rounding)
	if r == nil {
		return 0, pricingRoundingErr(rounding)
	}
	return float64(r.Apply(amount)) / 100, nil
}

// pricingCeilCents 金额向上取到分（先减一点浮点尾差，避免 12.9 存成 12.900000000000002 时多进一分）。
func pricingCeilCents(amount float64) int64 {
	return int64(math.Ceil(amount*100 - 1e-9))
}

// ceilToStep 分值向上取到 step 的整数倍。
func ceilToStep(cents, step int64) int64 {
	if step <= 1 {
		return cents
	}
	rem := cents % step
	if rem < 0 {
		rem += step
	}
	if rem == 0 {
		return cents
	}
	return cents + (step - rem)
}

// ceilToCentsRemainder 分值向上取到「模 100 等于 remainder」的第一个分值。
//
// 尾数 9 用 remainder=90（角位为 9），尾数 99 用 remainder=99。
func ceilToCentsRemainder(cents, remainder int64) int64 {
	rem := cents % 100
	if rem < 0 {
		rem += 100
	}
	if rem <= remainder {
		return cents + (remainder - rem)
	}
	return cents + (100 - rem) + remainder
}

// —— 作用范围 ——

// normalizePricingScope 校验作用范围取值。
func normalizePricingScope(scope string) (out string, err error) {
	switch strings.TrimSpace(scope) {
	case productenums.PricingScopeSKU:
		return productenums.PricingScopeSKU, nil
	case productenums.PricingScopeProduct:
		return productenums.PricingScopeProduct, nil
	case productenums.PricingScopeFilter:
		return productenums.PricingScopeFilter, nil
	}
	return "", errors.New(productenums.ErrPricingScopeInvalid)
}

// describePricingScope 作用范围的可读名（当前语言）。
func describePricingScope(tr func(key, fallback string) string, scope string) string {
	switch strings.TrimSpace(scope) {
	case productenums.PricingScopeSKU:
		return tr(pricingKeyScopeSKU, "单个 SKU")
	case productenums.PricingScopeProduct:
		return tr(pricingKeyScopeProduct, "单个商品的全部变体")
	case productenums.PricingScopeFilter:
		return tr(pricingKeyScopeFilter, "筛选出的商品集")
	}
	return tr(pricingKeyScopeUnknown, "未知作用范围")
}

// —— 后台选项（下拉与说明的唯一来源）——

// pricingRuleTypeOptions 内置定价规则清单（展示名与参数说明按当前语言取词）。
func pricingRuleTypeOptions(tr func(key, fallback string) string) (out []*productdto.PricingRuleTypeResp) {
	out = make([]*productdto.PricingRuleTypeResp, 0, len(pricingRules))
	for _, r := range pricingRules {
		out = append(out, &productdto.PricingRuleTypeResp{
			Type: r.Type, Name: tr(r.NameKey, r.Name), Params: tr(r.ParamsKey, r.Params),
			RequiresCost: r.RequiresCost,
		})
	}
	return out
}

// pricingRoundingOptions 尾数处理清单。
func pricingRoundingOptions(tr func(key, fallback string) string) (out []*productdto.PricingRoundingOptionResp) {
	out = make([]*productdto.PricingRoundingOptionResp, 0, len(pricingRoundings))
	for _, r := range pricingRoundings {
		out = append(out, &productdto.PricingRoundingOptionResp{Value: r.Value, Name: tr(r.NameKey, r.Name)})
	}
	return out
}

// —— 文本工具 ——

// formatPricingNumber 数值的规范文本（整数不带小数尾巴）。
func formatPricingNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// formatPricingPercent 毛利率的小数 → 百分比文本（0.3 → 30%）。
func formatPricingPercent(v float64) string { return formatPricingNumber(v*100) + "%" }

// pricingParamsErr 参数错误的统一包装（消息带具体原因，便于后台直接显示）。
func pricingParamsErr(detail string) error {
	return fmt.Errorf("%s：%s", productenums.ErrPricingRuleParamsInvalid, detail)
}

// pricingRuleTypeErr 规则类型错误的统一包装。
func pricingRuleTypeErr(ruleType string) error {
	if strings.TrimSpace(ruleType) == "" {
		return errors.New(productenums.ErrPricingRuleTypeInvalid)
	}
	return fmt.Errorf("%s：%s", productenums.ErrPricingRuleTypeInvalid,
		i18n.ErrorDetail(productenums.DetailPricingUnknownRuleType, "type", ruleType))
}

// pricingRoundingErr 尾数处理错误的统一包装。
func pricingRoundingErr(rounding string) error {
	if strings.TrimSpace(rounding) == "" {
		return errors.New(productenums.ErrPricingRoundingInvalid)
	}
	return fmt.Errorf("%s：%s", productenums.ErrPricingRoundingInvalid,
		i18n.ErrorDetail(productenums.DetailPricingUnknownRounding, "value", rounding))
}
