package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var pendingControllerNormalize = map[string]bool{
	"internal/module/admin/inbound/http/admin_i18n_write.go": true,
	// admin_page.go 的 bulkids.Normalize 只做「请求形状」的上限判断（整批拒绝一句提示），
	// 不参与业务 id 归一去重 —— 后者仍在 service 的 checkBulkDeleteLimit。
	"internal/module/admin/inbound/http/admin_page.go":                     true,
	"internal/module/block/inbound/http/block_handle.go":                   true,
	"internal/module/inventory/inbound/http/inventory_reason_handle.go":    true,
	"internal/module/purchase/inbound/http/source_page_handle.go":          true,
	"internal/module/inventory/inbound/http/inventory_warehouse_handle.go": true,
	"internal/module/mail/inbound/http/mail_contact_write.go":              true,
}

var pendingControllerFiles = map[string]bool{
	"internal/module/admin/inbound/http/admin_page.go":                     true,
	"internal/module/admin/inbound/http/admin_err.go":                      true,
	"internal/module/admin/inbound/http/admin_jump.go":                     true,
	"internal/module/ai/inbound/http/ai_err.go":                            true,
	"internal/module/admin/inbound/http/admin_i18n_write.go":               true,
	"internal/module/ai/inbound/http/ai_page.go":                           true,
	"internal/module/analytics/inbound/http/analytics_page.go":             true,
	"internal/module/block/inbound/http/block_err.go":                      true,
	"internal/module/block/inbound/http/block_page.go":                     true,
	"internal/module/comment/inbound/http/comment_page.go":                 true,
	"internal/module/content/inbound/http/content_page.go":                 true,
	"internal/module/contenttemplate/inbound/http/content_template_err.go": true,
	"internal/module/contenttemplate/inbound/http/contenttemplate_page.go": true,
	"internal/module/mail/inbound/http/mail_contact_write.go":              true,
	"internal/module/mail/inbound/http/mail_err.go":                        true,
	"internal/module/mail/inbound/http/mail_jump.go":                       true,
	"internal/module/masterdata/inbound/http/masterdata_page.go":           true,
	"internal/module/membership/inbound/http/membership_page.go":           true,
	"internal/module/order/inbound/http/coupon_page.go":                    true,
	"internal/module/order/inbound/http/coupon_write.go":                   true,
	"internal/module/order/inbound/http/order_bulk_write.go":               true,
	"internal/module/order/inbound/http/order_create_page.go":              true,
	"internal/module/order/inbound/http/order_list_page.go":                true,
	"internal/module/order/inbound/http/order_shared.go":                   true,
	"internal/module/order/inbound/http/return_bulk_write.go":              true,
	"internal/module/order/inbound/http/return_page.go":                    true,
	"internal/module/order/inbound/http/sales_page.go":                     true,
	"internal/module/page/inbound/http/page_jump.go":                       true,
	"internal/module/page/inbound/http/page_redirect.go":                   true,
	"internal/module/page/inbound/http/page_stale.go":                      true,
	"internal/module/plugin/inbound/http/plugin_err.go":                    true,
	"internal/module/project/inbound/http/project_err.go":                  true,
	"internal/module/project/inbound/http/project_jump.go":                 true,
	"internal/module/project/inbound/http/project_page.go":                 true,
	"internal/module/sysconfig/inbound/http/sysconfig_admin_pages.go":      true,
	"internal/module/user/inbound/http/customer_bulk_write.go":             true,
	"internal/module/user/inbound/http/user_page.go":                       true,
	"internal/module/workbench/inbound/http/workbench_err.go":              true,

	"internal/module/workbench/inbound/http/workbench_page.go": true,
}

// pendingControllerBinding 是「仍用手工逐字段读表单」的存量文件（棘轮：只减不增）。
//
// 规范的入口是**原生显式绑定**：ShouldBindWith(..., binding.FormPost)、
// ShouldBindJSON、ShouldBindQuery，或 pkg/binding 的 ParseForm/ParseJSON/Query。
// 不再新增 c.PostForm / c.Query 这类手工逐字段读取，也不新增同义绑定包装。
var pendingControllerBinding = map[string]bool{
	"internal/module/admin/inbound/http/admin_page.go":     true,
	"internal/module/ai/inbound/http/ai_page.go":           true,
}

