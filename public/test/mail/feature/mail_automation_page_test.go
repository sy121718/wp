package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	maildto "go_wp/internal/module/mail/dto"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

func newAutomationRouter(t *testing.T, f *mailFixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Next()
		for _, e := range c.Errors {
			t.Logf("gin 渲染错误: %v", e.Err)
		}
	})
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	h := mailhttp.NewMailPageHandle(f.svc)
	api := mailhttp.NewHandle(f.svc)

	router.GET("/admin/mail/automation",
		adminui.PageSlot(mailhttp.AutomationPageTemplate), h.MailAutomationPage)
	router.GET("/admin/mail/automation/canvas",
		adminui.PageSlot(mailhttp.AutomationCanvasTemplate), h.MailAutomationCanvas)
	router.GET("/admin/mail/automation/run",
		adminui.PageSlot(mailhttp.AutomationRunPageTemplate), h.MailAutomationRunDetail)

	router.POST("/api/mail/automation/save", api.AutomationSave)
	return router
}

func TestMailAutomationPageRenders(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	router := newAutomationRouter(t, f)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/mail/automation", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	body := rec.Body.String()

	for _, want := range []string{"自动化流程", "新建流程", "运行实例", "还没有流程"} {
		if !strings.Contains(body, want) {
			t.Fatalf("列表页缺少 %q；前 600 字：\n%s", want, firstN(body, 600))
		}
	}
}

func TestMailAutomationSaveThenRunDetailRenders(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	router := newAutomationRouter(t, f)

	raw, err := json.Marshal(map[string]any{"entry": "n1", "nodes": []any{
		map[string]any{"key": "n1", "type": "trigger", "out": map[string]any{"next": "n2"}},
		map[string]any{"key": "n2", "type": "tag",
			"params": map[string]any{"add": []any{"vip", "hot"}},
			"out":    map[string]any{"next": "n3"}},
		map[string]any{"key": "n3", "type": "end", "out": map[string]any{}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.svc.SaveAutomation(ctx, &maildto.SaveAutomationReq{
		Name: "画布流程", TriggerType: mailmodel.TriggerManual, Definition: raw,
	})
	if err != nil {
		t.Fatalf("建流程失败: %v", err)
	}
	id := item.ID

	if err := f.svc.SetAutomationStatus(ctx, &maildto.SetAutomationStatusReq{ID: id, Status: mailmodel.AutomationStatusActive}); err != nil {
		t.Fatalf("启用失败: %v", err)
	}
	if err := f.seedContact(ctx, "auto-page@example.com", "小李", mailmodel.ContactStatusSubscribed, ""); err != nil {
		t.Fatal(err)
	}
	m := mailmodel.NewMailModel(f.db)
	contact, err := m.GetContactByEmail(ctx, "auto-page@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.StartRun(ctx, id, contact.ID, mailmodel.TriggerManual); err != nil {
		t.Fatal(err)
	}
	run, err := m.ActiveRun(ctx, id, contact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.svc.RunAutomation(ctx, run.ID); err != nil {
		t.Fatalf("推进失败: %v", err)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/mail/automation/run?id="+idStr2(run.ID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("排障页状态码 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"实例排障", "auto-page@example.com", "画布流程", "已完成", "节点时间线", "n1", "n2"} {
		if !strings.Contains(body, want) {
			t.Fatalf("排障页缺少 %q；前 600 字：\n%s", want, firstN(body, 600))
		}
	}

	after, _ := m.GetContact(ctx, contact.ID)
	joined := strings.Join(after.Tags, ",")
	if !strings.Contains(joined, "vip") || !strings.Contains(joined, "hot") {
		t.Fatalf("标签节点没生效: %v（原始标签串 %q）", after.Tags, joined)
	}
}

func idStr2(v uint64) string { return strconv.FormatUint(v, 10) }

func TestMailAutomationCanvasPageRenders(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	def := map[string]any{"entry": "n1", "nodes": []any{
		map[string]any{"key": "n1", "type": "trigger", "next": "n2"},
		map[string]any{"key": "n2", "type": "tag", "params": map[string]any{"add": []any{"x"}}, "next": "n3"},
		map[string]any{"key": "n3", "type": "end"},
	}}
	raw, _ := json.Marshal(def)
	item, err := f.svc.SaveAutomation(ctx, &maildto.SaveAutomationReq{
		Name: "画布页流程", TriggerType: mailmodel.TriggerManual, Definition: raw,
	})
	if err != nil {
		t.Fatalf("建流程失败: %v", err)
	}
	id := item.ID

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Next()
		for _, e := range c.Errors {
			t.Logf("gin 渲染错误: %v", e.Err)
		}
	})
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	h := mailhttp.NewMailPageHandle(f.svc)
	router.GET("/admin/mail/automation/canvas",
		adminui.PageSlot(mailhttp.AutomationCanvasTemplate), h.MailAutomationCanvas)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/mail/automation/canvas?id="+strconv.FormatUint(id, 10), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"autoCanvas", "autoEdges", "autoNodes", "autoMeta",

		`class="auto-palette"`, `id="autoMiniMap"`, `id="autoUndo"`, `id="autoRedo"`, `id="autoFit"`,

		"@keyframes sky-fade-in-up",
		"/static/js/automation/index.js", "/static/css/automation.css",
		"画布页流程", "保存位置",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("画布页缺少 %q；前 600 字：\n%s", want, firstN(body, 600))
		}
	}

	if !strings.Contains(body, "\"key\":\"n1\"") {
		t.Fatalf("节点数据没注入；前 800 字：\n%s", firstN(body, 800))
	}
}

func TestMailAutomationCanvasNewFlowRenders(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	router := newAutomationRouter(t, f)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/mail/automation/canvas", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("新建画布状态码 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "autoCanvas") || !strings.Contains(body, "autoNodes") {
		t.Fatalf("新建画布页没渲染出画布容器；前 600 字：\n%s", firstN(body, 600))
	}

	if !strings.Contains(body, `type`) || !strings.Contains(body, `trigger`) {
		t.Fatalf("新建画布应有一个 trigger 节点；前 800 字：\n%s", firstN(body, 800))
	}
	if !strings.Contains(body, `"isNew":true`) {
		t.Fatalf("新建画布的 meta.isNew 应为 true；前 800 字：\n%s", firstN(body, 800))
	}
}

