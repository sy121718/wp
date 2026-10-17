package feature

// order_json_write_chain_test.go — 订单列表页「写动作迁 JSON」的**链式证据**（task-98 S2 / task-111）。
//
// 静态核验只能证明「属性写了字」；这条用例把整条链真跑一遍：
//
//	真渲染列表页（真 handler + 真 Jet）→ 从渲染出来的 HTML 里取**按钮/表单的原始属性**
//	→ 交给**真基座** internal/templates/static/js/ui/json_write.js（含真 confirm.js 的确认协议）
//	→ 它的 fetch 真打到 httptest 服务器上的**真 handler**
//	→ 服务端记录实际收到的 JSON 体（类型逐个断言）+ 响应码；数据库的最终状态也断言。
//
// 为什么非这样不可（S1 的 P1 就是这个形态）：属性看着齐全、静态检查也过，但少一个
// data-write-number 或 data-write-extra 写错，基座发出去的体就与 handler 的期望差一层
//（字符串 vs 数字、字段干脆不在），页面上只看到一句「操作失败」。这条链上任何一环松动，本用例都会红。
//
// 三个用例覆盖两种模式：
//   · bulk-status：批量条的两个勾选 + 目标状态下拉 + 备注框 → /api/order/status 的**批档**；
//   · bulk-cancel：同一个表单的另一个按钮（data-write-extra 只有 remark）→ /api/order/cancel；
//   · detail-status：详情里的流转表单（create 模式）→ /api/order/status 的**单档**，
//     关键在 orderId 必须是**数字**（模板漏写 data-write-number 就变字符串 → 绑定失败）。
//
// 驱动脚本沿用 S1 的 public/test/order/feature/testdata/coupon_json_write_chain_driver.cjs：
// 它本身是**通用**的（fixture 只描述表单/按钮属性与字段），文件名里的 coupon 是历史产物。
// DOM 桩手法抄自 internal/templates/static/js/ui/json_write.test.cjs（node:test + vm + 极简 DOM）。
// 没有 node 时**跳过并说明**（不伪装成绿）：本用例要的就是真实基座，桩掉它等于测了个假东西。

