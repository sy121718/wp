package productenums

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"go_wp/pkg/facing"
)

var flatTranslationKey = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,14}$`)

func isTranslationConstant(name string) bool {
	for _, prefix := range []string{"Err", "Detail", "Msg", "VariantSkip", "BundleMember", "Bulk", "ProductTranslations", "ProductsAction"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func TestTranslationKeysAreCompleteShortLiterals(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	count := 0
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range parsed.Decls {
			group, ok := decl.(*ast.GenDecl)
			if !ok || group.Tok != token.CONST {
				continue
			}
			for _, spec := range group.Specs {
				value := spec.(*ast.ValueSpec)
				for i, name := range value.Names {
					if !isTranslationConstant(name.Name) {
						continue
					}
					count++
					if i >= len(value.Values) {
						t.Errorf("%s 必须声明完整词条字面量", name.Name)
						continue
					}
					literal, ok := value.Values[i].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						t.Errorf("%s 不能拼接或间接构造词条 key", name.Name)
						continue
					}
					key, err := strconv.Unquote(literal.Value)
					if err != nil {
						t.Fatal(err)
					}
					if !flatTranslationKey.MatchString(key) {
						t.Errorf("%s=%q：词条 key 必须为 1–15 位字母数字，首位字母", name.Name, key)
					}
					if previous, exists := seen[key]; exists {
						t.Errorf("%s 与 %s 重复声明 %q", previous, name.Name, key)
					}
					seen[key] = name.Name
				}
			}
		}
	}
	if count == 0 {
		t.Fatal("没有检查到词条声明")
	}
}

func TestShortBusinessKeysUseExplicitFacts(t *testing.T) {
	forEachStringConst(t, func(name, key string) {
		if !isErrConstant(name) {
			return
		}
		err := fmt.Errorf("private SQLSTATE: %w", Err(key))
		fact, ok := facing.FactOf(err)
		if !ok || !fact.Business || fact.Key != key || fact.Tail != "" {
			t.Errorf("%s 包装后未保留安全错误事实: %+v", name, fact)
		}
		want := StatusFor(key)
		if status, ok := facing.StatusOf(err); status != want || ok != (want > 0) {
			t.Errorf("%s 包装后状态声明改变: %d/%t，期望 %d", name, status, ok, want)
		}
	})
	if _, ok := facing.FactOf(errors.New("privateFailure")); ok {
		t.Fatal("内部短字符串不得仅凭形状成为对外业务错误")
	}
}
