// Package nonsuperuser 用**真实的非超级角色第二条连接**验证工程隔离（RLS，审计 DB-009）
// 在「换连接角色」之后是不是真的挡得住。
//
// 与上层 public/test/rls（SET ROLE 单连接那一套）的分工：
//
//   - 上层用 SET ROLE 把已有连接切到非超级角色，证明「包了 rls.InProjectScope 就能读到本工程」；
//   - 本包另开**一条真正用 NOSUPERUSER NOBYPASSRLS 登录角色认证的连接**，证明的是
//     「应用连接换成非超级角色之后」的四件事：
//     (1) 未设 app.project_id 时策略表 0 行（fail closed）；
//     (2) 事务内设成工程 A 时只能看到 A 的行，工程 B 的行不可见；
//     (3) 以非超级身份写「别的工程」的行被 WITH CHECK 拒绝；
//     (4) 新分区子表（partition.EnsureAhead 建出来的）同样在覆盖内。
//
// 为什么值得单开一条连接而不是复用 SET ROLE：SET ROLE 是**会话级**状态，连接池里
// 只要多一条连接，后续语句就可能落在没切角色的那条上（上层用例因此把池锁成 1 条）；
// 而角色写在 DSN 里时，池里每条连接天然都是该角色，不需要任何会话级状态。
//
// 库用 support.NewMigratedPGTestDB（模板库复制 + 结构全部来自生产迁移），
// 不手抄 DDL、不重跑迁移。
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
	// appRole 本包使用的非超级角色名。
	//
	// **固定名**（不是随机后缀）是刻意的：pg_roles 是**集群级**对象，随机名每跑一次
	// 就在集群里留一个角色，而测试结束不清理角色（并发用例正在用同一个名字，
	// 谁都不能安全地 DROP）。固定名 + 幂等建/改让重复跑与并发跑都收敛到同一个角色。
	// 前缀 wp_test_ 与业务角色（root、scripts/rls-role-setup.sh 建的角色）区分开。
	appRole = "wp_test_rls_regress"
	// appPassword 与角色名同值：只用于本机测试库的临时角色。
	appPassword = "wp_test_rls_regress"

	// localSchema 测试库的业务 schema（support 的隔离单位是**库**，不是 schema）。
	localSchema = "public"
	// sharedExtSchema 承载 pg_trgm 的专用 schema（迁移 210），必须进 search_path。
	sharedExtSchema = "ext_shared"
)

// pgEnv 读标准 libpq 环境变量，空则回退（默认值取自 support 的导出口径）。
func pgEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// appDSN 拼业务连接（角色 = appRole）的 DSN。
//
// host/port 与环境变量覆盖口径与 support.localPGEndpoint 一致（后者不导出，
// 这里只用它导出的 Default* 常量拼，避免两套默认值各自漂移）。
func appDSN(dbname string) string {
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai search_path=%s,%s",
		pgEnv("PGHOST", support.DefaultPGHost), appRole, appPassword, dbname,
		pgEnv("PGPORT", support.DefaultPGPort), localSchema, sharedExtSchema)
}

// fixture 一套「生产结构 + 真实非超级角色」的环境。
type fixture struct {
	// admin 管理连接（超级用户）：建角色、授权、播种、建分区 —— 这些是运维/属主动作，
	// 不经过应用连接（应用连接连 GRANT 都没有）。
	admin *gorm.DB
	// app 业务连接：以非超级角色登录的第二条连接，RLS 在它上面真正生效。
	app *gorm.DB
}

