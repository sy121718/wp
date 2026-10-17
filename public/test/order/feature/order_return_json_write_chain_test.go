package feature

// order_return_json_write_chain_test.go — 退货页「写动作迁 JSON」的**链式证据**（task-98 S3 / task-119）。
//
// 与 S1/S2 同一条链，只是换了页面与端点：
//
//	真渲染退货页（真 handler + 真 Jet）→ 从渲染出来的 HTML 里取**表单/按钮的原始属性**
//	→ 交给**真基座** internal/templates/static/js/ui/json_write.js
//	→ 它的 fetch 真打到 httptest 服务器上的**真 handler**
//	→ 服务端记录实际收到的 JSON 体（类型逐个断言）+ 响应码；数据库的最终状态也断言。
//
// 本页比列表页多了一层形状风险，所以非这样测不可：
//
//   · **`autoReceive` 是布尔**：DOM 里它是 `value="1"` 的复选框（字符串），服务端按 bool 严格绑定。
//     模板少写 `data-write-bool="autoReceive"`，基座就会把 "1" 当字符串发出去 → 整条请求 400，
//     而页面上只看到「操作失败」。这一条只有真基座能证。
//   · **`returnId` 是数字**：DOM 的值恒为字符串，模板少写 `data-write-number="returnId"` 就是绑定失败。
//   · **批量两个按钮各投各的端点**：端点声明落在按钮上（表单上刻意不声明），且批量体里不得出现
//     `autoReceive`（批档白名单里没有它，带上会整批 400 —— 批量不自动入库是硬语义）。
//
// 驱动脚本沿用 S1 的 testdata/coupon_json_write_chain_driver.cjs：它本身是**通用**的
// （fixture 只描述表单/按钮属性与字段），文件名里的 coupon 是历史产物。
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
	"testing"

	"github.com/gin-gonic/gin"

	orderdto "go_wp/internal/module/order/dto"
	orderhttp "go_wp/internal/module/order/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

