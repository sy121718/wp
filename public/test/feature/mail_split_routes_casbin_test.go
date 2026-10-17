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
//   D. 仍留在页面 POST 上的写动作：结论走 shell.RenderJump（200 整页提示 + no-store + **无 Location**），
//      回跳目标 = 各自所属页面 —— 拆页后最容易回错页的地方（成功档 1 秒后经 meta refresh 自动跳，
//      失败档只给「立即前往」链接：运营要先看清原因再走）。
//      联系人的四种写动作已迁到 /api/mail/contact/{save,delete,tag}（见本文件末尾的
//      TestMailSplitContactJSONWritesRequireCasbinPolicy 与 TestMailSplitContactPageWritePostsAreRetired），
//      所以它们不再出现在这张「页面写动作回跳」表里，而是按「JSON 契约 + 真实落库」验收。
//
// 前置条件不满足一律 t.Fatal：本包先例不写 t.Skip —— 「账号没建出来 → 登录失败 → Skip」
// 会让 go test 显示 ok 而断言从未执行。

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
	"go_wp/internal/permission"
	"go_wp/internal/templates"
	"go_wp/pkg/captcha"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/html"
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
				adminctx.PermContextMiddleware(adminAuthz))
			// 邮件模块：/api/mail/* 业务接口 + /admin/mail* 页面路由一起注册。
			//
			// /api 上再显式包一层三层链（Session + CSRF + Casbin），与 assembly.go 的
			// authorizedAPI 逐字同构：mail 的 API 路由**不**自带中间件，判权全靠组级链。
			// 少了这一层，「无策略 403」就永远测不出来（拿到的会是 handler 自己的 4xx/200），
			// 而生产有 —— 测试装置必须与生产同构，否则断言的是另一个系统。
			authorizedAPI := permission.NewRouteGroup(
				api.Group("", builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(), builtin.CasbinMiddleware()))
			// 发信账号保存要加密 SMTP 密码：密钥由装配层从 config.yaml 的 app.secret 读入后注入
			//（SetupMailRoutes → config.GetViper）。测试配置这一项留空 ⇒ 不补一个临时密钥，
			// 「保存账号」这条表单链路就永远停在「未配置加密密钥」的失败档，白测一趟。
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

// mailSplitRoutesJump 提示页（shell.RenderJump）里能当验收判据的三样东西。
type mailSplitRoutesJump struct {
	// State data-jump-state 的值：ok（成功档）/ err（失败档）。
	State string
	// Back 回跳地址原文（可能带筛选 query）。
	Back string
	// BackPath 回跳地址的路径部分（比归属页时只看路径，page= 之类的 query 不算差异）。
	BackPath string
	// FromMeta 回跳地址来自 meta refresh（成功档 Seconds>0）而不是「立即前往」链接。
	FromMeta bool
}

// mailSplitRoutesJumpPage 解析提示页：状态标记 + 回跳地址。
//
// 用 golang.org/x/net/html 建树，不用正则扫全文：提示页是**完整 layout**（侧栏本来就有
// 几十个 /admin/* 链接），只有 jump 卡片**内部**的那个链接才是这次动作的回跳目标 ——
// 正则命中侧栏不会红，却会让「回错了页」的断言永远通过。
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