var pendingControllerQuery = map[string]bool{
	"internal/module/admin/inbound/http/admin_page.go":                     true,
	"internal/module/ai/inbound/http/ai_handle.go":                         true,
	"internal/module/ai/inbound/http/ai_page.go":                           true,
	"internal/module/analytics/inbound/http/analytics_page.go":             true,
	"internal/module/artifact/inbound/http/artifact_router.go":             true,
	"internal/module/block/inbound/http/block_handle.go":                   true,
	"internal/module/block/inbound/http/block_page.go":                     true,
	"internal/module/build/inbound/http/build_handle.go":                   true,
	"internal/module/cart/inbound/http/cart_router.go":                     true,
	"internal/module/comment/inbound/http/comment_page.go":                 true,
	"internal/module/content/inbound/http/content_page.go":                 true,
	"internal/module/contenttemplate/inbound/http/contenttemplate_page.go": true,
	"internal/module/inventory/inbound/http/inventory_page_handle.go":      true,
	"internal/module/inventory/inbound/http/inventory_reason_handle.go":    true,
	"internal/module/inventory/inbound/http/inventory_warehouse_handle.go": true,
	"internal/module/mail/inbound/http/mail_err.go":                        true,
	"internal/module/mail/inbound/http/mail_handle.go":                     true,
	"internal/module/masterdata/inbound/http/masterdata_page.go":           true,
	"internal/module/media/inbound/http/media_handle.go":                   true,
	"internal/module/membership/inbound/http/membership_page.go":           true,
	"internal/module/navigation/inbound/http/navigation_page_handle.go":    true,
	"internal/module/order/inbound/http/coupon_page.go":                    true,
	"internal/module/order/inbound/http/order_create_page.go":              true,
	"internal/module/order/inbound/http/order_handle.go":                   true,
	"internal/module/order/inbound/http/order_list_page.go":                true,
	"internal/module/order/inbound/http/order_shared.go":                   true,
	"internal/module/order/inbound/http/return_page.go":                    true,
	"internal/module/order/inbound/http/sales_page.go":                     true,
	"internal/module/page/inbound/http/page_redirect.go":                   true,
	"internal/module/page/inbound/http/page_translations_handle.go":        true,
	"internal/module/project/inbound/http/project_handle.go":               true,
	"internal/module/project/inbound/http/project_page.go":                 true,
	"internal/module/user/inbound/http/user_customer_admin_handle.go":      true,
	"internal/module/user/inbound/http/user_page.go":                       true,
	"internal/module/webhook/inbound/http/webhook_handle.go":               true,

	"internal/module/workbench/inbound/http/workbench_page.go":        true,
	"internal/module/workbench/inbound/http/canvas_handle.go":         true,
	"internal/module/workbench/inbound/http/canvas_preview_handle.go": true,
	"internal/module/workbench/inbound/http/canvas_bridge_handle.go":  true,
}

