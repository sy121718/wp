package architecture

// page_slot_exit_test.go — 挂了 PageSlot 的路由，handler 必须走 pageview.Render。
//
// ## 判据
//
// `adminui.PageSlot(tpl)` 的语义是「这条路由的模板名与渲染实现由装配点提供」，
// 于是 handler 里只剩 `pageview.Render(c, view)` 一件事。
//
// 如果一个 handler 既在 PageSlot 上，又自己写 `c.HTML(...)`，那有两种可能：
//
//  1. **半迁移**：路由改了、控制器没改（漏改）。症状是控制器还持有模板名，
//     而 PageSlot 挂的那个模板永远是死的 —— 改动模板名只改一处不会生效。
//  2. **写动作失败重渲**：这条路由同时承载成功（JSON）与失败（HTML 片段）。
//     那种情况**不该挂 PageSlot**（写动作不需要页面出口装配），
//     挂了会让 PageSlot 的语义变得不确定。
//
// 两种都该判失败。第 2 种如果确实需要，正确形态是 `adminui.BareSlot`（片段出口）
// 或控制器显式 `c.HTML(路由常量, ...)` 且**不挂 PageSlot**。
//
// ## 为什么值得单独一条
//
// 我在这轮迁移里踩过一次：`ArticleNewPage` 的路由早已挂好 PageSlot，
// 控制器却还在 `c.HTML("admin/content/article_new.html", ...)`。
// 功能上没坏（c.HTML 自己就能渲染），所以测试全绿 —— 但 PageSlot 是死的，
// 而「模板名归路由层」这个约定已经从中间裂开了。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// pageSlotWiredHandlers 取「挂了 PageSlot 的路由 → 它的 handler 方法名」。
//
// 只认形如 `x.GET("/path", ..., adminui.PageSlot(Tpl), handle.Method)` 的注册：
// PageSlot 与 handler 在**同一次调用**里，且 PageSlot 在前。这是全仓的写法。
func pageSlotWiredHandlers(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, file := range goFilesUnder(t, filepath.Join(root, "internal", "module"), func(p string) bool {
		return strings.Contains(p, string(filepath.Separator)+"inbound"+string(filepath.Separator)+"http"+string(filepath.Separator)) &&
			!strings.HasSuffix(p, "_test.go")
	}) {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			continue
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 3 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "GET", "POST", "PUT", "PATCH", "DELETE", "Any":
			default:
				return true
			}
			hasSlot := false
			handler := ""
			for _, arg := range call.Args {
				inner, ok := arg.(*ast.CallExpr)
				if !ok {
					// handler 一般是最后一个参数：`handle.Method` 或 `pkg.Handle.Method`
					if s, ok := arg.(*ast.SelectorExpr); ok {
						handler = s.Sel.Name
					}
					continue
				}
				if fs, ok := inner.Fun.(*ast.SelectorExpr); ok && fs.Sel.Name == "PageSlot" {
					hasSlot = true
				}
			}
			if hasSlot && handler != "" {
				out[handler] = fset.Position(call.Pos()).String()
			}
			return true
		})
	}
	return out
}

// controllerCallsCHTML 取一个 handler 方法体里是否出现 c.HTML 调用。
func controllerCallsCHTML(t *testing.T, root, method string) []string {
	t.Helper()
	var hits []string
	for _, file := range goFilesUnder(t, filepath.Join(root, "internal", "module"), func(p string) bool {
		return strings.Contains(p, string(filepath.Separator)+"inbound"+string(filepath.Separator)+"http"+string(filepath.Separator)) &&
			!strings.HasSuffix(p, "_test.go")
	}) {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			continue
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			fd, ok := n.(*ast.FuncDecl)
			if !ok || fd.Name.Name != method || fd.Body == nil {
				return true
			}
			ast.Inspect(fd.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				s, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || s.Sel.Name != "HTML" {
					return true
				}
				hits = append(hits, fset.Position(call.Pos()).String())
				return true
			})
			return true
		})
	}
	return hits
}

func TestPageSlotHandlersUseRenderOnly(t *testing.T) {
	root := repoRoot(t)
	wired := pageSlotWiredHandlers(t, root)
	if len(wired) < 50 {
		t.Fatalf("只找到 %d 个挂 PageSlot 的 handler：判据失效（路由写法变了？）", len(wired))
	}
	for method, pos := range wired {
		for _, hit := range controllerCallsCHTML(t, root, method) {
			t.Errorf("%s 挂在 PageSlot 上（%s），但方法体里还有 c.HTML（%s）\n"+
				"  半迁移：模板名归了路由层，控制器却自己渲染 —— PageSlot 挂的那个模板永远不会被用到",
				method, relToRoot(t, root, pos), relToRoot(t, root, hit))
		}
	}
}
