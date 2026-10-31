package feature

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	purchasedto "go_wp/internal/module/purchase/dto"
)

const paginationBar = `class="pagination"`

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

	noLimit := httptestGet(engine, "/admin/inventory/sources?project="+f.projectID+"&page=2")
	if strings.Contains(noLimit.Body.String(), paginationBar) {
		t.Fatalf("缺省每页 200 条时 3 个货源只占一页，不应渲染分页条")
	}
	if got := strings.Count(noLimit.Body.String(), "<code>PAGE_SRC_"); got != 3 {
		t.Fatalf("越界页码应收敛到末页（3 个货源全在），实际 %d 个", got)
	}
}

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
		"&limit=1&status=ordered&keyword=PO-PAGE")
	if rec.Code != http.StatusOK {
		t.Fatalf("采购入库页应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, paginationBar) {
		t.Fatalf("采购单超过一页时应出现分页条（审计 D3：这一页原先没有分页条）")
	}

	if !strings.Contains(body, "共 2 条，第 1-1 条") {
		t.Fatalf("分页信息行应给出真源总数与区间（共 2 条，第 1-1 条）")
	}
	if got := strings.Count(body, "<code>PO-PAGE-"); got != 1 {
		t.Fatalf("第 1 页应渲染 1 张采购单，实际 %d 张", got)
	}
	line := paginationLinkLine(t, body, 2)
	for _, want := range []string{"status=ordered", "keyword=PO-PAGE"} {
		if !strings.Contains(line, want) {
			t.Fatalf("翻页链接丢了筛选条件 %s：%s", want, line)
		}
	}

	noLimit := httptestGet(engine, "/admin/inventory/purchases?project="+f.projectID+"&page=2")
	if strings.Contains(noLimit.Body.String(), paginationBar) {
		t.Fatalf("缺省每页 100 张时 2 张单只占一页，不应渲染分页条")
	}
	if got := strings.Count(noLimit.Body.String(), "<code>PO-PAGE-"); got != 2 {
		t.Fatalf("越界页码应收敛到末页（2 张单全在），实际 %d 张", got)
	}
}

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
