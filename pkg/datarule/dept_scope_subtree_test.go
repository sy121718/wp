package datarule

// dept_scope_subtree_test.go —— 部门范围（dept.scope:SELF_AND_CHILDREN）的两条路径。
//
// SQL 优化点：改造前 deptScopeCondition 一律生成子查询：
//
//	field IN (SELECT id FROM sys_dept WHERE (',' || ancestors || ',') LIKE ('%,' || ? || ',%') OR id = ?)
//
// 左边的 || 是**表达式**、匹配又在串中间，B-tree 索引用不上 —— 每一次带部门范围的查询都要
// 把 sys_dept 整表扫一遍。UserContext.DeptSubtreeIDs 非空时改为直接展开 field IN (?,?,...)：
// 走目标表 dept_id 上的索引，且完全没有子查询。
//
// 向后兼容是硬要求：DeptSubtreeIDs 为空（既有调用方都没设这个字段）时生成的 SQL 必须与
// 改造前**逐字相同**（既有断言见 plugin_test.go 的 TestBuildConditionDeptScopeExactMatch）。
import (
	"reflect"
	"strings"
	"testing"
)

// TestDeptScopeConditionPrefersSubtreeIN 子树已知 → IN (?,?,...)，参数个数与顺序一致。
func TestDeptScopeConditionPrefersSubtreeIN(t *testing.T) {
	t.Parallel()

	user := &UserContext{DeptID: 10, DeptSubtreeIDs: []uint64{10, 20, 30}}
	condition := Condition{Field: "dept_id", Op: "EQ", Value: "dept.scope:SELF_AND_CHILDREN"}

	for dialect, wantQuery := range map[string]string{
		"postgres": `"dept_id" IN (?,?,?)`,
		"mysql":    "`dept_id` IN (?,?,?)",
	} {
		got, ok := buildCondition(condition, user, dialect)
		if !ok {
			t.Fatalf("方言 %s 条件应当构建成功", dialect)
		}
		if got.Query != wantQuery {
			t.Fatalf("方言 %s SQL 不一致：got %q, want %q", dialect, got.Query, wantQuery)
		}
		if strings.Contains(got.Query, "sys_dept") || strings.Contains(got.Query, "LIKE") {
			t.Fatalf("方言 %s 优化路径不应包含子查询或 LIKE：%q", dialect, got.Query)
		}
		if !reflect.DeepEqual(got.Args, []any{uint64(10), uint64(20), uint64(30)}) {
			t.Fatalf("方言 %s 参数不一致：got %#v", dialect, got.Args)
		}
	}
}

// TestDeptScopeConditionSubtreeINPlaceholderCount 占位符个数必须与参数个数严格相等
// （多一个少一个都是「参数错位」这类静默错误）。
func TestDeptScopeConditionSubtreeINPlaceholderCount(t *testing.T) {
	t.Parallel()

	for _, size := range []int{1, 2, 5} {
		subtree := make([]uint64, 0, size)
		for i := 0; i < size; i++ {
			subtree = append(subtree, uint64(100+i))
		}
		user := &UserContext{DeptID: 100, DeptSubtreeIDs: subtree}
		got, ok := buildCondition(Condition{Field: "dept_id", Op: "EQ", Value: "dept.scope:SELF_AND_CHILDREN"}, user, "postgres")
		if !ok {
			t.Fatalf("size=%d 条件应当构建成功", size)
		}
		if placeholders := strings.Count(got.Query, "?"); placeholders != size {
			t.Fatalf("size=%d 占位符个数不符：query=%q", size, got.Query)
		}
		if len(got.Args) != size {
			t.Fatalf("size=%d 参数个数不符：%#v", size, got.Args)
		}
	}
}

// TestDeptScopeConditionFallsBackToSubquery 子树未知（nil 或空切片）→ 回退既有子查询。
func TestDeptScopeConditionFallsBackToSubquery(t *testing.T) {
	t.Parallel()

	wantPG := `"dept_id" IN (SELECT "id" FROM sys_dept WHERE (',' || "ancestors" || ',') LIKE ('%,' || ? || ',%') OR "id" = ?)`
	wantMy := "`dept_id` IN (SELECT `id` FROM sys_dept WHERE CONCAT(',', `ancestors`, ',') LIKE CONCAT('%,', ?, ',%') OR `id` = ?)"

	cases := map[string]*UserContext{
		"DeptSubtreeIDs 为 nil": {DeptID: 1},
		"DeptSubtreeIDs 为空切片":  {DeptID: 1, DeptSubtreeIDs: []uint64{}},
	}

	for name, user := range cases {
		cond := Condition{Field: "dept_id", Op: "EQ", Value: "dept.scope:SELF_AND_CHILDREN"}

		pg, ok := buildCondition(cond, user, "postgres")
		if !ok {
			t.Fatalf("%s：postgres 条件应当构建成功", name)
		}
		if pg.Query != wantPG {
			t.Fatalf("%s：postgres 回退 SQL 不一致：got %q, want %q", name, pg.Query, wantPG)
		}
		if !reflect.DeepEqual(pg.Args, []any{"1", uint64(1)}) {
			t.Fatalf("%s：postgres 回退参数不一致：%#v", name, pg.Args)
		}

		my, ok := buildCondition(cond, user, "mysql")
		if !ok {
			t.Fatalf("%s：mysql 条件应当构建成功", name)
		}
		if my.Query != wantMy {
			t.Fatalf("%s：mysql 回退 SQL 不一致：got %q, want %q", name, my.Query, wantMy)
		}
		if !reflect.DeepEqual(my.Args, []any{"1", uint64(1)}) {
			t.Fatalf("%s：mysql 回退参数不一致：%#v", name, my.Args)
		}
	}
}
