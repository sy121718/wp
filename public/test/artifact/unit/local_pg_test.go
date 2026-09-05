package unit

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

// 本文件提供与 public/test/support.NewPGTestDB 等价的隔离 PG 测试基建。
//
// 背景：support 包 import 了 go_wp/internal/routers 与 go_wp/config，而当前
// 工作区存在他人未提交的 WIP 修改（internal/middleware/builtin/cors.go、
// internal/module/project/service/theme_service.go 等）处于编译失败中间态，
// 导致 support 包整体无法编译。为不触碰 internal/ 生产代码，这里在测试包内
// 复刻同等语义：每次调用创建独立 schema（search_path 隔离），测试结束
// DROP SCHEMA；PG 不可达时由调用方 t.Skip。
//
// 与 support 版本的差异：直接用 gorm.Config{TranslateError:true} 打开连接，
// 与生产数据库配置（pkg/database/database.go:240）一致，避免 gorm 错误翻译
// 语义偏差。待工作区 WIP 修复后可切回 support.NewPGTestDB。

const (
	localPGHost     = "127.0.0.1"
	localPGPort     = "5432"
	localPGUser     = "root"
	localPGPassword = "root"
	localPGDatabase = "wp_test"
)

var errPGUnavailable = fmt.Errorf("本地 PostgreSQL 不可用")

// pgEnv 读取单个环境变量，空则回退到默认值（对齐 support.pgEnv）。
func pgEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// pgDSN 拼装 libpq 风格 DSN。
func pgDSN(host, port, user, password, dbname string) string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		host, user, password, dbname, port)
}

// newLocalPGDB 打开一个专属隔离 schema 的 *gorm.DB；t.Cleanup 中
// DROP SCHEMA ... CASCADE。err 非 nil 时调用方应 t.Skipf。
func newLocalPGDB(t *testing.T) (*gorm.DB, error) {
	t.Helper()

	host := pgEnv("PGHOST", localPGHost)
	port := pgEnv("PGPORT", localPGPort)
	user := pgEnv("PGUSER", localPGUser)
	password := pgEnv("PGPASSWORD", localPGPassword)
	dbname := pgEnv("PGDATABASE", localPGDatabase)

	adminDB, err := gorm.Open(postgres.Open(pgDSN(host, port, user, password, dbname)), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errPGUnavailable, err)
	}
	adminSQL, err := adminDB.DB()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errPGUnavailable, err)
	}

	if err := adminSQL.Ping(); err != nil {
		adminSQL.Close()
		return nil, fmt.Errorf("%w: %v", errPGUnavailable, err)
	}

	schema := "t_" + randomHex(10)
	if err := adminDB.Exec(fmt.Sprintf("CREATE SCHEMA %s", schema)).Error; err != nil {
		return nil, fmt.Errorf("创建测试 schema 失败: %w", err)
	}

	dsn := pgDSN(host, port, user, password, dbname) + " search_path=" + schema
	// 与生产数据库一致启用 TranslateError，gorm.ErrDuplicatedKey 才可被
	// service 的 mapPersistenceError 归一化。
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		adminDB.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema))
		return nil, fmt.Errorf("打开测试连接失败: %w", err)
	}

	t.Cleanup(func() {
		// 顺序：先关测试连接 → 再 DROP schema → 最后关 admin 连接池
		// （旧实现 defer 提前关闭 adminSQL，DROP 用已关闭的池必然失败）。
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