// mailSplitRoutesHTMLAttr 取元素的属性值（不存在返回空串）。
func mailSplitRoutesHTMLAttr(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
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

// TestMailSplitPageGETRequiresCasbinPolicy 页面 GET 与写操作走**同一条** Casbin 链。
//
// 本用例的前身（TestMailSplitPageGETHasNoCasbinButPOSTDoes）钉的是旧口径「GET 页面不挂
// Casbin，只靠侧栏按权限渲染」—— 那条约定已被 docs/02-Z §4.3 判定为缺陷：菜单隐藏不是
// 访问控制，任何登录账号直输 URL 就能进。现在页面 GET 借**该页菜单绑定的读权限点**
// enforce（adminui.PageAuthz），与 /api/* 共用同一批策略，所以「无策略 → 403」「补策略 → 200」
// 与写操作完全同构。
func TestMailSplitPageGETRequiresCasbinPolicy(t *testing.T) {
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

	// 1) 无读策略的普通管理员：六个页面全部被拒，且**页面没有被渲染**（403 里不能出现
	//    列表页痕迹）—— 菜单隐藏不是访问控制，直输 URL 必须同样被拦。
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

	// 2) 超管对照：六个页面全部可打开（超管策略由 seed 通道动态补全）。
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

	// 4) 读侧同一条反证：只补「发信账号」这一条读策略 → /admin/mail 立刻 200，而别的页仍 403
	//    —— 证明拒绝由该页借用的那条权限点决定，不是一把总开关。
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

// TestMailSplitPagePostJumpsBackToOwningPage 十组页面写动作的提示页回跳目标。
//
// 结论走 shell.RenderJump（200 整页提示 + no-store + 无 Location），不是 302 + Location：
// 成功档 1 秒后经 meta refresh 自动跳，失败档只给「立即前往」链接。两档都必须回到
// **各自所属页面** —— 拆页后最容易回错页的地方。
func TestMailSplitPagePostJumpsBackToOwningPage(t *testing.T) {
	env := newMailSplitRoutesEnv(t)

	// 联系人状态变更要改一条**真实存在**的联系人：这个库里不一定有 id=1，而本用例验的是
	// 「成功档的回跳目标」，不是「失败档怎么走」。「id 不存在」的行为另有归属：
	// service 先查存在性（internal/module/mail/service/mail_contact.go:81-84 GetContact，
	// 未命中回 mailenums.ErrContactNotFound）⇒ 页面 err 档 / JSON 400，已由
	// public/test/mail/feature/mail_contact_crud_test.go 的
	// TestMailContactCrudStatusMissingIDIsNotReportedAsSuccess 钉住，别混进这条用例。
	contactID := mailSplitContactSeed(t, env,
		fmt.Sprintf("qa.route.status.%d@example.invalid", mailSplitContactNext()))

	cases := []struct {
		name string
		path string
		form url.Values
		// want 归属页路径（回跳目标）。
		want string
		// wantState 提示页档位：ok = 成功档（meta refresh 自动跳）/ err = 失败档（只给链接）。
		wantState string
	}{
		{
			name: "账号保存",
			path: "/admin/mail/account/save",
			form: url.Values{
				// 字段与 mail.html 的新建账号表单一致：handler 不读 is_default / status，
				// 带上它们只会让这条用例以为自己在测「默认 / 启用」。
				"name": {"路由验收账号"}, "purpose": {"marketing"},
				"from_name": {"QA"}, "from_email": {"qa@example.invalid"},
				"host": {"smtp.example.invalid"}, "port": {"25"},
				"username": {"qa"}, "password": {"qa-secret"},
			},
			want:      "/admin/mail",
			wantState: "ok",
		},
		{
			name: "模板保存",
			path: "/admin/mail/template/save",
			form: url.Values{
				// 字段与 mail_templates.html 的新建表单 / handler 逐字对齐（template_key / body_html）。
				"template_key": {"qa_route_tpl"}, "locale": {"zh-CN"},
				"name": {"路由验收模板"}, "subject": {"路由验收主题"},
				"body_html": {"<p>hi</p>"},
			},
			want:      "/admin/mail/templates",
			wantState: "ok",
		},
		{
			name: "联系人导入",
			path: "/admin/mail/contact/import",
			form: url.Values{
				// 导入体是文本域 content（可含 CSV 行），不是 emails；同意声明与来源是合规留痕。
				"content":          {"qa-import@example.invalid"},
				"consent_declared": {"on"},
				"consent_source":   {"qa 路由验收"},
			},
			want:      "/admin/mail/contacts",
			wantState: "ok",
		},
		{
			// 联系人的保存 / 单删 / 批删 / 批标签已迁到 /api/mail/contact/{save,delete,tag}：
			// 它们不再有页面 POST，因此不在这张回跳表里（用 TestMailSplitContactPageWritePostsAreRetired
			// 钉「旧路径已消失」，用 TestMailSplitContactJSONWritesRequireCasbinPolicy 钉新通道。
			name: "联系人状态变更",
			// 用真实落库的联系人 id（page 上就是这么来的）⇒ 成功档。
			path:      "/admin/mail/contact/status",
			form:      url.Values{"id": {strconv.FormatUint(contactID, 10)}, "status": {"subscribed"}},
			want:      "/admin/mail/contacts",
			wantState: "ok",
		},
		{
			name:      "活动保存",
			path:      "/admin/mail/campaign/save",
			form:      url.Values{"name": {"路由验收活动"}, "template_id": {"1"}, "account_id": {"1"}},
			want:      "/admin/mail/campaigns",
			wantState: "err",
		},
		{
			name:      "活动启动",
			path:      "/admin/mail/campaign/start",
			form:      url.Values{"id": {"1"}},
			want:      "/admin/mail/campaigns",
			wantState: "err",
		},
		{
			name:      "活动删除",
			path:      "/admin/mail/campaign/delete",
			form:      url.Values{"id": {"1"}},
			want:      "/admin/mail/campaigns",
			wantState: "err",
		},
		{
			name:      "流程状态变更",
			path:      "/admin/mail/automation/status",
			form:      url.Values{"id": {"1"}, "status": {"1"}},
			want:      "/admin/mail/automation",
			wantState: "err",
		},
		{
			name:      "流程删除",
			path:      "/admin/mail/automation/delete",
			form:      url.Values{"id": {"1"}},
			want:      "/admin/mail/automation",
			wantState: "err",
		},
		{
			name:      "自动化 tick",
			path:      "/admin/mail/automation/tick",
			form:      url.Values{},
			want:      "/admin/mail/automation/runs",
			wantState: "ok",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := mailSplitRoutesPOST(t, env, env.SuperAdmin, tc.path, tc.form)
			if recorder.Code != http.StatusOK {
				t.Fatalf("超管 POST %s 的提示页应回 200: got=%d body=%s",
					tc.path, recorder.Code, mailSplitRoutesHead(recorder.Body.String(), 400))
			}
			// 结论不进缓存：否则「重试」拿回的还是上一次的结论。
			if cc := recorder.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
				t.Errorf("提示页应带 no-store，得到 %q", cc)
			}
			// 无 Location：这条通道已由 302 换成响应体，留着 Location 说明有人还在往旧通道上接。
			if loc := recorder.Header().Get("Location"); loc != "" {
				t.Errorf("提示页不该带 Location（那是旧 302 通道），得到 %q", loc)
			}

			jump := mailSplitRoutesJumpPage(t, recorder)
			if jump.State != tc.wantState {
				t.Errorf("POST %s 的提示页应为 %s 档，实际 %s（正文前 400 字：%s）",
					tc.path, tc.wantState, jump.State, mailSplitRoutesHead(recorder.Body.String(), 400))
			}
			if jump.BackPath != tc.want {
				t.Errorf("POST %s 应回跳到 %q，实际 %q（拆页后回错了页）",
					tc.path, tc.want, jump.BackPath)
			}
			// 档位与回跳通道要一致：成功档自动跳（meta refresh），失败档不自动跳（只给链接）。
			if tc.wantState == "ok" && !jump.FromMeta {
				t.Errorf("成功档应经 meta refresh 自动回跳，实际回跳地址来自链接：%q", jump.Back)
			}
			if tc.wantState == "err" && jump.FromMeta {
				t.Errorf("失败档不该自动跳走（运营要先看清原因），实际 meta refresh：%q", jump.Back)
			}
		})
	}
}

