package productservice

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/rls"
)

const (
	defaultPricingAdjustments = 20
	maxPricingAdjustments     = 200

	maxPricingAdjustmentItems = 500

	maxPricingProducts = 500
)

type pricingTarget struct {
	product *productmodel.ProductEntity
	variant *productmodel.VariantEntity
}

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

func (s *Service) PreviewPricing(ctx context.Context, req *productdto.PricingPreviewReq) (res *productdto.PricingPreviewResp, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	pr, err := normalizePricingRequest(&req.PricingRuleReq)
	if err != nil {
		return nil, err
	}
	targets, err := resolvePricingTargets(ctx, s.m, pr)
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
		RuleValues:     pricingRuleValues(pr.rule.Type, pr.ruleParams),
		Rounding:       pr.rounding,
		Scope:          pr.scope,
		ProjectID:      pr.projectID,
		TargetCount:    len(lines),
		ChangedCount:   counts.changed,
		UnchangedCount: counts.unchanged,
		SkippedCount:   counts.skipped,
		Lines:          lines,
	}, nil
}

func (s *Service) ApplyPricing(ctx context.Context, req *productdto.PricingApplyReq) (*productdto.PricingApplyResp, error) {
	return s.applyPricingOnce(ctx, req)
}

type pricingPlan struct {
	projectID    string
	updated      []*productmodel.VariantEntity
	changeInputs []*masterdatacontract.ChangeInput
	adj          *productmodel.PriceAdjustmentEntity
	items        []*productmodel.PriceAdjustmentItemEntity
	res          *productdto.PricingApplyResp
}

func isPricingNothingChanged(err error) bool {
	fact, ok := facing.FactOf(err)
	return ok && fact.Business && fact.Key == productenums.ErrPricingNothingChanged
}

func (s *Service) computePricingPlan(ctx context.Context, tx *gorm.DB, req *productdto.PricingApplyReq) (*pricingPlan, error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	pr, err := normalizePricingRequest(&req.PricingRuleReq)
	if err != nil {
		return nil, err
	}
	targets, err := s.resolvePricingTargetsTx(ctx, tx, pr)
	if err != nil {
		return nil, err
	}
	lines, counts, err := s.computePricingLines(ctx, pr, targets)
	if err != nil {
		return nil, err
	}
	blocked := make([]facing.BlockedItem, 0)
	for _, line := range lines {
		if line.Status != productenums.PricingLineSkipped {
			continue
		}
		reason := productenums.ErrPricingCostMissing
		if line.Reason == productenums.PricingSkipOutOfRange {
			reason = productenums.ErrPricingOutOfRange
		}
		blocked = append(blocked, bulkBlocked(line.VariantID, line.ProductName+" / "+line.SKUCode, reason))
	}
	if len(blocked) > 0 {
		return nil, bulkRejected(blocked)
	}
	if counts.changed == 0 {
		return nil, productenums.Err(productenums.ErrPricingNothingChanged)
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
	return &pricingPlan{
		projectID:    pr.projectID,
		updated:      updated,
		changeInputs: changeInputs,
		adj:          adj,
		items:        items,
		res: &productdto.PricingApplyResp{
			AdjustmentID: adj.ID,
			RuleType:     pr.rule.Type,
			RuleParams:   pr.ruleParams,
			RuleValues:   pricingRuleValues(pr.rule.Type, pr.ruleParams),
			Rounding:     pr.rounding,
			Scope:        pr.scope,
			ProjectID:    pr.projectID,
			TargetCount:  len(lines),
			ChangedCount: counts.changed,
			Lines:        changedLines,
		},
	}, nil
}

func (s *Service) writePricingPlansTx(ctx context.Context, tx *gorm.DB, plans []*pricingPlan) error {
	if len(plans) == 0 {
		return nil
	}
	projectID := plans[0].projectID

	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return serr
	}
	targets := make([]invalidationTarget, 0, len(plans))
	changes := make([]*masterdatacontract.ChangeInput, 0, len(plans))
	for _, plan := range plans {
		if uerr := s.m.SaveVariantsTx(tx, plan.updated, nil); uerr != nil {
			return uerr
		}
		if cerr := s.m.CreateAdjustmentWithItemsTx(tx, plan.adj, plan.items); cerr != nil {
			return cerr
		}

		for _, uv := range plan.updated {
			targets = append(targets, productInvalidationTarget(uv.ProductID))
		}
		changes = append(changes, plan.changeInputs...)
	}
	if xerr := s.enqueueInvalidationTx(ctx, tx, projectID, targets...); xerr != nil {
		return xerr
	}

	return s.recordChangesTx(ctx, tx, changes...)
}

