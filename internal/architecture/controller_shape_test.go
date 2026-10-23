// controller_shape_test.go — 控制器（inbound/http）的五条形状门禁。
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
//  4. **渲染出口**：方法里不出现 c.HTML / adminui.RenderPage / adminui.RenderBare。
//     控制器交「视图 + 错误」，渲染由 *_router.go 的 adminui.Page / adminui.Bare 装配
//     —— 模板名归路由，失败一律 JSON，成功页才走 Jet。
//  5. **UI 线依赖**：控制器不 import internal/adminui。取词走 internal/text、
//     读请求上下文走 pkg/binding、响应走 pkg/response —— 三条线各归各的包。
//  6. **不做计算**：控制器里不出现 bulkids.Normalize —— 归一（去重 / 上限）是 service 的事。
//     控制器只是编排出口：绑请求 → 调用例 → 写响应。它不 trim、不去重、不数条数、不判业务
//     （「没勾选算不算错误」这类判断在 service），也不替错误挑状态码（状态在声明处）。
//     2026-10-18 用户拍板：「控制器自身不能有任何动作，这里只是个编排出口」。
//
// **pending 是必须缩小的清单**（双向判定：登记了却已消失同样变红）：
// 五条门禁各自登记此刻的存量，收一个删一条。新写的控制器直接命门禁，不要往 pending 里加。
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

// pendingControllerNormalize 还在控制器里做批量 id 归一的文件（判据见第 6 条）。
//
// 收一个删一条：文件里最后一次 bulkids.Normalize 调用搬进 service 后，这条登记要一起删，
// 否则本测试反向判定会报「登记了却已无命中」。
var pendingControllerNormalize = map[string]bool{
	"internal/module/admin/inbound/http/admin_i18n_write.go":                  true,
	"internal/module/block/inbound/http/block_handle.go":                      true,
	"internal/module/contenttemplate/inbound/http/contenttemplate_handle.go":  true,
	"internal/module/inventory/inbound/http/inventory_reason_handle.go":       true,
	"internal/module/inventory/inbound/http/inventory_source_handle.go":       true,
	"internal/module/inventory/inbound/http/inventory_warehouse_handle.go":    true,
	"internal/module/mail/inbound/http/mail_contact_write.go":                 true,
	"internal/module/mail/inbound/http/mail_page.go":                          true,
	"internal/module/navigation/inbound/http/navigation_add_source_handle.go": true,
	"internal/module/navigation/inbound/http/navigation_delete_handle.go":     true,
	"internal/module/order/inbound/http/order_write.go":                       true,
	"internal/module/user/inbound/http/customer_bulk_write.go":                true,
}

// pendingControllerFiles 还没按 <模块>_router.go / <域>_handle.go 命名的控制器文件。
var pendingControllerFiles = map[string]bool{
	"internal/module/admin/inbound/http/admin_err.go":                      true,
	"internal/module/admin/inbound/http/admin_i18n_write.go":               true,
	"internal/module/admin/inbound/http/admin_jump.go":                     true,
	"internal/module/admin/inbound/http/admin_page.go":                     true,
	"internal/module/ai/inbound/http/ai_err.go":                            true,
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
	"internal/module/mail/inbound/http/mail_page.go":                       true,
	"internal/module/masterdata/inbound/http/masterdata_page.go":           true,
	"internal/module/membership/inbound/http/membership_page.go":           true,
	"internal/module/order/inbound/http/coupon_page.go":                    true,
	"internal/module/order/inbound/http/coupon_write.go":                   true,
	"internal/module/order/inbound/http/order_bulk_write.go":               true,
	"internal/module/order/inbound/http/order_create_page.go":              true,
	"internal/module/order/inbound/http/order_list_page.go":                true,
	"internal/module/order/inbound/http/order_shared.go":                   true,
	"internal/module/order/inbound/http/order_write.go":                    true,
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
	"internal/module/publication/inbound/http/seo_audit_result.go":         true,
	"internal/module/sysconfig/inbound/http/sysconfig_admin_pages.go":      true,
	"internal/module/user/inbound/http/customer_bulk_write.go":             true,
	"internal/module/user/inbound/http/user_page.go":                       true,
	"internal/module/workbench/inbound/http/router.go":                     true,
	"internal/module/workbench/inbound/http/workbench_err.go":              true,
	// 工作台画布逻辑按域拆成 5 个文件后，同一条存量豁免跟着拆（判据是"这些文件还没换 binding"，
	// 与文件数无关）。收敛时逐个文件改，改完删对应那一行。
	"internal/module/workbench/inbound/http/workbench_page.go":            true,
}

