package feature

// mail_split_render_guard_test.go — 邮件后台六页拆分后的渲染守卫（task-3 验收）。
//
// 拆页把原来的「配置页（账号+模板）」「营销页（联系人+活动）」变成六页一职，
// 有三类事故 go build 拦不住，只有在真正渲染之后逐行数才发现：
//
//  1. 表头列数与空态 colspan 不一致 —— 浏览器把缺的列补在行尾，整表左移，
//     但页面照样返回 200，肉眼只看到「数据对不上」；
//  2. 失败分支没给齐模板必需键 —— Jet 直接断在渲染中间，页面变半截或 500；
//  3. i18n 键没有兜底文案 —— 正文里躺着 admin.mail.xxx 的原始 key。
//
// 本文件把这三类钉住，并断言旧路径只以 302 的形式存在。
// 列数以 ux-review 的 task-2 规格为准：账号 8 / 模板 7 / 联系人 8 / 活动 6 / 流程 6 / 运行记录 7。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	mailhttp "go_wp/internal/module/mail/inbound/http"
	"go_wp/internal/templates"
)

// mailSplitInternText 与 mail_error_leak_test.go 的 mailInternText 同源：
// internal/module/mail/inbound/http/mail_err.go 的 mail.err.internal 在 i18n 未初始化时的兜底中文。
const mailSplitInternText = "操作失败，请稍后重试"

// 旧「邮件营销」页路径（mailSplitLegacyPath）在本包已由
// mail_split_menu_migration_test.go 定义：拆页后它只允许出现在 302 的源位置，
// 不允许再有任何模板指向它（文件层由 mail_split_guard_test.go 钉住）。

var (
	// mailSplitKeyResidueRe 抓渲染后仍留在正文里的 i18n key：取词时没有第二参数兜底，
	// 或键被当成字面量输出。Jet 的取词形态是 .["t"]("admin.mail.x.y", "兜底")，
	// 正常渲染不可能在 HTML 里留下点分形态的 key。
	mailSplitKeyResidueRe = regexp.MustCompile(`admin\.[a-z0-9_]+(?:\.[a-z0-9_]+)+`)
	mailSplitTheadRe      = regexp.MustCompile(`(?s)<thead>.*?</thead>`)
	mailSplitThRe         = regexp.MustCompile(`<th[\s>]`)
)

// mailSplitEngine 建一个只挂页面路由的最小 engine：不起 i18n、不注入 PermSet、不挂 Session/CSRF。
// 页面测试问的是「模板能不能渲染」，鉴权链由真实装配层的用例负责。
func mailSplitEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Next()
		// 渲染错误在 gin 里不会让请求失败，只会让正文变半截；这里把它显式打出来。
		for _, item := range c.Errors {
			t.Logf("gin 渲染错误: %v", item.Err)
		}
	})
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	return engine
}

// mailSplitGET 必须经 engine.ServeHTTP 发请求：直接读 recorder 会拿到默认 200，
// 而 gin 的状态码是延迟写出的（302 之类会被误判成 200）。
func mailSplitGET(t *testing.T, engine *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	engine.ServeHTTP(rec, req)
	return rec
}

// mailSplitTheadColumns 数第一张表的表头列数；没有表返回 -1。
func mailSplitTheadColumns(body string) int {
	thead := mailSplitTheadRe.FindString(body)
	if thead == "" {
		return -1
	}
	return len(mailSplitThRe.FindAllString(thead, -1))
}

// mailSplitKeyResidue 返回正文里残留的 i18n key（去重，保持出现顺序）。
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

// mailSplitPages 六页的规格：路径、注册方法、该页标题文案、表头列数。
// 注册方法用名字而不是方法值 —— mailPageHandle 未导出，表驱动的字段类型写不出这个函数类型。
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

// TestMailSplitSixPagesRenderWithMatchingColumns 六页各自渲染成功，且
// 标题、表头列数、空态 colspan、文档完整性、i18n 兜底五项都成立。
func TestMailSplitSixPagesRenderWithMatchingColumns(t *testing.T) {
	fixture := newMailFeatureFixture(t)
	if fixture == nil {
		return
	}
	handle := mailhttp.NewMailPageHandle(fixture.svc)

	mount := map[string]func(*gin.Engine, string){
		"MailPage":               func(e *gin.Engine, p string) { e.GET(p, handle.MailPage) },
		"MailTemplatesPage":      func(e *gin.Engine, p string) { e.GET(p, handle.MailTemplatesPage) },
		"MailContactsPage":       func(e *gin.Engine, p string) { e.GET(p, handle.MailContactsPage) },
		"MailCampaignsPage":      func(e *gin.Engine, p string) { e.GET(p, handle.MailCampaignsPage) },
		"MailAutomationPage":     func(e *gin.Engine, p string) { e.GET(p, handle.MailAutomationPage) },
		"MailAutomationRunsPage": func(e *gin.Engine, p string) { e.GET(p, handle.MailAutomationRunsPage) },
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

// TestMailSplitFailureBranchKeepsFullShellAndHidesInternalError 逐页把主列表表 DROP 掉，
// 断言失败分支仍然：返回 200、给出归口文案、页面壳完整（h1 + </html>）、不漏内部错误。
//
// 每页单独建 fixture：DROP 会改共享 schema，同一 fixture 里连做六次会互相污染。
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
				"MailPage":               func(e *gin.Engine, p string) { e.GET(p, handle.MailPage) },
				"MailTemplatesPage":      func(e *gin.Engine, p string) { e.GET(p, handle.MailTemplatesPage) },
				"MailContactsPage":       func(e *gin.Engine, p string) { e.GET(p, handle.MailContactsPage) },
				"MailCampaignsPage":      func(e *gin.Engine, p string) { e.GET(p, handle.MailCampaignsPage) },
				"MailAutomationPage":     func(e *gin.Engine, p string) { e.GET(p, handle.MailAutomationPage) },
				"MailAutomationRunsPage": func(e *gin.Engine, p string) { e.GET(p, handle.MailAutomationRunsPage) },
			}
			register, ok := mount[tc.method]
			if !ok {
				t.Fatalf("用例表里的注册方法 %q 没有对应挂载函数", tc.method)
			}

			if err := fixture.db.Exec("DROP TABLE IF EXISTS " + tc.dropTable + " CASCADE").Error; err != nil {
				t.Fatalf("制造 %s 读取失败失败：%v", tc.dropTable, err)
			}
			// 反证：DROP 真的生效了，否则下面断言的是「正常页面也返回 200」这种废话。
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

// TestMailSplitLegacyMarketingRedirectsToContacts 旧页只以 302 存在：
// 落点固定为联系人页，只透传该页认识的关键字（keyword / status / page），
// 活动类参数（campaign_*）按 task-2 裁定丢弃。
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
			// 落点页必须真存在，否则 302 只是把 404 挪了个地方。
			engine.GET("/admin/mail/contacts", handle.MailContactsPage)

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

			// 落点可打开（302 之后用户真的要落到一个能渲染的页面）。
			landing := mailSplitGET(t, engine, parsed.RequestURI())
			if landing.Code != http.StatusOK {
				t.Errorf("落点页 %s 状态码 %d，正文前 400 字：%s", parsed.RequestURI(), landing.Code, mailHead(landing.Body.String()))
			}
		})
	}
}
