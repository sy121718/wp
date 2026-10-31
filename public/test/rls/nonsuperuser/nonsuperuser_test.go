package nonsuperuser

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"go_wp/internal/partition"
	"go_wp/pkg/rls"
	"go_wp/public/test/support"
)

const (
	appRole = "wp_test_rls_regress"

	appPassword = "wp_test_rls_regress"

	localSchema = "public"

	sharedExtSchema = "ext_shared"
)

func pgEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func appDSN(dbname string) string {
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai search_path=%s,%s",
		pgEnv("PGHOST", support.DefaultPGHost), appRole, appPassword, dbname,
		pgEnv("PGPORT", support.DefaultPGPort), localSchema, sharedExtSchema)
}

type fixture struct {
	admin *gorm.DB

	app *gorm.DB
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	admin := support.NewMigratedPGTestDB(t)

	ensureAppRole(t, admin)
	grantToAppRole(t, admin)

	var dbName string
	if err := admin.Raw("SELECT current_database()").Scan(&dbName).Error; err != nil {
		t.Fatalf("读取当前库名失败：%v", err)
	}
	app, err := gorm.Open(postgres.Open(appDSN(dbName)), &gorm.Config{})
	if err != nil {
		t.Skipf("以非超级角色建立第二条连接失败（可能是 pg_hba 不允许该角色登录）：%v", err)
		return nil
	}
	sqlDB, err := app.DB()
	if err != nil {
		t.Fatalf("取业务连接底层句柄失败：%v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	var currentUser string
	if err := app.Raw("SELECT current_user").Scan(&currentUser).Error; err != nil {
		t.Fatalf("读取业务连接当前角色失败：%v", err)
	}
	if currentUser != appRole {
		t.Fatalf("业务连接当前角色应为 %q，实际 %q", appRole, currentUser)
	}

	bypass, err := rls.BypassedRole(context.Background(), app)
	if err != nil {
		t.Fatalf("查询角色 RLS 属性失败：%v", err)
	}
	if bypass {
		t.Fatalf("角色 %s 是 superuser / BYPASSRLS，RLS 对它不生效，本包断言将全部失真", appRole)
	}
	return &fixture{admin: admin, app: app}
}

func ensureAppRole(t *testing.T, admin *gorm.DB) {
	t.Helper()
	create := fmt.Sprintf("DO $$ BEGIN\n"+
		"    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%s') THEN\n"+
		"        CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOBYPASSRLS;\n"+
		"    END IF;\n"+
		"END $$", appRole, appRole, appPassword)

	if err := admin.Exec(create).Error; err != nil && !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("建立非超级角色 %s 失败：%v", appRole, err)
	}
	alter := fmt.Sprintf("ALTER ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOBYPASSRLS", appRole, appPassword)
	if err := admin.Exec(alter).Error; err != nil {
		t.Fatalf("对齐角色 %s 属性失败：%v", appRole, err)
	}
}

func grantToAppRole(t *testing.T, admin *gorm.DB) {
	t.Helper()
	stmts := []string{
		"GRANT USAGE ON SCHEMA " + localSchema + " TO " + appRole,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA " + localSchema + " TO " + appRole,
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA " + localSchema + " TO " + appRole,

		"ALTER DEFAULT PRIVILEGES IN SCHEMA " + localSchema + " GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO " + appRole,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA " + localSchema + " GRANT USAGE, SELECT ON SEQUENCES TO " + appRole,
	}
	var hasExt bool
	if err := admin.Raw("SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = ?)", sharedExtSchema).
		Scan(&hasExt).Error; err == nil && hasExt {
		stmts = append(stmts, "GRANT USAGE ON SCHEMA "+sharedExtSchema+" TO "+appRole)
	}
	for _, stmt := range stmts {
		if err := admin.Exec(stmt).Error; err != nil {
			t.Fatalf("授权失败（%s）：%v", stmt, err)
		}
	}
}

func seedBrand(t *testing.T, admin *gorm.DB, projectID, name, slug string) string {
	t.Helper()
	id := uuid.NewString()
	if err := admin.Exec(
		"INSERT INTO product_brands (id, project_id, name, slug, create_time, update_time) VALUES (?, ?, ?, ?, NOW(), NOW())",
		id, projectID, name, slug,
	).Error; err != nil {
		t.Fatalf("播种品牌行失败：%v", err)
	}
	return id
}

func countOn(t *testing.T, db *gorm.DB, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(sql, args...).Scan(&n).Error; err != nil {
		t.Fatalf("计数查询失败（%s）：%v", sql, err)
	}
	return n
}

func countInScope(t *testing.T, app *gorm.DB, projectID, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	err := rls.InProjectScope(context.Background(), app, projectID, func(tx *gorm.DB) error {
		return tx.Raw(sql, args...).Scan(&n).Error
	})
	if err != nil {
		t.Fatalf("作用域内计数失败（%s）：%v", sql, err)
	}
	return n
}

func TestNonSuperuser_FailClosedWithoutScope(t *testing.T) {
	f := newFixture(t)
	pA := uuid.NewString()
	support.SeedProjectRow(t, f.admin, pA, "工程 A")
	brandID := seedBrand(t, f.admin, pA, "A 的品牌", "a-brand")

	if n := countOn(t, f.admin, "SELECT count(*) FROM product_brands"); n != 1 {
		t.Fatalf("管理连接应看到 1 行（证明数据存在），实际 %d", n)
	}
	if n := countOn(t, f.app, "SELECT count(*) FROM product_brands"); n != 0 {
		t.Fatalf("非超级角色未设 app.project_id 时应 0 行（fail closed），实际 %d", n)
	}

	if n := countOn(t, f.app, "SELECT count(*) FROM product_brands WHERE id = ?", brandID); n != 0 {
		t.Fatalf("未设作用域时按 id 单查也应 0 行，实际 %d", n)
	}
}

func TestNonSuperuser_ScopeSeesOwnProjectOnly(t *testing.T) {
	f := newFixture(t)
	pA, pB := uuid.NewString(), uuid.NewString()
	support.SeedProjectRow(t, f.admin, pA, "工程 A")
	support.SeedProjectRow(t, f.admin, pB, "工程 B")
	seedBrand(t, f.admin, pA, "A 的品牌", "a-brand")
	seedBrand(t, f.admin, pB, "B 的品牌", "b-brand")

	if n := countOn(t, f.admin, "SELECT count(*) FROM product_brands"); n != 2 {
		t.Fatalf("管理连接应看到 2 行，实际 %d", n)
	}

	for _, tc := range []struct {
		name      string
		projectID string
		wantSlug  string
	}{
		{"工程 A", pA, "a-brand"},
		{"工程 B", pB, "b-brand"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if n := countInScope(t, f.app, tc.projectID, "SELECT count(*) FROM product_brands"); n != 1 {
				t.Fatalf("作用域 %s 内应只看到 1 行，实际 %d", tc.projectID, n)
			}

			var slug, projectID string
			err := rls.InProjectScope(context.Background(), f.app, tc.projectID, func(tx *gorm.DB) error {
				return tx.Raw("SELECT slug, project_id FROM product_brands").Row().Scan(&slug, &projectID)
			})
			if err != nil {
				t.Fatalf("作用域内取行失败：%v", err)
			}
			if slug != tc.wantSlug || projectID != tc.projectID {
				t.Fatalf("作用域 %s 内读到的应是自己的行，实际 slug=%q project=%s", tc.projectID, slug, projectID)
			}

			other := pB
			if tc.projectID == pB {
				other = pA
			}
			if n := countInScope(t, f.app, tc.projectID,
				"SELECT count(*) FROM product_brands WHERE project_id = ?", other); n != 0 {
				t.Fatalf("作用域 %s 内不应看到他工程的行，实际 %d", tc.projectID, n)
			}
		})
	}
}