func (s *Service) applyPricingOnce(ctx context.Context, req *productdto.PricingApplyReq) (res *productdto.PricingApplyResp, err error) {
	var plan *pricingPlan
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		var err error
		plan, err = s.computePricingPlan(ctx, tx, req)
		if err != nil {
			return err
		}
		if err := s.writePricingPlansTx(ctx, tx, []*pricingPlan{plan}); err != nil {
			return err
		}
		plan.res.RecalculatedTags, err = s.recalcAutoTagsTx(ctx, tx, plan.projectID)
		return err
	}); err != nil {
		return nil, err
	}

	return plan.res, nil
}

func (s *Service) ListPricingRuleTypes(ctx context.Context) (list []*productdto.PricingRuleTypeResp) {
	return pricingRuleTypeOptions()
}

func (s *Service) ListPricingRoundingOptions(ctx context.Context) (list []*productdto.PricingRoundingOptionResp) {
	return pricingRoundingOptions()
}

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
	if req != nil && req.IncludeItems {
		ids := make([]string, 0, len(list))
		for _, a := range list {
			ids = append(ids, a.ID)
		}
		grouped, err := s.pricingHistoryItems(ctx, projectID, ids)
		if err != nil {
			return nil, err
		}
		for _, a := range list {
			a.Items = grouped[a.ID]
		}
	}
	return list, nil
}

func (s *Service) GetPriceAdjustment(ctx context.Context, req *productdto.GetPriceAdjustmentReq) (res *productdto.PriceAdjustmentResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	row, err := s.m.GetAdjustment(ctx, req.ID, projectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, productenums.Err(productenums.ErrPricingAdjustmentNotFound)
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
	rows, lerr := s.m.ListNameFactsByIDs(ctx, ids, projectID)
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

func toAdjustmentResp(e *productmodel.PriceAdjustmentEntity, items []*productdto.PriceAdjustmentItemResp) *productdto.PriceAdjustmentResp {
	resp := &productdto.PriceAdjustmentResp{
		ID: e.ID, ProjectID: e.ProjectID,
		RuleType: e.RuleType, RuleParams: orJSON(e.RuleParams, "{}"),
		RuleValues:   pricingRuleValues(e.RuleType, e.RuleParams),
		Rounding:     e.Rounding,
		Scope:        e.Scope,
		Filter:       orJSON(e.Filter, "{}"),
		FilterParts:  pricingFilterParts(e.Filter),
		VariantCount: e.VariantCount,
		ChangedCount: e.ChangedCount,
		Note:         e.Note,
		OperatorID:   e.OperatorID,
		CreatedAt:    utils.NewJSONTime(e.CreatedAt),
	}
	if e.TargetID != nil {
		resp.TargetID = *e.TargetID
	}
	if items != nil {
		resp.Items = items
	}
	return resp
}

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
			CreatedAt: utils.NewJSONTime(r.CreatedAt),
		})
	}
	return out
}

