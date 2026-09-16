// Package feature product 模块 feature 测试 —— 标签与自动规则（issue #11）。
//
// 覆盖本票四条验收（真实 PostgreSQL + 生产 DDL + 真实 service）：
//  1. 可建标签并手工挂到商品（引用校验：同工程 + 必须存在 + 必须手工标签）；
//  2. 自动标签只接受内置规则类型与白名单参数，非法规则被拒绝（自由表达式一律不受理）；
//  3. 自动标签按定义的重算时机更新商品归属（商品写、变体写、标签定义变更、显式重算），
//     且重算只动自己那一个 tag id —— 不覆盖手工标签；
//  4. 后台可查看某标签命中哪些商品（页面 + 接口）。
//
// 另覆盖两条容易踩的边界：
//
//	· 「新品」以 products.published_at 判定，不是 create_time（建了草稿很久才上架的商品不算新品）；
//	· 变体维度的规则（价格区间 / 促销）在变体表上没有工程列，命中集合必须按工程兜底过滤。
package feature

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"

	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	"go_wp/internal/templates"

	"go_wp/public/migrations"
)

// TestTagManualAttachAndGuards 验收 1：手工标签可建、可手工挂到商品，引用面有明确拦截。
func TestTagManualAttachAndGuards(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	other, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "另一个工程"})
	if err != nil {
		t.Fatalf("建第二工程失败: %v", err)
	}

	manual, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{
		ProjectID: f.projectID, Name: "清仓", Slug: "clearance", Sort: 2,
	})
	if err != nil {
		t.Fatalf("建手工标签失败: %v", err)
	}
	// 类型留空默认手工（最保守：不会因为一条规则自动改归属）。
	if manual.Kind != productenums.TagKindManual || manual.RuleType != "" {
		t.Fatalf("缺省类型应为手工标签，实际 kind=%q ruleType=%q", manual.Kind, manual.RuleType)
	}
	if _, err = f.svc.CreateTag(ctx, &productdto.CreateTagReq{
		ProjectID: f.projectID, Name: "清仓副本", Slug: "clearance",
	}); err == nil || err.Error() != productenums.ErrTagSlugTaken {
		t.Fatalf("slug 重复应返回 ErrTagSlugTaken，实际 %v", err)
	}

	// 建商品时直接挂手工标签。
	p, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "清仓商品", TagIDs: []string{manual.ID},
	})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	if len(p.TagIDs) != 1 || p.TagIDs[0] != manual.ID {
		t.Fatalf("商品应挂上手工标签，实际 %v", p.TagIDs)
	}
	// 验收 4：按标签反查命中商品。
	// DB-009：库里有多个工程时按 id 单查必须显式给工程（唯一工程兜底不成立）。
	detail, err := f.svc.GetTag(ctx, &productdto.GetTagReq{ProjectID: f.projectID, ID: manual.ID})
	if err != nil {
		t.Fatalf("标签详情失败: %v", err)
	}
	if detail.ProductCount != 1 || len(detail.Products) != 1 || detail.Products[0].ID != p.ID {
		t.Fatalf("标签应命中 1 个商品，实际 %+v", detail.Products)
	}
	// 验收 4 的接口形态：ListTagProducts。
	hits, err := f.svc.ListTagProducts(ctx, &productdto.ListTagProductsReq{ProjectID: f.projectID, TagID: manual.ID})
	if err != nil || len(hits) != 1 || hits[0].Name != "清仓商品" {
		t.Fatalf("ListTagProducts 应返回命中商品，实际 %v %+v", err, hits)
	}

	// 手工解绑（整体替换语义）。
	if _, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, TagIDs: []string{}}); err != nil {
		t.Fatalf("解绑标签失败: %v", err)
	}
	got, err := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
	if err != nil || len(got.TagIDs) != 0 {
		t.Fatalf("解绑后不应带标签，实际 %v", got.TagIDs)
	}

	// 三条引用拦截：不存在 / 跨工程 / 自动标签。
	missing := "00000000-0000-0000-0000-000000000000"
	if _, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, TagIDs: []string{missing}}); err == nil ||
		err.Error() != productenums.ErrTagNotFound {
		t.Fatalf("不存在的标签应返回 ErrTagNotFound，实际 %v", err)
	}
	foreign, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{ProjectID: other.ID, Name: "别家标签", Slug: "foreign-tag"})
	if err != nil {
		t.Fatalf("建他工程标签失败: %v", err)
	}
	if _, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, TagIDs: []string{foreign.ID}}); err == nil ||
		err.Error() != productenums.ErrTagProjectMismatch {
		t.Fatalf("跨工程标签应返回 ErrTagProjectMismatch，实际 %v", err)
	}
	rule, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{
		ProjectID: f.projectID, Name: "新品", Kind: productenums.TagKindRule,
		RuleType: productenums.TagRuleNewArrival, RuleParams: json.RawMessage(`{"days":30}`),
	})
	if err != nil {
		t.Fatalf("建自动标签失败: %v", err)
	}
	if _, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, TagIDs: []string{rule.ID}}); err == nil ||
		err.Error() != productenums.ErrTagNotManual {
		t.Fatalf("手工挂自动标签应返回 ErrTagNotManual，实际 %v", err)
	}
}

