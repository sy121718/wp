package i18n_test

import (
	"context"
	"testing"

	"go_wp/pkg/database"
	"go_wp/pkg/i18n"

	"github.com/spf13/viper"
	"go_wp/public/test/support"
)

func ensureI18nDB(t *testing.T) {
	t.Helper()
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
		if _, gerr := database.GetDB(); gerr != nil {
			support.FailIfRequiredPG(t, err)
			t.Skipf("本地 PostgreSQL 不可用，跳过：%v", err)
		}
	}
	if _, err := database.GetDB(); err != nil {
		t.Skipf("数据库实例不可用，跳过：%v", err)
	}
}

func resetGlobalSiteLangURLMode(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {

		i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) { return i18n.RuntimeValues{}, nil })
		i18n.SetValueLoader(nil)
	})
}

func TestSiteLangURLModeDefaults(t *testing.T) {
	resetGlobalSiteLangURLMode(t)
	ensureI18nDB(t)
	if err := i18n.Init(viper.New()); err != nil {
		t.Skipf("i18n 初始化依赖数据库，跳过：%v", err)
	}
	if got := i18n.DefaultSiteLangURLMode(); got != i18n.SiteLangURLModeDefaultPlain {
		t.Fatalf("未配置时全局默认方案应为 default_plain，实际 %q", got)
	}
	if !i18n.SiteLangURLsSeparated(i18n.SiteLangURLModeDefaultPlain) {
		t.Fatal("default_plain 应按语言分离访问路径")
	}
	if i18n.SiteLangURLPrefixDefault(i18n.SiteLangURLModeDefaultPlain) {
		t.Fatal("default_plain 下默认语言不应带前缀")
	}
}

func TestSiteLangURLModeFromValueLoader(t *testing.T) {
	resetGlobalSiteLangURLMode(t)
	ensureI18nDB(t)

	cases := []struct {
		raw           string
		wantMode      i18n.SiteLangURLMode
		wantSeparated bool
		wantPrefixDef bool
	}{
		{"default_plain", i18n.SiteLangURLModeDefaultPlain, true, false},
		{"all_prefix", i18n.SiteLangURLModeAllPrefix, true, true},
		{"off", i18n.SiteLangURLModeOff, false, false},
	}
	for _, c := range cases {
		t.Run(c.raw, func(t *testing.T) {
			raw := c.raw
			i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) {
				return i18n.RuntimeValues{SiteLangURLMode: raw}, nil
			})
			if got := i18n.DefaultSiteLangURLMode(); got != c.wantMode {
				t.Fatalf("方案 = %q，期望 %q", got, c.wantMode)
			}
			if got := i18n.SiteLangURLsSeparated(i18n.DefaultSiteLangURLMode()); got != c.wantSeparated {
				t.Fatalf("Separated = %v，期望 %v", got, c.wantSeparated)
			}
			if got := i18n.SiteLangURLPrefixDefault(i18n.DefaultSiteLangURLMode()); got != c.wantPrefixDef {
				t.Fatalf("PrefixDefault = %v，期望 %v", got, c.wantPrefixDef)
			}
		})
	}
}

func TestSiteLangURLModeInvalidFallsBack(t *testing.T) {
	resetGlobalSiteLangURLMode(t)
	ensureI18nDB(t)

	i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) {
		return i18n.RuntimeValues{SiteLangURLMode: "prefix_everything"}, nil
	})
	if got := i18n.DefaultSiteLangURLMode(); got != i18n.SiteLangURLModeDefaultPlain {
		t.Fatalf("非法取值应回退 default_plain，实际 %q", got)
	}
}
