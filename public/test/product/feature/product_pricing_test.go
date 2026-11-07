package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/pkg/builder/core"
	text "go_wp/pkg/view/jet"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

type pricingFixture struct {
	svc       *productservice.Service
	db        *gorm.DB
	projects  *projectservice.Service
	registry  core.EntitySourceRegistry
	projectID string
}

func newPricingFixture(t *testing.T) *pricingFixture {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "定价测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	registry := core.NewEntitySourceRegistry()
	svc := productservice.NewService(productmodel.NewModel(db), projects, masterdataservice.NewService(masterdatamodel.NewModel(db), projects))
	if err := svc.RegisterEntityTypes(registry); err != nil {
		t.Fatalf("注册商品域实体类型失败: %v", err)
	}
	return &pricingFixture{svc: svc, db: db, projects: projects, registry: registry, projectID: project.ID}
}

func pfCreateProduct(t *testing.T, f *pricingFixture, name string, price, cost float64) *productdto.ProductResp {
	t.Helper()
	ctx := context.Background()
	zero := 0.0
	created, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, DefaultPrice: &zero,
	})
	if err != nil {
		t.Fatalf("建商品 %s 失败: %v", name, err)
	}
	if len(created.Variants) == 0 {
		t.Fatalf("商品 %s 应自带首个变体", name)
	}
	pv, cv := price, cost
	if _, err = f.svc.UpdateVariant(ctx, &productdto.UpdateVariantReq{
		ID: created.Variants[0].ID, Price: &pv, CostPrice: &cv,
	}); err != nil {
		t.Fatalf("设置首个变体价格失败: %v", err)
	}
	return pfGetProduct(t, f, created.ID)
}

func pfAddVariant(t *testing.T, f *pricingFixture, productID, sku string, price, cost float64) *productdto.VariantResp {
	t.Helper()
	req := &productdto.CreateVariantReq{ProductID: productID, SKUCode: sku}
	if price >= 0 {
		req.Price = &price
	}
	if cost >= 0 {
		req.CostPrice = &cost
	}
	v, err := f.svc.CreateVariant(context.Background(), req)
	if err != nil {
		t.Fatalf("新增变体 %s 失败: %v", sku, err)
	}
	return v
}

func pfGetProduct(t *testing.T, f *pricingFixture, productID string) *productdto.ProductResp {
	t.Helper()
	detail, err := f.svc.Get(context.Background(), &productdto.GetReq{ProjectID: f.projectID, ID: productID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	return detail
}

func pfVariantBySKU(t *testing.T, p *productdto.ProductResp, sku string) *productdto.VariantResp {
	t.Helper()
	for _, v := range p.Variants {
		if v.SKUCode == sku {
			return v
		}
	}
	t.Fatalf("商品 %s 下找不到 SKU %s", p.Name, sku)
	return nil
}

func pfPriceOf(t *testing.T, f *pricingFixture, sku string) float64 {
	t.Helper()
	var price float64
	if err := f.db.Raw("SELECT price FROM product_variants WHERE sku_code = ?", sku).Scan(&price).Error; err != nil {
		t.Fatalf("读变体价格失败: %v", err)
	}
	return price
}

func pfCountAdjustments(t *testing.T, f *pricingFixture) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM product_price_adjustments").Scan(&n).Error; err != nil {
		t.Fatalf("统计调价批次失败: %v", err)
	}
	return n
}

func pfReq(f *pricingFixture, ruleType, params, rounding, scope, targetID string) productdto.PricingRuleReq {
	req := productdto.PricingRuleReq{
		ProjectID: f.projectID, RuleType: ruleType, Rounding: rounding, Scope: scope, TargetID: targetID,
	}
	if params != "" {
		req.RuleParams = json.RawMessage(params)
	}
	return req
}

