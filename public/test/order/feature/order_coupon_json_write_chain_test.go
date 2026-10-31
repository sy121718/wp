package feature

import (
	"bytes"
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"io"
	"mime/multipart"
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

	"go_wp/internal/adminui"
	orderdto "go_wp/internal/module/order/dto"
	orderhttp "go_wp/internal/module/order/inbound/http"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/public/test/support"
)

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

func couponAttrsOf(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range couponAttrRe.FindAllStringSubmatch(tag, -1) {
		out[m[1]] = m[2]
	}
	return out
}

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

func buildCouponChainFixture(t *testing.T, html, toggleID, deleteID, projectID string) *couponChainFixture {
	t.Helper()
	fx := &couponChainFixture{}

	bulkForm := couponBulkFormTag(html)
	if bulkForm == "" {
		t.Fatal("渲染出来的列表页里找不到批量表单（批量条只在有数据时渲染）")
	}
	fx.bulkFormAttrs = couponAttrsOf(bulkForm)

	var toggleButton string
	for _, tag := range couponButtonRe.FindAllString(html, -1) {
		if strings.Contains(tag, `data-write-url="/api/order/coupon/update"`) {
			toggleButton = tag
			break
		}
	}
	if toggleButton == "" {
		t.Fatal("列表页里找不到批量启停按钮（data-write-url=/api/order/coupon/update）")
	}
	fx.bulkSubmitter = couponAttrsOf(toggleButton)

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

func TestOrderCouponJSONWriteChainTemplateToHandler(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("本机没有 node：链式证据要求真实基座 write-base.js，跳过而不是用桩冒充")
	}
	f := newOrderFixture(t)
	if f == nil {
		return
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(f.db))

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

				cap.add(c.Request.URL.Path, string(body), c.Writer.Status())
			})
			e.GET("/admin/coupons", adminui.PageSlot(orderhttp.CouponListPageTemplate), couponsPage.CouponsPage)
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

	bulkBefore, berr := f.orders.GetCoupon(context.Background(), bulkKeep.ID)
	if berr != nil || bulkBefore == nil {
		t.Fatalf("重读批量用例的券失败: %v", berr)
	}
	fx.bulkTarget = "0"
	if bulkBefore.Status == 0 {
		fx.bulkTarget = "1"
	}
	fx.bulkFields[0] = couponChainField{Name: "status", Value: fx.bulkTarget, Tag: "select"}

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
	cmd := exec.Command(node, "testdata/coupon_json_write_chain_driver.cjs", fixturePath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("驱动脚本失败: %v\n%s\nstderr=%s", err, string(out), stderr.String())
	}
	if stderr.Len() > 0 {
		t.Logf("驱动脚本 stderr（调试用）:\n%s", stderr.String())
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

	bulkBody, bulkStatus := cap.nth(t, 0)
	if bulkStatus != http.StatusOK {
		t.Fatalf("批量启停应 200，实际 %d：body=%q", bulkStatus, bulkBody)
	}

	bulkForm, ferr := parseMultipartFormBody(bulkBody)
	if ferr != nil {
		t.Fatalf("批量请求体解析失败: %v（body=%s）", ferr, bulkBody)
	}
	if got := bulkForm["status"]; got == "" {
		t.Errorf("批量请求体缺少 status 字段（body=%s）", bulkBody)
	}
	if got := bulkForm["ids"]; got != strconv.FormatUint(bulkKeep.ID, 10) {
		t.Errorf("批量 ids 应为被勾选那一行，实际 %q（body=%s）", got, bulkBody)
	}
	if got := bulkForm["projectId"]; got != f.projectID {
		t.Errorf("批量 projectId 应保持原值，实际 %q", got)
	}

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

	rowBody, rowStatus := cap.nth(t, 1)
	if rowStatus != http.StatusOK {
		t.Fatalf("行内启停应 200，实际 %d：%s", rowStatus, rowBody)
	}

	rowForm, rerr := parseMultipartFormBody(rowBody)
	if rerr != nil {
		t.Fatalf("行内启停请求体解析失败: %v（body=%s）", rerr, rowBody)
	}
	for _, name := range []string{"id", "discountValue", "minSubtotal", "maxUses", "perUserLimit", "status"} {
		if got := strings.TrimSpace(rowForm[name]); got == "" {
			t.Fatalf("行内启停请求体缺少字段 %s（或值为空）：body=%s", name, rowBody)
		}
	}
	if got := strings.TrimSpace(rowForm["status"]); got != fx.rowStatusValue {
		t.Errorf("行内启停的 status 应等于渲染出来的翻转目标 %s，实际 %s（请求体：%s）", fx.rowStatusValue, got, rowBody)
	}
	afterRow, _ := f.orders.GetCoupon(context.Background(), keep.ID)
	if afterRow == nil || strconv.Itoa(afterRow.Status) != fx.rowStatusValue {
		t.Fatalf("行内启停应把状态翻转成 %s，实际 %+v", fx.rowStatusValue, afterRow)
	}

	delBody, delStatus := cap.nth(t, 2)
	if delStatus != http.StatusOK {
		t.Fatalf("行内删除应 200，实际 %d：%s", delStatus, delBody)
	}
	delForm, derr := parseMultipartFormBody(delBody)
	if derr != nil {
		t.Fatalf("行内删除请求体解析失败: %v（body=%s）", derr, delBody)
	}

	if len(delForm) != 1 {
		t.Errorf("single 模式只应发 {id}，实际字段：%v（body=%s）", keysOf(delForm), delBody)
	}
	if got := strings.TrimSpace(delForm["id"]); got != strconv.FormatUint(remove.ID, 10) {
		t.Errorf("行内删除的 id 应是 %d，实际 %q", remove.ID, got)
	}
	if couponAlive(t, f, remove.ID) {
		t.Fatal("行内删除后券应已不存在（链走通了才删得掉）")
	}
}

func parseMultipartFormBody(body string) (map[string]string, error) {
	out := map[string]string{}

	first := strings.SplitN(body, "\r\n", 2)
	if len(first) < 2 {
		return nil, fmt.Errorf("缺少 multipart 分隔行")
	}
	boundary := strings.TrimPrefix(strings.TrimSpace(first[0]), "--")
	reader := multipart.NewReader(strings.NewReader(body), boundary)
	form, err := reader.ReadForm(1 << 20)
	if err != nil {
		return nil, err
	}
	for k, v := range form.Value {
		if len(v) > 0 {

			out[k] = strings.Join(v, ",")
		}
	}
	return out, nil
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
