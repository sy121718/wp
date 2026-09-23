package templates

import (
	"regexp"
	"strings"
	"testing"
)

// admin_product_tag_hits_card_test.go — 「命中的商品」卡改为内容驱动（审计 02-L P1-7）。
//
// 形态：卡片外壳搬进片段 partials/product_tag_hits.html，页面上只剩一个空的锚点容器
// #tag-hits-panel 作为 hx-target —— 点开之前页面上没有任何与命中商品有关的元素。
// 为什么不做 tabs：hx-target 指的就是这个容器，卡片若变成 hidden 的 tab 面板，HTMX 会把
// 商品行换进看不见的地方（点了「N 个商品」什么也没发生）。
//
// 三条判据各防一类静默回归：
//  ① 首屏只有标签列表一张 .card.card-body，锚点容器存在且初始为空；
//  ② 片段自带 .card 外壳 + .card-title + 商品表（点开之后才出现的就是这张卡）；
//  ③ role/aria-live/aria-label 仍在**页面那个空容器**上 —— 这是最容易被顺手搬走的一处：
//     搬进片段后每次替换都会重建播报区域，换入的内容对读屏不再播报，而页面上看不出任何异常。

// tagHitsAnchorRe 抓页面上锚点容器的整段开标签（属性判据都从这里读，避免整页文本里的
// 同名字符串误判 —— 例如卡片标题与 aria-label 用的是同一句中文）。
var tagHitsAnchorRe = regexp.MustCompile(`<div id="tag-hits-panel"[^>]*>`)

// tagHitsPageData 标签页的最小数据：一个手工标签带 3 个命中商品（命中数按钮因此渲染）。
func tagHitsPageData() map[string]any {
	return groupDData(map[string]any{
		"Err": "", "SelectedProject": "pr1", "Projects": groupDProjects(),
		"FilterKeyword": "", "Filtered": false,
		"RuleTypes": []map[string]any{{"Type": "new", "Name": "新品", "Params": "days"}},
		"Tags": []map[string]any{{
			"ID": "t1", "Name": "清仓", "Slug": "clearance", "Kind": "manual", "KindLabel": "手工",
			"IsRule": false, "RuleLabel": "", "ProductCount": 3, "RecalcAt": "",
			"RuleType": "", "RuleDays": "", "RuleMinPrice": "", "RuleMaxPrice": "",
		}},
	})
}

// ① 首屏：只有标签列表这张卡，命中卡整张不渲染（只剩一个空的锚点容器）。
func TestProductTagsPageRendersEmptyHitsAnchorOnly(t *testing.T) {
	out, err := render(t, groupDSet(t), "admin/product/product_tags", tagHitsPageData())
	if err != nil {
		t.Fatalf("标签页渲染失败: %v", err)
	}
	// 判据用 `class="card card-body"`（标签列表卡的类组合）而不是 .card 计数：
	// 同页的 details.section-fold.card（内置规则类型）也带 card 类，但它不带卡体，
	// 折叠形态在本页已有先例、不属于本次改动。
	if got := strings.Count(out, `class="card card-body"`); got != 1 {
		t.Fatalf("首屏应有且只有标签列表一张 .card.card-body，实际 %d 张", got)
	}
	// <section class="card …"> 只该有一处（标签列表卡）。同页还带 card 类的只有
	// details.section-fold.card（内置规则类型）—— 折叠形态在本页已有先例，本批不动它，
	// 这条同时钉住「别为了凑 .card 计数把折叠块改掉」。
	if got := strings.Count(out, `<section class="card`); got != 1 {
		t.Fatalf("首屏 <section class=\"card\"> 应只有标签列表一张，实际 %d 张", got)
	}
	if got := strings.Count(out, `class="section-fold card"`); got != 1 {
		t.Fatalf("内置规则类型的折叠块应原样保留，实际 %d 个", got)
	}
	// 「命中的商品」这句中文在首屏仍会出现 —— 但只作为锚点的 aria-label（区域名），
	// 不再是 visible 的卡标题。判据只能盯卡标题这个形状。
	if strings.Contains(out, `<h2 class="card-title mb-lg">命中的商品`) {
		t.Fatal("首屏不应渲染「命中的商品」卡标题：卡壳在片段里，内容到了才出现")
	}
	if strings.Contains(out, "在这里按页查看") {
		t.Fatal("首屏不应残留空壳卡里的引导语（它随卡壳一起交给了片段）")
	}
	anchor := tagHitsAnchorRe.FindString(out)
	if anchor == "" {
		t.Fatalf("首屏缺少 #tag-hits-panel 锚点容器（hx-target 的落点）")
	}
	if !strings.Contains(out, anchor+"</div>") {
		t.Fatalf("锚点容器初始应为空：%s", anchor)
	}
	// 标签数为 0 时同样不出现命中卡（现状如此，别改坏）：没有可点的命中数按钮，
	// 页面上就不该有任何与命中商品有关的元素。
	empty := tagHitsPageData()
	empty["Tags"] = []map[string]any{}
	out, err = render(t, groupDSet(t), "admin/product/product_tags", empty)
	if err != nil {
		t.Fatalf("空标签页渲染失败: %v", err)
	}
	if got := strings.Count(out, `class="card card-body"`); got != 1 {
		t.Fatalf("标签数为 0 时仍应只有标签列表一张卡，实际 %d 张", got)
	}
	if strings.Contains(out, `<h2 class="card-title mb-lg">命中的商品`) {
		t.Fatal("标签数为 0 时不应渲染「命中的商品」卡")
	}
}

