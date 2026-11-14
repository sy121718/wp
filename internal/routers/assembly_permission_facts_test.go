package routers

import (
	"net/http"
	"os"
	"strings"
	"testing"

	mw "go_wp/pkg/middleware"
)

// 这些断言跑的是**真实装配**产出的登记索引（bootstrapRouter → SetupRoutesWithRegistry），
// 不是手工拼两个 Registry：只有真实装配才能证明页面前缀、页面复用鉴权对象、
// 冻结时机与「运行时入口全部已登记」在同一次装配里同时成立。
//
// 需要数据库与完整组件装配，因此与 TestDumpRoutesForAudit 一样用环境变量开关：
//
//	WP_DUMP_ROUTES=1 GOWP_AUDIT_CONFIG=<隔离配置> go test ./internal/routers -run TestRealAssembly
func requireRealAssembly(t *testing.T) {
	t.Helper()
	if os.Getenv("WP_DUMP_ROUTES") != "1" {
		t.Skip("设置 WP_DUMP_ROUTES=1 才运行（需要数据库与完整组件装配）")
	}
}

// 真实装配的权限核对必须**通过**，并且结论与闸门一致：
//
//   - 目录核对包含工具权限码与策略存在性（目录有行但一条 p 策略都没有 = 全员 403）；
//   - 通过时闸门必须放行；不通过时闸门必须拒绝，且问题清单逐条可见。
//
// 只看「装配没崩」不够：核对失败的实例以前照样能注册路由，
// 差别只在 permProblems 与闸门状态上，必须真的断言这两项。
func TestRealAssemblyPermissionCheckPassesAndOpensGate(t *testing.T) {
	requireRealAssembly(t)
	bootstrapRouter(t)

	if len(auditPermProblems) > 0 {
		t.Fatalf("真实装配的权限核对没有通过（实例会拒绝业务流量）：\n    %s",
			strings.Join(auditPermProblems, "\n    "))
	}
	if !auditGateReady {
		t.Fatal("权限核对通过后闸门必须放行；否则实例永远 503")
	}
}

func TestRealAssemblyRegistryIsFrozenAndComplete(t *testing.T) {
	requireRealAssembly(t)
	bootstrapRouter(t)
	if auditRegistry == nil {
		t.Fatal("真实装配没有交回登记索引")
	}
	if !auditRegistry.Frozen() {
		t.Fatal("真实装配结束后登记索引必须已冻结")
	}
	specs := auditRegistry.Snapshot()
	if len(specs) == 0 {
		t.Fatal("真实装配的登记索引为空")
	}
	for _, spec := range specs {
		if spec.Kind == "" {
			t.Errorf("%s %s 没有显式 Kind", spec.Method, spec.Path)
		}
		if spec.Perm == "" {
			continue
		}
		if spec.AuthObject == "" || spec.AuthMethod == "" {
			t.Errorf("%s %s 的鉴权目标不完整：%q %q", spec.Method, spec.Path, spec.AuthObject, spec.AuthMethod)
		}
	}
}

// 页面登记必须写真实入口：mail 页面挂在 /admin 组下，
// 登记若还写成 /mail，启动核对逻辑就看不见真实入口。
func TestRealAssemblyMailPagesUseAdminPrefix(t *testing.T) {
	requireRealAssembly(t)
	bootstrapRouter(t)

	want := map[string]string{
		"GET /admin/mail":            "/api/mail/account/list",
		"GET /admin/mail/contacts":   "/api/mail/contact/list",
		"GET /admin/mail/campaigns":  "/api/mail/campaign/list",
		"GET /admin/mail/marketing":  "/api/mail/contact/list",
		"GET /admin/mail/templates":  "/api/mail/template/list",
		"GET /admin/mail/automation": "/api/mail/automation/list",
	}
	got := map[string]string{}
	for _, spec := range auditRegistry.Snapshot() {
		key := spec.Method + " " + spec.Path
		if strings.HasPrefix(spec.Path, "/mail") {
			t.Errorf("邮件页面登记用了无 /admin 前缀的路径：%s（真实入口是 /admin%s）", key, spec.Path)
		}
		if _, ok := want[key]; ok {
			got[key] = spec.AuthObject
		}
	}
	for key, authObject := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("真实装配里没有登记邮件页面 %s", key)
			continue
		}
		if got[key] != authObject {
			t.Errorf("%s 复用的鉴权对象应为 %s，实际 %s", key, authObject, got[key])
		}
	}
}

