package templates

// admin_product_tag_hits_pagination_test.go — P3 B2b 的兼容安全网（task-109，Sol 设计审四条更正）。
//
// product 的展开区（`/admin/product-tags/hits` → admin/product/product_tag_hits.html）自带 nav，
// **不消费** admin/partials/pagination.html：按钮是 hx-get 打回本片段，不是 <a href>。它却是
// 模块里最后一处在 Go 里拼展示串的地方（旧 tagHitsPageInfo 用 fmt.Sprintf 填
// shell.pagination.info），本批把它也交到 Jet 侧。
//
// 冻结手法与 P0a/P2 一致：**改模板之前**用旧数据形态（PageInfo / PrevURL / NextURL）渲染真实
// 模板，把字节钉进下面这些 golden；改成事实键（Pagination）后必须渲染出同样的字节。
//
// 两条必须保住的语义（Sol 更正 ③ 与 P2）：
//   · 单页非空**仍要渲染** nav（两个禁用按钮）—— 这里不套 HasPagination()；
//   · info 行的区间按**实际行数**算（from=(Page-1)*Limit+1、to=from+len(Items)-1），
//     不按 Facts 的 Start/End —— Count 与 List 是两次独立事务，并发下行数会少于期望值。

import (
	"fmt"
	"strings"
	"testing"

	"go_wp/pkg/pagination"
)

// b2bCase 一个片段形态：total/page/size 来自 service 的响应，rows 是本页实际行数。
type b2bCase struct {
	name    string
	total   int
	page    int
	size    int
	rows    int
	project string
}

func b2bCases() []b2bCase {
	return []b2bCase{
		{name: "第二页满页", total: 45, page: 2, size: 20, rows: 20, project: "p1"},
		{name: "并发下本页行数变少", total: 45, page: 2, size: 20, rows: 3, project: "p1"},
		{name: "单页非空仍渲染nav", total: 5, page: 1, size: 20, rows: 5, project: "p1"},
		{name: "末页无下一页", total: 45, page: 3, size: 20, rows: 5, project: "p1"},
		{name: "空工程参数仍回带", total: 45, page: 2, size: 20, rows: 20, project: ""},
		{name: "本页无行", total: 45, page: 2, size: 20, rows: 0, project: "p1"},
	}
}

// b2bRows 造本页行（内容与分页无关，但模板会渲染表格）。
func b2bRows(n int) []map[string]any {
	rows := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, map[string]any{
			"ID": "r", "Name": "商品", "Slug": "sku", "Status": "on",
		})
	}
	return rows
}

// b2bOldPageInfo 复刻迁移前 tagHitsPageInfo 的成品串：区间按**实际行数**算，词条走
// shell.pagination.info 的 zh 原文（测试进程 i18n 未初始化，TranslateFunc 落回下文兜底）。
func b2bOldPageInfo(total, page, size, rows int) string {
	from, to := 0, 0
	if rows > 0 {
		from = (page-1)*size + 1
		to = from + rows - 1
	}
	return fmt.Sprintf("共 %s 条，第 %s-%s 条", itoaB2b(total), itoaB2b(from), itoaB2b(to))
}

// b2bOldTagHitsURL 复刻迁移前 tagHitsURL：url.Values{project,id,page}.Encode() → 键序 id<page<project。
func b2bOldTagHitsURL(projectID string, tagID string, page int) string {
	return "/admin/product-tags/hits?id=" + tagID + "&page=" + itoaB2b(page) + "&project=" + projectID
}

func b2bOldData(t *testing.T, c b2bCase) map[string]any {
	t.Helper()
	rows := b2bRows(c.rows)
	data := adminShellData()
	data["TagID"] = "t1"
	data["ProjectID"] = c.project
	data["TagName"] = "标签"
	data["Total"] = c.total
	data["Page"] = c.page
	data["PageSize"] = c.size
	data["Items"] = rows
	data["PageInfo"] = b2bOldPageInfo(c.total, c.page, c.size, len(rows))
	totalPage := (c.total + c.size - 1) / c.size
	if totalPage < 1 {
		totalPage = 1
	}
	if c.page > 1 {
		data["PrevURL"] = b2bOldTagHitsURL(c.project, "t1", c.page-1)
	}
	if c.page < totalPage {
		data["NextURL"] = b2bOldTagHitsURL(c.project, "t1", c.page+1)
	}
	return data
}

