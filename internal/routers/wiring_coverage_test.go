package routers

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var uninjectableSetters = map[string]string{

	"SetStaleMarkers": "media 的多路注入由 media 自身在两处分别登记（portMediaStaleMarker*）",
}

var moduleInternalSetters = map[string]string{

	"SetArticleLinkLocator": "content.SetupContentPages：形参 loc（文章链接定位）转交译文工作台 handle",
	"SetContentWriter":      "content.SetupContentPages：模块内 i18n.NewContentWriterDefault() 构造后转交",
	"SetCollectionSchemas":  "content.SetupContentRoutes：形参 collections 转交集合源注册",

	"SetBlockPanelPort": "navigation.SetupNavigationPages：形参 blocks（BlockService）转交页面 handle",

	"SetCallLogWriter":     "ai.SetupAIRoutes：模块内 aimodel.NewCallLogModel(db) 构造（调用流水写侧）",
	"SetCallLogReader":     "ai.SetupAIRoutes：同一个 callLog 实例的读侧（同一个 model，无跨模块依赖）",
	"SetChatPort":          "ai.SetupAIRoutes：模块内 aiservice 实例转交会话层",
	"SetScopeValidator":    "ai.SetupAIRoutes：函数字面量（permission.Known 白名单）注入令牌层",
	"SetToolProvider":      "ai.SetupAIRoutes：模块内 &toolProvider{...}（mcp.NewRunner）构造",
	"SetToolCallLogWriter": "ai.SetupAIRoutes：模块内 aimodel.NewToolCallLogModel(db) 构造",
	"SetConfigReader":      "ai.SetupAIRoutes：形参 aiConfig（sysconfigcontract.Service）转交 MCP 端点",
	"SetConfigService":     "ai.SetupAIPages：形参 aiConfig 转交 MCP 页面 handle",

	"SetCustomerCohort":    "user.SetupCustomerPages：形参 cohort（客户域聚合端口）转交",
	"SetCustomerGrowth":    "user.SetupCustomerPages：形参 growth 转交",
	"SetCustomerRfm":       "user.SetupCustomerPages：形参 rfm 转交",
	"SetCustomerSegments":  "user.SetupCustomerPages：形参 segments 转交",
	"SetMembershipDisplay": "user.SetupCustomerPages：形参 membership 转交（会员等级 / 权益只读展示）",
	"SetMembershipFilters": "user.SetupCustomerPages：形参 membershipAdmin 转交（按等级筛选的两个只读端口）",

	"SetInventoryDeps":      "product.SetupProductPages：形参 inventories 转交；它在转交方法内注入真端口 product.SetWarehouseOptionsPort（已登记 manifest）",
	"SetDetailTemplateDeps": "product.SetupProductPages：形参 contentTemplates + instances 转交；它在转交方法内注入真端口 product.SetEntityTemplatePort / SetDetailTemplatePort（已登记 manifest）",

	"SetPublishRecordDeps": "product.SetupProductPages：形参 attempts 转交；它在转交方法内注入真端口 product.SetPublishAttemptPort（发布历史读口；发布记录页已改模块内一次只读语句，不再需要发布端口）",

	"SetPublishAttemptPort":   "product.SetPublishAttemptPort：发布历史读口，由 moduleInbound 的转交方法把 outbound/publishrecord 适配器交到 service",
	"SetSeoTitleSources":      "product.SetupProductPages：形参 pageSvc 转交 SEO 标题候选来源",
	"SetI18nStaleMarker":      "product.NewProductTranslationHandle：构造参数 pages（失效标记）转交",
	"SetTranslationInstances": "product.NewProductTranslationHandle：构造参数 instances 转交",
	"SetTranslationStore":     "product.SetContentTranslationStore：形参 port（译文存储口）转交",

	"SetDictReader": "admin.SetupAdminPages：形参 dict（sysconfigcontract.DictReader）转交 i18n 词条页",
	"SetLangCookie": "admin.AdminLangSwitch：包级助手 response.SetLangCookie —— HTTP 出口写语言 cookie，不是端口",
	"SetProvider":   "admin.bootstrapDataRule：包级函数 datarule.SetProvider —— 数据域注册入口（装配期必做），不是端口",

	"SetCollectionResolver": "presentation.SetupPresentationRoutes：形参 collections 转交集合源解析",
	"SetPlugins":            "workbench.New（router.go 构造期）：形参 plugins 转交编辑器平台服务",
	"SetProjects":           "analytics.SetupAnalyticsRoutes：形参 projects 转交访问统计服务",
}