// newFixture 建隔离库（生产迁移结构）→ 建/复用非超级角色 → 授权 → 开第二条连接。
//
// 环境不满足时 t.Skip 并写明原因（PG 不可用由 support 内部 Skip）：
// 角色建立、登录失败都可能是环境条件，但**只跳过明确的那些**，
// 不能把断言失败也咽下去。
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

	// 自检 1：这条连接当前确实是那个非超级角色（DSN 写错 / 有人改了角色都会在这里红）。
	var currentUser string
	if err := app.Raw("SELECT current_user").Scan(&currentUser).Error; err != nil {
		t.Fatalf("读取业务连接当前角色失败：%v", err)
	}
	if currentUser != appRole {
		t.Fatalf("业务连接当前角色应为 %q，实际 %q", appRole, currentUser)
	}
	// 自检 2：该角色不得绕过 RLS。superuser / BYPASSRLS 上任一条成立时，
	// 下面所有「看不见 / 写不进」的断言都会变绿而**没有一条是真的**。
	bypass, err := rls.BypassedRole(context.Background(), app)
	if err != nil {
		t.Fatalf("查询角色 RLS 属性失败：%v", err)
	}
	if bypass {
		t.Fatalf("角色 %s 是 superuser / BYPASSRLS，RLS 对它不生效，本包断言将全部失真", appRole)
	}
	return &fixture{admin: admin, app: app}
}

// ensureAppRole 幂等建/收敛非超级角色。
//
// 幂等的两层：① 存在性判断（CREATE ROLE 没有 IF NOT EXISTS）；
// ② 已存在时用 ALTER 把属性收敛回期望值 —— 上一次跑或被手工改过留下的漂移
// （被提成 SUPERUSER / BYPASSRLS）会让本包全部断言失真，必须在建连接前对齐。
func ensureAppRole(t *testing.T, admin *gorm.DB) {
	t.Helper()
	create := fmt.Sprintf("DO $$ BEGIN\n"+
		"    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%s') THEN\n"+
		"        CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOBYPASSRLS;\n"+
		"    END IF;\n"+
		"END $$", appRole, appRole, appPassword)
	// 并发跑本包时两个进程可能同时过存在性判断，落后的那个拿到 duplicate_object ——
	// 那是无害竞态（角色已经在了），属性下一步由 ALTER 收敛。
	if err := admin.Exec(create).Error; err != nil && !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("建立非超级角色 %s 失败：%v", appRole, err)
	}
	alter := fmt.Sprintf("ALTER ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOBYPASSRLS", appRole, appPassword)
	if err := admin.Exec(alter).Error; err != nil {
		t.Fatalf("对齐角色 %s 属性失败：%v", appRole, err)
	}
}

