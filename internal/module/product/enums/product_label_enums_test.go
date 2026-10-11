package productenums_test

// product_label_enums_test.go — 「值 → 文案」映射的取值覆盖与**镜像对账**。
//
// 两条判据各管一件事：
//   · 覆盖：每个标签函数对已知取值给到哪条 key、哪句兜底，逐字钉住（改词条或改兜底会变红）；
//   · 镜像：enums 里的取值镜像（实体类型 / 评分页型）与它的**真源**逐字相等。
//     镜像存在的唯一理由是 enums 零依赖（不 import contract / scoring），
//     于是真源改了而镜像没跟上时必须有人喊 —— 否则 switch 静默走到 default，
//     五类实体全叫「商品」、分类页的缺参说成「缺少商品」，都不报错。

import (
	"testing"

	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/seo/scoring"
)

// TestEntityTypeMirrorsContract 实体类型镜像 == contract 的取值。
func TestEntityTypeMirrorsContract(t *testing.T) {
	pairs := []struct {
		mirror   string
		contract string
	}{
		{productenums.EntityTypeProduct, productcontract.EntityTypeProduct},
		{productenums.EntityTypeCategory, productcontract.EntityTypeCategory},
		{productenums.EntityTypeBrand, productcontract.EntityTypeBrand},
		{productenums.EntityTypeTag, productcontract.EntityTypeTag},
		{productenums.EntityTypeAttribute, productcontract.EntityTypeAttribute},
	}
	for _, p := range pairs {
		if p.mirror != p.contract {
			t.Errorf("实体类型镜像失配：enums %q != productcontract %q —— "+
				"enums.EntityTypeLabel 会走到 default 把这一类说成「商品」", p.mirror, p.contract)
		}
	}
}

// TestScoreKindMirrorsScoring 评分页型镜像 == internal/seo/scoring 的 PageKind 取值。
func TestScoreKindMirrorsScoring(t *testing.T) {
	pairs := []struct {
		mirror  string
		scoring string
	}{
		{productenums.ScoreKindProduct, string(scoring.KindProduct)},
		{productenums.ScoreKindCategory, string(scoring.KindCategory)},
		{productenums.ScoreKindBrand, string(scoring.KindBrand)},
	}
	for _, p := range pairs {
		if p.mirror != p.scoring {
			t.Errorf("评分页型镜像失配：enums %q != scoring %q —— "+
				"enums.ScoreMissingEntityLabel 的空态文案会张冠李戴", p.mirror, p.scoring)
		}
	}
}

// TestStatusLabel 商品状态：三档 + 筛选下拉的空档，未知值回落原值。
func TestStatusLabel(t *testing.T) {
	cases := []struct {
		value    string
		key      string
		fallback string
	}{
		{productenums.StatusDraft, productenums.ProductsStatusDraft, "草稿"},
		{productenums.StatusPublished, productenums.ProductsStatusPublished, "已上架"},
		{productenums.StatusArchived, productenums.ProductsStatusArchived, "已下架"},
		{"", productenums.ProductsStatusAll, "全部状态"},
		{"plan2", "plan2", "plan2"},
	}
	for _, tc := range cases {
		key, fallback := productenums.StatusLabel(tc.value)
		if key != tc.key || fallback != tc.fallback {
			t.Errorf("StatusLabel(%q) = (%q, %q)，期望 (%q, %q)",
				tc.value, key, fallback, tc.key, tc.fallback)
		}
	}
}

// TestTagKindLabel 标签类型：未知值回落原值（不再一律说成「手工」）。
func TestTagKindLabel(t *testing.T) {
	cases := []struct {
		value    string
		key      string
		fallback string
	}{
		{productenums.TagKindRule, productenums.ProductTagsKindRule, "自动"},
		{productenums.TagKindManual, productenums.ProductTagsKindManual, "手工"},
		{"auto2", "auto2", "auto2"},
	}
	for _, tc := range cases {
		key, fallback := productenums.TagKindLabel(tc.value)
		if key != tc.key || fallback != tc.fallback {
			t.Errorf("TagKindLabel(%q) = (%q, %q)，期望 (%q, %q)",
				tc.value, key, fallback, tc.key, tc.fallback)
		}
	}
}

