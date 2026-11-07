package inventoryhttp

import (
	pageview "go_wp/pkg/view"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
)

type transferPageRenderStub struct {
	inventorycontract.InventoryService
	rows  []*inventorydto.TransferRowResp
	total int64
}

func (s *transferPageRenderStub) ListTransfers(context.Context, *inventorydto.ListTransferReq) ([]*inventorydto.TransferRowResp, error) {
	return s.rows, nil
}

func (s *transferPageRenderStub) CountTransfers(context.Context, *inventorydto.ListTransferReq) (int64, error) {
	return s.total, nil
}

func warehouseRowLister(_ *gin.Context, projectID string) ([]WarehouseRowResp, error) {
	if projectID == "" {
		return []WarehouseRowResp{}, nil
	}
	return []WarehouseRowResp{
		{ID: "wh-from", Code: "SZ", Name: "苏州仓"},
		{ID: "wh-to", Code: "SH", Name: "上海仓"},
	}, nil
}

func newTransferPageRenderRouter(t *testing.T, stub *transferPageRenderStub) *gin.Engine {
	t.Helper()
	router := newInventoryPageTestRouter(t, stub, &inventoryPageRenderProjects{})
	handle := NewInventoryTransferPageHandle(stub, &inventoryPageRenderProjects{}, warehouseRowLister)
	router.GET("/admin/inventory/transfers",
		pageview.Slot(InventoryTransfersPageTemplate, pageview.RenderPage), handle.InventoryTransfersPage)
	return router
}

func TestTransfersPageTemplateParses(t *testing.T) {
	router := newTransferPageRenderRouter(t, &transferPageRenderStub{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/inventory/transfers?project=project-1", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("调拨页应 200（模板必须能解析），实际 %d：%s", rec.Code, clipBody(rec.Body.String()))
	}
	body := rec.Body.String()

	if !strings.Contains(body, "<title>调拨单") {
		t.Errorf(`页签标题应取自视图（缺 "<title>调拨单"）—— Title 漏了 view:"title" 会静默回落成品牌名`)
	}
	if strings.Contains(body, "<title>管理后台") {
		t.Errorf("标题退化成品牌名了（视图的 Title 没有摊平到 .title 键）")
	}
}

func TestTransfersPageApproveDrawerHasWarehouses(t *testing.T) {
	stub := &transferPageRenderStub{
		total: 1,
		rows: []*inventorydto.TransferRowResp{{
			ID: "1", TransferNo: "TR-0001", Status: "requested",
			FromCode: "SZ", ToCode: "SH", TotalQuantity: 2, ItemCount: 1,
		}},
	}
	router := newTransferPageRenderRouter(t, stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/inventory/transfers?project=project-1", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d：%s", rec.Code, clipBody(rec.Body.String()))
	}

	body := rec.Body.String()
	if !strings.Contains(body, "TR-0001") {
		t.Fatalf("调拨单号没有出现在页面上")
	}
	at := strings.Index(body, "tpl-transfer-approve-")
	if at < 0 {
		t.Fatalf("申请中的单应渲出「同意」抽屉（模板按 status == requested 渲）")
	}
	drawer := body[at:]
	if end := strings.Index(drawer, "</template>"); end > 0 {
		drawer = drawer[:end]
	}

	for _, code := range []string{"SZ", "SH"} {
		if !strings.Contains(drawer, code) {
			t.Errorf("同意抽屉的目标仓下拉里没有 %q —— 模板取不到外层 Warehouses（Jet 的 $.Warehouses 不成立）", code)
		}
	}
}

func clipBody(s string) string {
	if len(s) <= 800 {
		return s
	}
	return s[:800] + "…"
}