// pendingControllerBinding 还没换成 pkg/binding 的文件（c.ShouldBind* / c.PostForm）。
var pendingControllerBinding = map[string]bool{
	"internal/module/admin/inbound/http/admin_page.go":                  true,
	"internal/module/ai/inbound/http/ai_page.go":                        true,
	"internal/module/content/inbound/http/content_handle.go":            true,
	"internal/module/content/inbound/http/content_page.go":              true,
	"internal/module/mail/inbound/http/mail_page.go":                    true,
	"internal/module/navigation/inbound/http/navigation_page_handle.go": true,
	"internal/module/order/inbound/http/order_create_page.go":           true,
	"internal/module/project/inbound/http/project_page.go":              true,
}

// pendingControllerQuery 还没换成 pkg/binding 的查询串读取（c.Query / c.DefaultQuery / c.Param）。
//
// 为什么与 pendingControllerBinding 分表：那三条（ShouldBind / PostForm）说的是「请求体」，
// 这三条说的是「查询串」。商品模块两类都已清零，但存量模块的查询串读取量大（约 300 处），
// 混在一张表里会让「已登记 = 这个文件可违反两类规则」的误读。
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
	"internal/module/inventory/inbound/http/inventory_purchase_handle.go":  true,
	"internal/module/inventory/inbound/http/inventory_reason_handle.go":    true,
	"internal/module/inventory/inbound/http/inventory_source_handle.go":    true,
	"internal/module/inventory/inbound/http/inventory_warehouse_handle.go": true,
	"internal/module/mail/inbound/http/mail_err.go":                        true,
	"internal/module/mail/inbound/http/mail_handle.go":                     true,
	"internal/module/mail/inbound/http/mail_page.go":                       true,
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
	"internal/module/page/inbound/http/page_langs_handle.go":               true,
	"internal/module/page/inbound/http/page_redirect.go":                   true,
	"internal/module/page/inbound/http/page_schedule_handle.go":            true,
	"internal/module/page/inbound/http/pages_handle.go":                    true,
	"internal/module/page/inbound/http/page_site_slot_handle.go":           true,
	"internal/module/page/inbound/http/page_translations_handle.go":        true,
	"internal/module/project/inbound/http/project_handle.go":               true,
	"internal/module/project/inbound/http/project_page.go":                 true,
	"internal/module/publication/inbound/http/seo_audit_result.go":         true,
	"internal/module/user/inbound/http/user_customer_admin_handle.go":      true,
	"internal/module/user/inbound/http/user_page.go":                       true,
	"internal/module/webhook/inbound/http/webhook_handle.go":               true,
	// 工作台画布逻辑按域拆成 5 个文件后，同一条存量豁免跟着拆（判据是"这些文件还没换 binding"，
	// 与文件数无关）。收敛时逐个文件改，改完删对应那一行。
	"internal/module/workbench/inbound/http/workbench_page.go":            true,
	"internal/module/workbench/inbound/http/canvas_handle.go":             true,
	"internal/module/workbench/inbound/http/canvas_preview_handle.go":     true,
	"internal/module/workbench/inbound/http/canvas_bridge_handle.go":      true,
}

