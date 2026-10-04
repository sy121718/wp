package feature

// mail_automation_page_test.go — 自动化后台页（issue #38 P3，目标 ⑦）。
//
// 页面模板是**运行时解析**的，go build 通过不代表模板正确。这条测试把
// 「列表 / 编辑 / 排障详情三个页面都渲染得出来」与「表单提交能组装成图并走校验」钉住。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

// newAutomationRouter 挂三个页面路由 + 保存 / 状态接口。
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
	router.GET("/admin/mail/automation", h.MailAutomationPage)
	router.GET("/admin/mail/automation/edit", h.MailAutomationEdit)
	router.GET("/admin/mail/automation/run", h.MailAutomationRunDetail)
	router.POST("/admin/mail/automation/save", h.MailAutomationSave)
	return router
}

// postAutomationForm 发一个表单 POST（跟随重定向到最终 Location 的内容不取，只看 302 与 Location）。
func postAutomationForm(router *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
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

// TestMailAutomationEditPageRenders 编辑页渲染（步骤表格 + 按类型渲染的控件都在）。
//
// 步骤化重做（issue #38 后续）：界面不再出现「标识 / 下一步 / yes / no」四个输入框，
// 顺序即执行顺序；旧的 7 列版字段（next_1 / yes_1 / no_1 / 入口节点标识）必须消失。
func TestMailAutomationEditPageRenders(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	router := newAutomationRouter(t, f)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/mail/automation/edit", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"新建自动化流程", "触发方式", "步骤", "添加一步", "保存",
		"node_key_1", "node_type_1", "条件分支", "等待", "发邮件"} {
		if !strings.Contains(body, want) {
			t.Fatalf("编辑页缺少 %q；前 600 字：\n%s", want, firstN(body, 600))
		}
	}
	for _, gone := range []string{"入口节点标识", "next_1", "yes_1", "no_1"} {
		if strings.Contains(body, gone) {
			t.Fatalf("编辑页不该再出现旧字段 %q；前 600 字：\n%s", gone, firstN(body, 600))
		}
	}
}

// TestMailAutomationFormSaveRejectsBackwardJump 分支往前跳会被行级校验拦下，
// 并且**回显 200 保住用户刚填的步骤**（302 会把页面翻回存库里的旧状态，改类型就成了死循环）。
func TestMailAutomationFormSaveRejectsBackwardJump(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	router := newAutomationRouter(t, f)
	form := url.Values{
		"id": {"0"}, "name": {"回跳流程"}, "trigger_type": {"manual"}, "entry": {"n1"},
		"node_key_1": {"n2"}, "node_type_1": {"branch"}, "param_1": {"opened"},
		"yes_1": {"1"}, "no_1": {"end"},
	}
	rec := postAutomationForm(router, "/admin/mail/automation/save", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("校验失败应回显表单（200），实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "跳转目标只能选本步之后的步骤") {
		t.Fatalf("应提示跳转目标不合法；前 600 字：\n%s", firstN(body, 600))
	}
	// 回显必须把用户刚填的东西留在页面上（这正是选 200 回显而不是 302 的全部理由）：
	// 名称、这一行选的类型、按新类型渲染出来的参数控件，一个都不能丢。
	for _, want := range []string{
		`value="回跳流程"`,
		`<option value="branch" selected>`,
		`name="param_tag_1"`, `name="yes_1"`, `name="no_1"`,
		`value="opened" selected`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("回显丢失 %s；前 600 字：\n%s", want, firstN(body, 600))
		}
	}
}

// TestMailAutomationFormSaveThenRunDetailRenders 表单保存成功 → 实例 → 排障页能渲染。
func TestMailAutomationFormSaveThenRunDetailRenders(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	router := newAutomationRouter(t, f)

	// 1. 表单保存一条合法流程（触发 → 打标签 → 结束）。
	form := url.Values{
		"id": {"0"}, "name": {"表单流程"}, "description": {"测试"},
		"trigger_type": {"manual"}, "entry": {"n1"},
		"node_key_1": {"n2"}, "node_type_1": {"tag"}, "param_1": {"vip, hot"},
		"node_key_2": {"n3"}, "node_type_2": {"end"},
		// 多填一行空行（表单固定几行）应被跳过，而不是报错。
		"node_key_3": {""}, "node_type_3": {""},
	}
	rec := postAutomationForm(router, "/admin/mail/automation/save", form)
	if rec.Code != http.StatusFound {
		t.Fatalf("应 302，实际 %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	decoded, _ := url.QueryUnescape(loc)
	if strings.Contains(decoded, "err=") {
		t.Fatalf("合法表单不该报错: %q", decoded)
	}
	// 从 Location 里取新 id。
	idStr := ""
	if i := strings.Index(decoded, "id="); i >= 0 {
		rest := decoded[i+3:]
		if j := strings.IndexAny(rest, "&"); j >= 0 {
			idStr = rest[:j]
		} else {
			idStr = rest
		}
	}
	id, _ := strconv.ParseUint(idStr, 10, 64)
	if id == 0 {
		t.Fatalf("保存后应跳回编辑页并带 id，实际 %q", decoded)
	}

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
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/mail/automation/run?id="+idStr2(run.ID), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("排障页状态码 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"实例排障", "auto-page@example.com", "表单流程", "已完成", "节点时间线", "n1", "n2"} {
		if !strings.Contains(body, want) {
			t.Fatalf("排障页缺少 %q；前 600 字：\n%s", want, firstN(body, 600))
		}
	}
	// 表单里填的两个标签真的加上了（说明参数解析正确）。
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
		// 后台按需内联动效：本页声明了 sky-fade-up，layout 才输出它的 @keyframes。
		// 未声明的页面为零字节（见 core.KeyframeCSS 的单测）。
		"@keyframes sky-fade-up",
		"/static/js/automation/canvas.js", "/static/css/automation.css",
		"画布页流程", "保存位置", "改用表单编辑",
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
