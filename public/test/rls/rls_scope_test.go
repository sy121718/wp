package rlstest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	blockmodel "go_wp/internal/module/block/model"
	pkgpartition "go_wp/pkg/database/partition"
	"go_wp/pkg/rls"
	"go_wp/public/schema"
	"go_wp/public/test/support"
)

func pgEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func pgDSN(dbname string) string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		pgEnv("PGHOST", "127.0.0.1"), pgEnv("PGUSER", "root"), pgEnv("PGPASSWORD", "root"),
		dbname, pgEnv("PGPORT", "5432"))
}

func seedProject(t *testing.T, db *gorm.DB, id, name string) {
	t.Helper()
	if err := db.Exec(
		"INSERT INTO projects (id, name, settings, create_time, update_time) VALUES (?, ?, '{}'::jsonb, NOW(), NOW())",
		id, name,
	).Error; err != nil {
		t.Fatalf("准备工程行失败：%v", err)
	}
}

func rlsFixture(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	admin, err := gorm.Open(postgres.Open(pgDSN(pgEnv("PGDATABASE", "wp_test"))), &gorm.Config{})
	if err != nil {
		support.FailIfRequiredPG(t, err)
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil, ""
	}
	adminSQL, err := admin.DB()
	if err != nil {
		support.FailIfRequiredPG(t, err)
		t.Skipf("取管理连接失败：%v", err)
		return nil, ""
	}
	if err := adminSQL.Ping(); err != nil {
		adminSQL.Close()
		support.FailIfRequiredPG(t, err)
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil, ""
	}

	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("生成随机名字失败: %v", err)
	}
	suffix := hex.EncodeToString(buf[:])

	role := "rls_app_" + suffix
	if err := admin.Exec("CREATE ROLE " + role + " NOLOGIN").Error; err != nil {
		adminSQL.Close()
		t.Fatalf("建非超级角色失败: %v", err)
	}
	dbName := "rls_t_" + suffix
	if err := admin.Exec("CREATE DATABASE " + dbName).Error; err != nil {
		_ = admin.Exec("DROP ROLE IF EXISTS " + role).Error
		adminSQL.Close()
		t.Fatalf("建测试库失败: %v", err)
	}

	db, err := gorm.Open(postgres.Open(pgDSN(dbName)+" search_path=public,ext_shared"), &gorm.Config{})
	if err != nil {
		adminSQL.Close()
		t.Fatalf("打开测试库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("取底层连接失败: %v", err)
	}

	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = db.Exec("RESET ROLE").Error
		sqlDB.Close()
		_ = admin.Exec("DROP DATABASE IF EXISTS " + dbName).Error
		_ = admin.Exec("DROP ROLE IF EXISTS " + role).Error
		adminSQL.Close()
	})

	if err := schema.Apply(db); err != nil {
		t.Fatalf("载入 schema 失败: %v", err)
	}

	if err := db.Exec("GRANT USAGE ON SCHEMA public TO " + role).Error; err != nil {
		t.Fatalf("授权 schema 失败: %v", err)
	}
	if err := db.Exec("GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO " + role).Error; err != nil {
		t.Fatalf("授权表失败: %v", err)
	}
	if err := db.Exec("GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO " + role).Error; err != nil {
		t.Fatalf("授权序列失败: %v", err)
	}
	if err := db.Exec("SET ROLE " + role).Error; err != nil {
		t.Fatalf("切换角色失败: %v", err)
	}

	bypass, err := rls.BypassedRole(context.Background(), db)
	if err != nil {
		t.Fatalf("查询角色属性失败: %v", err)
	}
	if bypass {
		t.Fatalf("测试跑在会绕过 RLS 的角色上（superuser / BYPASSRLS），断言将全部失真")
	}
	return db, role
}

func seedBlock(t *testing.T, db *gorm.DB, projectID, name string) string {
	t.Helper()
	id := uuid.NewString()
	now := time.Now()
	err := blockmodel.NewBlockModel(db).Create(context.Background(), &blockmodel.BlockEntity{
		ID:         id,
		ProjectID:  projectID,
		Name:       name,
		Kind:       blockmodel.KindBlock,
		Category:   blockmodel.DefaultCategory,
		ReuseMode:  blockmodel.ReuseGlobal,
		Document:   []byte(`{"root":{}}`),
		CreateTime: now,
		UpdatedAt:  now,
	})
	if err != nil {
		t.Fatalf("写入块失败（RLS 生效时写入必须经 InProjectScope）: %v", err)
	}
	return id
}

