package feature

// mail_automation_page_test.go — 自动化后台页（issue #38 P3，目标 ⑦）。
//
// 页面模板是**运行时解析**的，go build 通过不代表模板正确。这条测试把
// 「列表 / 编辑 / 排障详情三个页面都渲染得出来」与「表单提交能组装成图并走校验」钉住。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	maildto "go_wp/internal/module/mail/dto"
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
	h := dashboardhttp.NewMailPageHandle(f.svc)
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
	for _, want := range []string{"自动化流程", "新建流程", "运行实例", "补投延时实例", "还没有流程"} {
		if !strings.Contains(body, want) {
			t.Fatalf("列表页缺少 %q；前 600 字：\n%s", want, firstN(body, 600))
		}
	}
}

// TestMailAutomationEditPageRenders 编辑页渲染（类型下拉与参数说明都在）。
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
	for _, want := range []string{"新建自动化流程", "触发方式", "入口节点标识", "node_key_1", "node_type_1",
		"param_1", "next_1", "yes_1", "no_1", "条件分支", "等待", "发邮件", "node_key_12"} {
		if !strings.Contains(body, want) {
			t.Fatalf("编辑页缺少 %q；前 600 字：\n%s", want, firstN(body, 600))
		}
	}
}

// TestMailAutomationFormSaveRejectsCycle 表单提交的图有环时被拒绝，并带定位信息。
func TestMailAutomationFormSaveRejectsCycle(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	router := newAutomationRouter(t, f)
	form := url.Values{
		"id": {"0"}, "name": {"环流程"}, "trigger_type": {"manual"}, "entry": {"n1"},
		"node_key_1": {"n1"}, "node_type_1": {"trigger"}, "next_1": {"n2"},
		"node_key_2": {"n2"}, "node_type_2": {"delay"}, "param_2": {"60"}, "next_2": {"n1"},
	}
	rec := postAutomationForm(router, "/admin/mail/automation/save", form)
	if rec.Code != http.StatusFound {
		t.Fatalf("应 302 回编辑页，实际 %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "err=") {
		t.Fatalf("有环的图应被拒绝并回带错误，实际跳转 %q", loc)
	}
	decoded, _ := url.QueryUnescape(loc)
	if !strings.Contains(decoded, "环") {
		t.Fatalf("错误里应说明是环，实际 %q", decoded)
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

	// 1. 表单保存一条合法流程（入口 → 打标签 → 结束）。
	form := url.Values{
		"id": {"0"}, "name": {"表单流程"}, "description": {"测试"},
		"trigger_type": {"manual"}, "entry": {"n1"},
		"node_key_1": {"n1"}, "node_type_1": {"trigger"}, "next_1": {"n2"},
		"node_key_2": {"n2"}, "node_type_2": {"tag"}, "param_2": {"vip, hot"}, "next_2": {"n3"},
		"node_key_3": {"n3"}, "node_type_3": {"end"},
		// 多填一行空行（表单有 12 行）应被跳过，而不是报错。
		"node_key_4": {""}, "node_type_4": {""},
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
