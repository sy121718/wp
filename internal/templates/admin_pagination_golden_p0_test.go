package templates

// admin_pagination_golden_p0_test.go — 分页迁移的 P0a 兼容安全网（task-97，设计见 task-94 v2）。
//
// 目的：在**动任何生产代码之前**，把「旧 pkg/pagination 数据 + 现 partial」渲染出来的分页条
// 字节冻结下来。后续 P2 把 partial 改成「新事实（pagination.Page）+ 模板构造」时，必须渲染出
// **逐字节相同**的 HTML —— 这份文件就是那次等价声明的唯一凭据；没有它，「搬家」与「改版」在
// 评审里无法区分。
//
// 为什么 golden 用**旧 API 现算**（`pagination.BuildPagination`）而不是手写一串 HTML：
// 手写的期望值只能证明「我以为旧代码输出什么」，而这里要钉的是**当前实现**的输出 —— 谁改了
// BuildPagination 的窗口/文案/URL 规则，这份 golden 立刻变红。
//
// 覆盖七形态（对 task-94 §4 P0a 的逐条落实）：
//  1. 单页内（pages<=7）：无省略号；
//  2. 多页双省略号（中段页码）；
//  3. 首页 prev 禁用（且有尾省略号）；
//  4. 末页 next 禁用（且有首省略号）；
//  5. 单页/空集：**不渲染** `.pagination`（BuildPagination 返回 nil → 键缺失）；
//  6. mail 列表页形态：页码参数改名（`account_page`）且**不带 limit**；
//  7. product 展开区形态：`rows=0` 时的信息行（「第 0-0 条」）。
//
// 不碰生产：本文件只读 pkg/pagination 的现有 API，不改它、不改 partial、不改文档
// （task-97 的写域仅 pkg/pagination 与 internal/templates，且本卡只新增测试）。

import (
	"strings"
	"testing"

	"go_wp/pkg/pagination"
)

// p0PaginationShape 一个待冻结的分页条形态。
type p0PaginationShape struct {
	name string
	// 直接给旧 API 的入参（total/page/limit/base），用 BuildPagination 现算 Info/Links。
	total int64
	page  int
	limit int
	base  string
	// info/links 非空时改用它们（用于旧数据形态不由 BuildPagination 产出的两种场景：
	// mail 的参数改名重写、product 展开区的 rows=0 信息行）。
	info  string
	links []pagination.PageLink
}

// p0PaginationKeys 把形态翻成 partial 读的那两个键（与 handler 注入方式一致：nil → 空 map）。
func p0PaginationKeys(t *testing.T, s p0PaginationShape) map[string]any {
	t.Helper()
	if s.info != "" || s.links != nil {
		return map[string]any{"PaginationInfo": s.info, "PaginationLinks": s.links}
	}
	// t=nil → 走中文原文（与 TranslateFunc 未初始化时同一条链；本包测试进程不连 i18n 库）。
	return pagination.BuildPagination(s.total, s.page, s.limit, s.base, nil).TemplateKeys()
}