// b2bNewData 迁移后 handler 注入的事实键（Items 仍照旧在场）。
func b2bNewData(t *testing.T, c b2bCase) map[string]any {
	t.Helper()
	data := adminShellData()
	data["TagID"] = "t1"
	data["ProjectID"] = c.project
	data["TagName"] = "标签"
	data["Total"] = c.total
	data["Items"] = b2bRows(c.rows)
	pg := pagination.Facts(int64(c.total), c.page, c.size)
	pg.Base = "/admin/product-tags/hits"
	pg.PageParam = "page"
	pg.KeepLimit = false
	data["Pagination"] = pg
	return data
}

func b2bRender(t *testing.T, data map[string]any) string {
	t.Helper()
	out, err := render(t, newAdminTestSet(), "admin/product/product_tag_hits.html", data)
	if err != nil {
		t.Fatalf("渲染命中商品片段失败: %v", err)
	}
	return out
}

func itoaB2b(n int) string { return fmt.Sprintf("%d", n) }

// b2bFrozenLegacyNav 迁移前的 nav 段**逐字副本**（改模板前从 product_tag_hits.html 抄下来）。
//
// 为什么把旧标记整段抄进测试而不是只抄输出字节：输出字节依赖数据形态，抄进测试后要重新推导
// 一遍旧模板的渲染规则（等于自己证明自己）；而这段标记 + 旧数据形态 = 真实的旧渲染路径，
// 与新模板在新数据形态下的输出做 nav 段对比，才是「只换了取数形态、没换展示」的直接证据。
const b2bFrozenLegacyNav = `    <nav class="pagination" aria-label="{{ .["t"]("shell.pagination.label", "分页") }}">
        <div class="pagination-info">{{.PageInfo}}</div>
        <div class="pagination-pages">
            {{if isset(.PrevURL)}}<button type="button" class="pagination-btn" hx-get="{{.PrevURL}}" hx-target="#tag-hits-panel" hx-swap="innerHTML">{{ .["t"]("shell.pagination.prev", "上一页") }}</button>{{else}}<span class="pagination-btn is-disabled">{{ .["t"]("shell.pagination.prev", "上一页") }}</span>{{end}}
            {{if isset(.NextURL)}}<button type="button" class="pagination-btn" hx-get="{{.NextURL}}" hx-target="#tag-hits-panel" hx-swap="innerHTML">{{ .["t"]("shell.pagination.next", "下一页") }}</button>{{else}}<span class="pagination-btn is-disabled">{{ .["t"]("shell.pagination.next", "下一页") }}</span>{{end}}
        </div>
    </nav>`

// b2bNavSection 取出输出里的 nav 段（分页条是本次改动唯一会变的字节）。
func b2bNavSection(s string) string {
	i := strings.Index(s, `<nav class="pagination"`)
	if i < 0 {
		return ""
	}
	j := strings.Index(s[i:], "</nav>")
	if j < 0 {
		return ""
	}
	return s[i : i+j+len("</nav>")]
}

// b2bLegacyNav 用冻结的旧标记 + 旧数据形态渲染出 nav 段。
func b2bLegacyNav(t *testing.T, c b2bCase) string {
	t.Helper()
	out, err := p0plusRenderData(t, b2bFrozenLegacyNav, b2bOldData(t, c))
	if err != nil {
		t.Fatalf("渲染冻结的旧 nav 失败: %v", err)
	}
	return b2bNavSection(out)
}

// TestB2bNavBytesMatchFrozenLegacy 六个形态下 nav 段与迁移前逐字节相同。
//
// 「本页无行」不在本对照里：冻结的旧标记**只有 nav 段**，而「有没有行」的闸门写在它外面
// （`{{if len(.Items) == 0}}` 走提示分支）—— 片段单独渲染时永远会出 nav，比不出东西。
// 那一档由 TestB2bNoRowsRendersHintNotNav 正面断言。
func TestB2bNavBytesMatchFrozenLegacy(t *testing.T) {
	for _, c := range b2bCases() {
		if c.rows == 0 {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			want := b2bLegacyNav(t, c)
			got := b2bNavSection(b2bRender(t, b2bNewData(t, c)))
			if want != got {
				t.Errorf("nav 段与迁移前不一致：%s", p2FirstDiff(want, got))
			}
		})
	}
}

