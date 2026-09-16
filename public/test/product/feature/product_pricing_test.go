// Package feature product 模块 feature 测试 —— 定价工具（issue #13）。
//
// 覆盖本票五条验收（真实 PostgreSQL + 生产迁移 + 真实 service）：
//  1. 四种定价规则（成本乘倍数 / 成本加价 / 目标毛利率 / 统一售价）+ 尾数处理：
//     规则类型、参数键、尾数取值全部走内置白名单，非法一律拒绝（不接受自由表达式）；
//  2. 三种作用范围：单个 SKU、单个商品的全部变体、筛选集（工程 + 状态 / 关键词 / 分类 / 品牌 / 标签）；
//  3. 结果写库：算出的售价写回 product_variants.price，不是运行时计算；
//  4. 应用前可预览（预览不落库、不留痕），应用有留痕（批次 + 逐变体「原价 → 新价」）；
//  5. 不进构建管线：构建期（实体类型解析器）读到的就是落库后的确定值，
//     且定价源码不依赖 builder / pipeline / artifact。
package feature

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/builder/core"
	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// pricingFixture 隔离 PG schema + 生产迁移 + 真实工程行 + 商品 service + 实体类型注册表。
type pricingFixture struct {
	svc       *productservice.Service
	db        *gorm.DB
	projects  *projectservice.Service
	registry  core.EntitySourceRegistry
	projectID string
}

// newPricingFixture 装配 fixture；PG 不可用时 t.Skip（返回 nil）。
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
	svc := productservice.NewService(productmodel.NewModel(db), projects)
	if err := svc.RegisterEntityTypes(registry); err != nil {
		t.Fatalf("注册商品域实体类型失败: %v", err)
	}
	return &pricingFixture{svc: svc, db: db, projects: projects, registry: registry, projectID: project.ID}
}

// —— 测试内的小工具 ——

// pfCreateProduct 建商品并把它的首个变体设成指定的售价与成本价。
//
// 价格与成本只能落在变体上（商品主体不存价格，issue #5 已定语义），
// 所以这里建完商品再修一次首个变体。
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

// pfAddVariant 给商品加一个变体（price / cost 传负数表示不设该字段）。
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

// pfGetProduct 商品详情（含变体）。
func pfGetProduct(t *testing.T, f *pricingFixture, productID string) *productdto.ProductResp {
	t.Helper()
	detail, err := f.svc.Get(context.Background(), &productdto.GetReq{ProjectID: f.projectID, ID: productID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	return detail
}

// pfVariantBySKU 按 SKU 取变体（找不到即失败）。
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

// pfPriceOf SKU 的当前售价（直接读库，不经过任何派生逻辑）。
func pfPriceOf(t *testing.T, f *pricingFixture, sku string) float64 {
	t.Helper()
	var price float64
	if err := f.db.Raw("SELECT price FROM product_variants WHERE sku_code = ?", sku).Scan(&price).Error; err != nil {
		t.Fatalf("读变体价格失败: %v", err)
	}
	return price
}

// pfCountAdjustments 调价批次数。
func pfCountAdjustments(t *testing.T, f *pricingFixture) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM product_price_adjustments").Scan(&n).Error; err != nil {
		t.Fatalf("统计调价批次失败: %v", err)
	}
	return n
}

// pfReq 构造一条定价请求（三种范围共用）。
func pfReq(f *pricingFixture, ruleType, params, rounding, scope, targetID string) productdto.PricingRuleReq {
	req := productdto.PricingRuleReq{
		ProjectID: f.projectID, RuleType: ruleType, Rounding: rounding, Scope: scope, TargetID: targetID,
	}
	if params != "" {
		req.RuleParams = json.RawMessage(params)
	}
	return req
}

