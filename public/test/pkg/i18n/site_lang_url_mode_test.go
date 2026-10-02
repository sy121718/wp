package i18n_test

// site_lang_url_mode_test.go — 站点访问路径的语言方案（多语言 P2/P3，docs/06-D §5 方案 A'）。
//
// **本文件在本批被改写**（原先覆盖的对象已不存在）：
//   - 旧口径：方案从 config.yaml / viper 读（键 i18n.site_lang_url_mode 与更早的兼容键
//     i18n.site_lang_prefix），非法值让 Init fail-fast；进程里有一个可被站点设置页热更新的
//     全局值（SetSiteLangURLMode）。
//   - 新口径：**工程级**的值在 projects.settings.langURLMode（按工程解析，唯一入口
//     pipeline.SiteLangURLModeOf），**全局默认**在 sys_config 的 i18n 组（由装配层的
//     ValueLoader 注入）；进程级 setter 已删除（它会让一个工程的设置决定另一个工程的判定）。
//
// 因此这里只保留「无配置时的默认值」与「配置源注入后生效」两件事；「旧键兼容映射」
// 「config.yaml 里必须有该键」两类用例随口径一起删除（兼容层与那个键都不存在了）。

import (
	"context"
	"testing"

	"go_wp/pkg/database"
	"go_wp/pkg/i18n"

	"github.com/spf13/viper"
)

// ensureI18nDB 保证 i18n.Init 首次调用时缓存可加载（本测试独立于包内其他用例的
// 初始化顺序；PG 不可用才跳过）。
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
			t.Skipf("本地 PostgreSQL 不可用，跳过：%v", err)
		}
	}
	if _, err := database.GetDB(); err != nil {
		t.Skipf("数据库实例不可用，跳过：%v", err)
	}
}

// resetGlobalSiteLangURLMode 复位全局默认方案到「未注入 loader」的状态（代码内常量）。
func resetGlobalSiteLangURLMode(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		// 先注入零值把已生效的值打回常量，再摘掉 loader（没有 setter，只能走这条正式入口）。
		i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) { return i18n.RuntimeValues{}, nil })
		i18n.SetValueLoader(nil)
	})
}

// TestSiteLangURLModeDefaults 未接入配置源时全局默认方案是 default_plain（默认语言无前缀）。
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

// TestSiteLangURLModeFromValueLoader 配置源（sys_config 的 i18n 组）注入后立即生效。
//
// 走的是装配层注入用的同一入口（SetValueLoader）：三值都验一遍，并确认判定是纯函数。
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

// TestSiteLangURLModeInvalidFallsBack 配置值非法时回退默认方案并继续（**不再 fail-fast**）。
//
// 口径变化的理由：这个值现在是运行期可改的配置（后台保存即生效），一个写错的值不该让
// 整个站点起不来；回退是**可见的**（applyRuntimeValues 记一条 Warn，日志里写明原值与回退值）。
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
