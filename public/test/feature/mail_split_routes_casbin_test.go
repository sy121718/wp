package feature

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"go_wp/config"
	"go_wp/internal/middleware/adminctx"
	builtin "go_wp/internal/middleware/builtin"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	captcharouter "go_wp/internal/module/common/captcha/router"
	maildto "go_wp/internal/module/mail/dto"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	mailmodel "go_wp/internal/module/mail/model"
	mailservice "go_wp/internal/module/mail/service"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	"go_wp/pkg/captcha"
	pkgcasbin "go_wp/pkg/casbin"
	mw "go_wp/pkg/middleware"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/html"
	"gorm.io/gorm"
)

const (
	mailSplitRoutesPlainAdmin = "mail_split_routes_plain_admin"

	mailSplitRoutesLegacyPath = "/admin/mail/marketing"

	mailSplitRoutesContactsPath = "/admin/mail/contacts"

	mailSplitRoutesAccountSaveAPI = "/api/mail/account/save"
)

type mailSplitRoutesEnv struct {
	Engine     *gin.Engine
	DB         *gorm.DB
	SuperAdmin *support.AdminSession
	PlainAdmin *support.AdminSession
	PlainID    int64
}

func newMailSplitRoutesEnv(t *testing.T) *mailSplitRoutesEnv {
	t.Helper()

	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Fatal("NewMigratedPGTestDB 返回 nil：没有拿到真实 PostgreSQL 库")
	}
	for _, table := range []string{
		"sys_admin", "sys_menus", "sys_casbin_rule",
		"mail_accounts", "mail_templates", "mail_contacts", "mail_campaigns", "mail_automations",
	} {
		var regclass *string
		if err := db.Raw("SELECT to_regclass(?::text)::text", table).Scan(&regclass).Error; err != nil {
			t.Fatalf("检查表 %s 是否存在失败: %v", table, err)
		}
		if regclass == nil || *regclass == "" {
			t.Fatalf("测试库缺少表 %s：生产迁移未生效", table)
		}
	}

	if err := support.SeedTestAdmin(t, db, support.TestAdminUsername, support.TestAdminPassword); err != nil {
		t.Fatalf("准备测试超管失败: %v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行业务权限 seed 失败: %v", err)
	}
	if err := pkgcasbin.InitCasbin(db); err != nil {
		t.Fatalf("初始化 Casbin 失败: %v", err)
	}
	t.Cleanup(func() {
		if err := pkgcasbin.Close(); err != nil {
			t.Errorf("关闭 Casbin 失败: %v", err)
		}
		_ = captcha.Close()
	})

	hash, err := bcrypt.GenerateFromPassword([]byte(support.TestAdminPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}

	if err := db.Exec("DELETE FROM sys_admin WHERE username = ?", mailSplitRoutesPlainAdmin).Error; err != nil {
		t.Fatalf("清理普通管理员测试账号失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO sys_admin (username, password, status, is_admin)
		VALUES (?, ?, 1, 0)`, mailSplitRoutesPlainAdmin, string(hash)).Error; err != nil {
		t.Fatalf("写入普通管理员测试账号失败: %v", err)
	}
	var plainRow struct {
		ID      int64
		IsAdmin int
	}
	if err := db.Raw("SELECT id, is_admin FROM sys_admin WHERE username = ?", mailSplitRoutesPlainAdmin).
		Scan(&plainRow).Error; err != nil {
		t.Fatalf("读取普通管理员测试账号失败: %v", err)
	}
	if plainRow.ID == 0 || plainRow.IsAdmin != 0 {
		t.Fatalf("普通管理员测试账号前置不成立: id=%d is_admin=%d", plainRow.ID, plainRow.IsAdmin)
	}

	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		GinMode:         gin.TestMode,
		UseDefaultRoute: false,

		InitComponents: false,
		RouteRegistrar: func(engine *gin.Engine) {
			engine.HTMLRender = templates.NewJetHTMLRender("../../../internal/templates", true)
			// 生产装配在基础层挂请求取词作用域；缺了它 c.HTML 的片段会在 t 处渲染失败。
			engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })

			api := engine.Group("/api")
			captcharouter.SetupCaptchaRoutes(api)
			adminAuthz := adminhttp.SetupAdminRoutes(mw.NewRouteGroup(api, mw.RouteProtected), db)

			adminPages := engine.Group("/admin",
				builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(),
				adminctx.PermContextMiddleware(adminAuthz))

			authorizedAPI := mw.NewRouteGroup(
				api.Group("", builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(), builtin.CasbinMiddleware()), mw.RouteProtected)

			if v, cerr := config.GetViper(); cerr == nil && v != nil {
				v.Set("app.secret", "qa-mail-split-routes-secret")
			}
			mailhttp.SetupMailRoutes(authorizedAPI, db, adminPages)
		},
	})
	if err != nil {
		t.Fatalf("初始化测试引擎失败: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := cleanup(); closeErr != nil {
			t.Errorf("清理测试资源失败: %v", closeErr)
		}
	})

	redisAddr, redisSource, err := support.AcquireRedis(t)
	if err != nil {
		t.Fatalf("登录链路需要真实 Redis 会话存储，本地与容器两级都不可用: %v", err)
	}
	if err := support.SetupRedisForTestAt(t, redisAddr); err != nil {
		t.Fatalf("初始化测试 Redis（%s，来源 %s）失败: %v", redisAddr, redisSource, err)
	}

	return &mailSplitRoutesEnv{
		Engine:     engine,
		DB:         db,
		SuperAdmin: mailSplitRoutesLogin(t, engine, support.TestAdminUsername, "超管"),
		PlainAdmin: mailSplitRoutesLogin(t, engine, mailSplitRoutesPlainAdmin, "普通管理员"),
		PlainID:    plainRow.ID,
	}
}

func mailSplitRoutesLogin(t *testing.T, engine *gin.Engine, username, label string) *support.AdminSession {
	t.Helper()
	sess, err := support.LoginAdminSession(t, engine, username, support.TestAdminPassword)
	if err != nil {
		t.Fatalf("%s（%s）登录失败: %v", label, username, err)
	}
	if sess.Cookie == "" || sess.CSRFToken == "" {
		t.Fatalf("%s（%s）登录未拿到完整会话：cookie=%q csrf_token=%q", label, username, sess.Cookie, sess.CSRFToken)
	}
	return sess
}

func mailSplitRoutesGET(t *testing.T, env *mailSplitRoutesEnv, sess *support.AdminSession, path string) *httptest.ResponseRecorder {
	t.Helper()
	recorder, err := support.SendRequest(env.Engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   path,
		Headers: map[string]string{
			"Cookie": sess.Cookie,
			"Accept": "text/html,application/xhtml+xml",
		},
	})
	if err != nil {
		t.Fatalf("发送 GET %s 失败: %v", path, err)
	}
	return recorder
}

func mailSplitRoutesPOST(t *testing.T, env *mailSplitRoutesEnv, sess *support.AdminSession, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	if form == nil {
		form = url.Values{}
	}
	recorder, err := support.SendRequest(env.Engine, support.RequestOptions{
		Method: http.MethodPost,
		Path:   path,
		Headers: map[string]string{
			"Cookie":       sess.Cookie,
			"X-CSRF-Token": sess.CSRFToken,
			"Content-Type": "application/x-www-form-urlencoded",
		},
		RawBody: []byte(form.Encode()),
	})
	if err != nil {
		t.Fatalf("发送 POST %s 失败: %v", path, err)
	}
	return recorder
}

type mailSplitRoutesJump struct {
	State string

	Back string

	BackPath string

	FromMeta bool
}

func mailSplitRoutesJumpPage(t *testing.T, recorder *httptest.ResponseRecorder) mailSplitRoutesJump {
	t.Helper()
	body := recorder.Body.String()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("提示页不是可解析的 HTML: %v（正文前 400 字：%s）", err, mailSplitRoutesHead(body, 400))
	}

	var state, fromMeta, fromLink string
	var walk func(node *html.Node, inJump bool)
	walk = func(node *html.Node, inJump bool) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "div":
				if value := mailSplitRoutesHTMLAttr(node, "data-jump-state"); value != "" {
					if state == "" {
						state = value
					}
					inJump = true
				}
			case "meta":
				if fromMeta == "" && strings.EqualFold(mailSplitRoutesHTMLAttr(node, "http-equiv"), "refresh") {
					if content := mailSplitRoutesHTMLAttr(node, "content"); content != "" {
						if idx := strings.Index(content, "url="); idx >= 0 {
							fromMeta = strings.TrimSpace(content[idx+len("url="):])
						}
					}
				}
			case "a":
				if inJump && fromLink == "" {
					if href := mailSplitRoutesHTMLAttr(node, "href"); strings.HasPrefix(href, "/admin/") {
						fromLink = href
					}
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, inJump)
		}
	}
	walk(doc, false)

	if state == "" {
		t.Fatalf("响应不是整页提示页（缺 data-jump-state；这条通道的出口是 shell 层的 RenderJump）；正文前 400 字：%s",
			mailSplitRoutesHead(body, 400))
	}
	back, fromMetaRefresh := fromMeta, true
	if back == "" {
		back, fromMetaRefresh = fromLink, false
	}
	if back == "" {
		t.Fatalf("提示页里找不到回跳地址（meta refresh 与 jump 卡片内的 /admin 链接都没有）；正文前 400 字：%s",
			mailSplitRoutesHead(body, 400))
	}
	parsed, err := url.Parse(back)
	if err != nil {
		t.Fatalf("回跳地址 %q 解析失败: %v", back, err)
	}
	return mailSplitRoutesJump{State: state, Back: back, BackPath: parsed.Path, FromMeta: fromMetaRefresh}
}

func mailSplitRoutesHTMLAttr(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func mailSplitRoutesHead(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func mailSplitRoutesPageCases() []struct {
	name string
	path string
} {
	return []struct {
		name string
		path string
	}{
		{"发信账号", "/admin/mail"},
		{"邮件模板", "/admin/mail/templates"},
		{"联系人", "/admin/mail/contacts"},
		{"群发活动", "/admin/mail/campaigns"},
		{"自动化流程", "/admin/mail/automation"},
		{"运行记录", "/admin/mail/automation/runs"},
	}
}

func TestMailSplitRoutesSixPagesOpenThroughRealMiddlewareChain(t *testing.T) {
	env := newMailSplitRoutesEnv(t)

	for _, tc := range mailSplitRoutesPageCases() {
		t.Run(tc.name, func(t *testing.T) {
			recorder := mailSplitRoutesGET(t, env, env.SuperAdmin, tc.path)
			body := recorder.Body.String()
			if recorder.Code != http.StatusOK {
				t.Fatalf("超管访问 %s 应 200: got=%d body=%s", tc.path, recorder.Code, mailSplitRoutesHead(body, 400))
			}
			if !strings.Contains(body, "</html>") {
				t.Fatalf("%s 页面未完整渲染（缺 </html>，模板中途中断）: %s", tc.path, mailSplitRoutesHead(body, 400))
			}
			if !strings.Contains(body, "<h1") {
				t.Errorf("%s 页面缺 <h1>", tc.path)
			}
			if strings.Contains(body, mailSplitRoutesLegacyPath) {
				t.Errorf("%s 页面正文出现了旧路径 %s（拆页残留：模板或菜单里还挂着已删的页面）",
					tc.path, mailSplitRoutesLegacyPath)
			}
		})
	}
}

func TestMailSplitRoutesLegacyMarketingIsRedirectOnly(t *testing.T) {
	env := newMailSplitRoutesEnv(t)

	t.Run("无查询参数时原样跳到联系人页", func(t *testing.T) {
		recorder := mailSplitRoutesGET(t, env, env.SuperAdmin, mailSplitRoutesLegacyPath)
		if recorder.Code != http.StatusFound {
			t.Fatalf("%s 应 302: got=%d body=%s",
				mailSplitRoutesLegacyPath, recorder.Code, mailSplitRoutesHead(recorder.Body.String(), 300))
		}
		if loc := recorder.Header().Get("Location"); loc != mailSplitRoutesContactsPath {
			t.Errorf("Location 应为 %q（无 query 原样跳），实际 %q", mailSplitRoutesContactsPath, loc)
		}
	})

	t.Run("只透传筛选白名单参数", func(t *testing.T) {
		recorder := mailSplitRoutesGET(t, env, env.SuperAdmin,
			mailSplitRoutesLegacyPath+"?keyword=abc&status=subscribed&page=3&foo=bar&utm_source=x&keyword=")
		if recorder.Code != http.StatusFound {
			t.Fatalf("带查询参数时也应 302: got=%d body=%s", recorder.Code, mailSplitRoutesHead(recorder.Body.String(), 300))
		}
		loc := recorder.Header().Get("Location")
		parsed, err := url.Parse(loc)
		if err != nil {
			t.Fatalf("Location %q 解析失败: %v", loc, err)
		}
		if parsed.Path != mailSplitRoutesContactsPath {
			t.Fatalf("Location 路径应为 %q，实际 %q（Location=%q）", mailSplitRoutesContactsPath, parsed.Path, loc)
		}
		q := parsed.Query()
		if q.Get("keyword") != "abc" || q.Get("status") != "subscribed" || q.Get("page") != "3" {
			t.Errorf("筛选参数未透传：keyword=%q status=%q page=%q（Location=%q）",
				q.Get("keyword"), q.Get("status"), q.Get("page"), loc)
		}
		for _, dropped := range []string{"foo", "utm_source"} {
			if _, ok := q[dropped]; ok {
				t.Errorf("非白名单参数 %q 被透传了（Location=%q）", dropped, loc)
			}
		}
		if len(q) != 3 {
			t.Errorf("查询参数应只留 keyword/status/page 三个，实际 %d 个：%v", len(q), q)
		}
	})

	t.Run("落点页必须打得开", func(t *testing.T) {
		recorder := mailSplitRoutesGET(t, env, env.SuperAdmin, mailSplitRoutesContactsPath)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s 应 200（否则 302 是把用户丢到打不开的页面）: got=%d body=%s",
				mailSplitRoutesContactsPath, recorder.Code, mailSplitRoutesHead(recorder.Body.String(), 300))
		}
	})
}

func TestMailSplitPageGETRequiresCasbinPolicy(t *testing.T) {
	env := newMailSplitRoutesEnv(t)

	subject := strconv.FormatInt(env.PlainID, 10)
	var policyCount int64
	if err := env.DB.Raw(`SELECT count(*) FROM sys_casbin_rule
		WHERE ptype = 'p' AND v0 = ? AND v1 = ?`, subject, mailSplitRoutesAccountSaveAPI).
		Scan(&policyCount).Error; err != nil {
		t.Fatalf("查询普通管理员的 %s 策略失败: %v", mailSplitRoutesAccountSaveAPI, err)
	}
	if policyCount != 0 {
		t.Fatalf("反例前提不成立：普通管理员（id=%s）已有 %d 条 %s 策略",
			subject, policyCount, mailSplitRoutesAccountSaveAPI)
	}

	profile, err := support.SendRequest(env.Engine, support.RequestOptions{
		Method:  http.MethodGet,
		Path:    "/api/admin/profile",
		Headers: map[string]string{"Cookie": env.PlainAdmin.Cookie},
	})
	if err != nil {
		t.Fatalf("发送 /api/admin/profile 请求失败: %v", err)
	}
	if profile.Code != http.StatusOK {
		t.Fatalf("普通管理员会话应有效（/api/admin/profile 期望 200）: got=%d body=%s",
			profile.Code, mailSplitRoutesHead(profile.Body.String(), 300))
	}

	for _, tc := range mailSplitRoutesPageCases() {
		recorder := mailSplitRoutesGET(t, env, env.PlainAdmin, tc.path)
		if recorder.Code != http.StatusForbidden {
			t.Errorf("无读策略的普通管理员打开 %s 应 403（页面 GET 与 API 同一条链）: got=%d",
				tc.path, recorder.Code)
			continue
		}
		if body := recorder.Body.String(); strings.Contains(body, `<table class="data-table">`) {
			t.Errorf("%s 的 403 响应里出现了列表页表格 —— 页面被渲染了: %s",
				tc.path, mailSplitRoutesHead(body, 300))
		}
	}

	for _, tc := range mailSplitRoutesPageCases() {
		recorder := mailSplitRoutesGET(t, env, env.SuperAdmin, tc.path)
		if recorder.Code != http.StatusOK {
			t.Errorf("超管打开 %s 应 200: got=%d body=%s",
				tc.path, recorder.Code, mailSplitRoutesHead(recorder.Body.String(), 300))
		}
	}

	form := url.Values{"name": {"路由验收账号"}, "purpose": {"marketing"}}
	forbidden := mailSplitRoutesPOST(t, env, env.PlainAdmin, "/admin/mail/account/save", form)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("无策略的普通管理员 POST /admin/mail/account/save 应 403: got=%d body=%s",
			forbidden.Code, mailSplitRoutesHead(forbidden.Body.String(), 300))
	}

	if err := env.DB.Exec(`INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, ?, 'POST', 'mail:account_save')
		ON CONFLICT (ptype, v0, v1, v2, v3, v4, v5) DO NOTHING`,
		subject, mailSplitRoutesAccountSaveAPI).Error; err != nil {
		t.Fatalf("补写 %s 策略失败: %v", mailSplitRoutesAccountSaveAPI, err)
	}
	if err := pkgcasbin.ReloadPolicy(); err != nil {
		t.Fatalf("重载 Casbin 策略失败: %v", err)
	}
	allowed := mailSplitRoutesPOST(t, env, env.PlainAdmin, "/admin/mail/account/save", form)
	if allowed.Code == http.StatusForbidden {
		t.Fatalf("补上策略后不应再 403（说明 403 由该条 Casbin 路径决定）: body=%s",
			mailSplitRoutesHead(allowed.Body.String(), 300))
	}

	if err := env.DB.Exec(`INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, ?, 'GET', 'mail:account_list')
		ON CONFLICT (ptype, v0, v1, v2, v3, v4, v5) DO NOTHING`,
		subject, "/api/mail/account/list").Error; err != nil {
		t.Fatalf("补写 /api/mail/account/list 策略失败: %v", err)
	}
	if err := pkgcasbin.ReloadPolicy(); err != nil {
		t.Fatalf("重载 Casbin 策略失败: %v", err)
	}
	pageAllowed := mailSplitRoutesGET(t, env, env.PlainAdmin, "/admin/mail")
	if pageAllowed.Code != http.StatusOK {
		t.Fatalf("补上读策略后 /admin/mail 应 200: got=%d body=%s",
			pageAllowed.Code, mailSplitRoutesHead(pageAllowed.Body.String(), 300))
	}
	otherPage := mailSplitRoutesGET(t, env, env.PlainAdmin, "/admin/mail/contacts")
	if otherPage.Code != http.StatusForbidden {
		t.Fatalf("只补一条读策略不应放行别的页：/admin/mail/contacts got=%d", otherPage.Code)
	}
}

// TestMailSplitPageWritePostsReturnHTMLFragments 取代旧的「整页提示页」断言：
// 邮件页面写端点已统一成 HTMX + Jet 片段，成功换结果区、失败换错误区，都不再整页跳转。
func TestMailSplitPageWritePostsReturnHTMLFragments(t *testing.T) {
	env := newMailSplitRoutesEnv(t)

	contactID := mailSplitContactSeed(t, env,
		fmt.Sprintf("qa.route.status.%d@example.invalid", mailSplitContactNext()))

	cases := []struct {
		name   string
		path   string
		form   url.Values
		status int
		marker []string
	}{
		{
			name: "账号保存",
			path: "/admin/mail/account/save",
			form: url.Values{
				"name": {"路由验收账号"}, "purpose": {"marketing"},
				"from_name": {"QA"}, "from_email": {"qa@example.invalid"},
				"host": {"smtp.example.invalid"}, "port": {"25"},
				"username": {"qa"}, "password": {"qa-secret"},
			},
			status: http.StatusOK,
			marker: []string{`role="status"`},
		},
		{
			name: "模板保存",
			path: "/admin/mail/template/save",
			form: url.Values{
				"template_key": {"qa_route_tpl"}, "locale": {"zh-CN"},
				"name": {"路由验收模板"}, "subject": {"路由验收主题"},
				"body_html": {"<p>hi</p>"},
			},
			status: http.StatusOK,
			marker: []string{`role="status"`},
		},
		{
			name: "联系人导入",
			path: "/admin/mail/contact/import",
			form: url.Values{
				"content":          {"qa-import@example.invalid"},
				"consent_declared": {"1"},
				"consent_source":   {"qa 路由验收"},
			},
			status: http.StatusOK,
			marker: []string{`role="status"`},
		},
		{
			name:   "联系人状态变更",
			path:   "/admin/mail/contact/status",
			form:   url.Values{"id": {strconv.FormatUint(contactID, 10)}, "status": {"subscribed"}},
			status: http.StatusOK,
			marker: []string{`role="status"`},
		},
		{
			name:   "活动保存缺必填",
			path:   "/admin/mail/campaign/save",
			form:   url.Values{"name": {"路由验收活动"}},
			status: http.StatusBadRequest,
			marker: []string{`data-ui-error`},
		},
		{
			name:   "活动启动不存在",
			path:   "/admin/mail/campaign/start",
			form:   url.Values{"id": {"999999901"}},
			status: http.StatusBadRequest,
			marker: []string{`data-ui-error`},
		},
		{
			name:   "活动删除不存在",
			path:   "/admin/mail/campaign/delete",
			form:   url.Values{"id": {"999999902"}},
			status: http.StatusBadRequest,
			marker: []string{`data-ui-error`},
		},
		{
			name:   "流程状态非法",
			path:   "/admin/mail/automation/status",
			form:   url.Values{"id": {"999999903"}, "status": {"bogus"}},
			status: http.StatusBadRequest,
			marker: []string{`data-ui-error`},
		},
		{
			name:   "流程删除不存在",
			path:   "/admin/mail/automation/delete",
			form:   url.Values{"id": {"999999904"}},
			status: http.StatusBadRequest,
			marker: []string{`data-ui-error`},
		},
		{
			name:   "自动化 tick",
			path:   "/admin/mail/automation/tick",
			form:   url.Values{},
			status: http.StatusOK,
			marker: []string{`role="status"`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := mailSplitRoutesPOST(t, env, env.SuperAdmin, tc.path, tc.form)
			if recorder.Code != tc.status {
				t.Fatalf("POST %s 应 %d: got=%d body=%s",
					tc.path, tc.status, recorder.Code, mailSplitRoutesHead(recorder.Body.String(), 400))
			}
			if cc := recorder.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
				t.Errorf("写响应应带 no-store，得到 %q", cc)
			}
			if loc := recorder.Header().Get("Location"); loc != "" {
				t.Errorf("写响应不该带 Location（那是旧 302 通道），得到 %q", loc)
			}
			if ct := recorder.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Errorf("写响应必须是 HTML 片段，实际 %q", ct)
			}
			body := recorder.Body.String()
			for _, marker := range tc.marker {
				if !strings.Contains(body, marker) {
					t.Errorf("POST %s 的响应缺少 %q；正文前 400 字：%s",
						tc.path, marker, mailSplitRoutesHead(body, 400))
				}
			}
			for _, forbidden := range []string{"<!DOCTYPE", "<html", "<body"} {
				if strings.Contains(body, forbidden) {
					t.Errorf("局部响应不得携带整页壳 %q：%s", forbidden, mailSplitRoutesHead(body, 400))
				}
			}
		})
	}
}

// 联系人写端点已统一为「标准表单 + Jet 片段」，权限点与 METHOD+PATH 不变，
// 本用例仍验证「没有策略就 403、补上策略才落库」，但请求与断言都落在真实协议上。
type mailSplitContactWrite struct {
	name string
	path string
	api  string
	code string

	build func(t *testing.T, env *mailSplitRoutesEnv) url.Values

	check func(t *testing.T, env *mailSplitRoutesEnv, form url.Values)

	checkDenied func(t *testing.T, env *mailSplitRoutesEnv, form url.Values)
}

func mailSplitContactWrites() []mailSplitContactWrite {
	var deletedEmail string
	var bulkEmails []string
	tagged := map[string]string{}
	return []mailSplitContactWrite{
		{
			name: "联系人保存（新建走同一端点）",
			path: "/api/mail/contact/save", api: "/api/mail/contact/save", code: "mail:contact_save",
			build: func(t *testing.T, _ *mailSplitRoutesEnv) url.Values {
				return url.Values{
					"email":  {fmt.Sprintf("qa.form.save.%d@example.invalid", mailSplitContactNext())},
					"name":   {"Casbin 表单验收"},
					"status": {string(mailmodel.ContactStatusPending)},
				}
			},
			check: func(t *testing.T, env *mailSplitRoutesEnv, form url.Values) {
				email := form.Get("email")
				if !mailSplitContactExists(t, env, email) {
					t.Fatalf("放行后联系人 %s 应已落库", email)
				}
			},
			checkDenied: func(t *testing.T, env *mailSplitRoutesEnv, form url.Values) {
				email := form.Get("email")
				if mailSplitContactExists(t, env, email) {
					t.Fatalf("403 的请求不该写库，但 %s 已经存在了", email)
				}
			},
		},
		{
			name: "联系人删除（单条）",
			path: "/api/mail/contact/delete", api: "/api/mail/contact/delete", code: "mail:contact_delete",
			build: func(t *testing.T, env *mailSplitRoutesEnv) url.Values {
				deletedEmail = fmt.Sprintf("qa.form.del.%d@example.invalid", mailSplitContactNext())
				return url.Values{"id": {strconv.FormatUint(mailSplitContactSeed(t, env, deletedEmail), 10)}}
			},
			check: func(t *testing.T, env *mailSplitRoutesEnv, _ url.Values) {
				if mailSplitContactExists(t, env, deletedEmail) {
					t.Fatalf("放行后 %s 应已删除", deletedEmail)
				}
			},
			checkDenied: func(t *testing.T, env *mailSplitRoutesEnv, _ url.Values) {
				if !mailSplitContactExists(t, env, deletedEmail) {
					t.Fatalf("403 的删除不该动数据，但 %s 已经不在了", deletedEmail)
				}
			},
		},
		{
			name: "联系人删除（批量，与单条同路径同权限点）",
			path: "/api/mail/contact/delete", api: "/api/mail/contact/delete", code: "mail:contact_delete",
			build: func(t *testing.T, env *mailSplitRoutesEnv) url.Values {
				bulkEmails = make([]string, 0, 2)
				ids := make([]string, 0, 2)
				for i := 0; i < 2; i++ {
					email := fmt.Sprintf("qa.form.bulk.%d@example.invalid", mailSplitContactNext())
					bulkEmails = append(bulkEmails, email)
					ids = append(ids, strconv.FormatUint(mailSplitContactSeed(t, env, email), 10))
				}
				return url.Values{"ids": ids}
			},
			check: func(t *testing.T, env *mailSplitRoutesEnv, _ url.Values) {
				for _, email := range bulkEmails {
					if mailSplitContactExists(t, env, email) {
						t.Fatalf("放行后 %s 应已删除", email)
					}
				}
			},
			checkDenied: func(t *testing.T, env *mailSplitRoutesEnv, _ url.Values) {
				for _, email := range bulkEmails {
					if !mailSplitContactExists(t, env, email) {
						t.Fatalf("403 的批量删除不该动数据，但 %s 已经不在了", email)
					}
				}
			},
		},
		{
			name: "联系人打标签（批量，非破坏动作）",
			path: "/api/mail/contact/tag", api: "/api/mail/contact/tag", code: "mail:contact_tag",
			build: func(t *testing.T, env *mailSplitRoutesEnv) url.Values {
				seq := mailSplitContactNext()
				email := fmt.Sprintf("qa.form.tag.%d@example.invalid", seq)
				tag := fmt.Sprintf("qa-form-%d", seq)
				tagged["tag"] = tag
				return url.Values{
					"ids": {strconv.FormatUint(mailSplitContactSeed(t, env, email), 10)},
					"add": {tag},
				}
			},
			check: func(t *testing.T, env *mailSplitRoutesEnv, form url.Values) {
				ids := form["ids"]
				if !mailSplitContactTagged(t, env, ids[0], tagged["tag"]) {
					t.Fatalf("放行后联系人 %s 应带上标签 %q", ids[0], tagged["tag"])
				}
			},
			checkDenied: func(t *testing.T, env *mailSplitRoutesEnv, form url.Values) {
				ids := form["ids"]
				if mailSplitContactTagged(t, env, ids[0], tagged["tag"]) {
					t.Fatalf("403 的打标签不该动数据，但 %s 已经带上了 %q", ids[0], tagged["tag"])
				}
			},
		},
	}
}

func TestMailSplitContactWritesRequireCasbinPolicy(t *testing.T) {
	env := newMailSplitRoutesEnv(t)
	subject := strconv.FormatInt(env.PlainID, 10)

	for _, tc := range mailSplitContactWrites() {
		t.Run(tc.name, func(t *testing.T) {
			if err := env.DB.Exec(`DELETE FROM sys_casbin_rule
				WHERE ptype = 'p' AND v0 = ? AND v1 = ? AND v3 = ?`,
				subject, tc.api, tc.code).Error; err != nil {
				t.Fatalf("清理历史策略失败: %v", err)
			}
			if err := pkgcasbin.ReloadPolicy(); err != nil {
				t.Fatalf("重载 Casbin 策略失败: %v", err)
			}
			var policyCount int64
			if err := env.DB.Raw(`SELECT count(*) FROM sys_casbin_rule
				WHERE ptype = 'p' AND v0 = ? AND v1 = ? AND v3 = ?`,
				subject, tc.api, tc.code).Scan(&policyCount).Error; err != nil {
				t.Fatalf("查询普通管理员的 %s 策略失败: %v", tc.api, err)
			}
			if policyCount != 0 {
				t.Fatalf("反例前提不成立：普通管理员（id=%s）已有 %d 条 %s 策略", subject, policyCount, tc.api)
			}

			deniedForm := tc.build(t, env)
			denied := mailSplitRoutesPOST(t, env, env.PlainAdmin, tc.path, deniedForm)
			if denied.Code != http.StatusForbidden {
				t.Fatalf("无策略的普通管理员 POST %s 应 403: got=%d body=%s",
					tc.path, denied.Code, mailSplitRoutesHead(denied.Body.String(), 300))
			}
			tc.checkDenied(t, env, deniedForm)

			asAdmin := mailSplitRoutesPOST(t, env, env.SuperAdmin, tc.path, tc.build(t, env))
			if asAdmin.Code != http.StatusOK {
				t.Fatalf("超管 POST %s 应 200: got=%d body=%s",
					tc.path, asAdmin.Code, mailSplitRoutesHead(asAdmin.Body.String(), 300))
			}
			if ct := asAdmin.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("超管 POST %s 应返回 HTML 片段，实际 %q", tc.path, ct)
			}

			if err := env.DB.Exec(`INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
				VALUES ('p', ?, ?, 'POST', ?)
				ON CONFLICT (ptype, v0, v1, v2, v3, v4, v5) DO NOTHING`,
				subject, tc.api, tc.code).Error; err != nil {
				t.Fatalf("补写 %s 策略失败: %v", tc.api, err)
			}
			if err := pkgcasbin.ReloadPolicy(); err != nil {
				t.Fatalf("重载 Casbin 策略失败: %v", err)
			}
			form := tc.build(t, env)
			allowed := mailSplitRoutesPOST(t, env, env.PlainAdmin, tc.path, form)
			if allowed.Code != http.StatusOK {
				t.Fatalf("补上策略后 %s 应 200（说明 403 由该条 Casbin 路径决定）: got=%d body=%s",
					tc.path, allowed.Code, mailSplitRoutesHead(allowed.Body.String(), 300))
			}
			tc.check(t, env, form)
		})
	}
}

func TestMailSplitContactPageWritePostsAreRetired(t *testing.T) {
	env := newMailSplitRoutesEnv(t)
	for _, path := range []string{
		"/admin/mail/contact/save",
		"/admin/mail/contact/delete",
		"/admin/mail/contacts/bulk-delete",
		"/admin/mail/contacts/bulk-tag",
	} {
		recorder := mailSplitRoutesPOST(t, env, env.SuperAdmin, path, url.Values{"id": {"1"}})
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("旧页面写路由 %s 应已下线（404），实际 %d: body=%s",
				path, recorder.Code, mailSplitRoutesHead(recorder.Body.String(), 200))
		}
	}
}

func mailSplitRoutesJSONPost(t *testing.T, env *mailSplitRoutesEnv, sess *support.AdminSession, path string, payload map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("序列化 %s 的请求体失败: %v", path, err)
	}
	recorder, err := support.SendRequest(env.Engine, support.RequestOptions{
		Method: http.MethodPost,
		Path:   path,
		Headers: map[string]string{
			"Cookie":       sess.Cookie,
			"X-CSRF-Token": sess.CSRFToken,
			"Content-Type": "application/json",
		},
		RawBody: raw,
	})
	if err != nil {
		t.Fatalf("发送 POST %s 失败: %v", path, err)
	}
	return recorder
}

func mailSplitRoutesJSONBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	body := map[string]interface{}{}
	if recorder.Body.Len() == 0 {
		return body
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("回执不是 JSON: %s", mailSplitRoutesHead(recorder.Body.String(), 300))
	}
	return body
}

func mailSplitContactSeed(t *testing.T, env *mailSplitRoutesEnv, email string) uint64 {
	t.Helper()
	svc := mailservice.NewService(mailmodel.NewMailModel(env.DB))
	id, err := svc.CreateContact(context.Background(), &maildto.SaveContactReq{
		Email: email, Name: "Casbin JSON 验收", Status: mailmodel.ContactStatusPending, OperatorID: 1,
	})
	if err != nil {
		t.Fatalf("落联系人 %s 失败: %v", email, err)
	}
	if id == 0 {
		t.Fatalf("落联系人 %s 返回 id=0", email)
	}
	return id
}

func mailSplitContactExists(t *testing.T, env *mailSplitRoutesEnv, email string) bool {
	t.Helper()
	var n int64
	if err := env.DB.Raw(`SELECT count(*) FROM mail_contacts WHERE email = ?`, email).Scan(&n).Error; err != nil {
		t.Fatalf("查询联系人 %s 失败: %v", email, err)
	}
	return n > 0
}

func mailSplitContactTagged(t *testing.T, env *mailSplitRoutesEnv, id, tag string) bool {
	t.Helper()
	contactID, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		t.Fatalf("id %q 不是数字: %v", id, err)
	}
	row, err := mailmodel.NewMailModel(env.DB).GetContact(context.Background(), contactID)
	if err != nil {
		t.Fatalf("读取联系人 %d 失败: %v", contactID, err)
	}
	for _, got := range row.Tags {
		if got == tag {
			return true
		}
	}
	return false
}

var mailSplitContactSeq int64

func mailSplitContactNext() int64 { return atomic.AddInt64(&mailSplitContactSeq, 1) }
