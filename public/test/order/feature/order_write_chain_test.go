package feature

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	orderhttp "go_wp/internal/module/order/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"go_wp/public/test/support"
)

// 这组用例守的是「渲染出来的表单 → 真实端点 → 业务落定」这条链：
// 属性改名、字段名改了模板没改控制器、表单没挂到正确的端点 —— 这些在静态扫描里
// 看不出来，只有把真实页面渲染出来、按页面里的表单提交、再看业务事实才能现形。

var (
	orderFormTagRe = regexp.MustCompile(`<form[^>]*>`)
	orderInputRe   = regexp.MustCompile(`<input[^>]*>`)
	orderSelectRe  = regexp.MustCompile(`<select[^>]*>([\s\S]*?)</select>`)
	orderOptionRe  = regexp.MustCompile(`<option[^>]*>`)
	orderAttrRe    = regexp.MustCompile(`([a-zA-Z_:][-a-zA-Z0-9_:.]*)(?:="([^"]*)")?`)
)

func orderAttrsOf(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range orderAttrRe.FindAllStringSubmatch(tag, -1) {
		if m[1] == "form" || m[1] == "input" || m[1] == "select" || m[1] == "button" || m[1] == "option" {
			continue
		}
		out[m[1]] = m[2]
	}
	return out
}

// orderFormBlock 截出 id=<formID> 的整个 <form>…</form> 片段。
func orderFormBlock(t *testing.T, html, formID string) string {
	t.Helper()
	marker := `id="` + formID + `"`
	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatalf("页面里找不到表单 #%s", formID)
	}
	open := strings.LastIndex(html[:i], "<form")
	if open < 0 {
		t.Fatalf("#%s 不在任何 <form> 里", formID)
	}
	end := strings.Index(html[i:], "</form>")
	if end < 0 {
		t.Fatalf("表单 #%s 没有闭合标签", formID)
	}
	return html[open : i+end]
}

// orderFormHiddenFields 取出某段 HTML 里所有 input 的 name/value（hidden 已由调用方筛过）。
func orderFormHiddenFields(block string) url.Values {
	out := url.Values{}
	for _, tag := range orderInputRe.FindAllString(block, -1) {
		attrs := orderAttrsOf(tag)
		name := attrs["name"]
		if name == "" {
			continue
		}
		out.Set(name, attrs["value"])
	}
	return out
}

// orderSelectOptionValues 取 name=<name> 的下拉选项值（按页面顺序）。
func orderSelectOptionValues(t *testing.T, block, name string) []string {
	t.Helper()
	for _, m := range orderSelectRe.FindAllStringSubmatch(block, -1) {
		attrs := orderAttrsOf(m[0])
		if attrs["name"] != name {
			continue
		}
		var out []string
		for _, opt := range orderOptionRe.FindAllString(m[1], -1) {
			if v := orderAttrsOf(opt)["value"]; v != "" {
				out = append(out, v)
			}
		}
		if len(out) == 0 {
			t.Fatalf("下拉 name=%q 没有任何非空选项", name)
		}
		return out
	}
	t.Fatalf("页面里找不到 name=%q 的下拉", name)
	return nil
}

// orderHTMLPost 按标准表单编码提交，返回响应。
func orderHTMLPost(t *testing.T, base, path string, form url.Values) *http.Response {
	t.Helper()
	res, err := http.Post(base+path, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("提交 %s 失败: %v", path, err)
	}
	return res
}

func orderMustReadBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("读取响应体失败: %v", err)
	}
	return string(b)
}

// orderChainEngine 装配真实页面与写端点：页面走生产渲染器，写端点声明 HTML 片段。
func orderChainEngine(t *testing.T, listPage gin.HandlerFunc, routes func(e *gin.Engine, h *orderhttp.Handle), h *orderhttp.Handle) (*httptest.Server, func()) {
	t.Helper()
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath:     support.NewComponentTestConfig(t),
		GinMode:        gin.TestMode,
		InitComponents: true,
		RouteRegistrar: func(e *gin.Engine) {
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			e.Use(pageview.FailureMiddleware(adminui.RenderFailure))
			e.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
			e.GET("/admin/orders", pageview.Slot(orderhttp.OrderListPageTemplate, adminui.RenderPage), listPage)
			routes(e, h)
		},
	})
	if err != nil {
		t.Fatalf("组件初始化失败: %v", err)
	}
	server := httptest.NewServer(engine)
	return server, func() { server.Close(); cleanup() }
}

