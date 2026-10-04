package feature

// mail_split_routes_casbin_test.go — 邮件模块拆页后的**路由级**契约
// （真实 gin 路由 + 真实中间件链 + 真实 PostgreSQL + 真实 Casbin + 真实 Redis 会话）。
//
// 为什么落在 public/test/feature：这里钉的是「/admin 页面组注册了哪些 GET、哪些写操作挂了
// Casbin、失败后回跳到哪个页面」—— 属跨模块的路由注册与中间件装配行为，与同目录
// admin_pages_casbin_guard_test.go、api_auth_test.go 是同一类，装配模式直接沿用它们
// （不另立一套）。页面渲染细节（表头列数、空态 colspan、错误文案白名单）由
// public/test/mail/feature 承担，两边按「钉的是哪一层能力」分工。
//
// 覆盖四项：
//   A. 六页 GET 走完 Session → CSRF → 权限上下文后 200、页面完整渲染，且渲染结果里
//      不再出现旧路径 /admin/mail/marketing（拆页后它只剩 302 一种形态）；
//   B. 旧 /admin/mail/marketing 的 302：Location 固定 /admin/mail/contacts，
//      只透传白名单筛选参数（keyword / status / page），其余参数与空值参数一律丢弃，
//      跟随落点必须 200；
//   C. 「GET 页面裸注册（无 Casbin）」与「写操作挂 Casbin」的差别：普通管理员
//      （is_admin=0、无任何 mail 策略）能打开六页，但写操作 403；再用因果反证
//      （补策略 → 同一请求立刻放行）证明 403 确实由该条 Casbin 路径决定；
//   D. 五组写操作的 302 回跳目标 = 各自所属页面 —— 拆页后最容易回错页的地方。
//
// 前置条件不满足一律 t.Fatal：本包先例不写 t.Skip —— 「账号没建出来 → 登录失败 → Skip」
// 会让 go test 显示 ok 而断言从未执行。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	builtin "go_wp/internal/middleware/builtin"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	captcharouter "go_wp/internal/module/common/captcha/router"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	"go_wp/internal/permission"
	"go_wp/internal/templates"
	"go_wp/internal/web/shell"
	"go_wp/pkg/captcha"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	// mailSplitRoutesPlainAdmin 反例主体：is_admin=0、没有任何 Casbin 策略的后台账号。
	mailSplitRoutesPlainAdmin = "mail_split_routes_plain_admin"
	// mailSplitRoutesLegacyPath 拆页后只允许出现在 302 与测试断言里的旧路径。
	mailSplitRoutesLegacyPath = "/admin/mail/marketing"
	// mailSplitRoutesContactsPath 旧路径 302 的落点。
	mailSplitRoutesContactsPath = "/admin/mail/contacts"
	// mailSplitRoutesAccountSaveAPI 写操作在 Casbin 里真正被校验的 API 路径。
	mailSplitRoutesAccountSaveAPI = "/api/mail/account/save"
)

// mailSplitRoutesEnv 一个用例所需的全部真实资源。
type mailSplitRoutesEnv struct {
	Engine     *gin.Engine
	DB         *gorm.DB
	SuperAdmin *support.AdminSession
	PlainAdmin *support.AdminSession
	PlainID    int64
}

