package routers

// wiring_coverage_test.go — 防复发：装配期 Set* 注入必须在 wiringManifest 里有条目
// （FIX-25 的根因就是「注入了但没进盘点表」）。
//
// 判据不看注释、只看调用。扫描面是**两处**装配期注入现场：
//
//	① 本包 internal/routers 的非测试源码 —— 装配段直接把跨模块端口交给别的模块；
//	② 各模块 internal/module/<mod>/inbound/http/*.go —— 模块自装配内部的转交
//	   （product.SetInventoryDeps 里调 wiring.SetWarehouseOptionsPort 这类）。
//
// ② 是后补的：旧口径只看 ① ，于是「注入发生在模块内部」的端口在盘点上凭空消失 ——
// manifest 有条目、启动自检也 marks 了，但扫描看不见它，漏接时同样既不 fail-fast
// 也不进降级日志。两个已知的盲区端口就是这么漏的：
// product.SetWarehouseOptionsPort（portProductPageWarehouses）与
// product.SetDetailTemplatePort（portProductPageDetailTemplate）。
//
// ② 的判别口径：**只认不接收返回值的调用语句**（ast.ExprStmt）。装配期注入型 setter
// 无返回值（注入的是依赖，不是结果）；而模块里以 Set 开头的**业务写方法**
// （h.svc.SetBOM(ctx, req)、h.products.SetAttributeValues(ctx, req)）返回 error 且必须被
// 检查 —— 实测这些调用全是「赋值/判断」形态、首参是 ctx 或 c.Request.Context()，
// 被这条口径自然排除，不需要为它们写豁免条目。
//
// 未登记 = 这个注入点漏接时既不 fail-fast 也不进降级日志 —— 正是要消灭的空洞。

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

// uninjectableSetters 本包（internal/routers）扫到、但不属于「装配期端口注入」的 Set* 调用 → 理由。
//
// 只列真正不适用清单（不是尚未登记的端口）：新增条目必须写明理由。
// 条目一旦在扫描里不再出现（调用点被删 / 改名 / 已补进 manifest）即判失败，见
// reportOutdatedExemptions —— 清单只减不增，不许退化成注释。
var uninjectableSetters = map[string]string{
	// 装配期内部状态写入（无「未注入」语义：调用恒发生且不跨模块）。
	"SetStaleMarkers": "media 的多路注入由 media 自身在两处分别登记（portMediaStaleMarker*）",
	// gin.Context.Set（请求上下文写值）不在此列：它的名字是 Set，长度 <= 3，
	// 扫描口径直接跳过（见 scanSetterCalls），留一条永远不会被命中的条目只会让清单失真。
}

