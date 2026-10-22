package adminui

// page_list_contract_test.go — 分页契约（view.PageList）在渲染出口的接线。
//
// 要回答的问题：视图实现了 Pagination() 时，「分页条要的事实」是不是**自动**进了渲染数据 ——
// 而不是每个 handler 各写一次 data["Pagination"] = ...（22 处字符串字面量）。
//
// 两条判据缺一不可：
//   - 实现了 → 键出现且值正确（否则契约没用）；
//   - 没实现 / 已自己塞了 → 不覆盖（否则分批迁移时会打乱存量页面的显式赋值）。

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/pagination"
	"go_wp/pkg/view"
)

// newTestCtx 建一个能安全调用的 gin 上下文（Prepare 会读请求头与 cookie）。
func newTestCtx() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/admin/x", nil)
	return c
}

// pageListView 实现 view.PageList 的视图（模拟 dto 列表页回执）。
type pageListView struct {
	Rows  int
	Total int64
	Page  int
	Limit int
	Base  string
}

func (v pageListView) Paging() pagination.Page {
	return pagination.Page{Total: v.Total, Page: v.Page, Limit: v.Limit, Base: v.Base}
}

// plainView 不实现 view.PageList 的视图。
type plainView struct {
	Rows int
}

func TestPrepareViewInjectsPaginationFromContract(t *testing.T) {
	c := newTestCtx()
	data := PrepareView(c, pageListView{Rows: 2, Total: 120, Page: 3, Limit: 20, Base: "/admin/x?keyword=a"})

	raw, ok := data[view.PaginationKey]
	if !ok {
		t.Fatalf("实现 view.PageList 的视图应自动获得 %q 键", view.PaginationKey)
	}
	got, ok := raw.(pagination.Page)
	if !ok {
		t.Fatalf("%q 的值应是 pagination.Page，实际 %T", view.PaginationKey, raw)
	}
	if got.Total != 120 || got.Page != 3 || got.Limit != 20 || got.Base != "/admin/x?keyword=a" {
		t.Fatalf("分页事实不对：%+v", got)
	}
}

func TestPrepareViewNoPaginationWithoutContract(t *testing.T) {
	c := newTestCtx()
	if _, ok := PrepareView(c, plainView{Rows: 1})[view.PaginationKey]; ok {
		t.Fatalf("没实现 view.PageList 的视图不该被注入分页键")
	}
}

// TestPrepareViewKeepsExplicitPagination 视图自己已经塞了 Pagination 时以它为准。
//
// 这条保证「分批迁移」可行：先把契约与出口接好，22 处手写赋值逐个删；
// 没删的页面行为完全不变（它给的 Base 往往比契约默认值更具体）。
func TestPrepareViewKeepsExplicitPagination(t *testing.T) {
	c := newTestCtx()
	explicit := pagination.Page{Total: 7, Page: 1, Limit: 5, Base: "/hand-written"}
	data := PrepareView(c, gin.H{
		view.PaginationKey: explicit,
		"Rows":             0, // gin.H 原样透传，不触发契约
	})
	got, ok := data[view.PaginationKey].(pagination.Page)
	if !ok || got.Base != "/hand-written" || got.Total != 7 {
		t.Fatalf("显式给的分页事实被覆盖了：%+v", data[view.PaginationKey])
	}
}

// TestPageListOfNilAndZero 契约的两个边界：nil 视图、零值 Page。
func TestPageListOfNilAndZero(t *testing.T) {
	if _, ok := view.PageListOf(nil); ok {
		t.Fatalf("nil 视图不该报有分页")
	}
	// 实现了接口但返回零值：ok 为 true（成因是「列表页但没数据」），
	// 与「不是列表页」区分开 —— 但两者对模板的效果相同（Total=0 不渲染分页条）。
	p, ok := view.PageListOf(pageListView{})
	if !ok {
		t.Fatalf("实现了接口就应报 true")
	}
	if p.Total != 0 {
		t.Fatalf("零值视图应给出零值分页，实际 %+v", p)
	}
}