func TestPricingRuleValidation(t *testing.T) {
	f := newPricingFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p := pfCreateProduct(t, f, "规则校验商品", 100, 40)

	cases := []struct {
		name   string
		rule   string
		params string
		round  string
		scope  string

		noTarget bool
		want     string
	}{
		{"未知规则类型", "free_sql", "{}", "", productenums.PricingScopeProduct, false, productenums.ErrPricingRuleTypeInvalid},
		{"空规则类型", "", "{}", "", productenums.PricingScopeProduct, false, productenums.ErrPricingRuleTypeInvalid},
		{"缺 multiplier", productenums.PricingRuleCostMultiple, "{}", "", productenums.PricingScopeProduct, false, productenums.ErrPricingRuleParamsInvalid},
		{"multiplier 未知键", productenums.PricingRuleCostMultiple, "{\"multiplier\":1.5,\"expr\":\"price*2\"}", "", productenums.PricingScopeProduct, false, productenums.ErrPricingRuleParamsInvalid},
		{"multiplier 为 0", productenums.PricingRuleCostMultiple, "{\"multiplier\":0}", "", productenums.PricingScopeProduct, false, productenums.ErrPricingRuleParamsInvalid},
		{"multiplier 越界", productenums.PricingRuleCostMultiple, "{\"multiplier\":101}", "", productenums.PricingScopeProduct, false, productenums.ErrPricingRuleParamsInvalid},
		{"multiplier 是字符串", productenums.PricingRuleCostMultiple, "{\"multiplier\":\"1.5\"}", "", productenums.PricingScopeProduct, false, productenums.ErrPricingRuleParamsInvalid},
		{"加价负数", productenums.PricingRuleCostMarkup, "{\"amount\":-1}", "", productenums.PricingScopeProduct, false, productenums.ErrPricingRuleParamsInvalid},
		{"毛利率为 0", productenums.PricingRuleTargetMargin, "{\"margin\":0}", "", productenums.PricingScopeProduct, false, productenums.ErrPricingRuleParamsInvalid},
		{"毛利率为 1", productenums.PricingRuleTargetMargin, "{\"margin\":1}", "", productenums.PricingScopeProduct, false, productenums.ErrPricingRuleParamsInvalid},
		{"毛利率越界", productenums.PricingRuleTargetMargin, "{\"margin\":1.5}", "", productenums.PricingScopeProduct, false, productenums.ErrPricingRuleParamsInvalid},
		{"参数不是对象", productenums.PricingRuleFixedPrice, "[88]", "", productenums.PricingScopeProduct, false, productenums.ErrPricingRuleParamsInvalid},
		{"统一售价超上限", productenums.PricingRuleFixedPrice, "{\"amount\":10000000000}", "", productenums.PricingScopeProduct, false, productenums.ErrPricingRuleParamsInvalid},
		{"未知尾数处理", productenums.PricingRuleFixedPrice, "{\"amount\":88}", "half_up", productenums.PricingScopeProduct, false, productenums.ErrPricingRoundingInvalid},
		{"未知作用范围", productenums.PricingRuleFixedPrice, "{\"amount\":88}", "", "everything", false, productenums.ErrPricingScopeInvalid},
		{"SKU 范围缺目标", productenums.PricingRuleFixedPrice, "{\"amount\":88}", "", productenums.PricingScopeSKU, true, productenums.ErrPricingTargetRequired},
		{"商品范围缺目标", productenums.PricingRuleFixedPrice, "{\"amount\":88}", "", productenums.PricingScopeProduct, true, productenums.ErrPricingTargetRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := p.ID
			if tc.noTarget {
				target = ""
			}
			_, err := f.svc.PreviewPricing(ctx, &productdto.PricingPreviewReq{
				PricingRuleReq: pfReq(f, tc.rule, tc.params, tc.round, tc.scope, target),
			})
			if err == nil {
				t.Fatalf("非法输入应被拒绝，实际通过")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("应返回 %s，实际 %v", tc.want, err)
			}
		})
	}

	missing := "00000000-0000-0000-0000-000000000000"
	for _, scope := range []string{productenums.PricingScopeSKU, productenums.PricingScopeProduct} {
		_, err := f.svc.PreviewPricing(ctx, &productdto.PricingPreviewReq{
			PricingRuleReq: pfReq(f, productenums.PricingRuleFixedPrice, "{\"amount\":88}", "", scope, missing),
		})
		if err == nil || !strings.Contains(err.Error(), productenums.ErrPricingTargetNotFound) {
			t.Fatalf("%s 范围目标不存在应返回 ErrPricingTargetNotFound，实际 %v", scope, err)
		}
	}

	_, err := f.svc.PreviewPricing(ctx, &productdto.PricingPreviewReq{
		PricingRuleReq: productdto.PricingRuleReq{
			RuleType: productenums.PricingRuleFixedPrice, RuleParams: json.RawMessage("{\"amount\":88}"),
			Scope: productenums.PricingScopeFilter,
		},
	})
	if err == nil || !strings.Contains(err.Error(), productenums.ErrInvalidParam) {
		t.Fatalf("筛选集缺工程应被拒绝，实际 %v", err)
	}

	if rules := f.svc.ListPricingRuleTypes(ctx); len(rules) != 4 {
		t.Fatalf("应有 4 种内置定价规则，实际 %d", len(rules))
	}
	if roundings := f.svc.ListPricingRoundingOptions(ctx); len(roundings) != 4 {
		t.Fatalf("应有 4 种尾数处理，实际 %d", len(roundings))
	}
}