// moduleInternalSetters 模块 inbound/http 侧扫到、但**不是跨模块可空端口**的 Set* 调用 → 理由。
//
// 口径同 wiring.go 文件头第二条边界：这些调用的实参在编译期必给（模块 Setup 的形参、
// 或本模块自己构造的对象），不存在「未注入 → 用户看到什么」这个状态，因此不进
// wiringManifest。两条例外是**包级助手**（response.SetLangCookie / datarule.SetProvider），
// 它们只是名字以 Set 开头，与端口无关。
//
// 每条都写明「谁在哪把什么交给谁」：下一个人要能分清它是端口还是噪声。
// 条目一旦在扫描里不再出现即判失败（调用点被删 / 改名 / 已补进 manifest）。
var moduleInternalSetters = map[string]string{
	// —— content ——
	"SetArticleLinkLocator": "content.SetupContentPages：形参 loc（文章链接定位）转交译文工作台 handle",
	"SetContentWriter":      "content.SetupContentPages：模块内 i18n.NewContentWriterDefault() 构造后转交",
	"SetCollectionSchemas":  "content.SetupContentRoutes：形参 collections 转交集合源注册",
	// —— navigation ——
	"SetBlockPanelPort":  "navigation.SetupNavigationPages：形参 blocks（BlockService）转交页面 handle",
	"SetMenuInvalidator": "navigation.SetupNavigationPages：形参 navigations 的收窄断言（navTranslationMenuInvalidator）转交",
	"SetPageMarker":      "navigation.SetupNavigationPages：形参 pageSvc 的收窄断言（navTranslationPageMarker）转交；admin.SetupAdminPages 另有一处形参 pages 转交（同名同义）",
	// —— ai ——
	"SetCallLogWriter":     "ai.SetupAIRoutes：模块内 aimodel.NewCallLogModel(db) 构造（调用流水写侧）",
	"SetCallLogReader":     "ai.SetupAIRoutes：同一个 callLog 实例的读侧（同一个 model，无跨模块依赖）",
	"SetChatPort":          "ai.SetupAIRoutes：模块内 aiservice 实例转交会话层",
	"SetScopeValidator":    "ai.SetupAIRoutes：函数字面量（permission.Known 白名单）注入令牌层",
	"SetToolProvider":      "ai.SetupAIRoutes：模块内 &toolProvider{...}（mcp.NewRunner）构造",
	"SetToolCallLogWriter": "ai.SetupAIRoutes：模块内 aimodel.NewToolCallLogModel(db) 构造",
	"SetConfigReader":      "ai.SetupAIRoutes：形参 aiConfig（sysconfigcontract.Service）转交 MCP 端点",
	"SetConfigService":     "ai.SetupAIPages：形参 aiConfig 转交 MCP 页面 handle",
	// —— user（客户页：全部是 SetupCustomerPages 的形参转交）——
	"SetCustomerCohort":    "user.SetupCustomerPages：形参 cohort（客户域聚合端口）转交",
	"SetCustomerGrowth":    "user.SetupCustomerPages：形参 growth 转交",
	"SetCustomerRfm":       "user.SetupCustomerPages：形参 rfm 转交",
	"SetCustomerSegments":  "user.SetupCustomerPages：形参 segments 转交",
	"SetMembershipDisplay": "user.SetupCustomerPages：形参 membership 转交（会员等级 / 权益只读展示）",
	"SetMembershipFilters": "user.SetupCustomerPages：形参 membershipAdmin 转交（按等级筛选的两个只读端口）",
	// —— product ——
	"SetInventoryDeps":          "product.SetupProductPages：形参 inventories 转交；它在转交方法内注入真端口 product.SetWarehouseOptionsPort（已登记 manifest）",
	"SetDetailTemplateDeps":     "product.SetupProductPages：形参 contentTemplates + instances 转交；它在转交方法内注入真端口 product.SetEntityTemplatePort / SetDetailTemplatePort（已登记 manifest）",
	"SetDetailTemplateModePort": "product.SetupProductPages：形参 presentations 转交详情页模板模式端口",
	// 发布记录页（B9）：与上面两条同形态 —— 形参 presentations（静态类型 PresentationService
	// 已嵌入 presentation 的 PublishRecordPort，编译期成立）交进来，在转交方法内部包成
	// outbound/publishrecord 再注入读模型。不需要运行期断言，也没有第二个真端口要 marks。
	"SetPublishRecordDeps":    "product.SetupProductPages：形参 instances 转交；它在转交方法内注入真端口 product.SetPublishRecordPort（本模块读模型字段）",
	"SetPublishRecordPort":    "product.SetPublishRecordPort：moduleInbound 的转交方法把 outbound/publishrecord 适配器交到 service 读模型，本行即那个真端口",
	"SetSeoTitleSources":      "product.SetupProductPages：形参 pageSvc 转交 SEO 标题候选来源",
	"SetI18nStaleMarker":      "product.NewProductTranslationHandle：构造参数 pages（失效标记）转交",
	"SetTranslationInstances": "product.NewProductTranslationHandle：构造参数 instances 转交",
	"SetTranslationStore":     "product.SetContentTranslationStore：形参 port（译文存储口）转交",
	// —— admin ——
	"SetDictReader": "admin.SetupAdminPages：形参 dict（sysconfigcontract.DictReader）转交 i18n 词条页",
	"SetLangCookie": "admin.AdminLangSwitch：包级助手 response.SetLangCookie —— HTTP 出口写语言 cookie，不是端口",
	"SetProvider":   "admin.bootstrapDataRule：包级函数 datarule.SetProvider —— 数据域注册入口（装配期必做），不是端口",
	// —— 其余模块 ——
	"SetCollectionResolver": "presentation.SetupPresentationRoutes：形参 collections 转交集合源解析",
	"SetPlugins":            "workbench.New（router.go 构造期）：形参 plugins 转交编辑器平台服务",
	"SetProjects":           "analytics.SetupAnalyticsRoutes：形参 projects 转交访问统计服务",
}