func normalizePricingRequest(req *productdto.PricingRuleReq) (pr *pricingRequest, err error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
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
			return nil, productenums.Err(productenums.ErrPricingTargetRequired)
		}
	case productenums.PricingScopeFilter:

		if pr.projectID == "" {
			return nil, fmt.Errorf("%w: %s", productenums.Err(productenums.ErrInvalidParam),
				i18n.ErrorDetail(productenums.DetailPricingFilterProjectRequired))
		}
		pr.filter.ProjectID = pr.projectID

		if pr.filter.Status == "" && pr.filter.Keyword == "" && pr.filter.CategoryID == "" &&
			pr.filter.BrandID == "" && pr.filter.TagID == "" {
			return nil, fmt.Errorf("%w: %s", productenums.Err(productenums.ErrPricingFilterEmpty),
				i18n.ErrorDetail(productenums.DetailPricingFilterConditionRequired))
		}
	}
	pr.filterJSON = pricingFilterJSON(pr.filter)
	return pr, nil
}

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

func resolvePricingTargets(ctx context.Context, m *productmodel.Model, pr *pricingRequest) (targets []*pricingTarget, err error) {
	switch pr.scope {
	case productenums.PricingScopeSKU:
		v, gerr := m.GetVariant(ctx, pr.targetID)
		if gerr != nil {
			if errors.Is(gerr, gorm.ErrRecordNotFound) {
				return nil, productenums.Err(productenums.ErrPricingTargetNotFound)
			}
			return nil, gerr
		}

		p, perr := m.GetSummaryFacts(ctx, v.ProductID, pr.projectID)
		if perr != nil {
			if errors.Is(perr, gorm.ErrRecordNotFound) {
				return nil, productenums.Err(productenums.ErrPricingTargetNotFound)
			}
			return nil, perr
		}
		return []*pricingTarget{{product: p, variant: v}}, nil

	case productenums.PricingScopeProduct:
		p, perr := m.GetSummaryFacts(ctx, pr.targetID, pr.projectID)
		if perr != nil {
			if errors.Is(perr, gorm.ErrRecordNotFound) {
				return nil, productenums.Err(productenums.ErrPricingTargetNotFound)
			}
			return nil, perr
		}
		variants, verr := m.ListVariants(ctx, p.ID)
		if verr != nil {
			return nil, verr
		}
		if len(variants) == 0 {
			return nil, productenums.Err(productenums.ErrPricingTargetNotFound)
		}
		targets = make([]*pricingTarget, 0, len(variants))
		for _, v := range variants {
			targets = append(targets, &pricingTarget{product: p, variant: v})
		}
		return targets, nil

	default:
		products, lerr := m.ListProductsForPricing(ctx, pr.filter)
		if lerr != nil {
			return nil, lerr
		}
		if len(products) == 0 {
			return nil, productenums.Err(productenums.ErrPricingFilterEmpty)
		}
		if len(products) > maxPricingProducts {
			return nil, fmt.Errorf("%w: %s", productenums.Err(productenums.ErrPricingTargetTooMany),
				i18n.ErrorDetail(productenums.DetailPricingTargetTooMany,
					"n", strconv.Itoa(len(products)), "max", strconv.Itoa(maxPricingProducts)))
		}
		ids := make([]string, 0, len(products))
		for _, p := range products {
			ids = append(ids, p.ID)
		}
		variants, verr := m.ListVariantsByProducts(ctx, ids)
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
			return nil, productenums.Err(productenums.ErrPricingFilterEmpty)
		}
		return targets, nil
	}
}

type pricingCounts struct {
	changed   int
	unchanged int
	skipped   int
}