// TestB2bInfoRangeFollowsActualRowCount info 区间按本页**实际行数**算，不按 Facts 的 End。
//
// Sol 设计审 P2：Count 与 List 无同一快照，total=45/page=2 时本页可能只有 3 行 ——
// 旧句子是「第 21-23 条」；若改用 Facts 的 Start/End 会变成「第 21-40 条」，那是**新的谎话**。
func TestB2bInfoRangeFollowsActualRowCount(t *testing.T) {
	c := b2bCase{name: "并发下本页行数变少", total: 45, page: 2, size: 20, rows: 3, project: "p1"}
	out := b2bRender(t, b2bNewData(t, c))
	if !strings.Contains(out, "共 45 条，第 21-23 条") {
		t.Fatalf("info 行未按实际行数计算：%s", b2bNavSection(out))
	}
	if strings.Contains(out, "第 21-40 条") {
		t.Fatal("info 行用了 Facts 的 End（21-40）—— 这正是设计审要求避免的谎话")
	}
}

// TestB2bSinglePageNonEmptyStillRendersNav 单页非空**仍要**渲染 nav（两个禁用按钮）。
//
// Sol 更正 ③：这里不能套 HasPagination() 把 nav 挡掉 —— 那会把「只有一页」变成「没有分页信息」。
func TestB2bSinglePageNonEmptyStillRendersNav(t *testing.T) {
	c := b2bCase{name: "单页非空仍渲染nav", total: 5, page: 1, size: 20, rows: 5, project: "p1"}
	nav := b2bNavSection(b2bRender(t, b2bNewData(t, c)))
	if nav == "" {
		t.Fatal("单页非空时 nav 没渲染")
	}
	if got := strings.Count(nav, `class="pagination-btn is-disabled"`); got != 2 {
		t.Fatalf("禁用按钮 = %d 个，期望 2 个（上一页 / 下一页）：%s", got, nav)
	}
	if !strings.Contains(nav, "共 5 条，第 1-5 条") {
		t.Fatalf("info 行不对：%s", nav)
	}
	// 反面：若有人给这里加上 HasPagination 闸门，本用例立刻红（Pages=1）。
	if pagination.Facts(5, 1, 20).HasPagination() {
		t.Fatal("对照值不成立：total=5/size=20 不应有分页（证明本用例确实在挡 HasPagination 闸门）")
	}
}

// TestB2bKeepsHtmxAttributes 按钮仍是 hx-get 打回本片段（Sol 更正 ④/钥匙守则：不许换 <a href>）。
func TestB2bKeepsHtmxAttributes(t *testing.T) {
	c := b2bCase{name: "第二页满页", total: 45, page: 2, size: 20, rows: 20, project: "p1"}
	nav := b2bNavSection(b2bRender(t, b2bNewData(t, c)))
	// 属性里的 & 由 Jet 转义一次（与旧 {{.PrevURL}} 插值同形）——判据是**实际字节**。
	for _, want := range []string{
		`hx-get="/admin/product-tags/hits?id=t1&amp;page=1&amp;project=p1"`,
		`hx-get="/admin/product-tags/hits?id=t1&amp;page=3&amp;project=p1"`,
		`hx-target="#tag-hits-panel"`,
		`hx-swap="innerHTML"`,
	} {
		if !strings.Contains(nav, want) {
			t.Errorf("翻页按钮丢了 %s：%s", want, nav)
		}
	}
	if strings.Contains(nav, `<a class="pagination-btn"`) {
		t.Fatal("nav 里出现了 <a href> 形态的分页链接：本片段必须走 hx-get")
	}
}

// TestB2bEmptyProjectKeepsParam 空 project 仍要回带成 `project=`（旧 url.Values.Encode() 同形）。
func TestB2bEmptyProjectKeepsParam(t *testing.T) {
	c := b2bCase{name: "空工程参数仍回带", total: 45, page: 2, size: 20, rows: 20, project: ""}
	nav := b2bNavSection(b2bRender(t, b2bNewData(t, c)))
	if !strings.Contains(nav, `hx-get="/admin/product-tags/hits?id=t1&amp;page=1&amp;project="`) {
		t.Fatalf("空 project 未回带成 project=：%s", nav)
	}
}

// TestB2bNoRowsRendersHintNotNav 本页无行时只有提示，没有分页条。
func TestB2bNoRowsRendersHintNotNav(t *testing.T) {
	c := b2bCase{name: "本页无行", total: 45, page: 2, size: 20, rows: 0, project: "p1"}
	out := b2bRender(t, b2bNewData(t, c))
	if b2bNavSection(out) != "" {
		t.Fatalf("无行时不该渲染分页条：%s", b2bNavSection(out))
	}
	if !strings.Contains(out, "当前没有商品命中这个标签") {
		t.Fatalf("无行时缺少提示：%s", out)
	}
}
