// Package feature 库存域三个列表页的分页（审计 D3）。
//
// 三页原先都是「handler 里写死上限 + 模板里没有分页条」：库存流水 50 条、货源 200 条、
// 采购单 100 条，第 N+1 条起**静默消失**（流水按时间倒序，第 51 条之后的老记录永远看不到），
// 页面上连「还有更多」都不说。
//
// 这里钉住三条判据，缺一条就会漏掉一类缺陷：
//
//	· 超过一页时**真的出现分页条**，且信息行给出**真源总数**（「共 N 条，第 X-Y 条」）；
//	· 翻页链接**带着当前筛选条件**（丢条件时记录会「变多」，看着像数据错乱）；
//	· 单页与**装载失败降级渲染**两条路都**不渲染分页条**（页壳仍完好，判据见
//	  inventory_page_load_failed_test.go）。
//
// 分页条的数据来自契约的 CountMovements / CountSources / CountPurchaseOrders（真源总数，
// 与 List 同一份过滤条件）—— 上一轮那版「实探第 page+1 页 + 只给上一页 / 下一页 +
// 信息行写『后面还有记录』」的降级形态已随契约补齐删除，故本文件对信息行的断言
// 从「第 N 页 · 每页 M 条」换成真源分页的「共 N 条，第 X-Y 条」。
//
// 每条断言都从**渲染出来的 HTML**上取（而不是重算一份期望值）：分页条由
// admin/partials/pagination.html 渲染，handler 注入的键名 / 链接形状与它不一致时，
// 手写的期望值会照样通过。
package feature

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	inventorydto "go_wp/internal/module/inventory/dto"
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

// TestInventoryMovementsPagePagination 流水页：超过一页出现分页条，翻到第 2 页能看到后面的记录。
func TestInventoryMovementsPagePagination(t *testing.T) {
	engine, f := newInventoryPageEngine(t)
	if engine == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "深圳仓", true)
	p := mustProduct(t, f, "流水分页商品")
	v := f.firstVariant(t, p.ID)
	// 三条流水：每页 2 条 ⇒ 第 1 页 2 行、第 2 页 1 行。
	changeIn(t, f, p, v, wh.ID, 1, "purchase_in")
	changeIn(t, f, p, v, wh.ID, 2, "purchase_in")
	changeIn(t, f, p, v, wh.ID, 3, "purchase_in")
	sku := bareSKU(v.SKUCode, "SZ")

	first := httptestGet(engine, "/admin/inventory?project="+f.projectID+"&limit=2")
	if first.Code != http.StatusOK {
		t.Fatalf("流水页应 200，实际 %d", first.Code)
	}
	body := first.Body.String()
	if !strings.Contains(body, paginationBar) {
		t.Fatalf("流水超过一页时应出现分页条（审计 D3：这一页原先没有分页条）")
	}
	// 真源分页的信息行：总数来自 CountMovements（不是「本页条数」也不是探测结果）——
	// 3 条流水、每页 2 条 ⇒「共 3 条，第 1-2 条」。
	if !strings.Contains(body, "共 3 条，第 1-2 条") {
		t.Fatalf("分页信息行应给出真源总数与区间（共 3 条，第 1-2 条）")
	}
	if got := strings.Count(body, "<code>"+sku+"</code>"); got != 2 {
		t.Fatalf("第 1 页应渲染 2 条流水，实际 %d 条", got)
	}
	paginationLinkLine(t, body, 2)

	// 第 2 页：剩 1 条，信息行给出末页区间。
	second := httptestGet(engine, "/admin/inventory?project="+f.projectID+"&limit=2&page=2")
	if second.Code != http.StatusOK {
		t.Fatalf("流水第 2 页应 200，实际 %d", second.Code)
	}
	body = second.Body.String()
	if got := strings.Count(body, "<code>"+sku+"</code>"); got != 1 {
		t.Fatalf("第 2 页应渲染 1 条流水，实际 %d 条（翻页没换数据？）", got)
	}
	if !strings.Contains(body, "共 3 条，第 3-3 条") {
		t.Fatalf("末页的信息行应给出末页区间（共 3 条，第 3-3 条）")
	}

	// 单页（每页 10 条）：不渲染分页条。
	single := httptestGet(engine, "/admin/inventory?project="+f.projectID+"&limit=10")
	if strings.Contains(single.Body.String(), paginationBar) {
		t.Fatalf("单页不应渲染分页条")
	}
}

