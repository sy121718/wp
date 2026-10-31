package feature

import (
	"bytes"
	"context"
	"fmt"
	"go_wp/internal/adminui"
	json "go_wp/pkg/jsonx"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	orderhttp "go_wp/internal/module/order/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

type orderChainCapture struct {
	mu     sync.Mutex
	body   []string
	status []int
	path   []string
}

func (cap *orderChainCapture) add(path, body string, status int) {
	cap.mu.Lock()
	defer cap.mu.Unlock()
	cap.body = append(cap.body, body)
	cap.status = append(cap.status, status)
	cap.path = append(cap.path, path)
}

func (cap *orderChainCapture) nth(t *testing.T, n int) (string, int) {
	t.Helper()
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if n >= len(cap.body) {
		t.Fatalf("服务端只收到 %d 个写请求，取不到第 %d 个 —— 链在基座那一环就断了（路径：%v）",
			len(cap.body), n, cap.path)
	}
	return cap.body[n], cap.status[n]
}

func orderChainFormSlice(t *testing.T, html, marker string) string {
	t.Helper()
	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatalf("渲染出来的列表页里找不到按钮声明 %s", marker)
	}
	open := strings.LastIndex(html[:i], "<form")
	if open < 0 {
		t.Fatalf("按钮 %s 不在任何 <form> 里", marker)
	}
	end := strings.Index(html[i:], "</form>")
	if end < 0 {
		t.Fatalf("按钮 %s 所在的 <form> 没有闭合标签", marker)
	}
	return html[open : i+end]
}

func orderChainButtonAttrs(t *testing.T, html, api, mode string) map[string]string {
	t.Helper()
	for _, tag := range couponButtonRe.FindAllString(html, -1) {
		if couponAttrOf(tag, "data-write-url") != api || couponAttrOf(tag, "data-write") != mode {
			continue
		}
		return couponAttrsOf(tag)
	}
	t.Fatalf("渲染出来的页面里找不到 data-write-url=%s 且 data-write=%s 的按钮", api, mode)
	return nil
}

func orderChainSelectOptions(t *testing.T, block, name string) []string {
	t.Helper()
	open := strings.Index(block, `name="`+name+`"`)
	if open < 0 {
		t.Fatalf("段内找不到 name=%q 的下拉", name)
	}
	rest := block[open:]
	if end := strings.Index(rest, "</select>"); end >= 0 {
		rest = rest[:end]
	}
	var out []string
	for _, tag := range couponOptionRe.FindAllString(rest, -1) {
		if v := couponAttrOf(tag, "value"); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		t.Fatalf("name=%q 的下拉没有任何选项（值都空？）", name)
	}
	return out
}

type orderChainFields []couponChainField