import (
	"bytes"
	"context"
	"fmt"
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

// orderChainCapture 记录服务端**实际收到**的请求体与响应码（链的另一端）。
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

// nth 第 n 个写请求（按服务端收到的先后）。
//
// 必须按序号取而不能「取最后一次」：三个用例在同一个驱动进程里按顺序跑，全部发完才轮到
// Go 侧断言 —— 只取最后一次会让批量段读到详情段的请求体（S1 实测踩过这个坑）。
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

// orderChainFormSlice 取「包含某条按钮声明的那张表单」的片段（含开闭标签）。
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

// orderChainButtonAttrs 取「端点 + 模式」都匹配的那个按钮的属性表。
//
// 同一个端点（/api/order/status）在页面上出现两次：批量条上 mode=bulk、详情里 mode=create。
// 只看端点会把两者搞混，所以两级都匹配；找不到就直说缺失，不静默取到错的按钮。
func orderChainButtonAttrs(t *testing.T, html, api, mode string) map[string]string {
	t.Helper()
	for _, tag := range couponButtonRe.FindAllString(html, -1) {
		if couponAttrOf(tag, "data-write-api") != api || couponAttrOf(tag, "data-write-mode") != mode {
			continue
		}
		return couponAttrsOf(tag)
	}
	t.Fatalf("渲染出来的页面里找不到 data-write-api=%s 且 data-write-mode=%s 的按钮", api, mode)
	return nil
}

// orderChainSelectOptions 取段内某个下拉的选项值（按渲染顺序）。
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

// orderChainFields 按渲染出来的名字造字段，用于 driver 的 fixture。
type orderChainFields []couponChainField

// TestOrderJSONWriteChainTemplateToHandler 链式证据主用例。
func TestOrderJSONWriteChainTemplateToHandler(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("本机没有 node：链式证据要求真实基座 json_write.js，跳过而不是用桩冒充")
	}
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))

	// 四个用例各用一张**独立**的订单：批量两单（流转）、批量一单（取消）、详情一单（流转）。
	// 共用一单会让后一个用例改掉前一个用例的断言对象 —— S1 就是这么踩到的。
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
				// 只记写请求的实际内容与响应码：这是「基座发出去的到底是什么类型」的权威来源。
				if c.Request.Method == http.MethodPost {
					cap.add(c.Request.URL.Path, string(body), c.Writer.Status())
				}
			})
			e.GET("/admin/orders", listPage.OrdersPage)
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

	// ① 真渲染列表页（详情块靠 orderId 参数展开，不新开路由）。
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

	// ② 取原始属性：不手抄任何 data-*，模板少写一个属性本用例就必然跟着红。
	bulkForm := orderChainFormSlice(t, html, `data-write-api="/api/order/status"`+` data-write-mode="bulk"`)
	bulkFormAttrs := couponAttrsOf(bulkForm[:strings.Index(bulkForm, ">")+1])
	bulkStatusSubmitter := orderChainButtonAttrs(t, html, "/api/order/status", "bulk")
	bulkCancelSubmitter := orderChainButtonAttrs(t, html, "/api/order/cancel", "bulk")

	detailForm := orderChainFormSlice(t, html, `data-write-api="/api/order/status"`+` data-write-mode="create"`)
	detailFormAttrs := couponAttrsOf(detailForm[:strings.Index(detailForm, ">")+1])
	// 详情表单的声明落在**表单**上（批量条才把端点放在按钮上，因为同一张表单有两个动作）——
	// 所以这里取表单里第一个 submit 按钮的属性即可，base 的 attrFrom 是「按钮优先、表单兜底」。
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

	// 行的勾选框必须真的带着这四个 id 渲染出来（不然「勾选两单」这个动作本身是假的）。
	for _, id := range []uint64{bulkA, bulkB, cancelC, detailD} {
		if !strings.Contains(html, fmt.Sprintf(`name="ids" value="%d"`, id)) {
			t.Fatalf("列表页里没有订单 %d 的行勾选框（name=ids value=%d）", id, id)
		}
	}
	// 目标状态：批量下拉取渲染出来的第一个选项（pending → paid 是通用流转的第一条边）。
	bulkStatusOptions := orderChainSelectOptions(t, bulkForm, "toStatus")
	detailStatusOptions := orderChainSelectOptions(t, detailForm, "toStatus")
	if bulkStatusOptions[0] != detailStatusOptions[0] {
		t.Fatalf("同一个 pending 单的目标状态两侧应一致：批量 %v / 详情 %v", bulkStatusOptions, detailStatusOptions)
	}
	targetStatus := bulkStatusOptions[0]

	// ③ 跑真实基座：三个用例（批量流转 / 批量取消 / 详情流转）。
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

	// ④ 链的另一端：服务端真正收到的 JSON 体（类型逐个断言）。
	assertBodyKeys := func(t *testing.T, where, body string, want ...string) map[string]json.RawMessage {
		t.Helper()
		got := map[string]json.RawMessage{}
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("%s 的请求体不是 JSON 对象: %s", where, body)
		}
		if len(got) != len(want) {
			t.Fatalf("%s 的请求体字段应为 %v，实际 %s", where, want, body)
		}
		for _, name := range want {
			if _, ok := got[name]; !ok {
				t.Fatalf("%s 的请求体缺少字段 %s：%s", where, name, body)
			}
		}
		return got
	}
	assertStringLiteral := func(t *testing.T, where, name, raw, want string) {
		t.Helper()
		if got := strings.TrimSpace(raw); got != strconv.Quote(want) {
			t.Errorf("%s 的 %s 应是字符串字面 %q，实际 %s", where, name, want, got)
		}
	}
	assertUintLiteral := func(t *testing.T, where, name, raw string, want uint64) {
		t.Helper()
		if got := strings.TrimSpace(raw); got != strconv.FormatUint(want, 10) {
			t.Errorf("%s 的 %s 应是**数字**字面 %d（模板漏写 data-write-number 就会是字符串），实际 %s",
				where, name, want, got)
		}
	}

	// 批量流转：恰好 ids + toStatus + remark 三个字段，且 toStatus 是字符串。
	bulkStatusBody, bulkStatus := cap.nth(t, 0)
	if bulkStatus != http.StatusOK {
		t.Fatalf("批量流转应 200，实际 %d：%s", bulkStatus, bulkStatusBody)
	}
	bulkStatusJSON := assertBodyKeys(t, "批量流转", bulkStatusBody, "ids", "toStatus", "remark")
	assertStringLiteral(t, "批量流转", "toStatus", string(bulkStatusJSON["toStatus"]), targetStatus)
	assertStringLiteral(t, "批量流转", "remark", string(bulkStatusJSON["remark"]), "链式证据批量流转")
	if got := strings.TrimSpace(string(bulkStatusJSON["ids"])); got !=
		fmt.Sprintf(`["%d","%d"]`, bulkA, bulkB) {
		t.Errorf("批量流转的 ids 应是字符串数组 [%d %d]，实际 %s", bulkA, bulkB, got)
	}
	for _, id := range []uint64{bulkA, bulkB} {
		if got := f.orderStatusOf(t, id); got != targetStatus {
			t.Errorf("订单 %d 流转后应为 %s，实际 %s", id, targetStatus, got)
		}
	}

	// 批量取消：字段恰好 ids + remark（按钮的 data-write-extra 只声明了 remark）——
	// 多出 toStatus 说明 extra 被表单上的声明污染了。
	cancelBody, cancelStatus := cap.nth(t, 1)
	if cancelStatus != http.StatusOK {
		t.Fatalf("批量取消应 200，实际 %d：%s", cancelStatus, cancelBody)
	}
	cancelJSON := assertBodyKeys(t, "批量取消", cancelBody, "ids", "remark")
	assertStringLiteral(t, "批量取消", "remark", string(cancelJSON["remark"]), "链式证据批量取消")
	if got := strings.TrimSpace(string(cancelJSON["ids"])); got != fmt.Sprintf(`["%d"]`, cancelC) {
		t.Errorf("批量取消的 ids 应是字符串数组 [%d]，实际 %s", cancelC, got)
	}
	if got := f.orderStatusOf(t, cancelC); got != "cancelled" {
		t.Errorf("订单 %d 批量取消后应为 cancelled，实际 %s", cancelC, got)
	}

	// 详情流转（create 模式）：字段恰好 orderId + toStatus + remark，且 orderId 必须是数字。
	detailBody, detailStatus := cap.nth(t, 2)
	if detailStatus != http.StatusOK {
		t.Fatalf("详情流转应 200（orderId 必须是数字，字符串会绑定失败），实际 %d：%s", detailStatus, detailBody)
	}
	detailJSON := assertBodyKeys(t, "详情流转", detailBody, "orderId", "toStatus", "remark")
	assertUintLiteral(t, "详情流转", "orderId", string(detailJSON["orderId"]), detailD)
	assertStringLiteral(t, "详情流转", "toStatus", string(detailJSON["toStatus"]), targetStatus)
	if got := f.orderStatusOf(t, detailD); got != targetStatus {
		t.Errorf("订单 %d 详情流转后应为 %s，实际 %s", detailD, targetStatus, got)
	}

	// ⑤ JS 侧结论：三个用例都不该把后端那句话当成失败展示（受控文案不是「操作失败」）。
	for _, r := range report.Results {
		if strings.Contains(r.StatusText, "操作失败") {
			t.Errorf("%s 不应显示失败文案，实际状态区：%q", r.Name, r.StatusText)
		}
	}
}