// p0PaginationShapes 七形态；base 用不带筛选的路径（与 mail 两处调用一致）。
func p0PaginationShapes() []p0PaginationShape {
	return []p0PaginationShape{
		{name: "P0a-1-单页内无省略号", total: 45, page: 1, limit: 20, base: "/admin/mail"},
		{name: "P0a-2-多页双省略号", total: 1000, page: 50, limit: 20, base: "/admin/mail"},
		{name: "P0a-3-首页prev禁用", total: 1000, page: 1, limit: 20, base: "/admin/mail"},
		{name: "P0a-4-末页next禁用", total: 1000, page: 50, limit: 20, base: "/admin/mail"},
		{name: "P0a-5-单页不渲染", total: 5, page: 1, limit: 20, base: "/admin/mail"},
		{
			// mail_page.go:1275 的形态：BuildPagination 产出 page=2 的链接后被
			// mailListPagination 改名为 account_page 并删掉 limit —— 这里按**改名后的字节**冻结。
			name: "P0a-6-mail参数改名无limit", total: 45, page: 2, limit: 20, base: "/admin/mail",
			info: "共 45 条，第 21-40 条",
			links: []pagination.PageLink{
				{Label: "上一页", URL: "/admin/mail?account_page=1"},
				{Label: "1", URL: "/admin/mail?account_page=1"},
				{Label: "2", URL: "/admin/mail?account_page=2", Active: true},
				{Label: "3", URL: "/admin/mail?account_page=3"},
				{Label: "下一页", URL: "/admin/mail?account_page=3"},
			},
		},
		{
			// product_page.go:5726 tagHitsPageInfo：本页 0 行时 from/to 都是 0，
			// 于是信息行是「第 0-0 条」——与分页条的 total 口径不同，这里冻结现状。
			name: "P0a-7-product片段rows为零", total: 120, page: 2, limit: 20, base: "/admin/product-tags/hits",
			info:  "共 120 条，第 0-0 条",
			links: pagination.BuildPagination(120, 2, 20, "/admin/product-tags/hits", nil).Links,
		},
	}
}

// p0PaginationGolden 七形态的冻结字节（= 2026-10 task-97 时点、旧 API + 现 partial 的真实输出）。
//
// 改这里的唯一正当理由：**分页条的可见形态真的要变**（那种改动必须带自己的行为类卡片与截图），
// 或是 P4 删除窗口期结束后把本文件一并删除。P2 搬家期间它只能「保持逐字节相等」。
var p0PaginationGolden = map[string]string{
	"P0a-1-单页内无省略号":         "\n<nav class=\"pagination\" aria-label=\"分页\">\n    <div class=\"pagination-info\">共 45 条，第 1-20 条</div>\n    <div class=\"pagination-pages\">\n        \n        \n        <span class=\"pagination-btn is-disabled\">上一页</span>\n        \n        \n        \n        <span class=\"pagination-btn is-active\">1</span>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=2&amp;limit=20\">2</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=3&amp;limit=20\">3</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=2&amp;limit=20\">下一页</a>\n        \n        \n    </div>\n</nav>\n\n",
	"P0a-2-多页双省略号":          "\n<nav class=\"pagination\" aria-label=\"分页\">\n    <div class=\"pagination-info\">共 1000 条，第 981-1000 条</div>\n    <div class=\"pagination-pages\">\n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=49&amp;limit=20\">上一页</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=1&amp;limit=20\">1</a>\n        \n        \n        \n        <span class=\"pagination-btn is-disabled\">…</span>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=45&amp;limit=20\">45</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=46&amp;limit=20\">46</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=47&amp;limit=20\">47</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=48&amp;limit=20\">48</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=49&amp;limit=20\">49</a>\n        \n        \n        \n        <span class=\"pagination-btn is-active\">50</span>\n        \n        \n        \n        <span class=\"pagination-btn is-disabled\">下一页</span>\n        \n        \n    </div>\n</nav>\n\n",
	"P0a-3-首页prev禁用":        "\n<nav class=\"pagination\" aria-label=\"分页\">\n    <div class=\"pagination-info\">共 1000 条，第 1-20 条</div>\n    <div class=\"pagination-pages\">\n        \n        \n        <span class=\"pagination-btn is-disabled\">上一页</span>\n        \n        \n        \n        <span class=\"pagination-btn is-active\">1</span>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=2&amp;limit=20\">2</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=3&amp;limit=20\">3</a>\n        \n        \n        \n        <span class=\"pagination-btn is-disabled\">…</span>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=50&amp;limit=20\">50</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=2&amp;limit=20\">下一页</a>\n        \n        \n    </div>\n</nav>\n\n",
	"P0a-4-末页next禁用":        "\n<nav class=\"pagination\" aria-label=\"分页\">\n    <div class=\"pagination-info\">共 1000 条，第 981-1000 条</div>\n    <div class=\"pagination-pages\">\n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=49&amp;limit=20\">上一页</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=1&amp;limit=20\">1</a>\n        \n        \n        \n        <span class=\"pagination-btn is-disabled\">…</span>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=45&amp;limit=20\">45</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=46&amp;limit=20\">46</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=47&amp;limit=20\">47</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=48&amp;limit=20\">48</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?page=49&amp;limit=20\">49</a>\n        \n        \n        \n        <span class=\"pagination-btn is-active\">50</span>\n        \n        \n        \n        <span class=\"pagination-btn is-disabled\">下一页</span>\n        \n        \n    </div>\n</nav>\n\n",
	"P0a-5-单页不渲染":           "\n",
	"P0a-6-mail参数改名无limit":  "\n<nav class=\"pagination\" aria-label=\"分页\">\n    <div class=\"pagination-info\">共 45 条，第 21-40 条</div>\n    <div class=\"pagination-pages\">\n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?account_page=1\">上一页</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?account_page=1\">1</a>\n        \n        \n        \n        <span class=\"pagination-btn is-active\">2</span>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?account_page=3\">3</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/mail?account_page=3\">下一页</a>\n        \n        \n    </div>\n</nav>\n\n",
	"P0a-7-product片段rows为零": "\n<nav class=\"pagination\" aria-label=\"分页\">\n    <div class=\"pagination-info\">共 120 条，第 0-0 条</div>\n    <div class=\"pagination-pages\">\n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/product-tags/hits?page=1&amp;limit=20\">上一页</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/product-tags/hits?page=1&amp;limit=20\">1</a>\n        \n        \n        \n        <span class=\"pagination-btn is-active\">2</span>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/product-tags/hits?page=3&amp;limit=20\">3</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/product-tags/hits?page=4&amp;limit=20\">4</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/product-tags/hits?page=5&amp;limit=20\">5</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/product-tags/hits?page=6&amp;limit=20\">6</a>\n        \n        \n        \n        <a class=\"pagination-btn\" href=\"/admin/product-tags/hits?page=3&amp;limit=20\">下一页</a>\n        \n        \n    </div>\n</nav>\n\n",
}

