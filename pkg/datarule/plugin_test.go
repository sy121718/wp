package datarule

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestBuildConditionBindsArguments(t *testing.T) {
	t.Parallel()

	user := &UserContext{DeptID: 12}
	tests := []struct {
		name      string
		condition Condition
		query     string
		args      []any
	}{
		{name: "等于", condition: Condition{Field: "status", Op: "EQ", Value: "1"}, query: "`status` = ?", args: []any{"1"}},
		{name: "大于等于", condition: Condition{Field: "id", Op: "GTE", Value: "10"}, query: "`id` >= ?", args: []any{"10"}},
		{name: "包含", condition: Condition{Field: "status", Op: "IN", Value: "1, 2"}, query: "`status` IN ?", args: []any{[]string{"1", "2"}}},
		{name: "不包含", condition: Condition{Field: "status", Op: "NOT_IN", Value: "0, 2"}, query: "`status` NOT IN ?", args: []any{[]string{"0", "2"}}},
		{name: "区间", condition: Condition{Field: "id", Op: "BETWEEN", Value: "10,20"}, query: "`id` BETWEEN ? AND ?", args: []any{"10", "20"}},
		{name: "模糊匹配", condition: Condition{Field: "username", Op: "LIKE", Value: "%admin%"}, query: "`username` LIKE ?", args: []any{"%admin%"}},
		{name: "排除模糊匹配", condition: Condition{Field: "username", Op: "NOT_LIKE", Value: "%test%"}, query: "`username` NOT LIKE ?", args: []any{"%test%"}},
		{name: "当前部门", condition: Condition{Field: "dept_id", Op: "EQ", Value: "dept.scope:SELF"}, query: "`dept_id` = ?", args: []any{uint64(12)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, ok := buildCondition(tt.condition, user)
			if !ok {
				t.Fatal("条件应当构建成功")
			}
			if actual.Query != tt.query {
				t.Fatalf("SQL 不一致：got %q, want %q", actual.Query, tt.query)
			}
			if !reflect.DeepEqual(actual.Args, tt.args) {
				t.Fatalf("参数不一致：got %#v, want %#v", actual.Args, tt.args)
			}
		})
	}
}

func TestBuildConditionRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	user := &UserContext{}
	invalid := []Condition{
		{Field: "status;DROP TABLE sys_admin", Op: "EQ", Value: "1"},
		{Field: "status", Op: "UNKNOWN", Value: "1"},
		{Field: "status", Op: "IN", Value: " , "},
		{Field: "id", Op: "BETWEEN", Value: "10"},
	}
	for _, condition := range invalid {
		if _, ok := buildCondition(condition, user); ok {
			t.Fatalf("非法条件不应构建成功：%+v", condition)
		}
	}
}

func TestBuildConditionsPreservesArgumentOrder(t *testing.T) {
	t.Parallel()

	actual, ok := buildConditions(ConditionGroup{
		Logic: "OR",
		Conditions: []Condition{
			{Field: "status", Op: "EQ", Value: "1"},
			{Field: "username", Op: "LIKE", Value: "%admin%"},
		},
	}, &UserContext{})
	if !ok {
		t.Fatal("条件组应当构建成功")
	}
	if actual.Query != "`status` = ? OR `username` LIKE ?" {
		t.Fatalf("SQL 不一致：%q", actual.Query)
	}
	if !reflect.DeepEqual(actual.Args, []any{"1", "%admin%"}) {
		t.Fatalf("参数顺序不一致：%#v", actual.Args)
	}
}

func TestMergeRulesIntersectsRowsAndUnionsOmitFields(t *testing.T) {
	t.Parallel()

	merged := mergeRules([]RuleConfig{
		{
			OmitFields: []string{"email", "phone"},
			ConditionGroups: []ConditionGroup{{
				Logic:      "AND",
				Conditions: []Condition{{Field: "status", Op: "EQ", Value: "1"}},
			}},
		},
		{
			OmitFields: []string{"phone", "username"},
			ConditionGroups: []ConditionGroup{{
				Logic:      "AND",
				Conditions: []Condition{{Field: "dept_id", Op: "EQ", Value: "10"}},
			}},
		},
	})

	if !reflect.DeepEqual(merged.OmitFields, []string{"email", "phone", "username"}) {
		t.Fatalf("字段屏蔽未取并集：%v", merged.OmitFields)
	}
	if len(merged.ConditionGroups) != 2 {
		t.Fatalf("行限制应全部保留并由插件逐组 AND，实际数量：%d", len(merged.ConditionGroups))
	}
}