// TestInventoryMovementsDefaultPageSize 缺省每页条数就是本页常量（流水 50 条）。
//
// 改动前写死的就是 50 条一页（只是没有分页条），这里钉住「补分页」没有顺手改掉默认页大小。
// 判据是**端到端可观测的**：51 条流水在缺省视图下应正好两页（第 1 页 50 行、第 2 页 1 行）——
// 缺省值若被改成别的数（service 的 20、或更小），页数、行数与信息行都会立刻不对。
func TestInventoryMovementsDefaultPageSize(t *testing.T) {
	engine, f := newInventoryPageEngine(t)
	if engine == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "深圳仓", true)
	p := mustProduct(t, f, "缺省页大小商品")
	v := f.firstVariant(t, p.ID)
	for i := 1; i <= 51; i++ {
		changeIn(t, f, p, v, wh.ID, 1, "purchase_in")
	}
	sku := bareSKU(v.SKUCode, "SZ")

	first := httptestGet(engine, "/admin/inventory?project="+f.projectID)
	if first.Code != http.StatusOK {
		t.Fatalf("流水页应 200，实际 %d", first.Code)
	}
	body := first.Body.String()
	if got := strings.Count(body, "<code>"+sku+"</code>"); got != 50 {
		t.Fatalf("缺省每页 50 条：第 1 页应渲染 50 行，实际 %d 行", got)
	}
	if !strings.Contains(body, "共 51 条，第 1-50 条") {
		t.Fatalf("分页信息行应为「共 51 条，第 1-50 条」")
	}

	second := httptestGet(engine, "/admin/inventory?project="+f.projectID+"&page=2")
	if got := strings.Count(second.Body.String(), "<code>"+sku+"</code>"); got != 1 {
		t.Fatalf("第 2 页应渲染 1 行（共 51 条、每页 50），实际 %d 行", got)
	}
	if !strings.Contains(second.Body.String(), "共 51 条，第 51-51 条") {
		t.Fatalf("第 2 页的信息行应为「共 51 条，第 51-51 条」")
	}
}

// TestInventoryMovementsPaginationKeepsFilters 翻页链接保留流水的五个筛选维度。
func TestInventoryMovementsPaginationKeepsFilters(t *testing.T) {
	engine, f := newInventoryPageEngine(t)
	if engine == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "深圳仓", true)
	p := mustProduct(t, f, "流水筛选商品")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 1, "purchase_in")
	changeIn(t, f, p, v, wh.ID, 2, "purchase_in")
	sku := bareSKU(v.SKUCode, "SZ")

	rec := httptestGet(engine, "/admin/inventory?project="+f.projectID+
		"&limit=1&direction=in&sku="+sku+"&warehouseId="+wh.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("流水页应 200，实际 %d", rec.Code)
	}
	line := paginationLinkLine(t, rec.Body.String(), 2)
	for _, want := range []string{"direction=in", "sku=" + sku, "warehouseId=" + wh.ID} {
		if !strings.Contains(line, want) {
			t.Fatalf("翻页链接丢了筛选条件 %s：%s", want, line)
		}
	}
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
	mustPurchaseOrder(t, f, "PO-PAGE-1", src.ID, wh.ID, []inventorydto.PurchaseLineReq{purchaseLine(p, v, 1, 10)})
	mustPurchaseOrder(t, f, "PO-PAGE-2", src.ID, wh.ID, []inventorydto.PurchaseLineReq{purchaseLine(p, v, 2, 10)})

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

