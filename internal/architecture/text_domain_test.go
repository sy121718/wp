package architecture

// text_domain_test.go — 文案域的两条门禁（见 AGENTS.md「响应与错误处理」）。
//
// 分域的理由：**响应文案**由 pkg/response 走 pkg/facing.FactOf 产出（{code,message,data}，
// key 是模块错误事实），**界面文案**由 internal/text 在渲染期取词（t / tf，key 是 UI 词条）。
// 两条链都读同一张 sys_i18n，混用的代价当下很轻（都能取到词或回落到兜底），但后果是把错误
// 事实当成了界面词条 —— 换个措辞、改个 key、或让某个 key 改由别的模块提供时，页面上就静默
// 换了一句话，没人报错。所以用会变红的判据把它们分开：
//
//  1. 模板里取词的 key 必须落在界面文案域：前缀 admin / shell / user / workbench / site，
//     老命名空间的 msg_，或归口文案的 Msg*；不得写成错误常量的名字（"ErrInternal" 这种），
//     也不得写模块错误 key（<module>.err.*，shell.err.* 除外 —— 那是壳自己的界面词条）。
//  2. 控制器里构造 facing.Msg 时，Key 不得是 <pkg>enums.Err* 常量或 <module>.err.* 字面量。
//
// 第 2 条带登记清单（其余模块待迁），第 1 条已是零容忍。

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// templateKeyCall 模板里取词的两种写法：
//   - {{ .["t"]("key", "兜底") }} / {{ .["tf"](...) }}（map 根索引）
//   - {{tr("key", "兜底")}} / {{gfTr(...)}} 等（先 let 成变量再调用）
var templateKeyCall = regexp.MustCompile("(?:\\[\"tf?\"\\]|[A-Za-z]*[Tt]r)\\(\\s*\"([^\"]*)\"")

// templateComment 注释里的示例不算代码：HTML 注释与 Jet 注释先整段抠掉。
var templateComment = regexp.MustCompile("(?s)<!--.*?-->|\\{\\*.*?\\*\\}")

// uiKeyPrefixes 界面文案的 key 前缀，每个都对应 sys_i18n 里真实存在的命名空间
// （site.* 属于访问面片段的词条，msg_* 是老命名空间 pkg/enums/messages.go 的词条）。
var uiKeyPrefixes = []string{"admin.", "shell.", "user.", "workbench.", "site.", "msg_"}

// uiKeyAllowed 判一个取词 key 是否落在界面文案域。
func uiKeyAllowed(key string) bool {
	if strings.HasPrefix(key, "Msg") {
		// 归口文案的 key（MsgInternalError 等）：它们是 sys_i18n 里 scope=ui 的界面词条，
		// 只是沿用大写开头的老命名，不是错误事实。
		return true
	}
	for _, prefix := range uiKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// TestTemplateTranslateKeysStayInUIDomain 模板取词的 key 只能来自界面文案域。
func TestTemplateTranslateKeysStayInUIDomain(t *testing.T) {
	root := filepath.Join("..", "templates")

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if ext := filepath.Ext(path); ext != ".html" && ext != ".jet" {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(root, path)
		code := templateComment.ReplaceAllString(string(body), "")
		for _, m := range templateKeyCall.FindAllStringSubmatch(code, -1) {
			key := m[1]
			if key == "" || uiKeyAllowed(key) {
				continue
			}
			t.Errorf("%s 用错误 key 取界面词条：%q（界面文案前缀只能是 admin./shell./user./workbench./site.、msg_ 或 Msg* 归口 key）", rel, key)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历模板目录失败: %v", err)
	}
}

// errorConstantName 判一个标识符是不是错误常量（ErrInvalidParam 这种）。
var errorConstantName = regexp.MustCompile("^Err[A-Z]")

// pendingMsgTextDomain 存量：控制器文件 → 仍用错误 key 当界面文案的处数。
//
// 记处数而不是布尔量：删掉一处却没同步改数字会立刻变红 —— 否则「改了一点」与「一点没改」
// 在清单上长得一样。product 已清零，其余模块按批迁。
var pendingMsgTextDomain = map[string]int{
	"internal/module/plugin/inbound/http/plugin_err.go":                  13,
	"internal/module/ai/inbound/http/ai_page.go":                         3,
	"internal/module/page/inbound/http/page_translations_handle.go":      2,
	"internal/module/navigation/inbound/http/navigation_error_handle.go": 2,
	"internal/module/user/inbound/http/user_page.go":                     1,
	"internal/module/publication/inbound/http/seo_audit_result.go":       1,
	"internal/module/page/inbound/http/pages_handle.go":                  1,
	"internal/module/page/inbound/http/page_schedule_handle.go":          1,
	"internal/module/mail/inbound/http/mail_err.go":                      1,
	"internal/module/admin/inbound/http/admin_err.go":                    1,
}

// msgKeyIsErrorFact 判一个 Msg.Key 的取值表达式是不是错误事实。
func msgKeyIsErrorFact(value ast.Expr) bool {
	switch v := value.(type) {
	case *ast.SelectorExpr:
		return errorConstantName.MatchString(v.Sel.Name)
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return false
		}
		key, err := strconv.Unquote(v.Value)
		if err != nil {
			return false
		}
		return strings.Contains(key, ".err.") && !strings.HasPrefix(key, "shell.")
	}
	return false
}

// TestMsgKeysStayInUIDomain 控制器构造界面提示时不得拿错误 key 当文案 key。
func TestMsgKeysStayInUIDomain(t *testing.T) {
	root := filepath.Join("..", "module")
	remaining := map[string]int{}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if !strings.Contains(filepath.ToSlash(path), "/inbound/http/") {
			return nil
		}
		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		fromRoot, _ := filepath.Rel(root, path)
		rel := filepath.ToSlash(filepath.Join("internal", "module", fromRoot))
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if ident, ok := kv.Key.(*ast.Ident); !ok || ident.Name != "Key" {
					continue
				}
				if !msgKeyIsErrorFact(kv.Value) {
					continue
				}
				remaining[rel]++
				if budget, ok := pendingMsgTextDomain[rel]; ok && remaining[rel] <= budget {
					continue
				}
				t.Errorf("%s:%d 用错误 key 当界面文案（Key 应取 admin./shell. 等界面词条或 text.InternalKey）", rel, fset.Position(kv.Pos()).Line)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("遍历控制器目录失败: %v", err)
	}
	for file, budget := range pendingMsgTextDomain {
		switch {
		case remaining[file] == 0:
			t.Errorf("%s 已不再用错误 key 当界面文案，请从 pendingMsgTextDomain 删掉这条", file)
		case remaining[file] < budget:
			t.Errorf("%s 的混用处数已降到 %d（登记 %d），请同步改小清单里的数字", file, remaining[file], budget)
		}
	}
}
