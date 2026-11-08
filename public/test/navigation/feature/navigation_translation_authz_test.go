package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	text "go_wp/pkg/view/jet"
	"net/http"
	"net/http/httptest"
	"regexp"
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
	navigationservice "go_wp/internal/module/navigation/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/internal/templates"
	"go_wp/pkg/captcha"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/i18n"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const navTranslationAuthzPlainAdmin = "nav-tr-authz-plain"

const navTranslationWriteAPIPath = "/api/navigation/translations"

const navTranslationWritePermission = "navigation:translate"

type navTranslationAuthzEnv struct {
	Engine    *gin.Engine
	DB        *gorm.DB
	Super     *support.AdminSession
	Plain     *support.AdminSession
	PlainID   int64
	ProjectID string
	NavSvc    navigationcontract.NavigationService

	Ports navWritePortFixture
}

func newNavTranslationAuthzEnv(t *testing.T) *navTranslationAuthzEnv {
	t.Helper()

	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Fatal("NewMigratedPGTestDB 返回 nil：没有拿到真实 PostgreSQL 库")
	}

	if err := support.SeedTestAdmin(t, db, support.TestAdminUsername, support.TestAdminPassword); err != nil {
		t.Fatalf("准备测试超管失败: %v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行业务权限 seed 失败: %v", err)
	}
	// RunSeeds 只写库、不刷新 i18n 进程缓存；不注入的话取词一律落兜底。
	injectNavigationFeatureWords(t, db)
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

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "导航译文保存鉴权测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}

	var navSvc navigationcontract.NavigationService

	var navImpl *navigationservice.Service

	var ports navWritePortFixture
	var navHandle *navigationhttp.Handle
	var pages pagecontract.PageService
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		GinMode:         gin.TestMode,
		UseDefaultRoute: false,

		InitComponents: false,
		RouteRegistrar: func(engine *gin.Engine) {

			engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
			// 生产装配在基础层挂请求取词与成功出口；缺了它们成功/失败片段都会渲染失败。
			engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
			engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
			engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })

			api := engine.Group("/api")
			captcharouter.SetupCaptchaRoutes(mw.NewRegistry(), api)
			adminAuthz := adminhttp.SetupAdminRoutes(mw.NewRouteGroup(api, mw.RouteProtected), db)

			adminPages := engine.Group("/admin",
				builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(),
				adminctx.PermContextMiddleware(adminAuthz))

			authorizedAPI := mw.NewRouteGroup(
				api.Group("", builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(), builtin.CasbinMiddleware()), mw.RouteProtected)

			registered, registeredHandle := navigationhttp.SetupNavigationRoutes(authorizedAPI, db, projects)
			navSvc, navHandle = registered, registeredHandle
			navImpl, _ = registered.(*navigationservice.Service)

			artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
			routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
			blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
			pages = pageservice.NewService(pagemodel.NewPageModel(db), artifacts, routes, projects, blocks,
				nil, nil, navSvc, nil)
			navigationhttp.SetupNavigationPages(adminPages, navSvc, projects, pages, blocks)

			wireNavigationTranslationWriter(t, db, navHandle, navImpl, projects)

			ports = attachNavWritePorts(t, navImpl, db, projects)
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

	env := &navTranslationAuthzEnv{
		Engine:    engine,
		DB:        db,
		Super:     navTranslationAuthzLogin(t, engine, support.TestAdminUsername, "超管"),
		Plain:     navTranslationAuthzLogin(t, engine, navTranslationAuthzPlainAdmin, "普通管理员"),
		PlainID:   plainID,
		ProjectID: project.ID,
		NavSvc:    navSvc,
		Ports:     ports,
	}

	addNavItem(t, navSvc, env.ProjectID, "首页", "/", nil, "self")
	return env
}

