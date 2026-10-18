// controller_shape_test.go — 控制器（inbound/http）的三条形状门禁。
//
// 口径（2026-10-18 用户拍板，「控制器瘦身是必然的」）：
//
//  1. **文件名**：控制器只有两种文件 —— `<模块>_router.go`（路由注册）与
//     `<域>_handle.go`（方法）。`xxx_page.go` / `xxx_write.go` / `xxx_shared.go`
//     / `xxx_err.go` / `xxx_jump.go` 这类名字要么是同一域被切碎，要么是错误出口
//     —— 两者都要收掉。
//  2. **绑定入口**：方法里不出现 c.ShouldBind* / c.PostForm / c.GetPostForm。
//     入参一律走 pkg/binding 的 JSON / Form / Query（DTO 声明字段，失败统一 400 JSON）。
//  3. **方法长度**：单个方法 ≤60 行。超长 = 装配、取数或逐行映射还留在控制器里。
//
// **pending 是必须缩小的清单**（双向判定：登记了却已消失同样变红）：
// 三条门禁各自登记此刻的存量，收一个删一条。新写的控制器直接命门禁，不要往 pending 里加。
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

// pendingControllerFiles 还没按 <模块>_router.go / <域>_handle.go 命名的控制器文件。
var pendingControllerFiles = map[string]bool{
	"internal/module/admin/inbound/http/admin_err.go":                         true,
	"internal/module/admin/inbound/http/admin_i18n_write.go":                  true,
	"internal/module/admin/inbound/http/admin_jump.go":                        true,
	"internal/module/admin/inbound/http/admin_page.go":                        true,
	"internal/module/ai/inbound/http/ai_err.go":                               true,
	"internal/module/ai/inbound/http/ai_page.go":                              true,
	"internal/module/analytics/inbound/http/analytics_page.go":                true,
	"internal/module/block/inbound/http/block_err.go":                         true,
	"internal/module/block/inbound/http/block_page.go":                        true,
	"internal/module/comment/inbound/http/comment_err.go":                     true,
	"internal/module/comment/inbound/http/comment_page.go":                    true,
	"internal/module/content/inbound/http/content_err.go":                     true,
	"internal/module/content/inbound/http/content_page.go":                    true,
	"internal/module/contenttemplate/inbound/http/content_template_err.go":    true,
	"internal/module/contenttemplate/inbound/http/contenttemplate_page.go":    true,
	"internal/module/inventory/inbound/http/inventory_err.go":                 true,
	"internal/module/inventory/inbound/http/inventory_jump.go":                true,
	"internal/module/inventory/inbound/http/inventory_page.go":                true,
	"internal/module/mail/inbound/http/mail_contact_write.go":                 true,
	"internal/module/mail/inbound/http/mail_err.go":                           true,
	"internal/module/mail/inbound/http/mail_jump.go":                          true,
	"internal/module/mail/inbound/http/mail_page.go":                          true,
	"internal/module/masterdata/inbound/http/masterdata_page.go":              true,
	"internal/module/membership/inbound/http/membership_err.go":               true,
	"internal/module/membership/inbound/http/membership_page.go":              true,
	"internal/module/navigation/inbound/http/navigation_add_source_write.go":  true,
	"internal/module/navigation/inbound/http/navigation_delete_write.go":      true,
	"internal/module/navigation/inbound/http/navigation_err.go":               true,
	"internal/module/navigation/inbound/http/navigation_jump.go":              true,
	"internal/module/navigation/inbound/http/navigation_move_write.go":        true,
	"internal/module/navigation/inbound/http/navigation_page.go":              true,
	"internal/module/navigation/inbound/http/navigation_translation_write.go": true,
	"internal/module/order/inbound/http/coupon_page.go":                       true,
	"internal/module/order/inbound/http/coupon_write.go":                      true,
	"internal/module/order/inbound/http/order_bulk_write.go":                  true,
	"internal/module/order/inbound/http/order_create_page.go":                 true,
	"internal/module/order/inbound/http/order_list_page.go":                   true,
	"internal/module/order/inbound/http/order_shared.go":                      true,
	"internal/module/order/inbound/http/order_write.go":                       true,
	"internal/module/order/inbound/http/return_bulk_write.go":                 true,
	"internal/module/order/inbound/http/return_page.go":                       true,
	"internal/module/order/inbound/http/sales_page.go":                        true,
	"internal/module/page/inbound/http/page_jump.go":                          true,
	"internal/module/page/inbound/http/page_langs.go":                         true,
	"internal/module/page/inbound/http/page_redirect.go":                      true,
	"internal/module/page/inbound/http/page_schedule.go":                      true,
	"internal/module/page/inbound/http/page_site_slot.go":                     true,
	"internal/module/page/inbound/http/page_stale.go":                         true,
	"internal/module/page/inbound/http/page_translations.go":                  true,
	"internal/module/plugin/inbound/http/plugin_err.go":                       true,
	"internal/module/project/inbound/http/project_err.go":                     true,
	"internal/module/project/inbound/http/project_jump.go":                    true,
	"internal/module/project/inbound/http/project_page.go":                    true,
	"internal/module/publication/inbound/http/seo_audit_result.go":            true,
	"internal/module/sysconfig/inbound/http/sysconfig_admin_pages.go":         true,
	"internal/module/sysconfig/inbound/http/sysconfig_err.go":                 true,
	"internal/module/user/inbound/http/customer_bulk_write.go":                true,
	"internal/module/user/inbound/http/user_page.go":                          true,
	"internal/module/workbench/inbound/http/router.go":                        true,
	"internal/module/workbench/inbound/http/workbench_err.go":                 true,
	"internal/module/workbench/inbound/http/workbench_page.go":                true,
}

