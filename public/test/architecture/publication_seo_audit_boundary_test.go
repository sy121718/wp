package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

const (
	seoAuditResultFile = "internal/module/publication/service/seo_audit_result.go"

	seoAuditHandleFile = "internal/module/publication/inbound/http/seo_audit_handle.go"

	seoAuditRouterFile = "internal/module/publication/inbound/http/publication_router.go"

)

var seoAuditBannedFacts = map[string]string{
	"Message":     "成品句（曾装「图片缺少 alt：xxx」这类中文句子）",
	"Text":        "展示文本",
	"Label":       "展示标签",
	"Tone":        "配色分档",
	"Badge":       "徽标文案",
	"ErrMsg":      "已取词的错误句",
	"DisplayText": "展示文本",
	"DisplayName": "展示名",
	"SummaryText": "汇总句（应由模板从 scanned/count 取词拼出）",
}

func TestSEOAuditResultTypesCarryFactsOnly(t *testing.T) {
	root := repoRoot(t)
	file := filepath.Join(root, seoAuditResultFile)
	parsed := mustParseFile(t, file, seoAuditResultFile)

	targets := map[string]*ast.StructType{}
	ast.Inspect(parsed, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}
		if ts.Name.Name != "AuditIssue" && ts.Name.Name != "AuditIssueParams" {
			return true
		}
		if st, ok := ts.Type.(*ast.StructType); ok {
			targets[ts.Name.Name] = st
		}
		return true
	})

	for _, name := range []string{"AuditIssue", "AuditIssueParams"} {
		st, ok := targets[name]
		if !ok {
			t.Fatalf("%s: 找不到类型 %s —— 结论事实类型必须留在 service 且可被解析", seoAuditResultFile, name)
		}
		for _, f := range st.Fields.List {
			for _, ident := range f.Names {
				if reason, bad := seoAuditBannedFacts[ident.Name]; bad {
					t.Errorf("%s: %s 里出现字段 %s —— %s。结论只交语言无关的事实，"+
						"文案由词条在模板取、参数由前端按需填充", seoAuditResultFile, name, ident.Name, reason)
				}
			}
		}
	}

	want := []string{"Path", "Item", "Level", "Params"}
	got := seoAuditFieldNames(targets["AuditIssue"])
	for _, name := range want {
		if !containsString(got, name) {
			t.Errorf("%s: AuditIssue 缺字段 %s —— 冻结契约是 {path,item,level,params}（现存 %v）",
				seoAuditResultFile, name, got)
		}
	}
}

// TestSEOAuditHasNoPageExit 约束体检端点的**出口形状**：
//   - 它是普通界面写入口：标准表单 + Jet 片段，结果区由发起页面声明；
//   - 不允许挂整页槽（那不是局部更新），也不允许走已清退的旧渲染包；
//   - 结果类型只交事实（见 TestSEOAuditResultTypesCarryFactsOnly）。
func TestSEOAuditHasNoPageExit(t *testing.T) {
	root := repoRoot(t)

	handler := mustParseFile(t, filepath.Join(root, seoAuditHandleFile), seoAuditHandleFile)
	for _, spec := range handler.Imports {
		switch spec.Path.Value {
		case `"go_wp/internal/adminui"`:
			// 该包已清退；这条保留为终局断言：任何代码都不得再引用旧渲染出口。
			t.Errorf("%s: import 了已清退的后台渲染包", seoAuditHandleFile)
		}
	}
	for path, why := range map[string]string{
		`"go_wp/pkg/view"`: "片段出口",
	} {
		if !hasImport(handler, path) {
			t.Errorf("%s: 没有 import %s（%s）", seoAuditHandleFile, path, why)
		}
	}

	router := mustParseFile(t, filepath.Join(root, seoAuditRouterFile), seoAuditRouterFile)
	count, pageExit := seoAuditRouteShape(router)
	if count != 1 {
		t.Errorf("%s: POST /seo-audit 注册数为 %d，预期恰好一个", seoAuditRouterFile, count)
	}
	if pageExit {
		t.Errorf("%s: 体检路由挂了页面槽，审计应统一返回事实 JSON", seoAuditRouterFile)
	}
}

func seoAuditRouteShape(file *ast.File) (count int, pageExit bool) {
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		method, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || method.Sel.Name != "POST" {
			return true
		}
		path, ok := call.Args[0].(*ast.BasicLit)
		if !ok || path.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(path.Value)
		if err != nil || value != "/seo-audit" {
			return true
		}
		count++
		ast.Inspect(call, func(child ast.Node) bool {
			selector, ok := child.(*ast.SelectorExpr)
			if ok && (selector.Sel.Name == "BareSlot" || selector.Sel.Name == "PageSlot") {
				pageExit = true
			}
			return true
		})
		return false
	})
	return count, pageExit
}

func TestSEOAuditRouteShape(t *testing.T) {
	for _, tt := range []struct {
		name     string
		body     string
		count    int
		pageExit bool
	}{
		{name: "missing"},
		{name: "json", body: `g.POST("/seo-audit", audit())`, count: 1},
		{name: "other_page", body: `g.POST("/other", ui.BareSlot(tpl)); g.POST("/seo-audit", audit())`, count: 1},
		{name: "duplicate", body: `g.POST("/seo-audit", audit()); g.POST("/seo-audit", audit())`, count: 2},
		{name: "multiline_slot", body: "g.POST(\"/seo-audit\",\n guard(),\n ui.BareSlot(tpl),\n audit(),\n)", count: 1, pageExit: true},
		{name: "page_slot", body: `routes.POST("/seo-audit", ui.PageSlot(tpl), audit())`, count: 1, pageExit: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", "package fixture\nfunc routes() {\n"+tt.body+"\n}", 0)
			if err != nil {
				t.Fatal(err)
			}
			count, pageExit := seoAuditRouteShape(file)
			if count != tt.count || pageExit != tt.pageExit {
				t.Fatalf("route shape = (%d, %t), want (%d, %t)", count, pageExit, tt.count, tt.pageExit)
			}
		})
	}
}

func mustParseFile(t *testing.T, absPath, relPath string) *ast.File {
	t.Helper()

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, absPath, nil, 0)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", relPath, err)
	}
	return parsed
}

func seoAuditFieldNames(st *ast.StructType) []string {
	var out []string
	for _, f := range st.Fields.List {
		for _, ident := range f.Names {
			out = append(out, ident.Name)
		}
	}
	return out
}

func hasImport(file *ast.File, quotedPath string) bool {
	for _, spec := range file.Imports {
		if spec.Path != nil && spec.Path.Value == quotedPath {
			return true
		}
	}
	return false
}

func containsString(list []string, want string) bool {
	for _, got := range list {
		if got == want {
			return true
		}
	}
	return false
}
