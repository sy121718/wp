package rlstest

// rls_policy_coverage_test.go — 「除白名单外，所有带 project_id 的基表都必须装 RLS」的**可执行判据**（审计 DB-01）。
//
// 为什么需要它：这句话以前只写在文档与代码注释里，而且写错过 —— `docs/rules/database.md` 与
// `build/model/build_model.go` 都说 `build_jobs` 是**唯一**的豁免表，实际有**两张**
// （迁移 309 的 `product_outbox_events` 自己都写着「本表不装 RLS 策略（与 build_jobs 同一情形）」）。
// 靠人记的断言一定会漂移，后果是评审按「唯一」只查一张表。
//
// 判据的形状刻意选成「**默认必须有策略，豁免要逐条登记**」：
//   - 新增一张带 project_id 的表却忘了装策略 → 红（这正是 215 铺开之后新增表最容易漏的事）；
//   - 有人在白名单外删掉某张表的 ENABLE / FORCE → 红；
//   - 白名单条目已过期（表被删、或已经不缺策略了）→ 红（清单只减不增，防止无限期挂着豁免）。
//
// 表结构来自生产迁移（与 rls_scope_test.go 同源），不手抄 DDL —— 手抄的会在下一次迁移推进时
// 与生产静默分叉。

import (
	"crypto/rand"
	"encoding/hex"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"go_wp/public/migrations"
)

// noRLSPolicyTables 允许「有 project_id 但不装 RLS」的表，逐条写理由。
//
// 共同理由：这两张都是**按状态跨工程领取**的队列 —— worker 一次领取所有工程的待办，
// 装了策略它就只能看见某一个工程的待办，队列直接停掉。豁免的代价与别的表**相反**：
// 切到非超级角色后，它们的查询既不报错也不被限制（不是 fail closed，而是没有隔离），
// 所以工程过滤必须显式写在 SQL 里（见 internal/module/build/model 的包注释）。
var noRLSPolicyTables = map[string]string{
	"build_jobs":            "构建任务队列：Claim / ReclaimStale 按状态跨工程领取（worker 服务全站队列）",
	"product_outbox_events": "商品事件发件箱队列：消费者按状态跨工程领取（迁移 309 的注释已声明不装策略）",
}

// TestProjectIDTablesHaveRLSPolicy 扫生产 schema：白名单之外的「带 project_id 的基表」
// 必须同时有 ENABLE（relrowsecurity）与 FORCE（relforcerowsecurity）。
//
// FORCE 不是可选项：没有它，表属主（迁移常用的管理角色）会绕过策略，而生产里
// 管理与业务连接的角色属性随时可能被 DBA 改回去 —— 那时「隔离生效」就只剩文档在说。
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

// coverageFixture 建一个独立临时库并把生产迁移跑进去，返回连上它的句柄。
//
// 只做「干净库 + 生产迁移」，不建角色也不 SET ROLE —— 本用例读的是 pg_class 元数据，
// 与连接身份无关（而 rls_scope_test.go 那套必须跑在非超级角色下，两件事不要混在一套 fixture 里）。
func coverageFixture(t *testing.T) *gorm.DB {
	t.Helper()
	admin, err := gorm.Open(postgres.Open(pgDSN(pgEnv("PGDATABASE", "wp_test"))), &gorm.Config{})
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Skipf("取管理连接失败：%v", err)
		return nil
	}
	if err := adminSQL.Ping(); err != nil {
		adminSQL.Close()
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

	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移失败: %v", err)
	}
	return db
}
