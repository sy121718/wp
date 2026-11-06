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
	"internal/module/admin/inbound/http/admin_handle.go":                        {offenders: 8, max: 13},
	"internal/module/admin/inbound/http/admin_i18n_write.go":                    {offenders: 1, max: 9},
	"internal/module/admin/inbound/http/admin_page.go":                          {offenders: 29, max: 37},
	"internal/module/ai/inbound/http/ai_handle.go":                              {offenders: 5, max: 28},
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
	"internal/module/inventory/inbound/http/inventory_reason_handle.go":         {offenders: 2, max: 15},
	"internal/module/purchase/inbound/http/source_page_handle.go":               {offenders: 1, max: 11},
	"internal/module/inventory/inbound/http/inventory_stock_handle.go":          {offenders: 2, max: 9},
	"internal/module/inventory/inbound/http/inventory_warehouse_handle.go":      {offenders: 2, max: 12},
	"internal/module/mail/inbound/http/mail_contact_write.go":                   {offenders: 3, max: 26},
	"internal/module/mail/inbound/http/mail_handle.go":                          {offenders: 1, max: 10},
	"internal/module/mail/inbound/http/mail_page.go":                            {offenders: 14, max: 19},
	"internal/module/masterdata/inbound/http/masterdata_page.go":                {offenders: 1, max: 42},
	"internal/module/media/inbound/http/media_handle.go":                        {offenders: 1, max: 8},
	"internal/module/membership/inbound/http/membership_page.go":                {offenders: 2, max: 31},
	"internal/module/navigation/inbound/http/navigation_handle.go":              {offenders: 3, max: 13},
	"internal/module/navigation/inbound/http/navigation_page_handle.go":         {offenders: 5, max: 20},
	"internal/module/navigation/inbound/http/navigation_translations_handle.go": {offenders: 1, max: 8},
	"internal/module/order/inbound/http/coupon_page.go":                         {offenders: 2, max: 29},
	"internal/module/order/inbound/http/order_create_page.go":                   {offenders: 2, max: 11},
	"internal/module/order/inbound/http/order_handle.go":                        {offenders: 12, max: 13},
	"internal/module/order/inbound/http/order_list_page.go":                     {offenders: 1, max: 15},
	"internal/module/order/inbound/http/return_page.go":                         {offenders: 1, max: 23},
	"internal/module/order/inbound/http/sales_page.go":                          {offenders: 1, max: 22},
	"internal/module/page/inbound/http/page_langs_handle.go":                    {offenders: 1, max: 11},
	"internal/module/page/inbound/http/page_translations_handle.go":             {offenders: 3, max: 52},
	"internal/module/page/inbound/http/pages_handle.go":                         {offenders: 7, max: 18},
	"internal/module/plugin/inbound/http/plugin_handle.go":                      {offenders: 1, max: 8},
	"internal/module/plugin/inbound/http/plugin_page_handle.go":                 {offenders: 2, max: 10},
	"internal/module/project/inbound/http/project_page.go":                      {offenders: 11, max: 49},
	"internal/module/sysconfig/inbound/http/sysconfig_admin_pages.go":           {offenders: 2, max: 19},
	"internal/module/user/inbound/http/customer_bulk_write.go":                  {offenders: 2, max: 21},
	"internal/module/user/inbound/http/user_customer_admin_handle.go":           {offenders: 3, max: 10},
	"internal/module/user/inbound/http/user_page.go":                            {offenders: 5, max: 31},
	"internal/module/workbench/inbound/http/workbench_handle.go":                {offenders: 1, max: 9},

	"internal/module/workbench/inbound/http/inspector_handle.go":      {offenders: 2, max: 10},
	"internal/module/workbench/inbound/http/canvas_handle.go":         {offenders: 1, max: 29},
	"internal/module/workbench/inbound/http/canvas_preview_handle.go": {offenders: 3, max: 15},
	"internal/module/workbench/inbound/http/canvas_bridge_handle.go":  {offenders: 1, max: 9},
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
