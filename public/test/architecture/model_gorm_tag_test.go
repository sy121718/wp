package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

var inferableGoTypes = map[string]bool{
	"string": true, "bool": true, "byte": true, "rune": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true,
	"time.Time": true, "gorm.DeletedAt": true,
}

func TestModelMustNotDeclareInferableColumnType(t *testing.T) {
	root := repoRoot(t)
	base := filepath.Join(root, "internal", "module")
	fset := token.NewFileSet()
	var offenders []string

	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		ast.Inspect(file, func(n ast.Node) bool {
			field, ok := n.(*ast.Field)
			if !ok || field.Tag == nil {
				return true
			}
			if !inferableGoTypes[goTypeName(field.Type)] {
				return true
			}
			raw, uerr := strconv.Unquote(field.Tag.Value)
			if uerr != nil {
				return true
			}
			gormTag, ok := reflect.StructTag(raw).Lookup("gorm")
			if !ok {
				return true
			}
			declared := declaredColumnTypes(gormTag)
			if len(declared) == 0 {
				return true
			}
			line := fset.Position(field.Pos()).Line
			offenders = append(offenders, fmt.Sprintf("%s:%d %s (%s) -> type:%s",
				rel, line, fieldNames(field), goTypeName(field.Type), strings.Join(declared, ",")))
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("扫描 internal/module 失败：%v", err)
	}

	for _, o := range offenders {
		t.Errorf("可推断类型不得声明列类型（列型由迁移决定）：%s", o)
	}
}

func declaredColumnTypes(gormTag string) []string {
	var out []string
	for _, part := range strings.Split(gormTag, ";") {
		key, value, found := strings.Cut(part, ":")
		if found && strings.TrimSpace(key) == "type" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func goTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return goTypeName(t.X)
	case *ast.ArrayType:
		return goTypeName(t.Elt)
	case *ast.SelectorExpr:
		return goTypeName(t.X) + "." + t.Sel.Name
	}
	return "?"
}

func fieldNames(field *ast.Field) string {
	names := make([]string, 0, len(field.Names))
	for _, n := range field.Names {
		names = append(names, n.Name)
	}
	if len(names) == 0 {
		return "(内嵌)"
	}
	return strings.Join(names, ",")
}