// TestP0PaginationGoldenFreezesCurrentBytes 七形态逐字节冻结。
func TestP0PaginationGoldenFreezesCurrentBytes(t *testing.T) {
	for _, shape := range p0PaginationShapes() {
		shape := shape
		t.Run(shape.name, func(t *testing.T) {
			data := adminShellData()
			for k, v := range p0PaginationKeys(t, shape) {
				data[k] = v
			}
			out, err := render(t, newAdminTestSet(), "admin/partials/pagination.html", data)
			if err != nil {
				t.Fatalf("渲染分页条失败: %v", err)
			}
			want := p0PaginationGolden[shape.name]
			if out != want {
				t.Fatalf("分页条字节与 P0a 冻结值不一致。\n实际: %q\n期望: %q", out, want)
			}
		})
	}
}

// TestP0PaginationSinglePageRenderingContract 形态 5 的语义判据（不依赖 golden 文本）：
// 单页/空集**不得**出现 `.pagination`（`docs/rules/testing.md:89-112`：单页挂一条
// 「上一页 / 下一页」是噪声，测试也只能断言「无分页条 + 行数」）。
func TestP0PaginationSinglePageRenderingContract(t *testing.T) {
	for _, tc := range []struct {
		name  string
		total int64
		page  int
		limit int
	}{
		{name: "总量小于一页", total: 5, page: 1, limit: 20},
		{name: "总量为零", total: 0, page: 1, limit: 20},
		{name: "恰好一页", total: 20, page: 1, limit: 20},
		{name: "空集但页码越界", total: 0, page: 3, limit: 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := adminShellData()
			for k, v := range pagination.BuildPagination(tc.total, tc.page, tc.limit, "/admin/mail", nil).TemplateKeys() {
				data[k] = v
			}
			out, err := render(t, newAdminTestSet(), "admin/partials/pagination.html", data)
			if err != nil {
				t.Fatalf("渲染分页条失败: %v", err)
			}
			if strings.Contains(out, "pagination") {
				t.Fatalf("单页/空集不该渲染分页条，实际输出: %q", out)
			}
		})
	}
}
