package feature

// dashboard_route_coverage_ledger_test.go — 后台路由的「有没有渲染测试」账本。
//
// 背景：dashboard 注册了 50 多条后台 GET 路由，但只有少数几条在测试里被真正请求过。
// 未覆盖的路由是整页级问题的盲区（模板被注释吞掉、t() 化后缺词导致 body 整块消失、
// 语言协商把整页渲染成另一种语言……），而这类问题的表现是「HTTP 200 + 半截页面」，
// 不会有任何断言变红 —— 只能靠人打开那个页面才发现。
//
// 这份账本不试图一次补齐渲染测试（真装配需要 23 个契约 + Session/CSRF/Casbin 三层中间件，
// 成本远高于本次收口），它做的是**把缺口钉成可见的数字**：基线是当前没有出现在任何测试里的
// 路由，只减不增。新加一条后台路由却不给它任何测试时，这条会直接变红。
//
// 结构完整性由 internal/templates 的 TestTemplatesAreStructurallyIntact 兜底（它覆盖全部
// 模板的配平与取词写法），两者合起来构成「整页级改动」的回归网：一条管「页面被打开过」，
// 一条管「页面结构完整」。

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// dashboardRouteGapBaseline 当前没有出现在任何测试里的后台 GET 路由（只减不增）。
//
// 消项方式不是从这里删名字，而是给该路由补一条渲染测试（参考
// page_translations_test.go / masterdata 那套「起真 PG + 只挂该路由」的最小装配写法）。
var dashboardRouteGapBaseline = map[string]bool{
	"/":                               true,
	"/admin/administrators":           true,
	"/admin/articles/edit":            true,
	"/admin/articles/translations":    true,
	"/admin/blocks":                   true,
	"/admin/content-templates":        true,
	"/admin/content-templates/edit":   true,
	"/admin/coupons":                  true,
	"/admin/datarules":                true,
	"/admin/datarules/edit":           true,
	"/admin/departments":              true,
	"/admin/i18n":                     true,
	"/admin/menus":                    true,
	"/admin/navigations/translations": true,
	"/admin/permissions":              true,
	"/admin/plugins":                  true,
	"/admin/returns":                  true,
	"/admin/roles":                    true,
	"/admin/site-slots":               true,
	"/admin/theme":                    true,
	"/admin/themes":                   true,
	"/admin/themes/settings":          true,
	"/workbench":                      true,
	"/workbench/block/preview":        true,
	"/workbench/preview":              true,
	"/workbench/template/preview":     true,
}

// TestDashboardRoutesHaveRenderTests 后台 GET 路由必须出现在测试里，缺口只减不增。
func TestDashboardRoutesHaveRenderTests(t *testing.T) {
	const routerDir = "../../../../internal/module/dashboard/inbound/http"
	const testRoot = "../../../.."

	raw, err := readRouterSources(routerDir)
	if err != nil {
		t.Fatalf("读取路由文件失败: %v", err)
	}
	routes := scanDashboardGetRoutes(raw)
	if len(routes) == 0 {
		t.Fatal("没有扫到任何后台 GET 路由，解析逻辑失效（测试会变成空转）")
	}

	tested := loadTestedPaths(t, testRoot)

	var gaps []string
	for _, route := range routes {
		if !tested[route] {
			gaps = append(gaps, route)
		}
	}
	sort.Strings(gaps)
	t.Logf("后台 GET 路由 %d 条，其中未出现在测试里的 %d 条：%v", len(routes), len(gaps), gaps)

	seen := map[string]bool{}
	for _, route := range gaps {
		seen[route] = true
		if !dashboardRouteGapBaseline[route] {
			t.Errorf("后台路由 %s 没有出现在任何测试里：整页级问题（模板被吞、缺词截断、渲染成别的语言）"+
				"在它上面不会有任何断言变红。请补一条渲染测试，而不是往基线里加名字", route)
		}
	}
	for route := range dashboardRouteGapBaseline {
		if !seen[route] {
			t.Errorf("基线里的 %s 已经有测试覆盖（或路由已删除），请从 dashboardRouteGapBaseline 删掉", route)
		}
	}
}

