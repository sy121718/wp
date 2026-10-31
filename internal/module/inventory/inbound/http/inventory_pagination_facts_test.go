package inventoryhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	inventorydto "go_wp/internal/module/inventory/dto"
	"go_wp/pkg/i18n"
)

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

type b3bInventoryFactsStub struct {
	*inventoryPageRenderStub
	movementsTotal int64
}

func (s *b3bInventoryFactsStub) CountMovements(context.Context, *inventorydto.ListMovementReq) (int64, error) {
	return s.movementsTotal, nil
}

func TestInventoryMovementsPaginationFactsWiring(t *testing.T) {
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

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

		`is-active">2</span>`,

		"共 120 条，第 51-100 条"} {
		if !strings.Contains(nav, want) {
			t.Errorf("分页条缺少 %q：\n%s", want, nav)
		}
	}
}

func inventoryFactsMapper(t *testing.T, target string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	return c, rec
}
func mustGinContext(t *testing.T, target string) *gin.Context {
	t.Helper()
	c, _ := inventoryFactsMapper(t, target)
	return c
}