func (s *Service) computePricingLines(ctx context.Context, pr *pricingRequest, targets []*pricingTarget) (lines []*productdto.PricingLineResp, counts pricingCounts, err error) {
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
		line.Diff = float64(pricingCentsOf(rounded)-pricingCentsOf(t.variant.Price)) / 100

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

func setPricingLineStatus(line *productdto.PricingLineResp, status, reason string) {
	line.Status = status
	line.Reason = reason
}

func pricingCentsOf(amount float64) int64 { return pricingCentsRound(amount) }

func pricingCentsRound(amount float64) int64 {
	if amount >= 0 {
		return int64(amount*100 + 0.5)
	}
	return int64(amount*100 - 0.5)
}

const (
	pricingMultiplierMin = 0.01
	pricingMultiplierMax = 100

	pricingMarkupMax = 1e9

	pricingMarginMin = 0.0001
	pricingMarginMax = 0.95

	pricingAmountMax = 9999999999.99
)

type pricingRule struct {
	Type string

	RequiresCost bool

	Normalize func(params json.RawMessage) (json.RawMessage, error)

	Compute func(cost float64, params json.RawMessage) (float64, error)
}

var pricingRules = []*pricingRule{
	{
		Type:         productenums.PricingRuleCostMultiple,
		RequiresCost: true,
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			return normalizeSinglePricingParam(params, "multiplier", pricingMultiplierMin, pricingMultiplierMax)
		},
		Compute: func(cost float64, params json.RawMessage) (float64, error) {
			v, ok := pricingParamFloat(params, "multiplier")
			if !ok {
				return 0, pricingParamsErr(i18n.ErrorDetail(productenums.DetailPricingParamMissingOrNaN, "field", "multiplier"))
			}
			return cost * v, nil
		},
	},
	{
		Type:         productenums.PricingRuleCostMarkup,
		RequiresCost: true,
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			return normalizeSinglePricingParam(params, "amount", 0, pricingMarkupMax)
		},
		Compute: func(cost float64, params json.RawMessage) (float64, error) {
			v, ok := pricingParamFloat(params, "amount")
			if !ok {
				return 0, pricingParamsErr(i18n.ErrorDetail(productenums.DetailPricingParamMissingOrNaN, "field", "amount"))
			}
			return cost + v, nil
		},
	},
	{
		Type:         productenums.PricingRuleTargetMargin,
		RequiresCost: true,
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			return normalizeSinglePricingParam(params, "margin", pricingMarginMin, pricingMarginMax)
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
	},
	{
		Type:         productenums.PricingRuleFixedPrice,
		RequiresCost: false,
		Normalize: func(params json.RawMessage) (json.RawMessage, error) {
			return normalizeSinglePricingParam(params, "amount", 0, pricingAmountMax)
		},
		Compute: func(_ float64, params json.RawMessage) (float64, error) {
			v, ok := pricingParamFloat(params, "amount")
			if !ok {
				return 0, pricingParamsErr(i18n.ErrorDetail(productenums.DetailPricingParamMissingOrNaN, "field", "amount"))
			}
			return v, nil
		},
	},
}

func lookupPricingRule(ruleType string) *pricingRule {
	ruleType = strings.TrimSpace(ruleType)
	for _, r := range pricingRules {
		if r.Type == ruleType {
			return r
		}
	}
	return nil
}

func pricingRuleValues(ruleType string, params json.RawMessage) productdto.RuleValuesResp {
	var out productdto.RuleValuesResp
	r := lookupPricingRule(ruleType)
	if r == nil {
		return out
	}
	key := map[string]string{
		productenums.PricingRuleCostMultiple: "multiplier",
		productenums.PricingRuleCostMarkup:   "amount",
		productenums.PricingRuleTargetMargin: "margin",
		productenums.PricingRuleFixedPrice:   "amount",
	}[r.Type]
	if key == "" {
		return out
	}
	if v, ok := pricingParamFloat(params, key); ok {
		out.Value, out.HasValue = v, true
	}
	return out
}

func pricingFilterParts(raw json.RawMessage) []productdto.PricingFilterPartResp {
	m, err := decodeRuleParams(raw)
	if err != nil || len(m) == 0 {
		return nil
	}
	keys := []string{"status", "keyword", "categoryId", "brandId", "tagId"}
	out := make([]productdto.PricingFilterPartResp, 0, len(keys))
	for _, k := range keys {
		rawValue, ok := m[k]
		if !ok {
			continue
		}
		var value string
		if jerr := json.Unmarshal(rawValue, &value); jerr != nil {
			value = strings.TrimSpace(string(rawValue))
		}
		if value == "" {
			continue
		}
		out = append(out, productdto.PricingFilterPartResp{Key: k, Value: value})
	}
	return out
}

