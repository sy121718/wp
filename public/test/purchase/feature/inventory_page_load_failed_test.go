package feature

import (
	"net/http"
	"strings"
	"testing"
)

func breakProjectsTable(t *testing.T, f *purchaseFixture) {
	t.Helper()
	if err := f.db.Exec("ALTER TABLE projects RENAME TO projects_gone_for_load_test").Error; err != nil {
		t.Fatalf("制造装载失败（改表名）失败: %v", err)
	}
}

func assertLoadFailedPage(t *testing.T, where, body string) {
	t.Helper()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("%s 的降级渲染没有渲染完整页（缺 </html>）：模板中途中断了", where)
	}
	if !strings.Contains(body, "系统内部错误") {
		t.Fatalf("%s 的降级渲染缺归口文案：页面上没有任何提示", where)
	}
	if strings.Contains(body, "uiInternalErr") {
		t.Fatalf("%s 直出了裸归口 key `MsgInternalError`（应当显示译文）", where)
	}

	if !strings.Contains(body, "这一页的数据没能读出来") {
		t.Fatalf("%s 的空态没走装载失败档（判据 LoadFailed 没传给模板）", where)
	}
	assertNoInternalTokens(t, where, body)
}

func TestInventorySourcesPageLoadFailedDegrades(t *testing.T) {
	engine, f := newSourcePageEngine(t)
	if engine == nil {
		return
	}
	breakProjectsTable(t, f)

	rec := httptestGet(engine, "/admin/inventory/sources")
	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败应降级渲染（200），实际 %d：%s", rec.Code, rec.Body.String())
	}
	assertLoadFailedPage(t, "货源管理页", rec.Body.String())
}

func TestInventoryPurchasesPageLoadFailedDegrades(t *testing.T) {
	engine, f := newPurchasePageEngine(t)
	if engine == nil {
		return
	}
	breakProjectsTable(t, f)

	rec := httptestGet(engine, "/admin/inventory/purchases")
	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败应降级渲染（200），实际 %d：%s", rec.Code, rec.Body.String())
	}
	assertLoadFailedPage(t, "采购入库页", rec.Body.String())
}