func TestRLS_FailClosed_WithoutScope(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()

	pA := uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedBlock(t, db, pA, "本工程的块")

	var raw int64
	if err := db.Table("blocks").Where("project_id = ?", pA).Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("未设 app.project_id 时应 0 行可见（fail closed），实际 %d 行", raw)
	}

	var viaBare int64
	if err := blockmodel.NewBlockModel(db).DB(ctx).Where("project_id = ?", pA).Count(&viaBare).Error; err != nil {
		t.Fatalf("裸句柄查询失败: %v", err)
	}
	if viaBare != 0 {
		t.Fatalf("未经作用域的读取应返回 0 行，实际 %d 行", viaBare)
	}
}

func TestRLS_ScopeReadsOwnProjectOnly(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	m := blockmodel.NewBlockModel(db)

	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")
	seedBlock(t, db, pA, "A 的块")
	seedBlock(t, db, pB, "B 的块")

	listA, err := m.ListByProject(ctx, pA, "", "", "")
	if err != nil {
		t.Fatalf("读工程 A 失败: %v", err)
	}
	if len(listA) != 1 || listA[0].ProjectID != pA {
		t.Fatalf("工程 A 应只见自己的 1 行，实际 %+v", listA)
	}

	listB, err := m.ListByProject(ctx, pB, "", "", "")
	if err != nil {
		t.Fatalf("读工程 B 失败: %v", err)
	}
	if len(listB) != 1 || listB[0].ProjectID != pB {
		t.Fatalf("工程 B 应只见自己的 1 行，实际 %+v", listB)
	}

	if _, err := m.GetByID(ctx, listB[0].ID, pA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("拿工程 A 的作用域按 id 读工程 B 的块，应 ErrRecordNotFound，实际 %v", err)
	}
	if _, err := m.GetByID(ctx, listA[0].ID, pA); err != nil {
		t.Fatalf("按 id 读本工程的行应成功，实际 %v", err)
	}
}

func TestRLS_WriteCheckRejectsForeignRow(t *testing.T) {
	db, _ := rlsFixture(t)

	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")

	now := time.Now()
	err := rls.InProjectScope(context.Background(), db, pA, func(tx *gorm.DB) error {

		return tx.Model(&blockmodel.BlockEntity{}).Create(&blockmodel.BlockEntity{
			ID:         uuid.NewString(),
			ProjectID:  pB,
			Name:       "越界写入",
			Kind:       blockmodel.KindBlock,
			Category:   blockmodel.DefaultCategory,
			ReuseMode:  blockmodel.ReuseGlobal,
			Document:   []byte(`{"root":{}}`),
			CreateTime: now,
			UpdatedAt:  now,
		}).Error
	})
	if err == nil {
		t.Fatal("跨工程写入必须被策略的 WITH CHECK 拒绝，实际写入成功")
	}
}

func TestRLS_ScopeIsTransactionLocal(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()

	pA := uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedBlock(t, db, pA, "A 的块")

	if _, err := blockmodel.NewBlockModel(db).ListByProject(ctx, pA, "", "", ""); err != nil {
		t.Fatalf("事务内读取失败: %v", err)
	}

	var leftover string
	if err := db.Raw("SELECT COALESCE(current_setting('app.project_id', true), '')").Scan(&leftover).Error; err != nil {
		t.Fatalf("读取会话变量失败: %v", err)
	}
	if leftover != "" {
		t.Fatalf("事务结束后 app.project_id 应已还原为空，实际 %q —— 会话变量泄漏给了下一次查询", leftover)
	}
	var raw int64
	if err := db.Table("blocks").Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("还原后应重新 fail closed（0 行），实际 %d 行", raw)
	}

	if list, err := blockmodel.NewBlockModel(db).ListByProject(ctx, pA, "", "", ""); err != nil || len(list) != 1 {
		t.Fatalf("重新设 scope 后应读到 1 行，实际 %d 行（err=%v）", len(list), err)
	}
}

