package adminhttp

// admin_i18n_borrow_test.go — 词条页的守卫：借用的 Casbin 对象「唯一可反解」+ 写端点是**真实路由**。
//
// 背景（task-13 确认借用形态，task-17 迁移写通道）：词条页原先的四条页面 POST
// （/i18n/update、/i18n/save、/i18n/delete、/i18n/bulk-delete）与片段口 GET /i18n/edit
// 全部借用 POST /api/i18n/save，而 /api/i18n 命名空间下连一条真实路由都没有。task-17 把四种
// 写动作收敛到**真实注册**的 JSON 端点 POST /api/i18n/save（注册点 admin_router.go，
// 权限点沿用迁移 178 的 i18n:manage → 该路径 POST），页面侧于是只剩两条借用：
//   · GET  /admin/i18n      → adminui.PageAuthz("/api/i18n/list")（act=GET，权限点 i18n:view）
//   · GET  /admin/i18n/edit → CasbinMiddlewareForPathAs("/api/i18n/save","POST")（act=POST）
// 写对象的声明不再由页面注册处的 permission.Declare 承担，而是由 API 注册处的
// RouteGroup.POST 承担 —— 所以「必须在本文件 Declare」这条判据已随迁移退役，换成本文件下面
// 两条：借用对象唯一可反解、写端点确实是挂着权限点的真实路由。
//
// 本文件守的不变量（判据都来自**真实装配**：SetupAdminPages + SetupAdminRoutes，不扫源码猜）：
//   · 页面注册处扫到的每个 /api/i18n* 借用对象，(方法, 路径) 在声明表里**恰好**反解出一个
//     权限点：零个 → permission.RoutesOf 查空 → ai_handle.go 的 casbinAuthorizer fail closed，
//     用到它的 AI 工具一律 forbidden；多个 → 判权只按 (路径, 方法) 匹配，会互相串权。
//   · POST /api/i18n/save 必须在 gin 路由表里（真实路由），且恰好归 i18n:manage 一个权限点 ——
//     它承担了原先四条页面 POST 的全部写动作，退回成「只有页面借用、没有宿主路由」的形态时，
//     这个权限点在路由侧就没有可命中的宿主。
//
// 运行时面（无策略 403 / 角色策略放行 / CSRF / JSON 写端点的成功与失败分支）由
// public/test/admin/feature/admin_i18n_edit_drawer_test.go 覆盖（真实链 + 真实 PG），
// 本文件只做装配级不变量，不代替它。

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/permission"
)

var (
	adminPageRouteRe = regexp.MustCompile(`adminPages\.(GET|POST)\("([^"]+)"`)
	adminBorrowObjRe = regexp.MustCompile(`(?:builtin\.)?CasbinMiddlewareForPath(?:As)?\("(/api/[^"]+)"(?:,\s*"([A-Z]+)")?`)
	adminPageAuthzRe = regexp.MustCompile(`adminui\.PageAuthz\("(/api/[^"]+)"\)`)
)

// adminPageBorrow 页面路由 → 它 enforce 的 (方法, 对象)。
type adminPageBorrow struct {
	PageMethod string // 页面路由自身的 HTTP 方法
	PagePath   string
	Act        string // enforce 用的方法（ForPathAs 可强制）
	Obj        string
}

// adminPageBorrows 从页面注册源码扫出所有页面路由的鉴权对象。
//
// 逐行扫：注册行都是一行一条（多行写法出现时本用例会因扫不到对象而显式报错，不会静默空转）。
func adminPageBorrows(t *testing.T) []adminPageBorrow {
	t.Helper()
	raw, err := os.ReadFile("admin_pages_router.go")
	if err != nil {
		t.Fatalf("读 admin_pages_router.go 失败：%v", err)
	}
	var out []adminPageBorrow
	for _, line := range strings.Split(string(raw), "\n") {
		page := adminPageRouteRe.FindStringSubmatch(line)
		if page == nil {
			continue
		}
		if m := adminBorrowObjRe.FindStringSubmatch(line); m != nil {
			act := m[2]
			if act == "" {
				act = page[1]
			}
			out = append(out, adminPageBorrow{PageMethod: page[1], PagePath: page[2], Act: act, Obj: m[1]})
			continue
		}
		if m := adminPageAuthzRe.FindStringSubmatch(line); m != nil {
			out = append(out, adminPageBorrow{PageMethod: page[1], PagePath: page[2], Act: "GET", Obj: m[1]})
		}
	}
	if len(out) == 0 {
		t.Fatal("没有扫到任何页面鉴权对象 —— 正则与注册写法脱节，本用例会静默变成空转")
	}
	return out
}

// setupAdminI18nBothSides 跑一次真实装配：页面侧（声明 /api/i18n/list）+ API 侧（注册写端点）。
//
// 两侧都必须跑：借用对象的声明现在横跨两处，只跑一侧会让「唯一可反解」这条判据误报为 0 个。
func setupAdminI18nBothSides(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	SetupAdminPages(gin.New().Group("/admin"), nil, nil, nil, nil, nil, nil, nil, nil)
	engine := gin.New()
	// db 传 nil：本用例只关心注册期的路径与权限点声明，不触碰任何表
	// （block 侧同形态的用例也是这么做的，见 block_router_permission_test.go）。
	SetupAdminRoutes(permission.NewRouteGroup(engine.Group("/api")), nil)
	return engine
}

