package shell

// shell_migration_test.go — shell 包「终局删除」的结构门禁（task-7 盘点 / task-12 落地）。
//
// 背景：internal/shell 的终局是**删除**，责任归 admin 模块（后台外壳与页面出口）、
// middleware（中间件与请求上下文）、pkg（通用工具）、project 模块（站点 URL 那两条）。
// scripts/check-shell-migration.sh 守的是**数量**（按 scope 的调用点计数只许下降）；
// 本文件守的是**结构**，两件事脚本看不见：
//
//   1. 每新导出一个符号，都等于给这条迁移路线加一个岔口 —— 脚本只数调用点，
//      看不见「多了一个导出符号」。所以这里要求**每个导出符号都登记终局归属**：
//      新符号没登记 → 红；登记了但代码里已经没了（搬走了没删条目）→ 也红。
//      两个方向都红，包面才是单调收敛的。
//   2. 迁移期最容易发生的退化是「长期别名」：把实现搬到 pkg 之后在原地留一层
//      `type X = pkg.X` / 一行转发函数，所有调用点都不用改、迁移看上去"完成"了，
//      而 shell 永远删不掉。这里把别名与转发固定成一份**只减不增**的清单。
//
// 为什么不做「引用全文快照」：1371 个引用抄进测试，下一次改一行动模板就要重写它，
// 这份门禁会在两周内被绕过 —— 而被绕过的门禁比没有门禁更坏，它让人以为守住了。
// 数量归脚本（scope 粒度），结构归这里（符号粒度），两张表合起来才够小、够稳。
//
// 判据是**形状**而不是文件/目录名：导出与否看首字母、别名看 `=` 的类型别名语法、
// 转发看「函数体只有一个 return + 被调方带包名前缀」。目录与文件名都会漂移，形状不会。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// 终局归属：shell 的内容将来分别落在哪里。
const (
	homePkg        = "pkg"        // 通用工具：不依赖 templates、不依赖业务模块契约
	homeMiddleware = "middleware" // 中间件与请求上下文（含页面鉴权的判定侧）
	homeAdmin      = "admin"      // 后台外壳与页面出口（侧栏、i18n 注入、整页提示）
	homeProject    = "project"    // 站点 URL 相关：本就是 project 模块的知识
)

type shellExportHome struct {
	Home   string
	Reason string
}

// shellExportPlan 是 shell 包**全部**包级导出符号的终局归属。
//
// 新增导出符号必须在这里登记 —— 登记这个动作就是「想清楚它归谁」本身；
// 符号被搬走后必须删掉对应条目（TestShellExportsHaveTerminalHome 两个方向都判）。
var shellExportPlan = map[string]shellExportHome{
	// ── 通用工具（纯字符串 / 请求解析）──
	//
	// 已收口的一批（纯请求参数，task-96）：FieldValue / PageParams / ParseUint /
	// ParseStatus / ParseStatusPtr / FilterBaseURL 六项整批迁入 `pkg/reqparams`
	// （含各自边界用例）—— 它们与后台页面无关，留在壳层等于把「解析一个 ?page=」
	// 绑上整个后台外壳的依赖面。156 处调用点改为直引 pkg/reqparams，条目同批删除。
	// JsonSafe 已迁 pkg/utils：mail 两处、workbench 九处生产消费者直引，原实现删除，
	// 不留别名或转发；TestJsonSafeStaysOutsideShell 永久守住旧入口归零。
	"BackPath": {homePkg, "回跳路径归一：纯字符串，无 gin 之外依赖"},
	"BulkIDs":  {homePkg, "批量 ID 旧表单入口：实现已在 pkg/bulkids，这里只剩转发（见别名清单）"},
	// BulkIDsFacingText 已于 task-151 迁 internal/adminui/bulkids.go（出口要 i18n 与页面归口文案，
	// 终局归属就是 adminui，不是 pkg）：18 处调用点同批改名，条目删除。

	// ── 中间件与请求上下文 ──
	// 权限上下文的**装配**与导航树**读取器**已随 task-92 批 3 离开本包：中间件归
	// internal/middleware/adminctx（写入面 + 一处有意的 fail-closed 修复），
	// NavTree 读取器零调用后删除；三个键名也已归 adminctx，写入与读取共用唯一契约。
	// PageAuthz / PageAuthzAs 已于 task-92 批 4a 迁 internal/adminui（internal/adminui/authz.go）：
	// 路由侧唯一约定写法现在是 `adminui.PageAuthz(...)`，20 个模块 router 文件的 73 处调用点同批改名。

	// ── 后台外壳与页面出口（归 admin 模块的 inbound）──
	"Jump": {homeAdmin, "整页提示的输入形状：页面出口"},
	// RejectPageRequest 同批迁 internal/adminui：整页拒绝出口的实现自 task-92 批 2b 起就在
	// adminui.Reject，本包只剩一行薄委托（adminui.Reject(adminui.Prepare)），批 4a 一并删除。
	"RenderJump":   {homeAdmin, "整页提示渲染：已直接调用 adminui.Prepare，终局归 internal/adminui"},
	"TranslateFor": {homeAdmin, "Go 侧文案翻译：templates 桥接，页面 handler 不再拼文案后即可撤"},

	// ── 回迁业务模块 ──
}