func TestPricingFourRulesAndRounding(t *testing.T) {
	f := newPricingFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	p := pfCreateProduct(t, f, "算价商品", 1, 10.28)

	preview := func(ruleType, params, rounding string) *productdto.PricingPreviewResp {
		t.Helper()
		res, err := f.svc.PreviewPricing(ctx, &productdto.PricingPreviewReq{
			PricingRuleReq: pfReq(f, ruleType, params, rounding, productenums.PricingScopeProduct, p.ID),
		})
		if err != nil {
			t.Fatalf("试算失败（%s/%s/%s）: %v", ruleType, params, rounding, err)
		}
		if len(res.Lines) != 1 {
			t.Fatalf("应有 1 行试算明细，实际 %d", len(res.Lines))
		}
		return res
	}

	cases := []struct {
		name     string
		ruleType string
		params   string
		rounding string
		want     float64
		wantVal  float64
	}{
		{"成本乘倍数 不舍入", productenums.PricingRuleCostMultiple, "{\"multiplier\":1.2}", productenums.PricingRoundingNone, 12.34, 1.2},
		{"成本乘倍数 取整到元", productenums.PricingRuleCostMultiple, "{\"multiplier\":1.2}", productenums.PricingRoundingInteger, 13, 1.2},
		{"成本乘倍数 尾数 9", productenums.PricingRuleCostMultiple, "{\"multiplier\":1.2}", productenums.PricingRoundingEnd9, 12.90, 1.2},
		{"成本乘倍数 尾数 99", productenums.PricingRuleCostMultiple, "{\"multiplier\":1.2}", productenums.PricingRoundingEnd99, 12.99, 1.2},
		{"成本加价 不舍入", productenums.PricingRuleCostMarkup, "{\"amount\":4.72}", productenums.PricingRoundingNone, 15, 4.72},
		{"成本加价 尾数 99", productenums.PricingRuleCostMarkup, "{\"amount\":4.72}", productenums.PricingRoundingEnd99, 15.99, 4.72},
		{"目标毛利率", productenums.PricingRuleTargetMargin, "{\"margin\":0.2}", productenums.PricingRoundingNone, 12.85, 0.2},
		{"统一售价（不看成本）", productenums.PricingRuleFixedPrice, "{\"amount\":88.5}", productenums.PricingRoundingNone, 88.5, 88.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := preview(tc.ruleType, tc.params, tc.rounding)
			if res.Lines[0].NewPrice != tc.want {
				t.Fatalf("新价应为 %v，实际 %v", tc.want, res.Lines[0].NewPrice)
			}
			if res.RuleType != tc.ruleType || !res.RuleValues.HasValue || res.RuleValues.Value != tc.wantVal {
				t.Fatalf("规则只给取值：期望 %s/%v，实际 %s/%+v", tc.ruleType, tc.wantVal, res.RuleType, res.RuleValues)
			}
			if res.Rounding != tc.rounding {
				t.Fatalf("尾数档应为 %q，实际 %q", tc.rounding, res.Rounding)
			}
			if res.ChangedCount != 1 {
				t.Fatalf("应算出 1 条改动，实际 %d", res.ChangedCount)
			}
		})
	}

	if res := preview(productenums.PricingRuleFixedPrice, "{\"amount\":12.9}", productenums.PricingRoundingEnd9); res.Lines[0].NewPrice != 12.90 {
		t.Fatalf("尾数 9 对已满足的价格应保持不变，实际 %v", res.Lines[0].NewPrice)
	}
	if res := preview(productenums.PricingRuleFixedPrice, "{\"amount\":12.91}", productenums.PricingRoundingEnd9); res.Lines[0].NewPrice != 13.90 {
		t.Fatalf("尾数 9 应向上进位到 13.90，实际 %v", res.Lines[0].NewPrice)
	}
	if res := preview(productenums.PricingRuleFixedPrice, "{\"amount\":12.99}", productenums.PricingRoundingEnd99); res.Lines[0].NewPrice != 12.99 {
		t.Fatalf("尾数 99 对已满足的价格应保持不变，实际 %v", res.Lines[0].NewPrice)
	}
	if res := preview(productenums.PricingRuleFixedPrice, "{\"amount\":13}", productenums.PricingRoundingEnd99); res.Lines[0].NewPrice != 13.99 {
		t.Fatalf("尾数 99 应向上进位到 13.99，实际 %v", res.Lines[0].NewPrice)
	}

	pfAddVariant(t, f, p.ID, "no-cost-sku", 500, -1)
	skipped, err := f.svc.PreviewPricing(ctx, &productdto.PricingPreviewReq{
		PricingRuleReq: pfReq(f, productenums.PricingRuleCostMultiple, "{\"multiplier\":2}", "", productenums.PricingScopeProduct, p.ID),
	})
	if err != nil {
		t.Fatalf("试算失败: %v", err)
	}
	if skipped.SkippedCount != 1 || skipped.ChangedCount != 1 {
		t.Fatalf("缺成本价的变体应恰好跳过 1 条、改动 1 条，实际 %+v", skipped)
	}
	var skipLine *productdto.PricingLineResp
	for _, l := range skipped.Lines {
		if l.Status == productenums.PricingLineSkipped {
			skipLine = l
		}
	}
	if skipLine == nil || skipLine.Reason != productenums.PricingSkipCostMissing {
		t.Fatalf("跳过行应带原因与可读说明，实际 %+v", skipLine)
	}
	fixed, err := f.svc.PreviewPricing(ctx, &productdto.PricingPreviewReq{
		PricingRuleReq: pfReq(f, productenums.PricingRuleFixedPrice, "{\"amount\":9}", "", productenums.PricingScopeProduct, p.ID),
	})
	if err != nil {
		t.Fatalf("试算失败: %v", err)
	}
	if fixed.SkippedCount != 0 || fixed.ChangedCount != 2 {
		t.Fatalf("统一售价规则不该跳过任何变体，实际 %+v", fixed)
	}
}

