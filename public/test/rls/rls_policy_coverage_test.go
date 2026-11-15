package rlstest

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"go_wp/public/schema"
	"go_wp/public/test/support"
)

var noRLSPolicyTables = map[string]string{
	"build_jobs":            "构建任务队列：Claim / ReclaimStale 按状态跨工程领取（worker 服务全站队列）",
	"product_outbox_events": "商品事件发件箱队列：消费者按状态跨工程领取（迁移 309 的注释已声明不装策略）",
}

func TestProjectIDTablesHaveRLSPolicy(t *testing.T) {
	db := coverageFixture(t)

	type row struct {
		TableName  string `gorm:"column:table_name"`
		RLSEnabled bool   `gorm:"column:rls_enabled"`
		RLSForced  bool   `gorm:"column:rls_forced"`
	}
	var rows []row
	err := db.Raw(`
		SELECT c.table_name,
		       pc.relrowsecurity      AS rls_enabled,
		       pc.relforcerowsecurity AS rls_forced
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		JOIN pg_class pc
		  ON pc.relnamespace = 'public'::regnamespace AND pc.relname = c.table_name
		WHERE c.table_schema = 'public'
		  AND c.column_name = 'project_id'
		  AND t.table_type = 'BASE TABLE'
		ORDER BY c.table_name`).Scan(&rows).Error
	if err != nil {
		t.Fatalf("查询带 project_id 的表失败: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("一张带 project_id 的表都没有 —— 迁移没跑起来？这个断言会永远为真，必须当成失败")
	}

	seen := make(map[string]bool, len(rows))
	covered := 0
	for _, r := range rows {
		seen[r.TableName] = true
		reason, exempt := noRLSPolicyTables[r.TableName]
		if exempt {
			if r.RLSEnabled || r.RLSForced {
				t.Errorf("表 %s 已经在白名单（理由：%s）里，但它其实装了 RLS —— 请从 noRLSPolicyTables 删掉这条豁免", r.TableName, reason)
			}
			continue
		}
		if !r.RLSEnabled {
			t.Errorf("表 %s 有 project_id 却没有 ENABLE ROW LEVEL SECURITY（要豁免就在 noRLSPolicyTables 里登记并写理由）", r.TableName)
			continue
		}
		if !r.RLSForced {
			t.Errorf("表 %s 启用了 RLS 但没有 FORCE —— 表属主（管理角色）仍会绕过策略", r.TableName)
			continue
		}
		covered++
	}
	for name, reason := range noRLSPolicyTables {
		if !seen[name] {
			t.Errorf("白名单里的 %s 在 schema 里找不到（理由：%s）—— 表被删/改名了？请清理这条豁免", name, reason)
		}
	}
	t.Logf("带 project_id 的基表 %d 张：%d 张有 ENABLE+FORCE，%d 张按白名单豁免（%s）",
		len(rows), covered, len(noRLSPolicyTables), "build_jobs / product_outbox_events")
}

func coverageFixture(t *testing.T) *gorm.DB {
	t.Helper()
	admin, err := gorm.Open(postgres.Open(pgDSN(pgEnv("PGDATABASE", "wp_test"))), &gorm.Config{})
	if err != nil {
		support.FailIfRequiredPG(t, err)
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	adminSQL, err := admin.DB()
	if err != nil {
		support.FailIfRequiredPG(t, err)
		t.Skipf("取管理连接失败：%v", err)
		return nil
	}
	if err := adminSQL.Ping(); err != nil {
		adminSQL.Close()
		support.FailIfRequiredPG(t, err)
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}

	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		adminSQL.Close()
		t.Fatalf("生成随机库名失败: %v", err)
	}
	dbName := "rls_cov_" + hex.EncodeToString(buf[:])
	if err := admin.Exec("CREATE DATABASE " + dbName).Error; err != nil {
		adminSQL.Close()
		t.Fatalf("建测试库失败: %v", err)
	}
	db, err := gorm.Open(postgres.Open(pgDSN(dbName)+" search_path=public,ext_shared"), &gorm.Config{})
	if err != nil {
		_ = admin.Exec("DROP DATABASE IF EXISTS " + dbName).Error
		adminSQL.Close()
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("取底层连接失败: %v", err)
	}
	t.Cleanup(func() {
		sqlDB.Close()
		_ = admin.Exec("DROP DATABASE IF EXISTS " + dbName).Error
		adminSQL.Close()
	})

	if err := schema.Apply(db); err != nil {
		t.Fatalf("载入 schema 失败: %v", err)
	}
	return db
}
