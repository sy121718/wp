// 导航译文保存的用户契约最终落在这条链上：Session（会话 Cookie）→ CSRF（X-CSRF-Token）
// → Casbin（navigation:update，迁移 036 落的权限点）→ handler → PG。
//
// 本文件补的正是链路层：模块内用例是**不带中间件**的（直接 POST 到 gin 路由），它们能证明
// 形状与取词，却证明不了「无策略会被拦住」「没带 CSRF 发不出去」「旧页面 POST 真的不在了」。
// 这里刻意与 internal/routers/assembly.go 同构地装配 /api 的三层链，装置与生产不同构
// 时断言的是另一个系统。
package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"go_wp/internal/middleware/adminctx"
	builtin "go_wp/internal/middleware/builtin"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	captcharouter "go_wp/internal/module/common/captcha/router"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationhttp "go_wp/internal/module/navigation/inbound/http"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/internal/permission"
	"go_wp/internal/templates"
	"go_wp/pkg/captcha"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const navTranslationAuthzPlainAdmin = "nav-tr-authz-plain"

// navTranslationWriteAPIPath 写端点：四种写动作收敛到这一条真实路由上（页面借用对象）
// —— 因此权限点与路径一一对应，不出现「一个码两条 path」的同步漂移。
const navTranslationWriteAPIPath = "/api/navigation/update"

// navTranslationWritePermission 迁移 036 落的权限点（sys_permission.api_path/api_method 与
// 上一行逐字一致，判权只看 (obj, act)，code 列仅用于文案与菜单树）。
const navTranslationWritePermission = "navigation:update"

type navTranslationAuthzEnv struct {
	Engine    *gin.Engine
	DB        *gorm.DB
	Super     *support.AdminSession
	Plain     *support.AdminSession
	PlainID   int64
	ProjectID string
	NavSvc    navigationcontract.NavigationService
}

