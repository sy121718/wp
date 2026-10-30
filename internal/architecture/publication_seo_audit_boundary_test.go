package architecture

// publication_seo_audit_boundary_test.go — publication SEO 体检这一域在「MVC 职责分离」上的
// 三条形状判据（AGENTS.md 分层表）。
//
// 与 controller_shape_test.go / text_domain_test.go 的分工：那两份是本批的**台账**
// （存量清单、双向判定）；本文件只钉这一域**已经改对**的形状。台账回答「还剩多少」，
// 这里回答「改完的样子长什么样」—— 不随台账增减移动，回退即红。
//
// 三条判据各自对应一次真实回退：
//
//  1. **成品句回到结论里**：AuditIssue 曾有 Message，装着「图片缺少 alt：xxx」这类
//     中文成品句。它不是数据，是展示层产物 —— 放回 service 等于把「取词在出口」拆了。
//  2. **页面出口在本审计路由上复活**：BareSlot + 片段模板曾让这条端点「HX 走片段、
//     非 HX 走 JSON」。模板删掉后，只要有人把出口加回来就退回双形状，且编译能过。
//  3. **结论事实字段缺失**：冻结契约是 {path,item,level,params}，少一个字段前端
//     就拿不到那一维信息。
//
// 判据是形状不是字面量：第 1、3 条按**具名类型**定位字段（不扫全文件任意 struct），
// 第 2 条只覆盖本审计的 handler 与路由装配点（publication 目录里其它页面槽合法，
// 不得一并禁掉）。Count 恒等式不在本文件：service 的单元用例与 feature 的
// count == len(issues) 已覆盖，此处再用 AST 锁私有 helper 名会把实现钉死。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

const (
	// seoAuditResultFile 冻结的结论事实类型文件（service 层）。
	seoAuditResultFile = "internal/module/publication/service/seo_audit_result.go"
	// seoAuditHandleFile 体检 handler（binding → service → JSON 出口）。
	seoAuditHandleFile = "internal/module/publication/inbound/http/seo_audit_handle.go"
	// seoAuditRouterFile 路由装配点（声明式权限点 + 出口形态）。
	seoAuditRouterFile = "internal/module/publication/inbound/http/publication_router.go"
	// seoAuditRetiredTemplate 已退役的结果片段模板。
	seoAuditRetiredTemplate = "internal/templates/admin/analytics/seo_audit_result.html"
)

// seoAuditBannedFacts 结论类型里不得出现的字段名 → 理由。
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

// TestSEOAuditResultTypesCarryFactsOnly 按具名类型定位字段：
// AuditIssue / AuditIssueParams 只准有语言无关的事实字段。
//
// 只查这两个类型 —— 同包其它结构（请求 DTO、内部中间结构）不在本判据范围内，
// 扫「文件里所有 struct」会把无关字段也算成违规。
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

	// 反向判定：登记的四个事实字段必须仍在 AuditIssue 上。
	// 字段被全删时上面那条会「全绿」，而端点其实已无结论可交 —— 那种绿是装置坏了。
	want := []string{"Path", "Item", "Level", "Params"}
	got := seoAuditFieldNames(targets["AuditIssue"])
	for _, name := range want {
		if !containsString(got, name) {
			t.Errorf("%s: AuditIssue 缺字段 %s —— 冻结契约是 {path,item,level,params}（现存 %v）",
				seoAuditResultFile, name, got)
		}
	}
}

// TestSEOAuditHasNoPageExit 本审计的 handler 与路由装配点不得有页面出口。
//
// 范围刻意收窄到这两个文件：publication 模块里其它路由挂 adminui 页面槽是合法的
// （它们本来就是页面），只有「审计这条 API 又回片段」是违规。
//
// 三件事一起判，因为它们是一套装置的三个零件：
//
//	片段模板文件（渲染体）+ 路由上挂的槽（出口）+ handler 对页面包的 import（通路）
func TestSEOAuditHasNoPageExit(t *testing.T) {
	root := repoRoot(t)

	// 1) 退役的片段模板不得复活。
	retired := filepath.Join(root, seoAuditRetiredTemplate)
	if _, err := os.Stat(retired); err == nil {
		t.Errorf("%s 仍然存在 —— 审计只回事实 JSON，结果区由页面预置结构 + 专用 JS 填值；"+
			"片段模板复活就等于「同一端点两种形状」回来了", seoAuditRetiredTemplate)
	} else if !os.IsNotExist(err) {
		t.Errorf("检查 %s 失败（不是「不存在」而是别的错误，判据不可信）: %v", seoAuditRetiredTemplate, err)
	}

	// 2) handler 不得 import 页面渲染出口；且必须走 binding + response。
	handler := mustParseFile(t, filepath.Join(root, seoAuditHandleFile), seoAuditHandleFile)
	for _, spec := range handler.Imports {
		switch spec.Path.Value {
		case `"go_wp/internal/adminui"`:
			t.Errorf("%s: import 了后台渲染出口 —— handler 只做 binding → 调用 → JSON 出口", seoAuditHandleFile)
		case `"go_wp/pkg/view"`:
			t.Errorf("%s: import 了视图结果接口 —— 审计回数据，不回视图", seoAuditHandleFile)
		}
	}
	for path, why := range map[string]string{
		`"go_wp/pkg/binding"`:  "绑定唯一入口",
		`"go_wp/pkg/response"`: "JSON 出口",
	} {
		if !hasImport(handler, path) {
			t.Errorf("%s: 没有 import %s（%s）—— 缺了它，绑定或出口会退回手写形态",
				seoAuditHandleFile, path, why)
		}
	}

	// 按路由参数定位完整调用，避免多行注册被文本截断；缺失注册同样报错。
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

// mustParseFile 解析单个 Go 文件，失败即终止。
func mustParseFile(t *testing.T, absPath, relPath string) *ast.File {
	t.Helper()

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, absPath, nil, 0)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", relPath, err)
	}
	return parsed
}

// seoAuditFieldNames 取结构体的字段名（跳过内嵌无名字段）。
//
// 不叫 fieldNames：model_gorm_tag_test.go 里已有同名不同签名的助手，同包会冲突。
func seoAuditFieldNames(st *ast.StructType) []string {
	var out []string
	for _, f := range st.Fields.List {
		for _, ident := range f.Names {
			out = append(out, ident.Name)
		}
	}
	return out
}

// hasImport 文件是否 import 了给定路径（含引号的字面量形态）。
func hasImport(file *ast.File, quotedPath string) bool {
	for _, spec := range file.Imports {
		if spec.Path != nil && spec.Path.Value == quotedPath {
			return true
		}
	}
	return false
}

// containsString 字符串切片是否含给定值。
func containsString(list []string, want string) bool {
	for _, got := range list {
		if got == want {
			return true
		}
	}
	return false
}