func TestNonSuperuser_WriteCheckRejectsForeignProject(t *testing.T) {
	f := newFixture(t)
	pA, pB := uuid.NewString(), uuid.NewString()
	support.SeedProjectRow(t, f.admin, pA, "工程 A")
	support.SeedProjectRow(t, f.admin, pB, "工程 B")

	insertBrand := func(projectID, slug string) error {
		return rls.InProjectScope(context.Background(), f.app, pA, func(tx *gorm.DB) error {
			return tx.Exec(
				"INSERT INTO product_brands (id, project_id, name, slug, create_time, update_time) VALUES (?, ?, ?, ?, NOW(), NOW())",
				uuid.NewString(), projectID, "写入探针", slug,
			).Error
		})
	}

	if err := insertBrand(pA, "own-brand"); err != nil {
		t.Fatalf("作用域 A 内写 A 的行应成功（正向对照），实际失败：%v", err)
	}
	err := insertBrand(pB, "foreign-brand")
	if err == nil {
		t.Fatal("作用域 A 内写 B 的行必须被策略的 WITH CHECK 拒绝，实际写入成功")
	}

	if !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("期望 row-level security 违例，实际错误：%v", err)
	}

	if n := countOn(t, f.admin, "SELECT count(*) FROM product_brands WHERE slug = ?", "foreign-brand"); n != 0 {
		t.Fatalf("被拒的跨工程写入不应落库，实际 %d 行", n)
	}
	if n := countOn(t, f.admin, "SELECT count(*) FROM product_brands WHERE slug = ?", "own-brand"); n != 1 {
		t.Fatalf("本工程写入应落库，实际 %d 行", n)
	}
}