func TestRLS_ScopeTxRejectsNonTransactionHandle(t *testing.T) {
	db, _ := rlsFixture(t)
	if err := rls.ScopeTx(db, uuid.NewString()); !errors.Is(err, rls.ErrNotInTransaction) {
		t.Fatalf("非事务句柄应返回 ErrNotInTransaction，实际 %v", err)
	}

	if err := rls.InProjectScope(context.Background(), db, "not-a-uuid", func(*gorm.DB) error {
		t.Fatal("非法 uuid 不应执行事务体")
		return nil
	}); !errors.Is(err, rls.ErrInvalidProjectID) {
		t.Fatalf("非法 uuid 应返回 ErrInvalidProjectID，实际 %v", err)
	}
}

func TestRLS_MigrationCoversScopedTables(t *testing.T) {
	db, _ := rlsFixture(t)

	type relRow struct {
		Relname             string
		Relrowsecurity      bool
		Relforcerowsecurity bool
		Policies            int64
	}
	var rows []relRow
	if err := db.Raw(`SELECT c.relname,
			c.relrowsecurity, c.relforcerowsecurity,
			(SELECT COUNT(*) FROM pg_policy p WHERE p.polrelid = c.oid) AS policies
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relkind IN ('r','p')
		  AND c.relname LIKE 'master_data_changes%'`).Scan(&rows).Error; err != nil {
		t.Fatalf("查询分区策略失败: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("没有查到 master_data_changes 及其分区")
	}
	for _, r := range rows {
		if !r.Relrowsecurity || !r.Relforcerowsecurity {
			t.Errorf("%s 应同时 ENABLE + FORCE ROW LEVEL SECURITY（rls=%v force=%v）",
				r.Relname, r.Relrowsecurity, r.Relforcerowsecurity)
		}
		if r.Policies == 0 {
			t.Errorf("%s 缺 project_isolation 策略", r.Relname)
		}
	}
}

func TestRLS_EnsureAheadAddsPolicyToNewPartitions(t *testing.T) {
	db, _ := rlsFixture(t)

	if err := db.Exec("RESET ROLE").Error; err != nil {
		t.Fatalf("恢复属主身份失败: %v", err)
	}

	created, err := pkgpartition.EnsureAhead(context.Background(), db, schema.PartitionOptions(), 6)
	if err != nil {
		t.Fatalf("EnsureAhead 失败: %v", err)
	}
	if len(created) == 0 {
		t.Fatal("预期至少新建一个月分区（否则本用例没有验证目标）")
	}
	for _, name := range created {
		var enabled, forced bool
		var policies int64
		if err := db.Raw(`SELECT c.relrowsecurity, c.relforcerowsecurity,
				(SELECT COUNT(*) FROM pg_policy p WHERE p.polrelid = c.oid)
			FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = current_schema() AND c.relname = ?`, name).
			Row().Scan(&enabled, &forced, &policies); err != nil {
			t.Fatalf("查询新分区 %s 的策略失败: %v", name, err)
		}
		if !enabled || !forced || policies == 0 {
			t.Errorf("新分区 %s 未补上工程隔离（rls=%v force=%v policies=%d）", name, enabled, forced, policies)
		}
	}
}

func TestRLS_NestedScopeOverridesOuterScope(t *testing.T) {
	db, _ := rlsFixture(t)

	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")

	var seen string
	err := rls.InProjectScope(context.Background(), db, pA, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, pB); serr != nil {
			return serr
		}
		return tx.Raw("SELECT COALESCE(current_setting('app.project_id', true), '')").Scan(&seen).Error
	})
	if err != nil {
		t.Fatalf("嵌套设置作用域失败: %v", err)
	}
	if seen != pB {
		t.Fatalf("嵌套设置应覆盖外层作用域（期望 %q，实际 %q）—— 该语义变了说明 *Tx 变体的纪律前提变了", pB, seen)
	}

	var after string
	if err := db.Raw("SELECT COALESCE(current_setting('app.project_id', true), '')").Scan(&after).Error; err != nil {
		t.Fatalf("读取会话变量失败: %v", err)
	}
	if after != "" {
		t.Fatalf("事务结束后应还原为空，实际 %q", after)
	}
}
