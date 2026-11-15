package architecture

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

var templateKeyCall = regexp.MustCompile("(?:\\[\"tf?\"\\]|[A-Za-z]*[Tt]r)\\(\\s*\"([^\"]*)\"")

var templateComment = regexp.MustCompile("(?s)<!--.*?-->|\\{\\*.*?\\*\\}")

var uiKeyPrefixes = []string{"admin.", "shell.", "user.", "workbench.", "site.", "msg_"}

// flatUIKey 是 AGENTS.md 规定的扁平短 key 形态：ASCII 字母数字、首字符字母、长度 1–15。
var flatUIKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,14}$`)

// uiKeyAllowed 认两种合法形态：
//  1. 带域前缀的点分 key（历史形态，仍在用）；
//  2. 扁平短 key（现行规则：一个 key 表达完整语义，分类走 category 字段）。
func uiKeyAllowed(key string) bool {
	if strings.HasPrefix(key, "Msg") {
		return true
	}
	for _, prefix := range uiKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return flatUIKey.MatchString(key)
}

func TestTemplateTranslateKeysStayInUIDomain(t *testing.T) {
	root := filepath.Join(repoRoot(t), "internal", "templates")

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

var errorConstantName = regexp.MustCompile("^Err[A-Z]")

var pendingMsgTextDomain = map[string]int{
	"internal/module/plugin/inbound/http/plugin_err.go":                  13,
	"internal/module/page/inbound/http/page_translations_handle.go":      2,
	"internal/module/navigation/inbound/http/navigation_error_handle.go": 2,
	"internal/module/user/inbound/http/user_page.go":                     1,
	"internal/module/page/inbound/http/pages_handle.go":                  1,
	"internal/module/mail/inbound/http/mail_err.go":                      1,
}

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

func TestMsgKeysStayInUIDomain(t *testing.T) {
	root := filepath.Join(repoRoot(t), "internal", "module")
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
