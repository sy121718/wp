package architecture

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

var pendingControllerStatements = map[string]struct{ offenders, max int }{
	"internal/module/admin/inbound/http/admin_handle.go":                        {offenders: 3, max: 13},
	"internal/module/admin/inbound/http/admin_page.go":                          {offenders: 14, max: 15},
	"internal/module/ai/inbound/http/ai_handle.go":                              {offenders: 6, max: 26},
	"internal/module/analytics/inbound/http/analytics_handle.go":                {offenders: 1, max: 7},
	"internal/module/analytics/inbound/http/analytics_page.go":                  {offenders: 2, max: 20},
	"internal/module/block/inbound/http/block_handle.go":                        {offenders: 2, max: 8},
	"internal/module/block/inbound/http/block_page.go":                          {offenders: 3, max: 16},
	"internal/module/cart/inbound/http/cart_router.go":                          {offenders: 1, max: 11},
	"internal/module/comment/inbound/http/comment_page.go":                      {offenders: 1, max: 28},
	"internal/module/content/inbound/http/content_handle.go":                    {offenders: 2, max: 8},
	"internal/module/content/inbound/http/content_page.go":                      {offenders: 10, max: 51},
	"internal/module/contenttemplate/inbound/http/contenttemplate_handle.go":    {offenders: 1, max: 8},
	"internal/module/contenttemplate/inbound/http/contenttemplate_page.go":      {offenders: 2, max: 19},
	"internal/module/inventory/inbound/http/inventory_page_handle.go":           {offenders: 1, max: 37},
	"internal/module/inventory/inbound/http/inventory_reason_handle.go":         {offenders: 2, max: 9},
	"internal/module/inventory/inbound/http/inventory_warehouse_handle.go":      {offenders: 1, max: 8},
	"internal/module/mail/inbound/http/mail_page_handle.go":                     {offenders: 8, max: 19},
	"internal/module/mail/inbound/http/mail_contact_write.go":                   {offenders: 3, max: 19},
	"internal/module/mail/inbound/http/mail_handle.go":                          {offenders: 1, max: 9},
	"internal/module/masterdata/inbound/http/masterdata_page.go":                {offenders: 1, max: 42},
	"internal/module/media/inbound/http/media_handle.go":                        {offenders: 1, max: 8},
	"internal/module/membership/inbound/http/membership_page.go":                {offenders: 2, max: 31},
	"internal/module/navigation/inbound/http/navigation_page_handle.go":         {offenders: 5, max: 20},
	"internal/module/navigation/inbound/http/navigation_translations_handle.go": {offenders: 1, max: 8},
	"internal/module/order/inbound/http/coupon_page.go":                         {offenders: 2, max: 29},
	"internal/module/order/inbound/http/order_create_page.go":                   {offenders: 1, max: 10},
	"internal/module/order/inbound/http/order_handle.go":                        {offenders: 12, max: 10},
	"internal/module/order/inbound/http/order_list_page.go":                     {offenders: 1, max: 15},
	"internal/module/order/inbound/http/return_page.go":                         {offenders: 1, max: 23},
	"internal/module/order/inbound/http/sales_page.go":                          {offenders: 1, max: 22},
	"internal/module/page/inbound/http/page_langs_handle.go":                    {offenders: 1, max: 10},
	"internal/module/page/inbound/http/page_translations_handle.go":             {offenders: 3, max: 51},
	"internal/module/page/inbound/http/pages_handle.go":                         {offenders: 4, max: 17},
	"internal/module/plugin/inbound/http/plugin_handle.go":                      {offenders: 1, max: 8},
	"internal/module/plugin/inbound/http/plugin_page_handle.go":                 {offenders: 2, max: 13},
	// 9 → 8：三块编辑器面板改显式 JSON 绑定后，workbenchSettingsPanel 视图构造
	// 抽到 settingsPanelViewOf，语句数回到预算内。
	"internal/module/project/inbound/http/project_page.go":            {offenders: 8, max: 45},
	"internal/module/sysconfig/inbound/http/sysconfig_admin_pages.go": {offenders: 2, max: 13},
	"internal/module/user/inbound/http/customer_bulk_write.go":        {offenders: 1, max: 8},
	"internal/module/user/inbound/http/user_customer_admin_handle.go": {offenders: 1, max: 10},
	"internal/module/user/inbound/http/user_page.go":                  {offenders: 5, max: 31},
	"internal/module/workbench/inbound/http/workbench_handle.go":      {offenders: 1, max: 8},

	"internal/module/workbench/inbound/http/inspector_handle.go":      {offenders: 1, max: 8},
	"internal/module/workbench/inbound/http/canvas_handle.go":         {offenders: 1, max: 29},
	"internal/module/workbench/inbound/http/canvas_preview_handle.go": {offenders: 3, max: 15},
	"internal/module/workbench/inbound/http/canvas_bridge_handle.go":  {offenders: 1, max: 10},
}

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

			if isObjectQueryPipeline(fd.Body) {
				n = 3
			}
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

func workStatements(n ast.Node) int {
	skip, _ := bindPreambleNodes(n)
	c := 0
	ast.Inspect(n, func(x ast.Node) bool {
		if skip[x] {
			return false
		}
		switch x.(type) {
		case *ast.AssignStmt, *ast.ExprStmt, *ast.ForStmt, *ast.RangeStmt, *ast.DeclStmt,
			*ast.GoStmt, *ast.DeferStmt, *ast.SendStmt, *ast.IncDecStmt:
			c++
		}
		return true
	})
	return c
}