// TestTagRecalcLabel 只有「从没重算过」那一档有文案；有时间时展示的是时间本身。
func TestTagRecalcLabel(t *testing.T) {
	if key, fallback := productenums.TagRecalcLabel("  "); key != productenums.ProductTagsRecalcNone || fallback != "未按规则重算过" {
		t.Errorf("TagRecalcLabel(空) = (%q, %q)，期望「未按规则重算过」那一条", key, fallback)
	}
	if key, fallback := productenums.TagRecalcLabel("2026-10-11T07:00:00+08:00"); key != "" || fallback != "" {
		t.Errorf("TagRecalcLabel(有时间) = (%q, %q)，期望空事实（展示的是时间本身）", key, fallback)
	}
}

// TestBundleSourceLabel 成员来源：三种来源 + 空 / 未知 = 手工指定，兜底与库内词条同句。
func TestBundleSourceLabel(t *testing.T) {
	cases := []struct {
		value    string
		key      string
		fallback string
	}{
		{productenums.BundleSourceProduct, productenums.ProductBundleSourceProduct, "从商品导入"},
		{productenums.BundleSourceWarehouse, productenums.ProductBundleSourceWarehouse, "从仓库选"},
		{productenums.BundleSourceAttributes, productenums.ProductBundleSourceAttributes, "自选属性组合"},
		{"", productenums.ProductBundleSourceNone, "手工指定"},
		{"legacy", productenums.ProductBundleSourceNone, "手工指定"},
	}
	for _, tc := range cases {
		key, fallback := productenums.BundleSourceLabel(tc.value)
		if key != tc.key || fallback != tc.fallback {
			t.Errorf("BundleSourceLabel(%q) = (%q, %q)，期望 (%q, %q)",
				tc.value, key, fallback, tc.key, tc.fallback)
		}
	}
}

// TestEntityTypeLabel 五类实体各一条词条，未知类型回落到「商品」。
func TestEntityTypeLabel(t *testing.T) {
	cases := []struct {
		value    string
		key      string
		fallback string
	}{
		{productenums.EntityTypeCategory, productenums.ProductTranslationsEntityTypeCategory, "商品分类"},
		{productenums.EntityTypeBrand, productenums.ProductTranslationsEntityTypeBrand, "商品品牌"},
		{productenums.EntityTypeTag, productenums.ProductTranslationsEntityTypeTag, "商品标签"},
		{productenums.EntityTypeAttribute, productenums.ProductTranslationsEntityTypeAttribute, "商品属性"},
		{productenums.EntityTypeProduct, productenums.ProductTranslationsEntityTypeProduct, "商品"},
		{"product_unknown", productenums.ProductTranslationsEntityTypeProduct, "商品"},
	}
	for _, tc := range cases {
		key, fallback := productenums.EntityTypeLabel(tc.value)
		if key != tc.key || fallback != tc.fallback {
			t.Errorf("EntityTypeLabel(%q) = (%q, %q)，期望 (%q, %q)",
				tc.value, key, fallback, tc.key, tc.fallback)
		}
	}
}

// TestScoreMissingEntityLabel 三个页型各一条空态，未知页型回落到「缺少商品」。
func TestScoreMissingEntityLabel(t *testing.T) {
	cases := []struct {
		kind     string
		key      string
		fallback string
	}{
		{productenums.ScoreKindCategory, productenums.SEOEmptyMissingCategory, "缺少分类，无法评分"},
		{productenums.ScoreKindBrand, productenums.SEOEmptyMissingBrand, "缺少品牌，无法评分"},
		{productenums.ScoreKindProduct, productenums.SEOEmptyMissingProduct, "缺少商品，无法评分"},
		{"landing", productenums.SEOEmptyMissingProduct, "缺少商品，无法评分"},
	}
	for _, tc := range cases {
		key, fallback := productenums.ScoreMissingEntityLabel(tc.kind)
		if key != tc.key || fallback != tc.fallback {
			t.Errorf("ScoreMissingEntityLabel(%q) = (%q, %q)，期望 (%q, %q)",
				tc.kind, key, fallback, tc.key, tc.fallback)
		}
	}
}

