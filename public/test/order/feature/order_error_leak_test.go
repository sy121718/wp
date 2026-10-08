package feature

// order_error_leak_test.go — 订单后台页不把 error 原文送进响应（审计 CQ-009 / 第三波收口）。
//
// 本批（受控类型批）把三个页面（订单 / 优惠码 / 退货）里「shell.BulkIDs 的失败被 err.Error()
// 直接送进响应」的那一路，改成调用 shell.BulkIDsFacingText：超限错误是带 sentinel 的类型
// （shell.ErrBulkIDsTooMany / *shell.BulkIDsError），于是「当前 M 项」（去重后的条数）也回到了
// 页面上 —— 那个数只有 shell 知道，模块自己重算就是第二份真相（旧实现正是丢掉了它）。
//
// 断言因此是结构性的：① 响应里是受控文案且**不含任何内部细节指纹**；② 上限与本次条数
// **两个数字都在**，且来自类型字段而不是从错误原文里掐出来的（出口只认类型、不认文本，
// 见 internal/shell/bulk_test.go 的「包装不泄漏」用例）。
// 只断言「没有 SQLSTATE」不足以区分「真的受控」与「恰好这句话里没有敏感词」。
//
// 传输通道的契约变化（架构改造后）：写动作的结论**不再经 URL**（302 + ?err= / ?ok= / ?done=），
// 而是由 shell.RenderJump 渲染**提示页**（HTTP 200，文案在响应体里）。于是本用例横跨的
// 「已迁 / 未迁」两档已经合并为一档：订单 / 优惠码 / 退货**六条批量入口全部迁移**，
// 统一断言「200 + 提示页正文 + 无 Location 头」。读侧那套「查询参数不是可信边界」的
// 白名单判定（orderFacingText / orderDoneTexts / orderPageDone）随通道一起整批删除 ——
// 文案不进 URL，就没有「伪造的 ?err= 被当成提示」这条攻击面。

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
	"go_wp/internal/shell"
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

// TestOrderBulkPagesRejectOversizedSelectionWithControlledText 批量入口在 id 超限时都渲染
// **受控文案**的提示页，且该文案与 err.Error() 无关（原文里的计数不会跟着出去）。
//
// 六条批量入口现在**都在同一条契约上**：shell.RenderJump 渲染提示页（HTTP 200），
// 文案进响应体、不进 URL —— 于是「查询参数不是可信边界」那套读侧判定不再需要，
// 「伪造的 ?err= 冒充提示」这条攻击面被整块删掉。
//
// 共同的安全判据完全相同（受控文案 + 不含内部细节指纹 + 两个数字都在），
// 差别只在传输：都断言 200 + 无 Location 头 + 正文里是受控文案。
//
// 用 nil service 是刻意的最小化：超限在读到 service 之前就被 shell.BulkIDs 拒绝，
// 这条路径本来就不该碰数据库。
func TestOrderBulkPagesRejectOversizedSelectionWithControlledText(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orderPages := orderhttp.NewOrderPageHandle(nil, nil, nil)
	couponPages := orderhttp.NewCouponPageHandle(nil, nil)
	returnPages := orderhttp.NewReturnPageHandle(nil, nil, nil, nil)

	engine := gin.New()
	// 提示页要渲染模板（shell.RenderJump 渲染 admin/jump.html），所以引擎必须挂渲染器 ——
	// 与生产装配一致。
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
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

	// 共同判据抽成一个断言：受控文案出现 + 两个数字都在 + 不含内部细节。
	assertControlled := func(t *testing.T, where, text string) {
		t.Helper()
		if !strings.Contains(text, "一次最多操作") {
			t.Errorf("%s 应回带「一次最多操作」的受控文案，实际 %q", where, text)
		}
		// 文案由**受控类型**给出，所以「当前 M 项」这个只有 shell 知道的去重后条数也应当出现。
		// 少了 M 说明文案又退回了模块自己拼的版本。
		if !strings.Contains(text, strconv.Itoa(shell.MaxBulkIDs)) || !strings.Contains(text, over) {
			t.Errorf("%s 应带上限 %d 与本次条数 %s（由受控类型给回），实际 %q", where, shell.MaxBulkIDs, over, text)
		}
		assertOrderLeakFree(t, where, text)
	}

	// 六条批量入口（订单 / 优惠码 / 退货）：200 + 提示页，文案在响应体里，且**不再有 Location**。
	//
	// 批量取消 / 批量拒绝要求先填原因，否则整批不处理；补上这一项才能走到 id 校验。
	// 目标状态字段名与 orders.html 的批量下拉一致（toStatus）—— 旧用例跟着 handler 读错的
	// 字段构造请求，于是这条链路「测过了」，而真实页面提交的 toStatus 从未被读到。
	for _, tc := range []struct {
		path string
		form url.Values
	}{
		{"/admin/orders/bulk-status", orderOverLimitForm(url.Values{"toStatus": {"paid"}})},
		{"/admin/orders/bulk-cancel", orderOverLimitForm(url.Values{"remark": {"测试"}, "project": {"p"}})},
		{"/admin/coupons/bulk-delete", orderOverLimitForm(nil)},
		{"/admin/coupons/bulk-toggle", orderOverLimitForm(url.Values{"status": {"1"}})},
		{"/admin/returns/bulk-approve", orderOverLimitForm(nil)},
		{"/admin/returns/bulk-reject", orderOverLimitForm(url.Values{"remark": {"测试"}})},
	} {
		rec := orderPOSTForm(engine, tc.path, tc.form)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s 应回 200（提示页），实际 %d", tc.path, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Errorf("%s 不应再用 302 + ?err= 回带文案，实际 Location=%q", tc.path, loc)
		}
		assertControlled(t, tc.path+" 的提示页", rec.Body.String())
	}
}

// TestOrderPageHidesInternalError 订单列表取数撞上真实 PG 错误（表被删）：
// 页面降级渲染完整页面（200），只给归口文案，不带表名 / SQLSTATE。
//
// 新契约：归口文案不再走 `c.String(500, …)` 或 `?err=`，而是进模板的 `.LoadErr`
// （订单模板里是一条 `role="alert"` 的 badge，配空态段落「这一页的数据没能读出来」）。
// 判据不变：**原文一个片段都不进响应体**，页面显示的是 shell.PageInternalText 的中文。
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
	if rec.Code != http.StatusOK {
		t.Fatalf("装载失败必须降级渲染完整页面（200），实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("装载失败未渲染完整页面（缺 </html>）—— 页壳被整块吃掉")
	}
	assertOrderLeakFree(t, "订单列表页响应体", body)
	for _, tok := range []string{"relation", "SQLSTATE", "42P01"} {
		if strings.Contains(body, tok) {
			t.Errorf("订单列表页响应体泄漏内部细节 %q", tok)
		}
	}
	if !strings.Contains(body, "系统内部错误，请稍后重试") {
		t.Errorf("页面应显示归口文案（经 .LoadErr badge），实际未出现（渲染中断？）")
	}
}
