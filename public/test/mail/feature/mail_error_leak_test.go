package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

func mailHead(s string) string {
	if len(s) <= 400 {
		return s
	}
	return s[:400] + "…"
}

const mailInternText = "操作失败，请稍后重试"

var mailLeakTokens = []string{
	"SQLSTATE", "uq_", "pg_", `relation "`, "constraint", "mail_accounts", "mail_templates",
}

func assertMailLeakFree(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range mailLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, mailHead(text))
		}
	}
}

func newMailLeakRouter(t *testing.T, f *mailFixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	engine.Use(func(c *gin.Context) {
		c.Next()
		for _, ge := range c.Errors {
			t.Logf("gin 渲染错误: %v", ge.Err)
		}
	})
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	h := mailhttp.NewMailPageHandle(f.svc)
	engine.GET("/admin/mail",
		pageview.Slot(mailhttp.AccountsPageTemplate, adminui.RenderPage), h.MailPage)
	engine.GET("/admin/mail/contacts",
		pageview.Slot(mailhttp.ContactsPageTemplate, adminui.RenderPage), h.MailContactsPage)
	engine.GET("/admin/mail/automation",
		pageview.Slot(mailhttp.AutomationPageTemplate, adminui.RenderPage), h.MailAutomationPage)
	engine.POST("/admin/mail/account/save", pageview.HTMLFragment, h.MailAccountSave)
	engine.POST("/admin/mail/template/save", pageview.HTMLFragment, h.MailTemplateSave)
	return engine
}

func mailPOST(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func TestMailPageHidesInternalError(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	engine := newMailLeakRouter(t, f)

	if err := f.db.Exec("DROP TABLE IF EXISTS mail_accounts CASCADE").Error; err != nil {
		t.Fatalf("制造故障失败：%v", err)
	}

	_, rawErr := f.svc.ListAccounts(context.Background(), "")
	if rawErr == nil {
		t.Fatal("反证失败：表已删除，ListAccounts 却成功了")
	}
	if !strings.Contains(rawErr.Error(), "mail_accounts") || !strings.Contains(rawErr.Error(), "SQLSTATE") {
		t.Fatalf("反证失败：原始错误应带表名与 SQLSTATE，实际 %v", rawErr)
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/mail", nil))
	body := rec.Body.String()
	assertMailLeakFree(t, "邮箱配置页响应体", body)
	if !strings.Contains(body, mailInternText) {
		t.Errorf("页面应显示归口文案 %q，实际：%s", mailInternText, mailHead(body))
	}
}

func TestMailAccountSaveRedirectHidesInternalError(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	engine := newMailLeakRouter(t, f)
	if err := f.db.Exec("DROP TABLE IF EXISTS mail_accounts CASCADE").Error; err != nil {
		t.Fatalf("制造故障失败：%v", err)
	}

	_, rawErr := f.svc.CreateAccount(context.Background(), &maildto.SaveAccountReq{
		Name: "营销账号", FromEmail: "news@example.com", Host: "smtp.example.com",
		Purpose: "marketing", Password: "secret",
	})
	if rawErr == nil {
		t.Fatal("反证失败：表已删除，CreateAccount 却成功了")
	}
	if !strings.Contains(rawErr.Error(), "mail_accounts") || !strings.Contains(rawErr.Error(), "SQLSTATE") {
		t.Fatalf("反证失败：原始错误应带表名与 SQLSTATE，实际 %v", rawErr)
	}

	rec := mailPOST(engine, "/admin/mail/account/save", url.Values{
		"name":       {"营销账号"},
		"from_email": {"news@example.com"},
		"host":       {"smtp.example.com"},
		"purpose":    {"marketing"},
		"password":   {"secret"},
	})
	body := rec.Body.String()
	assertMailLeakFree(t, "保存发信账号的错误片段", body)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(body, "data-ui-error") {
		t.Fatalf("基础设施错误应渲染 500 错误片段：%d %s", rec.Code, mailHead(body))
	}
}

func TestMailAutomationPageHidesInternalError(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	engine := newMailLeakRouter(t, f)
	if err := f.db.Exec("DROP TABLE IF EXISTS mail_automations CASCADE").Error; err != nil {
		t.Fatalf("制造故障失败：%v", err)
	}
	_, rawErr := f.svc.ListAutomations(context.Background(), &maildto.AutomationListReq{Page: 1, PageSize: 20})
	if rawErr == nil {
		t.Fatal("反证失败：表已删除，ListAutomations 却成功了")
	}
	if !strings.Contains(rawErr.Error(), "mail_automations") || !strings.Contains(rawErr.Error(), "SQLSTATE") {
		t.Fatalf("反证失败：原始错误应带表名与 SQLSTATE，实际 %v", rawErr)
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/mail/automation", nil))
	body := rec.Body.String()
	assertMailLeakFree(t, "自动化列表页响应体", body)
	if !strings.Contains(body, mailInternText) {
		t.Errorf("页面应显示归口文案 %q，实际：%s", mailInternText, mailHead(body))
	}
}

func TestMailContactsPageHidesInternalError(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	engine := newMailLeakRouter(t, f)
	if err := f.db.Exec("DROP TABLE IF EXISTS mail_contacts CASCADE").Error; err != nil {
		t.Fatalf("制造故障失败：%v", err)
	}
	_, rawErr := f.svc.ListContacts(context.Background(), &maildto.ContactFilterReq{Page: 1, PageSize: 10})
	if rawErr == nil {
		t.Fatal("反证失败：表已删除，ListContacts 却成功了")
	}
	if !strings.Contains(rawErr.Error(), "mail_contacts") || !strings.Contains(rawErr.Error(), "SQLSTATE") {
		t.Fatalf("反证失败：原始错误应带表名与 SQLSTATE，实际 %v", rawErr)
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/mail/contacts", nil))
	body := rec.Body.String()
	assertMailLeakFree(t, "联系人页响应体", body)
	if !strings.Contains(body, mailInternText) {
		t.Errorf("页面应显示归口文案 %q，实际：%s", mailInternText, mailHead(body))
	}

	if !strings.Contains(body, "</html>") {
		t.Error("联系人页渲染被中断（缺少 </html>）—— 错误分支缺模板必需的键")
	}
}

// mailInvalidParamText 与词库（迁移 058 的 ErrInvalidParam/zh-CN）一致。
// 声明处只给 key，成品句归词库，所以这里必须注入同一条词条才能验出
// 「业务错误仍可读」——不注入的话任何业务 key 都会落成归口文案。
const mailInvalidParamText = "请求参数无效"

func TestMailBusinessMessageStillVisible(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	i18n.InjectForTest(map[string]map[string]string{
		mailenums.ErrInvalidParam: {"zh-CN": mailInvalidParamText},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	engine := newMailLeakRouter(t, f)

	rec := mailPOST(engine, "/admin/mail/template/save", url.Values{"locale": {"zh-CN"}})
	body := rec.Body.String()
	assertMailLeakFree(t, "业务错误片段", body)

	if rec.Code != http.StatusBadRequest || !strings.Contains(body, mailInvalidParamText) {
		t.Fatalf("业务错误应渲染 400 错误片段并给出原因：%d %s", rec.Code, mailHead(body))
	}
	if strings.Contains(body, mailInternText) {
		t.Error("白名单业务错误被误吞成归口文案 —— 运营再也看不到「哪一项不合法」")
	}

}
