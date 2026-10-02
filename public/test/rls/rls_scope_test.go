package rlstest

// rls_scope_test.go — 工程隔离（RLS，审计 DB-009）的读写作用域护栏。
//
// 这个文件要钉住的**不是**「迁移跑过了」，而是 DB-009 两步走里那个不能反的顺序：
//
//	① 所有读写路径都包上 pkg/rls 的工程作用域；
//	② 之后才能把连接角色换成非超级角色。
//
// 反过来的表现是「功能突然查不到数据」且**没有任何错误日志**（fail closed 不报错），
// 所以这里每一条断言都必须具备失败能力 —— 而不是断言「调用没报错」：
//
//   - 未设 scope 时读到 0 行：有人把策略删了/放宽了，这里立刻红；
//   - 设了 scope 能读到本工程行：有人把 model 里的 InProjectScope 拿掉，
//     RLS 生效下写入会被 WITH CHECK 拒绝、读取会返回 0 行，同样立刻红；
//   - 跨工程读不到：隔离被破坏（谓词写反、scope 没生效）时红；
//   - 写入他工程的行被拒：WITH CHECK 缺位时红；
//   - 事务结束后变量还原：把 is_local 改成 false（会话级）时会红 ——
//     那是最坏的一种错：下一个拿到同连接的请求会**继承上一个工程的 id**。
//
// 全程在**非超级角色**下跑：PostgreSQL 的超级用户无条件绕过 RLS（FORCE 只约束表属主），
// 用 root 跑这套断言会全绿而没有一行是真的。rls.BypassedRole 也在最后被断言为 false，
// 防止将来有人把 SET ROLE 那段删掉却让测试继续「通过」。

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
	"go_wp/internal/partition"
	"go_wp/pkg/rls"
	"go_wp/public/migrations"
)

// pgEnv 读标准 libpq 环境变量（默认对齐 config.yaml 与 public/test/support）。
//
// 刻意不 import public/test/support：那个包为了提供引擎级测试会 import routers，
// 把全部模块的 inbound/http 拉进本测试包的依赖链 —— 任何一个模块正在被改动，
// 这个本只关心数据库行为的用例都会跟着「build failed」。这里要的只是「一个干净的
// 库 + 生产迁移」，自己连三行就够。
func pgEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// pgDSN 拼装 libpq DSN。
func pgDSN(dbname string) string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai",
		pgEnv("PGHOST", "127.0.0.1"), pgEnv("PGUSER", "root"), pgEnv("PGPASSWORD", "root"),
		dbname, pgEnv("PGPORT", "5432"))
}

// seedProject 插入一条真实工程行（projects 是多数业务表的外键目标）。
func seedProject(t *testing.T, db *gorm.DB, id, name string) {
	t.Helper()
	if err := db.Exec(
		"INSERT INTO projects (id, name, settings, create_time, update_time) VALUES (?, ?, '{}'::jsonb, NOW(), NOW())",
		id, name,
	).Error; err != nil {
		t.Fatalf("准备工程行失败：%v", err)
	}
}

// rlsFixture 准备一套「生产结构 + 非超级角色」的环境。
//
// 表结构来自生产迁移（含 215 铺下 RLS 的那批对象）而不是手抄 DDL：手抄的会在下一次迁移
// 推进时与生产静默分叉，这正是 AGENTS.md 反复拦过的事。
func rlsFixture(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	admin, err := gorm.Open(postgres.Open(pgDSN(pgEnv("PGDATABASE", "wp_test"))), &gorm.Config{})
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil, ""
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Skipf("取管理连接失败：%v", err)
		return nil, ""
	}
	if err := adminSQL.Ping(); err != nil {
		adminSQL.Close()
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil, ""
	}

	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		t.Fatalf("生成随机名字失败: %v", err)
	}
	suffix := hex.EncodeToString(buf[:])
	// 角色是**集群级**对象（不像库/schema 那样随 cleanup 消失），随机名 + 显式清理。
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
	// SET ROLE 是**会话级**的：连接池里只要还有第二条连接，后续语句就可能落在没切角色的
	// 连接上，测试于是时而「通过」时而失败。锁成单连接是这套断言成立的前提。
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = db.Exec("RESET ROLE").Error
		sqlDB.Close()
		_ = admin.Exec("DROP DATABASE IF EXISTS " + dbName).Error
		_ = admin.Exec("DROP ROLE IF EXISTS " + role).Error
		adminSQL.Close()
	})

	// 表结构来自**生产迁移**（含 215 铺下 RLS 的那批对象），而不是手抄 DDL：手抄的会在下一次
	// 迁移推进时与生产静默分叉，这正是 AGENTS.md 反复拦过的事。
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移失败: %v", err)
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

	// 这套断言只有在本轮真的跑在非超级角色下才有意义。
	bypass, err := rls.BypassedRole(context.Background(), db)
	if err != nil {
		t.Fatalf("查询角色属性失败: %v", err)
	}
	if bypass {
		t.Fatalf("测试跑在会绕过 RLS 的角色上（superuser / BYPASSRLS），断言将全部失真")
	}
	return db, role
}

