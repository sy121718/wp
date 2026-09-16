package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const envTestYAML = `database:
  host: 127.0.0.1
  port: 5432
  user: file_user
  password: file_pw
  dbname: file_db
`

// databaseOverride 按 pkg/database 的真实读取方式取 database 段。
//
// 刻意用 UnmarshalKey 而不是 GetString：这两条路径在 Viper 里行为不同，
// 而本项目所有组件都走 UnmarshalKey —— 只在 GetString 上验证覆盖会漏掉真正的缺口。
type databaseOverride struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
}

// isolateEnvOverrides 清掉外部环境里可能存在的 GOWP_* 覆盖，只保留用例自己要设的键。
//
// 不隔离的话断言会随运行环境漂移：CI 的 integration job 就带着 GOWP_DATABASE_PASSWORD，
// 而 `go test ./...` 会跑到本包 —— 那时「未覆盖字段应保持配置文件值」这类断言会失败，
// 失败原因却指向配置读取逻辑，像是实现坏了。空值在本项目里等同未设置，故用 Setenv("") 清除。
func isolateEnvOverrides(t *testing.T, keep ...string) {
	t.Helper()

	kept := make(map[string]struct{}, len(keep))
	for _, name := range keep {
		kept[name] = struct{}{}
	}
	for _, key := range envBindableKeys {
		envName := "GOWP_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		if _, ok := kept[envName]; ok {
			continue
		}
		t.Setenv(envName, "")
	}
}

func loadOverrideForTest(t *testing.T) databaseOverride {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(envTestYAML), 0o600); err != nil {
		t.Fatalf("写入临时配置失败: %v", err)
	}

	ResetForTest()
	t.Cleanup(ResetForTest)
	if err := Init(path); err != nil {
		t.Fatalf("初始化配置失败: %v", err)
	}

	v, err := GetViper()
	if err != nil {
		t.Fatalf("取 Viper 失败: %v", err)
	}

	var got databaseOverride
	if err := v.UnmarshalKey("database", &got); err != nil {
		t.Fatalf("解析 database 段失败: %v", err)
	}
	return got
}

// 环境变量覆盖 YAML 必须在 UnmarshalKey 这条路径上生效。
//
// 曾经的实现只开了 AutomaticEnv：它作用于 Get* 系列，而 Unmarshal 读的是配置树里的
// 子树，于是 GOWP_* 被**静默忽略** —— 没有报错、退出码 0，只是配置没换。
// 实测反例：GOWP_DATABASE_DBNAME 指向不存在的库，-migrate-only 照旧迁移原库并成功。
func TestEnvOverrideAppliesOnUnmarshalPath(t *testing.T) {
	isolateEnvOverrides(t, "GOWP_DATABASE_DBNAME", "GOWP_DATABASE_PASSWORD")
	t.Setenv("GOWP_DATABASE_DBNAME", "env_db")
	t.Setenv("GOWP_DATABASE_PASSWORD", "env_pw")

	got := loadOverrideForTest(t)

	if got.DBName != "env_db" {
		t.Errorf("database.dbname 未被环境变量覆盖：期望 env_db，实际 %q", got.DBName)
	}
	if got.Password != "env_pw" {
		t.Errorf("database.password 未被环境变量覆盖：期望 env_pw，实际 %q", got.Password)
	}
	// 覆盖是逐字段的：没设环境变量的字段必须保持配置文件原值。
	// 这一条挡的是「整段被替换成只剩 env 字段」的实现 —— 那种做法会让未覆盖的字段
	// 静默变成零值（host 变空串、port 变 0），比不覆盖更难查。
	if got.Host != "127.0.0.1" {
		t.Errorf("database.host 应保持配置文件值，实际 %q", got.Host)
	}
	if got.User != "file_user" {
		t.Errorf("database.user 应保持配置文件值，实际 %q", got.User)
	}
	if got.Port != 5432 {
		t.Errorf("database.port 应保持配置文件值 5432，实际 %d", got.Port)
	}
}

// 空字符串等同未设置：`VAR=` 在 shell 与 CI 里太容易意外出现，
// 若被当成有效覆盖，一次无关的 export 就能把库名清空。
func TestEnvOverrideIgnoresEmptyValue(t *testing.T) {
	isolateEnvOverrides(t, "GOWP_DATABASE_DBNAME")
	t.Setenv("GOWP_DATABASE_DBNAME", "   ")

	got := loadOverrideForTest(t)

	if got.DBName != "file_db" {
		t.Errorf("空环境变量不应覆盖配置：期望 file_db，实际 %q", got.DBName)
	}
}

// 段名本身也要能覆盖：CI 里把连接指向服务容器靠的就是它。
func TestEnvOverrideAppliesToRedisSection(t *testing.T) {
	isolateEnvOverrides(t, "GOWP_REDIS_HOST", "GOWP_REDIS_DB")
	t.Setenv("GOWP_REDIS_HOST", "redis.internal")
	t.Setenv("GOWP_REDIS_DB", "3")

	ResetForTest()
	t.Cleanup(ResetForTest)

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	const yaml = `redis:
  host: 127.0.0.1
  port: 6379
  enabled: true
  db: 0
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("写入临时配置失败: %v", err)
	}
	if err := Init(path); err != nil {
		t.Fatalf("初始化配置失败: %v", err)
	}

	v, err := GetViper()
	if err != nil {
		t.Fatalf("取 Viper 失败: %v", err)
	}

	var got struct {
		Host    string `mapstructure:"host"`
		Port    int    `mapstructure:"port"`
		Enabled bool   `mapstructure:"enabled"`
		DB      int    `mapstructure:"db"`
	}
	if err := v.UnmarshalKey("redis", &got); err != nil {
		t.Fatalf("解析 redis 段失败: %v", err)
	}

	if got.Host != "redis.internal" {
		t.Errorf("redis.host 未被覆盖：实际 %q", got.Host)
	}
	if got.DB != 3 {
		t.Errorf("redis.db 未被覆盖：期望 3，实际 %d", got.DB)
	}
	// 字符串转 int / bool 走的是 mapstructure 的弱类型转换，必须仍然成立。
	if got.Port != 6379 || !got.Enabled {
		t.Errorf("未覆盖字段被破坏：port=%d enabled=%v", got.Port, got.Enabled)
	}
}
