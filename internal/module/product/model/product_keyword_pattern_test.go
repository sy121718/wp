package productmodel_test

// product_keyword_pattern_test.go — 后台关键词搜索的**口径构造点**判据（PRD-005）。
//
// 背景：同一个后台里，商品/属性/品牌/标签/分类列表与定价筛选走的是「两端加 %、不转义」，
// 而集合源（collectionQuery）与分类搜索森林早已用 database.EscapeLikePattern + ESCAPE
// 按字面量匹配。两种口径并存本身是待定事项（任务书 DEC-05：百分号/下划线/反斜杠的
// 搜索语义），所以本用例**不改匹配结果**，只钉住两件事：
//
//  1. 「不转义」这一档现在只有**一个**构造点（productKeywordPattern）。各入口用同一个
//     keyword 绑出来的 pattern 必须逐字相同 —— 少一处漏改，将来统一口径时就会留下
//     一条行为不同的入口，而那种差异只表现为「搜 50% 时这个页面命中、那个页面不命中」。
//  2. 「已转义」那一档仍然是字面量语义：% 与 _ 必须带着反斜杠进绑定值。
//
// DEC-05 定下来之后，本用例与 productKeywordPattern 一起改；在那之前它是防分叉的锁，
// 不是「现有行为已被批准」的断言。
//
// 表结构来自生产迁移（support.NewMigratedPGTestDB）；不灌业务数据也行 ——
// 判据是生成的 SQL 与其绑定值，不是命中行数。

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/public/test/support"
)

// keywordProbe 关键词里同时带 % 与 _：这是两种口径唯一会分开的输入。
const keywordProbe = "50%_折"

// keywordMarker 用来从绑定参数里认出「哪一个是关键词 pattern」的稳定子串。
//
// 不能直接找 keywordProbe 原文：已转义那一档绑进去的是 "50\%\_折"，
// 原文已经不在绑定值里，按原文筛会把「确实绑了」误判成「一个都没抓到手」。
// 这个含中文的子串在两种口径下都原样保留。
const keywordMarker = "折"

// captureKeywordVars 跑一次查询并返回它绑定参数里含关键词标记的那些字符串（按出现顺序）。
func captureKeywordVars(t *testing.T, db *gorm.DB, run func() error) []string {
	t.Helper()
	var vars []any
	const hook = "keyword:capture"
	if err := db.Callback().Query().After("gorm:query").Register(hook, func(tx *gorm.DB) {
		vars = append([]any(nil), tx.Statement.Vars...)
	}); err != nil {
		t.Fatalf("注册 SQL 抓取回调失败：%v", err)
	}
	defer func() { _ = db.Callback().Query().Remove(hook) }()
	if err := run(); err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	out := make([]string, 0, len(vars))
	for _, v := range vars {
		if s, ok := v.(string); ok && strings.Contains(s, keywordMarker) {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		t.Fatalf("没抓到含关键词的绑定值（共 %d 个参数）", len(vars))
	}
	return out
}

func TestKeywordEntrancesShareOnePattern(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "关键词口径")
	m := productmodel.NewModel(db)
	ctx := context.Background()

	// 不转义这一档：每一处入口都必须绑出同一个 pattern。
	want := "%" + keywordProbe + "%"
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"商品列表", func() error { _, err := m.List(ctx, projectID, keywordProbe, "", 20, 0); return err }},
		{"商品计数", func() error { _, err := m.Count(ctx, projectID, keywordProbe, ""); return err }},
		{"属性列表", func() error { _, err := m.ListAttributes(ctx, projectID, keywordProbe, nil, 20, 0); return err }},
		{"属性计数", func() error { _, err := m.CountAttributes(ctx, projectID, keywordProbe, nil); return err }},
		{"品牌列表", func() error { _, err := m.ListBrands(ctx, projectID, keywordProbe, 20, 0); return err }},
		{"品牌计数", func() error { _, err := m.CountBrands(ctx, projectID, keywordProbe); return err }},
		{"标签列表", func() error { _, err := m.ListTags(ctx, projectID, "", keywordProbe, 20, 0); return err }},
		{"标签计数", func() error { _, err := m.CountTags(ctx, projectID, "", keywordProbe); return err }},
		{"分类列表", func() error { _, err := m.ListCategories(ctx, projectID, keywordProbe); return err }},
		{"分类计数", func() error { _, err := m.CountCategories(ctx, projectID, keywordProbe); return err }},
		{"发布记录全量", func() error { _, err := m.ListForPublishRecords(ctx, projectID, keywordProbe); return err }},
		{"发布记录本页", func() error { _, err := m.ListForPublishRecordsPage(ctx, projectID, keywordProbe, 20, 0); return err }},
		{"发布记录计数", func() error { _, err := m.CountForPublishRecords(ctx, projectID, keywordProbe); return err }},
		{"定价筛选", func() error {
			_, err := m.ListProductsForPricing(ctx, productmodel.PricingFilter{ProjectID: projectID, Keyword: keywordProbe})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, got := range captureKeywordVars(t, db, tc.run) {
				if got != want {
					t.Fatalf("%s 的关键词 pattern 与统一构造点不一致：got %q want %q（该入口应走 productKeywordPattern）", tc.name, got, want)
				}
			}
		})
	}

	// 已转义这一档：集合源 / 站内搜索 / 分类搜索森林都按字面量匹配，% 与 _ 必须带反斜杠进绑定值。
	// 三条入口现在共用 literalKeywordPattern，所以「字面量档有几个构造点」是可数的。
	t.Run("字面量档入口共用同一个构造点", func(t *testing.T) {
		wantEscaped := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(keywordProbe) + "%"
		for _, tc := range []struct {
			name string
			run  func() error
		}{
			{"集合源", func() error {
				_, err := m.ListForCollection(ctx, productmodel.CollectionFilter{
					ProjectID: projectID, Keyword: keywordProbe,
				}, 20, 0)
				return err
			}},
			{"站内搜索", func() error { _, err := m.SearchPublished(ctx, projectID, keywordProbe, 20); return err }},
			{"分类搜索森林", func() error {
				_, _, err := m.ListCategorySearchForest(ctx, projectID, keywordProbe)
				return err
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				for _, v := range captureKeywordVars(t, db, tc.run) {
					if v != wantEscaped {
						t.Fatalf("%s 的关键词应转义后绑定：got %q want %q（该入口应走 literalKeywordPattern）", tc.name, v, wantEscaped)
					}
				}
			})
		}
	})

}
