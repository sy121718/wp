package feature

// order_error_leak_test.go — 订单后台页不把 error 原文送进响应（审计 CQ-009 / 第三波收口）。
//
// 本批（受控类型批）把三个页面（订单 / 优惠码 / 退货）里「shell.BulkIDs 的失败被 err.Error()
// 直接送进 ?err=」的那一路，从「用 shell.MaxBulkIDs 重组同一句话」改成调用
// shell.BulkIDsFacingText：超限错误是带 sentinel 的类型（shell.ErrBulkIDsTooMany /
// *shell.BulkIDsError），于是「当前 M 项」（去重后的条数）也回到了页面上 ——
// 那个数只有 shell 知道，模块自己重算就是第二份真相（旧实现正是丢掉了它）。
//
// 断言因此是结构性的：① 响应里是受控文案且**不含任何内部细节指纹**；② 上限与本次条数
// **两个数字都在**，且来自类型字段而不是从错误原文里掐出来的（出口只认类型、不认文本，
// 见 internal/web/shell/bulk_test.go 的「包装不泄漏」用例）。
// 只断言「没有 SQLSTATE」不足以区分「真的受控」与「恰好这句话里没有敏感词」。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderhttp "go_wp/internal/module/order/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/internal/web/shell"
)

// orderLeakTokens 内部细节指纹：PG 原文与库结构里一定出现、业务文案里一定不出现。
var orderLeakTokens = []string{"SQLSTATE", "uq_", "pg_", `relation "`, "constraint"}

// assertOrderLeakFree 断言给定文本不含任何内部细节指纹。
func assertOrderLeakFree(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range orderLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, text)
		}
	}
}

// orderOverLimitForm 构造超过 shell.MaxBulkIDs 的批量表单（多给一条即触发上限）。
func orderOverLimitForm(extra url.Values) url.Values {
	form := url.Values{}
	if extra != nil {
		for k, vs := range extra {
			form[k] = vs
		}
	}
	n := shell.MaxBulkIDs + 1
	ids := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		ids = append(ids, strconv.Itoa(i))
	}
	form["ids"] = ids
	return form
}

// orderPOSTForm 发一个表单 POST。
func orderPOSTForm(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// orderRedirectErr 取 302 的 Location 上的 err 参数（页面就是这样把它渲染出来的）。
func orderRedirectErr(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("Location 不可解析：%v", err)
	}
	return loc.Query().Get("err")
}