// pendingControllerBinding 还没换成 pkg/binding 的文件（c.ShouldBind* / c.PostForm）。
var pendingControllerBinding = map[string]bool{
	"internal/module/admin/inbound/http/admin_page.go":             true,
	"internal/module/ai/inbound/http/ai_handle.go":                 true,
	"internal/module/ai/inbound/http/ai_page.go":                   true,
	"internal/module/content/inbound/http/content_handle.go":       true,
	"internal/module/content/inbound/http/content_page.go":         true,
	"internal/module/inventory/inbound/http/inventory_page.go":     true,
	"internal/module/mail/inbound/http/mail_handle.go":             true,
	"internal/module/mail/inbound/http/mail_page.go":               true,
	"internal/module/navigation/inbound/http/navigation_page.go":   true,
	"internal/module/order/inbound/http/order_create_page.go":      true,
	"internal/module/page/inbound/http/page_langs.go":              true,
	"internal/module/page/inbound/http/page_schedule.go":           true,
	"internal/module/page/inbound/http/page_site_slot.go":          true,
	"internal/module/page/inbound/http/page_translations.go":       true,
	"internal/module/page/inbound/http/pages_handle.go":            true,
	"internal/module/project/inbound/http/project_page.go":         true,
	"internal/module/publication/inbound/http/seo_audit_result.go": true,
}

