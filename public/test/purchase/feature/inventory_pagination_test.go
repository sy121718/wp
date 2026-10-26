// Package feature 货源 / 采购入库两页的分页（审计 D3；2026-10 采购域拆成独立模块时迁入）。
//
// 两页原先都是「handler 里写死上限 + 模板里没有分页条」：货源 200 条、采购单 100 条，
// 第 N+1 条起**静默消失**，页面上连「还有更多」都不说。这里钉住三条判据：
//
//	· 超过一页时**真的出现分页条**，且信息行给出**真源总数**（「共 N 条，第 X-Y 条」）；
//	· 翻页链接**带着当前筛选条件**（丢条件时记录会「变多」，看着像数据错乱）；
//	· 单页**不渲染分页条**（页壳仍完好；装载失败的降级渲染见 inventory_page_load_failed_test.go）。
//
// 每条断言都从**渲染出来的 HTML**上取（而不是重算一份期望值）：分页条由
// admin/partials/pagination.html 渲染，handler 注入的键名 / 链接形状与它不一致时，
// 手写的期望值会照样通过。
package feature

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	purchasedto "go_wp/internal/module/purchase/dto"
)

// paginationBar 分页条在页面里的稳定标记（partials/pagination.html 的 <nav class="pagination">）。
const paginationBar = `class="pagination"`

// paginationLinkLine 取分页条里指向第 page 页的那一行链接。
//
// 判据必须落在**那一行**上，不能在整个响应体里各查一次：筛选栏的表单里也带着同样的
// 参数（value=... / selected），「链接丢了条件、但筛选栏还留着条件」时，全页级的两次
// Contains 会照常通过 —— 那正是这个用例要挡的缺陷。
func paginationLinkLine(t *testing.T, body string, page int) string {
	t.Helper()
	needle := "page=" + strconv.Itoa(page)
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, needle) && strings.Contains(line, "<a ") {
			return line
		}
	}
	t.Fatalf("分页条里没有指向第 %d 页的链接（分页条没渲染？还是链接里没有 page=%d？）", page, page)
	return ""
}