func TestPricingScopeSKUAndProduct(t *testing.T) {
	f := newPricingFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p := pfCreateProduct(t, f, "多规格商品", 100, 30)
	pfAddVariant(t, f, p.ID, "sku-b", 200, 60)
	pfAddVariant(t, f, p.ID, "sku-c", 300, 90)
	detail := pfGetProduct(t, f, p.ID)
	if detail.VariantCount != 3 {
		t.Fatalf("应有 3 个变体，实际 %d", detail.VariantCount)
	}
	target := pfVariantBySKU(t, detail, "sku-b")

	res, err := f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{
		PricingRuleReq: pfReq(f, productenums.PricingRuleFixedPrice, "{\"amount\":111}", "", productenums.PricingScopeSKU, target.ID),
		Note:           "只改一个 SKU",
	})
	if err != nil {
		t.Fatalf("按 SKU 应用调价失败: %v", err)
	}
	if res.ChangedCount != 1 || res.TargetCount != 1 {
		t.Fatalf("SKU 范围应只涉及 1 个变体，实际 %+v", res)
	}
	after := pfGetProduct(t, f, p.ID)
	if got := pfVariantBySKU(t, after, "sku-b").Price; got != 111 {
		t.Fatalf("目标 SKU 应被改动，实际 %v", got)
	}
	if got := pfVariantBySKU(t, after, "sku-c").Price; got != 300 {
		t.Fatalf("非目标 SKU 不该被改动，实际 %v", got)
	}

	res, err = f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{
		PricingRuleReq: pfReq(f, productenums.PricingRuleCostMultiple, "{\"multiplier\":2}", productenums.PricingRoundingEnd9, productenums.PricingScopeProduct, p.ID),
	})
	if err != nil {
		t.Fatalf("按商品应用调价失败: %v", err)
	}
	if res.ChangedCount != 3 || res.TargetCount != 3 {
		t.Fatalf("商品范围应涉及 3 个变体，实际 %+v", res)
	}
	after = pfGetProduct(t, f, p.ID)
	for _, want := range []struct {
		sku   string
		price float64
	}{{"sku-b", 120.90}, {"sku-c", 180.90}} {
		if got := pfVariantBySKU(t, after, want.sku).Price; got != want.price {
			t.Fatalf("%s 应改为 %v，实际 %v", want.sku, want.price, got)
		}
	}

	if got := pfVariantBySKU(t, after, "sku-b"); got.CostPrice == nil || *got.CostPrice != 60 {
		t.Fatalf("成本价不该被动，实际 %+v", got.CostPrice)
	}
	if price := pfPriceOf(t, f, "sku-c"); price != 180.90 {
		t.Fatalf("落库价格应为 180.90，实际 %v", price)
	}
}

