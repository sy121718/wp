package adminui

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/pagination"
	"go_wp/pkg/view"
)

func newTestCtx() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/admin/x", nil)
	return c
}

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

type plainView struct {
	Rows int
}

func TestPrepareViewInjectsPaginationFromContract(t *testing.T) {
	c := newTestCtx()
	data := prepareView(c, pageListView{Rows: 2, Total: 120, Page: 3, Limit: 20, Base: "/admin/x?keyword=a"})

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
	if _, ok := prepareView(c, plainView{Rows: 1})[view.PaginationKey]; ok {
		t.Fatalf("没实现 view.PageList 的视图不该被注入分页键")
	}
}

func TestPrepareViewKeepsExplicitPagination(t *testing.T) {
	c := newTestCtx()
	explicit := pagination.Page{Total: 7, Page: 1, Limit: 5, Base: "/hand-written"}
	data := prepareView(c, gin.H{
		view.PaginationKey: explicit,
		"Rows":             0,
	})
	got, ok := data[view.PaginationKey].(pagination.Page)
	if !ok || got.Base != "/hand-written" || got.Total != 7 {
		t.Fatalf("显式给的分页事实被覆盖了：%+v", data[view.PaginationKey])
	}
}

func TestPageListOfNilAndZero(t *testing.T) {
	if _, ok := view.PageListOf(nil); ok {
		t.Fatalf("nil 视图不该报有分页")
	}

	p, ok := view.PageListOf(pageListView{})
	if !ok {
		t.Fatalf("实现了接口就应报 true")
	}
	if p.Total != 0 {
		t.Fatalf("零值视图应给出零值分页，实际 %+v", p)
	}
}