// TestReturnJSONWriteChainTemplateToHandler 退货页链式证据主用例（单档三按钮 + 批档两按钮）。
func TestReturnJSONWriteChainTemplateToHandler(t *testing.T) {
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
	_, variantID := f.addProduct(t, "退货链式证据商品", 40, 60)

	// 五张**独立**申请：每个用例改的是自己那张，互不干扰（S1 共用一单踩过这个坑）。
	mkReturn := func(email string) uint64 {
		orderID, userID, itemID := f.paidOrder(t, email, variantID, 1)
		return f.requestReturn(t, orderID, userID, itemID, 1).ID
	}
	approveA := mkReturn("chain-ret-approve@example.com")
	rejectB := mkReturn("chain-ret-reject@example.com")
	receiveC := mkReturn("chain-ret-receive@example.com")
	bulkE := mkReturn("chain-ret-bulk-e@example.com")
	bulkF := mkReturn("chain-ret-bulk-f@example.com")
	// receiveC 必须先被同意，"确认收货"那张表单才会渲染出来（表单按状态出现）。
	if _, aerr := f.orders.ApproveReturn(ctx, &orderdto.ReturnReviewReq{
		ReturnID: receiveC, Remark: "先同意，等收货", OperatorName: "tester",
	}); aerr != nil {
		t.Fatalf("先把 receiveC 置为已同意失败：%v", aerr)
	}
	stockBeforeChain := f.stockOf(t, variantID)

	cap := &orderChainCapture{}
	h := orderhttp.NewHandle(f.orders)
	returnPages := orderhttp.NewReturnPageHandle(f.orders, projects, nil, nil)

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
			e.GET("/admin/returns", returnPages.ReturnsPage)
			e.POST("/api/order/return/approve", h.ApproveReturn)
			e.POST("/api/order/return/reject", h.RejectReturn)
			e.POST("/api/order/return/receive", h.ReceiveReturn)
		},
	})
	if err != nil {
		t.Fatalf("组件初始化失败: %v", err)
	}
	defer cleanup()

	server := httptest.NewServer(engine)
	defer server.Close()

	// ① 真渲染退货页。**每个状态渲染一次**：单档表单按选中申请的状态出现
	//    （列表页的「查看 / 审核」是同一个页面 + returnId 参数，不另开路由），
	//    所以「待审」与「已同意」两张页各取一次，属性集才是真实的。
	fetchPage := func(t *testing.T, returnID uint64) string {
		t.Helper()
		u := fmt.Sprintf("%s/admin/returns?project=%s&returnId=%d", server.URL, f.projectID, returnID)
		res, gerr := http.Get(u)
		if gerr != nil {
			t.Fatalf("取退货页（returnId=%d）失败: %v", returnID, gerr)
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("退货页（returnId=%d）应 200（渲染要真的成功才能取到属性），实际 %d\n%s",
				returnID, res.StatusCode, string(raw))
		}
		return string(raw)
	}
	requestedHTML := fetchPage(t, approveA) // 待审：同意 + 拒绝两张表单
	approvedHTML := fetchPage(t, receiveC)  // 已同意：确认收货表单

	// 行内勾选框必须真的渲染出来（批量动作靠它取 id），三张单档表单也得在对应的那一页上。
	for _, id := range []uint64{approveA, rejectB, receiveC, bulkE, bulkF} {
		if !strings.Contains(requestedHTML, fmt.Sprintf(`name="ids" value="%d"`, id)) {
			t.Fatalf("退货页里没有申请 %d 的行勾选框（批量动作就无从取 id）", id)
		}
	}
	if !strings.Contains(requestedHTML, fmt.Sprintf(`name="returnId" value="%d"`, approveA)) {
		t.Fatalf("待审页里没有申请 %d 的单档表单（hidden returnId 缺失）", approveA)
	}
	if !strings.Contains(approvedHTML, fmt.Sprintf(`name="returnId" value="%d"`, receiveC)) {
		t.Fatalf("已同意页里没有申请 %d 的收货表单（hidden returnId 缺失）", receiveC)
	}

	// ② 取原始属性：不手抄任何 data-*，模板少写一个属性本用例就必然跟着红。
	approveForm := orderChainFormSlice(t, requestedHTML, `data-write-api="/api/order/return/approve"`+` data-write-mode="create"`)
	approveFormAttrs := couponAttrsOf(approveForm[:strings.Index(approveForm, ">")+1])
	approveSubmitter := orderChainFormSubmitter(t, approveForm)

	rejectForm := orderChainFormSlice(t, requestedHTML, `data-write-api="/api/order/return/reject"`+` data-write-mode="create"`)
	rejectFormAttrs := couponAttrsOf(rejectForm[:strings.Index(rejectForm, ">")+1])
	rejectSubmitter := orderChainFormSubmitter(t, rejectForm)

	receiveForm := orderChainFormSlice(t, approvedHTML, `data-write-api="/api/order/return/receive"`+` data-write-mode="create"`)
	receiveFormAttrs := couponAttrsOf(receiveForm[:strings.Index(receiveForm, ">")+1])
	receiveSubmitter := orderChainFormSubmitter(t, receiveForm)

	// 两个批量按钮：端点声明在**按钮**上（表单上刻意不声明），所以按端点 + mode=bulk 取；
	// 批量条在列表区，与选中哪一条无关。
	bulkApproveBtn := orderChainButtonAttrs(t, requestedHTML, "/api/order/return/approve", "bulk")
	bulkRejectBtn := orderChainButtonAttrs(t, requestedHTML, "/api/order/return/reject", "bulk")

	// ③ 跑真实基座：五个用例。
	cases := []map[string]any{
		{
			"name":           "approve-single",
			"formAttrs":      approveFormAttrs,
			"submitterAttrs": approveSubmitter,
			"fields": orderChainFields{
				{Name: "returnId", Value: strconv.FormatUint(approveA, 10)},
				{Name: "remark", Value: "链式证据同意"},
				{Name: "warehouseId", Value: "", Tag: "select"},
				{Name: "transactionId", Value: "TX-CHAIN-A"},
				// 勾上「同意并立即完成入库 + 退款」：DOM 给的是字符串 "1"，必须由基座转成布尔。
				{Name: "autoReceive", Value: "1", Checked: true, Type: "checkbox"},
			},
		},
		{
			"name":           "reject-single",
			"formAttrs":      rejectFormAttrs,
			"submitterAttrs": rejectSubmitter,
			"fields": orderChainFields{
				{Name: "returnId", Value: strconv.FormatUint(rejectB, 10)},
				{Name: "remark", Value: "链式证据拒绝"},
			},
		},
		{
			"name":           "receive-single",
			"formAttrs":      receiveFormAttrs,
			"submitterAttrs": receiveSubmitter,
			"fields": orderChainFields{
				{Name: "returnId", Value: strconv.FormatUint(receiveC, 10)},
				{Name: "warehouseId", Value: "", Tag: "select"},
				{Name: "transactionId", Value: "TX-CHAIN-C"},
				{Name: "remark", Value: ""},
			},
		},
		{
			"name": "bulk-approve",
			// 批量**表单本身不带任何 data-write-\* 声明**（端点声明在按钮上，同一张表单两个动作）：
			// 所以「表单属性」这一格必须是空的 —— 把按钮属性填进来等于伪造一张带 data-confirm 的表，
			// json_write 会把确认让给表单级确认框，请求一个也不发（实测就是这么踩到的）。
			"formAttrs":      map[string]string{},
			"submitterAttrs": bulkApproveBtn,
			"fields": orderChainFields{
				{Name: "ids", Value: strconv.FormatUint(bulkE, 10), Checked: true, Type: "checkbox"},
				{Name: "remark", Value: "链式证据批量同意"},
			},
		},
		{
			"name":           "bulk-reject",
			"formAttrs":      map[string]string{},
			"submitterAttrs": bulkRejectBtn,
			"fields": orderChainFields{
				{Name: "ids", Value: strconv.FormatUint(bulkF, 10), Checked: true, Type: "checkbox"},
				{Name: "remark", Value: "链式证据批量拒绝"},
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
			t.Fatalf("%s 应恰好发一个请求且 fetch 不报错，实际 requests=%d errors=%v 报告=%s",
				r.Name, len(r.Requests), r.FetchErrors, string(out))
		}
	}

	// ④ 链的另一端：服务端真正收到的 JSON 体（类型逐个断言）。
	assertKeys := func(t *testing.T, where, body string, want ...string) map[string]json.RawMessage {
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
	assertUint := func(t *testing.T, where, name, raw string, want uint64) {
		t.Helper()
		if got := strings.TrimSpace(raw); got != strconv.FormatUint(want, 10) {
			t.Errorf("%s 的 %s 应是**数字**字面 %d（缺 data-write-number 就会是字符串），实际 %s",
				where, name, want, got)
		}
	}
	assertStr := func(t *testing.T, where, name, raw, want string) {
		t.Helper()
		if got := strings.TrimSpace(raw); got != strconv.Quote(want) {
			t.Errorf("%s 的 %s 应是字符串字面 %q，实际 %s", where, name, want, got)
		}
	}

	// 单档同意：五个字段，returnId 数字、autoReceive **布尔** true、库落到「已完成」。
	approveBody, approveStatus := cap.nth(t, 0)
	if approveStatus != http.StatusOK {
		t.Fatalf("单档同意应 200（autoReceive 若不是布尔就会 400），实际 %d：%s", approveStatus, approveBody)
	}
	approveJSON := assertKeys(t, "单档同意", approveBody, "returnId", "remark", "warehouseId", "transactionId", "autoReceive")
	assertUint(t, "单档同意", "returnId", string(approveJSON["returnId"]), approveA)
	assertStr(t, "单档同意", "remark", string(approveJSON["remark"]), "链式证据同意")
	assertStr(t, "单档同意", "transactionId", string(approveJSON["transactionId"]), "TX-CHAIN-A")
	if got := strings.TrimSpace(string(approveJSON["autoReceive"])); got != "true" {
		t.Errorf("单档同意的 autoReceive 应是**布尔** true（缺 data-write-bool 时会是字符串 \"1\"），实际 %s", got)
	}
	if got := f.returnStatusOf(t, approveA); got != "completed" {
		t.Errorf("勾了 autoReceive 的同意应一步完成，实际 %s", got)
	}

	// 单档拒绝：两个字段，remark 是字符串字面。
	rejectBody, rejectStatus := cap.nth(t, 1)
	if rejectStatus != http.StatusOK {
		t.Fatalf("单档拒绝应 200，实际 %d：%s", rejectStatus, rejectBody)
	}
	rejectJSON := assertKeys(t, "单档拒绝", rejectBody, "returnId", "remark")
	assertUint(t, "单档拒绝", "returnId", string(rejectJSON["returnId"]), rejectB)
	assertStr(t, "单档拒绝", "remark", string(rejectJSON["remark"]), "链式证据拒绝")
	if got := f.returnStatusOf(t, rejectB); got != "rejected" {
		t.Errorf("单档拒绝后应为 rejected，实际 %s", got)
	}

	// 单档收货：四个字段（表单里带仓 / 流水 / 备注），走的是 receive 端点。
	recvBody, recvStatus := cap.nth(t, 2)
	if recvStatus != http.StatusOK {
		t.Fatalf("单档收货应 200，实际 %d：%s", recvStatus, recvBody)
	}
	recvJSON := assertKeys(t, "单档收货", recvBody, "returnId", "warehouseId", "transactionId", "remark")
	assertUint(t, "单档收货", "returnId", string(recvJSON["returnId"]), receiveC)
	assertStr(t, "单档收货", "transactionId", string(recvJSON["transactionId"]), "TX-CHAIN-C")
	if got := f.returnStatusOf(t, receiveC); got != "completed" {
		t.Errorf("确认收货（先入库后退款）后应为 completed，实际 %s", got)
	}

	// 批量同意：恰好 ids + remark，ids 是**字符串数组**，且**不得**带 autoReceive（批量不自动入库）。
	bulkABody, bulkAStatus := cap.nth(t, 3)
	if bulkAStatus != http.StatusOK {
		t.Fatalf("批量同意应 200，实际 %d：%s", bulkAStatus, bulkABody)
	}
	bulkAJSON := assertKeys(t, "批量同意", bulkABody, "ids", "remark")
	assertStr(t, "批量同意", "remark", string(bulkAJSON["remark"]), "链式证据批量同意")
	if got := strings.TrimSpace(string(bulkAJSON["ids"])); got != fmt.Sprintf(`["%d"]`, bulkE) {
		t.Errorf("批量同意的 ids 应是字符串数组 [%d]，实际 %s", bulkE, got)
	}
	if got := f.returnStatusOf(t, bulkE); got != "approved" {
		t.Errorf("批量同意后应为 approved（不自动入库），实际 %s", got)
	}

	// 批量拒绝：同一个表单的另一个按钮投到 reject 端点，体里同样只有 ids + remark。
	bulkRBody, bulkRStatus := cap.nth(t, 4)
	if bulkRStatus != http.StatusOK {
		t.Fatalf("批量拒绝应 200，实际 %d：%s", bulkRStatus, bulkRBody)
	}
	bulkRJSON := assertKeys(t, "批量拒绝", bulkRBody, "ids", "remark")
	assertStr(t, "批量拒绝", "remark", string(bulkRJSON["remark"]), "链式证据批量拒绝")
	if got := strings.TrimSpace(string(bulkRJSON["ids"])); got != fmt.Sprintf(`["%d"]`, bulkF) {
		t.Errorf("批量拒绝的 ids 应是字符串数组 [%d]，实际 %s", bulkF, got)
	}
	if got := f.returnStatusOf(t, bulkF); got != "rejected" {
		t.Errorf("批量拒绝后应为 rejected，实际 %s", got)
	}

	// ⑤ 库存守恒：两句用例各自入库一次（approveA 一步到底 1 件 + receiveC 1 件），
	//    批量的两条**一件都不许动**（批量同意不自动入库）。
	if got, want := f.stockOf(t, variantID), stockBeforeChain+2; got != want {
		t.Errorf("链跑完后库存应为 %d（两次单档入库各 +1），实际 %d —— 批量动作多动了库存？", want, got)
	}

	// ⑥ JS 侧结论：五个用例都不该把后端那句话当成失败展示。
	for _, r := range report.Results {
		if strings.Contains(r.StatusText, "操作失败") {
			t.Errorf("%s 不应显示失败文案，实际状态区：%q", r.Name, r.StatusText)
		}
	}
}

// orderChainFormSubmitter 取一张表单里第一个 submit 按钮的属性。
//
// 单档表单的端点声明落在 `<form>` 上（批量条才把端点放在按钮上，因为同一张表单有两个动作），
// 所以取第一个 submit 按钮即可 —— base 的 attrFrom 口径是「按钮优先、表单兜底」。
func orderChainFormSubmitter(t *testing.T, form string) map[string]string {
	t.Helper()
	for _, tag := range couponButtonRe.FindAllString(form, -1) {
		if couponAttrOf(tag, "type") == "submit" {
			return couponAttrsOf(tag)
		}
	}
	t.Fatal("表单里找不到提交按钮")
	return nil
}
