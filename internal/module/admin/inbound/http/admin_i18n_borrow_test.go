package adminhttp

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	adminenums "go_wp/internal/module/admin/enums"
	mw "go_wp/pkg/middleware"
)

var (
	adminPageRouteRe = regexp.MustCompile(`adminPages\.(GET|POST)\("([^"]+)"`)
	adminBorrowObjRe = regexp.MustCompile(`(?:builtin\.)?CasbinMiddlewareForPath(?:As)?\("(/api/[^"]+)"(?:,\s*"([A-Z]+)")?`)
	adminPageAuthzRe = regexp.MustCompile(`(?:adminui|mw)\.PageAuthz\("(/api/[^"]+)"\)`)
)

type adminPageBorrow struct {
	PageMethod string
	PagePath   string
	Act        string
	Obj        string
}

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

// adminI18nTestRegistry 是两个装配调用的共同索引：页面与 API 都登记在同一实例，
// 测试因此能证明「页面借用的对象在声明表里确实有权限点」。
var adminI18nTestRegistry = mw.NewRegistry()

func setupAdminI18nBothSides(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	adminI18nTestRegistry.Reset()
	SetupAdminPages(adminI18nTestRegistry, gin.New().Group("/admin"), nil, nil, nil, nil, nil, nil, nil, nil)
	engine := gin.New()

	SetupAdminRoutes(mw.NewRouteGroupIn(adminI18nTestRegistry, engine.Group("/api"), mw.RouteProtected), nil)
	return engine
}

func adminI18nPermsOf(reg *mw.Registry, method, path string) []string {
	var matched []string
	for _, spec := range reg.Snapshot() {
		if spec.Path == path && spec.Method == method {
			matched = append(matched, spec.Perm)
		}
	}
	return matched
}

func TestAdminI18nBorrowObjectsAreDeclaredAndResolvable(t *testing.T) {
	setupAdminI18nBothSides(t)

	seen := map[string]bool{}
	for _, b := range adminPageBorrows(t) {
		if !strings.HasPrefix(b.Obj, "/api/i18n") {
			continue
		}
		key := b.Act + " " + b.Obj
		seen[b.PageMethod+" "+b.PagePath+" → "+key] = true

		matched := adminI18nPermsOf(adminI18nTestRegistry, b.Act, b.Obj)
		switch len(matched) {
		case 0:
			t.Errorf("页面 %s %s enforce 的 %s 在声明表里查不到权限点 —— mw.RoutesOf 查空，ai_handle.go 的 casbinAuthorizer 会 fail closed，用到它的 AI 工具一律 forbidden", b.PageMethod, b.PagePath, key)
		case 1:

		default:
			t.Errorf("页面 %s %s enforce 的 %s 被多个权限点声明：%v —— 判权只按 (路径, 方法) 匹配，会互相串权", b.PageMethod, b.PagePath, key, matched)
		}
	}

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

func TestAdminI18nWriteEndpointIsRealRoute(t *testing.T) {
	engine := setupAdminI18nBothSides(t)

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	if !registered[http.MethodPost+" /api/i18n/save"] {
		t.Fatalf("POST /api/i18n/save 没有注册成真实路由（现有 /api 路由：%v）—— 页面写动作与 AI 工具侧的写权限点都会失去宿主", apiRoutesOf(engine))
	}

	owners := adminI18nPermsOf(adminI18nTestRegistry, http.MethodPost, "/api/i18n/save")
	if len(owners) != 1 || owners[0] != adminenums.I18nManage {
		t.Fatalf("POST /api/i18n/save 的权限点应为唯一的 i18n:manage，实际 %v —— 迁移 178/294 的 code/obj 必须逐字不变（改它等于让既有超管与角色策略失配）", owners)
	}
}

func TestAdminPageBorrowRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetupAdminPages(mw.NewRegistry(), r.Group("/admin"), nil, nil, nil, nil, nil, nil, nil, nil)

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

func apiRoutesOf(engine *gin.Engine) []string {
	var out []string
	for _, route := range engine.Routes() {
		if strings.HasPrefix(route.Path, "/api/") {
			out = append(out, route.Method+" "+route.Path)
		}
	}
	return out
}

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
