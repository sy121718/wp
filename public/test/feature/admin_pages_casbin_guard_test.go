package feature

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"go_wp/internal/middleware/adminctx"
	builtin "go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	captcharouter "go_wp/internal/module/common/captcha/router"
	"go_wp/internal/templates"
	"go_wp/pkg/captcha"
	pkgcasbin "go_wp/pkg/casbin"
	mw "go_wp/pkg/middleware"
	"go_wp/public/schema"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	adminPagesPlainAdminUsername = "admin_pages_plain_admin"

	adminPagesProbeAdminUsername = "admin_pages_leak_probe"
	adminPagesProbeAdminEmail    = "admin-pages-leak-probe@example.invalid"

	adminPagesProbeRoleCode = "admin_pages_probe_role"
	adminPagesProbeRoleName = "管理页保护探针角色"
)

type adminPagesTestEnv struct {
	Engine       *gin.Engine
	DB           *gorm.DB
	SuperAdmin   *support.AdminSession
	PlainAdmin   *support.AdminSession
	PlainAdminID int64
}

func newAdminPagesTestEnv(t *testing.T) *adminPagesTestEnv {
	t.Helper()

	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Fatal("NewMigratedPGTestDB 返回 nil：没有拿到真实 PostgreSQL 库")
	}

	for _, table := range []string{"sys_admin", "sys_role", "sys_permission", "sys_casbin_rule"} {
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

	if err := schema.Apply(db); err != nil {
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

	if err := db.Exec("DELETE FROM sys_admin WHERE username = ?", adminPagesPlainAdminUsername).Error; err != nil {
		t.Fatalf("清理普通管理员测试账号失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO sys_admin (username, password, status, is_admin)
		VALUES (?, ?, 1, 0)`, adminPagesPlainAdminUsername, string(hash)).Error; err != nil {
		t.Fatalf("写入普通管理员测试账号失败: %v", err)
	}
	var plainRow struct {
		ID      int64
		IsAdmin int
	}
	if err := db.Raw("SELECT id, is_admin FROM sys_admin WHERE username = ?", adminPagesPlainAdminUsername).
		Scan(&plainRow).Error; err != nil {
		t.Fatalf("读取普通管理员测试账号失败: %v", err)
	}
	if plainRow.ID == 0 {
		t.Fatal("普通管理员测试账号不存在：反例的前提条件不成立")
	}
	if plainRow.IsAdmin != 0 {
		t.Fatalf("普通管理员测试账号的 is_admin 应为 0，got=%d", plainRow.IsAdmin)
	}

	if err := db.Exec(`INSERT INTO sys_admin (username, password, email, status, is_admin)
		VALUES (?, ?, ?, 1, 0)`,
		adminPagesProbeAdminUsername, string(hash), adminPagesProbeAdminEmail).Error; err != nil {
		t.Fatalf("写入探针管理员失败: %v", err)
	}

	if err := db.Exec(`INSERT INTO sys_role (role_code, role_name, status, is_system, sort_order, remark)
		VALUES (?, ?, 1, 0, 0, ?)`,
		adminPagesProbeRoleCode, adminPagesProbeRoleName, "鉴权回归探针").Error; err != nil {
		t.Fatalf("写入探针角色失败: %v", err)
	}

	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		GinMode:         gin.TestMode,
		UseDefaultRoute: false,

		InitComponents: false,
		RouteRegistrar: func(engine *gin.Engine) {

			engine.HTMLRender = templates.NewJetHTMLRender("../../../internal/templates", true)

			api := engine.Group("/api")
			captcharouter.SetupCaptchaRoutes(mw.NewRegistry(), api)

			adminAuthzSvc := adminhttp.SetupAdminRoutes(mw.NewRouteGroup(api, mw.RouteProtected), db)
			adminCRUD, ok := adminAuthzSvc.(interface {
				admincontract.AdminService
				admincontract.RoleService
				admincontract.PermService
				admincontract.MenuService
				admincontract.DeptService
				admincontract.RuleService
			})
			if !ok {
				t.Fatalf("admin 模块未实现六领域合并契约（装配缺陷）：%T", adminAuthzSvc)
			}

			adminPages := engine.Group("/admin",
				builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(),
				adminctx.PermContextMiddleware(adminAuthzSvc))

			adminhttp.SetupAdminPages(mw.NewRegistry(), adminPages, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, nil, nil)
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

	return &adminPagesTestEnv{
		Engine:       engine,
		DB:           db,
		SuperAdmin:   adminPagesRequireLogin(t, engine, support.TestAdminUsername, "超管"),
		PlainAdmin:   adminPagesRequireLogin(t, engine, adminPagesPlainAdminUsername, "普通管理员"),
		PlainAdminID: plainRow.ID,
	}
}

func adminPagesRequireLogin(t *testing.T, engine *gin.Engine, username, label string) *support.AdminSession {
	t.Helper()

	sess, err := support.LoginAdminSession(t, engine, username, support.TestAdminPassword)
	if err != nil {
		t.Fatalf("%s（%s）登录失败: %v", label, username, err)
	}
	if sess.Cookie == "" || sess.CSRFToken == "" {
		t.Fatalf("%s（%s）登录未拿到完整会话：cookie=%q csrf_token=%q",
			label, username, sess.Cookie, sess.CSRFToken)
	}
	return sess
}

func TestAdminPagesSuperAdminCanOpenRolesPage(t *testing.T) {
	env := newAdminPagesTestEnv(t)

	recorder, err := support.SendRequest(env.Engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/admin/roles",
		Headers: map[string]string{
			"Cookie": env.SuperAdmin.Cookie,
			"Accept": "text/html,application/xhtml+xml",
		},
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	body := recorder.Body.String()

	if recorder.Code != http.StatusOK {
		t.Fatalf("超管访问 /admin/roles 应 200: got=%d body=%s", recorder.Code, body)
	}
	if !strings.Contains(body, "</html>") {
		t.Fatalf("页面未完整渲染（缺 </html>，模板中途中断）: %s", body)
	}
	if !strings.Contains(body, adminPagesProbeRoleName) {
		t.Fatalf("角色列表未渲染出库里的角色名 %q：页面没拿到 RoleList 数据（body=%s）",
			adminPagesProbeRoleName, body)
	}
	if !strings.Contains(body, adminPagesProbeRoleCode) {
		t.Fatalf("角色列表未渲染出角色编码 %q（body=%s）", adminPagesProbeRoleCode, body)
	}
	if strings.Contains(body, "无权限访问") {
		t.Fatalf("超管被 Casbin 拒绝：/api/role/list 的超管策略缺失（seed 031/050 未生效？）: %s", body)
	}
}

func TestAdminPagesReadOnlyPageForbiddenWithoutPolicy(t *testing.T) {
	env := newAdminPagesTestEnv(t)

	subject := strconv.FormatInt(env.PlainAdminID, 10)
	var policyCount int64
	if err := env.DB.Raw(`SELECT count(*) FROM sys_casbin_rule
		WHERE ptype = 'p' AND v0 = ? AND v1 = '/api/admin/list'`, subject).
		Scan(&policyCount).Error; err != nil {
		t.Fatalf("查询普通管理员的 /api/admin/list 策略失败: %v", err)
	}
	if policyCount != 0 {
		t.Fatalf("反例前提不成立：普通管理员（id=%s）已有 %d 条 /api/admin/list 策略", subject, policyCount)
	}

	profileRecorder, err := support.SendRequest(env.Engine, support.RequestOptions{
		Method:  http.MethodGet,
		Path:    "/api/admin/profile",
		Headers: map[string]string{"Cookie": env.PlainAdmin.Cookie},
	})
	if err != nil {
		t.Fatalf("发送 /api/admin/profile 请求失败: %v", err)
	}
	if profileRecorder.Code != http.StatusOK {
		t.Fatalf("普通管理员会话应有效（/api/admin/profile 期望 200）: got=%d body=%s",
			profileRecorder.Code, profileRecorder.Body.String())
	}

	recorder, err := support.SendRequest(env.Engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/admin/administrators",
		Headers: map[string]string{
			"Cookie": env.PlainAdmin.Cookie,
			"Accept": "text/html,application/xhtml+xml",
		},
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	body := recorder.Body.String()

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("无策略的普通管理员访问 /admin/administrators 应 403 拒绝: got=%d want=%d body=%s",
			recorder.Code, http.StatusForbidden, body)
	}
	t.Logf("无策略时 /admin/administrators 的 403 响应体: %s", body)

	forbidden, perr := support.ParseStandardResponse(recorder)
	if perr != nil {
		t.Fatalf("403 应是 JSON 标准响应，解析失败: %v body=%s", perr, body)
	}
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("403 响应的信封 code 应为 403，实际 %d body=%s", forbidden.Code, body)
	}
	if strings.Contains(body, "</html>") {
		t.Fatalf("403 不应渲染整页 HTML（拒绝出口是 JSON）: %s", body)
	}
	for _, leaked := range []string{
		adminPagesProbeAdminUsername,
		adminPagesProbeAdminEmail,
		support.TestAdminUsername,
		`<table class="data-table">`,
	} {
		if strings.Contains(body, leaked) {
			t.Fatalf("403 响应里出现了 %q —— 管理员列表页被渲染了（用户名/邮箱泄漏）: %s", leaked, body)
		}
	}

	if err := env.DB.Exec(`INSERT INTO sys_casbin_rule (ptype, v0, v1, v2, v3)
		VALUES ('p', ?, '/api/admin/list', 'GET', 'admin:list')
		ON CONFLICT (ptype, v0, v1, v2, v3, v4, v5) DO NOTHING`, subject).Error; err != nil {
		t.Fatalf("补写 /api/admin/list 策略失败: %v", err)
	}
	if err := pkgcasbin.ReloadPolicy(); err != nil {
		t.Fatalf("重载 Casbin 策略失败: %v", err)
	}

	allowed, err := support.SendRequest(env.Engine, support.RequestOptions{
		Method: http.MethodGet,
		Path:   "/admin/administrators",
		Headers: map[string]string{
			"Cookie": env.PlainAdmin.Cookie,
			"Accept": "text/html,application/xhtml+xml",
		},
	})
	if err != nil {
		t.Fatalf("发送请求失败: %v", err)
	}
	allowedBody := allowed.Body.String()
	if allowed.Code != http.StatusOK {
		t.Fatalf("补上 /api/admin/list 策略后应放行（说明 403 由该策略决定）: got=%d body=%s",
			allowed.Code, allowedBody)
	}
	for _, visible := range []string{adminPagesProbeAdminUsername, adminPagesProbeAdminEmail} {
		if !strings.Contains(allowedBody, visible) {
			t.Fatalf("放行后管理员列表页应渲染出 %q（否则上面的 403「没泄漏」是无意义的）: body=%s",
				visible, allowedBody)
		}
	}
}
