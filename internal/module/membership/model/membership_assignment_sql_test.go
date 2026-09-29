package membershipmodel

// membership_assignment_sql_test.go — 钉住「手工指定不被自动重算覆盖」那条 SQL 的形态。
//
// 这条不变量**只存在于 SQL 里**（`ON CONFLICT ... DO UPDATE ... WHERE source <> 'manual'`），
// 不在任何 Go 分支上：写错了、或者哪天有人把 Where 去掉，编译照样通过、
// 页面上照样一切正常 —— 直到运营发现自己手工调的等级第二天变回去了。
//
// 所以在这里把生成的 SQL 逐字断言一次（DryRun，不连数据库、不执行）。

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// dryRunDB 构造一个只生成 SQL、不连库的 gorm 句柄。
//
// DisableAutomaticPing：gorm.Open 默认会 Ping 一次；DryRun 场景下没有连接可 Ping。
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

// TestUpsertAutoSQLCarriesManualGuard 自动重算的 upsert 必须带 `WHERE ... source <> 'manual'`。
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

// TestUpsertManualSQLOverwrites 手工指定的 upsert **不带**守卫（它就是覆盖的那一方）。
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

// TestUnlockManualSQLScopesToManual 解锁只动 manual 行（改回 auto 且带 source 条件）。
func TestUnlockManualSQLScopesToManual(t *testing.T) {
	db := dryRunDB(t)
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return unlockManualStmt(context.Background(), tx, "p-1", 1024)
	})
	norm := strings.Join(strings.Fields(sql), " ")
	// WHERE 里的条件是原始 SQL（不走 NamingStrategy），所以没有双引号 —— 与 upsert 的 SET 不同。
	if !strings.Contains(norm, "WHERE") || !strings.Contains(norm, `source = 'manual'`) {
		t.Fatalf("解锁必须限定在 manual 行上：%s", norm)
	}
	// 解锁只改 source（等级留给下一次日结）—— 带上 tier_id 就意味着解锁会造成一次可见跳变。
	if strings.Contains(norm, "tier_id") {
		t.Fatalf("解锁不该动 tier_id：%s", norm)
	}
}

// TestListAndCountShareFilters 取值与计数用同一组筛选条件（分页条不会多出一页空列表）。
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
	// 分页只出现在取值那一条上（计数不该带 limit / offset）。
	if !strings.Contains(listSQL, "LIMIT") {
		t.Errorf("取值应带分页：%s", listSQL)
	}
	if strings.Contains(countSQL, "LIMIT") {
		t.Errorf("计数不该带分页：%s", countSQL)
	}
}
