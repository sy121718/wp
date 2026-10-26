package architecture

// controller_orchestration_test.go — 控制器是编排出口：一个 handler 只允许「绑定 → 调业务层 → 出口」。
//
// 判据用**语句数**而不是行数：行数会被注释、换行和错误处理展开骗过去，语句数直接反映
//「这个入口自己干了多少活」。上限 6 条（含 if / return / 赋值 / 表达式语句 / defer）。
//
// 存量按**文件**登记（pendingControllerStatements）：offenders 是「超过上限的函数个数」，
// max 是这些函数里最大的语句数。两条都**双向**判：新超限的函数让 offenders 变多 → 红；
// 改好一个让 offenders 变少、或最大语句数下降 → 也要改登记（只能下降，不许偷偷放宽）。
//
// 商品模块的目标是**零存量**：它的 12 条登记（属性值行 / 预览框 / 评分抽屉 / 详情模板页等）
// 正在逐屏下沉，改掉一条就删一条；其余模块按同一形态收敛。

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const controllerStatementBudget = 6

// pendingControllerStatements 各控制器的存量（键是仓库相对路径）。
var pendingControllerStatements = map[string]struct{ offenders, max int }{
	"internal/module/admin/inbound/http/admin_handle.go":                        {offenders: 8, max: 13},
	"internal/module/admin/inbound/http/admin_i18n_write.go":                    {offenders: 1, max: 33},
	"internal/module/admin/inbound/http/admin_page.go":                          {offenders: 36, max: 37},
	"internal/module/ai/inbound/http/ai_handle.go":                              {offenders: 4, max: 28},
	"internal/module/ai/inbound/http/ai_page.go":                                {offenders: 6, max: 24},
	"internal/module/analytics/inbound/http/analytics_handle.go":                {offenders: 1, max: 7},
	"internal/module/analytics/inbound/http/analytics_page.go":                  {offenders: 2, max: 25},
	"internal/module/block/inbound/http/block_handle.go":                        {offenders: 1, max: 15},
	"internal/module/block/inbound/http/block_page.go":                          {offenders: 3, max: 16},
	"internal/module/cart/inbound/http/cart_router.go":                          {offenders: 1, max: 11},
	"internal/module/comment/inbound/http/comment_page.go":                      {offenders: 1, max: 30},
	"internal/module/content/inbound/http/content_handle.go":                    {offenders: 2, max: 23},
	"internal/module/content/inbound/http/content_page.go":                      {offenders: 10, max: 52},
	"internal/module/contenttemplate/inbound/http/contenttemplate_handle.go":    {offenders: 1, max: 28},
	"internal/module/contenttemplate/inbound/http/contenttemplate_page.go":      {offenders: 2, max: 31},
	"internal/module/inventory/inbound/http/inventory_page_handle.go":           {offenders: 1, max: 39},
	"internal/module/inventory/inbound/http/inventory_purchase_handle.go":       {offenders: 1, max: 29},
	"internal/module/inventory/inbound/http/inventory_reason_handle.go":         {offenders: 2, max: 15},
	"internal/module/inventory/inbound/http/inventory_source_handle.go":         {offenders: 2, max: 39},
	"internal/module/inventory/inbound/http/inventory_stock_handle.go":          {offenders: 2, max: 9},
	"internal/module/inventory/inbound/http/inventory_warehouse_handle.go":      {offenders: 2, max: 12},
	"internal/module/mail/inbound/http/mail_contact_write.go":                   {offenders: 3, max: 26},
	"internal/module/mail/inbound/http/mail_handle.go":                          {offenders: 1, max: 10},
	"internal/module/mail/inbound/http/mail_page.go":                            {offenders: 14, max: 19},
	"internal/module/masterdata/inbound/http/masterdata_page.go":                {offenders: 1, max: 42},
	"internal/module/media/inbound/http/media_handle.go":                        {offenders: 1, max: 8},
	"internal/module/membership/inbound/http/membership_page.go":                {offenders: 2, max: 31},
	"internal/module/navigation/inbound/http/navigation_handle.go":              {offenders: 3, max: 14},
	"internal/module/navigation/inbound/http/navigation_page_handle.go":         {offenders: 5, max: 20},
	"internal/module/navigation/inbound/http/navigation_translations_handle.go": {offenders: 1, max: 8},
	"internal/module/order/inbound/http/coupon_page.go":                         {offenders: 2, max: 29},
	"internal/module/order/inbound/http/order_create_page.go":                   {offenders: 2, max: 11},
	"internal/module/order/inbound/http/order_handle.go":                        {offenders: 12, max: 13},
	"internal/module/order/inbound/http/order_list_page.go":                     {offenders: 1, max: 23},
	"internal/module/order/inbound/http/return_page.go":                         {offenders: 1, max: 23},
	"internal/module/order/inbound/http/sales_page.go":                          {offenders: 1, max: 22},
	"internal/module/page/inbound/http/page_langs_handle.go":                    {offenders: 1, max: 11},
	"internal/module/page/inbound/http/page_redirect.go":                        {offenders: 1, max: 10},
	"internal/module/page/inbound/http/page_schedule_handle.go":                 {offenders: 3, max: 10},
	"internal/module/page/inbound/http/page_site_slot_handle.go":                {offenders: 2, max: 18},
	"internal/module/page/inbound/http/page_translations_handle.go":             {offenders: 3, max: 52},
	"internal/module/page/inbound/http/pages_handle.go":                         {offenders: 8, max: 25},
	"internal/module/plugin/inbound/http/plugin_handle.go":                      {offenders: 1, max: 8},
	"internal/module/plugin/inbound/http/plugin_page_handle.go":                 {offenders: 2, max: 10},
	"internal/module/project/inbound/http/project_page.go":                      {offenders: 11, max: 49},
	"internal/module/sysconfig/inbound/http/sysconfig_admin_pages.go":           {offenders: 2, max: 19},
	"internal/module/user/inbound/http/customer_bulk_write.go":                  {offenders: 2, max: 21},
	"internal/module/user/inbound/http/user_customer_admin_handle.go":           {offenders: 3, max: 10},
	"internal/module/user/inbound/http/user_page.go":                            {offenders: 14, max: 31},
	"internal/module/workbench/inbound/http/workbench_handle.go":                {offenders: 1, max: 9},
	// 工作台画布逻辑按域拆成 5 个文件（原先是一个 2538 行的 workbench_page.go）。
	// 登记按**文件**记，所以拆分后要跟着拆：offenders 是该文件里超限的函数数，
	// max 是其中最大的语句数。拆分本身不改变这些函数的大小 —— 豁免不是拆分换来的，
	// 拆分只是为了下一步逐个把装配下沉到 service 读模型时能分开做。
	"internal/module/workbench/inbound/http/inspector_handle.go":       {offenders: 2, max: 10},  // InspectorPanel=10, OutlineTree=7
	"internal/module/workbench/inbound/http/canvas_handle.go":          {offenders: 1, max: 29},  // Workbench=29
	"internal/module/workbench/inbound/http/canvas_preview_handle.go":  {offenders: 3, max: 15},  // InstanceSave=15, TemplatePreviewDraft=12, TemplatePreview=10
	"internal/module/workbench/inbound/http/canvas_bridge_handle.go":   {offenders: 1, max: 9},   // PreviewDraft=9
}

