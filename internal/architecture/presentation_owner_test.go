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

// dtoPresentationFields 是「后端替界面拼好的成品展示」字段。
//
// 判据是是否已取词：`StatusLabel`（成品译文）算违规；`LabelKey` / `LabelFallback`
// 是「词条 key + 缺词兜底」，交给模板按请求语言取词，属于正确形态，不在名单内。
var dtoPresentationFields = map[string]struct{}{
	"Tone": {}, "Class": {}, "StatusClass": {}, "StatusLabel": {},
	"TypeLabel": {}, "KindLabel": {},
	"LabelText": {}, "DisplayText": {}, "DisplayName": {},
}

var pendingDTOPresentation = map[string]int{
	"internal/module/ai/dto/ai_req.go":                1,
	"internal/module/ai/dto/ai_resp.go":               2,
	"internal/module/comment/dto/comment_resp.go":     1,
	"internal/module/order/dto/order_resp.go":         1,
	"internal/module/order/dto/order_return_resp.go":  1,
	"internal/module/user/dto/customer_admin_resp.go": 3,

	"internal/module/ai/dto/ai_page_resp.go": 1,

	"internal/module/ai/dto/ai_session_page_resp.go": 1,
	"internal/module/user/dto/user_resp.go":          1,
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

var moduleTextFuncs = map[string]struct{}{
	"Translate": {}, "TranslateFunc": {}, "FillTranslate": {}, "TranslateArgs": {},
}

var pendingModuleText = map[string]int{
	"internal/module/block/inbound/http/block_err.go":                  1,
	"internal/module/content/inbound/http/content_page.go":             1,
	"internal/module/inventory/inbound/http/inventory_error_handle.go": 1,
	"internal/module/purchase/inbound/http/purchase_shared_handle.go":  1,
	"internal/module/inventory/inbound/mcp/inventory_tools.go":         1,
	"internal/module/mail/inbound/http/mail_err.go":                    1,
	"internal/module/mail/inbound/http/mail_handle.go":                 2,
	"internal/module/mail/service/mail_account.go":                     3,
	// 打包下载把 README.txt 写进 zip 字节流，物理上不经过 Jet 模板；
	// 这是全域唯一允许 service 取词的地方（理由见该文件头）。
	"internal/module/media/service/media_export_text.go":      9,
	"internal/module/order/inbound/http/order_handle.go":      1,
	"internal/module/page/inbound/http/page_jump.go":          1,

	"internal/module/workbench/inbound/http/canvas_bridge_handle.go":  1,
	"internal/module/workbench/inbound/http/canvas_preview_handle.go": 1,
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

// 三种取词形态都要认：全局 t / tf / tn、点作用域 .["t"] / .["tf"] / .["tn"]，以及别名 tr。
var templateTranslateCall = regexp.MustCompile(`\["tf?"\]\(|\["tn"\]\(|\btr\(|\btf?\(|\btn\(`)

// templateNotWholePage 识别**片段**：它们由写出端点单独渲染，不经整页布局，
// 取词由发起页面统一负责。`_result` 一类是写操作的结果片段，同属此列。
var templateNotWholePage = regexp.MustCompile(`(^|/)(partials|fragments)/|_(form|drawer|options|fields|text|rows|panel|notice|hits|card|values|group|result|saved|created|appended|switch|switch_result|row)\.html$`)

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

var allowedPkgDirs = map[string]struct{}{
	"actor": {}, "auth": {}, "backpath": {}, "binding": {}, "bulkids": {}, "cache": {},
	"captcha": {}, "casbin": {}, "crypto": {}, "database": {}, "datarule": {},
	"enums": {}, "facing": {}, "i18n": {}, "jsonx": {}, "logger": {},
	"mailer": {}, "money": {}, "pagination": {}, "pathkit": {}, "queue": {},
	"reqparams": {}, "response": {}, "rls": {}, "safepath": {}, "sitehttps": {},
	"sitetz": {}, "ssrf": {}, "tree": {}, "upload": {}, "utils": {}, "validate": {},
	"view": {}, "middleware": {}, "seo": {}, "uispec": {}, "builder": {}, "lifecycle": {}, "mcp": {}, "publish": {}, "scaffold": {},
}

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