// newNavTranslationAuthzEnv 装配与生产同构的真实链路，并落一条可翻译的导航项。
func newNavTranslationAuthzEnv(t *testing.T) *navTranslationAuthzEnv {
	t.Helper()

	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Fatal("NewMigratedPGTestDB 返回 nil：没有拿到真实 PostgreSQL 库")
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

	plainID := navTranslationAuthzSeedPlainAdmin(t, db)

	// 普通管理员先建、seed 后跑的顺序不能颠倒：seeds 只按 is_admin=1 授权，
	// 颠倒会让普通管理员也吃到超管策略，反例的前提就没了。
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "导航译文保存鉴权测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}

	var navSvc navigationcontract.NavigationService
	var pages pagecontract.PageService
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		GinMode:         gin.TestMode,
		UseDefaultRoute: false,
		// 组件由本用例显式装配（DB 是隔离 schema）；InitComponents 会去连生产库，刻意不接。
		InitComponents: false,
		RouteRegistrar: func(engine *gin.Engine) {
			// 相对路径以**本包目录**为基准：public/test/navigation/feature → 上升四级才是仓库根。
			// 写错层级时渲染器会加载到零个模板，页面 render 失败只会回一句归口错误页 ——
			// 那种失败看起来像「鉴权挂了」，其实是装置指错了目录。
			engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)

			api := engine.Group("/api")
			captcharouter.SetupCaptchaRoutes(api)
			adminAuthz := adminhttp.SetupAdminRoutes(permission.NewRouteGroup(api), db)
			// /admin 页面组：中间件链与 assembly.go 一致（Session + CSRF + 权限上下文）。
			adminPages := engine.Group("/admin",
				builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(),
				adminctx.PermContextMiddleware(adminAuthz))

			// /api 上显式包一层三层链，与 assembly.go 的 authorizedAPI 逐字同构：
			// navigation 的 API 路由自身不带中间件，判权全靠组级链。少了这一层，
			// 「无策略 403」就永远测不出来（拿到的会是 handler 自己的 4xx/200）。
			authorizedAPI := permission.NewRouteGroup(
				api.Group("", builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(), builtin.CasbinMiddleware()))

			navSvc = navigationhttp.SetupNavigationRoutes(authorizedAPI, db, projects)

			artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
			routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
			blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
			pages = pageservice.NewService(pagemodel.NewPageModel(db), artifacts, routes, projects, blocks,
				nil, nil, navSvc, nil)
			navigationhttp.SetupNavigationPages(adminPages, navSvc, projects, pages, blocks)

			// 译文写入端口在装配层是用**默认库**句柄接的（i18n.NewContentWriterDefault），
			// 测试库是隔离 schema ⇒ 那里拿不到句柄，写分支会 fail closed（500）。
			// 显式换成测试库句柄：本用例验收的是鉴权链，不是「默认库有没有配好」。
			translations := navigationhttp.NewNavigationTranslationHandle(navSvc, projects)
			translations.SetContentWriter(i18n.NewContentWriter(db))
			navigationhttp.SetNavigationTranslationWriter(translations)
		},
	})
	if err != nil {
		t.Fatalf("初始化测试引擎失败: %v", err)
	}
	t.Cleanup(func() {
		navigationhttp.SetNavigationTranslationWriter(nil)
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

	env := &navTranslationAuthzEnv{
		Engine:    engine,
		DB:        db,
		Super:     navTranslationAuthzLogin(t, engine, support.TestAdminUsername, "超管"),
		Plain:     navTranslationAuthzLogin(t, engine, navTranslationAuthzPlainAdmin, "普通管理员"),
		PlainID:   plainID,
		ProjectID: project.ID,
		NavSvc:    navSvc,
	}
	// 一条可翻译的导航项：契约里 rows[].hash 由「菜单标签原文」算出，没有它就没有可写的行。
	addNavItem(t, navSvc, env.ProjectID, "首页", "/", nil, "self")
	return env
}

// navTranslationAuthzSeedPlainAdmin 落一个**无任何角色**的普通管理员（is_admin=0）。
func navTranslationAuthzSeedPlainAdmin(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(support.TestAdminPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	// id 交给序列：固定 id 与既有 seed 撞上时的冲突是静默跳过的（SeedTestAdmin 已 setval）。
	if err := db.Exec("DELETE FROM sys_admin WHERE username = ?", navTranslationAuthzPlainAdmin).Error; err != nil {
		t.Fatalf("清理普通管理员测试账号失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO sys_admin (username, password, status, is_admin)
		VALUES (?, ?, 1, 0)`, navTranslationAuthzPlainAdmin, string(hash)).Error; err != nil {
		t.Fatalf("写入普通管理员测试账号失败: %v", err)
	}
	var row struct {
		ID      int64
		IsAdmin int
	}
	if err := db.Raw("SELECT id, is_admin FROM sys_admin WHERE username = ?", navTranslationAuthzPlainAdmin).
		Scan(&row).Error; err != nil {
		t.Fatalf("读取普通管理员测试账号失败: %v", err)
	}
	if row.ID == 0 || row.IsAdmin != 0 {
		t.Fatalf("普通管理员测试账号前置不成立: id=%d is_admin=%d", row.ID, row.IsAdmin)
	}
	return row.ID
}

func navTranslationAuthzLogin(t *testing.T, engine *gin.Engine, username, label string) *support.AdminSession {
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

// navTranslationAuthzPost 发 JSON 写请求；withCSRF=false 时**不发** X-CSRF-Token
// （模拟「前端忘了带令牌」：会话有效、令牌缺失）。
func navTranslationAuthzPost(t *testing.T, env *navTranslationAuthzEnv, sess *support.AdminSession,
	path string, payload map[string]interface{}, withCSRF bool) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("序列化 %s 的请求体失败: %v", path, err)
	}
	headers := map[string]string{
		"Cookie":       sess.Cookie,
		"Content-Type": "application/json",
	}
	if withCSRF {
		headers["X-CSRF-Token"] = sess.CSRFToken
	}
	recorder, err := support.SendRequest(env.Engine, support.RequestOptions{
		Method: http.MethodPost, Path: path, Headers: headers, RawBody: raw,
	})
	if err != nil {
		t.Fatalf("发送 POST %s 失败: %v", path, err)
	}
	return recorder
}

// navTranslationAuthzPayload 与模板整表提交同形的载荷（action=translations 档）。
func navTranslationAuthzPayload(projectID, lang, sourceText, target string) map[string]interface{} {
	return map[string]interface{}{
		"action":  "translations",
		"project": projectID,
		"lang":    lang,
		"rows": []map[string]interface{}{{
			"context": i18n.ContentContext("navigation", "label"),
			"hash":    i18n.ContentHash(sourceText),
			"target":  target,
		}},
	}
}

func navTranslationAuthzClearTranslation(t *testing.T, env *navTranslationAuthzEnv, sourceText, lang string) {
	t.Helper()
	if err := env.DB.Exec("DELETE FROM sys_translation WHERE source_hash = ? AND lang = ?",
		i18n.ContentHash(sourceText), lang).Error; err != nil {
		t.Fatalf("清理译文行失败: %v", err)
	}
}

func navTranslationAuthzTarget(t *testing.T, env *navTranslationAuthzEnv, sourceText, lang string) string {
	t.Helper()
	var target string
	if err := env.DB.Raw("SELECT target_text FROM sys_translation WHERE source_hash = ? AND lang = ?",
		i18n.ContentHash(sourceText), lang).Scan(&target).Error; err != nil {
		t.Fatalf("查询译文失败: %v", err)
	}
	return target
}

func navTranslationAuthzPolicyCount(t *testing.T, env *navTranslationAuthzEnv) int64 {
	t.Helper()
	var count int64
	if err := env.DB.Raw(`SELECT count(*) FROM sys_casbin_rule
		WHERE ptype = 'p' AND v0 = ? AND v1 = ? AND v3 = ?`,
		strconv.FormatInt(env.PlainID, 10), navTranslationWriteAPIPath, navTranslationWritePermission).
		Scan(&count).Error; err != nil {
		t.Fatalf("查询普通管理员策略失败: %v", err)
	}
	return count
}

func navTranslationAuthzGrantPolicy(t *testing.T, env *navTranslationAuthzEnv) {
	t.Helper()
	if err := env.DB.Exec(`INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, ?, 'POST', ?)
		ON CONFLICT (ptype, v0, v1, v2, v3, v4, v5) DO NOTHING`,
		strconv.FormatInt(env.PlainID, 10), navTranslationWriteAPIPath, navTranslationWritePermission).Error; err != nil {
		t.Fatalf("补写策略失败: %v", err)
	}
	if err := pkgcasbin.ReloadPolicy(); err != nil {
		t.Fatalf("重载 Casbin 策略失败: %v", err)
	}
}

func navTranslationAuthzRevokePolicy(t *testing.T, env *navTranslationAuthzEnv) {
	t.Helper()
	if err := env.DB.Exec(`DELETE FROM sys_casbin_rule
		WHERE ptype = 'p' AND v0 = ? AND v1 = ? AND v3 = ?`,
		strconv.FormatInt(env.PlainID, 10), navTranslationWriteAPIPath, navTranslationWritePermission).Error; err != nil {
		t.Fatalf("清理历史策略失败: %v", err)
	}
	if err := pkgcasbin.ReloadPolicy(); err != nil {
		t.Fatalf("重载 Casbin 策略失败: %v", err)
	}
}

func navTranslationAuthzWritten(t *testing.T, recorder *httptest.ResponseRecorder) int {
	t.Helper()
	body := map[string]interface{}{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("回执不是 JSON: %s", recorder.Body.String())
	}
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("回执缺少 data 段: %s", recorder.Body.String())
	}
	written, ok := data["written"].(float64)
	if !ok {
		t.Fatalf("回执缺少 written 计数: %s", recorder.Body.String())
	}
	return int(written)
}

// TestNavigationTranslationWriteAuthzChain 无策略 403、超管放行、补策略即放行 ——
// 「403 由这条 Casbin 路径决定」用因果反证钉住（与 mail 的同一套手法）。
func TestNavigationTranslationWriteAuthzChain(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)

	// 前提：普通管理员没有任何 navigation:update 策略（否则下面的 403 反例不成立）。
	navTranslationAuthzRevokePolicy(t, env)
	if got := navTranslationAuthzPolicyCount(t, env); got != 0 {
		t.Fatalf("反例前提不成立：普通管理员仍有 %d 条 %s 策略", got, navTranslationWritePermission)
	}
	navTranslationAuthzClearTranslation(t, env, "首页", "en-US")

	// 1) 反例：无策略 —— 被 Casbin 拦在 handler 之前（不是形状错、不是 500）。
	denied := navTranslationAuthzPost(t, env, env.Plain, navTranslationWriteAPIPath,
		navTranslationAuthzPayload(env.ProjectID, "en-US", "首页", "Home"), true)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("无策略的普通管理员应 403，实际 %d: %s", denied.Code, denied.Body.String())
	}
	// 被拦下的回执里不能出现成功态字段：403 只是「没进去」的旁证，回执形状才是「没往下走」。
	if strings.Contains(denied.Body.String(), "written") {
		t.Errorf("403 的回执不该带写入计数: %s", denied.Body.String())
	}
	// 被拦下的请求不能留下任何痕迹：403 只是「没进去」的旁证，落库才是效果。
	if got := navTranslationAuthzTarget(t, env, "首页", "en-US"); got != "" {
		t.Fatalf("403 的请求不该写库，但译文已落成 %q", got)
	}

	// 2) 超管对照：策略由 seed 通道按 is_admin=1 补全 —— 必须是 200 且真落库。
	asAdmin := navTranslationAuthzPost(t, env, env.Super, navTranslationWriteAPIPath,
		navTranslationAuthzPayload(env.ProjectID, "en-US", "首页", "Home"), true)
	if asAdmin.Code != http.StatusOK {
		t.Fatalf("超管保存应 200，实际 %d: %s", asAdmin.Code, asAdmin.Body.String())
	}
	if got := navTranslationAuthzWritten(t, asAdmin); got != 1 {
		t.Fatalf("回执 written 应为 1，实际 %d: %s", got, asAdmin.Body.String())
	}
	if got := navTranslationAuthzTarget(t, env, "首页", "en-US"); got != "Home" {
		t.Fatalf("译文未落库，实际 target=%q", got)
	}

	// 3) 因果反证：补上策略后普通管理员的同一请求立刻放行。
	navTranslationAuthzClearTranslation(t, env, "首页", "en-US")
	navTranslationAuthzGrantPolicy(t, env)
	allowed := navTranslationAuthzPost(t, env, env.Plain, navTranslationWriteAPIPath,
		navTranslationAuthzPayload(env.ProjectID, "en-US", "首页", "Home"), true)
	if allowed.Code != http.StatusOK {
		t.Fatalf("补上策略后普通管理员应 200（说明 403 由该条 Casbin 路径决定），实际 %d: %s",
			allowed.Code, allowed.Body.String())
	}
	if got := navTranslationAuthzTarget(t, env, "首页", "en-US"); got != "Home" {
		t.Fatalf("补策略后的写入未落库，实际 target=%q", got)
	}
}

// TestNavigationTranslationWriteRequiresCSRF 会话有效但没带令牌：CSRF 层拒绝，且不落库。
//
// 这一档在模块内测不出来（那里的路由没有中间件）：CSRF 是**装配层**的中间件，
// 少了它，「去掉模板里的 hidden csrf」这类改动不会有任何测试变红。
func TestNavigationTranslationWriteRequiresCSRF(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	navTranslationAuthzClearTranslation(t, env, "首页", "en-US")

	withoutToken := navTranslationAuthzPost(t, env, env.Super, navTranslationWriteAPIPath,
		navTranslationAuthzPayload(env.ProjectID, "en-US", "首页", "Home"), false)
	// 缺令牌的契约状态码就是 403（builtin.CSRFMiddleware：缺失或不一致 → 403「CSRF 校验失败」）；
	// 只写「不等于 200」会把 400/500 也算通过 —— 那两种恰好是「请求进了 handler」的形态。
	if withoutToken.Code != http.StatusForbidden {
		t.Fatalf("缺少 X-CSRF-Token 的写请求应 403，实际 %d: %s", withoutToken.Code, withoutToken.Body.String())
	}
	if strings.Contains(withoutToken.Body.String(), "written") {
		t.Errorf("被 CSRF 拦下的回执不该带写入计数: %s", withoutToken.Body.String())
	}
	if got := navTranslationAuthzTarget(t, env, "首页", "en-US"); got != "" {
		t.Fatalf("被 CSRF 拦下的请求不该写库，但译文已落成 %q", got)
	}

	// 同一请求补上令牌立刻成功 —— 证明上面这一档确实是「缺令牌」，不是别的错误。
	withToken := navTranslationAuthzPost(t, env, env.Super, navTranslationWriteAPIPath,
		navTranslationAuthzPayload(env.ProjectID, "en-US", "首页", "Home"), true)
	if withToken.Code != http.StatusOK {
		t.Fatalf("带令牌的同一请求应 200，实际 %d: %s", withToken.Code, withToken.Body.String())
	}
	if got := navTranslationAuthzTarget(t, env, "首页", "en-US"); got != "Home" {
		t.Fatalf("带令牌的写入未落库，实际 target=%q", got)
	}
}

// TestNavigationTranslationLegacyPagePostIsGone 旧页面写路由已下线，译文工作台页仍在。
//
// 两件事必须一起钉：只断言旧路由 404，改坏页面装配（路由整体没挂上）也不会红；
// 只断言页面 200，则旧路由复活（回到两套写入口）也不会红。
func TestNavigationTranslationLegacyPagePostIsGone(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)

	legacy := support.RequestOptions{
		Method: http.MethodPost,
		Path:   "/admin/navigations/translations/save",
		Headers: map[string]string{
			"Cookie":       env.Super.Cookie,
			"X-CSRF-Token": env.Super.CSRFToken,
			"Content-Type": "application/x-www-form-urlencoded",
		},
		RawBody: []byte("lang=en-US&project=" + env.ProjectID + "&rowContext=nav&rowHash=x&rowTarget=y"),
	}
	recorder, err := support.SendRequest(env.Engine, legacy)
	if err != nil {
		t.Fatalf("发送旧页面 POST 失败: %v", err)
	}
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("旧页面写路由 /admin/navigations/translations/save 应 404，实际 %d: %s",
			recorder.Code, recorder.Body.String())
	}

	page, err := support.SendRequest(env.Engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/admin/navigations/translations?project=" + env.ProjectID + "&lang=en-US",
		Headers: map[string]string{
			"Cookie": env.Super.Cookie,
			"Accept": "text/html,application/xhtml+xml",
		},
	})
	if err != nil {
		t.Fatalf("发送译文页 GET 失败: %v", err)
	}
	if page.Code != http.StatusOK {
		t.Fatalf("译文工作台页应 200，实际 %d: %s", page.Code, page.Body.String())
	}
	body := page.Body.String()
	// 页面上的写契约必须与真实端点一致：点一次保存就该打到 /api/navigation/update。
	if !strings.Contains(body, `data-write-url="`+navTranslationWriteAPIPath+`"`) {
		t.Errorf("译文页缺少 JSON 写声明 data-write-url=%q", navTranslationWriteAPIPath)
	}
	if strings.Contains(body, "translations/save") {
		t.Errorf("译文页仍在引用已下线的页面写路由: %s", page.Body.String())
	}
}