// seedBlock 经 model 的写入路径（带工程作用域）落一行块。
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

// TestRLS_FailClosed_WithoutScope 未设 scope 时一行都读不到。
//
// 这条是「先接线后切角色」顺序的护栏：换角色之后任何漏包 scope 的路径都退化成
// 静默返回 0 行，功能表现为「突然查不到数据」而不报错 —— 这里把那个退化本身钉住。
func TestRLS_FailClosed_WithoutScope(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()

	pA := uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedBlock(t, db, pA, "本工程的块")

	// 1) 绕开 model 的作用域包装裸查（等价于「没改造的路径」）：策略谓词为 NULL ⇒ 0 行。
	var raw int64
	if err := db.Table("blocks").Where("project_id = ?", pA).Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("未设 app.project_id 时应 0 行可见（fail closed），实际 %d 行", raw)
	}

	// 2) 经 model 的**裸句柄**读取（等价于「还没接 scope 的路径」）：同一份数据、同一个
	//    WHERE 条件，裸句柄读不到而 ListByProject 读得到（下一条用例）—— 可见性由会话变量
	//    决定，不是由 WHERE 决定。这就是「先接线后切角色」的全部要害。
	var viaBare int64
	if err := blockmodel.NewBlockModel(db).DB(ctx).Where("project_id = ?", pA).Count(&viaBare).Error; err != nil {
		t.Fatalf("裸句柄查询失败: %v", err)
	}
	if viaBare != 0 {
		t.Fatalf("未经作用域的读取应返回 0 行，实际 %d 行", viaBare)
	}
}

// TestRLS_ScopeReadsOwnProjectOnly 设了 scope 只读到本工程的行。
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

	// 按 id 单查（最容易跨工程泄漏的一类）也必须读不到他工程的行。
	if _, err := m.GetByID(ctx, listB[0].ID, pA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("拿工程 A 的作用域按 id 读工程 B 的块，应 ErrRecordNotFound，实际 %v", err)
	}
	if _, err := m.GetByID(ctx, listA[0].ID, pA); err != nil {
		t.Fatalf("按 id 读本工程的行应成功，实际 %v", err)
	}
}