func TestNonSuperuser_NewPartitionChildCovered(t *testing.T) {
	f := newFixture(t)
	pA, pB := uuid.NewString(), uuid.NewString()
	support.SeedProjectRow(t, f.admin, pA, "工程 A")
	support.SeedProjectRow(t, f.admin, pB, "工程 B")

	before := relNames(t, f.admin, "page_views_%")
	created, err := partition.EnsureAhead(context.Background(), f.admin, 6)
	if err != nil {
		t.Fatalf("EnsureAhead 失败：%v", err)
	}

	var child, month string
	for _, name := range created {
		if !strings.HasPrefix(name, "page_views_") || before[name] {
			continue
		}
		if m, ok := partitionMonth(name); ok && (month == "" || m > month) {
			child, month = name, m
		}
	}
	if child == "" {
		t.Fatalf("EnsureAhead 未建出新的 page_views 月分区（返回 %v），本用例没有验证目标", created)
	}

	assertPartitionPolicy(t, f.admin, child)

	at := mustMonthTime(t, month)
	if err := f.admin.Exec(
		"INSERT INTO page_views (project_id, path, viewed_at) VALUES (?, ?, ?)",
		pA, "/rls-partition-probe", at,
	).Error; err != nil {
		t.Fatalf("向新分区播种失败：%v", err)
	}
	if n := countOn(t, f.admin, "SELECT count(*) FROM "+child+" WHERE project_id = ?", pA); n != 1 {
		t.Fatalf("探针行应落在新子表 %s，实际 %d 行", child, n)
	}

	if n := countOn(t, f.app, "SELECT count(*) FROM "+child); n != 0 {
		t.Fatalf("非超级角色未设作用域时读子表 %s 应 0 行，实际 %d", child, n)
	}
	if n := countInScope(t, f.app, pA, "SELECT count(*) FROM "+child); n != 1 {
		t.Fatalf("作用域 A 内读子表 %s 应 1 行，实际 %d", child, n)
	}
	if n := countInScope(t, f.app, pB, "SELECT count(*) FROM "+child); n != 0 {
		t.Fatalf("作用域 B 内读子表 %s 应 0 行（他工程的行不可见），实际 %d", child, n)
	}

	if n := countOn(t, f.app, "SELECT count(*) FROM page_views"); n != 0 {
		t.Fatalf("未设作用域时读父表应 0 行，实际 %d", n)
	}
	if n := countInScope(t, f.app, pA, "SELECT count(*) FROM page_views"); n != 1 {
		t.Fatalf("作用域 A 内读父表应 1 行，实际 %d", n)
	}
}

var exemptTables = map[string]string{

	"build_jobs": "构建任务队列按状态跨工程捞取，纳入 RLS 需先改造队列调度（本票不做）",

	"product_outbox_events": "商品失效事件队列按状态跨工程捞取，纳入 RLS 需先改造消费侧（本票不做）",
}

