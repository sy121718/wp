package pluginfeature

// plugin_sandbox_role_test.go — 插件迁移的**权限沙箱**判据（审计 SEC-01 / SEC-02）。
//
// 为什么这条必须连真库：判据是「数据库拦不拦得住」，不是「代码里有没有那句 SET LOCAL ROLE」。
// 只断言代码文本的话，角色名写错、membership 没建、public 有 CREATE 权限 ——
// 三种情况都会静默通过，而沙箱其实不存在。
//
// ## 背景：两道"防线"里只有一道是真的（实测 2026-10-29，本机 wp 库）
//
//	① SET LOCAL search_path TO plugin_x —— **不是安全边界**。
//	   之后 `CREATE TABLE public.y` 仍然成功：search_path 只影响未限定的名字，不是权限。
//	② validatePluginStatement 的正则黑名单 —— **可绕过**。
//	   `DO $$ BEGIN EXECUTE 'DROP' || ' TABLE public.sys_config'; END $$` 执行成功，
//	   拼接后的语句文本里没有连续的 "drop ... table" 模式。
//
// 换成受限角色后，同样的实验分别得到 `permission denied for schema public` 与
// `must be owner of table`。本文件把这两个结果钉住 —— 它们是沙箱成立的定义。

import (
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	pluginservice "go_wp/internal/module/plugin/service"
	"go_wp/public/test/support"
)

// pluginRoleCanEscape 用受限角色尝试越权写 public，返回 nil 表示**越权成功**（沙箱失效）。
func pluginRoleCanEscape(t *testing.T, db *gorm.DB) error {
	t.Helper()
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL ROLE " + quoteIdent(pluginservice.PluginMigrationRole)).Error; err != nil {
			t.Fatalf("切换角色失败（沙箱前置条件不满足，请跑 scripts/rls-role-setup.sh）：%v", err)
		}
		// 形态与绕过黑名单的那次实测一致：拼接字符串，正则匹配不到。
		return tx.Exec(`DO $$ BEGIN EXECUTE 'DROP' || ' TABLE public.sys_config'; END $$`).Error
	})
}

// TestPluginMigrationRoleBlocksPublicWrite 受限角色**不能**改动 public 的对象。
//
// 这条是沙箱的正向判据：如果它通过了（err == nil），说明表真被删了，沙箱不存在。
func TestPluginMigrationRoleBlocksPublicWrite(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)

	escapeErr := pluginRoleCanEscape(t, db)
	if escapeErr == nil {
		t.Fatalf("受限角色 %s 成功删除了 public.sys_config —— 权限沙箱失效。"+
			"请检查 scripts/rls-role-setup.sh 的授权：该角色不应有 public 的 CREATE/所有权",
			pluginservice.PluginMigrationRole)
	}
	// 报错必须是**权限类**的，而不是语法错误 —— 后者会让"拦住了"变成一个假象。
	msg := escapeErr.Error()
	permissionish := strings.Contains(msg, "must be owner") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "denied")
	if !permissionish {
		t.Errorf("拒绝原因应是权限，实际：%v（若为语法/解析错误，说明没走到权限判定）", escapeErr)
	}
}

// TestPluginMigrationRoleCanBuildOwnSchema 受限角色在**自己的 schema** 内必须能建表。
//
// 与上一条成对：只验"挡得住"而不验"不误伤"，最常见的退化是
// 「把角色权限收得太紧，插件装不上」，而那种失败会在安装时才暴露。
func TestPluginMigrationRoleCanBuildOwnSchema(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)

	// 前提检查：临时测试库是**新库**，go_wp_app 在它上面没有 CREATE ON DATABASE 授权
	// （授权由 scripts/rls-role-setup.sh 针对生产库执行）。这里先补上授权，
	// 才能单独验证"沙箱不误伤合法能力"；补不上就跳过并说明，不伪装成通过。
	if err := db.Exec("GRANT CREATE ON DATABASE " + quoteIdent(db.Migrator().CurrentDatabase()) +
		" TO " + quoteIdent(pluginservice.PluginMigrationRole)).Error; err != nil {
		t.Skipf("无法在测试库上授权（验证沙箱不误伤需要该前提）：%v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("REVOKE CREATE ON DATABASE " + quoteIdent(db.Migrator().CurrentDatabase()) +
			" FROM " + quoteIdent(pluginservice.PluginMigrationRole)).Error
	})

	schema := "plugin_sandbox_probe"
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL ROLE " + quoteIdent(pluginservice.PluginMigrationRole)).Error; err != nil {
			t.Fatalf("切换角色失败：%v", err)
		}
		if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS " + quoteIdent(schema)).Error; err != nil {
			return fmt.Errorf("建自己的 schema 失败（沙箱把合法能力也挡了）：%w", err)
		}
		if err := tx.Exec("SET LOCAL search_path TO " + quoteIdent(schema)).Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE TABLE probe_t (x int)").Error; err != nil {
			return fmt.Errorf("在自己 schema 内建表失败：%w", err)
		}
		// 回滚整个事务，不留残留（含 schema）。
		return errRollbackProbe
	})
	if err != nil && err != errRollbackProbe {
		t.Fatal(err)
	}
	// 兜底清理：事务回滚已应移除它，但若 DDL 未被包进同一事务，这里清一次。
	if cleanup := db.Exec("DROP SCHEMA IF EXISTS " + quoteIdent(schema) + " CASCADE").Error; cleanup != nil {
		t.Logf("清理探测 schema 失败（不影响判据）：%v", cleanup)
	}
}

// quoteIdent 双引号包裹标识符（与 service 内部的 quoteSchemaIdent 同形；
// 测试侧独立实现，避免为了一个引号函数把未导出符号导出）。
func quoteIdent(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// errRollbackProbe 用于主动回滚探测事务（避免把 DDL 留在库里）。
var errRollbackProbe = fmt.Errorf("rollback probe")