var pendingControllerMethods = map[string]bool{
	"internal/module/admin/inbound/http/admin_pages_router.go:SetupAdminPages":                  true,
	"internal/module/admin/inbound/http/admin_router.go:SetupAdminRoutes":                       true,
	"internal/module/ai/inbound/http/ai_handle.go:FabAskStream":                                 true,
	"internal/module/ai/inbound/http/ai_router.go:SetupAIRoutes":                                true,
	"internal/module/analytics/inbound/http/analytics_page.go:AnalyticsPage":                    true,
	"internal/module/comment/inbound/http/comment_page.go:CommentsPage":                         true,
	"internal/module/content/inbound/http/content_page.go:SaveArticleTranslations":              true,
	"internal/module/content/inbound/http/content_page.go:build":                                true,
	"internal/module/content/inbound/http/content_page_router.go:SetupContentPages":             true,
	"internal/module/inventory/inbound/http/inventory_page_handle.go:InventoryPage":             true,
	"internal/module/mail/inbound/http/mail_page_router.go:setupMailPageRoutes":                 true,
	"internal/module/masterdata/inbound/http/masterdata_page.go:MasterDataChangesPage":          true,
	"internal/module/membership/inbound/http/membership_page.go:MembershipAssignmentsPage":      true,
	"internal/module/navigation/inbound/http/navigation_page_handle.go:build":                   true,
	"internal/module/order/inbound/http/coupon_page.go:CouponsPage":                             true,
	"internal/module/order/inbound/http/order_create_page.go:render":                            true,
	"internal/module/order/inbound/http/order_list_page.go:OrdersPage":                          true,
	"internal/module/order/inbound/http/order_page_router.go:SetupOrderPages":                   true,
	"internal/module/order/inbound/http/order_router.go:SetupOrderRoutes":                       true,
	"internal/module/order/inbound/http/return_page.go:ReturnsPage":                             true,
	"internal/module/order/inbound/http/sales_page.go:SalesOverviewPage":                        true,
	"internal/module/page/inbound/http/page_router.go:registerPageRoutes":                       true,
	"internal/module/page/inbound/http/page_translations_handle.go:SavePageTranslations":        true,
	"internal/module/page/inbound/http/page_translations_handle.go:buildPageTranslationsData":   true,
	"internal/module/page/inbound/http/page_translations_handle.go:buildSiteContentIndex":       true,
	"internal/module/project/inbound/http/project_page.go:SaveSiteLocales":                      true,
	"internal/module/project/inbound/http/project_page.go:SaveSiteSettings":                     true,
	"internal/module/project/inbound/http/project_page.go:SaveThemeSettings":                    true,
	"internal/module/project/inbound/http/project_page.go:loadThemeSettings":                    true,
	"internal/module/project/inbound/http/project_page.go:mergeSiteSettings":                    true,
	"internal/module/user/inbound/http/user_page.go:CustomerDetailPage":                         true,
	"internal/module/user/inbound/http/user_page.go:CustomersPage":                              true,
	"internal/module/user/inbound/http/user_page.go:customerCounterTabs":                        true,
	"internal/module/user/inbound/http/user_page.go:customerDetailPageData":                     true,
	"internal/module/workbench/inbound/http/canvas_handle.go:Workbench":                         true,
	"internal/module/workbench/inbound/http/workbench_page.go:collectOrderOverview":             true,
	"internal/module/workbench/inbound/http/canvas_handle.go:workbenchBlock":                    true,
	"internal/module/workbench/inbound/http/canvas_preview_handle.go:workbenchInstance":         true,
	"internal/module/workbench/inbound/http/canvas_handle.go:workbenchTemplate":                 true,
}

const controllerMethodMaxLines = 60

var (
	controllerFileName = regexp.MustCompile(`^[a-z0-9_]+_(router|handle)\.go$`)
	// 只禁**自动合并**的入口：ShouldBind/Bind 会按 Content-Type 选 binder 并把
	// query 并进表单，写 body 与查询串的边界因此不存在。原生显式绑定
	// （ShouldBindWith/ShouldBindJSON/ShouldBindQuery）与手工读表单属另一条判据。
	controllerAutoBindMethods = map[string]bool{
		"ShouldBind": true, "Bind": true,
	}
	// 手工逐字段读表单：按文件棘轮，只减不增。
	controllerManualFormMethods = map[string]bool{
		"PostForm": true, "GetPostForm": true, "PostFormArray": true, "GetPostFormArray": true,
	}

	// 只约束查询串。c.Param 是路径参数（路由段），不属于「入参走 Query[T]」的范围。
	controllerQueryMethods = map[string]bool{
		"Query": true, "DefaultQuery": true,
	}
)

func controllerDirs(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	dirs, err := filepath.Glob(filepath.Join(root, "internal/module/*/inbound/http"))
	if err != nil {
		t.Fatalf("枚举控制器目录失败: %v", err)
	}
	return dirs
}

func TestControllerFileNames(t *testing.T) {
	root := repoRoot(t)
	seen := map[string]bool{}
	for _, dir := range controllerDirs(t) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			rel, _ := filepath.Rel(root, filepath.Join(dir, name))
			rel = filepath.ToSlash(rel)
			if controllerFileName.MatchString(name) {
				continue
			}
			seen[rel] = true
			if !pendingControllerFiles[rel] {
				t.Errorf("%s：控制器文件只允许 <模块>_router.go / <域>_handle.go", rel)
			}
		}
	}
	for rel := range pendingControllerFiles {
		if !seen[rel] {
			t.Errorf("pendingControllerFiles 登记了 %s，但该文件已合规或已不存在 —— 请删掉这条登记", rel)
		}
	}
}