// TestTagRuleValidation 验收 2：只接受内置规则类型与白名单参数，非法规则被拒绝。
func TestTagRuleValidation(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	create := func(name, kind, ruleType string, params string) (*productdto.TagResp, error) {
		req := &productdto.CreateTagReq{ProjectID: f.projectID, Name: name, Kind: kind, RuleType: ruleType}
		if params != "" {
			req.RuleParams = json.RawMessage(params)
		}
		return f.svc.CreateTag(ctx, req)
	}

	// 合法：三个内置规则类型。
	if _, err := create("新品30", productenums.TagKindRule, productenums.TagRuleNewArrival, `{"days":30}`); err != nil {
		t.Fatalf("新品规则应被接受: %v", err)
	}
	both, err := create("价格100-200", productenums.TagKindRule, productenums.TagRulePriceRange, `{"minPrice":100,"maxPrice":200}`)
	if err != nil {
		t.Fatalf("价格区间规则应被接受: %v", err)
	}
	if string(both.RuleParams) != `{"minPrice":100,"maxPrice":200}` {
		t.Fatalf("规则参数应归一为落库形态，实际 %s", both.RuleParams)
	}
	if _, err = create("价格下限", productenums.TagKindRule, productenums.TagRulePriceRange, `{"minPrice":10}`); err != nil {
		t.Fatalf("只给下限应被接受: %v", err)
	}
	if _, err = create("促销", productenums.TagKindRule, productenums.TagRuleOnSale, ""); err != nil {
		t.Fatalf("无参数规则应被接受: %v", err)
	}

	// 非法规则类型 / 形状。
	cases := []struct {
		name     string
		kind     string
		ruleType string
		params   string
		want     string
	}{
		{"未知规则类型", productenums.TagKindRule, "free_sql", `{}`, productenums.ErrTagRuleTypeInvalid},
		{"自动标签缺规则类型", productenums.TagKindRule, "", "", productenums.ErrTagRuleTypeInvalid},
		{"手工标签带规则类型", productenums.TagKindManual, productenums.TagRuleNewArrival, "", productenums.ErrTagRuleNotAllowed},
		{"手工标签带规则参数", productenums.TagKindManual, "", `{"days":30}`, productenums.ErrTagRuleNotAllowed},
		{"未知标签类型", "smart", "", "", productenums.ErrTagKindInvalid},
		{"新品缺 days", productenums.TagKindRule, productenums.TagRuleNewArrival, `{}`, productenums.ErrTagRuleParamsInvalid},
		{"新品 days 越界", productenums.TagKindRule, productenums.TagRuleNewArrival, `{"days":999}`, productenums.ErrTagRuleParamsInvalid},
		{"新品 days 为 0", productenums.TagKindRule, productenums.TagRuleNewArrival, `{"days":0}`, productenums.ErrTagRuleParamsInvalid},
		{"新品 days 是字符串", productenums.TagKindRule, productenums.TagRuleNewArrival, `{"days":"30"}`, productenums.ErrTagRuleParamsInvalid},
		{"参数带未知键", productenums.TagKindRule, productenums.TagRuleNewArrival, `{"days":30,"expr":"price>1"}`, productenums.ErrTagRuleParamsInvalid},
		{"参数不是对象", productenums.TagKindRule, productenums.TagRuleNewArrival, `[30]`, productenums.ErrTagRuleParamsInvalid},
		{"价格区间两个都没给", productenums.TagKindRule, productenums.TagRulePriceRange, `{}`, productenums.ErrTagRuleParamsInvalid},
		{"价格区间下限大于上限", productenums.TagKindRule, productenums.TagRulePriceRange, `{"minPrice":200,"maxPrice":100}`, productenums.ErrTagRuleParamsInvalid},
		{"价格区间负数", productenums.TagKindRule, productenums.TagRulePriceRange, `{"minPrice":-1}`, productenums.ErrTagRuleParamsInvalid},
		{"价格区间未知键", productenums.TagKindRule, productenums.TagRulePriceRange, `{"price":10}`, productenums.ErrTagRuleParamsInvalid},
		{"无参数规则给了参数", productenums.TagKindRule, productenums.TagRuleOnSale, `{"days":1}`, productenums.ErrTagRuleParamsInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := create("非法-"+tc.name, tc.kind, tc.ruleType, tc.params)
			if err == nil {
				t.Fatalf("非法规格应被拒绝，实际通过")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("应返回 %s，实际 %v", tc.want, err)
			}
		})
	}

	// 更新路径同样严格：换规则类型 + 给对应参数；改参数为非法值被拒且不落库。
	upd := `{"days":7}`
	kind := productenums.TagRuleNewArrival
	updated, err := f.svc.UpdateTag(ctx, &productdto.UpdateTagReq{
		ID: both.ID, RuleType: &kind, RuleParams: json.RawMessage(upd),
	})
	if err != nil {
		t.Fatalf("改规则参数失败: %v", err)
	}
	if string(updated.RuleParams) != `{"days":7}` || updated.RuleType != productenums.TagRuleNewArrival {
		t.Fatalf("改规则参数未生效: %+v", updated)
	}
	bad := `{"days":0}`
	if _, err = f.svc.UpdateTag(ctx, &productdto.UpdateTagReq{ProjectID: f.projectID, ID: both.ID, RuleParams: json.RawMessage(bad)}); err == nil ||
		!strings.Contains(err.Error(), productenums.ErrTagRuleParamsInvalid) {
		t.Fatalf("非法参数应被拒绝，实际 %v", err)
	}
	again, err := f.svc.GetTag(ctx, &productdto.GetTagReq{ProjectID: f.projectID, ID: both.ID})
	if err != nil {
		t.Fatalf("读标签失败: %v", err)
	}
	// jsonb 回读会规范化键序与空格，比较语义而不是字节。
	if !jsonEqual(string(again.RuleParams), `{"days":7}`) {
		t.Fatalf("被拒的更新不应改库，实际 %s", again.RuleParams)
	}
	// 规则类型清单来自注册表（后台下拉的唯一来源）。
	types := f.svc.ListTagRuleTypes(ctx)
	if len(types) != 3 {
		t.Fatalf("应有 3 个内置规则类型，实际 %d", len(types))
	}
}

