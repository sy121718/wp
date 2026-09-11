package i18n_test

// site_lang_url_mode_test.go — 站点访问路径的语言方案（多语言 P2/P3，docs/06-D §5 方案 A'）。
//
// 策略：默认 default_plain（默认语言无前缀 + 非默认语言短码前缀）。
// 本文件覆盖：无键默认值、三种显式枚举、旧键 site_lang_prefix 兼容映射、
// 非法取值必须报错、配置文件与示例配置的默认值。

import (
	"os"
	"path/filepath"
	"strings"
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

// TestSiteLangURLModeDefaults 无配置键时默认 default_plain（默认语言无前缀）。
func TestSiteLangURLModeDefaults(t *testing.T) {
	t.Cleanup(func() { i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain) })
	ensureI18nDB(t)
	if err := i18n.Init(viper.New()); err != nil {
		t.Skipf("i18n 初始化依赖数据库，跳过：%v", err)
	}
	if got := i18n.SiteLangURLModeValue(); got != i18n.SiteLangURLModeDefaultPlain {
		t.Fatalf("无配置键时方案应为 default_plain，实际 %q", got)
	}
	if !i18n.SiteLangURLsSeparated() {
		t.Fatal("default_plain 应按语言分离访问路径")
	}
	if i18n.SiteLangURLPrefixDefault() {
		t.Fatal("default_plain 下默认语言不应带前缀")
	}
}

// TestSiteLangURLModeExplicit 显式枚举三值 + 旧键兼容映射。
func TestSiteLangURLModeExplicit(t *testing.T) {
	t.Cleanup(func() { i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain) })
	ensureI18nDB(t)

	cases := []struct {
		name          string
		setup         func(*viper.Viper)
		wantMode      i18n.SiteLangURLMode
		wantSeparated bool
		wantPrefixDef bool
	}{
		{
			"显式 default_plain", func(v *viper.Viper) { v.Set("i18n.site_lang_url_mode", "default_plain") },
			i18n.SiteLangURLModeDefaultPlain, true, false,
		},
		{
			"显式 all_prefix", func(v *viper.Viper) { v.Set("i18n.site_lang_url_mode", "all_prefix") },
			i18n.SiteLangURLModeAllPrefix, true, true,
		},
		{
			"显式 off", func(v *viper.Viper) { v.Set("i18n.site_lang_url_mode", "off") },
			i18n.SiteLangURLModeOff, false, false,
		},
		{
			"旧键 site_lang_prefix=true → all_prefix", func(v *viper.Viper) { v.Set("i18n.site_lang_prefix", true) },
			i18n.SiteLangURLModeAllPrefix, true, true,
		},
		{
			"旧键 site_lang_prefix=false → off", func(v *viper.Viper) { v.Set("i18n.site_lang_prefix", false) },
			i18n.SiteLangURLModeOff, false, false,
		},
		{
			"新键优先于旧键", func(v *viper.Viper) {
				v.Set("i18n.site_lang_url_mode", "default_plain")
				v.Set("i18n.site_lang_prefix", true)
			},
			i18n.SiteLangURLModeDefaultPlain, true, false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := viper.New()
			c.setup(v)
			if err := i18n.Init(v); err != nil {
				t.Fatalf("初始化失败: %v", err)
			}
			if got := i18n.SiteLangURLModeValue(); got != c.wantMode {
				t.Fatalf("方案 = %q，期望 %q", got, c.wantMode)
			}
			if got := i18n.SiteLangURLsSeparated(); got != c.wantSeparated {
				t.Fatalf("Separated = %v，期望 %v", got, c.wantSeparated)
			}
			if got := i18n.SiteLangURLPrefixDefault(); got != c.wantPrefixDef {
				t.Fatalf("PrefixDefault = %v，期望 %v", got, c.wantPrefixDef)
			}
		})
	}
}

// TestSiteLangURLModeRejectsInvalid 非法取值必须初始化失败（fail-fast，不静默降级）。
func TestSiteLangURLModeRejectsInvalid(t *testing.T) {
	ensureI18nDB(t)
	v := viper.New()
	v.Set("i18n.site_lang_url_mode", "prefix_everything")
	if err := i18n.Init(v); err == nil {
		t.Fatal("非法 site_lang_url_mode 应初始化失败")
	}
}

// TestSiteLangURLModeFromConfigFile 真实 config.yaml 的取值经 viper 解析后生效
// （本地配置文件被 gitignore，缺失时跳过）。
func TestSiteLangURLModeFromConfigFile(t *testing.T) {
	t.Cleanup(func() { i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain) })
	ensureI18nDB(t)
	v := viper.New()
	v.SetConfigFile(filepath.Join("..", "..", "..", "..", "config.yaml"))
	if err := v.ReadInConfig(); err != nil {
		t.Skipf("读取 config.yaml 失败（跳过）：%v", err)
	}
	if err := i18n.Init(v); err != nil {
		t.Skipf("i18n 初始化依赖数据库，跳过：%v", err)
	}
	if got := i18n.SiteLangURLModeValue(); got != i18n.SiteLangURLModeDefaultPlain {
		t.Fatalf("config.yaml 生效后方案应为 default_plain，实际 %q", got)
	}
	if !i18n.SiteLangURLsSeparated() || i18n.SiteLangURLPrefixDefault() {
		t.Fatalf("config.yaml 应为「默认语言无前缀 + 非默认语言短码」，实际 Separated=%v PrefixDefault=%v",
			i18n.SiteLangURLsSeparated(), i18n.SiteLangURLPrefixDefault())
	}
}

// TestSiteLangURLModeConfigFile 配置文件与示例配置必须显式给出 default_plain。
func TestSiteLangURLModeConfigFile(t *testing.T) {
	for _, name := range []string{"config.yaml", "config.yaml.example"} {
		path := filepath.Join("..", "..", "..", "..", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("读取 %s 失败（跳过）：%v", name, err)
		}
		found := false
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "site_lang_url_mode:") {
				continue
			}
			found = true
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "site_lang_url_mode:"))
			if value != "default_plain" {
				t.Fatalf("%s 的 site_lang_url_mode 应为 default_plain，实际 %q", name, value)
			}
		}
		if !found {
			t.Fatalf("%s 缺少 i18n.site_lang_url_mode 配置项", name)
		}
	}
}