// pendingControllerMethods 超过 60 行的控制器方法（"文件:方法名"）。
var pendingControllerMethods = map[string]bool{
	"internal/module/admin/inbound/http/admin_i18n_write.go:adminI18nWriteReqOf":                  true,
	"internal/module/admin/inbound/http/admin_page.go:I18nEntriesPage":                            true,
	"internal/module/admin/inbound/http/admin_page.go:dataruleConfigFromForm":                     true,
	"internal/module/admin/inbound/http/admin_page.go:dataruleEditorContext":                      true,
	"internal/module/admin/inbound/http/admin_pages_router.go:SetupAdminPages":                    true,
	"internal/module/admin/inbound/http/admin_router.go:SetupAdminRoutes":                         true,
	"internal/module/ai/inbound/http/ai_handle.go:FabAskStream":                                   true,
	"internal/module/ai/inbound/http/ai_page.go:FabAsk":                                           true,
	"internal/module/ai/inbound/http/ai_page.go:SessionsPage":                                     true,
	"internal/module/ai/inbound/http/ai_router.go:SetupAIRoutes":                                  true,
	"internal/module/analytics/inbound/http/analytics_page.go:AnalyticsPage":                      true,
	"internal/module/block/inbound/http/block_page.go:SaveBlockContent":                           true,
	"internal/module/comment/inbound/http/comment_page.go:CommentsPage":                           true,
	"internal/module/content/inbound/http/content_handle.go:ArticleLinkSuggestions":               true,
	"internal/module/content/inbound/http/content_page.go:SaveArticleTranslations":                true,
	"internal/module/content/inbound/http/content_page.go:build":                                  true,
	"internal/module/content/inbound/http/content_page_router.go:SetupContentPages":               true,
	"internal/module/contenttemplate/inbound/http/contenttemplate_page.go:ContentTemplatesPage":   true,
	"internal/module/inventory/inbound/http/inventory_page_handle.go:InventoryPage":               true,
	"internal/module/inventory/inbound/http/inventory_purchase_handle.go:InventoryPurchasesPage":  true,
	"internal/module/inventory/inbound/http/inventory_source_handle.go:InventorySourcesPage":      true,
	"internal/module/inventory/inbound/http/inventory_purchase_handle.go:purchaseOrderRows":       true,
	"internal/module/inventory/inbound/http/inventory_router.go:SetupInventoryRoutes":             true,
	"internal/module/mail/inbound/http/mail_contact_write.go:ContactDelete":                       true,
	"internal/module/mail/inbound/http/mail_contact_write.go:ContactSave":                         true,
	"internal/module/mail/inbound/http/mail_contact_write.go:ContactTag":                          true,
	"internal/module/mail/inbound/http/mail_page_router.go:setupMailPageRoutes":                   true,
	"internal/module/mail/inbound/http/mail_router.go:SetupMailRoutes":                            true,
	"internal/module/masterdata/inbound/http/masterdata_page.go:MasterDataChangesPage":            true,
	"internal/module/membership/inbound/http/membership_page.go:MembershipAssignmentsPage":        true,
	"internal/module/navigation/inbound/http/navigation_add_source_handle.go:createFromSource":    true,
	"internal/module/navigation/inbound/http/navigation_page_handle.go:build":                     true,
	"internal/module/navigation/inbound/http/navigation_translations_handle.go:writeTranslations": true,
	"internal/module/order/inbound/http/coupon_page.go:CouponsPage":                               true,
	"internal/module/order/inbound/http/order_create_page.go:render":                              true,
	"internal/module/order/inbound/http/order_list_page.go:OrdersPage":                            true,
	"internal/module/order/inbound/http/order_page_router.go:SetupOrderPages":                     true,
	"internal/module/order/inbound/http/order_router.go:SetupOrderRoutes":                         true,
	"internal/module/order/inbound/http/return_page.go:ReturnsPage":                               true,
	"internal/module/order/inbound/http/sales_page.go:SalesOverviewPage":                          true,
	"internal/module/page/inbound/http/page_router.go:registerPageRoutes":                         true,
	"internal/module/page/inbound/http/page_translations_handle.go:SavePageTranslations":          true,
	"internal/module/page/inbound/http/page_translations_handle.go:buildPageTranslationsData":     true,
	"internal/module/page/inbound/http/page_translations_handle.go:buildSiteContentIndex":         true,
	"internal/module/page/inbound/http/pages_handle.go:buildPagesData":                            true,
	"internal/module/project/inbound/http/project_page.go:SaveSiteLocales":                        true,
	"internal/module/project/inbound/http/project_page.go:SaveSiteSettings":                       true,
	"internal/module/project/inbound/http/project_page.go:SaveThemeSettings":                      true,
	"internal/module/project/inbound/http/project_page.go:loadThemeSettings":                      true,
	"internal/module/project/inbound/http/project_page.go:mergeSiteSettings":                      true,
	"internal/module/user/inbound/http/user_page.go:CustomerCohortPage":                           true,
	"internal/module/user/inbound/http/user_page.go:CustomerDetailPage":                           true,
	"internal/module/user/inbound/http/user_page.go:CustomersPage":                                true,
	"internal/module/user/inbound/http/user_page.go:customerCounterTabs":                          true,
	"internal/module/user/inbound/http/user_page.go:customerDetailPageData":                       true,
	"internal/module/user/inbound/http/user_page.go:customerRow":                                  true,
	"internal/module/workbench/inbound/http/router.go:SetupWorkbenchRoutes":                       true,
	"internal/module/workbench/inbound/http/canvas_handle.go:Workbench":                          true,
	"internal/module/workbench/inbound/http/workbench_page.go:collectOrderOverview":               true,
	"internal/module/workbench/inbound/http/workbench_page.go:layoutTrendBars":                    true,
	"internal/module/workbench/inbound/http/canvas_handle.go:workbenchBlock":                     true,
	"internal/module/workbench/inbound/http/canvas_preview_handle.go:workbenchInstance":                  true,
	"internal/module/workbench/inbound/http/canvas_handle.go:workbenchTemplate":                  true,
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
	// controllerQueryMethods 是「读查询串」的三个入口，与请求体的四个分开判：查询串一律
	// binding.Query[T]（键名与去空白在 DTO），跨路由共享的隐藏域才用 binding.Field。
	controllerQueryMethods = map[string]bool{
		"Query": true, "DefaultQuery": true, "Param": true,
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

// TestControllerQueryBinding 控制器不直读查询串。
//
// 判据只有一条：页面 / 接口的参数键名属于请求形状，声明在 dto 的 form tag 上；控制器里出现
// c.Query / c.DefaultQuery / c.Param 就意味着键名散落在代码里（改一处必然漏一处）。
// 与 TestControllerBindingEntry 同一台机器，只是读的方向不同。
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
					// 只认接收者是 gin.Context 的惯用变量 c：pagination.Query(...) 是拼查询串的
					// 纯函数、c.Request.URL.Query() 是 net/url 的解析口，两者都不是「读参数」。
					// 判据收在这里而不是名单里，否则门禁会因为同名方法误报，最后被调宽到没有意义。
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

// ── 渲染出口门禁 ──
//
// 控制器**不渲染**：它只交出视图（any）与错误，模板选择、页壳装配、状态码由 UI 线
// （adminui.Page / adminui.Bare）在路由装配处决定。理由：渲染入口长在控制器里时
// 「这一屏用什么模板」与「这一屏取什么数」耦在一个文件里，于是同一份视图既可能被
// 当整页渲、又可能被当片段渲，而两种渲法的注入键（页壳 / 仅取词）与失败形态都不同 ——
// 出错时表现为「片段拿整页壳」「错误被当 200 渲出去」，都不报错。
//
// 判据只看**调用点**（c.HTML / adminui.RenderPage / adminui.RenderBare），不看 import：
// 文案注入（adminui.Prepare*）、取词（MsgText）、表单上下文（ListQuery）仍在控制器里，
// 那是另一条线（文案线）的归属。router 文件是装配点，允许调用。

// pendingControllerRenderExit 还没把渲染出口交出去的控制器文件。
// **只减不增**：收一个删一条；登记了却已无渲染出口同样变红。
var pendingControllerRenderExit = map[string]bool{
	"internal/module/admin/inbound/http/admin_page.go":                     true,
	"internal/module/ai/inbound/http/ai_page.go":                           true,
	"internal/module/analytics/inbound/http/analytics_page.go":             true,
	"internal/module/block/inbound/http/block_page.go":                     true,
	"internal/module/comment/inbound/http/comment_page.go":                 true,
	"internal/module/content/inbound/http/content_page.go":                 true,
	"internal/module/contenttemplate/inbound/http/contenttemplate_page.go": true,
	"internal/module/mail/inbound/http/mail_page.go":                       true,
	"internal/module/masterdata/inbound/http/masterdata_page.go":           true,
	"internal/module/media/inbound/http/media_page_handle.go":              true,
	"internal/module/membership/inbound/http/membership_page.go":           true,
	"internal/module/navigation/inbound/http/navigation_page_handle.go":    true,
	"internal/module/order/inbound/http/coupon_page.go":                    true,
	"internal/module/order/inbound/http/order_create_page.go":              true,
	"internal/module/order/inbound/http/order_list_page.go":                true,
	"internal/module/order/inbound/http/return_page.go":                    true,
	"internal/module/order/inbound/http/sales_page.go":                     true,
	"internal/module/page/inbound/http/page_langs_handle.go":               true,
	"internal/module/page/inbound/http/page_redirect.go":                   true,
	"internal/module/page/inbound/http/page_schedule_handle.go":            true,
	"internal/module/page/inbound/http/pages_handle.go":                    true,
	"internal/module/page/inbound/http/page_site_slot_handle.go":           true,
	"internal/module/page/inbound/http/page_translations_handle.go":        true,
	"internal/module/plugin/inbound/http/plugin_page_handle.go":            true,
	"internal/module/project/inbound/http/project_page.go":                 true,
	"internal/module/publication/inbound/http/seo_audit_result.go":         true,
	"internal/module/sysconfig/inbound/http/sysconfig_admin_pages.go":      true,
	"internal/module/user/inbound/http/user_page.go":                       true,
	// 工作台画布逻辑按域拆成 5 个文件后，同一条存量豁免跟着拆（判据是"这些文件还没换 binding"，
	// 与文件数无关）。收敛时逐个文件改，改完删对应那一行。
	"internal/module/workbench/inbound/http/inspector_handle.go":          true,
	"internal/module/workbench/inbound/http/canvas_handle.go":             true,
	"internal/module/workbench/inbound/http/canvas_preview_handle.go":     true,
}

// controllerRenderCalls 渲染出口的方法名（接收者不限：c / h / 变量都可能）。
var controllerRenderCalls = map[string]bool{
	"HTML":       true, // c.HTML(status, tpl, data)
	"RenderPage": true, // adminui.RenderPage
	"RenderBare": true, // adminui.RenderBare
}

// controllerIsRouter 路由装配文件（唯一允许调用渲染出口的地方）。
func controllerIsRouter(name string) bool {
	return name == "router.go" || strings.HasSuffix(name, "_router.go")
}

func TestControllerRenderExit(t *testing.T) {
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
					if !ok || !controllerRenderCalls[sel.Sel.Name] {
						return true
					}
					seen[rel] = true
					if !pendingControllerRenderExit[rel] {
						t.Errorf("%s:%d：控制器不渲染 —— 返回视图（any, error），由 *_router.go 用 adminui.Page / adminui.Bare 装配",
							rel, fset.Position(call.Pos()).Line)
					}
					return true
				})
			}
		}
	}
	for rel := range pendingControllerRenderExit {
		if !seen[rel] {
			t.Errorf("pendingControllerRenderExit 登记了 %s，但该文件已无渲染出口 —— 请删掉这条登记", rel)
		}
	}
}

