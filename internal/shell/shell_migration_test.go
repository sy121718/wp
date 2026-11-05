package shell

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

const (
	homePkg        = "pkg"
	homeMiddleware = "middleware"
	homeAdmin      = "admin"
	homeProject    = "project"
)

type shellExportHome struct {
	Home   string
	Reason string
}

var shellExportPlan = map[string]shellExportHome{

	"BackPath": {homePkg, "回跳路径归一：纯字符串，无 gin 之外依赖"},
	"BulkIDs":  {homePkg, "批量 ID 旧表单入口：实现已在 pkg/bulkids，这里只剩转发（见别名清单）"},

	"Jump": {homeAdmin, "整页提示的输入形状：页面出口"},

	"RenderJump":   {homeAdmin, "写操作结果出口：成功交统一提示、失败交共享失败出口，不再渲染整页跳转提示"},
	"TranslateFor": {homeAdmin, "Go 侧文案翻译：pkg/view/jet 桥接，页面 handler 不再拼文案后即可撤"},
	"SafeBack":     {homeAdmin, "安全回跳判定的应用侧薄包装：实现归 pkg/safepath"},
}

var shellForwardAllow = map[string]string{
	"TranslateFor": "转发 templates.TranslateFunc：Go 侧拼文案的桥接。页面 handler 不再拼 UI 文案（分页条是最后一处）后即可撤 —— 否则会原地再造一个 shell",

	"BulkIDs": "旧表单入口：读 gin 表单域后立刻交给 pkg/bulkids.Normalize（唯一还需要 gin 的部分；终局删除，新代码直指 pkg）",
}

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