// newMailSplitRoutesEnv 装配与 internal/routers/assembly.go 同构的真实链路。
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

	// 超管必须在业务权限 seed 之前建好（031/050 按 is_admin=1 逐行授策略）。
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
	// 普通管理员：id 交给序列，绝不写固定 id（SeedTestAdmin 会把序列 setval 到 MAX(id)，
	// 固定 id 与 030a 默认超管 seed 撞上时冲突是静默跳过的）。按 username 先删后插。
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
		// 组件由本用例显式装配（DB 是测试库）；config.InitComponents() 会去连生产库，刻意不接。
		InitComponents: false,
		RouteRegistrar: func(engine *gin.Engine) {
			engine.HTMLRender = templates.NewJetHTMLRender("../../../internal/templates", true)

			api := engine.Group("/api")
			captcharouter.SetupCaptchaRoutes(api)
			adminAuthz := adminhttp.SetupAdminRoutes(permission.NewRouteGroup(api), db)
			// /admin 页面组：中间件链与 assembly.go 一致（Session + CSRF + 权限上下文）。
			adminPages := engine.Group("/admin",
				builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(),
				shell.PermContextMiddleware(adminAuthz))
			// 邮件模块：/api/mail/* 业务接口 + /admin/mail* 页面路由一起注册。
			mailhttp.SetupMailRoutes(permission.NewRouteGroup(api), db, adminPages)
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

// mailSplitRoutesLogin 走真实登录链路拿会话；失败一律 Fatal。
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

// mailSplitRoutesGET 带会话的页面 GET（浏览器会带的 Accept 头照抄，避免落到 JSON 分支）。
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

// mailSplitRoutesPOST 带会话与 CSRF 的页面表单 POST（页面写操作是 form-urlencoded）。
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

// mailSplitRoutesLocationPath 取 302 的 Location 并只留路径（错误回跳会带 ?err=...）。
func mailSplitRoutesLocationPath(t *testing.T, recorder *httptest.ResponseRecorder, where string) string {
	t.Helper()
	loc := recorder.Header().Get("Location")
	if loc == "" {
		t.Fatalf("%s：响应没有 Location 头（code=%d body=%s）", where, recorder.Code, mailSplitRoutesHead(recorder.Body.String(), 300))
	}
	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("%s：Location %q 解析失败: %v", where, loc, err)
	}
	return parsed.Path
}

// mailSplitRoutesHead 截取正文前 n 个字符用于失败输出（整页 HTML 会淹没日志）。
func mailSplitRoutesHead(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// mailSplitRoutesPageCases 六页：路径 + 该页标题里必然出现的一段文案。
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

// TestMailSplitRoutesSixPagesOpenThroughRealMiddlewareChain 六页 GET 在完整中间件链下可打开。
//
// 页面测试（public/test/mail/feature）用的是只有 HTMLRender 的最小链路，它证明不了
// 「Session + CSRF + 权限上下文 + 邮件路由注册」这一整条链上六页都在；这里补上。
// 同时断言正文里不再出现旧路径：拆页后侧栏数据来自 sys_menus（521 已改），
// 若哪个模板还硬写着旧链接，这里会直接红。
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

// TestMailSplitRoutesLegacyMarketingIsRedirectOnly 旧路径只剩 302 一种形态。
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

// TestMailSplitPageGETHasNoCasbinButPOSTDoes 读页面裸注册、写操作挂 Casbin。
//
// GET 页面无 Casbin 是项目既有约定（侧栏按权限渲染、但页面本身不拦），本用例不把它判成缺陷，
// 只是**钉住这个差异**：读放行、写必须 403。若哪天一页写操作漏挂了 Casbin，这里变红。
func TestMailSplitPageGETHasNoCasbinButPOSTDoes(t *testing.T) {
	env := newMailSplitRoutesEnv(t)

	// 前置：反例主体必须真的没有该写操作的策略。
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

	// 会话有效性对照：403 必须来自 Casbin，而不是登录态失效。
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
		if recorder.Code != http.StatusOK {
			t.Errorf("无 mail 策略的普通管理员打开 %s 应 200（GET 页面不挂 Casbin）: got=%d",
				tc.path, recorder.Code)
		}
	}

	form := url.Values{"name": {"路由验收账号"}, "purpose": {"marketing"}}
	forbidden := mailSplitRoutesPOST(t, env, env.PlainAdmin, "/admin/mail/account/save", form)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("无策略的普通管理员 POST /admin/mail/account/save 应 403: got=%d body=%s",
			forbidden.Code, mailSplitRoutesHead(forbidden.Body.String(), 300))
	}

	// 因果反证：补上策略后同一请求立刻放行（不再是 403），证明上面的 403 就来自这条路径。
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
}