// ② 片段自带卡壳 + 表格：点开「N 个商品」之后出现的就是这张带壳的卡。
func TestProductTagHitsFragmentProvidesCardShell(t *testing.T) {
	data := groupDData(map[string]any{
		"TagID": "t1", "ProjectID": "pr1", "TagName": "清仓",
		"Total": 120, "Page": 2, "PageSize": 50, "TotalPage": 3,
		"PageInfo": "共 120 条，第 51-100 条",
		"PrevURL":  "/admin/product-tags/hits?project=pr1&id=t1&page=1",
		"NextURL":  "/admin/product-tags/hits?project=pr1&id=t1&page=3",
		"Items":    []map[string]any{{"ID": "p1", "Name": "上架商品", "Slug": "p-one", "Status": "published"}},
	})
	out, err := render(t, groupDSet(t), "admin/product/product_tag_hits", data)
	if err != nil {
		t.Fatalf("命中商品片段渲染失败: %v", err)
	}
	for _, want := range []string{
		`class="card card-body"`,  // 卡壳随片段一起来
		`class="card-title mb-lg"`, // 卡标题
		"命中的商品", "<strong>清仓</strong>",
		`<table class="data-table`, "上架商品",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("片段缺少 %q：\n%s", want, out)
		}
	}
	// 翻页按钮换回的仍是**页面上**那个锚点（两个方向各一处）。
	if got := strings.Count(out, `hx-target="#tag-hits-panel"`); got != 2 {
		t.Fatalf("上一页/下一页都应换回 #tag-hits-panel，实际 %d 处", got)
	}
	// 锚点与播报区域只属于页面：片段若自带一份，换入后文档里就有两个同 id 元素
	// （HTMX 取第一个，翻页落点随时可能漂移），且 aria-live 区域每次被替换、读屏不再播报。
	if strings.Contains(out, `id="tag-hits-panel"`) {
		t.Fatalf("片段不应自带 #tag-hits-panel：\n%s", out)
	}
	if strings.Contains(out, "aria-live") {
		t.Fatalf("aria-live 必须留在页面容器上，不能随片段一起被替换：\n%s", out)
	}
}

// ②b 取数失败与「当前没有命中」两个分支同样自带卡壳 —— 落到锚点里的任何东西都得有容器，
// 否则提示会裸露在无背景的空白区域里。
func TestProductTagHitsFragmentBranchesCarryCardShell(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
		want string
	}{
		{
			name: "取数失败",
			data: map[string]any{"TagID": "t1", "ProjectID": "pr1", "Err": "标签不存在"},
			want: "标签不存在",
		},
		{
			name: "没有商品命中",
			data: map[string]any{
				"TagID": "t1", "ProjectID": "pr1", "TagName": "清仓",
				"Total": 0, "Page": 1, "PageSize": 50, "TotalPage": 0,
				"PageInfo": "共 0 条，第 0-0 条", "Items": []map[string]any{},
			},
			want: "当前没有商品命中这个标签",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := render(t, groupDSet(t), "admin/product/product_tag_hits", groupDData(tt.data))
			if err != nil {
				t.Fatalf("片段渲染失败: %v", err)
			}
			for _, want := range []string{`class="card card-body"`, "命中的商品", tt.want} {
				if !strings.Contains(out, want) {
					t.Fatalf("%s 分支缺少 %q：\n%s", tt.name, want, out)
				}
			}
		})
	}
}

// ③ 播报区域仍在页面的空容器上：HTMX 把片段换进一个带 aria-live 的区域，读屏才播报新内容；
// 三条属性缺任何一条都是一次静默回归（页面上完全看不出来）。
func TestProductTagHitsAnchorStaysLiveRegion(t *testing.T) {
	out, err := render(t, groupDSet(t), "admin/product/product_tags", tagHitsPageData())
	if err != nil {
		t.Fatalf("标签页渲染失败: %v", err)
	}
	anchor := tagHitsAnchorRe.FindString(out)
	if anchor == "" {
		t.Fatalf("首屏缺少 #tag-hits-panel 锚点容器")
	}
	for _, want := range []string{`role="region"`, `aria-live="polite"`, `aria-label="`} {
		if !strings.Contains(anchor, want) {
			t.Fatalf("锚点容器丢了 %s —— 换入的商品行对读屏不再播报：%s", want, anchor)
		}
	}
	if strings.Contains(anchor, `aria-label=""`) {
		t.Fatalf("锚点容器应有区域名（aria-label）：%s", anchor)
	}
}
