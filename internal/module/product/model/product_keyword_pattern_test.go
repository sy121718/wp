package productmodel_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/public/test/support"
)

const keywordProbe = "50%_折"

const keywordMarker = "折"

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
