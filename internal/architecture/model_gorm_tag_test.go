// Package architecture 存放跨模块边界与全仓结构红线。
//
// 这些测试不验证行为，只验证「写下来就长期成立」的结构约定：一条越界不会让任何用例变红，
// 却会在下一次改动时以「为什么这里还有一份」的形式收利息。
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

// inferableGoTypes gorm（配合 PG 驱动）能自行推断列型的 Go 类型。
// 挂在它们上面的 gorm type: 声明不是「多一层保险」，而是第二份列型真相。
var inferableGoTypes = map[string]bool{
	"string": true, "bool": true, "byte": true, "rune": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"float32": true, "float64": true,
	"time.Time": true, "gorm.DeletedAt": true,
}

// TestModelMustNotDeclareInferableColumnType 可推断类型上不得声明列类型（type:xxx）。
//
// 列的类型由迁移决定（AGENTS.md「数据库」）：在 model 里再声明一次就是两份真相。
// 抄错时没有任何东西会报错 —— AdminEntity.Status 的真实列是 smallint，标签写着 tinyint(4)；
// 时间列的真实类型是 timestamptz(6)，标签写着 timestamp(3)（无时区 + 毫秒）。更要紧的是：
// 任何 AutoMigrate 路径都会照标签把错误的列型建出来，测试 schema 与生产因此静默分叉
// （publication 手抄 DDL 停在旧列名、presentation 缺列，都是同一类问题）。
//
// 这批声明于 2026-09 清掉 598 处（含 57 处 type:timestamp(3) 与 17 处 type:datetime(3)）——
// 上一轮只清了时间列那 57 处、没有测试兜底，随后又长了回来，所以这里钉死。
//
// 例外是类型映射：字段类型不在上表里时（json.RawMessage / JSONMap / StringArray 等），
// gorm 无法自行判断怎么落库，必须由 type:（或 serializer:）指明 —— 那表达的是「Go 值怎么
// 变成 SQL 值」，不是列型真相，删掉会直接报 unsupported data type。其余 gorm 键
// （column / primaryKey / index / uniqueIndex / not null / default / size / autoCreateTime /
// autoUpdateTime）表达约束与语义，不受本测试约束。
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

// declaredColumnTypes 取出 gorm 标签里 type: 键的值（正常只有一个，多个也算违规）。
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

// goTypeName 把字段类型表达式还原为可比较的名字（去指针与切片）。
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

// fieldNames 拼出字段名列表（一条声明可能带多个名字）。
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