// bindPreambleNodes 标出「原生显式绑定 + 标准失败出口」这段固定前言。
//
// 控制器规范要求请求先绑定成 DTO、失败立即交共享失败出口；这段形状是判据强制的
// 声明，不是业务编排。把它计入语句预算会让「内联绑定」与「绑定薄包装」二选一地
// 违反判据 —— 前者超预算、后者是新增包装。这里把它整体排除在业务语句计数之外，
// 既保留「不新增绑定包装」，也不迫使控制器为了省语句去手工读字段。
func bindPreambleNodes(n ast.Node) (map[ast.Node]bool, int) {
	skip := map[ast.Node]bool{}
	count := 0
	ast.Inspect(n, func(x ast.Node) bool {
		block, ok := x.(*ast.BlockStmt)
		if !ok {
			return true
		}
		for i, stmt := range block.List {
			ifStmt, okIf := stmt.(*ast.IfStmt)
			if okIf && isBindFailureGuard(ifStmt) {
				skip[ifStmt] = true
				count++
				continue
			}
			// 分离式形状：先 `x, err := <显式绑定>`，紧接着 `if err != nil { return }`。
			// 两段同属绑定前言，整体排除。
			if !okIf && i+1 < len(block.List) && isExplicitBindAssign(stmt) {
				guard, okGuard := block.List[i+1].(*ast.IfStmt)
				if okGuard && isErrReturnGuard(guard) {
					skip[stmt] = true
					skip[guard] = true
					count++
				}
			}
		}
		return true
	})
	return skip, count
}

// isBindFailureGuard 判断一个 if 是否是「显式绑定失败 → 立即返回」的固定形状。
func isBindFailureGuard(stmt *ast.IfStmt) bool {
	if stmt.Init == nil || stmt.Body == nil || len(stmt.Body.List) == 0 {
		return false
	}
	assign, ok := stmt.Init.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || len(assign.Rhs) != 1 {
		return false
	}
	// 两种固定前言形状：
	//   err := c.ShouldBindWith(&req, FormPost)       —— 原生 binder 只返回 error
	//   req, err := binding.ParseForm[T](c)           —— 泛型入口返回 (DTO, error)
	// 两者都是「绑定成 DTO + 失败立即返回」，属于判据强制的声明，不占业务语句预算。
	var errIdent *ast.Ident
	switch len(assign.Lhs) {
	case 1:
		ident, okLHS := assign.Lhs[0].(*ast.Ident)
		if !okLHS {
			return false
		}
		errIdent = ident
	case 2:
		if _, okFirst := assign.Lhs[0].(*ast.Ident); !okFirst {
			return false
		}
		ident, okLHS := assign.Lhs[1].(*ast.Ident)
		if !okLHS {
			return false
		}
		errIdent = ident
	default:
		return false
	}
	if errIdent.Name != "err" {
		return false
	}
	cond, ok := stmt.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.NEQ {
		return false
	}
	left, ok := cond.X.(*ast.Ident)
	if !ok || left.Name != "err" {
		return false
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok || !isExplicitBindCall(call) {
		return false
	}
	// 出口必须是 return：显式绑定失败不能继续往下走。
	last := stmt.Body.List[len(stmt.Body.List)-1]
	_, ret := last.(*ast.ReturnStmt)
	return ret
}

// isExplicitBindAssign 判断一条语句是否是「显式绑定」赋值：
//
//	req, err := binding.ParseForm[T](c)
//	err := c.ShouldBindWith(&req, FormPost)
//
// 返回 (DTO, error) 或只返回 error 都算；自动合并的 ShouldBind / Bind 不算。
func isExplicitBindAssign(stmt ast.Stmt) bool {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE || len(assign.Rhs) != 1 {
		return false
	}
	call, okCall := assign.Rhs[0].(*ast.CallExpr)
	return okCall && isExplicitBindCall(call)
}

// isErrReturnGuard 判断 if 是否是 `if err != nil { … return }` 的固定失败出口。
func isErrReturnGuard(stmt *ast.IfStmt) bool {
	if stmt.Init != nil || stmt.Else != nil || stmt.Body == nil || len(stmt.Body.List) == 0 {
		return false
	}
	cond, ok := stmt.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.NEQ {
		return false
	}
	left, okLeft := cond.X.(*ast.Ident)
	if !okLeft || left.Name != "err" {
		return false
	}
	_, ret := stmt.Body.List[len(stmt.Body.List)-1].(*ast.ReturnStmt)
	return ret
}

// isExplicitBindCall 只看显式 binder：原生方法，或 pkg/binding 的
// ParseForm / ParseJSON / ParseQuery 泛型入口；不含自动合并的 ShouldBind / Bind。
func isExplicitBindCall(call *ast.CallExpr) bool {
	fun := call.Fun
	// binding.ParseForm[T](c)：泛型调用的被调方是 IndexExpr / IndexListExpr。
	switch node := fun.(type) {
	case *ast.IndexExpr:
		fun = node.X
	case *ast.IndexListExpr:
		fun = node.X
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "ShouldBindWith", "ShouldBindJSON", "ShouldBindQuery",
		"ParseForm", "ParseJSON", "ParseQuery":
		return true
	}
	return false
}

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
				"业务聚合归 service、展示归模板，现成对象可逐份读取（判据见 AGENTS.md 分层表）",
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