// mailSplitContactJSONWrite 一条联系人 JSON 写端点的验收：端点 / 权限码 / 载荷 / 真实落库断言。
//
// 三条端点在同一条权限点的**不同 code** 上判权（/api/mail/contact/{save,delete,tag}，
// 权限点由迁移 525 落库）—— 批量与单条**共用同一条路径**：sys_permission 每个 code 只有一列
// api_path/api_method，另开一条 /api/mail/contact/bulk-delete 就必须新增权限点，
// 而新增权限点 = 已授权角色静默缩权（见 521 与 block_router.go 的成文教训）。
// 区分单条/批量的是**请求体形状**（恰好给 id 或恰好给 ids），不是路径。
type mailSplitContactJSONWrite struct {
	name string
	path string
	api  string
	code string
	// build 每次都现造一份载荷（并可能先备好它要操作的数据）：同一份载荷会被发三次
	//（403 反例 / 超管 / 补策略后），而删除是破坏性动作 —— 复用同一份数据会让第三次拿到
	//「这一条已经不存在」，于是用例红在夹具上而不是红在权限上。让 build 负责备料，三次各自成立。
	build func(t *testing.T, env *mailSplitRoutesEnv) map[string]interface{}
	// check 在「补策略后放行」的那一次之后跑：断言效果**真的落到库里**。
	// 只看状态码会把「回 200 但没写库」当成通过。
	check func(t *testing.T, env *mailSplitRoutesEnv, payload map[string]interface{}, body map[string]interface{})
	// checkDenied 在 403 那一次之后跑：断言被拦下的请求没有留下任何痕迹。
	checkDenied func(t *testing.T, env *mailSplitRoutesEnv, payload map[string]interface{})
}

