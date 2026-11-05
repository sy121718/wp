package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
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
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
)

var orderLeakTokens = []string{"SQLSTATE", "uq_", "pg_", `relation "`, "constraint"}

func assertOrderLeakFree(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range orderLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, text)
		}
	}
}

func orderOverLimitJSON(extra map[string]any) string {
	payload := map[string]any{"ids": orderOverLimitIDs()}
	for k, v := range extra {
		payload[k] = v
	}
	b, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func orderOverLimitIDs() []string {
	n := bulkids.MaxIDs + 1
	ids := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		ids = append(ids, strconv.Itoa(i))
	}
	return ids
}

// orderPOSTForm 按标准表单编码提交：ids 重复提交，其余是普通字段。
func orderPOSTForm(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// orderOverLimitForm 造一份超过上限的批量表单：ids 重复 201 次。
func orderOverLimitForm(extra map[string]string) url.Values {
	form := url.Values{}
	for _, id := range orderOverLimitIDs() {
		form.Add("ids", id)
	}
	for k, v := range extra {
		form.Set(k, v)
	}
	return form
}

func TestOrderBulkPagesRejectOversizedSelectionWithControlledText(t *testing.T) {

	i18n.InjectForTest(map[string]map[string]string{
		bulkids.MsgTooMany: {"zh-CN": "一次最多操作 %s 项，当前 %s 项，请分批进行",
			"en-US": "Too many items at once: max %s, got %s"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	gin.SetMode(gin.TestMode)
	orderHandle := orderhttp.NewHandle(nil)

	engine := gin.New()

	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	// 生产装配：失败出口与请求取词都必须在场，否则失败片段落成空响应。
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })

	engine.POST("/api/order/status", pageview.HTMLFragment, orderHandle.ChangeStatus)
	engine.POST("/api/order/cancel", pageview.HTMLFragment, orderHandle.CancelOrder)
	engine.POST("/api/order/return/approve", pageview.HTMLFragment, orderHandle.ApproveReturn)
	engine.POST("/api/order/return/reject", pageview.HTMLFragment, orderHandle.RejectReturn)

	_, rawErr := bulkids.Normalize(orderOverLimitIDs())
	if rawErr == nil {
		t.Fatalf("反证失败：ids 超过 %d 条时 bulkids.Normalize 应报错", bulkids.MaxIDs)
	}
	over := strconv.Itoa(bulkids.MaxIDs + 1)
	if !strings.Contains(rawErr.Error(), over) {
		t.Fatalf("反证失败：原文应带本次条数 %s，实际 %v", over, rawErr)
	}

	assertControlled := func(t *testing.T, where, text string) {
		t.Helper()
		if !strings.Contains(text, "一次最多操作") {
			t.Errorf("%s 应回带「一次最多操作」的受控文案，实际 %q", where, text)
		}

		if !strings.Contains(text, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(text, over) {
			t.Errorf("%s 应带上限 %d 与本次条数 %s（由受控类型给回），实际 %q", where, bulkids.MaxIDs, over, text)
		}
		assertOrderLeakFree(t, where, text)
	}

	for _, tc := range []struct{ path string; form url.Values }{
		{"/api/order/return/approve", orderOverLimitForm(map[string]string{"remark": "测试"})},
		{"/api/order/return/reject", orderOverLimitForm(map[string]string{"remark": "测试"})},
	} {
		rec := orderPOSTForm(engine, tc.path, tc.form)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 超限应回 400，实际 %d：%s", tc.path, rec.Code, rec.Body.String())
		}
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Errorf("%s 不应再用 302 + ?err= 回带文案，实际 Location=%q", tc.path, loc)
		}
		assertControlled(t, tc.path+" 的批档回执", rec.Body.String())
	}

	assertBatchControlled := func(t *testing.T, where, text string) {
		t.Helper()
		if !strings.Contains(text, strconv.Itoa(bulkids.MaxIDs)) || !strings.Contains(text, over) {
			t.Errorf("%s 应带上限 %d 与本次条数 %s（由受控类型给回），实际 %q", where, bulkids.MaxIDs, over, text)
		}

		for _, tok := range []string{"bulkids:", "IDsError", "一次最多操作 %s"} {
			if strings.Contains(text, tok) {
				t.Errorf("%s 泄漏原文/未填参模板 %q：%s", where, tok, text)
			}
		}
		assertOrderLeakFree(t, where, text)
	}
	for _, tc := range []struct {
		path string
		form url.Values
	}{
		{"/api/order/status", orderOverLimitForm(map[string]string{"toStatus": "paid"})},
		{"/api/order/cancel", orderOverLimitForm(map[string]string{"remark": "测试"})},
	} {
		rec := orderPOSTForm(engine, tc.path, tc.form)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 超限应回 400，实际 %d：%s", tc.path, rec.Code, rec.Body.String())
		}
		assertBatchControlled(t, tc.path+" 的批档回执", rec.Body.String())
	}
}

func TestOrderPageHidesInternalError(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.GET("/admin/orders", pageview.Slot(orderhttp.OrderListPageTemplate, adminui.RenderPage), orderhttp.NewOrderPageHandle(f.orders, projects, nil).OrdersPage)

	if err := f.db.Exec("DROP TABLE IF EXISTS orders CASCADE").Error; err != nil {
		t.Fatalf("制造故障失败：%v", err)
	}

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
