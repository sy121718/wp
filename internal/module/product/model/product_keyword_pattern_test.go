package productmodel_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/database"
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

	want := "%" + database.EscapeLikePattern(keywordProbe) + "%"
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
		{"发布记录整页", func() error {
			_, err := m.QueryPublishRecordPage(ctx, projectID, keywordProbe, "", 20, 1)
			return err
		}},
		{"定价筛选", func() error {
			_, err := m.ListProductsForPricing(ctx, productmodel.PricingFilter{ProjectID: projectID, Keyword: keywordProbe})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, got := range captureKeywordVars(t, db, tc.run) {
				if got != want {
					t.Fatalf("%s 的关键词 pattern 与统一构造点不一致：got %q want %q（该入口应走 literalKeywordPattern，用户输入按字面量）", tc.name, got, want)
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

func TestKeywordLiteralMatchingHitsOnlyLiteralRows(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	m := productmodel.NewModel(db)
	ctx := context.Background()

	cases := []struct {
		name     string
		rows     []string
		keyword  string
		wantRows []string
	}{
		{
			name:     "百分号按字面量",
			rows:     []string{"折扣50%折", "折扣50X折", "折扣5%X折"},
			keyword:  "50%折",
			wantRows: []string{"折扣50%折"},
		},
		{
			name:     "下划线按字面量",
			rows:     []string{"款号A_1", "款号AB1", "款号A_2"},
			keyword:  "A_1",
			wantRows: []string{"款号A_1"},
		},
		{
			name:     "反斜杠按字面量",
			rows:     []string{"规格16\\8", "规格16X8"},
			keyword:  "16\\8",
			wantRows: []string{"规格16\\8"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projID := uuid.NewString()
			support.SeedProjectRow(t, db, projID, "关键词字面量命中-"+tc.name)
			for i, name := range tc.rows {
				if err := db.Exec(
					"INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time)"+
						" VALUES (?, ?, ?, ?, ?, 'draft', ?, now(), now())",
					uuid.NewString(), projID, name,
					fmt.Sprintf("kw-%d", i), fmt.Sprintf("KW-%d", i), i,
				).Error; err != nil {
					t.Fatalf("落库 %q 失败：%v", name, err)
				}
			}
			list, err := m.List(ctx, projID, tc.keyword, "", 50, 0)
			if err != nil {
				t.Fatalf("List 检索失败：%v", err)
			}
			count, err := m.Count(ctx, projID, tc.keyword, "")
			if err != nil {
				t.Fatalf("Count 检索失败：%v", err)
			}
			got := make([]string, 0, len(list))
			for _, p := range list {
				got = append(got, p.Name)
			}
			if len(got) != len(tc.wantRows) {
				t.Fatalf("检索 %q 应只命中 %v，实际 %v", tc.keyword, tc.wantRows, got)
			}
			for i := range tc.wantRows {
				if got[i] != tc.wantRows[i] {
					t.Fatalf("检索 %q 命中集合不符：got %v want %v", tc.keyword, got, tc.wantRows)
				}
			}
			if count != int64(len(got)) {
				t.Fatalf("count 与 list 不是同一批行：count=%d rows=%d", count, len(got))
			}
			page, err := m.QueryPublishRecordPage(ctx, projID, tc.keyword, "", 200, 1)
			if err != nil {
				t.Fatalf("发布记录整页检索失败：%v", err)
			}
			if len(page.Rows) != len(tc.wantRows) || page.Total != int64(len(page.Rows)) {
				t.Fatalf("发布记录整页检索 %q 应同样只命中字面量行且 total 相等：rows=%d total=%d want=%d",
					tc.keyword, len(page.Rows), page.Total, len(tc.wantRows))
			}
		})
	}
}