// —— 验收 1：四种规则 + 尾数处理，非法输入一律拒绝 ——

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
		// noTarget 为 true 表示这一行故意不传目标 id（测「缺目标」的拦截）。
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

	// 目标不存在（SKU / 商品两个范围）。
	missing := "00000000-0000-0000-0000-000000000000"
	for _, scope := range []string{productenums.PricingScopeSKU, productenums.PricingScopeProduct} {
		_, err := f.svc.PreviewPricing(ctx, &productdto.PricingPreviewReq{
			PricingRuleReq: pfReq(f, productenums.PricingRuleFixedPrice, "{\"amount\":88}", "", scope, missing),
		})
		if err == nil || !strings.Contains(err.Error(), productenums.ErrPricingTargetNotFound) {
			t.Fatalf("%s 范围目标不存在应返回 ErrPricingTargetNotFound，实际 %v", scope, err)
		}
	}

	// 筛选集范围必须指定工程：没有工程条件的批量改价会跨工程改到别人的商品上。
	_, err := f.svc.PreviewPricing(ctx, &productdto.PricingPreviewReq{
		PricingRuleReq: productdto.PricingRuleReq{
			RuleType: productenums.PricingRuleFixedPrice, RuleParams: json.RawMessage("{\"amount\":88}"),
			Scope: productenums.PricingScopeFilter,
		},
	})
	if err == nil || !strings.Contains(err.Error(), productenums.ErrInvalidParam) {
		t.Fatalf("筛选集缺工程应被拒绝，实际 %v", err)
	}

	// 内置清单只有一份：4 种规则 + 4 种尾数处理。
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
	// 成本 10.28 → 乘 1.2 = 12.336，正好能把四种尾数处理区分开。
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
		wantRule string
	}{
		{"成本乘倍数 不舍入", productenums.PricingRuleCostMultiple, "{\"multiplier\":1.2}", productenums.PricingRoundingNone, 12.34, "成本 × 1.2"},
		{"成本乘倍数 取整到元", productenums.PricingRuleCostMultiple, "{\"multiplier\":1.2}", productenums.PricingRoundingInteger, 13, "成本 × 1.2"},
		{"成本乘倍数 尾数 9", productenums.PricingRuleCostMultiple, "{\"multiplier\":1.2}", productenums.PricingRoundingEnd9, 12.90, "成本 × 1.2"},
		{"成本乘倍数 尾数 99", productenums.PricingRuleCostMultiple, "{\"multiplier\":1.2}", productenums.PricingRoundingEnd99, 12.99, "成本 × 1.2"},
		{"成本加价 不舍入", productenums.PricingRuleCostMarkup, "{\"amount\":4.72}", productenums.PricingRoundingNone, 15, "成本 + 4.72"},
		{"成本加价 尾数 99", productenums.PricingRuleCostMarkup, "{\"amount\":4.72}", productenums.PricingRoundingEnd99, 15.99, "成本 + 4.72"},
		{"目标毛利率", productenums.PricingRuleTargetMargin, "{\"margin\":0.2}", productenums.PricingRoundingNone, 12.85, "目标毛利率 20%"},
		{"统一售价（不看成本）", productenums.PricingRuleFixedPrice, "{\"amount\":88.5}", productenums.PricingRoundingNone, 88.5, "统一售价 88.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := preview(tc.ruleType, tc.params, tc.rounding)
			if res.Lines[0].NewPrice != tc.want {
				t.Fatalf("新价应为 %v，实际 %v", tc.want, res.Lines[0].NewPrice)
			}
			if res.RuleLabel != tc.wantRule {
				t.Fatalf("规则描述应为 %q，实际 %q", tc.wantRule, res.RuleLabel)
			}
			if res.ChangedCount != 1 {
				t.Fatalf("应算出 1 条改动，实际 %d", res.ChangedCount)
			}
		})
	}

	// 尾数处理一律向上取（只抬不降）：已满足尾数的价格保持不变，差一点就进位。
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

	// 缺成本价的变体按成本类规则被跳过（其余变体照常），统一售价规则不受影响。
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
	if skipLine == nil || skipLine.Reason != productenums.PricingSkipCostMissing || skipLine.ReasonLabel == "" {
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

// —— 验收 2：三种作用范围 ——

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

	// ① 单个 SKU：只有它变。
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

	// ② 单个商品的全部变体：三个都变。
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
	// 成本价不参与改动。
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
	// 命中全部筛选条件的商品。
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
	// 不命中的商品（草稿 + 无分类 / 品牌 / 标签 + 名称不同）。
	miss, err := f.svc.Create(ctx, &productdto.CreateReq{ProjectID: f.projectID, Name: "夏季短裤", DefaultPrice: &zero})
	if err != nil {
		t.Fatalf("建无关商品失败: %v", err)
	}

	// apply 用不同金额逐次应用：同一条规则应用到同一批商品上第二次会「无改动」，
	// 而「无改动」在本票里是明确报错的（不写空台账），故每次换一个金额。
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

	// ① 状态筛选。
	if res := apply("59", func(r *productdto.PricingRuleReq) { r.Status = productenums.StatusPublished }); res.ChangedCount != 1 {
		t.Fatalf("状态筛选应命中 1 个商品，实际 %+v", res)
	}
	if got := pfGetProduct(t, f, hit.ID).Variants[0].Price; got != 59.90 {
		t.Fatalf("命中商品应改为 59.90，实际 %v", got)
	}
	if got := pfGetProduct(t, f, miss.ID).Variants[0].Price; got != 0 {
		t.Fatalf("未命中商品不该被改动，实际 %v", got)
	}

	// ② 只有工程、没有其它条件的筛选集：明确报错，不静默全表改价。
	if _, err = f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{
		PricingRuleReq: pfReq(f, productenums.PricingRuleFixedPrice, "{\"amount\":1}", "", productenums.PricingScopeFilter, ""),
	}); err == nil || !strings.Contains(err.Error(), productenums.ErrPricingFilterEmpty) {
		t.Fatalf("无条件的筛选集应返回 ErrPricingFilterEmpty，实际 %v", err)
	}
	// ②' 有筛选条件但一个都没命中：同样明确报错，不静默成功。
	if _, err = f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{
		PricingRuleReq: func() productdto.PricingRuleReq {
			r := pfReq(f, productenums.PricingRuleFixedPrice, "{\"amount\":1}", "", productenums.PricingScopeFilter, "")
			r.Keyword = "根本不存在的商品名"
			return r
		}(),
	}); err == nil || !strings.Contains(err.Error(), productenums.ErrPricingFilterEmpty) {
		t.Fatalf("无命中的筛选集应返回 ErrPricingFilterEmpty，实际 %v", err)
	}
	// ③ 分类筛选。
	if res := apply("61", func(r *productdto.PricingRuleReq) { r.CategoryID = cat.ID }); res.ChangedCount != 1 {
		t.Fatalf("分类筛选应命中 1 个商品，实际 %+v", res)
	}
	// ④ 品牌筛选。
	if res := apply("62", func(r *productdto.PricingRuleReq) { r.BrandID = brand.ID }); res.ChangedCount != 1 {
		t.Fatalf("品牌筛选应命中 1 个商品，实际 %+v", res)
	}
	// ⑤ 标签筛选。
	if res := apply("63", func(r *productdto.PricingRuleReq) { r.TagID = tag.ID }); res.ChangedCount != 1 {
		t.Fatalf("标签筛选应命中 1 个商品，实际 %+v", res)
	}
	// ⑥ 关键词筛选。
	if res := apply("64", func(r *productdto.PricingRuleReq) { r.Keyword = "春季" }); res.ChangedCount != 1 {
		t.Fatalf("关键词筛选应命中 1 个商品，实际 %+v", res)
	}

	// ⑦ 跨工程隔离：别的工程里条件相同的商品不该被本工程的筛选集改到。
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
	// 用**它自己的工程**读：Get 强制工程 scope，拿本工程 id 去查会被正确拒绝
	//（ErrNotFound）—— 那本身也是一条越权防护的证据，但不是这里要断言的东西。
	otherDetail, err := f.svc.Get(ctx, &productdto.GetReq{ProjectID: other.ID, ID: otherProduct.ID})
	if err != nil {
		t.Fatalf("读他工程商品失败: %v", err)
	}
	if got := otherDetail.Variants[0].Price; got != 0 {
		t.Fatalf("跨工程商品不该被改动，实际 %v", got)
	}
}