// grantToAppRole 授权 schema / 表 / 序列。
//
// 测试库是**每次新建**的（support 复制模板库），所以这里的授权只覆盖本库的既有对象，
// 不需要 ALTER DEFAULT PRIVILEGES（生产换角色时那份由 scripts/rls-role-setup.sh 负责）。
func grantToAppRole(t *testing.T, admin *gorm.DB) {
	t.Helper()
	stmts := []string{
		"GRANT USAGE ON SCHEMA " + localSchema + " TO " + appRole,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA " + localSchema + " TO " + appRole,
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA " + localSchema + " TO " + appRole,
		// 默认权限：本用例要在测试中途用 partition.EnsureAhead 建出**新的分区子表**，
		// 只授权「当下已有的对象」会让那张新表没有授权 —— 后续断言会先撞
		// permission denied（测到的是「没授权」，不是「策略覆盖」）。
		// 生产换角色时同一条由 scripts/rls-role-setup.sh 负责，不是测试特有的绕路。
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

// ── 断言/播种辅助 ────────────────────────────────────────────────────────────

// seedBrand 经管理连接插一行品牌（超级用户不走策略；这一行是后续「业务连接看不见」的对照物）。
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

// countOn 在给定连接上执行一条计数 SQL（不设任何作用域 —— 调用方要测的就是「没设」的形态）。
func countOn(t *testing.T, db *gorm.DB, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(sql, args...).Scan(&n).Error; err != nil {
		t.Fatalf("计数查询失败（%s）：%v", sql, err)
	}
	return n
}

// countInScope 在**业务连接**上设好工程作用域后计数。
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

// ── (1) fail closed ─────────────────────────────────────────────────────────

// TestNonSuperuser_FailClosedWithoutScope 未设 app.project_id 时策略表一行都读不到。
//
// 「0 行」必须与「表是空的」区分开：先用管理连接确认那一行确实在库里。
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
	// 按主键单查是「最像行不存在」的一条路径：这里它必须同样是 0 行。
	if n := countOn(t, f.app, "SELECT count(*) FROM product_brands WHERE id = ?", brandID); n != 0 {
		t.Fatalf("未设作用域时按 id 单查也应 0 行，实际 %d", n)
	}
}

// ── (2) 作用域内只见本工程 ──────────────────────────────────────────────────

// TestNonSuperuser_ScopeSeesOwnProjectOnly 事务内设成工程 A 时只看到 A 的行。
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
			// 看到的必须就是本工程的行（不是「数量恰好为 1」）。
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
			// 反证：他工程的行一条也读不到。
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

// ── (3) WITH CHECK 拒绝跨工程写入 ───────────────────────────────────────────

// TestNonSuperuser_WriteCheckRejectsForeignProject 作用域是 A 时写 B 的行必须被拒。
//
// 关键是有**正向对照**：同一个作用域里写 A 的行必须成功 —— 否则「报错」可能只是
// 这条 INSERT 本身写不进去（缺权限 / 缺列 / 撞约束），与策略无关。
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
	// 必须是策略拒的，不是随便一个错（权限 / 约束 / 语法）。
	if !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("期望 row-level security 违例，实际错误：%v", err)
	}
	// 越界行绝不能落库。
	if n := countOn(t, f.admin, "SELECT count(*) FROM product_brands WHERE slug = ?", "foreign-brand"); n != 0 {
		t.Fatalf("被拒的跨工程写入不应落库，实际 %d 行", n)
	}
	if n := countOn(t, f.admin, "SELECT count(*) FROM product_brands WHERE slug = ?", "own-brand"); n != 1 {
		t.Fatalf("本工程写入应落库，实际 %d 行", n)
	}
}

// ── (4) 新分区子表同样被覆盖 ────────────────────────────────────────────────

// TestNonSuperuser_NewPartitionChildCovered 分区子表也受策略约束。
//
// 为什么单独一条：**PG 的 ENABLE / FORCE 不递归到分区**（实测父表 relrowsecurity=t、
// 子表全为 f）。迁移 215 只能覆盖它执行那一刻已存在的分月表，之后每月新建的由
// internal/partition.EnsureAhead 建表后补策略 —— 补没补上，只能在这里验。
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
	// EnsureAhead 的返回值包含「本来就在」的分区（CREATE TABLE IF NOT EXISTS 也算执行过），
	// 所以要用建表前的清单筛出**真的新建**的那一个。
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

	// 结构事实：新子表必须 ENABLE + FORCE + 有引用 app.project_id 的策略。
	assertPartitionPolicy(t, f.admin, child)

	// 数据事实：把一行**落到该子表**（管理连接，超级用户不受策略约束）。
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

	// 直接按子表名查（排障 / 归档 / 报表那条路）也必须受策略约束。
	if n := countOn(t, f.app, "SELECT count(*) FROM "+child); n != 0 {
		t.Fatalf("非超级角色未设作用域时读子表 %s 应 0 行，实际 %d", child, n)
	}
	if n := countInScope(t, f.app, pA, "SELECT count(*) FROM "+child); n != 1 {
		t.Fatalf("作用域 A 内读子表 %s 应 1 行，实际 %d", child, n)
	}
	if n := countInScope(t, f.app, pB, "SELECT count(*) FROM "+child); n != 0 {
		t.Fatalf("作用域 B 内读子表 %s 应 0 行（他工程的行不可见），实际 %d", child, n)
	}
	// 走父表也一样（应用路径都走父表，分区裁剪不能把策略裁掉）。
	if n := countOn(t, f.app, "SELECT count(*) FROM page_views"); n != 0 {
		t.Fatalf("未设作用域时读父表应 0 行，实际 %d", n)
	}
	if n := countInScope(t, f.app, pA, "SELECT count(*) FROM page_views"); n != 1 {
		t.Fatalf("作用域 A 内读父表应 1 行，实际 %d", n)
	}
}