// 页面复用别的操作鉴权时，真实路径与鉴权目标必须分开登记（publication SEO 审核）。
func TestRealAssemblyPublicationSEOAuditKeepsBothFacts(t *testing.T) {
	requireRealAssembly(t)
	bootstrapRouter(t)

	var found *mw.RouteSpec
	for _, spec := range auditRegistry.Snapshot() {
		if spec.Method == http.MethodPost && spec.Path == "/api/publication/seo-audit" {
			s := spec
			found = &s
			break
		}
	}
	if found == nil {
		t.Fatal("真实装配里没有登记 /api/publication/seo-audit")
	}
	if found.AuthObject != "/api/seo/audit" || found.AuthMethod != http.MethodPost {
		t.Fatalf("SEO 审核入口应复用 /api/seo/audit：实际 %s %s", found.AuthMethod, found.AuthObject)
	}
	if !strings.HasPrefix(found.Perm, "publication:") {
		t.Fatalf("SEO 审核入口应保留自己的权限点，实际 %q", found.Perm)
	}
}

// 运行时 /api 入口必须在同一装配里登记为受保护或明确豁免。
// 这是「漏登记」的机器判据：没有它，一条根本没进登记的路由永远不会被问到。
func TestRealAssemblyEveryAPIRouteIsRegistered(t *testing.T) {
	requireRealAssembly(t)
	lines := bootstrapRouter(t)

	registered := map[string]bool{}
	for _, spec := range auditRegistry.Snapshot() {
		registered[spec.Method+" "+spec.Path] = true
	}
	for _, key := range auditRegistry.Exempts() {
		registered[key] = true
	}
	var missing []string
	for _, line := range lines {
		if !strings.HasPrefix(line, "GET /api/") && !strings.HasPrefix(line, "POST /api/") &&
			!strings.HasPrefix(line, "PUT /api/") && !strings.HasPrefix(line, "DELETE /api/") &&
			!strings.HasPrefix(line, "PATCH /api/") {
			continue
		}
		if !registered[line] {
			missing = append(missing, line)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("以下运行时 /api 入口没有进入登记：\n    %s", strings.Join(missing, "\n    "))
	}
}

// 冻结后迟到注册必须明确报错；冻结前登记的同一事实幂等。
func TestRealAssemblyRegistryRejectsLateRegistration(t *testing.T) {
	requireRealAssembly(t)
	bootstrapRouter(t)
	defer func() {
		if recover() == nil {
			t.Fatal("冻结后的迟到注册必须 panic")
		}
	}()
	auditRegistry.DeclareRoute(mw.RouteSpec{
		Method: http.MethodPost, Path: "/api/__test__/late", Perm: "test:late", Kind: mw.RouteProtected,
	})
}

// 工作台三块面板在**真实装配**里必须既有登记、又有守卫。
//
// 这条测试只证明「登记事实」：路由存在、且登记声明了复用的鉴权对象。
// 它发不出请求（真实装配的会话依赖外部组件），所以**不能**用它证明守卫被删除后会红；
// 那一半由 public/test/workbench/feature 的
// TestWorkbenchPanelGuardComesFromProductionRouter 证明（直接调用生产注册函数并发请求）。
// 两条合起来才是「登记 + 执行」成对。
func TestWorkbenchPanelsAreRegisteredWithAuthObject(t *testing.T) {
	requireRealAssembly(t)
	lines := bootstrapRouter(t)

	// 从真实装配的路由表确认这三条入口存在（handler 数量 > 1 说明链路里有中间件）。
	present := map[string]bool{}
	for _, line := range lines {
		if strings.HasPrefix(line, "POST /workbench/settings") ||
			strings.HasPrefix(line, "POST /workbench/seo-score-panel") ||
			strings.HasPrefix(line, "POST /workbench/global") {
			present[line] = true
		}
	}
	for _, want := range []string{
		"POST /workbench/settings", "POST /workbench/seo-score-panel", "POST /workbench/global",
	} {
		if !present[want] {
			t.Errorf("真实装配里缺少 %s", want)
		}
	}

	// 登记必须声明它们复用的鉴权对象；并且装配里的这三个写入口必须挂在守卫上。
	authObjects := map[string]string{}
	for _, spec := range auditRegistry.Snapshot() {
		if spec.Method != http.MethodPost {
			continue
		}
		switch spec.Path {
		case "/workbench/settings", "/workbench/seo-score-panel", "/workbench/global":
			authObjects[spec.Path] = spec.AuthObject
		}
	}
	for _, path := range []string{"/workbench/settings", "/workbench/seo-score-panel", "/workbench/global"} {
		if authObjects[path] == "" {
			t.Errorf("%s 没有登记复用的鉴权对象", path)
		}
	}
}
