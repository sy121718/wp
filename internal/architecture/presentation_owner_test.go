package architecture

// presentation_owner_test.go — 展示归属的四道全仓门禁（PLAN 阶段 2）。
//
// 判据只有一句：**V 之外不出现「给用户看的字符串」与「字符串的形状」**。
// 落到四个可判定的位置：dto 不带展示字段、模块不取词、整页模板自己取词、pkg 不长出页面助手。
//
// 四条都按双向台账判（assertBudget）：未登记的违规失败，登记了却不再命中同样失败 —— 存量只减不增。

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

// dtoPresentationFields 展示语义的字段名（形状或成品的字符串）。
var dtoPresentationFields = map[string]struct{}{
	"Tone": {}, "Class": {}, "StatusClass": {}, "StatusLabel": {},
	"TypeLabel": {}, "KindLabel": {}, "LabelKey": {}, "LabelFallback": {},
	"TextKey": {}, "LabelText": {}, "DisplayText": {}, "DisplayName": {},
}

// pendingDTOPresentation 存量：dto 文件 → 命中的字段/方法数。
var pendingDTOPresentation = map[string]int{
	"internal/module/ai/dto/ai_req.go":                     1,
	"internal/module/ai/dto/ai_resp.go":                    2,
	"internal/module/ai/dto/ai_token_resp.go":              1,
	"internal/module/comment/dto/comment_resp.go":          1,
	"internal/module/inventory/dto/inventory_page_resp.go": 8,
	"internal/module/order/dto/order_resp.go":              1,
	"internal/module/order/dto/order_return_resp.go":       1,
	"internal/module/user/dto/customer_admin_resp.go":      3,
	"internal/module/user/dto/user_resp.go":                1,
}

func TestDTOsCarryNoPresentationFields(t *testing.T) {
	hits := map[string]int{}
	root := repoRoot(t)
	for _, path := range goFilesUnder(t, filepath.Join(root, "internal", "module"), func(p string) bool {
		return strings.Contains(p, string(filepath.Separator)+"dto"+string(filepath.Separator)) && !strings.HasSuffix(p, "_test.go")
	}) {
		if count := presentationFieldsIn(t, path); count > 0 {
			hits[relToRoot(t, root, path)] = count
		}
	}
	assertBudget(t, hits, pendingDTOPresentation, "dto 不带展示字段")
}

func presentationFieldsIn(t *testing.T, path string) int {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("解析失败 %s: %v", path, err)
	}
	count := 0
	ast.Inspect(f, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Field:
			for _, name := range node.Names {
				if _, bad := dtoPresentationFields[name.Name]; bad {
					count++
				}
			}
		case *ast.FuncDecl:
			if node.Recv != nil && (node.Name.Name == "PageStatus" || node.Name.Name == "PageNoStore") {
				count++
			}
		}
		return true
	})
	return count
}

// moduleTextFuncs 模块内的取词调用：文案由响应出口按声明处取，页面由模板取，模块里两处都不该出现。
var moduleTextFuncs = map[string]struct{}{
	"Translate": {}, "TranslateFunc": {}, "FillTranslate": {}, "TranslateArgs": {},
}

// pendingModuleText 存量：模块相对路径 → 取词调用数。
var pendingModuleText = map[string]int{
	"internal/module/analytics/inbound/http/analytics_page.go":               2,
	"internal/module/block/inbound/http/block_err.go":                        1,
	"internal/module/comment/inbound/http/comment_handle.go":                 1,
	"internal/module/comment/inbound/http/comment_page.go":                   4,
	"internal/module/content/inbound/http/content_page.go":                   2,
	"internal/module/contenttemplate/inbound/http/contenttemplate_handle.go": 1,
	"internal/module/inventory/inbound/http/inventory_error_handle.go":       1,
	"internal/module/inventory/inbound/http/inventory_purchase_handle.go":    1,
	"internal/module/inventory/inbound/mcp/inventory_tools.go":               1,
	"internal/module/mail/inbound/http/mail_err.go":                          2,
	"internal/module/mail/inbound/http/mail_handle.go":                       2,
	"internal/module/mail/service/mail_account.go":                           3,
	"internal/module/masterdata/inbound/http/masterdata_page.go":             6,
	"internal/module/media/inbound/http/media_handle.go":                     1,
	"internal/module/media/service/media.go":                                 8,
	"internal/module/order/inbound/http/coupon_page.go":                      1,
	"internal/module/order/inbound/http/order_handle.go":                     1,
	"internal/module/order/inbound/http/order_shared.go":                     1,
	"internal/module/order/inbound/http/return_page.go":                      1,
	"internal/module/page/inbound/http/page_jump.go":                         1,
	"internal/module/plugin/service/plugin.go":                               1,
	"internal/module/runtimefragment/facing.go":                              2,
	"internal/module/sysconfig/inbound/http/sysconfig_admin_pages.go":        4,
	"internal/module/user/inbound/http/customer_bulk_write.go":               3,
	"internal/module/user/inbound/http/user_page.go":                         1,
	"internal/module/webhook/inbound/http/webhook_handle.go":                 1,
	"internal/module/workbench/inbound/http/workbench_err.go":                3,
	// 页面逻辑拆成 5 个文件后，两处取词跟着调用点走（都在预览编译的出口）。
	"internal/module/workbench/inbound/http/canvas_bridge_handle.go":   1,
	"internal/module/workbench/inbound/http/canvas_preview_handle.go":  1,
}

