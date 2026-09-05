// Package support 提供测试通用辅助。
// pgtest 为 feature / functional 测试提供直连本地 PostgreSQL 的隔离连接：
// 每次调用创建一个独立的 schema（search_path 隔离），测试结束后 DROP。
// 连接信息默认对齐 config.yaml（127.0.0.1:5432 root/root wp_test），
// 可用标准 libpq 环境变量覆盖：PGHOST / PGPORT / PGUSER / PGPASSWORD / PGDATABASE。
package support

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// DefaultPGHost is the local postgres host used by tests; mirrors config.yaml database.host.
const DefaultPGHost = "127.0.0.1"

// DefaultPGPort is the local postgres port used by tests; mirrors config.yaml database.port.
const DefaultPGPort = "5432"

// DefaultPGUser is the local postgres user used by tests; mirrors config.yaml database.user.
const DefaultPGUser = "root"

// DefaultPGPassword is the local postgres password used by tests; mirrors config.yaml database.password.
const DefaultPGPassword = "root"

// DefaultPGDatabase is the dedicated test database created/used by tests.
const DefaultPGDatabase = "wp_test"

// ErrPGUnavailable 表示本地 PostgreSQL 不可达（无环境、或连接失败）。
// 测试遇到该错误时应 t.Skip 而非 fail，避免在无 postgres 的 CI 环境误报。
var ErrPGUnavailable = fmt.Errorf("本地 PostgreSQL 不可用")

// pgEnv 读取单个环境变量，空则回退到默认值。
func pgEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// pgDSN 拼装 pgx / libpq 风格 DSN。
func pgDSN(host, port, user, password, dbname string) string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		host, user, password, dbname, port)
}

// pgTestDB handles a per-test, isolated postgres schema.
type pgTestDB struct {
	host, port, user, password, dbname string
	schema                             string
}

// NewPGTestDB opens a dedicated, isolated postgres schema for one test.
// It connects to PGDATABASE (default wp_test), creates a unique schema,
// and returns a *gorm.DB whose search_path points at that schema so all
// DDL / queries are fully isolated. t.Skip 应交给调用方在 err != nil 时执行。
// Cleanup 通过 t.Cleanup 注册：DROP SCHEMA ... CASCADE 并回收连接。
func NewPGTestDB(t *testing.T) (*gorm.DB, error) {
	t.Helper()

	host := pgEnv("PGHOST", DefaultPGHost)
	port := pgEnv("PGPORT", DefaultPGPort)
	user := pgEnv("PGUSER", DefaultPGUser)
	password := pgEnv("PGPASSWORD", DefaultPGPassword)
	dbname := pgEnv("PGDATABASE", DefaultPGDatabase)

	adminDB, err := gorm.Open(postgres.Open(pgDSN(host, port, user, password, dbname)), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPGUnavailable, err)
	}
	adminSQL, err := adminDB.DB()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPGUnavailable, err)
	}

	if err := adminSQL.Ping(); err != nil {
		adminSQL.Close()
		return nil, fmt.Errorf("%w: %v", ErrPGUnavailable, err)
	}

	schema := "t_" + randomHex(10)
	if err := adminDB.Exec(fmt.Sprintf("CREATE SCHEMA %s", schema)).Error; err != nil {
		adminSQL.Close()
		return nil, fmt.Errorf("创建测试 schema 失败: %w", err)
	}

	// 用 search_path 指向专属 schema 的连接做隔离。
	dsn := pgDSN(host, port, user, password, dbname) + " search_path=" + schema
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		// 连接失败时尽力清理 schema，避免残留。
		adminDB.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema))
		adminSQL.Close()
		return nil, fmt.Errorf("打开测试连接失败: %w", err)
	}

	t.Cleanup(func() {
		// 顺序：先关测试连接 → 再 DROP schema → 最后关 admin 连接池。
		// （旧实现 defer 提前关闭 adminSQL，Cleanup 里 DROP 用已关闭的池必然失败，schema 大量残留。）
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
		if err := adminDB.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema)).Error; err != nil {
			t.Logf("清理测试 schema %s 失败: %v", schema, err)
		}
		adminSQL.Close()
	})
	return db, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}