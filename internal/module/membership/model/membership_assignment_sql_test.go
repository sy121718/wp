package membershipmodel

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func dryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{DriverName: "pgx", DSN: "dry-run"}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("构造 DryRun 句柄失败: %v", err)
	}
	return db
}

func TestUpsertAutoSQLCarriesManualGuard(t *testing.T) {
	db := dryRunDB(t)
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return autoUpsertStmt(context.Background(), tx, "p-1", 1024, 7)
	})
	norm := strings.Join(strings.Fields(sql), " ")
	if !strings.Contains(norm, "ON CONFLICT") {
		t.Fatalf("缺失 ON CONFLICT：%s", norm)
	}
	if !strings.Contains(norm, `WHERE membership_assignments.source <> 'manual'`) {
		t.Fatalf("缺失手工锁定的守卫（手工指定会被自动重算覆盖）：%s", norm)
	}
	if !strings.Contains(norm, `"source"='auto'`) && !strings.Contains(norm, `"source" = 'auto'`) {
		t.Fatalf("自动重算应把来源写成 auto：%s", norm)
	}
}

func TestUpsertManualSQLOverwrites(t *testing.T) {
	db := dryRunDB(t)
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return manualUpsertStmt(context.Background(), tx, "p-1", 1024, 7)
	})
	norm := strings.Join(strings.Fields(sql), " ")
	if !strings.Contains(norm, "ON CONFLICT") {
		t.Fatalf("缺失 ON CONFLICT：%s", norm)
	}
	if strings.Contains(norm, "source <> 'manual'") {
		t.Fatalf("手工指定不该带守卫（它就是要覆盖当前归属）：%s", norm)
	}
	if !strings.Contains(norm, `"source"='manual'`) && !strings.Contains(norm, `"source" = 'manual'`) {
		t.Fatalf("手工指定应把来源写成 manual：%s", norm)
	}
}

func TestUnlockManualSQLScopesToManual(t *testing.T) {
	db := dryRunDB(t)
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return unlockManualStmt(context.Background(), tx, "p-1", 1024)
	})
	norm := strings.Join(strings.Fields(sql), " ")

	if !strings.Contains(norm, "WHERE") || !strings.Contains(norm, `source = 'manual'`) {
		t.Fatalf("解锁必须限定在 manual 行上：%s", norm)
	}

	if strings.Contains(norm, "tier_id") {
		t.Fatalf("解锁不该动 tier_id：%s", norm)
	}
}

func TestListAndCountShareFilters(t *testing.T) {
	db := dryRunDB(t)
	listSQL := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return listAssignmentsStmt(context.Background(), tx, "p-1", 7, SourceManual, 1024, 2, 20).Find(&[]*AssignmentEntity{})
	})
	countSQL := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var n int64
		return countAssignmentsStmt(context.Background(), tx, "p-1", 7, SourceManual, 1024).Count(&n)
	})
	for name, sql := range map[string]string{"list": listSQL, "count": countSQL} {
		norm := strings.Join(strings.Fields(sql), " ")
		for _, want := range []string{`tier_id = 7`, `source = 'manual'`, `user_id = 1024`} {
			if !strings.Contains(norm, want) {
				t.Errorf("%s 缺少筛选条件 %s：%s", name, want, norm)
			}
		}
	}

	if !strings.Contains(listSQL, "LIMIT") {
		t.Errorf("取值应带分页：%s", listSQL)
	}
	if strings.Contains(countSQL, "LIMIT") {
		t.Errorf("计数不该带分页：%s", countSQL)
	}
}
