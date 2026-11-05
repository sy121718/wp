package feature

import (
	pageview "go_wp/pkg/view"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	"go_wp/internal/templates"
)

const mailSplitInternText = "操作失败，请稍后重试"

var (
	mailSplitKeyResidueRe = regexp.MustCompile(`admin\.[a-z0-9_]+(?:\.[a-z0-9_]+)+`)
	mailSplitTheadRe      = regexp.MustCompile(`(?s)<thead>.*?</thead>`)
	mailSplitThRe         = regexp.MustCompile(`<th[\s>]`)
)

func mailSplitEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Next()

		for _, item := range c.Errors {
			t.Logf("gin 渲染错误: %v", item.Err)
		}
	})
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	return engine
}

func mailSplitGET(t *testing.T, engine *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	engine.ServeHTTP(rec, req)
	return rec
}

func mailSplitTheadColumns(body string) int {
	thead := mailSplitTheadRe.FindString(body)
	if thead == "" {
		return -1
	}
	return len(mailSplitThRe.FindAllString(thead, -1))
}

func mailSplitKeyResidue(body string) []string {
	seen := map[string]bool{}
	var out []string
	for _, hit := range mailSplitKeyResidueRe.FindAllString(body, -1) {
		if !seen[hit] {
			seen[hit] = true
			out = append(out, hit)
		}
	}
	return out
}

func mailSplitPages() []struct {
	name    string
	path    string
	method  string
	heading string
	cols    int
} {
	return []struct {
		name    string
		path    string
		method  string
		heading string
		cols    int
	}{
		{"发信账号", "/admin/mail", "MailPage", "发信账号", 8},
		{"邮件模板", "/admin/mail/templates", "MailTemplatesPage", "邮件模板", 7},
		{"联系人", "/admin/mail/contacts", "MailContactsPage", "联系人", 8},
		{"群发活动", "/admin/mail/campaigns", "MailCampaignsPage", "群发活动", 6},
		{"自动化流程", "/admin/mail/automation", "MailAutomationPage", "自动化流程", 6},
		{"运行记录", "/admin/mail/automation/runs", "MailAutomationRunsPage", "运行实例", 7},
	}
}

func TestMailSplitSixPagesRenderWithMatchingColumns(t *testing.T) {
	fixture := newMailFeatureFixture(t)
	if fixture == nil {
		return
	}
	handle := mailhttp.NewMailPageHandle(fixture.svc)

	mount := map[string]func(*gin.Engine, string){
		"MailPage": func(e *gin.Engine, p string) {
			e.GET(p, pageview.Slot(mailhttp.AccountsPageTemplate, adminui.RenderPage), handle.MailPage)
		},
		"MailTemplatesPage": func(e *gin.Engine, p string) {
			e.GET(p, pageview.Slot(mailhttp.TemplatesPageTemplate, adminui.RenderPage), handle.MailTemplatesPage)
		},
		"MailContactsPage": func(e *gin.Engine, p string) {
			e.GET(p, pageview.Slot(mailhttp.ContactsPageTemplate, adminui.RenderPage), handle.MailContactsPage)
		},
		"MailCampaignsPage": func(e *gin.Engine, p string) {
			e.GET(p, pageview.Slot(mailhttp.CampaignsPageTemplate, adminui.RenderPage), handle.MailCampaignsPage)
		},
		"MailAutomationPage": func(e *gin.Engine, p string) {
			e.GET(p, pageview.Slot(mailhttp.AutomationPageTemplate, adminui.RenderPage), handle.MailAutomationPage)
		},
		"MailAutomationRunsPage": func(e *gin.Engine, p string) {
			e.GET(p, pageview.Slot(mailhttp.AutomationRunsPageTemplate, adminui.RenderPage), handle.MailAutomationRunsPage)
		},
	}

	for _, page := range mailSplitPages() {
		page := page
		t.Run(page.name, func(t *testing.T) {
			register, ok := mount[page.method]
			if !ok {
				t.Fatalf("用例表里的注册方法 %q 没有对应挂载函数", page.method)
			}
			engine := mailSplitEngine(t)
			register(engine, page.path)

			rec := mailSplitGET(t, engine, page.path)
			body := rec.Body.String()
			if rec.Code != http.StatusOK {
				t.Fatalf("%s 状态码 %d，正文前 400 字：%s", page.path, rec.Code, mailHead(body))
			}

			if !strings.Contains(body, "<h1>") {
				t.Errorf("%s 页面没有 <h1>", page.path)
			}
			if !strings.Contains(body, page.heading) {
				t.Errorf("%s 页面标题缺少文案 %q", page.path, page.heading)
			}

			cols := mailSplitTheadColumns(body)
			if cols != page.cols {
				t.Errorf("%s 表头 %d 列，规格 %d 列", page.path, cols, page.cols)
			}
			empty := fmt.Sprintf(`<td colspan="%d" class="cell-wrap">`, page.cols)
			if !strings.Contains(body, empty) {
				t.Errorf("%s 空态行缺列数对齐：期望 %s", page.path, empty)
			}

			if !strings.Contains(body, "</html>") {
				t.Errorf("%s 文档不完整（缺 </html>），正文前 400 字：%s", page.path, mailHead(body))
			}
			if keys := mailSplitKeyResidue(body); len(keys) > 0 {
				t.Errorf("%s 正文残留未翻译的 i18n key：%v", page.path, keys)
			}
		})
	}
}

