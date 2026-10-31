package productlist

import (
	"testing"

	"go_wp/internal/builder/core"
)

func archiveCtx(entityType, entityID string) *core.RenderContext {
	ctx := &core.RenderContext{}
	ctx.SetArchiveEntity(entityType, entityID)
	return ctx
}

func TestWithArchiveFilter(t *testing.T) {
	cases := []struct {
		name       string
		props      Props
		entityType string
		entityID   string
		wantNil    bool
		check      func(t *testing.T, got *Props)
	}{
		{
			name: "关闭开关时不合并", props: Props{FilterFromArchive: ""},
			entityType: "category", entityID: "cat-1", wantNil: true,
		},
		{
			name: "分类归档落分类维度", props: Props{FilterFromArchive: "on"},
			entityType: "category", entityID: "cat-1",
			check: func(t *testing.T, got *Props) {
				if got.FilterCategoryID != "cat-1" {
					t.Fatalf("分类归档应筛 filterCategoryId，实际 %q", got.FilterCategoryID)
				}
			},
		},
		{
			name: "品牌归档落品牌维度", props: Props{FilterFromArchive: "on"},
			entityType: "brand", entityID: "brand-1",
			check: func(t *testing.T, got *Props) {
				if got.FilterBrandID != "brand-1" {
					t.Fatalf("品牌归档应筛 filterBrandId，实际 %q", got.FilterBrandID)
				}
			},
		},
		{
			name: "标签归档落标签维度", props: Props{FilterFromArchive: "on"},
			entityType: "tag", entityID: "tag-1",
			check: func(t *testing.T, got *Props) {
				if got.FilterTagIDs != "tag-1" {
					t.Fatalf("标签归档应筛 filterTagIds，实际 %q", got.FilterTagIDs)
				}
			},
		},
		{
			name: "无对应维度的归档类型不筛", props: Props{FilterFromArchive: "on"},
			entityType: "article", entityID: "art-1", wantNil: true,
		},
		{
			name: "空归档实体不筛", props: Props{FilterFromArchive: "on"},
			entityType: "category", entityID: "", wantNil: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			props := tc.props
			got := props.withArchiveFilter(archiveCtx(tc.entityType, tc.entityID))
			if tc.wantNil {
				if got != nil {
					t.Fatalf("应返回 nil（保持原 Props），实际 %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("应返回合并后的副本")
			}

			if props.FilterCategoryID != "" || props.FilterBrandID != "" || props.FilterTagIDs != "" {
				t.Fatalf("不应改写原 Props，实际 %+v", props)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}