func normalizePricingRuleParams(ruleType string, params json.RawMessage) (norm json.RawMessage, err error) {
	r := lookupPricingRule(ruleType)
	if r == nil {
		return nil, pricingRuleTypeErr(ruleType)
	}
	return r.Normalize(params)
}

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

type pricingRounding struct {
	Value string
	Name  string

	Apply func(amount float64) int64
}

var pricingRoundings = []*pricingRounding{
	{
		Value: productenums.PricingRoundingNone,
		Apply: func(amount float64) int64 { return int64(math.Round(amount * 100)) },
	},
	{
		Value: productenums.PricingRoundingInteger,

		Apply: func(amount float64) int64 { return ceilToStep(pricingCeilCents(amount), 100) },
	},
	{
		Value: productenums.PricingRoundingEnd9,

		Apply: func(amount float64) int64 { return ceilToCentsRemainder(pricingCeilCents(amount), 90) },
	},
	{
		Value: productenums.PricingRoundingEnd99,

		Apply: func(amount float64) int64 { return ceilToCentsRemainder(pricingCeilCents(amount), 99) },
	},
}

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

func normalizePricingRounding(rounding string) (out string, err error) {
	r := lookupPricingRounding(rounding)
	if r == nil {
		return "", pricingRoundingErr(rounding)
	}
	return r.Value, nil
}

func applyPricingRounding(amount float64, rounding string) (out float64, err error) {
	r := lookupPricingRounding(rounding)
	if r == nil {
		return 0, pricingRoundingErr(rounding)
	}
	return float64(r.Apply(amount)) / 100, nil
}

func pricingCeilCents(amount float64) int64 {
	return int64(math.Ceil(amount*100 - 1e-9))
}

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

func normalizePricingScope(scope string) (out string, err error) {
	switch strings.TrimSpace(scope) {
	case productenums.PricingScopeSKU:
		return productenums.PricingScopeSKU, nil
	case productenums.PricingScopeProduct:
		return productenums.PricingScopeProduct, nil
	case productenums.PricingScopeFilter:
		return productenums.PricingScopeFilter, nil
	}
	return "", productenums.Err(productenums.ErrPricingScopeInvalid)
}

func pricingRuleTypeOptions() (out []*productdto.PricingRuleTypeResp) {
	out = make([]*productdto.PricingRuleTypeResp, 0, len(pricingRules))
	for _, r := range pricingRules {
		out = append(out, &productdto.PricingRuleTypeResp{Type: r.Type, RequiresCost: r.RequiresCost})
	}
	return out
}

func pricingRoundingOptions() (out []*productdto.PricingRoundingOptionResp) {
	out = make([]*productdto.PricingRoundingOptionResp, 0, len(pricingRoundings))
	for _, r := range pricingRoundings {
		out = append(out, &productdto.PricingRoundingOptionResp{Value: r.Value})
	}
	return out
}

func formatPricingNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func formatPricingPercent(v float64) string { return formatPricingNumber(v*100) + "%" }

func pricingParamsErr(detail string) error {
	return fmt.Errorf("%w: %s", productenums.Err(productenums.ErrPricingRuleParamsInvalid), detail)
}

func pricingRuleTypeErr(ruleType string) error {
	if strings.TrimSpace(ruleType) == "" {
		return productenums.Err(productenums.ErrPricingRuleTypeInvalid)
	}
	return fmt.Errorf("%w: %s", productenums.Err(productenums.ErrPricingRuleTypeInvalid),
		i18n.ErrorDetail(productenums.DetailPricingUnknownRuleType, "type", ruleType))
}

func pricingRoundingErr(rounding string) error {
	if strings.TrimSpace(rounding) == "" {
		return productenums.Err(productenums.ErrPricingRoundingInvalid)
	}
	return fmt.Errorf("%w: %s", productenums.Err(productenums.ErrPricingRoundingInvalid),
		i18n.ErrorDetail(productenums.DetailPricingUnknownRounding, "value", rounding))
}