func TestPricingFilterScope(t *testing.T) {
	f := newPricingFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	cat, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "男装", Slug: "men"})
	if err != nil {
		t.Fatalf("建分类失败: %v", err)
	}
	brand, err := f.svc.CreateBrand(ctx, &productdto.CreateBrandReq{ProjectID: f.projectID, Name: "自家品牌", Slug: "own"})
	if err != nil {
		t.Fatalf("建品牌失败: %v", err)
	}
	tag, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{ProjectID: f.projectID, Name: "清仓", Slug: "clearance"})
	if err != nil {
		t.Fatalf("建标签失败: %v", err)
	}

	zero := 0.0

	hit, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "春季夹克", DefaultPrice: &zero,
		CategoryIDs: []string{cat.ID}, BrandID: brand.ID, TagIDs: []string{tag.ID},
	})
	if err != nil {
		t.Fatalf("建命中商品失败: %v", err)
	}
	published := productenums.StatusPublished
	if _, err = f.svc.Update(ctx, &productdto.UpdateReq{ID: hit.ID, Status: &published}); err != nil {
		t.Fatalf("上架失败: %v", err)
	}

	miss, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "夏季短裤", DefaultPrice: &zero})
	if err != nil {
		t.Fatalf("建无关商品失败: %v", err)
	}

	apply := func(amount string, mutate func(*productdto.PricingRuleReq)) *productdto.PricingApplyResp {
		t.Helper()
		req := productdto.PricingRuleReq{
			ProjectID: f.projectID, RuleType: productenums.PricingRuleFixedPrice,
			RuleParams: json.RawMessage("{\"amount\":" + amount + "}"),
			Rounding:   productenums.PricingRoundingEnd9,
			Scope:      productenums.PricingScopeFilter,
		}
		mutate(&req)
		res, aerr := f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{PricingRuleReq: req})
		if aerr != nil {
			t.Fatalf("按筛选集应用调价失败: %v", aerr)
		}
		return res
	}

	if res := apply("59", func(r *productdto.PricingRuleReq) { r.Status = productenums.StatusPublished }); res.ChangedCount != 1 {
		t.Fatalf("状态筛选应命中 1 个商品，实际 %+v", res)
	}
	if got := pfGetProduct(t, f, hit.ID).Variants[0].Price; got != 59.90 {
		t.Fatalf("命中商品应改为 59.90，实际 %v", got)
	}
	if got := pfGetProduct(t, f, miss.ID).Variants[0].Price; got != 0 {
		t.Fatalf("未命中商品不该被改动，实际 %v", got)
	}

	if _, err = f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{
		PricingRuleReq: pfReq(f, productenums.PricingRuleFixedPrice, "{\"amount\":1}", "", productenums.PricingScopeFilter, ""),
	}); err == nil || !strings.Contains(err.Error(), productenums.ErrPricingFilterEmpty) {
		t.Fatalf("无条件的筛选集应返回 ErrPricingFilterEmpty，实际 %v", err)
	}

	if _, err = f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{
		PricingRuleReq: func() productdto.PricingRuleReq {
			r := pfReq(f, productenums.PricingRuleFixedPrice, "{\"amount\":1}", "", productenums.PricingScopeFilter, "")
			r.Keyword = "根本不存在的商品名"
			return r
		}(),
	}); err == nil || !strings.Contains(err.Error(), productenums.ErrPricingFilterEmpty) {
		t.Fatalf("无命中的筛选集应返回 ErrPricingFilterEmpty，实际 %v", err)
	}

	if res := apply("61", func(r *productdto.PricingRuleReq) { r.CategoryID = cat.ID }); res.ChangedCount != 1 {
		t.Fatalf("分类筛选应命中 1 个商品，实际 %+v", res)
	}

	if res := apply("62", func(r *productdto.PricingRuleReq) { r.BrandID = brand.ID }); res.ChangedCount != 1 {
		t.Fatalf("品牌筛选应命中 1 个商品，实际 %+v", res)
	}

	if res := apply("63", func(r *productdto.PricingRuleReq) { r.TagID = tag.ID }); res.ChangedCount != 1 {
		t.Fatalf("标签筛选应命中 1 个商品，实际 %+v", res)
	}

	if res := apply("64", func(r *productdto.PricingRuleReq) { r.Keyword = "春季" }); res.ChangedCount != 1 {
		t.Fatalf("关键词筛选应命中 1 个商品，实际 %+v", res)
	}

	other, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "另一个工程"})
	if err != nil {
		t.Fatalf("建第二工程失败: %v", err)
	}
	otherCat, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: other.ID, Name: "别家男装", Slug: "other-men"})
	if err != nil {
		t.Fatalf("在他工程建分类失败: %v", err)
	}
	otherProduct, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: other.ID, Name: "别家夹克", DefaultPrice: &zero, CategoryIDs: []string{otherCat.ID},
	})
	if err != nil {
		t.Fatalf("建他工程商品失败: %v", err)
	}
	if res := apply("65", func(r *productdto.PricingRuleReq) {
		r.CategoryID = cat.ID
		r.Keyword = "夹克"
	}); res.ChangedCount != 1 {
		t.Fatalf("筛选集应只命中本工程商品，实际 %+v", res)
	}

	otherDetail, err := f.svc.Get(ctx, &productdto.GetReq{ProjectID: other.ID, ID: otherProduct.ID})
	if err != nil {
		t.Fatalf("读他工程商品失败: %v", err)
	}
	if got := otherDetail.Variants[0].Price; got != 0 {
		t.Fatalf("跨工程商品不该被改动，实际 %v", got)
	}
}