// TestInventorySourcesPagePagination 货源页：超过一页出现分页条，且翻页链接保留筛选。
func TestInventorySourcesPagePagination(t *testing.T) {
	engine, f := newSourcePageEngine(t)
	if engine == nil {
		return
	}
	for _, code := range []string{"PAGE_SRC_A", "PAGE_SRC_B", "PAGE_SRC_C"} {
		mustCreateSource(t, f, code, "分页货源 "+code, "internal", nil, floatPtr(1.5))
	}

	rec := httptestGet(engine, "/admin/inventory/sources?project="+f.projectID+
		"&limit=2&type=internal&keyword=PAGE_SRC")
	if rec.Code != http.StatusOK {
		t.Fatalf("货源页应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, paginationBar) {
		t.Fatalf("货源超过一页时应出现分页条（审计 D3：这一页原先没有分页条）")
	}
	// 真源总数来自 CountSources（与 ListSources 同一份过滤条件，含「缺省只列启用中」那一档）。
	if !strings.Contains(body, "共 3 条，第 1-2 条") {
		t.Fatalf("分页信息行应给出真源总数与区间（共 3 条，第 1-2 条）")
	}
	if got := strings.Count(body, "<code>PAGE_SRC_"); got != 2 {
		t.Fatalf("第 1 页应渲染 2 个货源，实际 %d 个", got)
	}
	line := paginationLinkLine(t, body, 2)
	for _, want := range []string{"type=internal", "keyword=PAGE_SRC"} {
		if !strings.Contains(line, want) {
			t.Fatalf("翻页链接丢了筛选条件 %s：%s", want, line)
		}
	}

	// 缺省每页条数就是本页常量（货源 200）：这 3 个货源在缺省视图下只占一页 ——
	// 连 page=2 都会被收敛回第 1 页（不会出现「表格空、分页条显示第 2 页」）。
	// 缺省值若被改成小值（例如按 service 的 50），这里会立刻出现分页条。
	noLimit := httptestGet(engine, "/admin/inventory/sources?project="+f.projectID+"&page=2")
	if strings.Contains(noLimit.Body.String(), paginationBar) {
		t.Fatalf("缺省每页 200 条时 3 个货源只占一页，不应渲染分页条")
	}
	if got := strings.Count(noLimit.Body.String(), "<code>PAGE_SRC_"); got != 3 {
		t.Fatalf("越界页码应收敛到末页（3 个货源全在），实际 %d 个", got)
	}
}

// TestInventoryPurchasesPagePagination 采购入库页：超过一页出现分页条，且翻页链接保留筛选。
func TestInventoryPurchasesPagePagination(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "深圳仓", true)
	src := mustPurchaseSource(t, f, "PAGE_PO_SRC", "分页采购货源", "external")
	p := mustProductPriced(t, f, "采购分页商品", 20)
	v := f.firstVariant(t, p.ID)
	mustPurchaseOrder(t, f, "PO-PAGE-1", src.ID, wh.ID, []purchasedto.PurchaseLineReq{purchaseLine(p, v, 1, 10)})
	mustPurchaseOrder(t, f, "PO-PAGE-2", src.ID, wh.ID, []purchasedto.PurchaseLineReq{purchaseLine(p, v, 2, 10)})

	rec := httptestGet(engine, "/admin/inventory/purchases?project="+f.projectID+
		"&limit=1&status=pending&keyword=PO-PAGE")
	if rec.Code != http.StatusOK {
		t.Fatalf("采购入库页应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, paginationBar) {
		t.Fatalf("采购单超过一页时应出现分页条（审计 D3：这一页原先没有分页条）")
	}
	// 真源总数来自 CountPurchaseOrders（与 ListPurchaseOrders 同一份过滤条件，
	// 含「状态推导值」那一维的归一）。
	if !strings.Contains(body, "共 2 条，第 1-1 条") {
		t.Fatalf("分页信息行应给出真源总数与区间（共 2 条，第 1-1 条）")
	}
	if got := strings.Count(body, "<code>PO-PAGE-"); got != 1 {
		t.Fatalf("第 1 页应渲染 1 张采购单，实际 %d 张", got)
	}
	line := paginationLinkLine(t, body, 2)
	for _, want := range []string{"status=pending", "keyword=PO-PAGE"} {
		if !strings.Contains(line, want) {
			t.Fatalf("翻页链接丢了筛选条件 %s：%s", want, line)
		}
	}

	// 缺省每页条数就是本页常量（采购单 100）：这 2 张单在缺省视图下只占一页 ——
	// 连 page=2 都会被收敛回第 1 页。缺省值若被改成小值，这里会立刻出现分页条。
	noLimit := httptestGet(engine, "/admin/inventory/purchases?project="+f.projectID+"&page=2")
	if strings.Contains(noLimit.Body.String(), paginationBar) {
		t.Fatalf("缺省每页 100 张时 2 张单只占一页，不应渲染分页条")
	}
	if got := strings.Count(noLimit.Body.String(), "<code>PO-PAGE-"); got != 2 {
		t.Fatalf("越界页码应收敛到末页（2 张单全在），实际 %d 张", got)
	}
}

// TestInventoryPaginationHiddenWhenLoadFailed 装载失败降级渲染时不渲染分页条。
//
// 降级渲染（HTTP 200 + 页壳完好 + 归口提示）由 inventory_page_load_failed_test.go 钉住；
// 这里补的是它与分页的交叉点：工程列表都没读出来时，URL 上的 page 不该被当成有效页码
// 渲染出「上一页」—— 那会给人「数据只是被翻掉了」的错觉。
func TestInventoryPaginationHiddenWhenLoadFailed(t *testing.T) {
	engine, f := newSourcePageEngine(t)
	if engine == nil {
		return
	}
	breakProjectsTable(t, f)

	rec := httptestGet(engine, "/admin/inventory/sources?page=3&limit=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败应降级渲染（200），实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("降级渲染没有渲染完整页（缺 </html>）：模板中途中断了")
	}
	if !strings.Contains(body, "这一页的数据没能读出来") {
		t.Fatalf("空态没走装载失败档（判据 LoadFailed 没传给模板）")
	}
	if strings.Contains(body, paginationBar) {
		t.Fatalf("装载失败时不应渲染分页条")
	}
}