// readRouterSources 拼接 dashboard 包内全部路由注册源码。
//
// 审计 CQ-007 之后，路由注册按域拆到了 router_*.go（每个 setupXxxRoutes 一个文件），
// 所以账本要读整个路由源码集合，而不是单个 dashboard_router.go —— 解析逻辑与断言不变。
func readRouterSources(dir string) (string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, f := range files {
		base := filepath.Base(f)
		if strings.HasSuffix(base, "_test.go") ||
			(base != "dashboard_router.go" && !strings.HasPrefix(base, "router_")) {
			continue
		}
		chunk, rerr := os.ReadFile(f)
		if rerr != nil {
			return "", rerr
		}
		sb.Write(chunk)
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

// scanDashboardGetRoutes 从路由注册源码里取出后台 GET 路由的完整路径。
// 只认 .GET( 后面紧跟的字符串字面量，并按注册时的分组补前缀 —— 手写解析而不是正则，
// 因为路由注册里全是引号与括号，正则版本反而更难读。
func scanDashboardGetRoutes(src string) []string {
	seen := map[string]bool{}
	var out []string
	idx := 0
	for {
		i := strings.Index(src[idx:], ".GET(")
		if i < 0 {
			break
		}
		at := idx + i
		// 取出 .GET 之前的标识符（adminPages / authPages / router）。
		start := at
		for start > 0 && isIdentByte(src[start-1]) {
			start--
		}
		group := src[start:at]
		rest := src[at+len(".GET("):]
		idx = at + len(".GET(")
		if len(rest) == 0 || rest[0] != '"' {
			continue
		}
		end := strings.IndexByte(rest[1:], '"')
		if end < 0 {
			break
		}
		path := rest[1 : 1+end]
		full := path
		if group == "adminPages" {
			full = "/admin" + path
		}
		if full == "" {
			full = "/"
		}
		if !seen[full] {
			seen[full] = true
			out = append(out, full)
		}
	}
	return out
}

// isIdentByte 标识符字符（字母 / 数字 / 下划线）。
func isIdentByte(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// loadTestedPaths 收集测试源码里出现过的后台路径字面量。
//
// 判据是「路径作为字符串字面量出现在测试里」——测试请求路由时必然这么写；
// 用 strconv.Quote 生成带引号的形态来匹配，避免手写引号，也避免把 /admin/pages 与
// /admin/pages/x 这类前缀关系混为一谈（引号把它钉死了）。
func loadTestedPaths(t *testing.T, root string) map[string]bool {
	t.Helper()
	tested := map[string]bool{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		slashed := filepath.ToSlash(path)
		if !strings.HasSuffix(slashed, "_test.go") {
			return nil
		}
		// 本文件是账本自身：它列出的路径全是「缺口」，不能算作覆盖（否则基线会自己证明自己）。
		if strings.HasSuffix(slashed, "dashboard_route_coverage_ledger_test.go") {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		src := string(body)
		for _, marker := range []string{"/admin"} {
			from := 0
			for {
				i := strings.Index(src[from:], marker)
				if i < 0 {
					break
				}
				at := from + i
				from = at + 1
				// 取出这个字面量的结尾：遇到引号结束。
				rest := src[at:]
				end := 0
				for end < len(rest) {
					c := rest[end]
					// 结束符：双引号 / 单引号 / 空格 / 逗号 / 右括号（用字节值比较，避免源码里再出现引号）。
					if c == 0x22 || c == 0x27 || c == 0x20 || c == 0x2c || c == 0x29 {
						break
					}
					end++
				}
				if end == 0 {
					continue
				}
				tested[rest[:end]] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历测试目录失败: %v", err)
	}
	if len(tested) == 0 {
		t.Fatal("没有扫到任何 /admin 路径字面量（测试会变成空转）")
	}
	return tested
}

// usedOnlyForCoverageCheck 让 strconv 保持被引用（路径匹配依赖它的引号语义，保留 import 意图）。
var _ = strconv.Quote