func TestPricingPreviewThenApplyWithAudit(t *testing.T) {
	f := newPricingFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p := pfCreateProduct(t, f, "留痕商品", 500, 200)
	pfAddVariant(t, f, p.ID, "audit-sku-b", 600, 240)
	detail := pfGetProduct(t, f, p.ID)
	if detail.VariantCount != 2 {
		t.Fatalf("应有 2 个变体，实际 %d", detail.VariantCount)
	}

	req := pfReq(f, productenums.PricingRuleCostMultiple, "{\"multiplier\":1.5}", productenums.PricingRoundingNone, productenums.PricingScopeProduct, p.ID)

	preview, err := f.svc.PreviewPricing(ctx, &productdto.PricingPreviewReq{PricingRuleReq: req})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if preview.ChangedCount != 2 || preview.UnchangedCount != 0 || preview.SkippedCount != 0 {
		t.Fatalf("预览计数不符：%+v", preview)
	}
	if preview.Rounding != req.Rounding || preview.Scope != req.Scope {
		t.Fatalf("预览应带回尾数与范围的取值：%+v", preview)
	}
	byOld := map[float64]float64{}
	for _, line := range preview.Lines {
		byOld[line.OldPrice] = line.NewPrice
		if line.Status != productenums.PricingLineChanged {
			t.Fatalf("预览行应为「改价」，实际 %s", line.Status)
		}
	}
	if byOld[500] != 300 || byOld[600] != 360 {
		t.Fatalf("预览应是 500→300 / 600→360（成本乘倍数用成本价），实际 %+v", byOld)
	}
	if got := pfGetProduct(t, f, p.ID).Variants[0].Price; got != 500 {
		t.Fatalf("预览不该改库，实际 %v", got)
	}
	if n := pfCountAdjustments(t, f); n != 0 {
		t.Fatalf("预览不该留痕，实际 %d 条", n)
	}

	applied, err := f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{
		PricingRuleReq: req, Note: "成本上涨，整店上调", OperatorID: "42",
	})
	if err != nil {
		t.Fatalf("应用调价失败: %v", err)
	}
	if applied.ChangedCount != 2 || applied.AdjustmentID == "" || len(applied.Lines) != 2 {
		t.Fatalf("应用结果不符：%+v", applied)
	}
	if got := pfPriceOf(t, f, "audit-sku-b"); got != 360 {
		t.Fatalf("落库价格应为 360，实际 %v", got)
	}

	list, err := f.svc.ListPriceAdjustments(ctx, &productdto.ListPriceAdjustmentReq{ProjectID: f.projectID})
	if err != nil || len(list) != 1 {
		t.Fatalf("应有 1 条留痕，实际 %v %+v", err, list)
	}
	batch := list[0]
	if batch.RuleType != productenums.PricingRuleCostMultiple || !batch.RuleValues.HasValue || batch.RuleValues.Value != 1.5 {
		t.Fatalf("留痕批次应记录规则取值：%+v", batch)
	}
	if batch.Rounding == "" || batch.Scope != productenums.PricingScopeProduct {
		t.Fatalf("留痕批次应记录尾数档与范围取值：%+v", batch)
	}
	if batch.Note != "成本上涨，整店上调" || batch.OperatorID != "42" {
		t.Fatalf("留痕应记录操作人与备注：%+v", batch)
	}
	if batch.ChangedCount != 2 || batch.VariantCount != 2 || batch.TargetID != p.ID {
		t.Fatalf("留痕计数与目标不符：%+v", batch)
	}

	detailBatch, err := f.svc.GetPriceAdjustment(ctx, &productdto.GetPriceAdjustmentReq{ID: batch.ID})
	if err != nil {
		t.Fatalf("读留痕详情失败: %v", err)
	}
	if len(detailBatch.Items) != 2 {
		t.Fatalf("应有 2 条逐变体明细，实际 %d", len(detailBatch.Items))
	}
	itemByOld := map[float64]float64{}
	for _, it := range detailBatch.Items {
		itemByOld[it.OldPrice] = it.NewPrice
		if it.ProductName == "" || it.SKUCode == "" {
			t.Fatalf("明细应带商品名与 SKU 快照：%+v", it)
		}
	}
	if itemByOld[500] != 300 || itemByOld[600] != 360 {
		t.Fatalf("明细应是原价→新价，实际 %+v", itemByOld)
	}

	if _, err = f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{PricingRuleReq: req}); err == nil ||
		!strings.Contains(err.Error(), productenums.ErrPricingNothingChanged) {
		t.Fatalf("无改动时应返回 ErrPricingNothingChanged，实际 %v", err)
	}
	if n := pfCountAdjustments(t, f); n != 1 {
		t.Fatalf("无改动的应用不该留痕，实际 %d 条", n)
	}

	missing := "00000000-0000-0000-0000-000000000000"
	if _, err = f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{
		PricingRuleReq: pfReq(f, productenums.PricingRuleFixedPrice, "{\"amount\":1}", "", productenums.PricingScopeProduct, missing),
	}); err == nil {
		t.Fatalf("目标不存在应报错")
	}
	if n := pfCountAdjustments(t, f); n != 1 {
		t.Fatalf("失败的应用不该留痕，实际 %d 条", n)
	}
}

