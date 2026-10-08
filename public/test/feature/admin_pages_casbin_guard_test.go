package feature

// admin_pages_casbin_guard_test.go — /admin/* 只读管理页的 Casbin 鉴权保护（回归）。
//
// 为什么落在 public/test/feature：本文件钉的是「/admin 页面组在**路由注册处**挂了哪条
// Casbin 策略」这一条中间件链行为（跨模块的路由级装配），与同包 api_auth_test.go
// （SessionAuth / CSRF / Casbin 三层链的接口级用例）是同一类，装配模式也直接沿用它的；
// admin 模块自己的 feature 包放的是页面 handler 的渲染与错误文案契约。两个位置都在
// public/test 下，按「钉的是哪一层能力」归位，而不是按 URL 前缀归位。
//
// 守的行为：internal/module/admin/inbound/http/admin_pages_router.go 里若干只读管理页的 GET
// 从「只挂 Session 认证」改成 builtin.CasbinMiddlewareForPath(对应业务 API 路径)：
//
//	/admin/administrators  → /api/admin/list       /admin/roles          → /api/role/list
//	/admin/menus           → /api/menu/tree        /admin/permissions    → /api/permission/list
//	/admin/departments     → /api/dept/tree        /admin/datarules      → /api/datarule/list
//	/admin/datarules/edit  → /api/datarule/detail
//
// 为什么重要：菜单按权限渲染，但**菜单隐藏不是访问控制** —— 直接输 URL 就能绕过。
// 收口前任何登录的后台账号打开 /admin/administrators 都能拿到全部管理员的
// 用户名 / 姓名 / 邮箱 / 手机号（handler 直接渲染 AdminList 的结果）。
// （/admin/i18n 与 /admin/lang 是刻意留白的两条，不在本文件的断言范围内。）
//
// 覆盖：
//   - 正例 TestAdminPagesSuperAdminCanOpenRolesPage：超管（is_admin=1，seed 已授全量策略）
//     GET /admin/roles → 200，且库里的角色行真的渲染进了响应体（页面正常渲染）；
//   - 反例 TestAdminPagesReadOnlyPageForbiddenWithoutPolicy：is_admin=0、且 sys_casbin_rule
//     里没有任何 /api/admin/list 策略的普通管理员 GET /admin/administrators → 403，
//     且响应体里不出现任何管理员的用户名 / 邮箱。
//
// 全程真实链路：gin 路由 → /admin 组中间件（Session + CSRF + 权限上下文）→
// CasbinMiddlewareForPath → AdminPagesHandle → 真实 service → 真实 PostgreSQL
// （support.NewMigratedPGTestDB：表结构由生产迁移建成）。正例与反例用到的角色行、
// 管理员行都是本文件插入的真实数据，没有 mock、没有 sqlite。
//
// 前置条件不满足时一律 t.Fatal：本文件**不写 t.Skip** —— 上一批的真实教训是
// 「账号没建出来 → 登录失败 → t.Skipf 溜过去」，go test 显示 ok 而断言从未执行。
// （唯一不在本文件掌控的 Skip 是 support.NewMigratedPGTestDB 自身的「本地 PostgreSQL
// 不可达」探测契约；本机 PG 可用，实测不走那条分支。）

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	builtin "go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	adminhttp "go_wp/internal/module/admin/inbound/http"
	captcharouter "go_wp/internal/module/common/captcha/router"
	"go_wp/internal/permission"
	"go_wp/internal/templates"
	"go_wp/internal/shell"
	"go_wp/pkg/captcha"
	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	// adminPagesPlainAdminUsername 反例主体：is_admin=0、没有任何 Casbin 策略的后台账号。
	adminPagesPlainAdminUsername = "admin_pages_plain_admin"
	// adminPagesProbeAdminUsername / adminPagesProbeAdminEmail 反例要证明「没泄漏」的识别性 PII：
	// 这条管理员行确实在库里，列表页一旦被渲染出来必然带上这两个值。
	adminPagesProbeAdminUsername = "admin_pages_leak_probe"
	adminPagesProbeAdminEmail    = "admin-pages-leak-probe@example.invalid"
	// adminPagesProbeRoleCode / adminPagesProbeRoleName 正例要断言的识别性角色行
	//（库与 seed 都不建角色，只有本用例插这一条）：它是「列表真的渲染了数据」而不是
	// 「只有一个空壳页面」的判据。
	adminPagesProbeRoleCode = "admin_pages_probe_role"
	adminPagesProbeRoleName = "管理页保护探针角色"
)