func TestNonSuperuser_EveryProjectTableIsCovered(t *testing.T) {
	f := newFixture(t)

	type relRow struct {
		Relname             string
		Relrowsecurity      bool
		Relforcerowsecurity bool
		Policies            int64
		Quals               string
	}
	var rows []relRow

	sweep := "SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity, " +
		"(SELECT COUNT(*) FROM pg_policy p WHERE p.polrelid = c.oid) AS policies, " +
		"COALESCE((SELECT string_agg(COALESCE(pg_get_expr(p.polqual, p.polrelid), '') || ' ' || " +
		"COALESCE(pg_get_expr(p.polwithcheck, p.polrelid), ''), ' ') " +
		"FROM pg_policy p WHERE p.polrelid = c.oid), '') AS quals " +
		"FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace " +
		"WHERE n.nspname = current_schema() AND c.relkind IN ('r', 'p') " +
		"AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.oid AND a.attname = 'project_id' " +
		"AND a.attnum > 0 AND NOT a.attisdropped) ORDER BY c.relname"
	if err := f.admin.Raw(sweep).Scan(&rows).Error; err != nil {
		t.Fatalf("盘点工程表失败：%v", err)
	}
	if len(rows) < 30 {
		t.Fatalf("只盘点到 %d 张带 project_id 的表，怀疑查询本身没生效（迁移 215 名单是 50+ 个对象）", len(rows))
	}

	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.Relname] = true
		if _, ok := exemptTables[r.Relname]; ok {
			continue
		}
		if !r.Relrowsecurity || !r.Relforcerowsecurity {
			t.Errorf("%s 应同时 ENABLE + FORCE ROW LEVEL SECURITY（rls=%v force=%v）",
				r.Relname, r.Relrowsecurity, r.Relforcerowsecurity)
		}
		if r.Policies == 0 {
			t.Errorf("%s 缺工程隔离策略", r.Relname)
		}
		if !strings.Contains(r.Quals, "app.project_id") {
			t.Errorf("%s 的策略谓词没有读 app.project_id（quals=%q）", r.Relname, r.Quals)
		}
	}

	for name := range exemptTables {
		if !seen[name] {
			t.Errorf("豁免条目 %q 已不在带 project_id 的表里（或表已改名），请从 exemptTables 删除", name)
		}
	}

	for _, name := range []string{"products", "product_brands", "product_categories", "product_tags",
		"inventory_stocks", "page_views", "pages", "blocks"} {
		if !seen[name] {
			t.Errorf("代表表 %s 未出现在盘点结果里（策略被整体删掉时会这样）", name)
		}
	}
}

func relNames(t *testing.T, db *gorm.DB, like string) map[string]bool {
	t.Helper()
	var names []string
	if err := db.Raw("SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace "+
		"WHERE n.nspname = current_schema() AND c.relkind = 'r' AND c.relname LIKE ?", like,
	).Scan(&names).Error; err != nil {
		t.Fatalf("读取 relation 名失败：%v", err)
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out
}

func partitionMonth(name string) (string, bool) {
	idx := strings.LastIndex(name, "_")
	if idx < 0 {
		return "", false
	}
	prev := strings.LastIndex(name[:idx], "_")
	if prev < 0 {
		return "", false
	}
	month := name[prev+1:]
	if len(month) != len("2006_01") {
		return "", false
	}
	if _, err := time.Parse("2006_01", month); err != nil {
		return "", false
	}
	return month, true
}

func mustMonthTime(t *testing.T, month string) time.Time {
	t.Helper()
	m, err := time.Parse("2006_01", month)
	if err != nil {
		t.Fatalf("解析月份 %q 失败：%v", month, err)
	}
	return time.Date(m.Year(), m.Month(), 15, 12, 0, 0, 0, time.UTC)
}

func assertPartitionPolicy(t *testing.T, db *gorm.DB, name string) {
	t.Helper()
	type polRow struct {
		Relrowsecurity      bool
		Relforcerowsecurity bool
		Policies            int64
		Quals               string
	}
	var row polRow
	q := "SELECT c.relrowsecurity, c.relforcerowsecurity, " +
		"(SELECT COUNT(*) FROM pg_policy p WHERE p.polrelid = c.oid) AS policies, " +
		"COALESCE((SELECT string_agg(COALESCE(pg_get_expr(p.polqual, p.polrelid), '') || ' ' || " +
		"COALESCE(pg_get_expr(p.polwithcheck, p.polrelid), ''), ' ') " +
		"FROM pg_policy p WHERE p.polrelid = c.oid), '') AS quals " +
		"FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace " +
		"WHERE n.nspname = current_schema() AND c.relname = ?"
	if err := db.Raw(q, name).Scan(&row).Error; err != nil {
		t.Fatalf("查询分区 %s 的策略失败：%v", name, err)
	}
	if !row.Relrowsecurity || !row.Relforcerowsecurity {
		t.Fatalf("新分区 %s 应同时 ENABLE + FORCE ROW LEVEL SECURITY（rls=%v force=%v）",
			name, row.Relrowsecurity, row.Relforcerowsecurity)
	}
	if row.Policies == 0 || !strings.Contains(row.Quals, "app.project_id") {
		t.Fatalf("新分区 %s 的工程隔离策略缺失或谓词不对（policies=%d quals=%q）", name, row.Policies, row.Quals)
	}
}
