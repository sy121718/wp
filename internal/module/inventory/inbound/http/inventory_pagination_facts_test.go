package inventoryhttp

// inventory_pagination_facts_test.go — task-117（P3-B3b）：inventory 模块三个整页分页
// 站点的证据。
//
// 分两层：
//   · 库存流水（/admin/inventory）走**真 HTTP**：从入口打到渲染字节，钉住分页条出现、
//     链接带本次全部筛选 + limit、信息行页码窗口正确。
//   · 采购单页与货源页的 handler 依赖一整套契约方法（列 + 计数 + 汇总 + 仓库/变体选项），
//     本文件改为在**数据映射层**钉住（templateMap 把 pagination.Page 事实交给模板、
//     旧键一个不留）—— 加上模板层逐字节差分与「三个模块零旧分页残留」的源码扫描，
//     链路（handler → 数据结构 → 模板键 → partial）每一段都有独立证据。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	"go_wp/pkg/i18n"
	"go_wp/internal/templates"
	"go_wp/pkg/pagination"
)

// paginationNavOf 取出页面里的分页条片段（没渲染出来时返回空串）。
func paginationNavOf(body string) string {
	start := strings.Index(body, `<nav class="pagination"`)
	if start < 0 {
		return ""
	}
	rest := body[start:]
	end := strings.Index(rest, "</nav>")
	if end < 0 {
		return ""
	}
	return rest[:end+len("</nav>")]
}

// b3bInventoryFactsStub 在渲染测试的桩上覆写计数：只把**总数**抬到多页，
// 其余方法仍走父桩（未用到的契约方法继续由嵌入的 nil 接口兜底）。
type b3bInventoryFactsStub struct {
	*inventoryPageRenderStub
	movementsTotal int64
}

func (s *b3bInventoryFactsStub) CountMovements(context.Context, *inventorydto.ListMovementReq) (int64, error) {
	return s.movementsTotal, nil
}

// TestInventoryMovementsPaginationFactsWiring 库存流水：total=45 ⇒ 三页；链接保留
// project / sku 两个筛选维度，并带上 limit。
func TestInventoryMovementsPaginationFactsWiring(t *testing.T) {
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	// 库存流水的默认每页条数是 inventoryMovementPageSize（50，不是 20）：
	// 要造出多页必须让总数越过分页阈值，否则 Facts 判单页、片段不渲染。
	stub := &b3bInventoryFactsStub{inventoryPageRenderStub: &inventoryPageRenderStub{}, movementsTotal: 120}
	router := newInventoryPageTestRouter(t, stub, &inventoryPageRenderProjects{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/inventory?project=project-1&sku=SKU-1&page=2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("库存管理页返回 %d：%s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	nav := paginationNavOf(body)
	if nav == "" {
		slot := "未渲染失败槽"
		if strings.Contains(body, "这一页的数据没能读出来") {
			slot = "渲染了失败槽（装载失败）"
		}
		t.Fatalf("三页数据却没渲染分页条（%s）—— handler 很可能没有把 pagination.Page 事实交给模板", slot)
	}
	for _, want := range []string{"project=project-1", "sku=SKU-1", "limit=50", "page=1", "page=3",
		// 第 2 页是当前页：渲染成 is-active 的 span（无 href）。
		`is-active">2</span>`,
		// 信息行由 partial 用事实现算：total / start / end 都对才有这一句。
		"共 120 条，第 51-100 条"} {
		if !strings.Contains(nav, want) {
			t.Errorf("分页条缺少 %q：\n%s", want, nav)
		}
	}
}

// inventoryFactsMapper 造一个最小可用的 gin 上下文（templateMap 只读 query 与请求上下文）。
func inventoryFactsMapper(t *testing.T, target string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return c, rec
}

// TestInventoryPurchasesMapperCarriesPaginationFacts 采购单页：映射层必须交出事实键，
// 且不得再出现旧成品键。
func TestInventoryPurchasesMapperCarriesPaginationFacts(t *testing.T) {
	c, _ := inventoryFactsMapper(t, "/admin/inventory/purchases?project=project-1")

	pg := pagination.Page{Total: 45, Page: 2, Limit: 20}
	pg.Base = "/admin/inventory/purchases?project=project-1"
	m := (&inventoryPurchasesPageData{
		SelectedProject: "project-1",
		Pagination:      pg,
	}).templateMap(c)

	got, ok := m["Pagination"].(pagination.Page)
	if !ok {
		t.Fatalf("模板键 Pagination 不是 pagination.Page 事实：%T", m["Pagination"])
	}
	if got != pg {
		t.Errorf("交出的事实与 handler 构造的不一致：%+v", got)
	}
	if !templates.PagerOf(got).HasPagination() || got.Base != pg.Base {
		t.Errorf("事实应带基址且为多页：%+v", got)
	}
	for _, legacy := range []string{"PaginationInfo", "PaginationLinks"} {
		if v, exists := m[legacy]; exists {
			t.Errorf("旧成品键 %s 仍在：%v", legacy, v)
		}
	}
}

// TestInventorySourcesMapperCarriesPaginationFacts 货源页：同上（该页 mapper 额外读
// 请求 query 拼写动作回跳上下文，所以这里用真 context）。
func TestInventorySourcesMapperCarriesPaginationFacts(t *testing.T) {
	c, _ := inventoryFactsMapper(t, "/admin/inventory/sources?project=project-1&type=internal")

	pg := pagination.Page{Total: 45, Page: 2, Limit: 20}
	pg.Base = "/admin/inventory/sources?project=project-1&type=internal"
	m := (&inventorySourcesPageData{
		SelectedProject: "project-1",
		Pagination:      pg,
	}).templateMap(c)

	got, ok := m["Pagination"].(pagination.Page)
	if !ok {
		t.Fatalf("模板键 Pagination 不是 pagination.Page 事实：%T", m["Pagination"])
	}
	if got != pg {
		t.Errorf("交出的事实与 handler 构造的不一致：%+v", got)
	}
	for _, legacy := range []string{"PaginationInfo", "PaginationLinks"} {
		if v, exists := m[legacy]; exists {
			t.Errorf("旧成品键 %s 仍在：%v", legacy, v)
		}
	}
}

// TestInventoryPurchasesAndSourcesZeroFactsRenderNoPagination 单页 / 装载失败（零值事实）
// 时两条路都必须渲染空 —— 「零值」而不是「nil 指针」，正是本卡换掉旧形态的那一处。
func TestInventoryPurchasesAndSourcesZeroFactsRenderNoPagination(t *testing.T) {
	for _, tc := range []struct {
		name string
		data gin.H
	}{
		{
			name: "采购单页/单页",
			data: (&inventoryPurchasesPageData{SelectedProject: "project-1",
				Pagination: pagination.Page{Total: 5, Page: 1, Limit: 20}}).templateMap(mustGinContext(t, "/admin/inventory/purchases?project=project-1")),
		},
		{
			name: "货源页/装载失败（零值）",
			data: (&inventorySourcesPageData{SelectedProject: "project-1"}).
				templateMap(mustGinContext(t, "/admin/inventory/sources?project=project-1")),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pg, ok := tc.data["Pagination"].(pagination.Page)
			if !ok {
				t.Fatalf("模板键 Pagination 不是事实：%T", tc.data["Pagination"])
			}
			if templates.PagerOf(pg).HasPagination() {
				t.Errorf("单页 / 零值事实不该判定为多页：%+v", pg)
			}
		})
	}
}

func mustGinContext(t *testing.T, target string) *gin.Context {
	t.Helper()
	c, _ := inventoryFactsMapper(t, target)
	return c
}