func TestMailSplitFailureBranchKeepsFullShellAndHidesInternalError(t *testing.T) {
	cases := []struct {
		name      string
		path      string
		method    string
		dropTable string
	}{
		{"发信账号", "/admin/mail", "MailPage", "mail_accounts"},
		{"邮件模板", "/admin/mail/templates", "MailTemplatesPage", "mail_templates"},
		{"联系人", "/admin/mail/contacts", "MailContactsPage", "mail_contacts"},
		{"群发活动", "/admin/mail/campaigns", "MailCampaignsPage", "mail_campaigns"},
		{"自动化流程", "/admin/mail/automation", "MailAutomationPage", "mail_automations"},
		{"运行记录", "/admin/mail/automation/runs", "MailAutomationRunsPage", "mail_automation_runs"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			fixture := newMailFeatureFixture(t)
			if fixture == nil {
				return
			}
			handle := mailhttp.NewMailPageHandle(fixture.svc)

			mount := map[string]func(*gin.Engine, string){
				"MailPage": func(e *gin.Engine, p string) {
					e.GET(p, pageview.Slot(mailhttp.AccountsPageTemplate, adminui.RenderPage), handle.MailPage)
				},
				"MailTemplatesPage": func(e *gin.Engine, p string) {
					e.GET(p, pageview.Slot(mailhttp.TemplatesPageTemplate, adminui.RenderPage), handle.MailTemplatesPage)
				},
				"MailContactsPage": func(e *gin.Engine, p string) {
					e.GET(p, pageview.Slot(mailhttp.ContactsPageTemplate, adminui.RenderPage), handle.MailContactsPage)
				},
				"MailCampaignsPage": func(e *gin.Engine, p string) {
					e.GET(p, pageview.Slot(mailhttp.CampaignsPageTemplate, adminui.RenderPage), handle.MailCampaignsPage)
				},
				"MailAutomationPage": func(e *gin.Engine, p string) {
					e.GET(p, pageview.Slot(mailhttp.AutomationPageTemplate, adminui.RenderPage), handle.MailAutomationPage)
				},
				"MailAutomationRunsPage": func(e *gin.Engine, p string) {
					e.GET(p, pageview.Slot(mailhttp.AutomationRunsPageTemplate, adminui.RenderPage), handle.MailAutomationRunsPage)
				},
			}
			register, ok := mount[tc.method]
			if !ok {
				t.Fatalf("用例表里的注册方法 %q 没有对应挂载函数", tc.method)
			}

			if err := fixture.db.Exec("DROP TABLE IF EXISTS " + tc.dropTable + " CASCADE").Error; err != nil {
				t.Fatalf("制造 %s 读取失败失败：%v", tc.dropTable, err)
			}

			if fixture.db.Migrator().HasTable(tc.dropTable) {
				t.Fatalf("%s 仍然存在，失败场景没有构造出来", tc.dropTable)
			}

			engine := mailSplitEngine(t)
			register(engine, tc.path)

			rec := mailSplitGET(t, engine, tc.path)
			body := rec.Body.String()
			if rec.Code != http.StatusOK {
				t.Fatalf("%s 失败分支状态码 %d，正文前 400 字：%s", tc.path, rec.Code, mailHead(body))
			}
			if !strings.Contains(body, mailSplitInternText) {
				t.Errorf("%s 失败分支缺少归口文案 %q，正文前 400 字：%s", tc.path, mailSplitInternText, mailHead(body))
			}
			if !strings.Contains(body, "<h1>") {
				t.Errorf("%s 失败分支没有 <h1>（错误页也要注入 title，否则 Jet 会断在半路）", tc.path)
			}
			if !strings.Contains(body, "</html>") {
				t.Errorf("%s 失败分支文档不完整（缺 </html>），正文前 400 字：%s", tc.path, mailHead(body))
			}
			for _, token := range mailLeakTokens {
				if strings.Contains(body, token) {
					t.Errorf("%s 失败分支泄漏内部错误 token %q，正文前 400 字：%s", tc.path, token, mailHead(body))
				}
			}
		})
	}
}

