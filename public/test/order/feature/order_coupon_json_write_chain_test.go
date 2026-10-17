package feature

// order_coupon_json_write_chain_test.go — task-98 S1 的**链式证据**（Sol 报的 P1 就是这条链断的）。
//
// 静态核验只能证明「属性写了字」；这条用例把整条链真跑一遍：
//
//	真渲染列表页（真 handler + 真 Jet）→ 从渲染出来的 HTML 里取**按钮/表单的原始属性**
//	→ 交给**真基座** internal/templates/static/js/ui/json_write.js（含真 confirm.js 的确认协议）
//	→ 它的 fetch 真打到 httptest 服务器上的**真 handler**
//	→ 服务端记录下来实际收到的 JSON 体（类型逐个断言）+ 响应码；数据库的最终状态也断言。
//
// 为什么非这样不可（P1 的真实形态）：批量启停按钮少写一个 data-write-number=status 时，
// 属性看着齐全、静态检查也过，但基座把 <select> 的值按字符串发出去，服务端只认字面 0/1 →
// 整批 400，而页面上只看到一句「操作失败」。这条链上任何一环松动，本用例都会红。
//
// 驱动脚本 public/test/order/feature/testdata/coupon_json_write_chain_driver.cjs 的 DOM 桩
// 手法抄自同目录 json_write.test.cjs（node:test + vm + 极简 DOM，无浏览器、无依赖）。
// 没有 node 时**跳过并说明**（不伪装成绿）：本用例要的就是真实基座，桩掉它等于测了个假东西。

import (
	"bytes"
	"context"
	json "go_wp/pkg/jsonx"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderhttp "go_wp/internal/module/order/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

// couponChainCapture 记录服务端**实际收到**的请求体与响应码（链的另一端）。
type couponChainCapture struct {
	mu      sync.Mutex
	body    []string
	status  []int
	patched []string
}

func (cap *couponChainCapture) add(path, body string, status int) {
	cap.mu.Lock()
	defer cap.mu.Unlock()
	cap.body = append(cap.body, body)
	cap.status = append(cap.status, status)
	cap.patched = append(cap.patched, path)
}

// nth 第 n 个写请求（path 前缀匹配 /api/order/coupon/，按服务端收到的先后）。
//
// 必须按序号取而不能「取最后一次」：三个用例在同一个驱动进程里按顺序跑，全部发完
// 才轮到 Go 侧断言 —— 只取最后一次会让批量用例读到行内删除的请求体（实测踩过：
// 批量段的 status/ids/projectId 全都"缺失"，看着像模板漏声明，其实是判据取错了对象）。
func (cap *couponChainCapture) nth(t *testing.T, n int) (string, int) {
	t.Helper()
	cap.mu.Lock()
	defer cap.mu.Unlock()
	var seen int
	for i := range cap.patched {
		if !strings.HasPrefix(cap.patched[i], "/api/order/coupon/") {
			continue
		}
		if seen == n {
			return cap.body[i], cap.status[i]
		}
		seen++
	}
	t.Fatalf("服务端只收到 %d 个 /api/order/coupon/ 写请求，取不到第 %d 个 —— 链在基座那一环就断了", seen, n)
	return "", 0
}

// couponChainFixture 渲染真列表页，并抽出三个用例的原始属性。
type couponChainFixture struct {
	bulkFormAttrs  map[string]string
	bulkSubmitter  map[string]string
	bulkFields     []couponChainField
	bulkTarget     string
	rowFormAttrs   map[string]string
	rowSubmitter   map[string]string
	rowFields      []couponChainField
	rowStatusValue string
	delFormAttrs   map[string]string
	delSubmitter   map[string]string
	delFields      []couponChainField
}

type couponChainField struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Checked bool   `json:"checked,omitempty"`
	Tag     string `json:"tag,omitempty"`
	Type    string `json:"type,omitempty"`
}