// TestTagAutoRecalcMembership 验收 3：自动标签按定义的重算时机更新归属，
// 且只动自己那一个 tag id（不覆盖手工标签）。
func TestTagAutoRecalcMembership(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	newArrival, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{
		ProjectID: f.projectID, Name: "新品", Kind: productenums.TagKindRule,
		RuleType: productenums.TagRuleNewArrival, RuleParams: json.RawMessage(`{"days":30}`),
	})
	if err != nil {
		t.Fatalf("建新品规则失败: %v", err)
	}
	priceRange, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{
		ProjectID: f.projectID, Name: "百元档", Kind: productenums.TagKindRule,
		RuleType: productenums.TagRulePriceRange, RuleParams: json.RawMessage(`{"minPrice":100,"maxPrice":200}`),
	})
	if err != nil {
		t.Fatalf("建价格区间规则失败: %v", err)
	}
	onSale, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{
		ProjectID: f.projectID, Name: "促销", Kind: productenums.TagKindRule,
		RuleType: productenums.TagRuleOnSale,
	})
	if err != nil {
		t.Fatalf("建促销规则失败: %v", err)
	}
	manual, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{ProjectID: f.projectID, Name: "手工", Slug: "manual-tag"})
	if err != nil {
		t.Fatalf("建手工标签失败: %v", err)
	}

	// ① 商品写操作后重算：草稿商品不算「已上架」，新品规则不命中。
	price := 150.0
	p, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "百元商品", DefaultPrice: &price, TagIDs: []string{manual.ID},
	})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	// 价格区间规则在商品创建时就该命中（变体已随商品建出）。
	hits := tagProductIDs(t, f, priceRange.ID)
	if !containsString(hits, p.ID) {
		t.Fatalf("价格区间规则应在商品创建时命中，实际 %v", hits)
	}
	if containsString(tagProductIDs(t, f, newArrival.ID), p.ID) {
		t.Fatalf("草稿商品不应命中新品规则")
	}

	// ② 状态转 published → 上架时间落库、新品规则命中。
	published := productenums.StatusPublished
	if _, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, Status: &published}); err != nil {
		t.Fatalf("上架失败: %v", err)
	}
	got, err := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if got.PublishedAt == "" {
		t.Fatalf("上架后应记录 published_at")
	}
	if !containsString(tagProductIDs(t, f, newArrival.ID), p.ID) {
		t.Fatalf("上架后应命中新品规则")
	}
	// 手工标签没有被重算抹掉（重算只替换自己那一个 tag id）。
	if !containsString(got.TagIDs, manual.ID) {
		t.Fatalf("重算不应覆盖手工标签，实际 %v", got.TagIDs)
	}
	if !containsString(got.TagIDs, newArrival.ID) || !containsString(got.TagIDs, priceRange.ID) {
		t.Fatalf("自动标签应同时挂在商品上，实际 %v", got.TagIDs)
	}

	// ③ 「新品」以 published_at 判定，不是 create_time：把上架时间改成 40 天前后再重算，应脱钩。
	old := time.Now().UTC().AddDate(0, 0, -40)
	if err = f.db.Exec("UPDATE products SET published_at = ? WHERE id = ?", old, p.ID).Error; err != nil {
		t.Fatalf("回填上架时间失败: %v", err)
	}
	if _, err = f.svc.RecalcTags(ctx, &productdto.RecalcTagsReq{ProjectID: f.projectID, TagID: newArrival.ID}); err != nil {
		t.Fatalf("重算新品标签失败: %v", err)
	}
	if containsString(tagProductIDs(t, f, newArrival.ID), p.ID) {
		t.Fatalf("上架超过 30 天的商品应脱离新品标签")
	}

	// ④ 变体写操作后重算：单个变体新增（带划线价）让促销规则命中。
	sale := 90.0
	compare := 120.0
	if _, err = f.svc.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProductID: p.ID, Price: &sale, ComparePrice: &compare,
	}); err != nil {
		t.Fatalf("建变体失败: %v", err)
	}
	if !containsString(tagProductIDs(t, f, onSale.ID), p.ID) {
		t.Fatalf("有划线价的商品应命中促销规则")
	}
	// 改回价格区间外 → 价格区间标签脱钩（变体写操作触发的重算）。
	out := 999.0
	variants, err := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
	if err != nil || len(variants.Variants) == 0 {
		t.Fatalf("读变体失败: %v", err)
	}
	for _, v := range variants.Variants {
		if !v.Enabled {
			continue
		}
		if _, err = f.svc.UpdateVariant(ctx, &productdto.UpdateVariantReq{ID: v.ID, Price: &out}); err != nil {
			t.Fatalf("改变体价格失败: %v", err)
		}
	}
	if containsString(tagProductIDs(t, f, priceRange.ID), p.ID) {
		t.Fatalf("价格全部移出区间后应脱离价格区间标签")
	}

	// ⑤ 标签规则变更后立刻重算：把价格区间放宽到 1000，商品重新命中。
	wide := `{"minPrice":100,"maxPrice":1000}`
	if _, err = f.svc.UpdateTag(ctx, &productdto.UpdateTagReq{ProjectID: f.projectID, ID: priceRange.ID, RuleParams: json.RawMessage(wide)}); err != nil {
		t.Fatalf("放宽价格区间失败: %v", err)
	}
	if !containsString(tagProductIDs(t, f, priceRange.ID), p.ID) {
		t.Fatalf("规则放宽后应重新命中")
	}

	// ⑥ 显式重算：整工程重算只统计自动标签，手工标签不参与。
	res, err := f.svc.RecalcTags(ctx, &productdto.RecalcTagsReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("整工程重算失败: %v", err)
	}
	if res.Recalculated != 3 {
		t.Fatalf("整工程应重算 3 个自动标签，实际 %d", res.Recalculated)
	}
	if _, err = f.svc.RecalcTags(ctx, &productdto.RecalcTagsReq{TagID: manual.ID}); err != nil {
		t.Fatalf("重算手工标签应安全无操作: %v", err)
	}
	manualDetail, err := f.svc.GetTag(ctx, &productdto.GetTagReq{ProjectID: f.projectID, ID: manual.ID})
	if err != nil {
		t.Fatalf("读手工标签失败: %v", err)
	}
	if manualDetail.ProductCount != 1 {
		t.Fatalf("手工标签归属不应被重算改动，实际命中 %d", manualDetail.ProductCount)
	}
	// 重算时间已记录（后台可见的重算证据）。
	autoDetail, err := f.svc.GetTag(ctx, &productdto.GetTagReq{ProjectID: f.projectID, ID: newArrival.ID})
	if err != nil {
		t.Fatalf("读自动标签失败: %v", err)
	}
	if autoDetail.RecalcAt == "" || autoDetail.RuleLabel != "上架 30 天内" {
		t.Fatalf("自动标签应带重算时间与规则描述，实际 %+v", autoDetail)
	}

	// ⑦ 跨工程隔离：别的工程里同价格的商品不该被本工程标签命中。
	other, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "另一个工程"})
	if err != nil {
		t.Fatalf("建第二工程失败: %v", err)
	}
	otherPrice := 150.0
	otherProduct, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: other.ID, Name: "别家百元商品", DefaultPrice: &otherPrice,
	})
	if err != nil {
		t.Fatalf("建他工程商品失败: %v", err)
	}
	if containsString(tagProductIDs(t, f, priceRange.ID), otherProduct.ID) {
		t.Fatalf("跨工程商品不应被本工程标签命中")
	}
}