// TestBulkDeleteOutcome 批量删除的三条分支各一条判据：计数 → 哪一句 + 哪几个参数。
//
// 为什么必须有这一条：三个分支的**兜底原文**都在 enums 里，控制器只剩一次转换 ——
// 没有这条测试时，改错一条句子（比如把「跳过若干」与「全部成功」的 key 调换）不会有任何
// 编译错误，页面上也照样显示一句话，只是说的是另一件事。
func TestBulkDeleteOutcome(t *testing.T) {
	partial := productenums.Outcome{Key: "admin.tag.bulk.partial", Fallback: "已删除 %s 个，%s 个未能删除。"}
	done := productenums.Outcome{Key: "admin.tag.bulk.done", Fallback: "已删除 %s 个。"}
	cases := []struct {
		name             string
		deleted, skipped int
		ok               bool
		key              string
		args             []string
	}{
		{"跳过若干（失败态，两个计数都要说）", 3, 2, false, partial.Key, []string{"3", "2"}},
		{"全部成功（成功态，只报已删数）", 3, 0, true, done.Key, []string{"3"}},
		{"一个都没删也没跳过（不是成功）", 0, 0, false, productenums.ProductsBulkNoneSelected, nil},
	}
	for _, tc := range cases {
		ok, out := productenums.BulkDeleteOutcome(tc.deleted, tc.skipped, partial, done)
		if ok != tc.ok || out.Key != tc.key || !sameArgs(out.Args, tc.args) {
			t.Errorf("%s：BulkDeleteOutcome(%d, %d) = (ok=%v, key=%q, args=%v)，期望 (ok=%v, key=%q, args=%v)",
				tc.name, tc.deleted, tc.skipped, ok, out.Key, out.Args, tc.ok, tc.key, tc.args)
		}
		if out.Fallback == "" {
			t.Errorf("%s：兜底为空 —— 词条缺失时页面上会是一句空话", tc.name)
		}
	}
}

// TestPricingBulkOutcome 批量改价的四条分支各一条判据（含「跳过原因」这个数据参数）。
func TestPricingBulkOutcome(t *testing.T) {
	cases := []struct {
		name                        string
		changed, unchanged, skipped int
		reason                      string
		key                         string
		args                        []string
	}{
		{"一个都没改（都在目标价上）", 0, 2, 0, "无", productenums.BulkPricingNoChange, []string{"2"}},
		{"改到价了且没有跳过", 5, 2, 0, "无", productenums.BulkPricingApplied, []string{"5", "2"}},
		{"一个变体都没改，全被跳过", 0, 0, 3, "没有可改价的变体", productenums.BulkPricingAllSkip, []string{"3", "没有可改价的变体"}},
		{"改了一部分，其余被跳过", 5, 0, 3, "没有可改价的变体", productenums.BulkPricingPartial, []string{"5", "3", "没有可改价的变体"}},
	}
	for _, tc := range cases {
		out := productenums.PricingBulkOutcome(tc.changed, tc.unchanged, tc.skipped, tc.reason)
		if out.Key != tc.key || !sameArgs(out.Args, tc.args) {
			t.Errorf("%s：PricingBulkOutcome(%d, %d, %d, %q) = (key=%q, args=%v)，期望 (key=%q, args=%v)",
				tc.name, tc.changed, tc.unchanged, tc.skipped, tc.reason, out.Key, out.Args, tc.key, tc.args)
		}
	}
}

// sameArgs 逐字比较 `%s` 参数（nil 与空切片等价）。
func sameArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