// 允许存在的「转发/别名」清单（只减不增）。每一条都必须写明理由 + 终局。
//
// 为什么单列而不是直接判「不许有」：先抽实现到 pkg、旧入口留在壳层，是对既有调用点
// 分批改名的过渡；一次性改名会把迁移的 diff 淹没在无关改动里。所以允许暂存，但数量
// 只能往下走。
//
// 已收口的一批（分页）：pkg/pagination 的三个别名/转发（PageLink / PaginationData /
// BuildPagination）已随 `internal/shell/pagination.go` 的删除消失 —— 41 个调用点改为
// 直引 pkg/pagination，三条条目在同一批里从本清单与导出计划中删掉。搬完即删是本门禁的
// 方向：清单长度就是「还剩几处假完成」。
//
// 已收口的一批（导航视图，task-72）：`internal/shell/nav.go` 整体迁入
// `internal/adminui/nav.go` —— BuildNav / NavGroup / NavNode 三项从导出计划里删除，
// 且**本清单一条都不增**：包外消费者为 0（调用点只有 shell.Prepare、hasActiveDirGroup
// 与两个包外测试文件），没有需要过渡的第三方向，所以不留别名也不留转发。
// 上下文键名现归 adminctx；侧栏态 cookie 规则归 adminui，壳层均不留副本。
//
// 已收口的一批（语言回跳判据，task-75）：LangRedirectPath / LocalReturnPath 两项迁入
// `pkg/safepath`（判据此前已在那里，只剩这两个带长度口径的入口留在壳层）—— 消费者
// （admin 的 adminSafeLangRedirect、admin_notice_test、workbench 的 returnUrl、block 的
// 回跳）改为直引 safepath，本清单同样**一条不增**。LangRedirect 的生产消费者已归零，
// 请求取值由 adminui.Prepare 的私有 langRedirect 承担；旧壳与测试已随 task-134 删除。
// 会话身份读取器也已随 task-133 迁入 builtin，原定义与全部消费者同步迁移，无兼容转发。
//
// 清单为什么会长：**「先抽实现到 pkg、旧入口留在壳层」是迁移的分批方式**（lead 口径：
// shell.BulkIDs 旧入口转发，新代码直指 pkg）。每长一条都对应一次分批，因此每条必须写清
// 这一批的边界与删除条件；调用点归零后条目必须删掉，否则 TestNoUnplannedForwarding 会以
// 「已经不存在了」报红 —— 清单只减不增说的就是这个方向。
var shellForwardAllow = map[string]string{
	"TranslateFor": "转发 templates.TranslateFunc：Go 侧拼文案的桥接。页面 handler 不再拼 UI 文案（分页条是最后一处）后即可撤 —— 否则会原地再造一个 shell",

	// ── pkg/bulkids 抽取（task-10 起；task-55 已删四个纯别名）──
	// 为什么 BulkIDs 这一条还在：它是唯一还需要 gin 的入口（读表单域 ids 后立刻交给
	// pkg/bulkids.Normalize）。旧表单写路径随各自的 JSON 迁移逐批下线后才谈删除 ——
	// 删除条件：本包这个导出在代码里归零（届时本行与 shellExportPlan 对应行一起删）。
	// 四个纯别名（MaxBulkIDs / ErrBulkIDsTooMany / BulkIDsError / MsgBulkIDsTooMany）已在
	// task-55 删除：同值映射保住了旧调用方的 errors.Is / errors.As，但调用方全部直引
	// pkg/bulkids 之后，它只剩「第二份真值」的维护成本，没有任何收益。
	"BulkIDs": "旧表单入口：读 gin 表单域后立刻交给 pkg/bulkids.Normalize（唯一还需要 gin 的部分；终局删除，新代码直指 pkg）",

	// Prepare 过渡转发已随 order 五个生产文件六处调用直引 adminui.Prepare 收口；
	// prepare_parity_test.go 同批删除，现有 Prepare 契约与真 Jet 渲染测试继续守行为。

	// ── 页面出口与受控文本迁 internal/adminui（task-92 批 2b-1；task-114 已收口）──
	// 这里原先有一条 PageInternalText 过渡转发。它连同 shell/errors.go 的 PageError、
	// MsgInternalError、errors_parity_test.go 一起在 task-114 删除：8 处消费者
	//（pages_handle.go 4 处、coupon_page.go 2 处、workbench_handle.go 的 JSON 503、order 侧 3 处
	// 已由 task-119 先走）全部改成直引 internal/adminui 之后，壳层不再有任何页面出口。
	// 本清单的长度就是「还剩几处假完成」—— 这一批是负数，不是静止。

	// ── pkg/backpath 抽取（task-22 M2；task-51 已收口）──
	// 为什么 BackPath 本身**不在**这张清单里：它不是单行转发 —— 取查询（c.Request.URL.Query()）、
	// 非法路径的 Warn 与控制面首页 `/admin` 兜底、值被丢弃时的 Warn 都还在本包（`/admin` 是
	// 控制面概念，pkg 不替调用方选落点）。
	// 这里原先唯一的纯转发 WithParams 已在 task-51 删除：M3 实际迁移 = task-41 5 处 + task-47
	// 7 处 = 12 处（旧登记写「共 22 处」，与实况不符，按全仓引用清零为准）；删除时包外消费者 0
	//（生产与包外测试都无引用，别名导入/点导入/函数值引用皆无），包内 TestWithParams 同批删除，
	// 拼参语义由 pkg/backpath 侧测试覆盖。
}

