package config

import (
	"testing"

	"github.com/spf13/viper"
)

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

func TestSuppressWorkersUsesComponentConfig(t *testing.T) {
	originalConfig := v
	originalComponents, originalPreparers := runtimeComponents, runtimePreparers
	originalSuppression := suppressWorkers.Load()
	t.Cleanup(func() {
		_ = CloseComponents()
		v = originalConfig
		runtimeComponents, runtimePreparers = originalComponents, originalPreparers
		suppressWorkers.Store(originalSuppression)
	})
	for _, migration := range []bool{false, true} {
		cfg := viper.New()
		cfg.Set(keyRunWorker, true)
		v = cfg
		runtimePreparers = nil
		suppressWorkers.Store(false)
		if migration {
			SuppressWorkers()
		}
		called := false
		runtimeComponents = []runtimeComponent{{Name: "queue-policy-probe", Init: func(componentConfig *viper.Viper) error {
			called = true
			if componentConfig != cfg || componentConfig.GetBool(keyRunWorker) != !migration {
				t.Fatalf("迁移=%v 时实际组件消费者策略不正确", migration)
			}
			return nil
		}}}
		if err := InitComponents(); err != nil {
			t.Fatal(err)
		}
		if !called {
			t.Fatal("未走真实组件初始化入口")
		}
		if err := CloseComponents(); err != nil {
			t.Fatal(err)
		}
	}
}
