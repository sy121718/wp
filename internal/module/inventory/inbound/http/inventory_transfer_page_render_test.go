package inventoryhttp

// inventory_transfer_page_render_test.go — 调拨单页（GET /admin/inventory/transfers）
// 的**渲染**判据。
//
// 为什么需要它：调拨页此前没有任何渲染测试 —— 断言全在 service 层（状态机 / 审批 /
// 签收），页面模板一次都没被真渲染过。于是模板里 `{{range _, w := $.Warehouses}}`
// 这种 Jet 不支持的写法可以一直躺着：`$` 在 Jet 里不是父作用域，那一行解析即失败，
// 整页 500，而"三包全绿"看起来毫无异样。
//
// 本文件在包内（而不是 public/test/.../feature）：调拨页的仓库下拉由库存页句柄的
// listWarehouses 提供（未导出），只有包内才装配得出线上那条链。
//
// 与 inventory_page_render_test.go 同形：服务是桩、模板与页壳是真实实现。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
)

// transferPageRenderStub 只实现调拨页用到的两条读方法（其余走嵌入的 nil 接口）。
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

// warehouseRowLister 固定返回两个仓库（源 / 目标），供「同意」抽屉的下拉渲染。
func warehouseRowLister(_ *gin.Context, projectID string) ([]inventorydto.WarehouseRowResp, error) {
	if projectID == "" {
		return []inventorydto.WarehouseRowResp{}, nil
	}
	return []inventorydto.WarehouseRowResp{
		{ID: "wh-from", Code: "SZ", Name: "苏州仓"},
		{ID: "wh-to", Code: "SH", Name: "上海仓"},
	}, nil
}

// newTransferPageRenderRouter 与 inventory_page_render_test.go 的装配同一条渲染路径，
// 只多挂一个调拨页（页面出口 / 页壳 / 取词都走真实实现）。
func newTransferPageRenderRouter(t *testing.T, stub *transferPageRenderStub) *gin.Engine {
	t.Helper()
	router := newInventoryPageTestRouter(t, stub, &inventoryPageRenderProjects{})
	handle := NewInventoryTransferPageHandle(stub, &inventoryPageRenderProjects{}, warehouseRowLister)
	router.GET("/admin/inventory/transfers",
		adminui.PageSlot(InventoryTransfersPageTemplate), handle.InventoryTransfersPage)
	return router
}

// TestTransfersPageTemplateParses 空数据也必须 200：模板本身能解析。
//
// 这是本文件存在的首要理由 —— 模板解析失败会让整页 500，而没有任何 service 层断言能看到它。
func TestTransfersPageTemplateParses(t *testing.T) {
	router := newTransferPageRenderRouter(t, &transferPageRenderStub{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/inventory/transfers?project=project-1", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("调拨页应 200（模板必须能解析），实际 %d：%s", rec.Code, clipBody(rec.Body.String()))
	}
	body := rec.Body.String()
	// 页签标题取自视图：struct 的 view:"title" tag 摊平正确。
	if !strings.Contains(body, "<title>调拨单") {
		t.Errorf(`页签标题应取自视图（缺 "<title>调拨单"）—— Title 漏了 view:"title" 会静默回落成品牌名`)
	}
	if strings.Contains(body, "<title>管理后台") {
		t.Errorf("标题退化成品牌名了（视图的 Title 没有摊平到 .title 键）")
	}
}

// TestTransfersPageApproveDrawerHasWarehouses 申请中的单会渲出「同意」抽屉，
// 抽屉里的目标仓下拉必须能看到备选仓。
//
// 直接钉住 `$.Warehouses`：Jet 不支持 `$` 父作用域，那一行解析即失败（上一测试已拦住）；
// 即便改成能解析的写法，取不到外层键时下拉也只剩「沿用申请时的目标仓」一项 ——
// 那是一个不会报错的静默缺陷，只有渲染断言抓得到。
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
	// 两个仓都要在下拉里：取到外层 Warehouses 才会渲出全部选项。
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