// shellTestFiles 解析本包的非测试 .go 文件，返回按文件名分组的语法树。
func shellTestFiles(t *testing.T) map[string]*ast.File {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("读取包目录失败: %v", err)
	}
	fset := token.NewFileSet()
	out := map[string]*ast.File{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if perr != nil {
			t.Fatalf("解析 %s 失败: %v", name, perr)
		}
		out[name] = f
	}
	if len(out) == 0 {
		t.Fatal("没有解析到任何 shell 源文件 —— 判据已空转")
	}
	return out
}

// exportedSymbols 列出包级导出符号（不含方法：方法的可见性由类型的归属决定）。
func exportedSymbols(files map[string]*ast.File) map[string]string {
	out := map[string]string{}
	for name, f := range files {
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv != nil || !ast.IsExported(d.Name.Name) {
					continue
				}
				out[d.Name.Name] = name
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if ast.IsExported(s.Name.Name) {
							out[s.Name.Name] = name
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if ast.IsExported(n.Name) {
								out[n.Name] = name
							}
						}
					}
				}
			}
		}
	}
	return out
}

// TestShellExportsHaveTerminalHome 两个方向都判：
//   - 代码里有、计划里没有 → 新增导出符号必须先登记终局归属；
//   - 计划里有、代码里没有 → 符号已搬走，条目必须跟着删（否则计划表会变成僵尸清单）。
func TestShellExportsHaveTerminalHome(t *testing.T) {
	files := shellTestFiles(t)
	actual := exportedSymbols(files)

	var unregistered []string
	for name, file := range actual {
		entry, ok := shellExportPlan[name]
		if !ok {
			unregistered = append(unregistered, name+" ("+file+")")
			continue
		}
		switch entry.Home {
		case homePkg, homeMiddleware, homeAdmin, homeProject:
		default:
			t.Errorf("shellExportPlan[%s].Home=%q 不是已知归属（%s/%s/%s/%s）",
				name, entry.Home, homePkg, homeMiddleware, homeAdmin, homeProject)
		}
		if strings.TrimSpace(entry.Reason) == "" {
			t.Errorf("shellExportPlan[%s] 没写理由 —— 没有理由的归属等于把判据藏起来", name)
		}
	}
	if len(unregistered) > 0 {
		sort.Strings(unregistered)
		t.Errorf("有 %d 个导出符号没登记终局归属：\n  %s\n"+
			"新增导出等于给 shell 迁移加一个岔口。请在 shellExportPlan 里写清它归谁（pkg/middleware/admin/project）"+
			"并把实现直接写在归属地；确实要留在 shell 过度的，理由里写明终局。",
			len(unregistered), strings.Join(unregistered, "\n  "))
	}

	var stale []string
	for name := range shellExportPlan {
		if _, ok := actual[name]; !ok {
			stale = append(stale, name)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("shellExportPlan 里有 %d 条已经不在代码里（搬走了没删条目）：%s\n"+
			"僵尸计划表会让下一个人以为还有这么多东西要搬。",
			len(stale), strings.Join(stale, ", "))
	}
}

// TestJsonSafeStaysOutsideShell guards retired helpers, including aliases and function values.
// JsonSafe, identity readers and LangRedirect must stay at their final owners.
func TestJsonSafeStaysOutsideShell(t *testing.T) {
	retired := map[string]bool{"JsonSafe": true, "CurrentUserID": true, "CurrentUserIDText": true, "LangRedirect": true, "IsHXRequest": true}
	for name, file := range exportedSymbols(shellTestFiles(t)) {
		if retired[name] {
			t.Errorf("retired export %s revived in shell: %s", name, file)
		}
	}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".scratch", "node_modules", "vendor", ".review-graph", ".code-review-graph":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		aliases := map[string]bool{}
		dotImport := false
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) != "go_wp/internal/shell" {
				continue
			}
			name := "shell"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			aliases[name] = true
			dotImport = dotImport || name == "."
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.SelectorExpr:
				if pkg, ok := n.X.(*ast.Ident); ok && aliases[pkg.Name] && retired[n.Sel.Name] {
					t.Errorf("old %s reference in %s", n.Sel.Name, path)
				}
			case *ast.Ident:
				if dotImport && retired[n.Name] {
					t.Errorf("dot-imported %s reference in %s", n.Name, path)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// internalImportNames 返回该文件的「本地包名 → import 路径」，且只看本仓内部的包
// （`go_wp/...` 且不是 internal/shell 自己）。
//
// 为什么必须解析 import 而不是看包名：`return strings.TrimSpace(...)` 与
// `return bulkids.Normalize(...)` 的形状完全一样，区别只在被调方是不是**本仓
// 另一个包**。把 stdlib 也当成转发，判据就会对着一堆正常的取值函数报违规（实测 5 条里
// 4 条是 `strings.`/`strconv.` 的假阳性）—— 而假阳性会让人直接放宽门禁。
func internalImportNames(f *ast.File) map[string]string {
	out := map[string]string{}
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if !strings.HasPrefix(path, "go_wp/") || strings.HasPrefix(path, "go_wp/internal/shell") {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil && imp.Name.Name != "" {
			name = imp.Name.Name
		}
		out[name] = path
	}
	return out
}

// TestNoUnplannedForwarding 抓三类「迁移假完成」的形状：跨包类型别名、跨包常量/变量同值映射、
// 单行转发函数。
//
// 它们让上千个调用点一个都不用改，看起来迁移做完了，而 shell 永远删不掉。
// 允许暂存的都登记在 shellForwardAllow（只减不增）：pkg/pagination 的历史抽取、
// 页面鉴权与站点 URL 的过渡转发、pkg/bulkids 这批的同值映射与旧表单入口、
// pkg/backpath 的拼参转发（BackPath 本身不是转发：gin 取查询/日志/兜底仍在壳层）。
func TestNoUnplannedForwarding(t *testing.T) {
	files := shellTestFiles(t)
	found := map[string]string{}

	for name, f := range files {
		internal := internalImportNames(f)
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if !s.Assign.IsValid() || !ast.IsExported(s.Name.Name) {
							continue
						}
						sel, ok := s.Type.(*ast.SelectorExpr)
						if !ok {
							continue
						}
						if pkg, ok := sel.X.(*ast.Ident); ok {
							if target, internal := internal[pkg.Name]; internal {
								found[s.Name.Name] = "类型别名指向 " + target + "（" + name + "）"
							}
						}
					case *ast.ValueSpec:
						// 常量/变量的**同值映射**（`const Foo = pkg.Bar`）与类型别名同害：
						// 调用点一个都不用改、迁移看上去完成了，而壳层永远删不掉。只判导出的
						// （未导出的映射是包内实现细节，随文件一起走）。
						for i, n := range s.Names {
							if !ast.IsExported(n.Name) || i >= len(s.Values) {
								continue
							}
							sel, ok := s.Values[i].(*ast.SelectorExpr)
							if !ok {
								continue
							}
							pkg, ok := sel.X.(*ast.Ident)
							if !ok {
								continue
							}
							if target, internal := internal[pkg.Name]; internal {
								found[n.Name] = "常量/变量同值映射 " + target + "." + sel.Sel.Name + "（" + name + "）"
							}
						}
					}
				}
			case *ast.FuncDecl:
				if d.Recv != nil || !ast.IsExported(d.Name.Name) || d.Body == nil || len(d.Body.List) != 1 {
					continue
				}
				ret, ok := d.Body.List[0].(*ast.ReturnStmt)
				if !ok || len(ret.Results) != 1 {
					continue
				}
				call, ok := ret.Results[0].(*ast.CallExpr)
				if !ok {
					continue
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					continue
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok {
					continue
				}
				target, internal := internal[pkg.Name]
				if !internal {
					continue
				}
				found[d.Name.Name] = "单行转发到 " + target + "." + sel.Sel.Name + "（" + name + "）"
			}
		}
	}

	var unplanned []string
	for name, shape := range found {
		if _, ok := shellForwardAllow[name]; !ok {
			unplanned = append(unplanned, name+": "+shape)
		}
	}
	if len(unplanned) > 0 {
		sort.Strings(unplanned)
		t.Errorf("发现 %d 处未登记的别名/转发：\n  %s\n"+
			"兼容层一旦留成终态，shell 就永远删不掉。请把调用点改到归属包；"+
			"确需暂存的写进 shellForwardAllow 并写明终局。",
			len(unplanned), strings.Join(unplanned, "\n  "))
	}

	var stale []string
	for name := range shellForwardAllow {
		if _, ok := found[name]; !ok {
			stale = append(stale, name)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("shellForwardAllow 里的 %s 已经不存在了 —— 说明那一处已经改干净，条目要删掉"+
			"（这份清单只减不增）。", strings.Join(stale, ", "))
	}
}

// TestPrepareMigrationStaysClosed guards the completed migration independently of the plan.
func TestPrepareMigrationStaysClosed(t *testing.T) {
	if file, ok := exportedSymbols(shellTestFiles(t))["Prepare"]; ok {
		t.Errorf("shell.Prepare export returned in %s", file)
	}
	root := "../module/order"
	files := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files++
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			importPath, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if importPath != "go_wp/internal/shell" {
				continue
			}
			name := "shell"
			if imp.Name != nil {
				name = imp.Name.Name
			}
			if name == "." {
				t.Errorf("%s: shell dot import hides the Prepare migration boundary", path)
			}
			ast.Inspect(f, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "Prepare" {
					return true
				}
				if receiver, ok := selector.X.(*ast.Ident); ok && receiver.Name == name {
					t.Errorf("%s: order must use adminui.Prepare directly", path)
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("no order production files scanned")
	}
}