// TestMailSplitPOSTRedirectsBackToOwningPage 五组写操作的 302 回跳目标。
func TestMailSplitPOSTRedirectsBackToOwningPage(t *testing.T) {
	env := newMailSplitRoutesEnv(t)

	cases := []struct {
		name string
		path string
		form url.Values
		want string
	}{
		{
			name: "账号保存",
			path: "/admin/mail/account/save",
			form: url.Values{
				"name": {"路由验收账号"}, "purpose": {"marketing"},
				"from_name": {"QA"}, "from_email": {"qa@example.invalid"},
				"host": {"smtp.example.invalid"}, "port": {"25"},
				"username": {"qa"}, "password": {"qa-secret"},
				"is_default": {"1"}, "status": {"1"},
			},
			want: "/admin/mail",
		},
		{
			name: "模板保存",
			path: "/admin/mail/template/save",
			form: url.Values{
				"key": {"qa_route_tpl"}, "locale": {"zh-CN"},
				"name": {"路由验收模板"}, "subject": {"路由验收主题"},
				"body": {"<p>hi</p>"},
			},
			want: "/admin/mail/templates",
		},
		{
			name: "联系人导入",
			path: "/admin/mail/contact/import",
			form: url.Values{"emails": {"qa-import@example.invalid"}},
			want: "/admin/mail/contacts",
		},
		{
			name: "联系人状态变更",
			path: "/admin/mail/contact/status",
			form: url.Values{"id": {"1"}, "status": {"subscribed"}},
			want: "/admin/mail/contacts",
		},
		{
			name: "联系人保存",
			path: "/admin/mail/contact/save",
			form: url.Values{
				"id": {"0"}, "email": {"qa-route-contact@example.invalid"},
				"name": {"路由验收联系人"}, "tags": {"qa-route"}, "status": {"pending"},
			},
			want: "/admin/mail/contacts",
		},
		{
			// 用一个不存在的 id：回跳目标与「删成功」相同，但不会动别条用例的数据。
			name: "联系人删除",
			path: "/admin/mail/contact/delete",
			form: url.Values{"id": {"999999"}},
			want: "/admin/mail/contacts",
		},
		{
			name: "联系人批量删除",
			path: "/admin/mail/contacts/bulk-delete",
			form: url.Values{"ids": {"999999"}},
			want: "/admin/mail/contacts",
		},
		{
			name: "联系人批量打标签",
			path: "/admin/mail/contacts/bulk-tag",
			form: url.Values{"ids": {"999999"}, "add": {"qa-route"}},
			want: "/admin/mail/contacts",
		},
		{
			name: "活动保存",
			path: "/admin/mail/campaign/save",
			form: url.Values{"name": {"路由验收活动"}, "template_id": {"1"}, "account_id": {"1"}},
			want: "/admin/mail/campaigns",
		},
		{
			name: "活动启动",
			path: "/admin/mail/campaign/start",
			form: url.Values{"id": {"1"}},
			want: "/admin/mail/campaigns",
		},
		{
			name: "活动删除",
			path: "/admin/mail/campaign/delete",
			form: url.Values{"id": {"1"}},
			want: "/admin/mail/campaigns",
		},
		{
			name: "流程状态变更",
			path: "/admin/mail/automation/status",
			form: url.Values{"id": {"1"}, "status": {"1"}},
			want: "/admin/mail/automation",
		},
		{
			name: "流程删除",
			path: "/admin/mail/automation/delete",
			form: url.Values{"id": {"1"}},
			want: "/admin/mail/automation",
		},
		{
			name: "自动化 tick",
			path: "/admin/mail/automation/tick",
			form: url.Values{},
			want: "/admin/mail/automation/runs",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := mailSplitRoutesPOST(t, env, env.SuperAdmin, tc.path, tc.form)
			if recorder.Code != http.StatusFound {
				t.Fatalf("超管 POST %s 应 302 回跳: got=%d body=%s",
					tc.path, recorder.Code, mailSplitRoutesHead(recorder.Body.String(), 400))
			}
			if got := mailSplitRoutesLocationPath(t, recorder, tc.path); got != tc.want {
				t.Errorf("POST %s 应回跳到 %q，实际 %q（拆页后回错了页）",
					tc.path, tc.want, got)
			}
		})
	}
}

