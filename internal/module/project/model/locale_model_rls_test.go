package projectmodel

// locale_model_rls_test.go — project_locales 行级安全（迁移 199）行为验证（DB-009）。
//
// 直连本地 PostgreSQL（与 public/test/support 同一套连接约定），每次建独立 schema。
// 验证三件事：
//  1. 设置 app.project_id 后只能看到本工程行（隔离）；
//  2. 未设置 app.project_id 时行不可见（fail closed）；
//  3. withProjectScope 包装的 ListLocales / ReplaceLocales 读写路径可用，
//     且事务结束后变量还原（连接复用不泄漏）。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// 测试连接约定与 public/test/support 一致（可用标准 libpq 环境变量覆盖）；
// 不直接 import support 包 —— support 的 test_bootstrap 会把 routers 拉进
// 依赖链，与 projectmodel 构成测试 import 环。
func rlsPGEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// rlsTestDB 建一个独立 schema 并返回绑定它的 GORM 连接。
// 连接池限制为单连接，SET search_path 对该连接持续生效，实现 schema 级隔离。
// 另建一个非超级用户角色并 SET ROLE：PG 超级用户无条件绕过 RLS（FORCE 也拦不住），
// 只有切到普通角色后策略断言才是真实证据。
func rlsTestDB(t *testing.T) (*gorm.DB, string, string) {
	t.Helper()
	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		rlsPGEnv("PGHOST", "127.0.0.1"),
		rlsPGEnv("PGUSER", "root"),
		rlsPGEnv("PGPASSWORD", "root"),
		rlsPGEnv("PGDATABASE", "wp_test"),
		rlsPGEnv("PGPORT", "5432"))
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("取底层连接失败: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatalf("生成随机 schema 名失败: %v", err)
	}
	schema := "rls_" + hex.EncodeToString(raw[:])
	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA %s", schema)).Error; err != nil {
		t.Fatalf("建 schema 失败: %v", err)
	}
	if err := db.Exec(fmt.Sprintf("SET search_path TO %s", schema)).Error; err != nil {
		t.Fatalf("设置 search_path 失败: %v", err)
	}
	role := "rls_app_" + strings.TrimPrefix(schema, "rls_")
	if err := db.Exec(fmt.Sprintf("CREATE ROLE %s NOLOGIN", role)).Error; err != nil {
		t.Fatalf("建角色失败: %v", err)
	}
	// cleanup 按 LIFO 执行：schema（连带表）先删，role 依赖解除后再删 role。
	t.Cleanup(func() {
		_ = db.Exec("RESET ROLE").Error
		_ = db.Exec(fmt.Sprintf("DROP ROLE IF EXISTS %s", role)).Error
	})
	t.Cleanup(func() {
		// 连接此时仍处于 SET ROLE 身份，须先还原再用属主身份删 schema。
		_ = db.Exec("RESET ROLE").Error
		_ = db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema)).Error
	})
	return db, schema, role
}

// rlsFixture 在隔离 schema 内建 project_locales 表并写入两个工程的行。
func rlsFixture(t *testing.T) (*Model, string, string) {
	t.Helper()
	db, schema, role := rlsTestDB(t)
	if err := db.Exec(`CREATE TABLE project_locales (
		project_id uuid NOT NULL,
		lang text NOT NULL,
		sort_order int NOT NULL,
		is_default boolean NOT NULL,
		enabled boolean NOT NULL,
		create_time timestamptz NOT NULL DEFAULT now(),
		update_time timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (project_id, lang)
	)`).Error; err != nil {
		t.Fatalf("建表失败: %v", err)
	}
	// 与迁移 199 相同的策略与 FORCE，保证测试不依赖迁移执行顺序。
	if err := db.Exec(`
		ALTER TABLE project_locales ENABLE ROW LEVEL SECURITY;
	`).Error; err != nil {
		t.Fatalf("启用 RLS 失败: %v", err)
	}
	if err := db.Exec(`
		ALTER TABLE project_locales FORCE ROW LEVEL SECURITY;
	`).Error; err != nil {
		t.Fatalf("FORCE RLS 失败: %v", err)
	}
	if err := db.Exec(`CREATE POLICY project_isolation ON project_locales
		USING (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid)
		WITH CHECK (project_id = NULLIF(current_setting('app.project_id', true), '')::uuid)`).Error; err != nil {
		t.Fatalf("建策略失败: %v", err)
	}

	// 授权并切换到非超级角色：从此连接上所有语句都受 RLS 约束（超级用户绕过 RLS）。
	if err := db.Exec(fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO %s", schema, role)).Error; err != nil {
		t.Fatalf("授权 schema 失败: %v", err)
	}
	if err := db.Exec(fmt.Sprintf("GRANT SELECT, INSERT, DELETE, UPDATE ON project_locales TO %s", role)).Error; err != nil {
		t.Fatalf("授权表失败: %v", err)
	}
	if err := db.Exec(fmt.Sprintf("SET ROLE %s", role)).Error; err != nil {
		t.Fatalf("切换角色失败: %v", err)
	}

	m := NewProjectModel(db)
	pA := uuid.NewString()
	pB := uuid.NewString()
	now := time.Now()
	// 策略已在表上，写入必须经 withProjectScope 逐工程进行。
	fixtures := [][]LocaleEntity{
		{
			{ProjectID: pA, Lang: "en", SortOrder: 1, IsDefault: true, Enabled: true, CreatedAt: now, UpdatedAt: now},
			{ProjectID: pA, Lang: "de", SortOrder: 2, IsDefault: false, Enabled: true, CreatedAt: now, UpdatedAt: now},
		},
		{
			{ProjectID: pB, Lang: "zh", SortOrder: 1, IsDefault: true, Enabled: true, CreatedAt: now, UpdatedAt: now},
			{ProjectID: pB, Lang: "ja", SortOrder: 2, IsDefault: false, Enabled: true, CreatedAt: now, UpdatedAt: now},
		},
	}
	for _, rows := range fixtures {
		if err := m.withProjectScope(context.Background(), rows[0].ProjectID, func(tx *gorm.DB) error {
			return tx.Create(&rows).Error
		}); err != nil {
			t.Fatalf("写入夹具失败: %v", err)
		}
	}
	return m, pA, pB
}