// TestRLS_WriteCheckRejectsForeignRow 写入他工程的行被 WITH CHECK 拒绝。
func TestRLS_WriteCheckRejectsForeignRow(t *testing.T) {
	db, _ := rlsFixture(t)

	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")

	now := time.Now()
	err := rls.InProjectScope(context.Background(), db, pA, func(tx *gorm.DB) error {
		// 刻意**不经 model 的 scope 包装**：这里要验证的是数据库层的 WITH CHECK 本身。
		// 经包装的话，作用域会被改写成行自己声明的工程，测到的是嵌套覆盖而不是拒绝。
		return tx.Model(&blockmodel.BlockEntity{}).Create(&blockmodel.BlockEntity{
			ID:         uuid.NewString(),
			ProjectID:  pB, // 作用域是 A，行却声明 B
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

// TestRLS_ScopeIsTransactionLocal 事务结束后会话变量还原，不泄漏给下一次查询。
//
// 会话级（is_local => false）设置在这里是错的，且**方向是危险的那一侧**：
// 连接归还池中后变量仍在，下一个拿到该连接的请求若不设变量就会继承上一个工程的 id
// （fail open 到别人的工程），比不做隔离更糟。
func TestRLS_ScopeIsTransactionLocal(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()

	pA := uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedBlock(t, db, pA, "A 的块")

	// 事务内可见。
	if _, err := blockmodel.NewBlockModel(db).ListByProject(ctx, pA, "", "", ""); err != nil {
		t.Fatalf("事务内读取失败: %v", err)
	}
	// 事务结束后（同一条连接，池里只有这一条）变量必须已还原。
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
	// 再包一次 scope 仍然能读到：证明变量可重设，不是被上一次用坏了。
	if list, err := blockmodel.NewBlockModel(db).ListByProject(ctx, pA, "", "", ""); err != nil || len(list) != 1 {
		t.Fatalf("重新设 scope 后应读到 1 行，实际 %d 行（err=%v）", len(list), err)
	}
}

// TestRLS_ScopeTxRejectsNonTransactionHandle 在非事务句柄上设作用域必须显式报错。
//
// 静默放过的代价：set_config(is_local) 在 autocommit 下设完即失效，调用方以为包了
// 作用域，策略谓词读到的仍是 NULL —— 又是一次「查不到数据」的静默退化。
func TestRLS_ScopeTxRejectsNonTransactionHandle(t *testing.T) {
	db, _ := rlsFixture(t)
	if err := rls.ScopeTx(db, uuid.NewString()); !errors.Is(err, rls.ErrNotInTransaction) {
		t.Fatalf("非事务句柄应返回 ErrNotInTransaction，实际 %v", err)
	}
	// 非法 uuid 在进入 SQL 之前就被拒（否则 PG 的 ::uuid 强转错误会被误读成数据库故障）。
	if err := rls.InProjectScope(context.Background(), db, "not-a-uuid", func(*gorm.DB) error {
		t.Fatal("非法 uuid 不应执行事务体")
		return nil
	}); !errors.Is(err, rls.ErrInvalidProjectID) {
		t.Fatalf("非法 uuid 应返回 ErrInvalidProjectID，实际 %v", err)
	}
}

// TestRLS_MigrationCoversScopedTables 迁移 215 的覆盖：基表与分区子表都要有 ENABLE + FORCE。
//
// 分区子表单独断言，因为 **PG 的 ENABLE / FORCE 不递归到分区**（实测父表 relrowsecurity=t、
// 子表全为 f）：漏了子表，应用走父表时看不出来，但「排障时手写分区名」那条路完全没有隔离。
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

// TestRLS_EnsureAheadAddsPolicyToNewPartitions 新分区由 EnsureAhead 补策略。
//
// 迁移 215 只能覆盖它执行那一刻已存在的分区；之后每月新建的分区如果没人补，
// 隔离会随时间的推移**悄悄变薄**（父表看起来始终是 t）。
func TestRLS_EnsureAheadAddsPolicyToNewPartitions(t *testing.T) {
	db, _ := rlsFixture(t)

	// 建分区是属主/运维动作（启动期 + 每日任务），不是应用连接的日常写路径：
	// 这里用 RESET ROLE 回到属主身份执行。本用例断言的是「新分区有没有被补上策略」
	// 这一**结构事实**（pg_class / pg_policy 不受 RLS 影响），与当前角色无关。
	if err := db.Exec("RESET ROLE").Error; err != nil {
		t.Fatalf("恢复属主身份失败: %v", err)
	}

	// 提前 6 个月必然超出迁移建好的窗口（迁移建到 2026_12），确保真的新建了分区。
	created, err := partition.EnsureAhead(context.Background(), db, 6)
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

// TestRLS_NestedScopeOverridesOuterScope 记录并钉住「事务内重复设置作用域」的语义。
//
// set_config 是赋值而不是断言：在同一个事务里再设一次会**覆盖**前值。这不是 bug，但它
// 划出了各 model 的 *Tx 变体（CreateTx / EnsureStocksTx / AppendTx …）的纪律 ——
//
//	**只能传这笔业务数据自身的 project_id**。同一事务内它通常与外层一致（幂等，无害）；
//	一旦传了别的工程，事务中途就换了作用域，接下来所有语句的可见性跟着一起变，
//	而外层事务的原子性看起来完好无损。
//
// 之所以不在这里加硬错误：合法的嵌套（外层已设、内层补一次同值）太常见，且当前所有
// *Tx 调用点都来自同一工程。这条语义因此用测试固定下来，改 rls 时不会无意中翻转它。
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
	// 事务结束后仍然还原（覆盖不改变 is_local 的边界）。
	var after string
	if err := db.Raw("SELECT COALESCE(current_setting('app.project_id', true), '')").Scan(&after).Error; err != nil {
		t.Fatalf("读取会话变量失败: %v", err)
	}
	if after != "" {
		t.Fatalf("事务结束后应还原为空，实际 %q", after)
	}
}
