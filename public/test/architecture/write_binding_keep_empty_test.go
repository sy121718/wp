package architecture

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

var emptyMeansSemantic = regexp.MustCompile(`(指向空串|空串表示)`)

var pendingKeepEmptyFields = map[string]int{}

func TestKeepEmptyFieldsAreDeclared(t *testing.T) {
	root := repoRoot(t)

	fields := map[string][]string{}
	fset := token.NewFileSet()
	for _, path := range goFilesUnder(t, filepath.Join(root, "internal", "module"), func(string) bool { return true }) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range st.Fields.List {
				if field.Doc == nil || len(field.Names) == 0 {
					continue
				}
				doc := field.Doc.Text()
				if !emptyMeansSemantic.MatchString(doc) {
					continue
				}

				if !isPointerType(field.Type) {
					continue
				}

				name := formTagOf(field)
				if name == "" {
					name = lowerFirst(field.Names[0].Name)
				}
				fields[name] = append(fields[name], relDir(root, path)+":"+field.Names[0].Name)
			}
			return true
		})
	}

	tplText := readAllTemplates(t, filepath.Join(root, "internal", "templates"))
	declared := map[string]bool{}
	for _, m := range regexp.MustCompile(`data-write-keep-empty="([^"]*)"`).FindAllStringSubmatch(tplText, -1) {
		for _, name := range strings.Split(m[1], ",") {
			if n := strings.TrimSpace(name); n != "" {
				declared[n] = true
			}
		}
	}

	hits := map[string]int{}
	for name, sites := range fields {
		if declared[name] {
			continue
		}
		hits[name] = 1
		t.Logf("字段 %q 的注释标注了「空串有语义」，但模板里没有 data-write-keep-empty 声明（出处 %v）",
			name, sites)
	}

	assertBudget(t, hits, pendingKeepEmptyFields, "「空串有语义」的字段必须在模板里声明 keep-empty")
}

func formTagOf(field *ast.Field) string {
	if field.Tag == nil {
		return ""
	}
	tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
	return strings.Split(tag.Get("form"), ",")[0]
}

func isPointerType(expr ast.Expr) bool {
	_, ok := expr.(*ast.StarExpr)
	return ok
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func readAllTemplates(t *testing.T, dir string) string {
	t.Helper()
	var sb strings.Builder
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		sb.Write(b)
		sb.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatalf("读取模板失败: %v", err)
	}
	return sb.String()
}
