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

type databaseOverride struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DBName   string `mapstructure:"dbname"`
}

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

func TestEnvOverrideIgnoresEmptyValue(t *testing.T) {
	isolateEnvOverrides(t, "GOWP_DATABASE_DBNAME")
	t.Setenv("GOWP_DATABASE_DBNAME", "   ")

	got := loadOverrideForTest(t)

	if got.DBName != "file_db" {
		t.Errorf("空环境变量不应覆盖配置：期望 file_db，实际 %q", got.DBName)
	}
}

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

	if got.Port != 6379 || !got.Enabled {
		t.Errorf("未覆盖字段被破坏：port=%d enabled=%v", got.Port, got.Enabled)
	}
}