// —— 验收 3 + 4：预览不落库，应用落库并有留痕 ——

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

	// ① 预览：算得出来、看得见逐变体明细，但一个字都没落库。
	preview, err := f.svc.PreviewPricing(ctx, &productdto.PricingPreviewReq{PricingRuleReq: req})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if preview.ChangedCount != 2 || preview.UnchangedCount != 0 || preview.SkippedCount != 0 {
		t.Fatalf("预览计数不符：%+v", preview)
	}
	if preview.RoundingLabel == "" || preview.ScopeLabel == "" {
		t.Fatalf("预览应带尾数与范围的可读标签：%+v", preview)
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

	// ② 应用：落库 + 留痕（规则 / 尾数 / 范围 / 操作人 / 备注 + 逐变体原价→新价）。
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
	if batch.RuleLabel != "成本 × 1.5" || batch.RoundingLabel == "" || batch.ScopeLabel != "单个商品的全部变体" {
		t.Fatalf("留痕批次应带规则 / 尾数 / 范围描述：%+v", batch)
	}
	if batch.Note != "成本上涨，整店上调" || batch.OperatorID != "42" {
		t.Fatalf("留痕应记录操作人与备注：%+v", batch)
	}
	if batch.ChangedCount != 2 || batch.VariantCount != 2 || batch.TargetID != p.ID {
		t.Fatalf("留痕计数与目标不符：%+v", batch)
	}

	// 明细：逐变体「原价 → 新价」。
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

	// ③ 幂等：同一条规则再应用一次没有任何改动 → 明确报错，且不再新增留痕。
	if _, err = f.svc.ApplyPricing(ctx, &productdto.PricingApplyReq{PricingRuleReq: req}); err == nil ||
		!strings.Contains(err.Error(), productenums.ErrPricingNothingChanged) {
		t.Fatalf("无改动时应返回 ErrPricingNothingChanged，实际 %v", err)
	}
	if n := pfCountAdjustments(t, f); n != 1 {
		t.Fatalf("无改动的应用不该留痕，实际 %d 条", n)
	}

	// ④ 目标不存在时不留痕、不改价。
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

// —— 验收 5：不进构建管线，构建期读的是落库后的确定值 ——

func TestPricingPersistsValueReadByBuildTime(t *testing.T) {
	f := newPricingFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	p := pfCreateProduct(t, f, "构建期读价商品", 500, 200)

	// 构建期读价：实体类型解析器（与生产构建同一份实现）读 product.price（由变体派生）。
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
	// 构建期读到的就是落库后的确定值（没有第二次计算，也没有运行时求值）。
	if got := resolvePrice(); got != "649.99" {
		t.Fatalf("改价后构建期应读到 649.99，实际 %q", got)
	}
	if got := pfPriceOf(t, f, pfGetProduct(t, f, p.ID).Variants[0].SKUCode); got != 649.99 {
		t.Fatalf("库里应存 649.99，实际 %v", got)
	}
}

func TestPricingHasNoBuildPipelineDependency(t *testing.T) {
	// 定价是「改价动作」：它只写 product_variants.price，不参与构建期计算。
	// 这条断言把「不进构建管线」钉在源码依赖上 —— 一旦有人给定价源码加了
	// builder / pipeline / artifact 的 import，测试立刻失败。
	serviceDir := filepath.Join("..", "..", "..", "..", "internal", "module", "product", "service")
	banned := []string{
		"go_wp/internal/builder",
		"go_wp/internal/pipeline",
		"go_wp/internal/artifact",
		"go_wp/internal/module/artifact",
		"go_wp/internal/module/publication",
	}
	for _, name := range []string{"product_pricing.go", "product_pricing_rule.go"} {
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

// —— 与自动标签的衔接：价格落库后按「变体写操作后」的时机重算归属 ——

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

// tagHitIDs 取某标签当前命中的商品 id。
func tagHitIDs(t *testing.T, f *pricingFixture, tagID string) []string {
	t.Helper()
	hits, err := f.svc.ListTagProducts(context.Background(), &productdto.ListTagProductsReq{TagID: tagID})
	if err != nil {
		t.Fatalf("查询标签命中商品失败: %v", err)
	}
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.ID)
	}
	return out
}

// —— 迁移：权限点与后台菜单已 seed ——

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

// —— 后台页（真实 Jet 模板 + 真实 service）——

// newPricingPageEngine 装配只挂定价页的测试引擎。
func newPricingPageEngine(t *testing.T) (*gin.Engine, *pricingFixture) {
	t.Helper()
	f := newPricingFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	handle := dashboardhttp.NewProductPageHandle(f.svc, f.projects)
	engine.GET("/admin/product-pricing", handle.ProductPricingPage)
	engine.POST("/admin/product-pricing/preview", handle.ProductPricingPreview)
	engine.POST("/admin/product-pricing/apply", handle.ProductPricingApply)
	return engine, f
}

func TestPricingAdminPages(t *testing.T) {
	engine, f := newPricingPageEngine(t)
	if engine == nil {
		return
	}
	p := pfCreateProduct(t, f, "后台改价商品", 500, 200)

	// 页面：规则参考表 + 表单 + 留痕区都要渲染出来。
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

	// 预览：200 且渲染出试算明细（不落库）。
	form := url.Values{}
	form.Set("projectId", f.projectID)
	form.Set("ruleType", productenums.PricingRuleFixedPrice)
	form.Set("amount", "66")
	form.Set("rounding", productenums.PricingRoundingEnd99)
	form.Set("scope", productenums.PricingScopeProduct)
	form.Set("targetId", p.ID)
	rec = postForm(engine, "/admin/product-pricing/preview", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("预览应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	previewBody := rec.Body.String()
	for _, want := range []string{"试算预览（未落库）", "后台改价商品", "66.99", "改价"} {
		if !strings.Contains(previewBody, want) {
			t.Fatalf("预览结果缺少 %q", want)
		}
	}
	if got := pfPriceOf(t, f, pfGetProduct(t, f, p.ID).Variants[0].SKUCode); got != 500 {
		t.Fatalf("页面试算不该改库，实际 %v", got)
	}

	// 非法参数：200 + 页内错误提示（表单值保留，不重定向）。
	bad := url.Values{}
	bad.Set("projectId", f.projectID)
	bad.Set("ruleType", productenums.PricingRuleCostMultiple)
	bad.Set("scope", productenums.PricingScopeProduct)
	bad.Set("targetId", p.ID)
	rec = postForm(engine, "/admin/product-pricing/preview", bad)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), productenums.ErrPricingRuleParamsInvalid) {
		t.Fatalf("非法参数应在页内提示，实际 %d：%s", rec.Code, rec.Body.String())
	}

	// 应用：302 回列表并带 applied=1，价格落库、留痕可见。
	form.Set("note", "后台改价")
	rec = postForm(engine, "/admin/product-pricing/apply", form)
	if rec.Code != http.StatusFound {
		t.Fatalf("应用应 302，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "applied=1") {
		t.Fatalf("应用后应带 applied=1 回跳，实际 %s", loc)
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

	// 无改动时应用：302 回列表并带 err=（不写空台账）。
	rec = postForm(engine, "/admin/product-pricing/apply", form)
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Fatalf("无改动应用应带 err 回跳，实际 %d %s", rec.Code, rec.Header().Get("Location"))
	}
}