func navTranslationAuthzSeedPlainAdmin(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(support.TestAdminPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}

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

// navTranslationAuthzSuccess 校验普通成功只给公共一句提示：
// 写入条数属明细，不得进入成功消息。
func navTranslationAuthzSuccess(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	body := recorder.Body.String()
	if !strings.Contains(body, `role="status"`) {
		t.Fatalf("回执不是成功片段: %s", body)
	}
	if regexp.MustCompile(`\d`).MatchString(body) {
		t.Fatalf("成功提示不得携带计数或其它数字明细: %s", body)
	}
}

func TestNavigationTranslationWriteAuthzChain(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)

	navTranslationAuthzRevokePolicy(t, env)
	if got := navTranslationAuthzPolicyCount(t, env); got != 0 {
		t.Fatalf("反例前提不成立：普通管理员仍有 %d 条 %s 策略", got, navTranslationWritePermission)
	}
	navTranslationAuthzClearTranslation(t, env, "首页", "en-US")

	denied := navTranslationAuthzPost(t, env, env.Plain, navTranslationWriteAPIPath,
		navTranslationAuthzPayload(env.ProjectID, "en-US", "首页", "Home"), true)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("无策略的普通管理员应 403，实际 %d: %s", denied.Code, denied.Body.String())
	}

	if strings.Contains(denied.Body.String(), "written") {
		t.Errorf("403 的回执不该带写入计数: %s", denied.Body.String())
	}

	if got := navTranslationAuthzTarget(t, env, "首页", "en-US"); got != "" {
		t.Fatalf("403 的请求不该写库，但译文已落成 %q", got)
	}

	asAdmin := navTranslationAuthzPost(t, env, env.Super, navTranslationWriteAPIPath,
		navTranslationAuthzPayload(env.ProjectID, "en-US", "首页", "Home"), true)
	if asAdmin.Code != http.StatusOK {
		t.Fatalf("超管保存应 200，实际 %d: %s", asAdmin.Code, asAdmin.Body.String())
	}
	navTranslationAuthzSuccess(t, asAdmin)
	if got := navTranslationAuthzTarget(t, env, "首页", "en-US"); got != "Home" {
		t.Fatalf("译文未落库，实际 target=%q", got)
	}

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

func TestNavigationTranslationWriteRequiresCSRF(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	navTranslationAuthzClearTranslation(t, env, "首页", "en-US")

	withoutToken := navTranslationAuthzPost(t, env, env.Super, navTranslationWriteAPIPath,
		navTranslationAuthzPayload(env.ProjectID, "en-US", "首页", "Home"), false)

	if withoutToken.Code != http.StatusForbidden {
		t.Fatalf("缺少 X-CSRF-Token 的写请求应 403，实际 %d: %s", withoutToken.Code, withoutToken.Body.String())
	}
	if strings.Contains(withoutToken.Body.String(), "written") {
		t.Errorf("被 CSRF 拦下的回执不该带写入计数: %s", withoutToken.Body.String())
	}
	if got := navTranslationAuthzTarget(t, env, "首页", "en-US"); got != "" {
		t.Fatalf("被 CSRF 拦下的请求不该写库，但译文已落成 %q", got)
	}

	withToken := navTranslationAuthzPost(t, env, env.Super, navTranslationWriteAPIPath,
		navTranslationAuthzPayload(env.ProjectID, "en-US", "首页", "Home"), true)
	if withToken.Code != http.StatusOK {
		t.Fatalf("带令牌的同一请求应 200，实际 %d: %s", withToken.Code, withToken.Body.String())
	}
	if got := navTranslationAuthzTarget(t, env, "首页", "en-US"); got != "Home" {
		t.Fatalf("带令牌的写入未落库，实际 target=%q", got)
	}
}

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

	if !strings.Contains(body, `hx-post="`+navTranslationWriteAPIPath+`"`) {
		t.Errorf("译文页缺少 HTMX 写声明 hx-post=%q", navTranslationWriteAPIPath)
	}
	if strings.Contains(body, "translations/save") {
		t.Errorf("译文页仍在引用已下线的页面写路由: %s", page.Body.String())
	}
}