// TestControllerNoAutoBinding 禁止自动合并的绑定入口（ShouldBind / Bind）。
//
// 它们按 Content-Type 选 binder 并把 query 并进表单，写 body 与查询串的边界因此不存在。
// 原生显式绑定是规范允许的：ShouldBindWith(FormPost/FormMultipart)、ShouldBindJSON、
// ShouldBindQuery，以及 pkg/binding 的 ParseForm / ParseJSON / Query。
func TestControllerNoAutoBinding(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	for _, dir := range controllerDirs(t) {
		pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", dir, err)
		}
		for _, pkg := range pkgs {
			for path, file := range pkg.Files {
				rel, _ := filepath.Rel(root, path)
				rel = filepath.ToSlash(rel)
				ast.Inspect(file, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || !controllerAutoBindMethods[sel.Sel.Name] {
						return true
					}
					t.Errorf("%s:%d：控制器不用自动绑定的 %s（按 Content-Type 选 binder 且合并 query）—— "+
						"普通写用 ShouldBindWith(..., binding.FormPost)，复杂输入用 ShouldBindJSON，查询用 ShouldBindQuery",
						rel, fset.Position(call.Pos()).Line, sel.Sel.Name)
					return true
				})
			}
		}
	}
}

// TestControllerNoManualFormReads 是手工逐字段读表单的棘轮：存量只减不增。
func TestControllerNoManualFormReads(t *testing.T) {
	root := repoRoot(t)
	seen := map[string]bool{}
	fset := token.NewFileSet()
	for _, dir := range controllerDirs(t) {
		pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", dir, err)
		}
		for _, pkg := range pkgs {
			for path, file := range pkg.Files {
				rel, _ := filepath.Rel(root, path)
				rel = filepath.ToSlash(rel)
				ast.Inspect(file, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || !controllerManualFormMethods[sel.Sel.Name] {
						return true
					}
					seen[rel] = true
					if !pendingControllerBinding[rel] {
						t.Errorf("%s:%d：控制器不手工逐字段读表单 %s —— 绑定成 DTO（原生显式绑定或 ParseForm）",
							rel, fset.Position(call.Pos()).Line, sel.Sel.Name)
					}
					return true
				})
			}
		}
	}
	for rel := range pendingControllerBinding {
		if !seen[rel] {
			t.Errorf("pendingControllerBinding 登记了 %s，但该文件已无手工读表单 —— 请删掉这条登记", rel)
		}
	}
}

func TestControllerQueryBinding(t *testing.T) {
	root := repoRoot(t)
	seen := map[string]bool{}
	fset := token.NewFileSet()
	for _, dir := range controllerDirs(t) {
		pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", dir, err)
		}
		for _, pkg := range pkgs {
			for path, file := range pkg.Files {
				rel, _ := filepath.Rel(root, path)
				rel = filepath.ToSlash(rel)
				ast.Inspect(file, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || !controllerQueryMethods[sel.Sel.Name] {
						return true
					}

					if id, isIdent := sel.X.(*ast.Ident); !isIdent || id.Name != "c" {
						return true
					}
					seen[rel] = true
					if !pendingControllerQuery[rel] {
						t.Errorf("%s:%d：控制器不直读查询串 %s —— 入参走 pkg/binding 的 Query[T]（键名写在 dto 的 form tag）",
							rel, fset.Position(call.Pos()).Line, sel.Sel.Name)
					}
					return true
				})
			}
		}
	}
	for rel := range pendingControllerQuery {
		if !seen[rel] {
			t.Errorf("pendingControllerQuery 登记了 %s，但该文件已无手工读查询串 —— 请删掉这条登记", rel)
		}
	}
}

func TestControllerMethodSize(t *testing.T) {
	root := repoRoot(t)
	seen := map[string]bool{}
	fset := token.NewFileSet()
	for _, dir := range controllerDirs(t) {
		pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", dir, err)
		}
		for _, pkg := range pkgs {
			for path, file := range pkg.Files {
				rel, _ := filepath.Rel(root, path)
				rel = filepath.ToSlash(rel)
				for _, decl := range file.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || fn.Body == nil {
						continue
					}
					start := fset.Position(fn.Pos()).Line
					end := fset.Position(fn.End()).Line
					lines := end - start + 1
					if lines <= controllerMethodMaxLines {
						continue
					}
					key := rel + ":" + fn.Name.Name
					seen[key] = true
					if !pendingControllerMethods[key] {
						t.Errorf("%s：方法 %s 有 %d 行（上限 %d）—— 装配/取数/逐行映射应下沉到 service",
							rel, fn.Name.Name, lines, controllerMethodMaxLines)
					}
				}
			}
		}
	}
	for key := range pendingControllerMethods {
		if !seen[key] {
			t.Errorf("pendingControllerMethods 登记了 %s，但该方法已达标或不存在 —— 请删掉这条登记", key)
		}
	}
}

