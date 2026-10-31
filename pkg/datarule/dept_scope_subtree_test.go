package datarule

import (
	"reflect"
	"strings"
	"testing"
)

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
