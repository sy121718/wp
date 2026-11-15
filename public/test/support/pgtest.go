package support

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	tdbdriver "go_wp/pkg/database/driver"
	"go_wp/public/schema"
)

const DefaultPGHost = "127.0.0.1"

const DefaultPGPort = "5432"

const DefaultPGUser = "root"

const DefaultPGPassword = "root"

const DefaultPGDatabase = "wp_test"

var ErrPGUnavailable = fmt.Errorf("本地 PostgreSQL 不可用")

func pgEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func pgDSN(host, port, user, password, dbname string) string {
	dsn := tdbdriver.PostgresDSN(tdbdriver.Config{
		Host: host, Port: mustAtoi(port), User: user, Password: password, DBName: dbname,
	})

	return pgAppendParams(dsn, [2]string{"synchronous_commit", "off"})
}

func pgAppendParams(dsn string, params ...[2]string) string {
	isURL := strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://")
	for _, kv := range params {
		if isURL {
			separator := "?"
			if strings.Contains(dsn, "?") {
				separator = "&"
			}

			dsn += separator + kv[0] + "=" + strings.ReplaceAll(url.QueryEscape(kv[1]), "+", "%20")
			continue
		}

		value := kv[1]
		if strings.ContainsAny(value, " '\\") {
			value = "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value) + "'"
		}
		dsn += " " + kv[0] + "=" + value
	}
	return dsn
}

func mustAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic(fmt.Sprintf("测试库端口不是数字: %q", s))
	}
	return n
}

type pgTestDB struct {
	host, port, user, password, dbname string
	schema                             string
}

type PGEndpoint struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

func localPGEndpoint() PGEndpoint {
	return PGEndpoint{
		Host:     pgEnv("PGHOST", DefaultPGHost),
		Port:     pgEnv("PGPORT", DefaultPGPort),
		User:     pgEnv("PGUSER", DefaultPGUser),
		Password: pgEnv("PGPASSWORD", DefaultPGPassword),
		Database: pgEnv("PGDATABASE", DefaultPGDatabase),
	}
}

func NewPGTestDB(t *testing.T) (*gorm.DB, error) {
	t.Helper()
	db, err := newTestDatabase(t, localPGEndpoint(), false, false)

	FailIfRequiredPG(t, err)
	return db, err
}

func NewPGTestDBAt(t *testing.T, ep PGEndpoint) (*gorm.DB, error) {
	t.Helper()
	db, err := newTestDatabase(t, ep, false, false)
	FailIfRequiredPG(t, err)
	return db, err
}

func newTestDatabase(t *testing.T, ep PGEndpoint, useTemplate, translateError bool) (*gorm.DB, error) {
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

	dsn := pgAppendParams(pgDSN(host, port, user, password, name), [2]string{"search_path", "public," + sharedExtSchema})
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: translateError})
	if err != nil {

		_ = adminDB.Exec("DROP DATABASE IF EXISTS " + name).Error
		adminSQL.Close()
		return nil, fmt.Errorf("打开测试连接失败: %w", err)
	}

	t.Cleanup(func() {

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

const sharedExtSchema = "ext_shared"

const templatePrefix = "wp_test_tpl_"

const templateLockKey int64 = 0x6770775f74657374

const templateCleanupEnv = "WP_TEST_TPL_CLEANUP"

var (
	templateOnce sync.Once
	templateDB   string
	templateErr  error
)

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

		name := templatePrefix + schema.Digest()[:16]
		templateErr = admin.Connection(func(conn *gorm.DB) error {

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

				tpl, err := gorm.Open(postgres.Open(pgDSN(ep.Host, ep.Port, ep.User, ep.Password, name)), &gorm.Config{})
				if err != nil {
					return fmt.Errorf("连接模板库失败: %w", err)
				}
				if err := schema.Apply(tpl); err != nil {
					return fmt.Errorf("模板库载入 schema 失败: %w", err)
				}

				if sdb, err := tpl.DB(); err == nil {
					sdb.Close()
				}
			}

			if os.Getenv(templateCleanupEnv) == "1" {
				var stale []string
				conn.Raw("SELECT datname FROM pg_database WHERE datname LIKE ? AND datname <> ?", templatePrefix+"%", name).Scan(&stale)
				for _, old := range stale {
					conn.Exec("DROP DATABASE IF EXISTS " + old)
				}
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
