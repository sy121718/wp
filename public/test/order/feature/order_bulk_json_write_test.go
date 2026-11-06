package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	orderdto "go_wp/internal/module/order/dto"
	orderhttp "go_wp/internal/module/order/inbound/http"
	"go_wp/internal/templates"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

// 订单批量写是标准表单：ids 重复提交，其余是普通具名字段。
// 这里锁住三件事：形状不合法时不进 service、整批被拒时回 409 + 逐条原因、成功后回成功片段。

func orderBulkFormEngine(h *orderhttp.Handle) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	// 生产装配在基础层挂失败 / 成功出口与请求取词；缺了它们片段渲染为空。
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(pageview.UseSuccess(adminui.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.POST("/api/order/status", pageview.HTMLFragment, h.ChangeStatus)
	engine.POST("/api/order/cancel", pageview.HTMLFragment, h.CancelOrder)
	return engine
}

func orderBulkPost(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestOrderBulkShapeViolationsAreRejectedBeforeService(t *testing.T) {
	h := orderhttp.NewHandle(nil)
	engine := orderBulkFormEngine(h)

	statusForm := func(pairs ...string) url.Values {
		form := url.Values{}
		for i := 0; i+1 < len(pairs); i += 2 {
			form.Add(pairs[i], pairs[i+1])
		}
		return form
	}

	cases := []struct {
		name string
		path string
		form url.Values
	}{
		{"缺 toStatus", "/api/order/status", statusForm("ids", "1")},
		{"toStatus 全空白", "/api/order/status", statusForm("ids", "1", "toStatus", "   ")},
		{"ids 为空", "/api/order/status", statusForm("toStatus", "paid")},
		{"ids 只有空白项", "/api/order/status", statusForm("ids", "  ", "toStatus", "paid")},
		{"批量取消缺 remark", "/api/order/cancel", statusForm("ids", "1")},
		{"批量取消 remark 空串", "/api/order/cancel", statusForm("ids", "1", "remark", "")},
	}
	for _, tc := range cases {
		rec := orderBulkPost(engine, tc.path, tc.form)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s：应 400（形状档），实际 %d：%s", tc.name, rec.Code, rec.Body.String())
			continue
		}
		body := rec.Body.String()
		for _, tok := range []string{"SQLSTATE", "panic", "%s", "%!s("} {
			if strings.Contains(body, tok) {
				t.Errorf("%s：响应体出现非受控片段 %q：%s", tc.name, tok, body)
			}
		}
		if !strings.Contains(body, `data-ui-error`) {
			t.Errorf("%s：失败应渲染 Jet 错误片段：%s", tc.name, body)
		}
	}
}

func TestOrderBulkBusinessRejectionsRejectWholeBatch(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "批档业务档商品", 20, 50)
	mkOrder := func(email string) uint64 {
		req := f.createBaseReq(vid, 1)
		req.CustomerEmail = email
		res, err := f.orders.CreateOrder(context.Background(), req)
		if err != nil {
			t.Fatalf("建单失败: %v", err)
		}
		return res.ID
	}
	ok := mkOrder("bulk-biz-ok@example.com")
	badEdge := mkOrder("bulk-biz-edge@example.com")

	h := orderhttp.NewHandle(f.orders)
	engine := orderBulkFormEngine(h)

	// ① 状态机不允许的边：整批被拒，两条都没动。
	form := url.Values{}
	form.Add("ids", strconv.FormatUint(ok, 10))
	form.Add("ids", strconv.FormatUint(badEdge, 10))
	form.Set("toStatus", "completed")
	form.Set("remark", "批档业务档")
	rec := orderBulkPost(engine, "/api/order/status", form)
	if rec.Code != http.StatusConflict {
		t.Fatalf("整批被拒应回 409，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `data-ui-error`) {
		t.Fatalf("失败应渲染 Jet 错误片段：%s", body)
	}
	// 业务拒绝必须给出可读原因，且不得漏出声明 key。
	if !strings.Contains(body, "整批") && !strings.Contains(body, "状态") && !strings.Contains(body, "流转") {
		t.Errorf("失败片段应给出可读原因：%s", body)
	}
	if strings.Contains(body, "order.err.") || strings.Contains(body, "order.msg.") {
		t.Errorf("失败片段漏出了声明 key：%s", body)
	}
	for _, id := range []uint64{ok, badEdge} {
		if got := f.orderStatusOf(t, id); got != "pending" {
			t.Errorf("订单 %d 在整批被拒后改了状态：%s（批量必须全败）", id, got)
		}
	}

	// ② 批量取消里混进一张已取消的单：整批被拒。
	cancelable := mkOrder("bulk-biz-cancel-ok@example.com")
	already := mkOrder("bulk-biz-cancel-twice@example.com")
	if _, err := f.orders.CancelOrder(t.Context(), &orderdto.CancelOrderReq{
		OrderID: already, Reason: "先取消一次", OperatorName: "tester",
	}); err != nil {
		t.Fatalf("先取消一次失败：%v", err)
	}
	cancel := url.Values{}
	cancel.Add("ids", strconv.FormatUint(cancelable, 10))
	cancel.Add("ids", strconv.FormatUint(already, 10))
	cancel.Set("remark", "批档业务档取消")
	rec = orderBulkPost(engine, "/api/order/cancel", cancel)
	if rec.Code != http.StatusConflict {
		t.Fatalf("整批被拒应回 409，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := f.orderStatusOf(t, cancelable); got != "pending" {
		t.Errorf("整批被拒时可取消的订单不该被取消，实际 %s", got)
	}

	// ③ 两条都能取消：回成功片段，两条都落定。
	fine := mkOrder("bulk-biz-fine@example.com")
	fine2 := mkOrder("bulk-biz-fine2@example.com")
	cancelOK := url.Values{}
	cancelOK.Add("ids", strconv.FormatUint(fine, 10))
	cancelOK.Add("ids", strconv.FormatUint(fine2, 10))
	cancelOK.Set("remark", "批档合法取消")
	rec = orderBulkPost(engine, "/api/order/cancel", cancelOK)
	if rec.Code != http.StatusOK {
		t.Fatalf("批档全部可执行时应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ctype := rec.Header().Get("Content-Type"); !strings.HasPrefix(ctype, "text/html") {
		t.Fatalf("成功应回 HTML 片段，实际 Content-Type=%q", ctype)
	}
	if !strings.Contains(rec.Body.String(), `role="status"`) {
		t.Fatalf("成功应回 Jet 成功片段：%s", rec.Body.String())
	}
	for _, id := range []uint64{fine, fine2} {
		if got := f.orderStatusOf(t, id); got != "cancelled" {
			t.Errorf("订单 %d 应已取消，实际 %s", id, got)
		}
	}
}