// pendingControllerMethods 超过 60 行的控制器方法（"文件:方法名"）。
var pendingControllerMethods = map[string]bool{
	"internal/module/admin/inbound/http/admin_i18n_write.go:adminI18nWriteReqOf":                true,
	"internal/module/admin/inbound/http/admin_page.go:I18nEntriesPage":                          true,
	"internal/module/admin/inbound/http/admin_page.go:dataruleConfigFromForm":                   true,
	"internal/module/admin/inbound/http/admin_page.go:dataruleEditorContext":                    true,
	"internal/module/admin/inbound/http/admin_pages_router.go:SetupAdminPages":                  true,
	"internal/module/admin/inbound/http/admin_router.go:SetupAdminRoutes":                       true,
	"internal/module/ai/inbound/http/ai_handle.go:FabAskStream":                                 true,
	"internal/module/ai/inbound/http/ai_page.go:FabAsk":                                         true,
	"internal/module/ai/inbound/http/ai_page.go:SessionsPage":                                   true,
	"internal/module/ai/inbound/http/ai_router.go:SetupAIRoutes":                                true,
	"internal/module/analytics/inbound/http/analytics_page.go:AnalyticsPage":                    true,
	"internal/module/block/inbound/http/block_page.go:SaveBlockContent":                         true,
	"internal/module/comment/inbound/http/comment_page.go:CommentsPage":                         true,
	"internal/module/content/inbound/http/content_handle.go:ArticleLinkSuggestions":             true,
	"internal/module/content/inbound/http/content_page.go:SaveArticleTranslations":              true,
	"internal/module/content/inbound/http/content_page.go:build":                                true,
	"internal/module/content/inbound/http/content_page_router.go:SetupContentPages":             true,
	"internal/module/contenttemplate/inbound/http/contenttemplate_page.go:ContentTemplatesPage": true,
	"internal/module/inventory/inbound/http/inventory_page.go:InventoryPage":                    true,
	"internal/module/inventory/inbound/http/inventory_page.go:InventoryPurchasesPage":           true,
	"internal/module/inventory/inbound/http/inventory_page.go:InventorySourcesPage":             true,
	"internal/module/inventory/inbound/http/inventory_page.go:purchaseOrderRows":                true,
	"internal/module/inventory/inbound/http/inventory_router.go:SetupInventoryRoutes":           true,
	"internal/module/mail/inbound/http/mail_contact_write.go:ContactDelete":                     true,
	"internal/module/mail/inbound/http/mail_contact_write.go:ContactSave":                       true,
	"internal/module/mail/inbound/http/mail_contact_write.go:ContactTag":                        true,
	"internal/module/mail/inbound/http/mail_page.go:automationStepsFromGraph":                   true,
	"internal/module/mail/inbound/http/mail_page.go:buildAutomationDefinition":                  true,
	"internal/module/mail/inbound/http/mail_page_router.go:setupMailPageRoutes":                 true,
	"internal/module/mail/inbound/http/mail_router.go:SetupMailRoutes":                          true,
	"internal/module/masterdata/inbound/http/masterdata_page.go:MasterDataChangesPage":          true,
	"internal/module/membership/inbound/http/membership_page.go:MembershipAssignmentsPage":      true,
	"internal/module/navigation/inbound/http/navigation_add_source_write.go:createFromSource":   true,
	"internal/module/navigation/inbound/http/navigation_page.go:build":                          true,
	"internal/module/navigation/inbound/http/navigation_translation_write.go:writeTranslations": true,
	"internal/module/order/inbound/http/coupon_page.go:CouponsPage":                             true,
	"internal/module/order/inbound/http/order_create_page.go:render":                            true,
	"internal/module/order/inbound/http/order_list_page.go:OrdersPage":                          true,
	"internal/module/order/inbound/http/order_page_router.go:SetupOrderPages":                   true,
	"internal/module/order/inbound/http/order_router.go:SetupOrderRoutes":                       true,
	"internal/module/order/inbound/http/return_page.go:ReturnsPage":                             true,
	"internal/module/order/inbound/http/sales_page.go:SalesOverviewPage":                        true,
	"internal/module/page/inbound/http/page_router.go:registerPageRoutes":                       true,
	"internal/module/page/inbound/http/page_translations.go:SavePageTranslations":               true,
	"internal/module/page/inbound/http/page_translations.go:buildPageTranslationsData":          true,
	"internal/module/page/inbound/http/page_translations.go:buildSiteContentIndex":              true,
	"internal/module/page/inbound/http/pages_handle.go:buildPagesData":                          true,
	"internal/module/project/inbound/http/project_page.go:SaveSiteLocales":                      true,
	"internal/module/project/inbound/http/project_page.go:SaveSiteSettings":                     true,
	"internal/module/project/inbound/http/project_page.go:SaveThemeSettings":                    true,
	"internal/module/project/inbound/http/project_page.go:loadThemeSettings":                    true,
	"internal/module/project/inbound/http/project_page.go:mergeSiteSettings":                    true,
	"internal/module/user/inbound/http/user_page.go:CustomerCohortPage":                         true,
	"internal/module/user/inbound/http/user_page.go:CustomerDetailPage":                         true,
	"internal/module/user/inbound/http/user_page.go:CustomersPage":                              true,
	"internal/module/user/inbound/http/user_page.go:customerCounterTabs":                        true,
	"internal/module/user/inbound/http/user_page.go:customerDetailPageData":                     true,
	"internal/module/user/inbound/http/user_page.go:customerRow":                                true,
	"internal/module/workbench/inbound/http/router.go:SetupWorkbenchRoutes":                     true,
	"internal/module/workbench/inbound/http/workbench_page.go:Workbench":                        true,
	"internal/module/workbench/inbound/http/workbench_page.go:collectOrderOverview":             true,
	"internal/module/workbench/inbound/http/workbench_page.go:layoutTrendBars":                  true,
	"internal/module/workbench/inbound/http/workbench_page.go:workbenchBlock":                   true,
	"internal/module/workbench/inbound/http/workbench_page.go:workbenchInstance":                true,
	"internal/module/workbench/inbound/http/workbench_page.go:workbenchTemplate":                true,
}

// controllerMethodMaxLines 单个控制器方法的行数上限（AST 起止行之差）。
const controllerMethodMaxLines = 60

var (
	controllerFileName = regexp.MustCompile(`^[a-z0-9_]+_(router|handle)\.go$`)
	bindingMethodNames = map[string]bool{
		"ShouldBind": true, "ShouldBindJSON": true, "ShouldBindQuery": true,
		"ShouldBindUri": true, "ShouldBindHeader": true, "ShouldBindBodyWith": true,
		"PostForm": true, "GetPostForm": true, "PostFormArray": true, "GetPostFormArray": true,
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

func TestControllerBindingEntry(t *testing.T) {
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
					if !ok || !bindingMethodNames[sel.Sel.Name] {
						return true
					}
					seen[rel] = true
					if !pendingControllerBinding[rel] {
						t.Errorf("%s:%d：控制器不直调 %s —— 入参走 pkg/binding 的 JSON[T] / Form[T] / Query[T]",
							rel, fset.Position(call.Pos()).Line, sel.Sel.Name)
					}
					return true
				})
			}
		}
	}
	for rel := range pendingControllerBinding {
		if !seen[rel] {
			t.Errorf("pendingControllerBinding 登记了 %s，但该文件已无手工绑定 —— 请删掉这条登记", rel)
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
