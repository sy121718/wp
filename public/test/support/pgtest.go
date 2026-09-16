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

	"go_wp/public/migrations"
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
	return newTestDatabase(t, localPGEndpoint(), false)
}

// NewPGTestDBAt 与 NewPGTestDB 相同，但使用显式端点
// （support/testenv.go 的容器回退路径复用；行为与原函数完全一致）。
func NewPGTestDBAt(t *testing.T, ep PGEndpoint) (*gorm.DB, error) {
	t.Helper()
	return newTestDatabase(t, ep, false)
}

// newTestDatabase 建一个隔离的测试库并返回连接（每个测试独占一个库，Cleanup 时 DROP）。
//
// useTemplate=true：复制「跑完生产迁移的模板库」—— 表结构由生产迁移建成、与生产一致，
// 且只要约 65ms（对比跑完整 209 条迁移约 1.1s）。给需要真实 schema 的链路测试用。
// useTemplate=false：建**空库** —— 给那些自己建表（AutoMigrate、手抄 DDL）或故意构造
// 旧 schema 的用例用；它们要的本来就是空环境，塞给它们完整生产结构反而会撞上外键约束
// 与「约束名不符」这类 gorm 元数据对齐问题。
func newTestDatabase(t *testing.T, ep PGEndpoint, useTemplate bool) (*gorm.DB, error) {
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

	name := "t_" + randomHex(10)
	createSQL := "CREATE DATABASE " + name
	if useTemplate {
		// 跑完生产迁移的模板库：复制它，而不是每次重跑 209 条迁移。
		tpl, err := ensureTemplateDB(ep)
		if err != nil {
			adminSQL.Close()
			return nil, err
		}
		createSQL = "CREATE DATABASE " + name + " TEMPLATE " + tpl
	}
	if err := adminDB.Exec(createSQL).Error; err != nil {
		adminSQL.Close()
		return nil, fmt.Errorf("创建测试库失败: %w", err)
	}

	// 表结构全在 public（模板库由生产迁移建成）；search_path 带上 ext_shared 供 trgm
	// 索引使用 —— 扩展随模板库一起复制过来，不需要再装。
	dsn := pgDSN(host, port, user, password, name) + " search_path=public," + sharedExtSchema
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		// 连接失败时尽力清理，避免残留库堆积。
		_ = adminDB.Exec("DROP DATABASE IF EXISTS " + name).Error
		adminSQL.Close()
		return nil, fmt.Errorf("打开测试连接失败: %w", err)
	}

	t.Cleanup(func() {
		// 顺序：先关测试连接（DROP DATABASE 要求该库没有活动连接）→ 再 DROP → 最后关 admin 池。
		// （旧实现 defer 提前关闭 adminSQL，Cleanup 里用已关闭的池必然失败，对象大量残留。）
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
		if err := adminDB.Exec("DROP DATABASE IF EXISTS " + name).Error; err != nil {
			t.Logf("清理测试库 %s 失败: %v", name, err)
		}
		adminSQL.Close()
	})
	return db, nil
}

// sharedExtSchema 承载 pg_trgm 的专用 schema。
//
// 扩展是**库级唯一**的，而 gin_trgm_ops 按 schema 解析。它固定装在这里（迁移 167/210），
// 模板库因此自带它、复制出来的测试库也自带 —— 测试连接把它放进 search_path 就能用。
// 刻意不借用 public：public 承载业务表，进 search_path 会让迁移里的 to_regclass 判定误判。
const sharedExtSchema = "ext_shared"

// templatePrefix 模板库名前缀，后面接迁移指纹。
const templatePrefix = "wp_test_tpl_"

// templateLockKey 建模板库的跨进程互斥键（多个测试进程会同时启动）。
const templateLockKey int64 = 0x6770775f74657374 // "gwp_test"

var (
	templateOnce sync.Once
	templateDB   string
	templateErr  error
)

// ensureTemplateDB 保证「跑完生产迁移的模板库」存在，返回库名。
//
// 为什么值得这么做：测试的隔离单位过去是 schema，而 schema 没有复制原语 —— 每个用例都要
// 建一个空 schema 再跑完整 209 条迁移（约 1.1s），这段耗时与「测什么业务」毫无关系
// （admin 一个包 116 个用例就是 128s）。库有复制原语：CREATE DATABASE ... TEMPLATE 实测
// 约 65ms（快 15 倍），复制出来的库还自带 ext_shared 与 pg_trgm，连扩展都不用再装。
//
// 模板名带迁移指纹（migrations.Fingerprint()）：迁移一改就换一个新名字重建，绝不去 DROP
// 正在被别的测试进程使用的旧模板 —— 并发下那是必然冲突。旧模板库会残留，在建模板时顺手
// 清理（清理失败只说明有进程正在用，忽略即可）。
func ensureTemplateDB(ep PGEndpoint) (string, error) {
	templateOnce.Do(func() {
		admin, err := gorm.Open(postgres.Open(pgDSN(ep.Host, ep.Port, ep.User, ep.Password, ep.Database)), &gorm.Config{})
		if err != nil {
			templateErr = fmt.Errorf("%w: %v", ErrPGUnavailable, err)
			return
		}
		defer func() {
			if sdb, err := admin.DB(); err == nil {
				sdb.Close()
			}
		}()
		if err := admin.Exec("SELECT 1").Error; err != nil {
			templateErr = fmt.Errorf("%w: %v", ErrPGUnavailable, err)
			return
		}

		name := templatePrefix + migrations.Fingerprint()
		templateErr = admin.Connection(func(conn *gorm.DB) error {
			// 多个测试进程会同时走到这里：用 advisory lock 串行化建模板。
			if err := conn.Exec("SELECT pg_advisory_lock(?)", templateLockKey).Error; err != nil {
				return err
			}
			defer conn.Exec("SELECT pg_advisory_unlock(?)", templateLockKey)

			var exists bool
			if err := conn.Raw("SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = ?)", name).Scan(&exists).Error; err != nil {
				return err
			}
			if !exists {
				if err := conn.Exec("CREATE DATABASE " + name).Error; err != nil {
					return fmt.Errorf("创建模板库 %s 失败: %w", name, err)
				}
				// 结构由生产迁移建（与「测试建表走生产迁移」一致，不手抄 DDL）。
				tpl, err := gorm.Open(postgres.Open(pgDSN(ep.Host, ep.Port, ep.User, ep.Password, name)), &gorm.Config{})
				if err != nil {
					return fmt.Errorf("连接模板库失败: %w", err)
				}
				if err := migrations.Run(tpl); err != nil {
					return fmt.Errorf("模板库执行生产迁移失败: %w", err)
				}
				// 复制前模板库必须没有活动连接。
				if sdb, err := tpl.DB(); err == nil {
					sdb.Close()
				}
			}
			// 顺手清理其它指纹的旧模板（尽力而为：正被别的进程用时 DROP 会失败）。
			var stale []string
			conn.Raw("SELECT datname FROM pg_database WHERE datname LIKE ? AND datname <> ?", templatePrefix+"%", name).Scan(&stale)
			for _, old := range stale {
				conn.Exec("DROP DATABASE IF EXISTS " + old)
			}
			return nil
		})
		if templateErr == nil {
			templateDB = name
		}
	})
	return templateDB, templateErr
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