func TestOrderJSONWriteChainTemplateToHandler(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("本机没有 node：链式证据要求真实基座 write-base.js，跳过而不是用桩冒充")
	}
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
	cancelC := mkOrder("chain-cancel@example.com")
	detailD := mkOrder("chain-detail@example.com")

	cap := &orderChainCapture{}
	h := orderhttp.NewHandle(f.orders)
	listPage := orderhttp.NewOrderPageHandle(f.orders, projects, nil)

	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		ConfigPath:     support.NewComponentTestConfig(t),
		GinMode:        gin.TestMode,
		InitComponents: true,
		RouteRegistrar: func(e *gin.Engine) {
			e.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			e.Use(func(c *gin.Context) {
				body, _ := io.ReadAll(c.Request.Body)
				c.Request.Body = io.NopCloser(bytes.NewReader(body))
				c.Next()

				if c.Request.Method == http.MethodPost {
					cap.add(c.Request.URL.Path, string(body), c.Writer.Status())
				}
			})
			e.GET("/admin/orders", adminui.PageSlot(orderhttp.OrderListPageTemplate), listPage.OrdersPage)
			e.POST("/api/order/status", h.ChangeStatus)
			e.POST("/api/order/cancel", h.CancelOrder)
		},
	})
	if err != nil {
		t.Fatalf("组件初始化失败: %v", err)
	}
	defer cleanup()

	server := httptest.NewServer(engine)
	defer server.Close()

	pageURL := fmt.Sprintf("%s/admin/orders?project=%s&orderId=%d", server.URL, f.projectID, detailD)
	pageRes, err := http.Get(pageURL)
	if err != nil {
		t.Fatalf("取列表页失败: %v", err)
	}
	pageHTMLRaw, _ := io.ReadAll(pageRes.Body)
	pageRes.Body.Close()
	if pageRes.StatusCode != http.StatusOK {
		t.Fatalf("列表页应 200（渲染要真的成功才能取到属性），实际 %d", pageRes.StatusCode)
	}
	html := string(pageHTMLRaw)

	bulkForm := orderChainFormSlice(t, html, `data-write-url="/api/order/status"`+` data-write="bulk"`)
	bulkFormAttrs := couponAttrsOf(bulkForm[:strings.Index(bulkForm, ">")+1])
	bulkStatusSubmitter := orderChainButtonAttrs(t, html, "/api/order/status", "bulk")
	bulkCancelSubmitter := orderChainButtonAttrs(t, html, "/api/order/cancel", "bulk")

	detailForm := orderChainFormSlice(t, html, `data-write-url="/api/order/status"`+` data-write="create"`)
	detailFormAttrs := couponAttrsOf(detailForm[:strings.Index(detailForm, ">")+1])

	detailSubmitter := map[string]string{}
	for _, tag := range couponButtonRe.FindAllString(detailForm, -1) {
		if couponAttrOf(tag, "type") == "submit" {
			detailSubmitter = couponAttrsOf(tag)
			break
		}
	}
	if len(detailSubmitter) == 0 {
		t.Fatal("详情流转表单里找不到提交按钮")
	}

	for _, id := range []uint64{bulkA, bulkB, cancelC, detailD} {
		if !strings.Contains(html, fmt.Sprintf(`name="ids" value="%d"`, id)) {
			t.Fatalf("列表页里没有订单 %d 的行勾选框（name=ids value=%d）", id, id)
		}
	}

	bulkStatusOptions := orderChainSelectOptions(t, bulkForm, "toStatus")
	detailStatusOptions := orderChainSelectOptions(t, detailForm, "toStatus")
	if bulkStatusOptions[0] != detailStatusOptions[0] {
		t.Fatalf("同一个 pending 单的目标状态两侧应一致：批量 %v / 详情 %v", bulkStatusOptions, detailStatusOptions)
	}
	targetStatus := bulkStatusOptions[0]

	cases := []map[string]any{
		{
			"name":           "bulk-status",
			"formAttrs":      bulkFormAttrs,
			"submitterAttrs": bulkStatusSubmitter,
			"fields": orderChainFields{
				{Name: "ids", Value: strconv.FormatUint(bulkA, 10), Checked: true, Type: "checkbox"},
				{Name: "ids", Value: strconv.FormatUint(bulkB, 10), Checked: true, Type: "checkbox"},
				{Name: "toStatus", Value: targetStatus, Tag: "select"},
				{Name: "remark", Value: "链式证据批量流转"},
			},
		},
		{
			"name":           "bulk-cancel",
			"formAttrs":      bulkFormAttrs,
			"submitterAttrs": bulkCancelSubmitter,
			"fields": orderChainFields{
				{Name: "ids", Value: strconv.FormatUint(cancelC, 10), Checked: true, Type: "checkbox"},
				{Name: "remark", Value: "链式证据批量取消"},
			},
		},
		{
			"name":           "detail-status",
			"formAttrs":      detailFormAttrs,
			"submitterAttrs": detailSubmitter,
			"fields": orderChainFields{
				{Name: "orderId", Value: strconv.FormatUint(detailD, 10)},
				{Name: "toStatus", Value: targetStatus, Tag: "select"},
				{Name: "remark", Value: ""},
			},
		},
	}
	fixturePath := filepath.Join(t.TempDir(), "fixture.json")
	rawFixture, _ := json.Marshal(map[string]any{
		"uiDir": filepath.Join("..", "..", "..", "..", "internal", "templates", "static", "js", "ui"),
		"base":  server.URL,
		"csrf":  "CHAIN-TOKEN",
		"cases": cases,
	})
	if err := os.WriteFile(fixturePath, rawFixture, 0o600); err != nil {
		t.Fatalf("写 fixture 失败: %v", err)
	}
	out, err := exec.Command(node, "testdata/coupon_json_write_chain_driver.cjs", fixturePath).Output()
	if err != nil {
		t.Fatalf("驱动脚本失败: %v\n%s", err, string(out))
	}
	var report struct {
		Results []struct {
			Name        string   `json:"name"`
			FetchErrors []string `json:"fetchErrors"`
			Requests    []struct {
				URL  string         `json:"url"`
				Body map[string]any `json:"body"`
			} `json:"requests"`
			StatusText string `json:"statusText"`
		} `json:"results"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("驱动输出不是 JSON: %v\n%s", err, string(out))
	}
	if report.Error != "" {
		t.Fatalf("驱动报错: %s", report.Error)
	}
	if len(report.Results) != len(cases) {
		t.Fatalf("应跑 %d 个用例，实际 %d：%s", len(cases), len(report.Results), string(out))
	}
	for _, r := range report.Results {
		if len(r.FetchErrors) > 0 || len(r.Requests) != 1 {
			t.Fatalf("%s 应恰好发一个请求且 fetch 不报错，实际 requests=%d errors=%v",
				r.Name, len(r.Requests), r.FetchErrors)
		}
	}

	assertBodyKeys := func(t *testing.T, where, body string, want ...string) map[string]string {
		t.Helper()
		got, err := parseMultipartFormBody(body)
		if err != nil {
			t.Fatalf("%s 的请求体解析失败: %v（body=%s）", where, err, body)
		}
		if len(got) != len(want) {
			t.Fatalf("%s 的请求体字段应为 %v，实际 %v（body=%s）", where, want, keysOf(got), body)
		}
		for _, name := range want {
			if _, ok := got[name]; !ok {
				t.Fatalf("%s 的请求体缺少字段 %s：body=%s", where, name, body)
			}
		}
		return got
	}
	assertField := func(t *testing.T, where, name, got, want string) {
		t.Helper()
		if strings.TrimSpace(got) != want {
			t.Errorf("%s 的 %s 应为 %q，实际 %q", where, name, want, got)
		}
	}
	assertUintLiteral := func(t *testing.T, where, name, got string, want uint64) {
		t.Helper()
		assertField(t, where, name, got, strconv.FormatUint(want, 10))
	}

	bulkStatusBody, bulkStatus := cap.nth(t, 0)
	if bulkStatus != http.StatusOK {
		t.Fatalf("批量流转应 200，实际 %d：%s", bulkStatus, bulkStatusBody)
	}
	bulkStatusJSON := assertBodyKeys(t, "批量流转", bulkStatusBody, "ids", "toStatus", "remark")
	assertField(t, "批量流转", "toStatus", bulkStatusJSON["toStatus"], targetStatus)
	assertField(t, "批量流转", "remark", bulkStatusJSON["remark"], "链式证据批量流转")

	if got := bulkStatusJSON["ids"]; got != fmt.Sprintf("%d,%d", bulkA, bulkB) {
		t.Errorf("批量流转的 ids 应是 [%d %d]，实际 %q", bulkA, bulkB, got)
	}
	for _, id := range []uint64{bulkA, bulkB} {
		if got := f.orderStatusOf(t, id); got != targetStatus {
			t.Errorf("订单 %d 流转后应为 %s，实际 %s", id, targetStatus, got)
		}
	}

	cancelBody, cancelStatus := cap.nth(t, 1)
	if cancelStatus != http.StatusOK {
		t.Fatalf("批量取消应 200，实际 %d：%s", cancelStatus, cancelBody)
	}
	cancelJSON := assertBodyKeys(t, "批量取消", cancelBody, "ids", "remark")
	assertField(t, "批量取消", "remark", cancelJSON["remark"], "链式证据批量取消")
	if got := cancelJSON["ids"]; got != fmt.Sprintf("%d", cancelC) {
		t.Errorf("批量取消的 ids 应是 [%d]，实际 %q", cancelC, got)
	}
	if got := f.orderStatusOf(t, cancelC); got != "cancelled" {
		t.Errorf("订单 %d 批量取消后应为 cancelled，实际 %s", cancelC, got)
	}

	detailBody, detailStatus := cap.nth(t, 2)
	if detailStatus != http.StatusOK {
		t.Fatalf("详情流转应 200（orderId 必须是数字，字符串会绑定失败），实际 %d：%s", detailStatus, detailBody)
	}

	detailJSON := assertBodyKeys(t, "详情流转", detailBody, "orderId", "toStatus")
	assertUintLiteral(t, "详情流转", "orderId", string(detailJSON["orderId"]), detailD)
	assertField(t, "详情流转", "toStatus", detailJSON["toStatus"], targetStatus)
	if got := f.orderStatusOf(t, detailD); got != targetStatus {
		t.Errorf("订单 %d 详情流转后应为 %s，实际 %s", detailD, targetStatus, got)
	}

	for _, r := range report.Results {
		if strings.Contains(r.StatusText, "操作失败") {
			t.Errorf("%s 不应显示失败文案，实际状态区：%q", r.Name, r.StatusText)
		}
	}
}