func TestMailSplitLegacyMarketingRedirectsToContacts(t *testing.T) {
	fixture := newMailFeatureFixture(t)
	if fixture == nil {
		return
	}
	handle := mailhttp.NewMailPageHandle(fixture.svc)

	cases := []struct {
		name      string
		query     string
		wantQuery map[string]string
	}{
		{"无筛选参数", "", nil},
		{"keyword 透传", "?keyword=%E5%BC%A0%E4%B8%89", map[string]string{"keyword": "张三"}},
		{"status 与 page 一起透传", "?status=subscribed&page=3", map[string]string{"status": "subscribed", "page": "3"}},
		{"活动类参数被丢弃", "?campaign_page=2&campaign_status=running", nil},
		{"混合参数只留目标页认识的", "?keyword=abc&campaign_page=2", map[string]string{"keyword": "abc"}},
		{"空值参数不产生空 query", "?keyword=&status=", nil},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			engine := mailSplitEngine(t)
			engine.GET(mailSplitLegacyPath, handle.MailMarketingRedirect)

			engine.GET("/admin/mail/contacts",
				pageview.Slot(mailhttp.ContactsPageTemplate, adminui.RenderPage), handle.MailContactsPage)

			rec := mailSplitGET(t, engine, mailSplitLegacyPath+tc.query)
			if rec.Code != http.StatusFound {
				t.Fatalf("%s 状态码 %d，期望 302（Location=%q）", mailSplitLegacyPath, rec.Code, rec.Header().Get("Location"))
			}

			location := rec.Header().Get("Location")
			parsed, err := url.Parse(location)
			if err != nil {
				t.Fatalf("Location %q 不是合法 URL：%v", location, err)
			}
			if parsed.Path != "/admin/mail/contacts" {
				t.Errorf("Location 落点 %q，期望 /admin/mail/contacts", parsed.Path)
			}

			got := parsed.Query()
			if len(got) != len(tc.wantQuery) {
				t.Errorf("透传参数 %v，期望 %v", got, tc.wantQuery)
			}
			for key, want := range tc.wantQuery {
				if got.Get(key) != want {
					t.Errorf("透传参数 %s = %q，期望 %q", key, got.Get(key), want)
				}
			}

			landing := mailSplitGET(t, engine, parsed.RequestURI())
			if landing.Code != http.StatusOK {
				t.Errorf("落点页 %s 状态码 %d，正文前 400 字：%s", parsed.RequestURI(), landing.Code, mailHead(landing.Body.String()))
			}
		})
	}
}
