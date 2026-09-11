package productlist

// controls_test.go — 筛选栏 / 工具条 / 分页的交互测试（issue #27）。
//
// 这里验证的是「构建期真的把可点的控件拼出来了」：每个控件都要同时给出降级 href、
// 片段请求 URL（带实例配置）与推送 URL（只带语义参数）—— 三样缺一，
// 对应的交互路径（无 JS / 局部刷新 / 地址栏同步）就断一条。

import (
	"context"
	"encoding/json"
	"go_wp/internal/templates"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// fakeOptionsCollection 既给集合项、也给筛选选项（模拟实现了能力探测的集合源）。
type fakeOptionsCollection struct {
	fakeCollection
	options core.CollectionFilterOptions
}

func (f *fakeOptionsCollection) CollectionFilterOptions(_ context.Context, projectID string) (core.CollectionFilterOptions, error) {
	_ = projectID
	return f.options, nil
}

// optionsFixture 一份典型的筛选项：两个分类（一父一子）、一个品牌、两个标签、一个属性组。
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

// buildViewOf 用「已选集合源」的 props 渲染一次（未选源的节点会走空态分支，测不到控件）。
//
// pushQuery 模拟片段层灌进来的当前语义参数（构建期为空的默认态另有用例覆盖）。
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

// itemsColl 造一个「带筛选项能力 + 若干集合项」的集合源。
func itemsColl(items ...map[string]any) *fakeOptionsCollection {
	return &fakeOptionsCollection{fakeCollection: fakeCollection{items: items}, options: optionsFixture()}
}

// ctxWithOptions 带能力探测的渲染上下文。
func ctxWithOptions(coll core.CollectionResolver) *core.RenderContext {
	return &core.RenderContext{CSS: &core.CSSBuckets{}, Context: context.Background(), Collection: coll, ProjectID: "proj-1"}
}

// TestFilterSectionsFromOptions 筛选栏：勾选的维度渲染出来，未勾选的不渲染。
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
			if opt.Href == "" || opt.FragmentGet == "" || opt.PushURL == "" {
				t.Fatalf("选项 %q 三样 URL 必须齐全: %+v", opt.Label, opt)
			}
			if !strings.Contains(opt.FragmentGet, "nodeId=list1") || !strings.Contains(opt.FragmentGet, "projectId=proj-1") {
				t.Fatalf("片段请求必须带实例配置: %s", opt.FragmentGet)
			}
			if strings.Contains(opt.PushURL, "nodeId") || strings.Contains(opt.PushURL, "projectId") {
				t.Fatalf("推送 URL 不该带实例配置（地址栏只放语义参数）: %s", opt.PushURL)
			}
		}
	}

	// 只勾分类：其他三块不渲染。
	view2 := buildViewOf(t, itemsColl(itemOf("tee", "T 恤", "2026-01-01T00:00:00Z")),
		map[string]any{"filters": "categories"}, "")
	if len(view2.FilterSections) != 1 || view2.FilterSections[0].Kind != "categories" {
		t.Fatalf("只勾分类时应只渲染分类块: %+v", view2.FilterSections)
	}
}

// TestTemplateRendersInteractiveAttributes 模板必须把交互属性真的输出：
// 容器带片段请求与冷启动补正，控件带 hx-get / hx-push-url / 降级 href。
//
// 缺任何一条，对应的交互路径就断一条：没有 hx-get → 点击整页刷新（能用但失去局部刷新）；
// 没有 hx-push-url → 地址栏不同步（不可分享、刷新回默认）；
// 没有 href → 无 JS 时控件不可点；没有 load 补正 → 带 query 打开时看到的是默认那一屏。
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
		"/_fragments/productList?",
		"hx-push-url=",
		`hx-trigger="load[location.search.length > 1]"`,
		`hx-target="closest [data-sky-product-list]"`,
		"sky-product-list-filter-option",
		"sky-product-list-tool-option",
		"is-disabled", // 热度排序的禁用占位
		"只看在售",        // 工具条上的在售开关（与 #11 的 on_sale 同判定）
		"onSale=true", // 未选中时点它就是打开
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("产物缺少 %q", want)
		}
	}
}

// TestTagFilterToggles 多标签是可切换的：点未选中的加入、点已选中的移除。
func TestTagFilterToggles(t *testing.T) {
	// 当前已选「新品」。
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
	// 点「热卖」= 加入（两个都要）。
	if !strings.Contains(hot.PushURL, "tagIds=") || !strings.Contains(hot.PushURL, "tag-hot") || !strings.Contains(hot.PushURL, "tag-new") {
		t.Fatalf("点未选中标签应把它加进 tagIds: %s", hot.PushURL)
	}
	// 点「新品」= 从已选里移除。
	if strings.Contains(newer.PushURL, "tag-new") {
		t.Fatalf("再点一次应移除该标签: %s", newer.PushURL)
	}
}

// TestSortOptionsHaveDisabledPlaceholder 排序：三项可点 + 热度是禁用占位（不假装能排）。
func TestSortOptionsHaveDisabledPlaceholder(t *testing.T) {
	view := buildViewOf(t, itemsColl(itemOf("tee", "T 恤", "2026-01-01T00:00:00Z")),
		map[string]any{"toolbar": "sort,pageSize,columns"}, "")
	if len(view.SortOptions) != 4 {
		t.Fatalf("排序应有 3 个可点 + 1 个禁用占位，实际 %d", len(view.SortOptions))
	}
	last := view.SortOptions[len(view.SortOptions)-1]
	if !last.Disabled || last.DisabledHint == "" || last.FragmentGet != "" {
		t.Fatalf("热度应是禁用占位且不给链接: %+v", last)
	}
	if !view.ShowSort || !view.ShowPageSize || !view.ShowColumns {
		t.Fatalf("工具条三项都该打开: %+v", view)
	}
	if len(view.PageSizeOptions) != 3 || len(view.ColumnOptions) != 4 {
		t.Fatalf("每页 3 档、视图 4 档: %d / %d", len(view.PageSizeOptions), len(view.ColumnOptions))
	}
}

// TestPagerLinksCarrySemanticParams 分页链接：换页保留当前筛选，且回第 1 页时清掉 page。
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
	if !strings.Contains(view.NextLink.PushURL, "page=3") || !strings.Contains(view.NextLink.FragmentGet, "page=3") {
		t.Fatalf("下一页链接应指向第 3 页: %s", view.NextLink.PushURL)
	}
	if !strings.Contains(view.PrevLink.PushURL, "page=1") {
		t.Fatalf("上一页链接应指向第 1 页: %s", view.PrevLink.PushURL)
	}
	// 换筛选必须清 page：否则筛选后条目变少会落在越界页，看到空列表。
	if strings.Contains(view.SortOptions[1].PushURL, "page=") {
		t.Fatalf("换排序时不应保留 page: %s", view.SortOptions[1].PushURL)
	}
}