// TestOrderBulkPagesRejectOversizedSelectionWithControlledText 三个页面的批量入口在
// id 超限时都回带**受控文案**，且该文案与 err.Error() 无关（原文里的计数不会跟着出去）。
//
// 用 nil service 是刻意的最小化：超限在读到 service 之前就被 shell.BulkIDs 拒绝，
// 这条路径本来就不该碰数据库。
func TestOrderBulkPagesRejectOversizedSelectionWithControlledText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orderPages := orderhttp.NewOrderPageHandle(nil, nil, nil)
	couponPages := orderhttp.NewCouponPageHandle(nil, nil)
	returnPages := orderhttp.NewReturnPageHandle(nil, nil, nil, nil)

	engine := gin.New()
	engine.POST("/admin/orders/bulk-status", orderPages.OrderBulkStatus)
	engine.POST("/admin/orders/bulk-cancel", orderPages.OrderBulkCancel)
	engine.POST("/admin/coupons/bulk-delete", couponPages.CouponBulkDelete)
	engine.POST("/admin/coupons/bulk-toggle", couponPages.CouponBulkToggle)
	engine.POST("/admin/returns/bulk-approve", returnPages.ReturnBulkApprove)
	engine.POST("/admin/returns/bulk-reject", returnPages.ReturnBulkReject)

	// 反证：shell.BulkIDs 在同一个表单上给出的原文里带「当前 N 项」的计数。
	probe, _ := gin.CreateTestContext(httptest.NewRecorder())
	probe.Request = httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(orderOverLimitForm(nil).Encode()))
	probe.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, rawErr := shell.BulkIDs(probe)
	if rawErr == nil {
		t.Fatalf("反证失败：ids 超过 %d 条时 shell.BulkIDs 应报错", shell.MaxBulkIDs)
	}
	over := strconv.Itoa(shell.MaxBulkIDs + 1)
	if !strings.Contains(rawErr.Error(), over) {
		t.Fatalf("反证失败：原文应带本次条数 %s，实际 %v", over, rawErr)
	}

	// 批量取消要求先填原因，否则整批不处理；补上这一项才能走到 id 校验。
	cases := []struct {
		path string
		form url.Values
	}{
		{"/admin/orders/bulk-status", orderOverLimitForm(url.Values{"status": {"paid"}})},
		{"/admin/orders/bulk-cancel", orderOverLimitForm(url.Values{"remark": {"测试"}, "project": {"p"}})},
		{"/admin/coupons/bulk-delete", orderOverLimitForm(nil)},
		{"/admin/coupons/bulk-toggle", orderOverLimitForm(url.Values{"status": {"1"}})},
		{"/admin/returns/bulk-approve", orderOverLimitForm(nil)},
		{"/admin/returns/bulk-reject", orderOverLimitForm(url.Values{"remark": {"测试"}})},
	}
	for _, tc := range cases {
		rec := orderPOSTForm(engine, tc.path, tc.form)
		if rec.Code != http.StatusFound {
			t.Fatalf("%s 应回 302，实际 %d", tc.path, rec.Code)
		}
		got := orderRedirectErr(t, rec)
		if !strings.Contains(got, "一次最多操作") {
			t.Errorf("%s 应回带「一次最多操作」的受控文案，实际 %q", tc.path, got)
		}
		// 结构性断言（本批改向）：文案由**受控类型**给出，所以「当前 M 项」这个只有 shell
		// 知道的去重后条数也应当出现在页面上。旧实现能对上「一次最多操作」，却恰好丢了 M，
		// 所以这里两个数字一起断：少了 M 说明文案又退回了模块自己拼的版本。
		if !strings.Contains(got, strconv.Itoa(shell.MaxBulkIDs)) || !strings.Contains(got, over) {
			t.Errorf("%s 的 ?err= 应带上限 %d 与本次条数 %s（由受控类型给回），实际 %q",
				tc.path, shell.MaxBulkIDs, over, got)
		}
		assertOrderLeakFree(t, tc.path+" 的 ?err=", got)
	}
}

// TestOrderPageHidesInternalError 订单列表取数撞上真实 PG 错误（表被删）：
// 页面只给归口文案，不带表名 / SQLSTATE。
func TestOrderPageHidesInternalError(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.GET("/admin/orders", orderhttp.NewOrderPageHandle(f.orders, projects, nil).OrdersPage)

	// 制造**真实**基础设施错误。
	if err := f.db.Exec("DROP TABLE IF EXISTS orders CASCADE").Error; err != nil {
		t.Fatalf("制造故障失败：%v", err)
	}
	// 反证：原文带表名与 SQLSTATE。
	_, rawErr := f.orders.ListOrders(context.Background(), &orderdto.ListOrderReq{
		ProjectID: f.projectID, Limit: 5,
	})
	if rawErr == nil {
		t.Fatal("反证失败：表已删除，ListOrders 却成功了")
	}
	if !strings.Contains(rawErr.Error(), `relation "orders"`) || !strings.Contains(rawErr.Error(), "SQLSTATE") {
		t.Fatalf("反证失败：原始错误应带表名与 SQLSTATE，实际 %v", rawErr)
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/orders?project="+f.projectID, nil))
	body := rec.Body.String()
	assertOrderLeakFree(t, "订单列表页响应体", body)
	for _, tok := range []string{"relation", "SQLSTATE", "42P01"} {
		if strings.Contains(body, tok) {
			t.Errorf("订单列表页响应体泄漏内部细节 %q", tok)
		}
	}
	if !strings.Contains(body, "系统内部错误，请稍后重试") {
		t.Errorf("页面应显示归口文案，实际未出现（渲染中断？）")
	}
}
