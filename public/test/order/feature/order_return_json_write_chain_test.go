package feature

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

	"go_wp/internal/adminui"
	orderdto "go_wp/internal/module/order/dto"
	orderhttp "go_wp/internal/module/order/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

func TestReturnJSONWriteChainTemplateToHandler(t *testing.T) {
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
	_, variantID := f.addProduct(t, "退货链式证据商品", 40, 60)

	mkReturn := func(email string) uint64 {
		orderID, userID, itemID := f.paidOrder(t, email, variantID, 1)
		return f.requestReturn(t, orderID, userID, itemID, 1).ID
	}
	approveA := mkReturn("chain-ret-approve@example.com")
	rejectB := mkReturn("chain-ret-reject@example.com")
	receiveC := mkReturn("chain-ret-receive@example.com")
	bulkE := mkReturn("chain-ret-bulk-e@example.com")
	bulkF := mkReturn("chain-ret-bulk-f@example.com")

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
			e.GET("/admin/returns", adminui.PageSlot(orderhttp.ReturnListPageTemplate), returnPages.ReturnsPage)
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
	requestedHTML := fetchPage(t, approveA)
	approvedHTML := fetchPage(t, receiveC)

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

	approveForm := orderChainFormSlice(t, requestedHTML, `data-write-url="/api/order/return/approve"`+` data-write="create"`)
	approveFormAttrs := couponAttrsOf(approveForm[:strings.Index(approveForm, ">")+1])
	approveSubmitter := orderChainFormSubmitter(t, approveForm)

	rejectForm := orderChainFormSlice(t, requestedHTML, `data-write-url="/api/order/return/reject"`+` data-write="create"`)
	rejectFormAttrs := couponAttrsOf(rejectForm[:strings.Index(rejectForm, ">")+1])
	rejectSubmitter := orderChainFormSubmitter(t, rejectForm)

	receiveForm := orderChainFormSlice(t, approvedHTML, `data-write-url="/api/order/return/receive"`+` data-write="create"`)
	receiveFormAttrs := couponAttrsOf(receiveForm[:strings.Index(receiveForm, ">")+1])
	receiveSubmitter := orderChainFormSubmitter(t, receiveForm)

	bulkApproveBtn := orderChainButtonAttrs(t, requestedHTML, "/api/order/return/approve", "bulk")
	bulkRejectBtn := orderChainButtonAttrs(t, requestedHTML, "/api/order/return/reject", "bulk")

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

	assertKeys := func(t *testing.T, where, body string, want ...string) map[string]string {
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
	assertUint := func(t *testing.T, where, name, got string, want uint64) {
		t.Helper()
		if strings.TrimSpace(got) != strconv.FormatUint(want, 10) {
			t.Errorf("%s 的 %s 应为 %d，实际 %q", where, name, want, got)
		}
	}
	assertStr := func(t *testing.T, where, name, got, want string) {
		t.Helper()
		if strings.TrimSpace(got) != want {
			t.Errorf("%s 的 %s 应为 %q，实际 %q", where, name, want, got)
		}
	}

	approveBody, approveStatus := cap.nth(t, 0)
	if approveStatus != http.StatusOK {
		t.Fatalf("单档同意应 200（autoReceive 若不是布尔就会 400），实际 %d：%s", approveStatus, approveBody)
	}

	approveJSON := assertKeys(t, "单档同意", approveBody, "returnId", "remark", "transactionId", "autoReceive")
	assertUint(t, "单档同意", "returnId", approveJSON["returnId"], approveA)
	assertStr(t, "单档同意", "remark", approveJSON["remark"], "链式证据同意")
	assertStr(t, "单档同意", "transactionId", approveJSON["transactionId"], "TX-CHAIN-A")

	if got := strings.TrimSpace(approveJSON["autoReceive"]); got == "" || got == "false" || got == "0" {
		t.Errorf("单档同意的 autoReceive 应表达「勾选」（值为 \"1\" 等），实际 %q", got)
	}
	if got := f.returnStatusOf(t, approveA); got != "completed" {
		t.Errorf("勾了 autoReceive 的同意应一步完成，实际 %s", got)
	}

	rejectBody, rejectStatus := cap.nth(t, 1)
	if rejectStatus != http.StatusOK {
		t.Fatalf("单档拒绝应 200，实际 %d：%s", rejectStatus, rejectBody)
	}
	rejectJSON := assertKeys(t, "单档拒绝", rejectBody, "returnId", "remark")
	assertUint(t, "单档拒绝", "returnId", rejectJSON["returnId"], rejectB)
	assertStr(t, "单档拒绝", "remark", rejectJSON["remark"], "链式证据拒绝")
	if got := f.returnStatusOf(t, rejectB); got != "rejected" {
		t.Errorf("单档拒绝后应为 rejected，实际 %s", got)
	}

	recvBody, recvStatus := cap.nth(t, 2)
	if recvStatus != http.StatusOK {
		t.Fatalf("单档收货应 200，实际 %d：%s", recvStatus, recvBody)
	}

	recvJSON := assertKeys(t, "单档收货", recvBody, "returnId", "transactionId")
	assertUint(t, "单档收货", "returnId", recvJSON["returnId"], receiveC)
	assertStr(t, "单档收货", "transactionId", recvJSON["transactionId"], "TX-CHAIN-C")
	if got := f.returnStatusOf(t, receiveC); got != "completed" {
		t.Errorf("确认收货（先入库后退款）后应为 completed，实际 %s", got)
	}

	bulkABody, bulkAStatus := cap.nth(t, 3)
	if bulkAStatus != http.StatusOK {
		t.Fatalf("批量同意应 200，实际 %d：%s", bulkAStatus, bulkABody)
	}
	bulkAJSON := assertKeys(t, "批量同意", bulkABody, "ids", "remark")
	assertStr(t, "批量同意", "remark", bulkAJSON["remark"], "链式证据批量同意")

	if got := bulkAJSON["ids"]; got != fmt.Sprintf("%d", bulkE) {
		t.Errorf("批量同意的 ids 应为 [%d]，实际 %q", bulkE, got)
	}
	if got := f.returnStatusOf(t, bulkE); got != "approved" {
		t.Errorf("批量同意后应为 approved（不自动入库），实际 %s", got)
	}

	bulkRBody, bulkRStatus := cap.nth(t, 4)
	if bulkRStatus != http.StatusOK {
		t.Fatalf("批量拒绝应 200，实际 %d：%s", bulkRStatus, bulkRBody)
	}
	bulkRJSON := assertKeys(t, "批量拒绝", bulkRBody, "ids", "remark")
	assertStr(t, "批量拒绝", "remark", bulkRJSON["remark"], "链式证据批量拒绝")
	if got := bulkRJSON["ids"]; got != fmt.Sprintf("%d", bulkF) {
		t.Errorf("批量拒绝的 ids 应为 [%d]，实际 %q", bulkF, got)
	}
	if got := f.returnStatusOf(t, bulkF); got != "rejected" {
		t.Errorf("批量拒绝后应为 rejected，实际 %s", got)
	}

	if got, want := f.stockOf(t, variantID), stockBeforeChain; got != want {
		t.Errorf("链跑完后在库应为 %d（未发货的退货不改变在库数量），实际 %d —— 有人把货加回去了", want, got)
	}

	for _, r := range report.Results {
		if strings.Contains(r.StatusText, "操作失败") {
			t.Errorf("%s 不应显示失败文案，实际状态区：%q", r.Name, r.StatusText)
		}
	}
}

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