// scanSetterCalls 扫一组 Go 源文件里的 Set* 调用，返回 setter 名 → 首次出现的文件。
//
// stmtOnly 为真时只认**不接收返回值**的调用语句（模块装配期注入的形态，见文件头口径）。
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
			// Setup* 是模块装配入口（不是端口注入）；Set 本身是 gin 的上下文写值。
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

// reportOutdatedExemptions 过期条目检查：豁免清单里的名字必须真的挡下了一个未登记调用。
//
// 两个方向都失败：条目还在、扫描已看不到（调用点删了 / 改名了 / 已补进 manifest）→
// 清单退化成注释；条目从未命中（名字早就被 isKnown 短路）→ 更糟：它看上去在守一条边界，
// 实际什么都没守。判据：used 集合由「走了豁免分支」的调用构成，不是「名字出现过」。
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
	// 判定用**子串包含**：manifest 里存在合并条目（如
	// "presentation.SetNavigationService/SetSitePageResolver/SetMediaProbe/SetPluginService"、
	// "pipeline.Fanout.SetRebuilder(page)"），它们用一条记录覆盖同一次注入的多个 setter。
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
	// 判据失效比判据不全更糟：glob 打空（换了工作目录 / 目录被搬走）时必须报错，
	// 而不是「一个文件都没扫到 → 没有违规 → 绿」。
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

// TestRuntimeFragDepsFieldsMatchWiringManifest — runtimefragment 收敛（N setter → 1 SetDependencies）
// 的配套判据，补上上面那条扫描看不见的那一半。
//
// 收敛把 21 个 `runtimefragment.SetXxxProvider(v)` 调用换成了 `a.fragDeps.X = v` **赋值语句**，
// 而上面的扫描只看 CallExpr。于是出现一个新空洞：若将来「加了 Deps 字段 + manifest 条目 +
// marks.mark，却忘了在装配里赋值」，启动自检（marks 齐）会通过，直到运行到该片段才 nil panic。
// 本测试把这段覆盖补回来：
//
//	① manifest 里 `runtimefragment.SetDependencies.<字段>` 的字段集合
//	   ↔ 装配代码里真正被赋值的 `a.fragDeps.<字段>` 集合，**双向一致**；
//	② 两侧都必须非空 —— 一侧被清空时判据必须报错，不许静默通过（判据失效比判据不全更糟）。
func TestRuntimeFragDepsFieldsMatchWiringManifest(t *testing.T) {
	const prefix = "runtimefragment.SetDependencies."

	manifest := map[string]bool{}
	for _, e := range wiringManifest {
		if strings.HasPrefix(e.Port, prefix) {
			manifest[strings.TrimPrefix(e.Port, prefix)] = true
		}
	}

	assigned := map[string]string{} // 字段名 → 首次出现的文件
	fset := token.NewFileSet()
	for _, f := range []string{"assembly.go", "assembly_publish.go"} {
		parsed, perr := parser.ParseFile(fset, f, nil, 0)
		if perr != nil {
			t.Fatalf("解析 %s 失败: %v", f, perr)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			assign, ok := n.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, lhs := range assign.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				recv, ok := sel.X.(*ast.SelectorExpr)
				if !ok || recv.Sel.Name != "fragDeps" {
					continue
				}
				if _, seen := assigned[sel.Sel.Name]; !seen {
					assigned[sel.Sel.Name] = f
				}
			}
			return true
		})
	}

	if len(manifest) == 0 || len(assigned) == 0 {
		t.Fatalf("判据失效：manifest 侧 %d 条、装配侧 %d 条（两侧都必须非空）", len(manifest), len(assigned))
	}
	var missing, extra []string
	for field := range manifest {
		if _, ok := assigned[field]; !ok {
			missing = append(missing, prefix+field)
		}
	}
	for field, file := range assigned {
		if !manifest[field] {
			extra = append(extra, field+" ("+file+")")
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("runtimefragment 端口注入与 manifest 不一致：\n  manifest 有、装配里未赋值：%v\n  装配里赋了值、manifest 没有：%v",
			missing, extra)
	}
	t.Logf("已核对 %d 个 runtimefragment 端口字段（manifest ↔ a.fragDeps.<字段> 双向一致）", len(manifest))
}