func TestModulesDoNotTranslate(t *testing.T) {
	hits := map[string]int{}
	root := repoRoot(t)
	for _, path := range goFilesUnder(t, filepath.Join(root, "internal", "module"), func(p string) bool {
		return !strings.HasSuffix(p, "_test.go")
	}) {
		if count := moduleTranslationsIn(t, path); count > 0 {
			hits[relToRoot(t, root, path)] = count
		}
	}
	assertBudget(t, hits, pendingModuleText, "模块内不取词")
}

func moduleTranslationsIn(t *testing.T, path string) int {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("解析失败 %s: %v", path, err)
	}
	count := 0
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if ok && ident.Name == "i18n" {
			if _, bad := moduleTextFuncs[sel.Sel.Name]; bad {
				count++
			}
		}
		return true
	})
	return count
}

// templateTranslateCall 模板取词（t / tf / tn 三种注入形态，以及 tr 别名）。
var templateTranslateCall = regexp.MustCompile(`\["tf?"\]\(|\["tn"\]\(|\btr\(`)

// templateNotWholePage 片段：数据由调用点给，文案由调用点取好 —— 不适用「页面自己取词」。
var templateNotWholePage = regexp.MustCompile(`(^|/)(partials|fragments)/|_(form|drawer|options|fields|text|rows|panel|notice|hits|card|values|group)\.html$`)

// pendingTemplateOwnText 存量：整页模板 → 取词调用数（记账的是 0 次的那几个）。
//
// ai/ui_blocks.html 是块编辑器的画布骨架：块名与占位文案都来自 JSON 数据，模板本身不取词。
var pendingTemplateOwnText = map[string]int{
	"internal/templates/admin/ai/ui_blocks.html": 0,
}

func TestWholePageTemplatesOwnTheirText(t *testing.T) {
	hits := map[string]int{}
	walkTemplates(t, filepath.Join(repoRoot(t), "internal", "templates", "admin"), func(rel, body string) {
		if templateNotWholePage.MatchString(rel) {
			return
		}
		if len(templateTranslateCall.FindAllString(body, -1)) == 0 {
			hits[rel] = 0
		}
	})
	assertBudget(t, hits, pendingTemplateOwnText, "整页模板自己取词")
}

// allowedPkgDirs pkg/ 下允许存在的包：横切能力，不是页面助手。
var allowedPkgDirs = map[string]struct{}{
	"auth": {}, "backpath": {}, "binding": {}, "bulkids": {}, "cache": {},
	"captcha": {}, "casbin": {}, "crypto": {}, "database": {}, "datarule": {},
	"enums": {}, "facing": {}, "i18n": {}, "jsonx": {}, "logger": {},
	"mailer": {}, "money": {}, "pagination": {}, "pathkit": {}, "queue": {},
	"reqparams": {}, "response": {}, "rls": {}, "safepath": {}, "sitehttps": {},
	"sitetz": {}, "ssrf": {}, "tree": {}, "upload": {}, "utils": {}, "validate": {}, "view": {},
}

// pendingPkgDirs 存量：pkg 下的目录名 → 1。
var pendingPkgDirs = map[string]int{}

func TestPkgHasNoPageHelperPackages(t *testing.T) {
	hits := map[string]int{}
	entries, err := os.ReadDir(filepath.Join(repoRoot(t), "pkg"))
	if err != nil {
		t.Fatalf("读取 pkg 失败: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, ok := allowedPkgDirs[entry.Name()]; !ok {
			hits["pkg/"+entry.Name()] = 1
		}
	}
	assertBudget(t, hits, pendingPkgDirs, "pkg 不长出页面助手包")
}

func goFilesUnder(t *testing.T, root string, keep func(string) bool) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || !keep(path) {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		t.Fatalf("遍历失败 %s: %v", root, err)
	}
	return out
}

func relToRoot(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatalf("相对路径失败 %s: %v", path, err)
	}
	return filepath.ToSlash(rel)
}