// TestInventoryMovementCountMatchesListedRows 流水页的 Count 与 List 在同一组筛选条件下口径一致。
//
// 这是真源分页的**根本判据**：「共 N 条」来自契约的 CountMovements，而实际能翻出来的行数来自
// ListMovements —— 两处口径一旦分叉（某个筛选维度只加在一边），页面就会给出一个永远翻不到底
// 的页数，而两边都不会报错。流水的筛选维度最多，且 model 层曾经就漏过其中四个
// （商品 / 来源类型 / 来源引用 / 时间区间），所以这里按维度逐个筛一遍，把「逐页行数之和」
// 与「信息行给出的总数」对上。
func TestInventoryMovementCountMatchesListedRows(t *testing.T) {
	engine, f := newInventoryPageEngine(t)
	if engine == nil {
		return
	}
	wh := f.createWarehouse(t, "SZ", "深圳仓", true)
	p := mustProduct(t, f, "计口径一致商品")
	v := f.firstVariant(t, p.ID)
	changeIn(t, f, p, v, wh.ID, 1, "purchase_in")
	changeIn(t, f, p, v, wh.ID, 2, "purchase_in")
	changeIn(t, f, p, v, wh.ID, 3, "purchase_in")
	sku := bareSKU(v.SKUCode, "SZ")
	today := time.Now().Format("2006-01-02")

	cases := []struct {
		name  string
		query string
		want  int
	}{
		{"无筛选", "", 3},
		{"按仓库", "&warehouseId=" + wh.ID, 3},
		{"按 SKU", "&sku=" + sku, 3},
		{"按方向", "&direction=in", 3},
		{"按原因", "&reasonCode=purchase_in", 3},
		{"按时间区间（今天起）", "&timeFrom=" + today, 3},
		{"时间区间落在未来应一条不剩", "&timeFrom=2100-01-01", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base := "/admin/inventory?project=" + f.projectID + "&limit=2" + c.query
			got := 0
			// 逐页数**流水行**：判据是流水行独有的增减徽章 class（move-delta-*）——
			// 不能用 SKU 编码计数：同一个 SKU 还出现在筛选项与「各仓库存」表里，
			// 一行流水会被数成三行（实测踩过：按 SKU 筛选时逐页合计 61）。
			for page := 1; page <= 20; page++ {
				rec := httptestGet(engine, base+"&page="+strconv.Itoa(page))
				if rec.Code != http.StatusOK {
					t.Fatalf("第 %d 页应 200，实际 %d", page, rec.Code)
				}
				n := strings.Count(rec.Body.String(), "move-delta-")
				got += n
				// 满页才可能还有下一页；不满即到底。
				//（越界页会收敛到末页，所以「取到空页」这个终止条件永远不成立 ——
				//  实测踩过：按行数判断之外的写法会一直翻到循环上限。）
				if n < 2 {
					break
				}
			}
			if got != c.want {
				t.Fatalf("逐页行数合计 %d，期望 %d（该筛选条件下真实命中数）", got, c.want)
			}
			// 总数与逐页行数之和必须相等：多于一页时信息行才有，正好用它读总数。
			if c.want > 2 {
				body := httptestGet(engine, base).Body.String()
				if !strings.Contains(body, fmt.Sprintf("共 %d 条", c.want)) {
					t.Fatalf("信息行的总数应与实际能翻出来的条数一致（共 %d 条）", c.want)
				}
			}
		})
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

// TestInventoryPaginationI18nSeeded 分页条真正用到的那几个词条中英各一行真的落库。
//
// 真源分页把信息行与翻页按钮统一到 shell 的公共词条（迁移 059 登记）：
// shell.pagination.info（「共 %s 条，第 %s-%s 条」）/ .prev / .next。
// 模板与 Go 里的中文只是 t() 兜底，词条命中时显示的是库里的值 —— 一条「只写了兜底、
// 忘了迁移」的文案位在中文环境里看起来完全正常，只有英文站点才暴露（回落中文）。
//
// 上一轮的降级分页条自带 3 个本域词条（迁移 412 的 admin.inventory.pagination.info /
// .more / .end），它们随降级形态一起退出了使用路径 —— 已由迁移 419 从库里删除，
// 412 的 seed 幂等条件也同批改成「这 3 个 key 一行都不剩则跳过」（不再插回）。
// 本用例因此钉**现在真正生效**的那几个词条。
func TestInventoryPaginationI18nSeeded(t *testing.T) {
	f := newInvFixture(t)
	if f == nil {
		return
	}
	keys := []string{
		"shell.pagination.info",
		"shell.pagination.prev",
		"shell.pagination.next",
	}
	for _, lang := range []string{"zh-CN", "en-US"} {
		var n int64
		if err := f.db.Raw("SELECT COUNT(*) FROM sys_i18n WHERE lang = ? AND item_key IN (?)",
			lang, keys).Scan(&n).Error; err != nil {
			t.Fatalf("查 %s 词条失败: %v", lang, err)
		}
		if n != int64(len(keys)) {
			t.Errorf("%s 词条 %d 条，期望 %d 条（缺的那条在页面上会显示裸 key 或回落另一种语言）",
				lang, n, len(keys))
		}
	}
}
