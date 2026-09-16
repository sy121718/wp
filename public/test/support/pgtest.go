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
	"sync"
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

// PGEndpoint 一组 PG 连接参数（本地服务与 testcontainers 容器端点通用）。
type PGEndpoint struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

// localPGEndpoint 按现行约定读取本地 PG 连接参数（libpq 环境变量覆盖默认值）。
func localPGEndpoint() PGEndpoint {
	return PGEndpoint{
		Host:     pgEnv("PGHOST", DefaultPGHost),
		Port:     pgEnv("PGPORT", DefaultPGPort),
		User:     pgEnv("PGUSER", DefaultPGUser),
		Password: pgEnv("PGPASSWORD", DefaultPGPassword),
		Database: pgEnv("PGDATABASE", DefaultPGDatabase),
	}
}

// NewPGTestDB opens a dedicated, isolated postgres schema for one test.
// It connects to PGDATABASE (default wp_test), creates a unique schema,
// and returns a *gorm.DB whose search_path points at that schema so all
// DDL / queries are fully isolated. t.Skip 应交给调用方在 err != nil 时执行。
// Cleanup 通过 t.Cleanup 注册：DROP SCHEMA ... CASCADE 并回收连接。
func NewPGTestDB(t *testing.T) (*gorm.DB, error) {
	t.Helper()
	return NewPGTestDBAt(t, localPGEndpoint())
}

// NewPGTestDBAt 与 NewPGTestDB 相同，但使用显式端点
// （support/testenv.go 的容器回退路径复用；行为与原函数完全一致）。
func NewPGTestDBAt(t *testing.T, ep PGEndpoint) (*gorm.DB, error) {
	t.Helper()

	host, port, user, password, dbname := ep.Host, ep.Port, ep.User, ep.Password, ep.Database
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

	// 扩展先落到共享 schema（进程内一次；advisory lock 防多进程竞态）。
	if err := ensureSharedExtensions(adminDB); err != nil {
		adminSQL.Close()
		return nil, fmt.Errorf("准备共享扩展失败: %w", err)
	}

	schema := "t_" + randomHex(10)
	if err := adminDB.Exec(fmt.Sprintf("CREATE SCHEMA %s", schema)).Error; err != nil {
		adminSQL.Close()
		return nil, fmt.Errorf("创建测试 schema 失败: %w", err)
	}

	// 用 search_path 指向专属 schema 的连接做隔离。
	dsn := pgDSN(host, port, user, password, dbname) + " search_path=" + schema + "," + sharedExtSchema
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

// sharedExtSchema 承载 pg_trgm 的专用 schema。
//
// 扩展是**库级唯一**的：装进哪个 schema，只有 search_path 含它的连接才解析得到
// gin_trgm_ops。历史行为是「跟着第一个跑迁移的 schema 走」—— 串行时靠测试结束
// DROP SCHEMA CASCADE 把扩展一并删掉、下个 schema 重新装而侥幸通过，一旦并行就互相踩：
// 后来者的 CREATE EXTENSION IF NOT EXISTS 静默跳过，随后建 trgm 索引直接报
// "operator class gin_trgm_ops does not exist"。
//
// 刻意不借用 public：wp_test.public 里有历史残留的业务表，把它放进 search_path 会让
// 迁移里的 to_regclass 判定误判「表已存在」，静默跳过整条迁移。
const sharedExtSchema = "ext_shared"

var (
	sharedExtOnce sync.Once
	sharedExtErr  error
)

// ensureSharedExtensions 建好扩展 schema，并把 pg_trgm 固定在那里。
func ensureSharedExtensions(db *gorm.DB) error {
	sharedExtOnce.Do(func() {
		sharedExtErr = db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended('go_wp_test_ext_shared', 0))").Error; err != nil {
				return err
			}
			if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS " + sharedExtSchema).Error; err != nil {
				return err
			}
			if err := tx.Exec("CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA " + sharedExtSchema).Error; err != nil {
				return err
			}
			// 已存在但装在别处（历史遗留）：搬过来，否则 ext_shared 里没有 gin_trgm_ops。
			return tx.Exec("DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm' AND extnamespace <> '" + sharedExtSchema + "'::regnamespace) THEN ALTER EXTENSION pg_trgm SET SCHEMA " + sharedExtSchema + "; END IF; END $$;").Error
		})
	})
	return sharedExtErr
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