// controllerHandlerStatements 每个 handler 的语句数（键是「文件:函数」，只收参数恰好一个 *gin.Context 的函数）。
func controllerHandlerStatements(t *testing.T) (perFile map[string][]int, offenders map[string][]string) {
	t.Helper()
	root := repoRoot(t)
	perFile = map[string][]int{}
	offenders = map[string][]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && skipArchScanDir(info.Name()) {
			return filepath.SkipDir
		}
		if info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		if !strings.Contains(p, "/inbound/http/") {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		fset := token.NewFileSet()
		af, perr := parser.ParseFile(fset, p, nil, 0)
		if perr != nil {
			return fmt.Errorf("解析 %s: %w", rel, perr)
		}
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Type.Params == nil || len(fd.Type.Params.List) != 1 {
				continue
			}
			if !isGinContext(fd.Type.Params.List[0].Type) {
				continue
			}
			n := workStatements(fd.Body)
			perFile[rel] = append(perFile[rel], n)
			if n > controllerStatementBudget {
				offenders[rel] = append(offenders[rel], fmt.Sprintf("%s=%d", fd.Name.Name, n))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫描控制器失败：%v", err)
	}
	return perFile, offenders
}

// isGinContext 参数类型是不是 *gin.Context。
func isGinContext(e ast.Expr) bool {
	star, ok := e.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == "gin" && sel.Sel.Name == "Context"
}

// workStatements 「干活」的语句数：赋值 / 表达式 / 循环 / 声明 / defer / go / send / 自增。
//
// 不数 if / switch / return / break 这些骨架：任何入口都要有它们，数进来只会让预算变成
// 「谁能把错误处理写得更挤」。留下的是**这个入口自己做了多少事** —— 拼视图、算格式、
// 逐行映射都会以赋值与循环的形状暴露出来。
func workStatements(n ast.Node) int {
	c := 0
	ast.Inspect(n, func(x ast.Node) bool {
		switch x.(type) {
		case *ast.AssignStmt, *ast.ExprStmt, *ast.ForStmt, *ast.RangeStmt, *ast.DeclStmt,
			*ast.GoStmt, *ast.DeferStmt, *ast.SendStmt, *ast.IncDecStmt:
			c++
		}
		return true
	})
	return c
}

// TestControllerHandlersStayOrchestrationOnly 一个 handler ≤6 条语句；存量按文件双向对账。
func TestControllerHandlersStayOrchestrationOnly(t *testing.T) {
	_, offenders := controllerHandlerStatements(t)

	keys := make([]string, 0, len(offenders))
	for k := range offenders {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, file := range keys {
		want, ok := pendingControllerStatements[file]
		if !ok {
			t.Errorf("%s: handler 超过 %d 条语句（%s） —— 控制器只做绑定 → 调业务层 → 出口；"+
				"装配与派生值下沉到 service 读模型（判据见 AGENTS.md 分层表）",
				file, controllerStatementBudget, strings.Join(offenders[file], ", "))
			continue
		}
		if want.offenders != len(offenders[file]) {
			t.Errorf("%s: 超限函数从 %d 个变成 %d 个（%s） —— 改好一个就删登记；理由写进提交信息",
				file, want.offenders, len(offenders[file]), strings.Join(offenders[file], ", "))
		}
	}

	for file, want := range pendingControllerStatements {
		if len(offenders[file]) == 0 {
			t.Errorf("%s: 登记了 %d 个超限函数但已不再命中 —— 删掉这条登记", file, want.offenders)
		}
	}
}

// TestControllerHandlerStatementCeilingDrops 登记表里的 max 只能下降：上升说明某个 handler 又变胖了。
func TestControllerHandlerStatementCeilingDrops(t *testing.T) {
	perFile, offenders := controllerHandlerStatements(t)
	for file, want := range pendingControllerStatements {
		peak := 0
		for _, n := range perFile[file] {
			if n > peak {
				peak = n
			}
		}
		if peak > want.max {
			t.Errorf("%s: 最大语句数从 %d 涨到 %d（%s）", file, want.max, peak, strings.Join(offenders[file], ", "))
		}
		if peak < want.max && len(offenders[file]) > 0 {
			t.Errorf("%s: 最大语句数已降到 %d，登记还是 %d —— 同步下调", file, peak, want.max)
		}
	}
}