func TestPricingPersistsValueReadByBuildTime(t *testing.T) {
	f := newPricingFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p := pfCreateProduct(t, f, "构建期读价商品", 500, 200)

	resolvePrice := func() string {
		t.Helper()
		buildCtx := core.WithBuildProjectID(ctx, f.projectID)
		resolver, err := f.registry.ResolverFor(buildCtx, "product", p.ID)
		if err != nil {
			t.Fatalf("取实体解析器失败: %v", err)
		}
		value, err := resolver.ResolveString("product.price")
		if err != nil {
			t.Fatalf("解析 price 失败: %v", err)
		}
		return value
	}
	if got := resolvePrice(); got != "500" {
		t.Fatalf("改价前构建期应读到 500，实际 %q", got)
	}

	if _, err := f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{
		PricingRuleReq: pfReq(f, productenums.PricingRuleFixedPrice, "{\"amount\":649.99}", "", productenums.PricingScopeProduct, p.ID),
	}); err != nil {
		t.Fatalf("应用调价失败: %v", err)
	}

	if got := resolvePrice(); got != "649.99" {
		t.Fatalf("改价后构建期应读到 649.99，实际 %q", got)
	}
	if got := pfPriceOf(t, f, pfGetProduct(t, f, p.ID).Variants[0].SKUCode); got != 649.99 {
		t.Fatalf("库里应存 649.99，实际 %v", got)
	}
}

func TestPricingHasNoBuildPipelineDependency(t *testing.T) {

	serviceDir := filepath.Join("..", "..", "..", "..", "internal", "module", "product", "service")
	banned := []string{
		"go_wp/pkg/builder",
		"go_wp/internal/pipeline",
		"go_wp/internal/artifact",
		"go_wp/internal/module/artifact",
		"go_wp/internal/module/publication",
	}
	for _, name := range []string{"product_pricing.go"} {
		raw, err := os.ReadFile(filepath.Join(serviceDir, name))
		if err != nil {
			t.Fatalf("读定价源码失败（%s）: %v", name, err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "\"") && !strings.HasPrefix(trimmed, "go_wp/") {
				continue
			}
			for _, b := range banned {
				if strings.Contains(trimmed, b) {
					t.Fatalf("%s 不应依赖构建管线（命中 %s）：%s", name, b, trimmed)
				}
			}
		}
	}
}

func TestPricingRecalculatesAutoTags(t *testing.T) {
	f := newPricingFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	tag, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{
		ProjectID: f.projectID, Name: "百元档", Kind: productenums.TagKindRule,
		RuleType: productenums.TagRulePriceRange, RuleParams: json.RawMessage("{\"minPrice\":100,\"maxPrice\":200}"),
	})
	if err != nil {
		t.Fatalf("建价格区间自动标签失败: %v", err)
	}
	p := pfCreateProduct(t, f, "标签联动商品", 500, 200)
	if hits := tagHitIDs(t, f, tag.ID); containsString(hits, p.ID) {
		t.Fatalf("500 元的商品不该命中 100~200 的标签")
	}

	res, err := f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{
		PricingRuleReq: pfReq(f, productenums.PricingRuleFixedPrice, "{\"amount\":150}", "", productenums.PricingScopeProduct, p.ID),
	})
	if err != nil {
		t.Fatalf("应用调价失败: %v", err)
	}
	if res.RecalculatedTags != 1 {
		t.Fatalf("应用调价后应重算 1 个自动标签，实际 %d", res.RecalculatedTags)
	}
	if hits := tagHitIDs(t, f, tag.ID); !containsString(hits, p.ID) {
		t.Fatalf("改价到 150 后应命中价格区间标签，实际 %v", hits)
	}
}

func tagHitIDs(t *testing.T, f *pricingFixture, tagID string) []string {
	t.Helper()
	hits, err := f.svc.ListTagProducts(context.Background(), &productdto.ListTagProductsReq{TagID: tagID})
	if err != nil {
		t.Fatalf("查询标签命中商品失败: %v", err)
	}
	out := make([]string, 0, len(hits.Items))
	for _, h := range hits.Items {
		out = append(out, h.ID)
	}
	return out
}

func TestPricingPermissionsAndMenusSeeded(t *testing.T) {
	f := newPricingFixture(t)
	if f == nil {
		return
	}
	if err := migrations.RunSeeds(f.db); err != nil {
		t.Fatalf("执行数据种子失败: %v", err)
	}
	var n int64
	permSQL := "SELECT COUNT(*) FROM sys_permission WHERE module = 'product' AND permission_code LIKE 'product:pricing_%'"
	if err := f.db.Raw(permSQL).Scan(&n).Error; err != nil {
		t.Fatalf("查询权限点失败: %v", err)
	}
	if n != 6 {
		t.Fatalf("迁移 096 应 seed 6 个定价权限点，实际 %d", n)
	}
	menuSQL := "SELECT COUNT(*) FROM sys_menus WHERE type = 2 AND title = '定价工具' AND deleted_at IS NULL"
	if err := f.db.Raw(menuSQL).Scan(&n).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("迁移 097 应 seed 定价工具后台菜单，实际 %d", n)
	}
}

