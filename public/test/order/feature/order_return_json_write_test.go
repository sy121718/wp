package feature

import (
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

// 退货写是标准表单：单条用 returnId，批量用重复的 ids。两种形状走同一端点，
// 这里锁住形状校验、整批语义、以及 autoReceive 这种 checkbox 的语义。

func returnFormEngine(h *orderhttp.Handle) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	// 生产装配在基础层挂失败出口与请求取词；缺了它们片段渲染为空。
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.POST("/api/order/return/approve", pageview.HTMLFragment, h.ApproveReturn)
	engine.POST("/api/order/return/reject", pageview.HTMLFragment, h.RejectReturn)
	engine.POST("/api/order/return/receive", pageview.HTMLFragment, h.ReceiveReturn)
	return engine
}

func returnFormPost(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestReturnBulkShapeViolationsAreRejectedBeforeService(t *testing.T) {
	h := orderhttp.NewHandle(nil)
	engine := returnFormEngine(h)

	idsForm := func(path string, pairs ...string) url.Values {
		form := url.Values{}
		for i := 0; i+1 < len(pairs); i += 2 {
			form.Add(pairs[i], pairs[i+1])
		}
		return form
	}

	approve := "/api/order/return/approve"
	reject := "/api/order/return/reject"
	cases := []struct {
		name string
		path string
		form url.Values
	}{
		{"approve ids 为空", approve, idsForm(approve, "remark", "x")},
		{"approve ids 只有空白项", approve, idsForm(approve, "ids", "  ", "remark", "x")},
		{"reject 缺 remark", reject, idsForm(reject, "ids", "1")},
		{"reject remark 空串", reject, idsForm(reject, "ids", "1", "remark", "")},
		{"reject remark 全空白", reject, idsForm(reject, "ids", "1", "remark", "   ")},
	}
	for _, tc := range cases {
		rec := returnFormPost(engine, tc.path, tc.form)
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
		assertOrderLeakFree(t, tc.name, body)
	}
}

func TestReturnSingleFormSemantics(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货单档语义商品", 88, 20)
	orderID, userID, itemID := f.paidOrder(t, "return-single@example.com", vid, 2)
	stockAfterOrder := f.stockOf(t, vid)
	ret := f.requestReturn(t, orderID, userID, itemID, 2)

	h := orderhttp.NewHandle(f.orders)
	engine := returnFormEngine(h)

	// ① 拒绝缺理由：400，状态不变。
	rec := returnFormPost(engine, "/api/order/return/reject", url.Values{
		"returnId": {strconv.FormatUint(ret.ID, 10)}, "remark": {"  "},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("单档拒绝缺理由应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	assertOrderLeakFree(t, "单档拒绝缺理由", rec.Body.String())
	if got := f.returnStatusOf(t, ret.ID); got != "requested" {
		t.Fatalf("拒绝被拒后申请状态不该变，实际 %s", got)
	}

	// ② 拒绝成功：200 + Jet 片段，状态落定。
	rec = returnFormPost(engine, "/api/order/return/reject", url.Values{
		"returnId": {strconv.FormatUint(ret.ID, 10)}, "remark": {"表单通道拒绝"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("拒绝应成功，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ctype := rec.Header().Get("Content-Type"); !strings.HasPrefix(ctype, "text/html") {
		t.Fatalf("成功应回 HTML 片段，实际 Content-Type=%q", ctype)
	}
	if got := f.returnStatusOf(t, ret.ID); got != "rejected" {
		t.Fatalf("拒绝后状态应为 rejected，实际 %s", got)
	}

	// ③ autoReceive 是 checkbox：勾上（value=1）一步到底。
	orderID2, userID2, itemID2 := f.paidOrder(t, "return-single-auto@example.com", vid, 2)
	stockAfterOrder2 := f.stockOf(t, vid)
	reservedAfterOrder2 := f.reservedOf(t, vid)
	ret2 := f.requestReturn(t, orderID2, userID2, itemID2, 2)
	rec = returnFormPost(engine, "/api/order/return/approve", url.Values{
		"returnId":    {strconv.FormatUint(ret2.ID, 10)},
		"remark":      {"同意"},
		"autoReceive": {"1"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("单档同意（一步到底）应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := f.returnStatusOf(t, ret2.ID); got != "completed" {
		t.Fatalf("autoReceive=1 应一步完成，实际 %s", got)
	}
	if got := f.stockOf(t, vid); got != stockAfterOrder2 {
		t.Fatalf("未发货订单的退货不改变在库数量（应仍是 %d），实际 %d", stockAfterOrder2, got)
	}
	if got := f.reservedOf(t, vid); got != reservedAfterOrder2-2 {
		t.Fatalf("一步到底后预占应减 2（%d → %d），实际 %d", reservedAfterOrder2, reservedAfterOrder2-2, got)
	}
	if got := f.orderStatusOf(t, orderID2); got != "refunded" {
		t.Fatalf("全额退货后订单应为已退款，实际 %s", got)
	}

	// ④ 重复收货幂等：库存不会再变。
	rec = returnFormPost(engine, "/api/order/return/receive", url.Values{
		"returnId": {strconv.FormatUint(ret2.ID, 10)},
	})
	stockBeforeRetry := f.stockOf(t, vid)
	if got := f.stockOf(t, vid); got != stockBeforeRetry {
		t.Fatalf("重复点收货把库存又加了一遍：%d → %d（响应 %d）", stockBeforeRetry, got, rec.Code)
	}

	// ⑤ 非数字 returnId：400，且不动库存。
	rec = returnFormPost(engine, "/api/order/return/approve", url.Values{
		"returnId": {"abc"}, "remark": {"x"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非数字 returnId 应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	assertOrderLeakFree(t, "非数字 returnId", rec.Body.String())
	if got := f.stockOf(t, vid); got != stockAfterOrder2 {
		t.Fatalf("非数字 returnId 的请求不该动库存，实际 %d（应为 %d）", got, stockAfterOrder2)
	}
	_ = stockAfterOrder
}

func TestReturnBulkApproveApprovesWithoutReceiving(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货批档同意商品", 50, 30)
	a := f.newReturn(t, "ret-bulk-a@example.com", vid, 1)
	b := f.newReturn(t, "ret-bulk-b@example.com", vid, 1)
	stockBefore := f.stockOf(t, vid)

	h := orderhttp.NewHandle(f.orders)
	engine := returnFormEngine(h)

	form := url.Values{}
	form.Add("ids", strconv.FormatUint(a, 10))
	form.Add("ids", strconv.FormatUint(b, 10))
	form.Set("remark", "批量同意")
	rec := returnFormPost(engine, "/api/order/return/approve", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("批量同意应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}

	for _, id := range []uint64{a, b} {
		if got := f.returnStatusOf(t, id); got != "approved" {
			t.Fatalf("批量同意后申请 %d 应为 approved（不入库），实际 %s", id, got)
		}
	}
	if got := f.stockOf(t, vid); got != stockBefore {
		t.Fatalf("批量同意不该动库存：%d → %d", stockBefore, got)
	}
}

func TestReturnBulkApproveRejectsWholeBatchWhenAnyUnreviewable(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货批档拒绝整批商品", 30, 30)
	pending := f.newReturn(t, "ret-skip-pending@example.com", vid, 1)
	already := f.newReturn(t, "ret-skip-already@example.com", vid, 1)

	if _, err := f.orders.ApproveReturn(t.Context(), &orderdto.ReturnReviewReq{
		ReturnID: already, Remark: "先同意一次", OperatorName: "tester",
	}); err != nil {
		t.Fatalf("先行同意失败：%v", err)
	}
	missing := returnMissingID(t, f)

	h := orderhttp.NewHandle(f.orders)
	engine := returnFormEngine(h)

	form := url.Values{}
	form.Add("ids", strconv.FormatUint(pending, 10))
	form.Add("ids", strconv.FormatUint(already, 10))
	form.Set("remark", "批档业务档")
	rec := returnFormPost(engine, "/api/order/return/approve", form)
	if rec.Code != http.StatusConflict {
		t.Fatalf("整批被拒应 409，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `data-ui-error`) {
		t.Fatalf("失败应渲染 Jet 错误片段：%s", rec.Body.String())
	}
	if got := f.returnStatusOf(t, pending); got != "requested" {
		t.Fatalf("整批被拒时待审的那条不该被同意，实际 %s", got)
	}

	missingForm := url.Values{}
	missingForm.Add("ids", strconv.FormatUint(pending, 10))
	missingForm.Add("ids", strconv.FormatUint(missing, 10))
	missingForm.Set("remark", "批档缺单")
	if rec := returnFormPost(engine, "/api/order/return/approve", missingForm); rec.Code != http.StatusConflict {
		t.Fatalf("含不存在单号时整批被拒应 409，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := f.returnStatusOf(t, pending); got != "requested" {
		t.Fatalf("整批被拒时待审的那条不该被同意，实际 %s", got)
	}

	shapeForm := url.Values{}
	shapeForm.Add("ids", strconv.FormatUint(pending, 10))
	shapeForm.Add("ids", "abc")
	shapeForm.Set("remark", "批档形状")
	if rec := returnFormPost(engine, "/api/order/return/approve", shapeForm); rec.Code != http.StatusConflict {
		t.Fatalf("形状错也应整批被拒（409），实际 %d：%s", rec.Code, rec.Body.String())
	}

	c := f.newReturn(t, "ret-skip-ok@example.com", vid, 1)
	okForm := url.Values{}
	okForm.Add("ids", strconv.FormatUint(c, 10))
	okForm.Set("remark", "批档合法")
	rec = returnFormPost(engine, "/api/order/return/approve", okForm)
	if rec.Code != http.StatusOK {
		t.Fatalf("单条可审时应成功，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if got := f.returnStatusOf(t, c); got != "approved" {
		t.Fatalf("单条可审时应落为 approved，实际 %s", got)
	}
}

func TestReturnBulkRejectRequiresReasonBeforeAnyWrite(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	_, vid := f.addProduct(t, "退货批档拒绝商品", 20, 30)
	a := f.newReturn(t, "ret-rej-a@example.com", vid, 1)
	b := f.newReturn(t, "ret-rej-b@example.com", vid, 1)

	h := orderhttp.NewHandle(f.orders)
	engine := returnFormEngine(h)

	noReason := url.Values{}
	noReason.Add("ids", strconv.FormatUint(a, 10))
	noReason.Add("ids", strconv.FormatUint(b, 10))
	rec := returnFormPost(engine, "/api/order/return/reject", noReason)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("批量拒绝缺理由应 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	for _, id := range []uint64{a, b} {
		if got := f.returnStatusOf(t, id); got != "requested" {
			t.Fatalf("缺理由时不该有任何写入，申请 %d 已变成 %s", id, got)
		}
	}

	okForm := url.Values{}
	okForm.Add("ids", strconv.FormatUint(a, 10))
	okForm.Add("ids", strconv.FormatUint(b, 10))
	okForm.Set("remark", "尺码不合适，客户已确认")
	rec = returnFormPost(engine, "/api/order/return/reject", okForm)
	if rec.Code != http.StatusOK {
		t.Fatalf("批量拒绝应成功，实际 %d：%s", rec.Code, rec.Body.String())
	}
	for _, id := range []uint64{a, b} {
		if got := f.returnStatusOf(t, id); got != "rejected" {
			t.Fatalf("申请 %d 应被拒绝，实际 %s", id, got)
		}
	}
}

func (f *orderFixture) newReturn(t *testing.T, email, variantID string, qty int) uint64 {
	t.Helper()
	orderID, userID, itemID := f.paidOrder(t, email, variantID, qty)
	return f.requestReturn(t, orderID, userID, itemID, qty).ID
}

func returnMissingID(t *testing.T, f *orderFixture) uint64 {
	t.Helper()
	var maxID uint64
	if err := f.db.Raw("SELECT COALESCE(MAX(id), 0) FROM order_returns").Scan(&maxID).Error; err != nil {
		t.Fatalf("读最大退货单 id 失败: %v", err)
	}
	return maxID + 999999
}