// mailSplitContactWrites 四条子用例（save / delete 单条 / delete 批量 / tag 批量）。
//
// 邮箱与标签都带自增序号：同一个子用例会被跑多轮（本文件多个用例共用一张测试库），
// 撞已有邮箱会被 service 的「邮箱已存在」挡下，那是夹具问题、不是端点问题。
func mailSplitContactWrites() []mailSplitContactJSONWrite {
	// 夹具与断言之间要传的东西**不能塞进请求体**：三条端点都是严格白名单（多一个字段就 400），
	// 而「这次用的是哪个邮箱 / 哪个标签」正是端点不认识的信息。所以走闭包变量。
	var deletedEmail string
	var bulkEmails []string
	tagged := map[string]string{} // 子用例 → 本次使用的标签名
	return []mailSplitContactJSONWrite{
		{
			name: "联系人保存（新建走同一端点）",
			path: "/api/mail/contact/save", api: "/api/mail/contact/save", code: "mail:contact_save",
			build: func(t *testing.T, _ *mailSplitRoutesEnv) map[string]interface{} {
				return map[string]interface{}{
					"email":  fmt.Sprintf("qa.json.save.%d@example.invalid", mailSplitContactNext()),
					"name":   "Casbin JSON 验收",
					"status": string(mailmodel.ContactStatusPending),
				}
			},
			check: func(t *testing.T, env *mailSplitRoutesEnv, payload map[string]interface{}, _ map[string]interface{}) {
				email, _ := payload["email"].(string)
				if !mailSplitContactExists(t, env, email) {
					t.Fatalf("放行后联系人 %s 应已落库", email)
				}
			},
			checkDenied: func(t *testing.T, env *mailSplitRoutesEnv, payload map[string]interface{}) {
				email, _ := payload["email"].(string)
				if mailSplitContactExists(t, env, email) {
					t.Fatalf("403 的请求不该写库，但 %s 已经存在了", email)
				}
			},
		},
		{
			name: "联系人删除（单条）",
			path: "/api/mail/contact/delete", api: "/api/mail/contact/delete", code: "mail:contact_delete",
			build: func(t *testing.T, env *mailSplitRoutesEnv) map[string]interface{} {
				deletedEmail = fmt.Sprintf("qa.json.del.%d@example.invalid", mailSplitContactNext())
				return map[string]interface{}{"id": mailSplitContactSeed(t, env, deletedEmail)}
			},
			check: func(t *testing.T, env *mailSplitRoutesEnv, _ map[string]interface{}, _ map[string]interface{}) {
				if mailSplitContactExists(t, env, deletedEmail) {
					t.Fatalf("放行后 %s 应已删除", deletedEmail)
				}
			},
			checkDenied: func(t *testing.T, env *mailSplitRoutesEnv, _ map[string]interface{}) {
				if !mailSplitContactExists(t, env, deletedEmail) {
					t.Fatalf("403 的删除不该动数据，但 %s 已经不在了", deletedEmail)
				}
			},
		},
		{
			name: "联系人删除（批量，与单条同路径同权限点）",
			path: "/api/mail/contact/delete", api: "/api/mail/contact/delete", code: "mail:contact_delete",
			build: func(t *testing.T, env *mailSplitRoutesEnv) map[string]interface{} {
				bulkEmails = make([]string, 0, 2)
				ids := make([]string, 0, 2)
				for i := 0; i < 2; i++ {
					email := fmt.Sprintf("qa.json.bulk.%d@example.invalid", mailSplitContactNext())
					bulkEmails = append(bulkEmails, email)
					ids = append(ids, strconv.FormatUint(mailSplitContactSeed(t, env, email), 10))
				}
				return map[string]interface{}{"ids": ids}
			},
			check: func(t *testing.T, env *mailSplitRoutesEnv, _ map[string]interface{}, body map[string]interface{}) {
				data, ok := body["data"].(map[string]interface{})
				if !ok {
					t.Fatalf("批量回执缺少 data：%#v", body)
				}
				if deleted, _ := data["deleted"].(float64); int(deleted) != len(bulkEmails) {
					t.Fatalf("应删掉 %d 条，实际 %#v", len(bulkEmails), data)
				}
				for _, email := range bulkEmails {
					if mailSplitContactExists(t, env, email) {
						t.Fatalf("放行后 %s 应已删除", email)
					}
				}
			},
			checkDenied: func(t *testing.T, env *mailSplitRoutesEnv, _ map[string]interface{}) {
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
			build: func(t *testing.T, env *mailSplitRoutesEnv) map[string]interface{} {
				seq := mailSplitContactNext()
				email := fmt.Sprintf("qa.json.tag.%d@example.invalid", seq)
				tag := fmt.Sprintf("qa-json-%d", seq)
				tagged["tag"] = tag
				// add / remove 是**字符串**（页面上的文本框，`,` 或 `;` 分隔），不是数组：
				// 端点的白名单按页面表单的字段形状定，发数组会被判成参数非法。
				return map[string]interface{}{
					"ids": []string{strconv.FormatUint(mailSplitContactSeed(t, env, email), 10)},
					"add": tag,
				}
			},
			check: func(t *testing.T, env *mailSplitRoutesEnv, payload map[string]interface{}, body map[string]interface{}) {
				data, ok := body["data"].(map[string]interface{})
				if !ok {
					t.Fatalf("打标签回执缺少 data：%#v", body)
				}
				// 键名必须是 updated：deleted 会被前端当成「这一行可以移除了」。
				if _, isDelete := data["deleted"]; isDelete {
					t.Fatalf("打标签是非破坏动作，回执不该有 deleted 键：%#v", data)
				}
				if updated, _ := data["updated"].(float64); updated != 1 {
					t.Fatalf("应报告 1 条已更新，实际 %#v", data)
				}
				ids, _ := payload["ids"].([]string)
				if !mailSplitContactTagged(t, env, ids[0], tagged["tag"]) {
					t.Fatalf("放行后联系人 %s 应带上标签 %q", ids[0], tagged["tag"])
				}
			},
			checkDenied: func(t *testing.T, env *mailSplitRoutesEnv, payload map[string]interface{}) {
				ids, _ := payload["ids"].([]string)
				if mailSplitContactTagged(t, env, ids[0], tagged["tag"]) {
					t.Fatalf("403 的打标签不该动数据，但 %s 已经带上了 %q", ids[0], tagged["tag"])
				}
			},
		},
	}
}

// TestMailSplitContactJSONWritesRequireCasbinPolicy 联系人三条 JSON 写端点：无策略 403、超管放行、补策略即放行且真的落库。
//
// 与 TestMailSplitPageGETHasNoCasbinButPOSTDoes 同一套反证手法（先钉「策略不存在」这个前提，
// 再用「补策略 → 同一请求立刻不再是 403」证明 403 确实由该条 Casbin 路径决定），
// 区别是这里打的是 /api 上的真实端点，走的是生产链路：
// Session（会话 Cookie）→ CSRF（X-CSRF-Token）→ Casbin（迁移 525 的权限点）→ handler → PG。
func TestMailSplitContactJSONWritesRequireCasbinPolicy(t *testing.T) {
	env := newMailSplitRoutesEnv(t)
	subject := strconv.FormatInt(env.PlainID, 10)

	for _, tc := range mailSplitContactWrites() {
		t.Run(tc.name, func(t *testing.T) {
			// 自带清理：删除单条与批量删除共用同一个 api_path + 权限码，上一条子用例补进去的策略
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
				t.Fatalf("反例前提不成立：普通管理员（id=%s）已有 %d 条 %s 策略", subject, policyCount, tc.api)
			}

			// 1) 反例：无策略的普通管理员 —— 被 Casbin 拦在 handler 之前（不是 4xx 形状错）。
			// 载荷在这一刻造好并留着，403 之后就用它断言「没留下任何痕迹」。
			deniedPayload := tc.build(t, env)
			denied := mailSplitRoutesJSONPost(t, env, env.PlainAdmin, tc.path, deniedPayload)
			if denied.Code != http.StatusForbidden {
				t.Fatalf("无策略的普通管理员 POST %s 应 403: got=%d body=%s",
					tc.path, denied.Code, mailSplitRoutesHead(denied.Body.String(), 300))
			}
			if tc.checkDenied != nil {
				tc.checkDenied(t, env, deniedPayload)
			}

			// 2) 超管对照：权限点由迁移 525 落库、超管策略由 seed 通道动态补全 —— 必须是 200。
			asAdmin := mailSplitRoutesJSONPost(t, env, env.SuperAdmin, tc.path, tc.build(t, env))
			if asAdmin.Code != http.StatusOK {
				t.Fatalf("超管 POST %s 应 200: got=%d body=%s",
					tc.path, asAdmin.Code, mailSplitRoutesHead(asAdmin.Body.String(), 300))
			}

			// 3) 因果反证 + 落库：补上策略后普通管理员的同一请求立刻放行，且效果真的写进了库。
			if err := env.DB.Exec(`INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
				VALUES ('p', ?, ?, 'POST', ?)
				ON CONFLICT (ptype, v0, v1, v2, v3, v4, v5) DO NOTHING`,
				subject, tc.api, tc.code).Error; err != nil {
				t.Fatalf("补写 %s 策略失败: %v", tc.api, err)
			}
			if err := pkgcasbin.ReloadPolicy(); err != nil {
				t.Fatalf("重载 Casbin 策略失败: %v", err)
			}
			payload := tc.build(t, env)
			allowed := mailSplitRoutesJSONPost(t, env, env.PlainAdmin, tc.path, payload)
			if allowed.Code != http.StatusOK {
				t.Fatalf("补上策略后 %s 应 200（说明 403 由该条 Casbin 路径决定）: got=%d body=%s",
					tc.path, allowed.Code, mailSplitRoutesHead(allowed.Body.String(), 300))
			}
			tc.check(t, env, payload, mailSplitRoutesJSONBody(t, allowed))
		})
	}
}

