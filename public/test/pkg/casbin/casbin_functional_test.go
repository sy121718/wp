package casbin_test

import (
	"testing"

	pkgcasbin "go_wp/pkg/casbin"
	"go_wp/pkg/database"

	"github.com/spf13/viper"
)

func TestCasbinInitAndCloseWithSQLite(t *testing.T) {
	t.Cleanup(func() {
		if err := pkgcasbin.Close(); err != nil {
			t.Fatalf("关闭 Casbin 失败: %v", err)
		}
		if err := database.Close(); err != nil {
			t.Fatalf("关闭数据库失败: %v", err)
		}
	})

	cfg := viper.New()
	cfg.Set("server.mode", "test")
	cfg.Set("database.driver", "postgres")
	cfg.Set("database.dbname", "wp_test")
	cfg.Set("database.host", "127.0.0.1")
	cfg.Set("database.port", 5432)
	cfg.Set("database.user", "root")
	cfg.Set("database.password", "root")
	cfg.Set("database.max_idle_conns", 1)
	cfg.Set("database.max_open_conns", 1)

	if err := database.Init(cfg); err != nil {
		t.Fatalf("初始化数据库失败: %v", err)
	}

	if err := pkgcasbin.Init(cfg); err != nil {
		t.Fatalf("初始化 Casbin 失败: %v", err)
	}

	enforcer := pkgcasbin.GetEnforcer()
	if enforcer == nil {
		t.Fatalf("Casbin Enforcer 不应为空")
	}

	if err := pkgcasbin.Close(); err != nil {
		t.Fatalf("关闭 Casbin 失败: %v", err)
	}

	if pkgcasbin.GetEnforcer() != nil {
		t.Fatalf("关闭后 Enforcer 应被清空")
	}
}
