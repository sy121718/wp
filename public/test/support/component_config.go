package support

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"go_wp/public/migrations"
)

// NewComponentTestConfig 给需要真实组件的测试生成独立配置。
// 数据库由生产迁移模板复制，清理由 testing 生命周期承担；不读取开发 config.yaml。
func NewComponentTestConfig(t *testing.T) string {
	t.Helper()
	env, err := AcquireTestEnv(t)
	if err != nil {
		FailIfRequiredPG(t, err)
		t.Skipf("测试依赖不可用：%v", err)
	}
	db, err := newTestDatabase(t, env.PG, true, false)
	if err != nil {
		t.Fatalf("创建组件测试库失败：%v", err)
	}
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("初始化组件测试种子失败：%v", err)
	}
	var dbname string
	if err := db.Raw("SELECT current_database()").Scan(&dbname).Error; err != nil {
		t.Fatal(err)
	}
	example, err := os.ReadFile(resolveConfigPath("config.yaml.example"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := viper.New()
	cfg.SetConfigType("yaml")
	if err := cfg.ReadConfig(bytes.NewReader(example)); err != nil {
		t.Fatal(err)
	}
	// 环境变量优先于 YAML，显式绑定到测试端点，防止 CI 或开发环境覆盖隔离库。
	for key, value := range map[string]string{
		"GOWP_DATABASE_HOST": env.PG.Host, "GOWP_DATABASE_PORT": env.PG.Port,
		"GOWP_DATABASE_USER": env.PG.User, "GOWP_DATABASE_PASSWORD": env.PG.Password,
		"GOWP_DATABASE_DBNAME": dbname, "GOWP_DATABASE_DRIVER": "postgres",
		"GOWP_REDIS_PASSWORD": "", "GOWP_REDIS_DB": "15",
		"GOWP_AUTH_SESSION_SECRET": "gowp-isolated-component-test-secret",
	} {
		t.Setenv(key, value)
	}
	cfg.Set("database.dbname", dbname)
	cfg.Set("redis.addrs", []string{env.RedisAddr})
	cfg.Set("redis.db", 15)
	cfg.Set("queue.enabled", false)
	cfg.Set("upload.enabled", false)
	cfg.Set("i18n.auto_refresh", false)
	cfg.Set("log.base_dir", t.TempDir())
	cfg.Set("log.filename", filepath.Join(t.TempDir(), "test.log"))
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := cfg.WriteConfigAs(configPath); err != nil {
		t.Fatal(err)
	}
	return configPath
}
