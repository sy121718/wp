package config

// migrations_switch_test.go — 钉住 database.run_migrations 的三条语义（DB-009 切角色前置）：
// 缺省 true、显式 false 生效、-migrate-only 强制迁移。

import (
	"testing"

	"github.com/spf13/viper"
)

// TestRunMigrationsEnabledDefaultsTrue 缺省必须是 true。
//
// 这是这条开关最容易被写错的一处：viper 的 GetBool 对缺失键返回 false，
// 直接用它会把「配置文件里没有这个键」读成「关掉迁移」—— 升级后所有旧部署
// 都会静默不再迁移。判据因此必须是 IsSet 而不是 GetBool。
func TestRunMigrationsEnabledDefaultsTrue(t *testing.T) {
	forceMigrationsSet(false)
	defer forceMigrationsSet(false)

	cases := []struct {
		name string
		cfg  *viper.Viper
	}{
		{"nil 配置", nil},
		{"键缺失", viper.New()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !runMigrationsEnabled(tc.cfg) {
				t.Fatal("未显式配置时应视为 true（保持既有行为）")
			}
		})
	}
}

// TestRunMigrationsEnabledExplicitValues 显式取值按写的来。
func TestRunMigrationsEnabledExplicitValues(t *testing.T) {
	forceMigrationsSet(false)
	defer forceMigrationsSet(false)

	on := viper.New()
	on.Set(keyRunMigrations, true)
	if !runMigrationsEnabled(on) {
		t.Fatal("显式 true 应执行迁移")
	}

	off := viper.New()
	off.Set(keyRunMigrations, false)
	if runMigrationsEnabled(off) {
		t.Fatal("显式 false 应跳过迁移")
	}
}

// TestForceMigrationsOverridesConfig -migrate-only 是显式迁移命令，必须无视 run_migrations=false。
//
// 反例的代价是静默的：推荐流程是「同一份配置（run_migrations=false）+ 管理连接跑
// -migrate-only」，若开关把显式命令也关掉，迁移什么都不做、退出码仍是 0 ——
// 运维以为迁完了，实际上一个字符都没执行。
func TestForceMigrationsOverridesConfig(t *testing.T) {
	forceMigrationsSet(false)
	defer forceMigrationsSet(false)

	off := viper.New()
	off.Set(keyRunMigrations, false)
	if runMigrationsEnabled(off) {
		t.Fatal("前置条件不成立：未强制时应跳过")
	}

	ForceMigrations()
	if !runMigrationsEnabled(off) {
		t.Fatal("ForceMigrations 之后必须强制执行迁移与 seed")
	}
}