var (
	couponAttrRe     = regexp.MustCompile(`([A-Za-z-]+)="([^"]*)"`)
	couponButtonRe   = regexp.MustCompile(`(?s)<button\b[^>]*>`)
	couponOptionRe   = regexp.MustCompile(`(?s)<option\b[^>]*>`)
	couponSelectedRe = regexp.MustCompile(`\bselected\b`)
)

// couponAttrsOf 把一个标签的属性抽成表（模板里的属性值一律双引号）。
func couponAttrsOf(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range couponAttrRe.FindAllStringSubmatch(tag, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// couponFieldsOf 段内所有具名输入控件（按出现顺序），值取渲染出来的 value。
func couponFieldsOf(t *testing.T, block string) []couponChainField {
	t.Helper()
	var out []couponChainField
	for _, tag := range couponInputTags(block) {
		name := couponAttrOf(tag, "name")
		if name == "" {
			continue
		}
		out = append(out, couponChainField{Name: name, Value: couponAttrOf(tag, "value")})
	}
	return out
}

// buildCouponChainFixture 从真渲染的 HTML 里取属性：不手抄任何 data-*，
// 模板少写一个属性本用例就必然跟着红。
func buildCouponChainFixture(t *testing.T, html, toggleID, deleteID, projectID string) *couponChainFixture {
	t.Helper()
	fx := &couponChainFixture{}

	bulkForm := couponBulkFormTag(html)
	if bulkForm == "" {
		t.Fatal("渲染出来的列表页里找不到批量表单（批量条只在有数据时渲染）")
	}
	fx.bulkFormAttrs = couponAttrsOf(bulkForm)

	// 批量启停按钮：本页两个批量按钮里带 update 端点的那个。
	var toggleButton string
	for _, tag := range couponButtonRe.FindAllString(html, -1) {
		if strings.Contains(tag, `data-write-api="/api/order/coupon/update"`) {
			toggleButton = tag
			break
		}
	}
	if toggleButton == "" {
		t.Fatal("列表页里找不到批量启停按钮（data-write-api=/api/order/coupon/update）")
	}
	fx.bulkSubmitter = couponAttrsOf(toggleButton)

	// 目标状态下拉：取「与当前状态相反」的那个选项（真页面由人挑，这里挑另一个）。
	var target string
	for _, tag := range couponOptionRe.FindAllString(html, -1) {
		value := couponAttrOf(tag, "value")
		if value == "1" && !couponSelectedRe.MatchString(tag) {
			target = value
			break
		}
	}
	if target == "" {
		for _, tag := range couponOptionRe.FindAllString(html, -1) {
			if value := couponAttrOf(tag, "value"); value == "1" {
				target, _ = value, 0
				break
			}
		}
	}
	if target == "" {
		t.Fatal("找不到目标状态下拉的 value=1 选项")
	}
	fx.bulkTarget = target
	fx.bulkFields = []couponChainField{
		{Name: "status", Value: target, Tag: "select"},
		{Name: "projectId", Value: projectID},
		{Name: "ids", Value: toggleID, Checked: true, Type: "checkbox"},
	}

	// 行内启停（create 模式）：字段就是表单自己的隐藏域，提交者是行里那个 form="<id>" 的按钮。
	rowBlock, ok := couponFormBlockExact(html, "coupon-toggle-"+toggleID)
	if !ok {
		t.Fatal("列表页里找不到行内启停表单（coupon-toggle-）")
	}
	fx.rowFormAttrs = couponAttrsOf(rowBlock[:strings.Index(rowBlock, ">")+1])
	fx.rowFields = couponFieldsOf(t, rowBlock)
	for _, f := range fx.rowFields {
		if f.Name == "status" {
			fx.rowStatusValue = f.Value
		}
	}
	if fx.rowStatusValue == "" {
		t.Fatal("行内启停表单没有 status 隐藏域（翻转目标状态靠它带出去）")
	}
	for _, tag := range couponButtonRe.FindAllString(html, -1) {
		if couponAttrOf(tag, "form") == "coupon-toggle-"+toggleID {
			fx.rowSubmitter = couponAttrsOf(tag)
			break
		}
	}
	if fx.rowSubmitter == nil {
		t.Fatal("找不到与行内启停表单关联的提交按钮（form=\"coupon-toggle-<id>\"）")
	}

	// 行内删除（single 模式）。
	delBlock, ok := couponFormBlockExact(html, "coupon-del-"+deleteID)
	if !ok {
		t.Fatal("列表页里找不到行内删除表单（coupon-del-）")
	}
	fx.delFormAttrs = couponAttrsOf(delBlock[:strings.Index(delBlock, ">")+1])
	fx.delFields = couponFieldsOf(t, delBlock)
	for _, tag := range couponButtonRe.FindAllString(html, -1) {
		if couponAttrOf(tag, "form") == "coupon-del-"+deleteID {
			fx.delSubmitter = couponAttrsOf(tag)
			break
		}
	}
	if fx.delSubmitter == nil {
		t.Fatal("找不到与行内删除表单关联的提交按钮（form=\"coupon-del-<id>\"）")
	}
	return fx
}

// TestOrderCouponJSONWriteChainTemplateToHandler 链式证据主用例。
func TestOrderCouponJSONWriteChainTemplateToHandler(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("本机没有 node：链式证据要求真实基座 json_write.js，跳过而不是用桩冒充")
	}
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))

	// 三个用例各用一张券：批量启停 / 行内启停 / 行内删除。同一张券会被前后两个用例各改一次
	// 状态，而 Go 侧是在三个用例**都跑完**之后才读库断言 —— 共用一张券时「批量那段」读到的是
	// 行内那段写下的状态（实测踩过：批量断言拿到 0，看着像基座发错，其实是断言对象被后一个用例改过）。
	bulkKeep := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "CHAIN-1", Name: "链式证据券", DiscountType: "fixed", DiscountValue: 2000,
		MinSubtotal: 9900, MaxUses: 100, PerUserLimit: 2, Remark: "链式证据",
		StartsAt: "2026-01-01 09:00", EndsAt: "2026-01-31 23:59",
	})
	keep := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "CHAIN-2", Name: "链式启停券", DiscountType: "fixed", DiscountValue: 300,
		MinSubtotal: 1000, MaxUses: 10, PerUserLimit: 1, Remark: "链式启停",
		StartsAt: "2026-01-01 09:00", EndsAt: "2026-01-31 23:59",
	})
	remove := f.addCoupon(t, &orderdto.CouponSaveReq{
		Code: "CHAIN-3", Name: "链式删除券", DiscountType: "fixed", DiscountValue: 100,
	})

	cap := &couponChainCapture{}
	h := orderhttp.NewHandle(f.orders)
	couponsPage := orderhttp.NewCouponPageHandle(f.orders, projects)

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
				cap.add(c.Request.URL.Path, string(body), c.Writer.Status())
			})
			e.GET("/admin/coupons", couponsPage.CouponsPage)
			e.POST("/api/order/coupon/update", h.UpdateCoupon)
			e.POST("/api/order/coupon/delete", h.DeleteCoupon)
		},
	})
	if err != nil {
		t.Fatalf("组件初始化失败: %v", err)
	}
	defer cleanup()

	server := httptest.NewServer(engine)
	defer server.Close()

	// ① 真渲染列表页并取原始属性。
	pageReq, _ := http.NewRequest(http.MethodGet, server.URL+"/admin/coupons?project="+f.projectID, nil)
	pageRes, err := http.DefaultClient.Do(pageReq)
	if err != nil {
		t.Fatalf("取列表页失败: %v", err)
	}
	pageHTML, _ := io.ReadAll(pageRes.Body)
	pageRes.Body.Close()
	if pageRes.StatusCode != http.StatusOK {
		t.Fatalf("列表页应 200（渲染要真的成功才能取到属性），实际 %d", pageRes.StatusCode)
	}
	fx := buildCouponChainFixture(t, string(pageHTML),
		strconv.FormatUint(keep.ID, 10), strconv.FormatUint(remove.ID, 10), f.projectID)
	fx.bulkFields[2] = couponChainField{Name: "ids", Value: strconv.FormatUint(bulkKeep.ID, 10), Checked: true, Type: "checkbox"}
	// 批量那段必须**换一个状态**才算验证了写通：拿当前状态当目标时「状态没变」既可能是写成功也可能是没写。
	bulkBefore, berr := f.orders.GetCoupon(context.Background(), bulkKeep.ID)
	if berr != nil || bulkBefore == nil {
		t.Fatalf("重读批量用例的券失败: %v", berr)
	}
	fx.bulkTarget = "0"
	if bulkBefore.Status == 0 {
		fx.bulkTarget = "1"
	}
	fx.bulkFields[0] = couponChainField{Name: "status", Value: fx.bulkTarget, Tag: "select"}

	// ② 跑真实基座：三个用例（批量启停 / 行内启停 / 行内删除）。
	fixture := map[string]any{
		"uiDir": filepath.Join("..", "..", "..", "..", "internal", "templates", "static", "js", "ui"),
		"base":  server.URL,
		"csrf":  "CHAIN-TOKEN",
		"cases": []map[string]any{
			{
				"name":           "bulk-toggle",
				"formAttrs":      fx.bulkFormAttrs,
				"submitterAttrs": fx.bulkSubmitter,
				"fields":         fx.bulkFields,
			},
			{
				"name":           "row-toggle",
				"formAttrs":      fx.rowFormAttrs,
				"submitterAttrs": fx.rowSubmitter,
				"fields":         fx.rowFields,
			},
			{
				"name":           "row-delete",
				"formAttrs":      fx.delFormAttrs,
				"submitterAttrs": fx.delSubmitter,
				"fields":         fx.delFields,
			},
		},
	}
	fixturePath := filepath.Join(t.TempDir(), "fixture.json")
	rawFixture, _ := json.Marshal(fixture)
	if err := os.WriteFile(fixturePath, rawFixture, 0o600); err != nil {
		t.Fatalf("写 fixture 失败: %v", err)
	}
	out, err := exec.Command(node, "testdata/coupon_json_write_chain_driver.cjs", fixturePath).Output()
	if err != nil {
		t.Fatalf("驱动脚本失败: %v\n%s", err, string(out))
	}
	var report struct {
		Results []struct {
			Name     string `json:"name"`
			Requests []struct {
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
	if len(report.Results) != 3 {
		t.Fatalf("应跑三个用例，实际 %d：%s", len(report.Results), string(out))
	}

	// ③ 链的另一端：服务端真正收到的 JSON 体（类型逐个断言）。
	bulkBody, bulkStatus := cap.nth(t, 0)
	if bulkStatus != http.StatusOK {
		t.Fatalf("批量启停应 200（基座发出的 status 必须是数字，否则这里会是 400），实际 %d：%s", bulkStatus, bulkBody)
	}
	bulkJSON := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(bulkBody), &bulkJSON); err != nil {
		t.Fatalf("批量请求体不是 JSON 对象: %s", bulkBody)
	}
	if got := strings.TrimSpace(string(bulkJSON["status"])); got != fx.bulkTarget {
		t.Errorf("批量 status 必须是**数字**字面 %s（字符串会被服务端判形状错），实际 %q", fx.bulkTarget, got)
	}
	if got := strings.TrimSpace(string(bulkJSON["ids"])); got != `["`+strconv.FormatUint(bulkKeep.ID, 10)+`"]` {
		t.Errorf("批量 ids 必须是字符串数组，实际 %s", got)
	}
	if got := strings.TrimSpace(string(bulkJSON["projectId"])); got != `"`+f.projectID+`"` {
		t.Errorf("批量 projectId 必须保持字符串（点名成数字会绑定失败），实际 %s", got)
	}

	// JS 侧看到的结论：受控文案不是失败文案。
	var bulkReport string
	for _, r := range report.Results {
		if r.Name == "bulk-toggle" {
			if len(r.Requests) != 1 {
				t.Fatalf("批量启停应恰好发一个请求，实际 %d 个：%v", len(r.Requests), r.Requests)
			}
			bulkReport = r.StatusText
		}
	}
	if strings.Contains(bulkReport, "操作失败") {
		t.Errorf("批量启停不应显示失败文案，实际状态区：%q", bulkReport)
	}

	// 数据库：状态改成目标值，其余字段一个都不许回退。
	after, gerr := f.orders.GetCoupon(context.Background(), bulkKeep.ID)
	if gerr != nil || after == nil {
		t.Fatalf("重读券失败: %v", gerr)
	}
	if got := strconv.Itoa(after.Status); got != fx.bulkTarget {
		t.Fatalf("批量启停后状态应改成 %s，实际 %s", fx.bulkTarget, got)
	}
	if after.Name != "链式证据券" || after.MinSubtotal != 9900 || after.Remark != "链式证据" {
		t.Fatalf("整体更新漏字段：%+v", after)
	}

	// ④ 行内启停（create 模式）：数值字段必须逐个是数字。
	rowBody, rowStatus := cap.nth(t, 1)
	if rowStatus != http.StatusOK {
		t.Fatalf("行内启停应 200，实际 %d：%s", rowStatus, rowBody)
	}
	rowJSON := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(rowBody), &rowJSON); err != nil {
		t.Fatalf("行内启停请求体不是 JSON 对象: %s", rowBody)
	}
	for _, name := range []string{"id", "discountValue", "minSubtotal", "maxUses", "perUserLimit", "status"} {
		raw, ok := rowJSON[name]
		if !ok {
			t.Fatalf("行内启停请求体缺少字段 %s：%s", name, rowBody)
		}
		if got := strings.TrimSpace(string(raw)); got == "" || got[0] < '0' || got[0] > '9' {
			t.Errorf("行内启停的 %s 必须是数字字面（模板 data-write-number 漏声明就会是字符串），实际 %s", name, got)
		}
	}
	if got := strings.TrimSpace(string(rowJSON["status"])); got != fx.rowStatusValue {
		t.Errorf("行内启停的 status 应等于渲染出来的翻转目标 %s，实际 %s（请求体：%s）", fx.rowStatusValue, got, rowBody)
	}
	afterRow, _ := f.orders.GetCoupon(context.Background(), keep.ID)
	if afterRow == nil || strconv.Itoa(afterRow.Status) != fx.rowStatusValue {
		t.Fatalf("行内启停应把状态翻转成 %s，实际 %+v", fx.rowStatusValue, afterRow)
	}

	// ⑤ 行内删除（single 模式）：只发 {id}，且 id 是数字。
	delBody, delStatus := cap.nth(t, 2)
	if delStatus != http.StatusOK {
		t.Fatalf("行内删除应 200，实际 %d：%s", delStatus, delBody)
	}
	delJSON := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(delBody), &delJSON); err != nil {
		t.Fatalf("行内删除请求体不是 JSON 对象: %s", delBody)
	}
	if len(delJSON) != 1 {
		t.Errorf("single 模式只应发 {id}，实际字段：%s", delBody)
	}
	if got := strings.TrimSpace(string(delJSON["id"])); got != strconv.FormatUint(remove.ID, 10) {
		t.Errorf("行内删除的 id 应是数字 %d，实际 %s", remove.ID, got)
	}
	if couponAlive(t, f, remove.ID) {
		t.Fatal("行内删除后券应已不存在（链走通了才删得掉）")
	}
}