// ── UI 线依赖门禁 ──
//
// 控制器**不 import internal/adminui**。adminui 是 UI 线：页壳装配、渲染实现、拒绝出口、
// 导航树、权限码注入。控制器只需要三样东西，各有各的归属：
//
//	取词          → internal/text   （文案线；与渲染无关）
//	读请求上下文    → pkg/binding     （ListQuery / CSRFToken / JSON|Form|Query）
//	响应出口       → pkg/response    （{code,message,data}）
//
// 让控制器 import adminui 的连带代价是它顺手就会拿到 c.HTML / RenderPage / Prepare 这些东西，
// 于是「控制器不渲染」那条门禁只能靠自觉。本门禁管的是**依赖**，上一条管的是**调用点**。
//
// router 文件豁免：它是装配点，页壳与渲染就在那里接上。

// pendingControllerUIImport 还在 import adminui 的控制器文件。**只减不增**。
var pendingControllerUIImport = map[string]bool{
	"internal/module/admin/inbound/http/admin_err.go":                           true,
	"internal/module/admin/inbound/http/admin_jump.go":                          true,
	"internal/module/admin/inbound/http/admin_page.go":                          true,
	"internal/module/ai/inbound/http/ai_err.go":                                 true,
	"internal/module/ai/inbound/http/ai_page.go":                                true,
	"internal/module/analytics/inbound/http/analytics_page.go":                  true,
	"internal/module/block/inbound/http/block_err.go":                           true,
	"internal/module/block/inbound/http/block_page.go":                          true,
	"internal/module/comment/inbound/http/comment_page.go":                      true,
	"internal/module/content/inbound/http/content_page.go":                      true,
	"internal/module/contenttemplate/inbound/http/content_template_err.go":      true,
	"internal/module/contenttemplate/inbound/http/contenttemplate_page.go":      true,
	"internal/module/mail/inbound/http/mail_err.go":                             true,
	"internal/module/mail/inbound/http/mail_jump.go":                            true,
	"internal/module/mail/inbound/http/mail_page.go":                            true,
	"internal/module/masterdata/inbound/http/masterdata_page.go":                true,
	"internal/module/media/inbound/http/media_page_handle.go":                   true,
	"internal/module/membership/inbound/http/membership_page.go":                true,
	"internal/module/navigation/inbound/http/navigation_error_handle.go":        true,
	"internal/module/navigation/inbound/http/navigation_page_handle.go":         true,
	"internal/module/navigation/inbound/http/navigation_translations_handle.go": true,
	"internal/module/order/inbound/http/coupon_page.go":                         true,
	"internal/module/order/inbound/http/order_create_page.go":                   true,
	"internal/module/order/inbound/http/order_list_page.go":                     true,
	"internal/module/order/inbound/http/order_shared.go":                        true,
	"internal/module/order/inbound/http/return_page.go":                         true,
	"internal/module/order/inbound/http/sales_page.go":                          true,
	"internal/module/page/inbound/http/page_jump.go":                            true,
	"internal/module/page/inbound/http/page_langs_handle.go":                    true,
	"internal/module/page/inbound/http/page_schedule_handle.go":                 true,
	"internal/module/page/inbound/http/pages_handle.go":                         true,
	"internal/module/page/inbound/http/page_site_slot_handle.go":                true,
	"internal/module/page/inbound/http/page_translations_handle.go":             true,
	"internal/module/plugin/inbound/http/plugin_err.go":                         true,
	"internal/module/plugin/inbound/http/plugin_page_handle.go":                 true,
	"internal/module/project/inbound/http/project_jump.go":                      true,
	"internal/module/project/inbound/http/project_page.go":                      true,
	"internal/module/publication/inbound/http/seo_audit_result.go":              true,
	"internal/module/sysconfig/inbound/http/sysconfig_admin_pages.go":           true,
	"internal/module/user/inbound/http/customer_bulk_write.go":                  true,
	"internal/module/user/inbound/http/user_page.go":                            true,
	"internal/module/workbench/inbound/http/workbench_err.go":                   true,
	"internal/module/workbench/inbound/http/workbench_handle.go":                true,
	// 工作台画布逻辑拆成 5 个文件后，同一条存量豁免跟着拆（它们都还用 adminui 做页面渲染出口）。
	"internal/module/workbench/inbound/http/inspector_handle.go":          true,
	"internal/module/workbench/inbound/http/canvas_handle.go":             true,
	"internal/module/workbench/inbound/http/canvas_preview_handle.go":     true,
	"internal/module/workbench/inbound/http/canvas_bridge_handle.go":      true,
}