// TestMailSplitContactPageWritePostsAreRetired 四条联系人页面 POST 已下线：直连它们只拿到 404。
//
// 迁移后的唯一写入口是 /api/mail/contact/{save,delete,tag}。旧路径若还留着（哪怕是 403/405），
// 页面表单「还能写」的错觉就会一直在，而真正的写入路径反而没人验证 —— 所以这里钉的是
// 「路径已经不存在」，而不是「路径被拒绝」。
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

// mailSplitRoutesJSONPost 带会话与 CSRF 的 JSON POST —— 前端 json_write.js 在生产里就是这么发的。
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

// mailSplitRoutesJSONBody 解析回执体（pkg/response 的 {code,message,data}）。
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

// mailSplitContactSeed 落一条联系人并返回 id（走真实 service：本用例验收的是端点，不是建号逻辑）。
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

// mailSplitContactExists 库里是否还有这条联系人（真删了才算删）。
func mailSplitContactExists(t *testing.T, env *mailSplitRoutesEnv, email string) bool {
	t.Helper()
	var n int64
	if err := env.DB.Raw(`SELECT count(*) FROM mail_contacts WHERE email = ?`, email).Scan(&n).Error; err != nil {
		t.Fatalf("查询联系人 %s 失败: %v", email, err)
	}
	return n > 0
}

// mailSplitContactTagged 联系人是否带上了该标签（tags 是 PG 原生 text[]）。
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

// mailSplitContactSeq 让每次 build 造出互不相同的邮箱与标签（同一个子用例会被发多次）。
var mailSplitContactSeq int64

func mailSplitContactNext() int64 { return atomic.AddInt64(&mailSplitContactSeq, 1) }