func newPricingPageEngine(t *testing.T) (*gin.Engine, *pricingFixture) {
	t.Helper()
	f := newPricingFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	// 应用调价成功走共享成功出口（生产 assembly 同款接线）。
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { pageview.FailScene(c, "template", err) }))
	handle := producthttp.NewProductPageHandle(f.svc, f.svc)
	engine.GET("/admin/product-pricing", pageview.UseRenderer(pageview.RenderPage), handle.ProductPricingPage)
	engine.POST("/api/product/pricing/preview", pageview.HTMLFragment, pageview.UseRenderer(pageview.RenderPage), producthttp.NewProductHandle(f.svc).PreviewPricing)

	engine.POST("/api/product/pricing/apply", pageview.HTMLFragment, producthttp.NewProductHandle(f.svc).ApplyPricing)
	return engine, f
}

func TestPricingAdminPages(t *testing.T) {
	engine, f := newPricingPageEngine(t)
	if engine == nil {
		return
	}
	p := pfCreateProduct(t, f, "后台改价商品", 500, 200)

	rec := httptestGet(engine, "/admin/product-pricing?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("定价页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"定价工具", "成本乘倍数", "成本加价", "目标毛利率", "统一售价",
		"尾数 9", "尾数 99", "向上取整到元", "按规则改价", "预览试算（不落库）",
		"应用调价（落库）", "调价留痕", "csrf_token",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("定价页缺少 %q", want)
		}
	}

	form := url.Values{}
	form.Set("projectId", f.projectID)
	form.Set("ruleType", productenums.PricingRuleFixedPrice)
	form.Set("amount", "66")
	form.Set("rounding", productenums.PricingRoundingEnd99)
	form.Set("scope", productenums.PricingScopeProduct)
	form.Set("targetId", p.ID)
	rec = postForm(engine, "/api/product/pricing/preview", form)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("预览应返回 HTML 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	// 数值明细由 Jet 渲染；断言同一行里的新价与差额，业务口径不变。
	previewBody := rec.Body.String()
	if !strings.Contains(previewBody, "data-pricing-preview-result") || !strings.Contains(previewBody, "66.99") || !strings.Contains(previewBody, "-433.01") {
		t.Fatalf("预览应渲染新价与差额：%s", previewBody)
	}
	if strings.Count(previewBody, "<tr>") < 2 {
		t.Fatalf("预览应渲染一行明细：%s", previewBody)
	}
	if got := pfPriceOf(t, f, pfGetProduct(t, f, p.ID).Variants[0].SKUCode); got != 500 {
		t.Fatalf("页面试算不该改库，实际 %v", got)
	}

	bad := url.Values{}
	bad.Set("projectId", f.projectID)
	bad.Set("ruleType", productenums.PricingRuleCostMultiple)
	bad.Set("scope", productenums.PricingScopeProduct)
	bad.Set("targetId", p.ID)
	rec = postForm(engine, "/api/product/pricing/preview", bad)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), "data-ui-error") {
		t.Fatalf("非法参数应为400 JSON错误，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), productenums.ErrPricingRuleParamsInvalid) {
		t.Fatalf("页面上不应出现 enums 裸 key，实际 %s", rec.Body.String())
	}

	form.Set("note", "后台改价")
	rec = postForm(engine, "/api/product/pricing/apply", form)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), `role="status"`) {
		t.Fatalf("应用调价应返回 HTML 成功片段，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := pfPriceOf(t, f, pfGetProduct(t, f, p.ID).Variants[0].SKUCode); got != 66.99 {
		t.Fatalf("应用后库里应为 66.99，实际 %v", got)
	}
	rec = httptestGet(engine, "/admin/product-pricing?project="+f.projectID)
	historyBody := rec.Body.String()
	for _, want := range []string{"统一售价 66", "尾数 99", "后台改价", "66.99", "500"} {
		if !strings.Contains(historyBody, want) {
			t.Fatalf("留痕区缺少 %q", want)
		}
	}

	rec = postForm(engine, "/api/product/pricing/apply", form)
	if rec.Code == http.StatusOK || !strings.Contains(rec.Body.String(), "data-ui-error") || strings.TrimSpace(rec.Body.String()) == "" {
		t.Fatalf("无改动时应用应回失败 + 可读文案，实际 %d：%s", rec.Code, rec.Body.String())
	}
}

func mustPricingJSON(t *testing.T, data any) []byte {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// assertHTMLSuccess 断言写端点返回带状态区的 HTML 成功片段。
func assertHTMLSuccess(t *testing.T, rec *httptest.ResponseRecorder, action string) {
	t.Helper()
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), `role="status"`) {
		t.Fatalf("%s应返回 HTML 成功片段，实际 %d：%s", action, rec.Code, rec.Body.String())
	}
}