func TestMailAutomationEditRouteIsGone(t *testing.T) {
	if _, err := os.Stat("../../../../internal/templates/admin/mail/mail_automation_edit.html"); err == nil {
		t.Fatal("表单编辑页模板仍存在：入口合并没做完")
	}

	snapshot, err := os.ReadFile("../../../../internal/routers/testdata/routes.snapshot")
	if err != nil {
		t.Fatalf("读取路由快照失败: %v", err)
	}
	if strings.Contains(string(snapshot), "/admin/mail/automation/edit") {
		t.Fatal("路由快照里仍有 GET /admin/mail/automation/edit：/edit 入口没删干净")
	}

	list, err := os.ReadFile("../../../../internal/templates/admin/mail/mail_automation.html")
	if err != nil {
		t.Fatalf("读取流程列表模板失败: %v", err)
	}
	if strings.Contains(string(list), `/admin/mail/automation/edit`) {
		t.Fatal("流程列表仍有指向 /edit 的链接")
	}
	if !strings.Contains(string(list), `href="/admin/mail/automation/canvas?id={{a.ID}}"`) {
		t.Fatal("流程列表的「编辑」应指向画布（带 id）")
	}
}

func TestMailAutomationNewFlowCanBeSaved(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	router := newAutomationRouter(t, f)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/mail/automation/canvas", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("新建画布状态码 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`id="autoName"`, `id="autoTrigger"`, `id="autoSave"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("新建画布缺少可编辑控件 %s：保存结构时名称只能是空串，服务端会以「流程名称不能为空」拒绝", want)
		}
	}

	payload := map[string]any{
		"id":            0,
		"name":          "新建流程",
		"triggerType":   mailmodel.TriggerManual,
		"triggerParams": map[string]any{},
		"definition": map[string]any{"schema": 2, "entry": "n1", "nodes": []any{
			map[string]any{"key": "n1", "type": "trigger", "out": map[string]any{}},
		}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/mail/automation/save", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("新建流程保存状态码 %d，正文：%s", rec.Code, firstN(rec.Body.String(), 300))
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			ID    uint64 `json:"id"`
			Entry string `json:"entry"`
			Nodes []struct {
				Key string `json:"key"`
			} `json:"nodes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("保存响应不是合法 JSON: %v（%s）", err, firstN(rec.Body.String(), 300))
	}
	if resp.Code != http.StatusOK || resp.Data.ID == 0 {
		t.Fatalf("保存成功必须回带 id（画布靠它把地址栏从「新建」切成「编辑」），实际 code=%d id=%d",
			resp.Code, resp.Data.ID)
	}
	if resp.Data.Entry != "n1" || len(resp.Data.Nodes) != 1 {
		t.Fatalf("新建流程应是「一个入口节点」，实际 entry=%q nodes=%d", resp.Data.Entry, len(resp.Data.Nodes))
	}
}

func TestMailAutomationSaveKeepsTriggerParams(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	router := newAutomationRouter(t, f)

	item, err := f.svc.SaveAutomation(ctx, &maildto.SaveAutomationReq{
		Name:          "标签触发流程",
		TriggerType:   mailmodel.TriggerTagAdded,
		TriggerParams: map[string]any{"tag": "vip"},
		Definition:    json.RawMessage(`{"schema":2,"entry":"n1","nodes":[{"key":"n1","type":"trigger","out":{}}]}`),
	})
	if err != nil {
		t.Fatalf("建流程失败: %v", err)
	}

	payload := map[string]any{
		"id":          item.ID,
		"name":        "标签触发流程",
		"triggerType": mailmodel.TriggerTagAdded,
		"definition": map[string]any{"schema": 2, "entry": "n1", "nodes": []any{
			map[string]any{"key": "n1", "type": "trigger", "out": map[string]any{}},
		}},
	}
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/mail/automation/save", strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存状态码 %d：%s", rec.Code, firstN(rec.Body.String(), 300))
	}

	after, err := f.svc.GetAutomation(ctx, item.ID)
	if err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if got := after.TriggerParams["tag"]; got != "vip" {
		t.Fatalf("画布保存抹掉了触发条件：tag=%v（期望 vip）。清空后该流程对任意标签都触发", got)
	}
}
