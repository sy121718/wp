package feature

// mail_error_leak_test.go — 邮箱后台页不直出内部错误（审计 CQ-009 / 第三波收口，迁移 276 的验收）。
//
// 改造前：四个页面 handler 一律把 err.Error() 拼进 ?err= 或直接塞进渲染数据。service 上抛的
// PostgreSQL 原文（relation "mail_accounts"、SQLSTATE 42P01）会原样摆到运营面前 ——
// 后台不是可信边界。现在统一走 internal/module/mail/inbound/http/mail_err.go 的
// mailErrPageText（命中 enums 白名单 → 翻成当前语言；否则记日志 + mail.err.internal）。
//
// 断言是**强**的：响应体 / 重定向目标里出现 SQLSTATE / uq_ / pg_ / relation " 任一即失败；
// 同时断言归口文案确实出现 —— 只断言「没有泄漏」会被「把页面渲染成空白」蒙混过去。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	"go_wp/internal/templates"
)

// mailHead 截断长文本（断言失败时的输出要能读）。
func mailHead(s string) string {
	if len(s) <= 400 {
		return s
	}
	return s[:400] + "…"
}

// mailInternText 归口文案的中文兜底。
//
// 测试进程不初始化 i18n 组件，pkg/i18n 的兜底链会落到调用方给的中文原文
// （生产里命中 seed 词条 mail.err.internal，值是同一句话）。
const mailInternText = "操作失败，请稍后重试"

// mailLeakTokens 内部细节指纹：PG 原文与库结构里一定出现、业务文案里一定不出现。
var mailLeakTokens = []string{
	"SQLSTATE", "uq_", "pg_", `relation "`, "constraint", "mail_accounts", "mail_templates",
}

// assertMailLeakFree 断言给定文本不含任何内部细节指纹。
func assertMailLeakFree(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range mailLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, mailHead(text))
		}
	}
}

// newMailLeakRouter 只挂本批收口的页面路由（不经三层鉴权链：断言的是响应内容）。
func newMailLeakRouter(t *testing.T, f *mailFixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 渲染错误不会让 c.HTML 失败（HTTP 仍是 200，正文可能整块为空）—— 打进测试日志才看得见。
	engine.Use(func(c *gin.Context) {
		c.Next()
		for _, ge := range c.Errors {
			t.Logf("gin 渲染错误: %v", ge.Err)
		}
	})
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	h := mailhttp.NewMailPageHandle(f.svc)
	engine.GET("/admin/mail", h.MailPage)
	engine.GET("/admin/mail/contacts", h.MailContactsPage)
	engine.GET("/admin/mail/automation", h.MailAutomationPage)
	engine.POST("/admin/mail/account/save", h.MailAccountSave)
	engine.POST("/admin/mail/template/save", h.MailTemplateSave)
	engine.POST("/admin/mail/automation/save", h.MailAutomationSave)
	return engine
}

// mailPOST 发一个表单 POST 并回响应（页面路径靠 302 + Location 表达结果）。
func mailPOST(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// mailRedirectErr 取重定向 Location 上的 err 参数（页面就是这样把它渲染出来的）。
func mailRedirectErr(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusFound {
		t.Fatalf("应回 302，实际 %d，body=%s", rec.Code, mailHead(rec.Body.String()))
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("Location 不可解析: %v", err)
	}
	return loc.Query().Get("err")
}

// TestMailPageHidesInternalError 发信账号列表取数失败（表被删 = 真实 PG 错误）：
// 页面只给归口文案，不带表名 / SQLSTATE。
func TestMailPageHidesInternalError(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	engine := newMailLeakRouter(t, f)

	// 制造**真实**基础设施错误：把表删掉，service 拿到的就是 PG 自己的原文。
	if err := f.db.Exec("DROP TABLE IF EXISTS mail_accounts CASCADE").Error; err != nil {
		t.Fatalf("制造故障失败：%v", err)
	}
	// 反证：原文确实带表名与 SQLSTATE —— 直出的话这两样都会出现在页面上。
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

// TestMailAccountSaveRedirectHidesInternalError 保存发信账号撞上基础设施错误：
// 302 的 ?err= 只带归口文案。
func TestMailAccountSaveRedirectHidesInternalError(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	engine := newMailLeakRouter(t, f)
	if err := f.db.Exec("DROP TABLE IF EXISTS mail_accounts CASCADE").Error; err != nil {
		t.Fatalf("制造故障失败：%v", err)
	}
	// 反证：同一份表单在 service 上拿到的原文带表名与 SQLSTATE。
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
	got := mailRedirectErr(t, rec)
	assertMailLeakFree(t, "保存发信账号的重定向 ?err=", got)
	if got != mailInternText {
		t.Errorf("?err= 应为归口文案 %q，实际 %q", mailInternText, got)
	}
}

// TestMailAutomationPageHidesInternalError 自动化列表取数失败：渲染数据里的 Err 同样收口。
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

// TestMailContactsPageHidesInternalError 联系人页取数失败：同样是归口文案 + 整页渲染完。
//
// 这条同时守住联系人页错误分支的**空值注入**：模板在提示条之后就用 .Keyword / .Status /
// .Page / len(.Contacts) 取值，只注入 Err 会让 Jet 在那一行中断
// （HTTP 仍是 200、正文整块消失）—— 那种失败看起来像「页面本来就是空的」。
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
	// 整页渲染完：layout 的收尾标签必须在（Jet 中断时它一定不在）。
	if !strings.Contains(body, "</html>") {
		t.Error("联系人页渲染被中断（缺少 </html>）—— 错误分支缺模板必需的键")
	}
}

// TestMailBusinessMessageStillVisible 归口助手不是「一律吞成通用文案」：
//
//	· enums 白名单里的业务错误照原样回带（运营要知道哪一项不合法）；
//	· 本页自造的表单校验文案（「第 N 行…」）照原样回带 —— 那是照着改的依据。
//
// 测试进程未初始化 i18n，白名单 key 命中后走的是「词条缺失 → 兜底给 key」这一支；
// 生产里该 key 已 seed 成中文，页面上看到的是「参数不合法」。
func TestMailBusinessMessageStillVisible(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	engine := newMailLeakRouter(t, f)

	// ① 业务错误（service 产出，命中白名单）：TemplateKey 为空 → mail.err.invalidParam。
	rec := mailPOST(engine, "/admin/mail/template/save", url.Values{"locale": {"zh-CN"}})
	got := mailRedirectErr(t, rec)
	if got != mailenums.ErrInvalidParam {
		t.Errorf("白名单业务错误应原样回带 %q，实际 %q", mailenums.ErrInvalidParam, got)
	}
	if got == mailInternText {
		t.Error("白名单业务错误被误吞成归口文案 —— 运营再也看不到「哪一项不合法」")
	}
	assertMailLeakFree(t, "业务错误重定向", got)

	// ② 本页表单校验文案：名称必填。校验失败**回显表单（200）**而不是 302 ——
	// 用户刚改的步骤类型必须留在页面上，302 会把页面翻回存库里的旧状态（改类型就成了死循环）。
	rec = mailPOST(engine, "/admin/mail/automation/save", url.Values{"name": {""}})
	if rec.Code != http.StatusOK {
		t.Fatalf("表单校验失败应回显表单（200），实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "流程名称不能为空") {
		t.Errorf("表单校验文案应回显在页面上，实际：%s", mailHead(body))
	}
	if !strings.Contains(body, "</html>") {
		t.Error("表单校验回显渲染被中断（缺少 </html>）—— 错误分支缺模板必需的键")
	}
}