func TestOrderWriteChainFromRenderedPage(t *testing.T) {
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))

	_, variantID := f.addProduct(t, "链式证据商品", 30, 50)
	mkOrder := func(email string) uint64 {
		req := f.createBaseReq(variantID, 1)
		req.CustomerEmail = email
		res, cerr := f.orders.CreateOrder(ctx, req)
		if cerr != nil {
			t.Fatalf("造单失败（%s）：%v", email, cerr)
		}
		return res.ID
	}
	bulkA := mkOrder("chain-bulk-a@example.com")
	bulkB := mkOrder("chain-bulk-b@example.com")
	detail := mkOrder("chain-detail@example.com")

	h := orderhttp.NewHandle(f.orders)
	listPage := orderhttp.NewOrderPageHandle(f.orders, projects, nil)
	server, stop := orderChainEngine(t, listPage.OrdersPage, func(e *gin.Engine, h *orderhttp.Handle) {
		e.POST("/api/order/status", pageview.HTMLFragment, h.ChangeStatus)
		e.POST("/api/order/cancel", pageview.HTMLFragment, h.CancelOrder)
	}, h)
	defer stop()

	res, err := http.Get(fmt.Sprintf("%s/admin/orders?project=%s&orderId=%d", server.URL, f.projectID, detail))
	if err != nil {
		t.Fatalf("取列表页失败: %v", err)
	}
	html := orderMustReadBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("列表页应 200，实际 %d", res.StatusCode)
	}

	// ① 批量表单：下拉挂在承载表单上，按钮用 hx-include 带上列表表单与勾选行。
	bulkForm := orderFormBlock(t, html, "ord-bulk-status-form")
	statusValues := orderSelectOptionValues(t, html, "toStatus")
	targetStatus := statusValues[0]
	if targetStatus != "paid" && targetStatus != "shipped" && targetStatus != "completed" {
		t.Fatalf("批量流转的目标状态应从通用流转里选，实际首项 %q", targetStatus)
	}

	bulkReq := orderFormHiddenFields(bulkForm)
	bulkReq.Set("toStatus", targetStatus)
	bulkReq.Set("remark", "链式证据批量流转")
	bulkReq.Add("ids", strconv.FormatUint(bulkA, 10))
	bulkReq.Add("ids", strconv.FormatUint(bulkB, 10))
	res = orderHTMLPost(t, server.URL, "/api/order/status", bulkReq)
	body := orderMustReadBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("批量流转应 200，实际 %d：%s", res.StatusCode, body)
	}
	if !strings.Contains(body, `role="status"`) {
		t.Fatalf("批量流转应回 Jet 成功片段：%s", body)
	}
	for _, id := range []uint64{bulkA, bulkB} {
		if got := f.orderStatusOf(t, id); got != targetStatus {
			t.Errorf("订单 %d 流转后应为 %s，实际 %s", id, targetStatus, got)
		}
	}

	// ② 详情表单：orderId 是数字字面量，控制器按 uint64 绑定。
	detailBlock := orderDetailStatusForm(t, html)
	detailReq := orderFormHiddenFields(detailBlock)
	detailValues := orderSelectOptionValues(t, detailBlock, "toStatus")
	if detailValues[0] != targetStatus {
		t.Fatalf("同一个 pending 单的目标状态两侧应一致：批量 %q / 详情 %q", targetStatus, detailValues[0])
	}
	detailReq.Set("toStatus", detailValues[0])
	detailReq.Set("remark", "")
	res = orderHTMLPost(t, server.URL, "/api/order/status", detailReq)
	body = orderMustReadBody(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("详情流转应 200（orderId 必须是数字），实际 %d：%s", res.StatusCode, body)
	}
	if got := f.orderStatusOf(t, detail); got != targetStatus {
		t.Errorf("订单 %d 详情流转后应为 %s，实际 %s", detail, targetStatus, got)
	}
}

// orderDetailStatusForm 取详情区块里那张提交状态流转、且带 orderId 的表单。
// 页面里还有批量流转按钮也 POST 同一端点，靠 orderId 区分单条与批量。
func orderDetailStatusForm(t *testing.T, html string) string {
	t.Helper()
	const marker = `hx-post="/api/order/status"`
	for offset := 0; ; {
		i := strings.Index(html[offset:], marker)
		if i < 0 {
			t.Fatalf("页面里找不到带 orderId 的 /api/order/status 表单")
		}
		i += offset
		open := strings.LastIndex(html[:i], "<form")
		end := strings.Index(html[i:], "</form>")
		if open >= 0 && end >= 0 {
			block := html[open : i+end]
			if strings.Contains(block, `name="orderId"`) {
				return block
			}
		}
		offset = i + len(marker)
	}
}
