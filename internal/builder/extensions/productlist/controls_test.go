package productlist

import (
	"context"
	"go_wp/internal/templates"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
)

type fakeOptionsCollection struct {
	fakeCollection
	options core.CollectionFilterOptions
}

func (f *fakeOptionsCollection) CollectionFilterOptions(_ context.Context, source, projectID string) (core.CollectionFilterOptions, error) {

	_, _ = source, projectID
	return f.options, nil
}

func optionsFixture() core.CollectionFilterOptions {
	return core.CollectionFilterOptions{
		Categories: []core.CollectionFilterChoice{
			{ID: "cat-1", Name: "一次性"},
			{ID: "cat-2", Name: "烟油", ParentID: "cat-1"},
		},
		Brands: []core.CollectionFilterChoice{{ID: "brand-1", Name: "Alibarbar"}},
		Tags: []core.CollectionFilterChoice{
			{ID: "tag-hot", Name: "热卖"},
			{ID: "tag-new", Name: "新品"},
		},
		Attributes: []core.CollectionFilterAttributeGroup{{
			Key: "color", Name: "颜色",
			Values: []core.CollectionFilterChoice{{Key: "red", Name: "红"}, {Key: "blue", Name: "蓝"}},
		}},
	}
}

func buildViewOf(t *testing.T, coll core.CollectionResolver, kv map[string]any, pushQuery string) View {
	t.Helper()
	kv["collectionSource"] = "content:product"
	raw := propsOf(t, kv)
	var p Props
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("props 解码失败: %v", err)
	}
	p.PushQuery = pushQuery
	view, err := BuildView(&core.Node{ID: "list1", Type: Type, Props: raw}, &p, ctxWithOptions(coll))
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	return view
}

func itemsColl(items ...map[string]any) *fakeOptionsCollection {
	return &fakeOptionsCollection{fakeCollection: fakeCollection{items: items}, options: optionsFixture()}
}

func ctxWithOptions(coll core.CollectionResolver) *core.RenderContext {
	return &core.RenderContext{CSS: &core.CSSBuckets{}, Context: context.Background(), Collection: coll, ProjectID: "proj-1"}
}

func TestFilterSectionsFromOptions(t *testing.T) {
	view := buildViewOf(t, itemsColl(itemOf("tee", "T 恤", "2026-01-01T00:00:00Z")),
		map[string]any{"filters": "categories,brands,tags,attributes"}, "")
	if len(view.FilterSections) != 4 {
		t.Fatalf("应渲染 4 块筛选（分类/品牌/标签/属性），实际 %d", len(view.FilterSections))
	}
	for _, section := range view.FilterSections {
		if len(section.Options) == 0 {
			t.Fatalf("筛选块 %q 不该为空", section.Title)
		}
		for _, opt := range section.Options {
			if opt.Href == "" || opt.ActionQuery == "" || opt.PushURL == "" {
				t.Fatalf("选项 %q 三样 URL 必须齐全: %+v", opt.Label, opt)
			}
			if strings.Contains(opt.ActionQuery, "nodeId=") || strings.Contains(opt.ActionQuery, "projectId=") {
				t.Fatalf("数据请求只带工程与查询事实: %s", opt.ActionQuery)
			}
			if strings.Contains(opt.PushURL, "nodeId") || strings.Contains(opt.PushURL, "projectId") {
				t.Fatalf("推送 URL 不该带实例配置（地址栏只放语义参数）: %s", opt.PushURL)
			}
		}
	}

	view2 := buildViewOf(t, itemsColl(itemOf("tee", "T 恤", "2026-01-01T00:00:00Z")),
		map[string]any{"filters": "categories"}, "")
	if len(view2.FilterSections) != 1 || view2.FilterSections[0].Kind != "categories" {
		t.Fatalf("只勾分类时应只渲染分类块: %+v", view2.FilterSections)
	}
}

func TestTemplateRendersInteractiveAttributes(t *testing.T) {
	view := buildViewOf(t, itemsColl(itemOf("tee", "T 恤", "2026-01-01T00:00:00Z")), map[string]any{
		"filters": "categories,brands,tags", "toolbar": "sort,pageSize,columns,onSale",
	}, "categoryId=cat-1")
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("NewEmbeddedComponentSet: %v", err)
	}
	tpl, err := set.GetTemplate("product_list")
	if err != nil {
		t.Fatalf("GetTemplate(product_list): %v", err)
	}
	var buf strings.Builder
	if err := tpl.Execute(&buf, nil, struct {
		Classes  string
		CustomID string
		V        View
	}{Classes: "sky-c-list1", V: view}); err != nil {
		t.Fatalf("渲染模板失败: %v", err)
	}
	html := buf.String()
	for _, want := range []string{
		"data-sky-product-list",
		"/api/site/product/catalog?",
		`data-site-view="catalog"`,
		`data-catalog-query=`,
		`<template data-catalog-card>`,
		"sky-product-list-filter-option",
		"sky-product-list-tool-option",
		"is-disabled",
		"只看在售",
		"onSale=true",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("产物缺少 %q", want)
		}
	}
}