const controllerUIImportPath = "go_wp/internal/adminui"

func TestControllerNoUILineImport(t *testing.T) {
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
				for _, imp := range file.Imports {
					if imp.Path == nil || imp.Path.Value != "\""+controllerUIImportPath+"\"" {
						continue
					}
					seen[rel] = true
					if !pendingControllerUIImport[rel] {
						t.Errorf("%s:%d：控制器不 import internal/adminui —— 取词走 internal/text、"+
							"读请求走 pkg/binding、响应走 pkg/response",
							rel, fset.Position(imp.Pos()).Line)
					}
				}
			}
		}
	}
	for rel := range pendingControllerUIImport {
		if !seen[rel] {
			t.Errorf("pendingControllerUIImport 登记了 %s，但该文件已不 import adminui —— 请删掉这条登记", rel)
		}
	}
}

// TestControllerNoNormalization 控制器不做归一（第 6 条）。
//
// 判据抓的是**调用**（bulkids.Normalize 的 CallExpr），不是 import：控制器引用
// bulkids.MsgTooMany 之类只为了取词，那是文案，不在这里管。
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

// TestControllerPageExitSignature 页面出口只有一种形状（0.6 判据，零存量）：
//
//   - 处理器签名是 func(*gin.Context)，成功 pageview.Render(c, data)、失败 pageview.Fail(c, err)；
//     不再有 `(any, error)` —— 那种形状让渲染时机由装配处决定，控制器永远说不清「我现在是交数据
//     还是交错误」，也让「最后一句是封装调用」这条判据无法机械判定；
//   - 模板名只在 *_router.go，且装配只认 adminui.PageSlot / BareSlot；
//   - 非 router 控制器不出现 adminui.Page / adminui.Bare。
//
// 双向：这里没有 pending 清单 —— 迁移已完成，新写一个 (any, error) 处理器直接变红。
func TestControllerPageExitSignature(t *testing.T) {
	root := repoRoot(t)
	mods, err := filepath.Glob(filepath.Join(root, "internal", "module", "*", "inbound", "http", "*.go"))
	if err != nil || len(mods) == 0 {
		t.Fatalf("找不到控制器目录: %v", err)
	}
	for _, path := range mods {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		fset := token.NewFileSet()
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			t.Fatalf("解析 %s 失败: %v", rel, perr)
		}
		router := controllerIsRouter(filepath.Base(path))
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.FuncDecl:
				if isAnyErrorResult(node.Type.Results) {
					t.Errorf("%s:%d：页面处理器不再交 (any, error) —— 签名改 func(*gin.Context)，"+
						"成功 pageview.Render(c, data)、失败 pageview.Fail(c, err)",
						rel, fset.Position(node.Pos()).Line)
				}
			case *ast.SelectorExpr:
				if router {
					return true
				}
				ident, ok := node.X.(*ast.Ident)
				if !ok || ident.Name != "adminui" {
					return true
				}
				if node.Sel.Name == "Page" || node.Sel.Name == "Bare" ||
					node.Sel.Name == "RenderPage" || node.Sel.Name == "RenderBare" {
					t.Errorf("%s:%d：控制器不装配页面出口（adminui.%s）—— 模板名与渲染归 *_router.go",
						rel, fset.Position(node.Pos()).Line, node.Sel.Name)
				}
			}
			return true
		})
	}
}

// isAnyErrorResult 判断结果列表是不是「任意值 + error」这两个（老页面处理器形状）。
func isAnyErrorResult(results *ast.FieldList) bool {
	if results == nil || len(results.List) != 2 {
		return false
	}
	first, second := results.List[0], results.List[1]
	if len(first.Names) > 1 || len(second.Names) > 1 {
		return false
	}
	iface, ok := first.Type.(*ast.InterfaceType)
	if !ok || iface.Methods == nil || len(iface.Methods.List) != 0 {
		return false
	}
	errIdent, ok := second.Type.(*ast.Ident)
	return ok && errIdent.Name == "error"
}
