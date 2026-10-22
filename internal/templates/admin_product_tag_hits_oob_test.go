package templates

// admin_product_tag_hits_oob_test.go — 命中数回写（htmx 4 的 <hx-partial>）的形状判据。
//
// 修的是这个缺陷：标签页里每个标签的「N 个商品」按钮文字来自**首屏渲染**的 t.ProductCount，
// 而命中数会在别处变动（解绑商品、删商品）。用户展开某标签的命中片段并翻页时，
// 片段内部的表格与信息行都是新的，外层标签列表上那个数字却是旧的 ——
// 他会反复点开同一个空标签。
//
// 为什么是 <hx-partial> 而不是 <hx-swap-oob>（htmx 4 官方称 partial "preferred over OOB swaps"）：
//
//	oob 靠**被换元素自身的 id** 找落点，然后把那个元素从响应里摘掉。由此带来一串形状约束：
//	元素必须在顶层（否则摘掉内容留下空壳）、必须有 id、id 必须与页面侧一致、必须是完整元素。
//	partial 是**服务端 swap 指令**：htmx 读 hx-target 执行换入，再把信封整段丢弃 ——
//	信封自身不需要 id、不需要是顶层、也不会留在页面里。
//
//	所以本文件比 oob 版本**少三条约束**，只钉「目标对了、内容对了、信封不残留」。
//
// 机制已在真实浏览器里验证过（htmx 4.0.0，复刻页面 + 片段结构）：点击后按钮文字从
// 「120 个商品」变成「3 个商品」，主内容正常换入，hx-partial 信封不残留在页面。

import (
	"strings"
	"testing"
)

// fragmentWithItems 命中片段的真实数据形态（与 TestProductTagHitsFragmentProvidesCardShell 同源）：
// 有 Items 才走正常分支，回写指令也在那个分支里。
func fragmentWithItems() map[string]any {
	return groupDData(map[string]any{
		"TagID": "t1", "ProjectID": "pr1", "TagName": "清仓",
		"Total":      120,
		"Items":      []map[string]any{{"ID": "p1", "Name": "上架商品", "Slug": "p-one", "Status": "published"}},
		"Pagination": tagHitsFacts(120, 2, 50, "pr1"),
	})
}

// TestTagHitsPartialTargetsCountButton 片段必须带一条指向命中数按钮的 hx-partial。
func TestTagHitsPartialTargetsCountButton(t *testing.T) {
	out, err := render(t, groupDSet(t), "admin/product/product_tag_hits", fragmentWithItems())
	if err != nil {
		t.Fatalf("命中商品片段渲染失败: %v", err)
	}
	if !strings.Contains(out, "<hx-partial") {
		t.Fatalf("片段缺少 hx-partial 指令，外层命中数不会更新：\n%s", out)
	}
	// hx-target 必须指向页面侧那个按钮的 id（`tag-hit-count-<TagID>`）。
	if !strings.Contains(out, `hx-target="#tag-hit-count-t1"`) {
		t.Fatalf("hx-target 与页面侧按钮 id 对不上（应为 #tag-hit-count-t1）：\n%s", out)
	}
}

// TestTagHitsPartialCarriesCurrentTotal 回写内容必须是**当前的真实数**（.Total），不是首屏值。
func TestTagHitsPartialCarriesCurrentTotal(t *testing.T) {
	out, err := render(t, groupDSet(t), "admin/product/product_tag_hits", fragmentWithItems())
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	idx := strings.Index(out, "<hx-partial")
	end := strings.Index(out[idx:], "</hx-partial>")
	if idx < 0 || end < 0 {
		t.Fatalf("hx-partial 未闭合：\n%s", out)
	}
	body := out[idx : idx+end]
	// 数据里 Total=120、TagName=清仓；回写体里应是 120 而不是空。
	if !strings.Contains(body, "120") {
		t.Fatalf("回写内容应带当前总数 120，实际：%s", body)
	}
}

// TestTagHitsPartialNeedsNoIDOnEnvelope hx-partial 信封本身不需要 id —— 这正是它比 oob 简单的地方。
//
// 判据取自 htmx 4 文档：`<hx-partial>` 用 hx-target **或** id 定位，
// 而定位的是**目标元素**，不是信封自己。信封上加 id 只会让人误以为它在做 oob。
func TestTagHitsPartialNeedsNoIDOnEnvelope(t *testing.T) {
	out, err := render(t, groupDSet(t), "admin/product/product_tag_hits", fragmentWithItems())
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	idx := strings.Index(out, "<hx-partial")
	if idx < 0 {
		t.Fatalf("找不到 hx-partial")
	}
	end := strings.Index(out[idx:], ">")
	if end < 0 {
		t.Fatalf("hx-partial 开始标签未闭合")
	}
	tag := out[idx : idx+end]
	if strings.Contains(tag, " id=") {
		t.Fatalf("hx-partial 信封不该带 id（定位靠 hx-target）：%s", tag)
	}
	if strings.Contains(tag, "hx-swap-oob") {
		t.Fatalf("不该混用已被取代的 hx-swap-oob：%s", tag)
	}
}

// TestTagHitsPartialCarriesNoGet hx-partial 只回写文字，不提供新的可点入口。
//
// 带上 hx-get 会让它成为第三个「可点一下」的东西，而真实翻页按钮只有上/下两个
// （页面上那个原按钮还在，用户想继续翻就点它）。
func TestTagHitsPartialCarriesNoGet(t *testing.T) {
	out, err := render(t, groupDSet(t), "admin/product/product_tag_hits", fragmentWithItems())
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	idx := strings.Index(out, "<hx-partial")
	end := strings.Index(out[idx:], ">")
	tag := out[idx : idx+end]
	if strings.Contains(tag, "hx-get") {
		t.Fatalf("hx-partial 不该带 hx-get（它只回写文字）：%s", tag)
	}
}

// TestTagHitsPageButtonHasStableID 页面侧的命中数按钮必须带稳定 id（hx-target 的落点）。
func TestTagHitsPageButtonHasStableID(t *testing.T) {
	out, err := render(t, groupDSet(t), "admin/product/product_tags", tagHitsPageData())
	if err != nil {
		t.Fatalf("标签页渲染失败: %v", err)
	}
	if !strings.Contains(out, `id="tag-hit-count-t1"`) {
		t.Fatalf("首屏命中数按钮缺少 id（hx-target 会找不到落点，静默失效）：\n%s", out)
	}
}