// TestTagDeleteUnbindsProducts 删除标签要连同商品上的引用一起解绑（不留悬空引用）。
func TestTagDeleteUnbindsProducts(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	tag, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{ProjectID: f.projectID, Name: "待删", Slug: "to-delete"})
	if err != nil {
		t.Fatalf("建标签失败: %v", err)
	}
	p, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "挂标签商品", TagIDs: []string{tag.ID},
	})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	if err = f.svc.DeleteTag(ctx, &productdto.DeleteTagReq{ProjectID: f.projectID, ID: tag.ID}); err != nil {
		t.Fatalf("删除标签失败: %v", err)
	}
	got, err := f.svc.Get(ctx, &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if len(got.TagIDs) != 0 {
		t.Fatalf("删除标签后商品不应再有悬空引用，实际 %v", got.TagIDs)
	}
	if _, err = f.svc.GetTag(ctx, &productdto.GetTagReq{ProjectID: f.projectID, ID: tag.ID}); err == nil ||
		err.Error() != productenums.ErrTagNotFound {
		t.Fatalf("删除后应返回 ErrTagNotFound，实际 %v", err)
	}
}

// TestTagPermissionsAndMenusSeeded 迁移 092/093：权限点与后台菜单已 seed
// （未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403）。
func TestTagPermissionsAndMenusSeeded(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	if err := migrations.RunSeeds(f.db); err != nil {
		t.Fatalf("执行数据种子失败: %v", err)
	}
	var n int64
	if err := f.db.Raw(`SELECT COUNT(*) FROM sys_permission
		WHERE module = 'product' AND permission_code LIKE 'product:tag_%'`).Scan(&n).Error; err != nil {
		t.Fatalf("查询权限点失败: %v", err)
	}
	if n != 8 {
		t.Fatalf("迁移 092 应 seed 8 个标签权限点，实际 %d", n)
	}
	if err := f.db.Raw(`SELECT COUNT(*) FROM sys_menus
		WHERE type = 2 AND title = '商品标签' AND deleted_at IS NULL`).Scan(&n).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("迁移 093 应 seed 商品标签后台菜单，实际 %d", n)
	}
}