// 页面出口判据：控制器可以选真实模板并交业务事实（AGENTS.md 两种合法出口形状），
// 但只能用共享渲染入口——它们注入请求语言、CSRF 与后台请求事实；不得直连模板引擎。

// controllerRawRenderEntries 是绕过共享接线的直连出口：控制器不得使用。
var controllerRawRenderEntries = map[string]bool{
	"RenderHTML":      true,
	"HTMLRender":      true,
	"ExecuteTemplate": true,
}

func controllerIsRouter(name string) bool {
	return name == "router.go" || strings.HasSuffix(name, "_router.go")
}

func TestControllerRenderExit(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	for _, dir := range controllerDirs(t) {
		pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", dir, err)
		}
		for _, pkg := range pkgs {
			for path, file := range pkg.Files {
				if controllerIsRouter(filepath.Base(path)) {
					continue
				}
				rel, _ := filepath.Rel(root, path)
				rel = filepath.ToSlash(rel)
				ast.Inspect(file, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					if controllerRawRenderEntries[sel.Sel.Name] {
						t.Errorf("%s:%d：控制器不经共享渲染入口直接调用 %s —— 用 pkg/view 的 RenderPage / RenderBare / RenderTemplate / RenderFragment，由它注入请求语言与后台请求事实",
							rel, fset.Position(call.Pos()).Line, sel.Sel.Name)
					}
					return true
				})
			}
		}
	}
}

// controllerUIRetiredPath 是已清退的旧后台渲染包：它不存在，控制器也不得再 import。
const controllerUIRetiredPath = "go_wp/internal/adminui"

// TestControllerNoUILineImport 是终局断言：旧后台渲染包已删除，
// 控制器不能再依赖它取词 / 读请求 / 写响应 —— 对应职责分别在
// pkg/view/jet、pkg/binding、pkg/response。
func TestControllerNoUILineImport(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	for _, dir := range controllerDirs(t) {
		pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", dir, err)
		}
		for _, pkg := range pkgs {
			for path, file := range pkg.Files {
				if controllerIsRouter(filepath.Base(path)) {
					continue
				}
				rel, _ := filepath.Rel(root, path)
				rel = filepath.ToSlash(rel)
				for _, imp := range file.Imports {
					if imp.Path == nil || imp.Path.Value != "\""+controllerUIRetiredPath+"\"" {
						continue
					}
					t.Errorf("%s:%d：旧后台渲染包已删除，控制器不得再 import %s —— "+
						"取词走 pkg/view/jet、读请求走 pkg/binding、响应走 pkg/response",
						rel, fset.Position(imp.Pos()).Line, controllerUIRetiredPath)
				}
			}
		}
	}
}

func TestControllerNoNormalization(t *testing.T) {
	root := repoRoot(t)
	seen := map[string]bool{}
	fset := token.NewFileSet()
	for _, dir := range controllerDirs(t) {
		pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", dir, err)
		}
		for _, pkg := range pkgs {
			for path, file := range pkg.Files {
				if controllerIsRouter(filepath.Base(path)) {
					continue
				}
				rel, _ := filepath.Rel(root, path)
				rel = filepath.ToSlash(rel)
				ast.Inspect(file, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					pkgIdent, ok := sel.X.(*ast.Ident)
					if !ok || pkgIdent.Name != "bulkids" || sel.Sel.Name != "Normalize" {
						return true
					}
					seen[rel] = true
					if !pendingControllerNormalize[rel] {
						t.Errorf("%s:%d：控制器不做批量 id 归一 —— 归一（去重 / 上限）搬进 service，"+
							"控制器只绑请求、调用例、写响应",
							rel, fset.Position(call.Pos()).Line)
					}
					return true
				})
			}
		}
	}
	for rel := range pendingControllerNormalize {
		if !seen[rel] {
			t.Errorf("pendingControllerNormalize 登记了 %s，但该文件已不做归一 —— 请删掉这条登记", rel)
		}
	}
}