func TestTagFilterToggles(t *testing.T) {

	view := buildViewOf(t, itemsColl(itemOf("tee", "T 恤", "2026-01-01T00:00:00Z")),
		map[string]any{"filters": "tags", "filterTagIds": "tag-new"}, "tagIds=tag-new")
	if len(view.FilterSections) != 1 {
		t.Fatalf("应有且只有标签块: %+v", view.FilterSections)
	}
	var hot, newer ControlOption
	for _, opt := range view.FilterSections[0].Options {
		switch opt.Label {
		case "热卖":
			hot = opt
		case "新品":
			newer = opt
		}
	}
	if !newer.Active {
		t.Fatalf("已选标签应高亮: %+v", newer)
	}

	if !strings.Contains(hot.PushURL, "tagIds=") || !strings.Contains(hot.PushURL, "tag-hot") || !strings.Contains(hot.PushURL, "tag-new") {
		t.Fatalf("点未选中标签应把它加进 tagIds: %s", hot.PushURL)
	}

	if strings.Contains(newer.PushURL, "tag-new") {
		t.Fatalf("再点一次应移除该标签: %s", newer.PushURL)
	}
}

func TestSortOptionsHaveDisabledPlaceholder(t *testing.T) {
	view := buildViewOf(t, itemsColl(itemOf("tee", "T 恤", "2026-01-01T00:00:00Z")),
		map[string]any{"toolbar": "sort,pageSize,columns"}, "")

	if len(view.SortOptions) != 7 {
		t.Fatalf("排序应有 6 个可点 + 1 个禁用占位，实际 %d", len(view.SortOptions))
	}
	last := view.SortOptions[len(view.SortOptions)-1]
	if !last.Disabled || last.DisabledHint == "" || last.ActionQuery != "" {
		t.Fatalf("热度应是禁用占位且不给链接: %+v", last)
	}
	if !view.ShowSort || !view.ShowPageSize || !view.ShowColumns {
		t.Fatalf("工具条三项都该打开: %+v", view)
	}
	if len(view.PageSizeOptions) != 3 || len(view.ColumnOptions) != 4 {
		t.Fatalf("每页 3 档、视图 4 档: %d / %d", len(view.PageSizeOptions), len(view.ColumnOptions))
	}
}

func TestPagerLinksCarrySemanticParams(t *testing.T) {
	items := make([]map[string]any, 0, 3)
	for _, slug := range []string{"a", "b", "c"} {
		items = append(items, itemOf(slug, strings.ToUpper(slug), "2026-01-0"+slug+"T00:00:00Z"))
	}
	view := buildViewOf(t, itemsColl(items...),
		map[string]any{"pageSize": 1, "page": 2, "toolbar": "sort"}, "pageSize=1&page=2")
	if !view.HasPager || !view.HasPrev || !view.HasNext {
		t.Fatalf("第 2 页（共 3 条、每页 1 条）应上下页都有: %+v", view)
	}
	if !strings.Contains(view.NextLink.PushURL, "page=3") || !strings.Contains(view.NextLink.ActionQuery, "page=3") {
		t.Fatalf("下一页链接应指向第 3 页: %s", view.NextLink.PushURL)
	}
	if !strings.Contains(view.PrevLink.PushURL, "page=1") {
		t.Fatalf("上一页链接应指向第 1 页: %s", view.PrevLink.PushURL)
	}

	if strings.Contains(view.SortOptions[1].PushURL, "page=") {
		t.Fatalf("换排序时不应保留 page: %s", view.SortOptions[1].PushURL)
	}
}

func TestCategorySingleSelectSemantics(t *testing.T) {
	coll := itemsColl(itemOf("tee", "T 恤", "2026-01-01T00:00:00Z"))

	multiView := buildViewOf(t, coll, map[string]any{"filters": "categories"}, "")
	if len(multiView.FilterSections) != 1 {
		t.Fatalf("应渲染分类块: %+v", multiView.FilterSections)
	}
	multi := multiView.FilterSections[0]
	if !multi.Multi {
		t.Fatalf("默认应为多选")
	}

	singleView := buildViewOf(t, coll, map[string]any{"filters": "categories", "categoryMulti": "off"}, "")
	if len(singleView.FilterSections) != 1 {
		t.Fatalf("应渲染分类块: %+v", singleView.FilterSections)
	}
	single := singleView.FilterSections[0]
	if single.Multi {
		t.Fatalf("categoryMulti=off 时应为单选")
	}
	for _, opt := range single.Options {
		if !strings.Contains(opt.PushURL, "categoryIds=") {
			t.Fatalf("单选下未选中项应给出替换链接: %+v", opt)
		}
	}

	cancelView := buildViewOf(t, coll, map[string]any{
		"filters": "categories", "categoryMulti": "off", "filterCategoryIds": "cat-1",
	}, "")
	cancel := cancelView.FilterSections[0]
	var found bool
	for _, opt := range cancel.Options {
		if opt.Label != "一次性" {
			continue
		}
		found = true
		if !opt.Active {
			t.Fatalf("已选中的分类应处于选中态: %+v", opt)
		}
		if strings.Contains(opt.PushURL, "categoryIds=") {
			t.Fatalf("单选下点已选中项应清空筛选，实际 %s", opt.PushURL)
		}
	}
	if !found {
		t.Fatalf("未找到「一次性」分类选项: %+v", cancel.Options)
	}
}