// newTagPageEngine 装配只挂标签页 / 商品页的测试引擎（真实 Jet 模板 + 真实 service）。
func newTagPageEngine(t *testing.T) (*gin.Engine, *attrFixture) {
	t.Helper()
	f := newAttrFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	handle := dashboardhttp.NewProductPageHandle(f.svc, f.projects)
	engine.GET("/admin/product-tags", handle.ProductTagsPage)
	engine.POST("/admin/product-tags/create", handle.ProductTagsCreate)
	engine.POST("/admin/product-tags/update", handle.ProductTagsUpdate)
	engine.POST("/admin/product-tags/delete", handle.ProductTagsDelete)
	engine.POST("/admin/product-tags/recalc", handle.ProductTagsRecalc)
	engine.GET("/admin/products", handle.ProductsPage)
	engine.POST("/admin/products/tags", handle.ProductsTagsSet)
	return engine, f
}

// TestTagAdminPages 验收 4：后台可看标签与命中商品，页面写链路可用（真实模板渲染）。
func TestTagAdminPages(t *testing.T) {
	engine, f := newTagPageEngine(t)
	if engine == nil {
		return
	}
	// 建手工标签 + 自动标签（表单只给本规则用得到的参数）。
	rec := postForm(engine, "/admin/product-tags/create", url.Values{
		"projectId": {f.projectID}, "name": {"清仓"}, "slug": {"clearance"}, "kind": {"manual"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("POST 建手工标签应 302，实际 %d", rec.Code)
	}
	rec = postForm(engine, "/admin/product-tags/create", url.Values{
		"projectId": {f.projectID}, "name": {"新品"}, "slug": {"new-arrival"},
		"kind": {"rule"}, "ruleType": {productenums.TagRuleNewArrival}, "days": {"30"},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("POST 建自动标签应 302，实际 %d", rec.Code)
	}
	tags, err := f.svc.ListTags(context.Background(), &productdto.ListTagReq{ProjectID: f.projectID})
	if err != nil || len(tags) != 2 {
		t.Fatalf("应建成 2 个标签，实际 %v %+v", err, tags)
	}

	// 非法规则参数经表单提交 → 302 带 err 回显，且不落库。
	rec = postForm(engine, "/admin/product-tags/create", url.Values{
		"projectId": {f.projectID}, "name": {"错误规则"}, "kind": {"rule"},
		"ruleType": {productenums.TagRuleNewArrival}, "days": {"999"},
	})
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "err=") {
		t.Fatalf("越界参数应带 err 回跳，实际 %d %s", rec.Code, rec.Header().Get("Location"))
	}
	tags, _ = f.svc.ListTags(context.Background(), &productdto.ListTagReq{ProjectID: f.projectID})
	if len(tags) != 2 {
		t.Fatalf("被拒的标签不应落库，实际 %d 个", len(tags))
	}

	// 建一个上架商品让新品规则命中，再用表单给它挂手工标签。
	p, err := f.svc.Create(context.Background(), &productdto.CreateReq{ProjectID: f.projectID, Name: "上架商品"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	published := productenums.StatusPublished
	if _, err = f.svc.Update(context.Background(), &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, Status: &published}); err != nil {
		t.Fatalf("上架失败: %v", err)
	}
	manualID := tagIDByName(t, f, "清仓")
	rec = postForm(engine, "/admin/products/tags", url.Values{
		"projectId": {f.projectID}, "id": {p.ID}, "tagIds": {manualID},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("POST 商品挂标签应 302，实际 %d", rec.Code)
	}

	// 标签页：标签名、类型、规则描述、重算时间、命中商品都要渲染出来。
	rec = httptestGet(engine, "/admin/product-tags?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("标签页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"商品标签", "清仓", "新品", "手工", "自动", "上架 30 天内",
		"上架商品", "命中的商品", "内置规则类型", "立即重算这个标签", "删除标签",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("标签页缺少 %q", want)
		}
	}
	// 商品页：手工标签勾选框 + 自动标签只读展示。
	rec = httptestGet(engine, "/admin/products?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("商品页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	pageBody := rec.Body.String()
	for _, want := range []string{"商品标签", `name="tagIds"`, "保存手工标签", "自动标签（按规则重算维护，不能手工改动）"} {
		if !strings.Contains(pageBody, want) {
			t.Fatalf("商品页缺少 %q", want)
		}
	}

	// 页面「重算」按钮：显式重算时机。
	rec = postForm(engine, "/admin/product-tags/recalc", url.Values{
		"projectId": {f.projectID}, "tagId": {tagIDByName(t, f, "新品")},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("POST 重算应 302，实际 %d", rec.Code)
	}

	// 删除标签：302 且连同引用一起解绑。
	rec = postForm(engine, "/admin/product-tags/delete", url.Values{
		"projectId": {f.projectID}, "id": {manualID},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("POST 删除标签应 302，实际 %d", rec.Code)
	}
	if _, err = f.svc.GetTag(context.Background(), &productdto.GetTagReq{ProjectID: f.projectID, ID: manualID}); err == nil {
		t.Fatalf("删除后标签应不存在")
	}
}

// tagProductIDs 取某标签当前命中的商品 id（测试内的小工具）。
func tagProductIDs(t *testing.T, f *attrFixture, tagID string) []string {
	t.Helper()
	hits, err := f.svc.ListTagProducts(t.Context(), &productdto.ListTagProductsReq{ProjectID: f.projectID, TagID: tagID})
	if err != nil {
		t.Fatalf("查询标签命中商品失败: %v", err)
	}
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.ID)
	}
	return out
}

// tagIDByName 按名称取标签 id（测试内的小工具）。
func tagIDByName(t *testing.T, f *attrFixture, name string) string {
	t.Helper()
	tags, err := f.svc.ListTags(t.Context(), &productdto.ListTagReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("标签列表失败: %v", err)
	}
	for _, tag := range tags {
		if tag.Name == name {
			return tag.ID
		}
	}
	t.Fatalf("找不到标签 %q", name)
	return ""
}

// jsonEqual 比较两段 JSON 是否语义相同（jsonb 回读会规范化空格与键序）。
func jsonEqual(a, b string) bool {
	var av, bv any
	if err := json.Unmarshal([]byte(a), &av); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(b), &bv); err != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

// containsString id 是否在切片里。
func containsString(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