func scanSetterCalls(t *testing.T, files []string, stmtOnly bool) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	found := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, perr := parser.ParseFile(fset, f, nil, 0)
		if perr != nil {
			t.Fatalf("解析 %s 失败: %v", f, perr)
		}
		statements := map[*ast.CallExpr]bool{}
		if stmtOnly {
			ast.Inspect(parsed, func(n ast.Node) bool {
				if es, ok := n.(*ast.ExprStmt); ok {
					if call, ok := es.X.(*ast.CallExpr); ok {
						statements[call] = true
					}
				}
				return true
			})
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			name := sel.Sel.Name

			if !strings.HasPrefix(name, "Set") || strings.HasPrefix(name, "Setup") || len(name) <= 3 {
				return true
			}
			if stmtOnly && !statements[call] {
				return true
			}
			if _, seen := found[name]; !seen {
				found[name] = f
			}
			return true
		})
	}
	return found
}

func reportOutdatedExemptions(t *testing.T, side string, exempt map[string]string, used map[string]bool) {
	t.Helper()
	var outdated []string
	for name := range exempt {
		if !used[name] {
			outdated = append(outdated, name)
		}
	}
	if len(outdated) == 0 {
		return
	}
	sort.Strings(outdated)
	t.Fatalf("%s 豁免清单有过期条目（扫描已看不到该调用，或它已被 wiringManifest 覆盖）：\n  %s\n"+
		"过期条目必须删除：清单只减不增，留着会让下一个改动以为这条边界还有人守",
		side, strings.Join(outdated, "\n  "))
}

func TestEverySetterCallIsInWiringManifest(t *testing.T) {

	known := make([]string, 0, len(wiringManifest))
	for _, e := range wiringManifest {
		known = append(known, e.Port)
	}
	isKnown := func(name string) bool {
		for _, port := range known {
			if strings.Contains(port, name) {
				return true
			}
		}
		return false
	}

	localFiles, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("枚举本包源文件失败: %v", err)
	}
	const moduleGlob = "../module/*/inbound/http/*.go"
	moduleFiles, err := filepath.Glob(moduleGlob)
	if err != nil {
		t.Fatalf("枚举模块 inbound/http 源文件失败: %v", err)
	}

	if len(moduleFiles) == 0 {
		t.Fatalf("扫描面失效：%s 匹配不到任何文件（测试工作目录应为 internal/routers）", moduleGlob)
	}

	local := scanSetterCalls(t, localFiles, false)
	module := scanSetterCalls(t, moduleFiles, true)
	if len(local) == 0 || len(module) == 0 {
		t.Fatalf("判据失效：本包扫到 %d 个 Set* 调用、模块 inbound/http 扫到 %d 个（两侧都必须非空）",
			len(local), len(module))
	}

	var unregistered []string
	collect := func(side string, found map[string]string, exempt map[string]string) {
		used := map[string]bool{}
		for name, file := range found {
			if isKnown(name) {
				continue
			}
			if _, ok := exempt[name]; ok {
				used[name] = true
				continue
			}
			unregistered = append(unregistered, fmt.Sprintf("%s（%s：%s）", name, side, strings.TrimPrefix(file, "../")))
		}
		reportOutdatedExemptions(t, side, exempt, used)
	}
	collect("本包", local, uninjectableSetters)
	collect("模块 inbound/http", module, moduleInternalSetters)

	sort.Strings(unregistered)
	if len(unregistered) > 0 {
		t.Fatalf("以下 Set* 调用不在 wiringManifest 里（漏接时既不 fail-fast 也不进降级日志）：\n  %s\n"+
			"补 manifest 条目 + marks.mark，或加进 uninjectableSetters / moduleInternalSetters 并写明理由",
			strings.Join(unregistered, "\n  "))
	}
	t.Logf("已核对本包 %d 个 + 模块 inbound/http %d 个 Set* 调用；manifest 端口 %d 条",
		len(local), len(module), len(wiringManifest))
}