func TestRLSProjectLocalesIsolation(t *testing.T) {
	m, pA, pB := rlsFixture(t)
	ctx := context.Background()

	listA, err := m.ListLocales(ctx, pA)
	if err != nil {
		t.Fatalf("ListLocales(A) 失败: %v", err)
	}
	if len(listA) != 2 || listA[0].ProjectID != pA || listA[1].ProjectID != pA {
		t.Fatalf("工程 A 应只见自己的 2 行, got %+v", listA)
	}

	listB, err := m.ListLocales(ctx, pB)
	if err != nil {
		t.Fatalf("ListLocales(B) 失败: %v", err)
	}
	if len(listB) != 2 || listB[0].ProjectID != pB {
		t.Fatalf("工程 B 应只见自己的 2 行, got %+v", listB)
	}
}

func TestRLSProjectLocalesReplaceScopedToProject(t *testing.T) {
	m, pA, pB := rlsFixture(t)
	ctx := context.Background()

	// 工程 A 全量替换：先删（只删 A 可见行）再写 A 的新清单。
	now := time.Now()
	rows := []LocaleEntity{
		{ProjectID: pA, Lang: "fr", SortOrder: 1, IsDefault: true, Enabled: true, CreatedAt: now, UpdatedAt: now},
	}
	if err := m.ReplaceLocales(ctx, pA, rows); err != nil {
		t.Fatalf("ReplaceLocales(A) 失败: %v", err)
	}

	listA, err := m.ListLocales(ctx, pA)
	if err != nil {
		t.Fatalf("ListLocales(A) 失败: %v", err)
	}
	if len(listA) != 1 || listA[0].Lang != "fr" {
		t.Fatalf("工程 A 应只剩 fr 一行, got %+v", listA)
	}

	// 工程 B 的行不受 A 的全量替换波及（隔离的写侧证明）。
	listB, err := m.ListLocales(ctx, pB)
	if err != nil {
		t.Fatalf("ListLocales(B) 失败: %v", err)
	}
	if len(listB) != 2 {
		t.Fatalf("工程 B 应仍有 2 行, got %+v", listB)
	}
}

func TestRLSProjectLocalesFailClosedWithoutVariable(t *testing.T) {
	m, pA, _ := rlsFixture(t)

	// 绕开 withProjectScope 直接裸查：变量未设置，必须一行不可见（fail closed）。
	var n int64
	if err := m.LocaleDB(context.Background()).Session(&gorm.Session{}).
		Raw("SELECT COUNT(*) FROM project_locales").Scan(&n).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("未设置 app.project_id 时应 0 行可见（fail closed）, got %d", n)
	}

	// 非法 uuid 在 withProjectScope 入口被拒绝，不触达数据库强转报错。
	if err := m.withProjectScope(context.Background(), "not-a-uuid", func(tx *gorm.DB) error {
		t.Fatal("非法 uuid 不应执行事务体")
		return nil
	}); err == nil {
		t.Fatal("非法 uuid 应返回错误")
	}

	// pA 仍可正常读取：证明事务结束后变量已还原、连接可复用。
	if _, err := m.ListLocales(context.Background(), pA); err != nil {
		t.Fatalf("事务后续读失败: %v", err)
	}
}