// adminPagesTestEnv 一个用例所需的全部真实资源。
type adminPagesTestEnv struct {
	Engine       *gin.Engine
	DB           *gorm.DB
	SuperAdmin   *support.AdminSession // is_admin=1，seed 全量策略
	PlainAdmin   *support.AdminSession // is_admin=0，无任何策略
	PlainAdminID int64
}

// newAdminPagesTestEnv 装配与 internal/routers 同构的真实链路：
// 真实 PG（生产迁移建表）+ 业务权限 seed + Casbin + Redis 会话 + /api 六领域路由 + /admin 页面组。
//
// 任何一步失败都 t.Fatal —— 前置条件不成立时绝不能靠 Skip 把用例变成「绿色」。
func newAdminPagesTestEnv(t *testing.T) *adminPagesTestEnv {
	t.Helper()

	// 1) 真实 PostgreSQL：结构与生产逐字节一致（模板库由生产迁移建成）。
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		t.Fatal("NewMigratedPGTestDB 返回 nil：没有拿到真实 PostgreSQL 库")
	}
	// 结构自检：这几张表拿不到就说明「真实 schema」这个前提不成立，当场失败。
	for _, table := range []string{"sys_admin", "sys_role", "sys_permission", "sys_casbin_rule"} {
		var regclass *string
		if err := db.Raw("SELECT to_regclass(?::text)::text", table).Scan(&regclass).Error; err != nil {
			t.Fatalf("检查表 %s 是否存在失败: %v", table, err)
		}
		if regclass == nil || *regclass == "" {
			t.Fatalf("测试库缺少表 %s：生产迁移未生效", table)
		}
	}

	// 2) 测试超管必须在业务权限 seed 之前建好：031/050 的 seed 是「对当前 is_admin=1 的行」
	//    逐行授策略，晚建就拿不到超管策略（超管会被自己的鉴权拒之门外）。
	if err := support.SeedTestAdmin(t, db, support.TestAdminUsername, support.TestAdminPassword); err != nil {
		t.Fatalf("准备测试超管失败: %v", err)
	}
	// 3) 业务权限 seed：权限点 + 菜单 + 超管全量策略（含 /api/admin/list GET、/api/role/list GET）。
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行业务权限 seed 失败: %v", err)
	}
	// 4) Casbin：把 seed 策略加载进内存（CasbinMiddleware 的 Enforce 用的就是这份）。
	if err := pkgcasbin.InitCasbin(db); err != nil {
		t.Fatalf("初始化 Casbin 失败: %v", err)
	}
	t.Cleanup(func() {
		if err := pkgcasbin.Close(); err != nil {
			t.Errorf("关闭 Casbin 失败: %v", err)
		}
		// captcha.Get() 自动初始化起的过期清理协程随用例结束关闭（本包 TestMain 有 goroutine 泄漏检测）。
		_ = captcha.Close()
	})

	hash, err := bcrypt.GenerateFromPassword([]byte(support.TestAdminPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}

	// 5) 普通管理员（is_admin=0）：id 交给序列，**绝不写固定 id** —— SeedTestAdmin 会把
	//    sys_admin 的序列 setval 到 MAX(id)，紧接着 030a 的默认超管 seed（走 nextval）拿走
	//    下一个 id；固定 id 与它撞上时冲突是静默跳过的，账号根本建不出来，而失败会伪装成
	//    「登录链路不可用」被 Skip 掉。按 username 先删后插可重复运行、与 seed 互不干扰。
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

	// 6) 反例要证明「没泄漏」的管理员行：真实存在、带可识别的用户名与邮箱。
	if err := db.Exec(`INSERT INTO sys_admin (username, password, email, status, is_admin)
		VALUES (?, ?, ?, 1, 0)`,
		adminPagesProbeAdminUsername, string(hash), adminPagesProbeAdminEmail).Error; err != nil {
		t.Fatalf("写入探针管理员失败: %v", err)
	}

	// 7) 正例要断言的探针角色：库里本来没有角色（seed 也不建角色），这一行就是列表内容。
	if err := db.Exec(`INSERT INTO sys_role (role_code, role_name, status, is_system, sort_order, remark)
		VALUES (?, ?, 1, 0, 0, ?)`,
		adminPagesProbeRoleCode, adminPagesProbeRoleName, "鉴权回归探针").Error; err != nil {
		t.Fatalf("写入探针角色失败: %v", err)
	}

	// 8) 引擎与路由：与 internal/routers/assembly.go 同款装配。
	engine, cleanup, err := support.SetupTestBootstrap(support.BootstrapOptions{
		GinMode:         gin.TestMode,
		UseDefaultRoute: false,
		// 组件由本用例显式装配：DB 是测试库、Casbin 是测试库策略；
		// config.InitComponents() 会去连 config.yaml 里的生产库，这里刻意不接。
		InitComponents: false,
		RouteRegistrar: func(engine *gin.Engine) {
			// 默认路由不装配，Jet 渲染器自行挂上（生产由 routers.SetupRoutes 装配）；
			// 路径相对本包目录（go test 的 CWD 即包目录）。
			engine.HTMLRender = templates.NewJetHTMLRender("../../../internal/templates", true)

			api := engine.Group("/api")
			captcharouter.SetupCaptchaRoutes(api)
			// admin 六领域 API（内部自带 Session/CSRF/Casbin 中间件；/api/admin/login 匿名可达）。
			adminAuthzSvc := adminhttp.SetupAdminRoutes(permission.NewRouteGroup(api), db)
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
			// /admin 页面组：中间件链与 assembly.go 逐字一致（Session + CSRF + 权限上下文）。
			adminPages := engine.Group("/admin",
				builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(),
				shell.PermContextMiddleware(adminAuthzSvc))
			// 文案词条页的「站点待重建」标记与两条用例无关，pages 传 nil（未注入即 no-op）。
			adminhttp.SetupAdminPages(adminPages, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, adminCRUD, nil, nil)
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

	// 9) 真实 Redis：本地优先、testcontainers 兜底；两级都不可用即 Fatal（不跳过）。
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

// adminPagesRequireLogin 走真实登录链路拿会话；失败一律 Fatal。
// 登录不出来就没有任何断言可谈 —— 这正是上一批 Skip 掩盖掉真实缺陷的位置。
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

// TestAdminPagesSuperAdminCanOpenRolesPage 正例：超管访问只读管理页 /admin/roles。
//
// /admin/roles 的 GET 现在挂 CasbinMiddlewareForPath("/api/role/list")；超管（is_admin=1）
// 由 seed 031/050 拿到该路径策略，因此必须放行，并按 /api/role/list 的语义渲染真实角色列表。
// 断言分三层：状态码 200 → 页面完整渲染（有 </html>）→ 库里的角色行出现在响应体里。
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

// TestAdminPagesReadOnlyPageForbiddenWithoutPolicy 反例：普通管理员（is_admin=0、无策略）
// 访问 /admin/administrators —— 收口前这里返回 200 并整页渲染全部管理员的用户名 / 邮箱。
//
// 四段断言：
//  1. 前置条件：账号确实存在、is_admin=0、sys_casbin_rule 里没有它的 /api/admin/list 策略；
//  2. 会话确实有效（/api/admin/profile 200）—— 否则 403 会被误读成「没登录」而不是「Casbin 拒绝」；
//  3. 目标请求 403，且响应体里不出现任何管理员的用户名 / 邮箱，也没有列表页的表格结构；
//  4. 因果反证：给同一账号补上 /api/admin/list GET 策略 → 同一请求立刻 200 并渲染出这些 PII
//     （证明 403 确实由这条策略决定，且该页真的会泄漏数据 —— 策略是唯一屏障）。
func TestAdminPagesReadOnlyPageForbiddenWithoutPolicy(t *testing.T) {
	env := newAdminPagesTestEnv(t)

	// 1) 前置条件：反例必须真的是「非超管 + 无该路径策略」，否则 403 证明不了任何事。
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

	// 2) 会话有效性对照：/api/admin/profile 只挂 SessionAuth（permission.Exempt），
	//    它返回 200 就说明下面的 403 来自 Casbin，而不是登录态失效。
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

	// 3) 目标：只读管理页 /admin/administrators（CasbinMiddlewareForPath("/api/admin/list")）。
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
	for _, leaked := range []string{
		adminPagesProbeAdminUsername, // 识别性用户名
		adminPagesProbeAdminEmail,    // 识别性邮箱
		support.TestAdminUsername,    // 超管用户名
		`<table class="data-table">`, // 列表页被渲染过的痕迹
		"</html>",                    // 整页渲染完成
	} {
		if strings.Contains(body, leaked) {
			t.Fatalf("403 响应里出现了 %q —— 管理员列表页被渲染了（用户名/邮箱泄漏）: %s", leaked, body)
		}
	}

	// 4) 因果反证（不用改任何非测试文件就能证明「403 是这条 Casbin 策略决定的」）：
	//    给同一个账号补上 /api/admin/list GET 策略后，**同一个请求**必须立刻放行，
	//    并且真的把管理员 PII 渲染出来。这一条同时说明两件事：
	//      · 上面的 403 确实来自 CasbinMiddlewareForPath("/api/admin/list")，不是别的中间件；
	//      · 这个只读页确实会泄漏用户名 / 邮箱 —— 策略就是唯一的屏障（而不是「本来也不显示」）。
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
