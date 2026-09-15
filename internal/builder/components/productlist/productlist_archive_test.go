package productlist

// productlist_archive_test.go — 归档上下文筛选（审计 EDT-004 第一层）。
//
// 归档型实例（分类页 / 标签页 / 品牌页）渲染列表时，筛选值来自**实例本身**。
// 这里只测映射这一层：把「当前归档实体」正确落到组件的哪个筛选维度上。
//
// 最关键的一条是 default 分支：归档实体类型在这个组件里没有对应维度时**不筛**，
// 而不是随便挑一个维度凑条件 —— 后者会安静地筛出错误结果（比如拿分类 id 当品牌筛），
// 页面上看起来只是「商品少了几个」。

import (
	"testing"

	"go_wp/internal/builder/core"
)

// archiveCtx 构造带归档实体的编译上下文。
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
			// 副本语义：调用方的 Props 不能被改写（同一次编译里组件可能被多处使用）。
			if props.FilterCategoryID != "" || props.FilterBrandID != "" || props.FilterTagIDs != "" {
				t.Fatalf("不应改写原 Props，实际 %+v", props)
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}