// mailSplitContactCrudRoutes 本批新增的四条写路由（权限路径与 API 同源）。
//
// 批量端点复用单条的 API 路径：策略是按 (api_path, api_method) 授权的，
// 「能删一个」与「能批量删」必须由同一条策略决定（单列一条不会自动生效，
// 但少了它就会让有权限的人在批量入口被挡）。
var mailSplitContactCrudRoutes = []struct {
	name string
	path string
	api  string
	code string
	form url.Values
}{
	{
		name: "联系人保存", path: "/admin/mail/contact/save",
		api: "/api/mail/contact/save", code: "mail:contact_save",
		form: url.Values{"id": {"0"}, "email": {"qa-casbin-contact@example.invalid"}, "name": {"Casbin 验收"}},
	},
	{
		name: "联系人删除", path: "/admin/mail/contact/delete",
		api: "/api/mail/contact/delete", code: "mail:contact_delete",
		form: url.Values{"id": {"999999"}},
	},
	{
		name: "联系人批量删除", path: "/admin/mail/contacts/bulk-delete",
		api: "/api/mail/contact/delete", code: "mail:contact_delete",
		form: url.Values{"ids": {"999999"}},
	},
	{
		name: "联系人批量打标签", path: "/admin/mail/contacts/bulk-tag",
		api: "/api/mail/contact/tag", code: "mail:contact_tag",
		form: url.Values{"ids": {"999999"}, "add": {"qa-casbin"}},
	},
}

// TestMailSplitContactCrudRoutesRequireCasbinPolicy 本批四条写路由：裸注册 403、超管放行、补策略即放行。
//
// 与 TestMailSplitPageGETHasNoCasbinButPOSTDoes 同一套反证手法（先钉「策略不存在」这个前提，
// 再补策略看同一请求是否立刻不再被拦），差别只在覆盖面：这里逐条走完四条新路由。
func TestMailSplitContactCrudRoutesRequireCasbinPolicy(t *testing.T) {
	env := newMailSplitRoutesEnv(t)
	subject := strconv.FormatInt(env.PlainID, 10)

	for _, tc := range mailSplitContactCrudRoutes {
		t.Run(tc.name, func(t *testing.T) {
			// 自带清理：删除与批量删除共用同一个 api_path + 权限码，上一条子用例补进去的策略
			// 会让下一条的「403 反例」前提不成立。
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
				t.Fatalf("反例前提不成立：普通管理员（id=%s）已有 %d 条 %s 策略",
					subject, policyCount, tc.api)
			}

			forbidden := mailSplitRoutesPOST(t, env, env.PlainAdmin, tc.path, tc.form)
			if forbidden.Code != http.StatusForbidden {
				t.Fatalf("无策略的普通管理员 POST %s 应 403: got=%d body=%s",
					tc.path, forbidden.Code, mailSplitRoutesHead(forbidden.Body.String(), 300))
			}

			// 超管：权限点由迁移 525 落库，策略由 seed 通道（051_superadmin_all_policies）
			// 动态补全 —— 所以超管这里必须是 302（不是 403）。
			asAdmin := mailSplitRoutesPOST(t, env, env.SuperAdmin, tc.path, tc.form)
			if asAdmin.Code != http.StatusFound {
				t.Fatalf("超管 POST %s 应 302: got=%d body=%s",
					tc.path, asAdmin.Code, mailSplitRoutesHead(asAdmin.Body.String(), 300))
			}

			// 因果反证：补上策略后同一请求立刻放行。
			if err := env.DB.Exec(`INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
				VALUES ('p', ?, ?, 'POST', ?)
				ON CONFLICT (ptype, v0, v1, v2, v3, v4, v5) DO NOTHING`,
				subject, tc.api, tc.code).Error; err != nil {
				t.Fatalf("补写 %s 策略失败: %v", tc.api, err)
			}
			if err := pkgcasbin.ReloadPolicy(); err != nil {
				t.Fatalf("重载 Casbin 策略失败: %v", err)
			}
			allowed := mailSplitRoutesPOST(t, env, env.PlainAdmin, tc.path, tc.form)
			if allowed.Code == http.StatusForbidden {
				t.Fatalf("补上策略后 %s 不应再 403（说明 403 由该条 Casbin 路径决定）: body=%s",
					tc.path, mailSplitRoutesHead(allowed.Body.String(), 300))
			}
			if allowed.Code != http.StatusFound {
				t.Fatalf("补上策略后 %s 应 302 回跳: got=%d body=%s",
					tc.path, allowed.Code, mailSplitRoutesHead(allowed.Body.String(), 300))
			}
		})
	}
}