func TestNavigationTranslationsRowsShape(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)

	post := func(t *testing.T, payload string) (int, string) {
		t.Helper()
		rec, err := support.SendRequest(env.Engine, support.RequestOptions{
			Method:  http.MethodPost,
			Path:    navTranslationWriteAPIPath,
			RawBody: []byte(payload),
			Headers: map[string]string{
				"Cookie":       env.Super.Cookie,
				"Content-Type": "application/json",
				"X-CSRF-Token": env.Super.CSRFToken,
			},
		})
		if err != nil {
			t.Fatalf("发送请求失败: %v", err)
		}
		return rec.Code, rec.Body.String()
	}

	ctx := i18n.ContentContext("navigation", "label")
	hash := i18n.ContentHash("首页")

	okPayload := `{"action":"translations","lang":"en-US","project":"` + env.ProjectID + `","rows":[` +
		`{"context":"` + ctx + `","hash":"` + hash + `","target":"Item EN"}]}`
	if code, body := post(t, okPayload); code != http.StatusOK {
		t.Fatalf("合法的 rows 数组应 200，实际 %d: %s", code, body)
	}

	objectPayload := `{"action":"translations","lang":"en-US","project":"` + env.ProjectID + `","rows":` +
		`{"context":"` + ctx + `","hash":"` + hash + `","target":"Item EN"}}`
	if code, body := post(t, objectPayload); code != http.StatusBadRequest {
		t.Errorf("rows 是对象（应为数组）时必须 400，实际 %d: %s\n"+
			"被吞成 200 会让客户端以为保存成功、实际一个字段都没提交", code, body)
	}

	missingKey := `{"action":"translations","lang":"en-US","project":"` + env.ProjectID + `","rows":[` +
		`{"context":"` + ctx + `","hash":"` + hash + `"}]}`
	if code, body := post(t, missingKey); code != http.StatusBadRequest {
		t.Errorf("数组元素缺 target 键时必须 400，实际 %d: %s\n"+
			"缺键若被当成空串，下游会按『本次不改动这一行』跳过 —— 形状错误被静默吞成 200", code, body)
	}

	extraKey := `{"action":"translations","lang":"en-US","project":"` + env.ProjectID + `","rows":[` +
		`{"context":"` + ctx + `","hash":"` + hash + `","target":"x","bogus":"y"}]}`
	if code, body := post(t, extraKey); code != http.StatusBadRequest {
		t.Errorf("数组元素含未知键时必须 400，实际 %d: %s", code, body)
	}

	nonString := `{"action":"translations","lang":"en-US","project":"` + env.ProjectID + `","rows":[` +
		`{"context":"` + ctx + `","hash":"` + hash + `","target":123}]}`
	if code, body := post(t, nonString); code != http.StatusBadRequest {
		t.Errorf("数组元素的值不是字符串时必须 400，实际 %d: %s", code, body)
	}

	emptyRows := `{"action":"translations","lang":"en-US","project":"` + env.ProjectID + `","rows":[]}`
	if code, body := post(t, emptyRows); code != http.StatusOK {
		t.Errorf("空数组应 200（本次没有要改的行是正常形态），实际 %d: %s", code, body)
	}
}

// injectNavigationFeatureWords 把生产种子写进库的词条刷新进 i18n 进程缓存。
func injectNavigationFeatureWords(t *testing.T, db *gorm.DB) {
	t.Helper()
	var rows []struct {
		ItemKey string `gorm:"column:item_key"`
		Lang    string
		Value   string `gorm:"column:value"`
	}
	if err := db.Table("sys_i18n").Select("item_key, lang, item_value AS value").
		Where("status = ?", 1).Scan(&rows).Error; err != nil {
		t.Fatalf("读取词条失败：%v", err)
	}
	words := make(map[string]map[string]string, len(rows))
	for _, r := range rows {
		if words[r.ItemKey] == nil {
			words[r.ItemKey] = map[string]string{}
		}
		words[r.ItemKey][r.Lang] = r.Value
	}
	i18n.InjectForTest(words, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
}