type testRuleProvider struct {
	rules []RuleConfig
	err   error
}

// openTestDB 打开本地 PostgreSQL 测试连接（DryRun 即可，无需建表）。
// 连接信息默认对齐 config.yaml（127.0.0.1:5432 root/root wp），可用
// PGHOST/PGPORT/PGUSER/PGPASSWORD/PGDATABASE 覆盖。本地 PostgreSQL 不可用时
// 返回 error，由测试方决定 t.Skip —— 避免依赖环境导致的 CI 误报失败。
func openTestDB() (*gorm.DB, error) {
	host := envOr("PGHOST", "127.0.0.1")
	port := envOr("PGPORT", "5432")
	user := envOr("PGUSER", "root")
	password := envOr("PGPASSWORD", "root")
	dbname := envOr("PGDATABASE", "wp")
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		host, user, password, dbname, port)
	return gorm.Open(postgres.Open(dsn), &gorm.Config{DryRun: true})
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func (p testRuleProvider) GetRules(context.Context, *UserContext, string) ([]RuleConfig, error) {
	return p.rules, p.err
}

type protectedRecord struct {
	ID       uint64
	Status   int
	Username string
}

func (protectedRecord) TableName() string {
	return "protected_record"
}

func TestPluginInjectsSQLAndArguments(t *testing.T) {
	RegisterDomain(DomainConfig{Domain: "TEST", TableName: "protected_record"})
	provider := testRuleProvider{rules: []RuleConfig{{
		ConditionGroups: []ConditionGroup{{
			Logic: "AND",
			Conditions: []Condition{
				{Field: "status", Op: "IN", Value: "1,2"},
				{Field: "username", Op: "LIKE", Value: "%admin%"},
			},
		}},
	}}}
	db, err := openTestDB()
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过数据规则注入测试：%v", err)
	}
	if err = db.Use(NewDataRulePlugin(provider)); err != nil {
		t.Fatalf("注册插件失败：%v", err)
	}

	ctx := context.WithValue(context.Background(), UserContextKey{}, &UserContext{UserID: 9})
	result := db.WithContext(ctx).Find(&[]protectedRecord{})
	if result.Error != nil {
		t.Fatalf("生成查询失败：%v", result.Error)
	}
	query := result.Statement.SQL.String()
	// postgres 方言把占位符渲染为 $1/$2…，IN 展开为 ($1,$2)；条件标识与注入逻辑不变。
	if !strings.Contains(query, "`status` IN ($1,$2)") || !strings.Contains(query, "`username` LIKE $3") {
		t.Fatalf("数据规则未注入 SQL：%s", query)
	}
	if !reflect.DeepEqual(result.Statement.Vars, []any{"1", "2", "%admin%"}) {
		t.Fatalf("GORM 参数绑定不一致：%#v", result.Statement.Vars)
	}
}

func TestPluginFailsClosedWhenProviderErrors(t *testing.T) {
	RegisterDomain(DomainConfig{Domain: "TEST_ERROR", TableName: "protected_record_error"})
	providerErr := errors.New("规则查询失败")
	db, err := openTestDB()
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过数据规则 fail-closed 测试：%v", err)
	}
	if err = db.Use(NewDataRulePlugin(testRuleProvider{err: providerErr})); err != nil {
		t.Fatalf("注册插件失败：%v", err)
	}

	type errorRecord struct{ ID uint64 }
	result := db.WithContext(
		context.WithValue(context.Background(), UserContextKey{}, &UserContext{UserID: 9}),
	).Table("protected_record_error").Find(&[]errorRecord{})
	if !errors.Is(result.Error, providerErr) {
		t.Fatalf("规则查询失败时应阻止业务查询，实际错误：%v", result.Error)
	}
}
