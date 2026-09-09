package i18n_test

// site_lang_prefix_gate_test.go — 站点语言前缀灰度开关（多语言 P3，docs/06-D §15.3）。
//
// 策略：默认 false（单语言产物路径与 P3 之前一致，由配置显式开启）。
// 本文件覆盖：无键默认关闭、显式 true 开启、显式 false 关闭、配置文件默认值。

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

// TestSiteLangPrefixGateDefaultsOff 配置缺省时开关为 false；显式配置可双向切换。
func TestSiteLangPrefixGateDefaultsOff(t *testing.T) {
	t.Cleanup(func() { i18n.SetSiteLangPrefix(false) })
	ensureI18nDB(t)

	// 1) 无 i18n.site_lang_prefix 键：默认关闭。
	if err := i18n.Init(viper.New()); err != nil {
		t.Skipf("i18n 初始化依赖数据库，跳过：%v", err)
	}
	if i18n.SiteLangPrefixEnabled() {
		t.Fatal("未配置 site_lang_prefix 时应默认关闭")
	}

	// 2) 显式开启。
	on := viper.New()
	on.Set("i18n.site_lang_prefix", true)
	if err := i18n.Init(on); err != nil {
		t.Fatalf("显式开启后初始化失败: %v", err)
	}
	if !i18n.SiteLangPrefixEnabled() {
		t.Fatal("site_lang_prefix=true 应开启前缀")
	}

	// 3) 显式关闭（灰度回退）。
	off := viper.New()
	off.Set("i18n.site_lang_prefix", false)
	if err := i18n.Init(off); err != nil {
		t.Fatalf("显式关闭后初始化失败: %v", err)
	}
	if i18n.SiteLangPrefixEnabled() {
		t.Fatal("site_lang_prefix=false 应关闭前缀")
	}
}

// TestSiteLangPrefixConfigFileDefaultFalse 配置文件与示例配置的默认值必须为 false。
func TestSiteLangPrefixConfigFileDefaultFalse(t *testing.T) {
	for _, name := range []string{"config.yaml", "config.yaml.example"} {
		path := filepath.Join("..", "..", "..", "..", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("读取 %s 失败（跳过）：%v", name, err)
		}
		found := false
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "site_lang_prefix:") {
				continue
			}
			found = true
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "site_lang_prefix:"))
			if value != "false" {
				t.Fatalf("%s 的 site_lang_prefix 默认值应为 false，实际 %q", name, value)
			}
		}
		if !found {
			t.Fatalf("%s 缺少 i18n.site_lang_prefix 配置项", name)
		}
	}
}
