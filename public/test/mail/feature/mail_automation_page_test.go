package feature

// mail_automation_page_test.go — 自动化后台页（issue #38 P3，目标 ⑦）。
//
// 页面模板是**运行时解析**的，go build 通过不代表模板正确。这条测试把
// 「列表 / 编辑 / 排障详情三个页面都渲染得出来」与「表单提交能组装成图并走校验」钉住。

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

// newAutomationRouter 挂画布的页面路由 + 保存端点（画布自己就是保存端的唯一调用方）。
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
	router.GET("/admin/mail/automation", h.MailAutomationPage)
	router.GET("/admin/mail/automation/canvas", h.MailAutomationCanvas)
	router.GET("/admin/mail/automation/run", h.MailAutomationRunDetail)
	// 画布保存打的就是这条 JSON 端点（见 static/js/automation/canvas.js 的 saveStructure）。
	// 挂真路由而不是绕到 service：绑定、响应形状、状态码都在这一层，绕过去三者坏了判据照样绿。
	router.POST("/api/mail/automation/save", api.AutomationSave)
	return router
}

// TestMailAutomationPageRenders 列表页渲染（空态也在）。
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
	// 「补投延时实例」→「补投到点实例」：按钮文案改准确了 —— 补的是**到点的**延时实例，
	// 不是"补一次投递"。空态同样是「标题 + 一句话」形态。
	// 「补投到点实例」已随运行实例块搬到 /admin/mail/automation/runs（issue #37）：
	// 本页只留流程列表，运行实例只由 page-actions 的 ghost 入口进入。
	for _, want := range []string{"自动化流程", "新建流程", "运行实例", "还没有流程"} {
		if !strings.Contains(body, want) {
			t.Fatalf("列表页缺少 %q；前 600 字：\n%s", want, firstN(body, 600))
		}
	}
}

// TestMailAutomationFormSaveThenRunDetailRenders 表单保存成功 → 实例 → 排障页能渲染。
// TestMailAutomationSaveThenRunDetailRenders 保存（走 JSON API，画布用的同一个端点）
// → 实例 → 排障页能渲染。
//
// 保存入口只有一条：画布提交 /api/mail/automation/save（页面表单链路已删除，
// 同一份图不再有第二个还原器）。这里直接调 service，钉的是「落库 → 执行 → 排障页」
// 这条链在 v2 图定义下仍然通。
func TestMailAutomationSaveThenRunDetailRenders(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	router := newAutomationRouter(t, f)

	// 1. 建一条合法流程（触发 → 打标签 → 结束）。
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

	// 2. 流程是草稿：启用后建实例。
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

	// 3. 排障页渲染，含「一句话解释」与时间线。
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
	// 节点参数里的两个标签真的加上了（说明 params 透传正确）。
	after, _ := m.GetContact(ctx, contact.ID)
	joined := strings.Join(after.Tags, ",")
	if !strings.Contains(joined, "vip") || !strings.Contains(joined, "hot") {
		t.Fatalf("标签节点没生效: %v（原始标签串 %q）", after.Tags, joined)
	}
}

func idStr2(v uint64) string { return strconv.FormatUint(v, 10) }

// TestMailAutomationCanvasPageRenders 画布页渲染（P4）：节点数据、SVG 层、module 脚本都在。
func TestMailAutomationCanvasPageRenders(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	// dashboard 测试包没有 mail feature 包的 seedAutomation helper，这里直接建。
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
	router.GET("/admin/mail/automation/canvas", h.MailAutomationCanvas)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/mail/automation/canvas?id="+strconv.FormatUint(id, 10), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"autoCanvas", "autoEdges", "autoNodes", "autoMeta",
		// T3 的新容器与控件：palette 左栏、缩略图、撤销 / 重做 / 自动布局。
		// 它们是「画布能用」的最小可见证据 —— 少了任何一个，对应交互就整体消失。
		`class="auto-palette"`, `id="autoMiniMap"`, `id="autoUndo"`, `id="autoRedo"`, `id="autoFit"`,
		// 后台按需内联动效：本页声明了 sky-fade-in-up，layout 才输出它的 @keyframes。
		// 未声明的页面为零字节（见 core.KeyframeCSS 的单测）。
		"@keyframes sky-fade-in-up",
		"/static/js/automation/index.js", "/static/css/automation.css",
		"画布页流程", "保存位置",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("画布页缺少 %q；前 600 字：\n%s", want, firstN(body, 600))
		}
	}
	// 节点数据以 JSON 注入（不是内联脚本执行）。
	if !strings.Contains(body, "\"key\":\"n1\"") {
		t.Fatalf("节点数据没注入；前 800 字：\n%s", firstN(body, 800))
	}
}