// ── (5) 全表覆盖：每张工程表都在名单里 ──────────────────────────────────────

// exemptTables 允许「有 project_id 但没装策略」的表，必须逐条写明理由。
//
// 只增不减：条目不再命中会失败（豁免过期等于没门禁），新增条目必须在这里说明为什么。
var exemptTables = map[string]string{
	// build_jobs 是构建任务队列：worker 按「待办状态」跨工程捞任务（不按工程过滤），
	// 装了策略后缺 app.project_id 的捞取语句会静默 0 行 —— 队列直接停摆。
	// 它不在迁移 215 的对象名单里，属**刻意排除**，不是漏装。
	"build_jobs": "构建任务队列按状态跨工程捞取，纳入 RLS 需先改造队列调度（本票不做）",
	// product_outbox_events 是商品失效事件队列（迁移 309，审计 ARCH-01）：
	// ClaimPendingOutbox 按「未处理 + 租约过期」跨工程捞取（见 product_outbox_model.go 的
	// SQL），查询里没有任何 project_id 谓词 —— 装上 FORCE 后缺 app.project_id 的领取会
	// 静默 0 行，商品变更的失效事件从此再不被消费（页面停在旧字节且无报错）。
	// 与 build_jobs 同一情形：队列语义，属**刻意排除**。
	"product_outbox_events": "商品失效事件队列按状态跨工程捞取，纳入 RLS 需先改造消费侧（本票不做）",
}

// TestNonSuperuser_EveryProjectTableIsCovered 每张带 project_id 的表都必须有生效的策略。
//
// 这条把 DB-04 的验收口径（「每张工程表纳入覆盖」）钉在 catalog 上：
// **新增一张带 project_id 的表却忘了装策略，这里立刻红**，而不是等它上线后
// 表现为「某个接口偶尔读到别人的数据」。
//
// 判据是四条一起看：ENABLE、FORCE（只 ENABLE 时表属主仍绕过，等于没做）、
// 至少一条策略、策略谓词确实读 app.project_id（谓词被写成 true 之类也照样红）。
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
	// 只取表 / 分区表：pg_attribute 里**索引列**的名字同样可能叫 project_id
	// （pg_class.relkind = 'i'），不限定 relkind 会把一堆索引算进来。
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
	// 豁免清单只增不减：条目不再命中说明它已经被纳入覆盖，该从清单里删掉。
	for name := range exemptTables {
		if !seen[name] {
			t.Errorf("豁免条目 %q 已不在带 project_id 的表里（或表已改名），请从 exemptTables 删除", name)
		}
	}
	// 关键代表必须真的在名单里 —— 否则上面那段循环可能只是空转。
	for _, name := range []string{"products", "product_brands", "product_categories", "product_tags",
		"inventory_stocks", "page_views", "pages", "blocks"} {
		if !seen[name] {
			t.Errorf("代表表 %s 未出现在盘点结果里（策略被整体删掉时会这样）", name)
		}
	}
}

// ── catalog / 时间辅助 ──────────────────────────────────────────────────────

// relNames 返回 current_schema 下匹配前缀的表名集合（用于区分「新建」与「本来就在」）。
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

// partitionMonth 从 <表名>_YYYY_MM 里取出 "YYYY_MM"。
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

// mustMonthTime 把 "YYYY_MM" 变成该月 15 号（必然落在这个月的分区区间内）。
func mustMonthTime(t *testing.T, month string) time.Time {
	t.Helper()
	m, err := time.Parse("2006_01", month)
	if err != nil {
		t.Fatalf("解析月份 %q 失败：%v", month, err)
	}
	return time.Date(m.Year(), m.Month(), 15, 12, 0, 0, 0, time.UTC)
}

// assertPartitionPolicy 新分区的结构断言（与上层 rls_scope_test 同口径，独立一份）。
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
