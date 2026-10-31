package database

import (
	"context"
	"strings"
	"testing"

	"go_wp/pkg/rls"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

type stubDialector struct{ name string }

func (d stubDialector) Name() string                                          { return d.name }
func (d stubDialector) Initialize(*gorm.DB) error                             { return nil }
func (d stubDialector) Migrator(*gorm.DB) gorm.Migrator                       { return nil }
func (d stubDialector) DataTypeOf(*schema.Field) string                       { return "" }
func (d stubDialector) DefaultValueOf(*schema.Field) clause.Expression        { return clause.Expr{} }
func (d stubDialector) BindVarTo(clause.Writer, *gorm.Statement, interface{}) {}
func (d stubDialector) QuoteTo(clause.Writer, string)                         {}
func (d stubDialector) Explain(sql string, vars ...interface{}) string        { return sql }

func TestIsPostgresDialector(t *testing.T) {
	cases := map[string]bool{
		"postgres":   true,
		"PostgreSQL": true,
		"pgx":        true,
		" mysql ":    false,
		"mysql":      false,
		"":           false,
	}
	for name, want := range cases {
		if got := isPostgresDialector(name); got != want {
			t.Fatalf("isPostgresDialector(%q) = %v，期望 %v", name, got, want)
		}
	}
}

func TestCheckRLSIdentitySkipsNonPostgres(t *testing.T) {
	db, err := gorm.Open(stubDialector{name: "mysql"}, &gorm.Config{})
	if err != nil {
		t.Fatalf("构造 stub 方言失败: %v", err)
	}
	if err := CheckRLSIdentity(context.Background(), db, true); err != nil {
		t.Fatalf("非 PostgreSQL 应跳过探针（require=true 也一样），实际 %v", err)
	}
}

func TestCheckRLSIdentityNilDB(t *testing.T) {
	if err := CheckRLSIdentity(context.Background(), nil, true); err != nil {
		t.Fatalf("nil 句柄应安全跳过，实际 %v", err)
	}
}

func TestRLSRoleBypassErrorCarriesEvidence(t *testing.T) {
	err := rlsRoleBypassError(rls.Identity{SessionUser: "root", CurrentUser: "root", IsSuperuser: true})
	msg := err.Error()
	for _, want := range []string{"require_rls_role", "root", "超级用户", "rls-role-setup.sh"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误文案缺少 %q：%s", want, msg)
		}
	}

	bypassOnly := rlsRoleBypassError(rls.Identity{SessionUser: "app", CurrentUser: "app", BypassRLS: true}).Error()
	if !strings.Contains(bypassOnly, "BYPASSRLS") {
		t.Fatalf("仅 BYPASSRLS 的角色应写明确切理由：%s", bypassOnly)
	}
}