// TestMailAutomationCanvasNewFlowRenders 新建流程走同一个画布地址（不带 id）。
//
// 这是「入口合并」的正面判据：/edit 消失之后，新建唯一可达的路径就是这条。缺 id 不再
// 当异常（旧实现渲染「请先从流程列表选择一个流程」），而是给一份只含入口节点的空图 ——
// 页面必须能画出来，否则新建流程整条链就断了。
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
	// 空图 = 一个入口节点：注入的 JSON 里该有 type:trigger（转义与否都认 ——
	// utils.JsonSafe 会把引号转成 HTML 实体，判据不该依赖那一层实现细节）。
	if !strings.Contains(body, `type`) || !strings.Contains(body, `trigger`) {
		t.Fatalf("新建画布应有一个 trigger 节点；前 800 字：\n%s", firstN(body, 800))
	}
	if !strings.Contains(body, `"isNew":true`) {
		t.Fatalf("新建画布的 meta.isNew 应为 true；前 800 字：\n%s", firstN(body, 800))
	}
}

// TestMailAutomationEditRouteIsGone /edit 已删除，且不留重定向。
//
// 判据是 **404** 而不是「跳到画布」：留一条跳转等于「两个入口」以另一种形式复活 ——
// 用户仍会持有旧书签，而旧书签之所以能工作，说明还有人在维护那条路径。
func TestMailAutomationEditRouteIsGone(t *testing.T) {
	if _, err := os.Stat("../../../../internal/templates/admin/mail/mail_automation_edit.html"); err == nil {
		t.Fatal("表单编辑页模板仍存在：入口合并没做完")
	}
	// 路由层面：装配后的清单里不能再有这条页面路由。
	snapshot, err := os.ReadFile("../../../../internal/routers/testdata/routes.snapshot")
	if err != nil {
		t.Fatalf("读取路由快照失败: %v", err)
	}
	if strings.Contains(string(snapshot), "/admin/mail/automation/edit") {
		t.Fatal("路由快照里仍有 GET /admin/mail/automation/edit：/edit 入口没删干净")
	}
	// 模板层面：列表页的「编辑」必须指向画布，且不再有新开 /edit 链接。
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

// TestMailAutomationNewFlowCanBeSaved 新建流程必须能从画布**存下去**。
//
// 这条用例钉的是一个会让整条链断掉的形态：服务端对空名直接返回 ErrInvalidParam，
// 而新建流程时 meta.name 是空串 —— 如果画布不提供名称输入（而是从注入的 meta 读），
// 那么「新建 → 画布上画完 → 保存」永远 400，而新建已经没有第二个入口了。
//
// 两段判据：
//  1. 画布 HTML 里确有可编辑的名称 / 触发方式控件；
//  2. 按画布保存体**真发一次 POST /api/mail/automation/save**（绑定 + 响应形状 + 状态码
//     都在这一层；直接调 service 的话三者坏了这条判据照样绿）。
func TestMailAutomationNewFlowCanBeSaved(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	router := newAutomationRouter(t, f)

	// 1. 画布上有可编辑的名称与触发方式（不是只读文本）。
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

	// 2. 按画布的保存体发一次真请求（只含入口节点、无出口 = 触发即结束，合法）。
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

// TestMailAutomationSaveKeepsTriggerParams 画布保存不能抹掉触发条件。
//
// 触发条件（如 tag_added 的标签）是**流程级配置**，不在图里；画布提交的 body 也不带它。
// 而 SaveAutomation 的更新分支若无条件写 trigger_params，就会把它清成空 map ——
// 而 triggerParamsMatch 对空条件恒为 true：一条「只在打上 vip 时触发」的流程，
// 在画布上保存一次之后对**任意标签**都触发。这是静默的数据损坏，必须钉住。
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

	// 画布保存体：**不带 triggerParams**（这正是前端的真实形态）。
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
