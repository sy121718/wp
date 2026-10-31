package pluginfeature

import (
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	pluginservice "go_wp/internal/module/plugin/service"
	"go_wp/public/test/support"
)

func pluginRoleCanEscape(t *testing.T, db *gorm.DB) error {
	t.Helper()
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL ROLE " + quoteIdent(pluginservice.PluginMigrationRole)).Error; err != nil {
			t.Fatalf("切换角色失败（沙箱前置条件不满足，请跑 scripts/rls-role-setup.sh）：%v", err)
		}

		return tx.Exec(`DO $$ BEGIN EXECUTE 'DROP' || ' TABLE public.sys_config'; END $$`).Error
	})
}

func TestPluginMigrationRoleBlocksPublicWrite(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)

	escapeErr := pluginRoleCanEscape(t, db)
	if escapeErr == nil {
		t.Fatalf("受限角色 %s 成功删除了 public.sys_config —— 权限沙箱失效。"+
			"请检查 scripts/rls-role-setup.sh 的授权：该角色不应有 public 的 CREATE/所有权",
			pluginservice.PluginMigrationRole)
	}

	msg := escapeErr.Error()
	permissionish := strings.Contains(msg, "must be owner") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "denied")
	if !permissionish {
		t.Errorf("拒绝原因应是权限，实际：%v（若为语法/解析错误，说明没走到权限判定）", escapeErr)
	}
}

func TestPluginMigrationRoleCanBuildOwnSchema(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)

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

		return errRollbackProbe
	})
	if err != nil && err != errRollbackProbe {
		t.Fatal(err)
	}

	if cleanup := db.Exec("DROP SCHEMA IF EXISTS " + quoteIdent(schema) + " CASCADE").Error; cleanup != nil {
		t.Logf("清理探测 schema 失败（不影响判据）：%v", cleanup)
	}
}

func quoteIdent(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

var errRollbackProbe = fmt.Errorf("rollback probe")
