package mailenums

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var mailKeyShape = regexp.MustCompile("^mail[.][a-z][a-zA-Z0-9_]*([.][a-zA-Z0-9_]+)+$")

const mailEnumMinMessageConsts = 40

func isMessageConst(name string) bool {
	for _, prefix := range []string{"Err", "Msg", "Notice"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return strings.HasPrefix(name, "TestMail") && !strings.HasSuffix(name, "Fallback")
}

func TestMailEnumKeysAreWellFormed(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("枚举本包源文件失败: %v", err)
	}

	seen := map[string]string{}
	checked := 0

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		parsed, perr := parser.ParseFile(fset, file, nil, 0)
		if perr != nil {
			t.Fatalf("解析 %s 失败: %v", file, perr)
		}
		for _, decl := range parsed.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if !isMessageConst(name.Name) || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value, uerr := strconv.Unquote(lit.Value)
					if uerr != nil {
						t.Fatalf("%s 的 %s 取值失败: %v", file, name.Name, uerr)
					}
					checked++
					if !mailKeyShape.MatchString(value) {
						t.Errorf("%s 的 %s = %q 不是合法的文案 key（期望 mail.<段>.<段>）："+
							"值就是 sys_i18n 的主键，形状不对时取词永远命中不了，页面只会回落中文兜底",
							file, name.Name, value)
					}
					if prev, dup := seen[value]; dup {
						t.Errorf("%s 的 %s 与 %s 用了同一个 key %q：两条文案共用一个词条，"+
							"改一条会连带改另一条（一条文案一个 key）", file, name.Name, prev, value)
						continue
					}
					seen[value] = name.Name
				}
			}
		}
	}

	if checked < mailEnumMinMessageConsts {
		t.Fatalf("只解析到 %d 个文案常量（阈值 %d）：文件路径或解析逻辑写错了，测试会变成空转",
			checked, mailEnumMinMessageConsts)
	}
}