// adminI18nPermsOf 反解 (方法, 路径) 对应的权限点（判权只按这两个字段匹配，故这里也只按它们查）。
func adminI18nPermsOf(method, path string) []permission.Perm {
	var matched []permission.Perm
	for _, p := range permission.All() {
		for _, r := range permission.RoutesOf(p) {
			if r.Path == path && r.Method == method {
				matched = append(matched, p)
			}
		}
	}
	return matched
}

// TestAdminI18nBorrowObjectsAreDeclaredAndResolvable 页面借用的 /api/i18n* 对象必须唯一可反解。
func TestAdminI18nBorrowObjectsAreDeclaredAndResolvable(t *testing.T) {
	setupAdminI18nBothSides(t)

	seen := map[string]bool{}
	for _, b := range adminPageBorrows(t) {
		if !strings.HasPrefix(b.Obj, "/api/i18n") {
			continue // 其它页面借用的是各模块 API 注册处声明的对象，不属本用例的责任
		}
		key := b.Act + " " + b.Obj
		seen[b.PageMethod+" "+b.PagePath+" → "+key] = true

		matched := adminI18nPermsOf(b.Act, b.Obj)
		switch len(matched) {
		case 0:
			t.Errorf("页面 %s %s enforce 的 %s 在声明表里查不到权限点 —— permission.RoutesOf 查空，ai_handle.go 的 casbinAuthorizer 会 fail closed，用到它的 AI 工具一律 forbidden", b.PageMethod, b.PagePath, key)
		case 1:
			// 期望形态：一条 (方法, 路径) ↔ 一个权限点。
		default:
			t.Errorf("页面 %s %s enforce 的 %s 被多个权限点声明：%v —— 判权只按 (路径, 方法) 匹配，会互相串权", b.PageMethod, b.PagePath, key, matched)
		}
	}

	// 借用集合本身钉死：页面鉴权被改宽（多借一条）或被改窄（少借一条）都要显式过来说一声，
	// 不能因为「扫出来的东西各自都合法」而静默通过。
	want := map[string]bool{
		"GET /i18n → GET /api/i18n/list":       true,
		"GET /i18n/edit → POST /api/i18n/save": true,
	}
	for key := range want {
		if !seen[key] {
			t.Errorf("页面借用集合里少了 %q（实际：%v）", key, sortedKeys(seen))
		}
	}
	for key := range seen {
		if !want[key] {
			t.Errorf("页面借用集合里多了 %q —— 写动作已全部收敛到 JSON 端点，页面不该再借新的对象", key)
		}
	}
}

// TestAdminI18nWriteEndpointIsRealRoute 写端点必须是挂着 i18n:manage 的真实路由。
//
// 这是 task-17 迁移的**落点**：路径与权限点逐字沿用迁移 178（sys_permission 只写默认值、不覆盖
// 运营改动），所以端点必须真注册在这条 (POST, /api/i18n/save) 上 —— 否则权限点没有宿主，
// 页面写动作也就没有可命中的路由（前端会稳定 404）。
func TestAdminI18nWriteEndpointIsRealRoute(t *testing.T) {
	engine := setupAdminI18nBothSides(t)

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	if !registered[http.MethodPost+" /api/i18n/save"] {
		t.Fatalf("POST /api/i18n/save 没有注册成真实路由（现有 /api 路由：%v）—— 页面写动作与 AI 工具侧的写权限点都会失去宿主", apiRoutesOf(engine))
	}

	owners := adminI18nPermsOf(http.MethodPost, "/api/i18n/save")
	if len(owners) != 1 || owners[0] != permission.I18nManage {
		t.Fatalf("POST /api/i18n/save 的权限点应为唯一的 i18n:manage，实际 %v —— 迁移 178/294 的 code/obj 必须逐字不变（改它等于让既有超管与角色策略失配）", owners)
	}
}

// TestAdminPageBorrowRoutesAreRegistered 扫到的页面路由必须真的挂在 /admin 组上。
func TestAdminPageBorrowRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetupAdminPages(r.Group("/admin"), nil, nil, nil, nil, nil, nil, nil, nil)

	registered := map[string]bool{}
	for _, route := range r.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, b := range adminPageBorrows(t) {
		if !registered[b.PageMethod+" /admin"+b.PagePath] {
			t.Errorf("页面 %s /admin%s 没有注册（扫到的借用集合里有它）", b.PageMethod, b.PagePath)
		}
	}
}

// apiRoutesOf 列出 /api 前缀的路由（失败信息里带上，省一次复现）。
func apiRoutesOf(engine *gin.Engine) []string {
	var out []string
	for _, route := range engine.Routes() {
		if strings.HasPrefix(route.Path, "/api/") {
			out = append(out, route.Method+" "+route.Path)
		}
	}
	return out
}

// sortedKeys 把集合的键排成稳定顺序（失败信息可读、可 diff）。
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
