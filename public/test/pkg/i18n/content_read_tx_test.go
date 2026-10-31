package i18n_test

// content_read_tx_test.go — 事务内读译文 / 工程译文写入锁（导航译文改造 R1）在真实
// PostgreSQL 上的链路验证。
//
// 与同目录既有文件的边界：
//   - content_write_test.go 测旧写入口（Upsert）与旧读入口（LoadTargets / LoadDetails）；
//   - content_write_tx_test.go（T1）测 UpsertTx；
//   - content_project_scope_test.go 测工程覆盖 / 全局回落；
//   - 本文件只测 R1 新增的三个能力，并顺带回归两条旧读入口的行为不被改动波及：
//       1. LoadTargetsForProjectTx / LoadDetailsForProjectTx 在**未提交**的事务里看得到
//          本事务刚写的行，事务外的另一条连接看不到（「读现值必须进事务」D-9.0）；
//       2. 工程覆盖 + 回落全局与旧入口逐条一致，且按 context 稳定（同一 hash 多语境各取各的）；
//       3. 非事务句柄（nil / 连接池）→ ErrContentTxRequired，零 SQL（用 rtFailConnPool 观测）；
//          输入校验先于空输入：空 lang/hashes + 非事务句柄仍被拒；
//       4. 空 lang / 空 hashes → 空 map 且零 SQL；空 projectID 退化为全局形态（同旧 ForProject）；
//       5. LockProjectContentWrite 跨事务同工程阻塞直到提交/回滚；不同工程不互相阻塞；
//          uuid 大小写同锁；非事务句柄 → ErrContentTxRequired；空工程与非合法 uuid →
//          统一 ErrContentProjectRequired；取消 ctx 正常退出并释放等待。
//
// 并发握手不用 sleep 冒充「锁已等待」：
//   - A 取得锁后，用**同键** try-lock 当探针：返回 false 即「A 确实持锁」（可观测握手）；
//   - B 拿到锁后再做一次同键 try-lock：仍为 false，即「锁此刻在 B 手上」；
//   - 「确实在等待」以 pg_locks 里未授予的顾问锁为证据，不拿等待时间当证据；
//   - 取消用例：B 的 ctx 取消 → 返回 context.Canceled 且未授予锁消失；随后 A 回滚、C 取锁成功，
//     证明 B 没有把事务/锁留在半路。
//
// 表结构一律来自生产迁移（support.NewMigratedPGTestDB）；PG 不可用时 support 内部 Skip。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"go_wp/pkg/i18n"
	"go_wp/pkg/rls"

	"gorm.io/gorm"
)

const (
	// rtTestProbeTimeout 所有「等一个可观测状态」的上限：超时即失败（不无限等）。
	rtTestProbeTimeout = 30 * time.Second
	// rtTestProbeStep 探针轮询间隔（只用于 try-lock / Count 这类**可观测**状态，
	// 不是用等待时间冒充「锁已阻塞」；真正的阻塞由数据库的锁等待本身提供）。
	rtTestProbeStep = 10 * time.Millisecond
)

// rtSnapshot 测试收尾快照：任一 goroutine 仍在跑 / 有悬挂的锁等待 / 事务未结束，
// 都说明有泄漏（事务不在收尾前提交或回滚，就会一直挂着锁）。
type rtSnapshot struct {
	goroutines   int
	waitingLocks int64
	openTx       int64
}

// rtTakeSnapshot 读收尾快照（同一连接上顺序取三个计数）。
func rtTakeSnapshot(t *testing.T, db *gorm.DB) rtSnapshot {
	t.Helper()
	var snap rtSnapshot
	// 运行时协程数（不引入第三方泄漏检测依赖；判据见 rtAssertNoLeak 的余量说明）。
	snap.goroutines = runtime.NumGoroutine()
	if err := db.Raw("SELECT COUNT(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted").
		Scan(&snap.waitingLocks).Error; err != nil {
		t.Fatalf("读取等待中的顾问锁失败: %v", err)
	}
	if err := db.Raw("SELECT COUNT(*) FROM pg_stat_activity " +
		"WHERE datname = current_database() AND xact_start IS NOT NULL AND pid <> pg_backend_pid()").
		Scan(&snap.openTx).Error; err != nil {
		t.Fatalf("读取未结束的事务失败: %v", err)
	}
	return snap
}

// rtAssertNoLeak 断言收尾时没有新增未结束事务、没有等待中的顾问锁、goroutine 回到基线。
//
// 三者的容量余量：基线在用例开始前取（同一个测试内可能还有别的事务在跑，例如
// 上一段提交后的清理），因此只要求「不比基线多」。goroutine 的 2 个余量留给
// runtime 的调度/GC 辅助协程。
func rtAssertNoLeak(t *testing.T, db *gorm.DB, base rtSnapshot) {
	t.Helper()
	after := rtTakeSnapshot(t, db)
	if after.waitingLocks != 0 {
		t.Fatalf("收尾仍有 %d 个等待中的顾问锁（有 goroutine 卡在锁上）", after.waitingLocks)
	}
	if after.openTx > base.openTx {
		t.Fatalf("收尾仍有未结束事务：基线 %d，现在 %d", base.openTx, after.openTx)
	}
	if after.goroutines > base.goroutines+2 {
		t.Fatalf("goroutine 泄漏：基线 %d，现在 %d", base.goroutines, after.goroutines)
	}
}

// rtPGCount 在当前连接上执行一条计数 SQL（测试观测用，不是被测路径）。
func rtPGCount(t *testing.T, db *gorm.DB, sqlText string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(sqlText).Scan(&n).Error; err != nil {
		t.Fatalf("观测计数失败（%s）: %v", sqlText, err)
	}
	return n
}

// rtLockHeld 报告某个锁键此刻是否已被占用 —— 用**同键 try-lock** 当探针。
//
// 取到即立刻回滚：探针只回答「是否空闲」，不留下持锁事务。
func rtLockHeld(t *testing.T, db *gorm.DB, lockKey string) bool {
	held := false
	if err := db.Transaction(func(tx *gorm.DB) error {
		var got bool
		if err := tx.Raw("SELECT pg_try_advisory_xact_lock(hashtextextended($1, $2))", lockKey, 0).
			Scan(&got).Error; err != nil {
			return err
		}
		held = !got
		return nil
	}); err != nil {
		t.Fatalf("锁探针失败: %v", err)
	}
	return held
}

// rtWaitLockHeld 等到锁键被占用（或超时失败）。
func rtWaitLockHeld(t *testing.T, db *gorm.DB, lockKey string) {
	t.Helper()
	deadline := time.Now().Add(rtTestProbeTimeout)
	for !rtLockHeld(t, db, lockKey) {
		if time.Now().After(deadline) {
			t.Fatalf("等 %s 超时：锁始终未被占用", rtTestProbeTimeout)
		}
		time.Sleep(rtTestProbeStep)
	}
}

// rtWaitLockFree 等到锁键被释放（或超时失败）。
func rtWaitLockFree(t *testing.T, db *gorm.DB, lockKey string) {
	t.Helper()
	deadline := time.Now().Add(rtTestProbeTimeout)
	for rtLockHeld(t, db, lockKey) {
		if time.Now().After(deadline) {
			t.Fatalf("等 %s 超时：锁始终未被释放（事务没提交/回滚？）", rtTestProbeTimeout)
		}
		time.Sleep(rtTestProbeStep)
	}
}

// rtWaitAnyLockWaiting 等到本连接上出现「等待中的顾问锁」（即真有会话在锁上阻塞）。
func rtWaitAnyLockWaiting(t *testing.T, db *gorm.DB) {
	t.Helper()
	deadline := time.Now().Add(rtTestProbeTimeout)
	for rtPGCount(t, db, "SELECT COUNT(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted") == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("等 %s 超时：没有观察到等待中的顾问锁", rtTestProbeTimeout)
		}
		time.Sleep(rtTestProbeStep)
	}
}

// ── 事务内读 ───────────────────────────────────────────────────────────────

// TestContentReadTxSeesUncommittedWrites 事务内读看得到本事务未提交的写入，事务外看不到。
//
// 这是 R1 要的那个性质：锁内先用 LoadDetailsForProjectTx 取现值，再决定是否变更；
// 若走旧入口（自开事务、另取连接），读到的是锁外的旧快照。
func TestContentReadTxSeesUncommittedWrites(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()
	base := rtTakeSnapshot(t, db)

	project := txTestProjectA
	srcA := "提交订单"
	srcB := "查看物流"
	hashA := i18n.ContentHash(srcA)
	hashB := i18n.ContentHash(srcB)
	keyA := i18n.ContentIndexKey(hashA, txTestContext)
	keyB := i18n.ContentIndexKey(hashB, txTestContext)

	err := db.Transaction(func(tx *gorm.DB) error {
		// 本事务先取得本工程的写入锁（与生产调用顺序一致：锁 → 写 → 读回），
		// 同时把 scope 设上（写工程行需要，UpsertTx 的调用约定）。
		if lerr := i18n.LockProjectContentWrite(ctx, tx, project); lerr != nil {
			return lerr
		}
		if serr := rls.ScopeTx(tx, project); serr != nil {
			return serr
		}
		if _, werr := writer.UpsertTx(ctx, tx, []i18n.ContentWriteItem{
			txItem(project, srcA, "Place order"),
		}); werr != nil {
			return werr
		}

		// 未提交：同事务的读必须看得到。
		details, derr := writer.LoadDetailsForProjectTx(ctx, tx, project, txTestLang, []string{hashA})
		if derr != nil {
			return derr
		}
		if got := details[keyA].TargetText; got != "Place order" {
			t.Fatalf("事务内应看到本事务刚写的译文，实际 %q（map=%v）", got, details)
		}
		targets, terr := writer.LoadTargetsForProjectTx(ctx, tx, project, txTestLang, []string{hashA})
		if terr != nil {
			return terr
		}
		if got := targets[keyA]; got != "Place order" {
			t.Fatalf("事务内 LoadTargets 应看到未提交译文，实际 %q", got)
		}

		// 另一条连接（事务外）：看不到未提交的行（说明上面读到的确是「本事务」的视图，
		// 而不是事务外也能读到的已提交数据）。
		if n := txRowCount(t, db, project); n != 0 {
			t.Fatalf("事务外不应看到未提交行，实际 %d", n)
		}

		// 空 hashes 的候选（本事务还没写的那条）：只验证另一条 hash 不会串进来。
		targetsB, berr := writer.LoadTargetsForProjectTx(ctx, tx, project, txTestLang, []string{hashB})
		if berr != nil {
			return berr
		}
		if _, ok := targetsB[keyB]; ok {
			t.Fatalf("未写入的 hash 不应有译文，实际 %v", targetsB)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("事务执行失败: %v", err)
	}

	// 提交后：事务外读得到（旧入口也能读到，证明落库确实是这一笔）。
	if n := txRowCount(t, db, project); n != 1 {
		t.Fatalf("提交后应有 1 行，实际 %d", n)
	}
	targets, err := writer.LoadTargetsForProject(ctx, project, txTestLang, []string{hashA})
	if err != nil {
		t.Fatalf("提交后读回失败: %v", err)
	}
	if got := targets[keyA]; got != "Place order" {
		t.Fatalf("提交后应读到 %q，实际 %q", "Place order", got)
	}
	rtAssertNoLeak(t, db, base)
}

// TestContentReadTxProjectOverridesGlobalByContext 工程覆盖 + 回落全局，且按 context 稳定。
//
// 同一 source_hash 在不同 context 下的译法互不影响（DISTINCT ON 的键包含 context）；
// 工程 A 覆盖了一条，另一条回落全局；工程 B 两条都回落全局。
func TestContentReadTxProjectOverridesGlobalByContext(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()
	base := rtTakeSnapshot(t, db)

	projectA := txTestProjectA
	projectB := txTestProjectB
	src := "立即下单"
	hash := i18n.ContentHash(src)
	ctxMain := "core.button.text"
	ctxAside := "core.link.text"
	keyMain := i18n.ContentIndexKey(hash, ctxMain)
	keyAside := i18n.ContentIndexKey(hash, ctxAside)

	// 全局两条语境（无工程上下文：不需要 scope）。
	if _, err := writer.Upsert(ctx, []i18n.ContentWriteItem{
		{SourceHash: hash, Context: ctxMain, Lang: txTestLang, SourceText: src, TargetText: "Order now"},
		{SourceHash: hash, Context: ctxAside, Lang: txTestLang, SourceText: src, TargetText: "Order"},
	}); err != nil {
		t.Fatalf("写全局译文失败: %v", err)
	}
	// 工程 A 只覆盖 ctxMain。
	if err := db.Transaction(func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectA); serr != nil {
			return serr
		}
		_, werr := writer.UpsertTx(ctx, tx, []i18n.ContentWriteItem{{
			ProjectID: projectA, SourceHash: hash, Context: ctxMain, Lang: txTestLang,
			SourceText: src, TargetText: "Buy now",
		}})
		return werr
	}); err != nil {
		t.Fatalf("写工程 A 覆盖失败: %v", err)
	}

	check := func(project string, wantMain, wantAside string) {
		t.Helper()
		err := db.Transaction(func(tx *gorm.DB) error {
			if serr := rls.ScopeTx(tx, project); serr != nil {
				return serr
			}
			details, derr := writer.LoadDetailsForProjectTx(ctx, tx, project, txTestLang, []string{hash})
			if derr != nil {
				return derr
			}
			if got := details[keyMain].TargetText; got != wantMain {
				t.Fatalf("工程 %s 的 %s 应为 %q，实际 %q", project, ctxMain, wantMain, got)
			}
			if got := details[keyAside].TargetText; got != wantAside {
				t.Fatalf("工程 %s 的 %s 应为 %q，实际 %q", project, ctxAside, wantAside, got)
			}
			targets, terr := writer.LoadTargetsForProjectTx(ctx, tx, project, txTestLang, []string{hash})
			if terr != nil {
				return terr
			}
			if got := targets[keyMain]; got != wantMain {
				t.Fatalf("LoadTargets 工程 %s 的 %s 应为 %q，实际 %q", project, ctxMain, wantMain, got)
			}
			if got := targets[keyAside]; got != wantAside {
				t.Fatalf("LoadTargets 工程 %s 的 %s 应为 %q，实际 %q", project, ctxAside, wantAside, got)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("读取工程 %s 失败: %v", project, err)
		}
	}
	check(projectA, "Buy now", "Order")
	check(projectB, "Order now", "Order")

	// 旧入口回归：同一条 hash 上，旧 LoadDetailsForProject / LoadTargetsForProject 与事务版同解。
	oldDetails, err := writer.LoadDetailsForProject(ctx, projectA, txTestLang, []string{hash})
	if err != nil {
		t.Fatalf("旧 LoadDetailsForProject 失败: %v", err)
	}
	oldTargets, err := writer.LoadTargetsForProject(ctx, projectA, txTestLang, []string{hash})
	if err != nil {
		t.Fatalf("旧 LoadTargetsForProject 失败: %v", err)
	}
	if oldDetails[keyMain].TargetText != "Buy now" || oldTargets[keyMain] != "Buy now" {
		t.Fatalf("旧入口应看到工程覆盖（Buy now），实际 details=%q targets=%q",
			oldDetails[keyMain].TargetText, oldTargets[keyMain])
	}
	if oldDetails[keyAside].TargetText != "Order" || oldTargets[keyAside] != "Order" {
		t.Fatalf("旧入口应看到全局回落（Order），实际 details=%q targets=%q",
			oldDetails[keyAside].TargetText, oldTargets[keyAside])
	}

	// 空 projectID：与旧 ForProject("") 一致 —— 无工程上下文退化为全局形态（全局行优先）。
	err = db.Transaction(func(tx *gorm.DB) error {
		targets, terr := writer.LoadTargetsForProjectTx(ctx, tx, "", txTestLang, []string{hash})
		if terr != nil {
			return terr
		}
		if got := targets[keyMain]; got != "Order now" {
			t.Fatalf("空工程应走全局形态（Order now），实际 %q", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("空工程读取失败: %v", err)
	}
	rtAssertNoLeak(t, db, base)
}

// ── 入参判据与零 SQL ───────────────────────────────────────────────────────

// TestContentReadTxRejectsNonTxHandle 非事务句柄 → ErrContentTxRequired，且**零 SQL**。
//
// failConnPool 的任何 Exec/Query 都返回错误：若方法在判定前发了任何语句，
// 这里会拿到那个哨兵错误而不是 ErrContentTxRequired，用例即失败。
func TestContentReadTxRejectsNonTxHandle(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()
	hash := i18n.ContentHash("任意原文")

	cases := []struct {
		name string
		tx   *gorm.DB
	}{
		{name: "nil 句柄", tx: nil},
		{name: "连接池句柄", tx: &gorm.DB{Statement: &gorm.Statement{ConnPool: &rtFailConnPool{}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := writer.LoadTargetsForProjectTx(ctx, tc.tx, txTestProjectA, txTestLang, []string{hash}); !errors.Is(err, i18n.ErrContentTxRequired) {
				t.Fatalf("LoadTargetsForProjectTx 应返回 ErrContentTxRequired，实际 %v", err)
			}
			if _, err := writer.LoadDetailsForProjectTx(ctx, tc.tx, txTestProjectA, txTestLang, []string{hash}); !errors.Is(err, i18n.ErrContentTxRequired) {
				t.Fatalf("LoadDetailsForProjectTx 应返回 ErrContentTxRequired，实际 %v", err)
			}
			// 判据顺序：事务判别先于空输入 —— 空 lang/hashes + 非事务句柄同样被拒。
			if _, err := writer.LoadTargetsForProjectTx(ctx, tc.tx, txTestProjectA, "  ", nil); !errors.Is(err, i18n.ErrContentTxRequired) {
				t.Fatalf("空输入 + 非事务句柄应仍返回 ErrContentTxRequired，实际 %v", err)
			}
			if _, err := writer.LoadDetailsForProjectTx(ctx, tc.tx, txTestProjectA, txTestLang, nil); !errors.Is(err, i18n.ErrContentTxRequired) {
				t.Fatalf("空 hashes + 非事务句柄应仍返回 ErrContentTxRequired，实际 %v", err)
			}
			if err := i18n.LockProjectContentWrite(ctx, tc.tx, txTestProjectA); !errors.Is(err, i18n.ErrContentTxRequired) {
				t.Fatalf("LockProjectContentWrite 应返回 ErrContentTxRequired，实际 %v", err)
			}
			// 锁的事务判别同样先于工程入参校验：空工程 + 连接池 → 事务错误。
			if err := i18n.LockProjectContentWrite(ctx, tc.tx, " "); !errors.Is(err, i18n.ErrContentTxRequired) {
				t.Fatalf("空工程 + 非事务句柄应返回 ErrContentTxRequired，实际 %v", err)
			}
		})
	}

	// 空输入在**真事务**上返回空 map，零 SQL：failConnPool 不行（它不是事务），
	// 这里用「真事务 + 空输入」并把期望落在空 map 与不报错上；零 SQL 由
	// 下面 TestContentReadTxEmptyInputIssuesNoSQL 用查询计数证明。
	err := db.Transaction(func(tx *gorm.DB) error {
		targets, terr := writer.LoadTargetsForProjectTx(ctx, tx, txTestProjectA, "   ", []string{hash})
		if terr != nil {
			return terr
		}
		if len(targets) != 0 {
			t.Fatalf("空 lang 应返回空 map，实际 %v", targets)
		}
		details, derr := writer.LoadDetailsForProjectTx(ctx, tx, txTestProjectA, txTestLang, nil)
		if derr != nil {
			return derr
		}
		if len(details) != 0 {
			t.Fatalf("空 hashes 应返回空 map，实际 %v", details)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("真事务上的空输入应返回空 map: %v", err)
	}
}

// TestContentReadTxEmptyInputIssuesNoSQL 空输入 → 空 map 且**零 SQL**。
//
// pg_stat_activity.query 在事务内读到的是本会话**最近一条**语句：空输入之后只要方法没发
// 任何语句，它仍然是空输入之前那句标记 SELECT；被覆盖就说明方法在空输入路径上发了 SQL。
// （不走 failConnPool：它不是真事务，会在 requireTx 就被挡掉，测不到空输入分支。）
func TestContentReadTxEmptyInputIssuesNoSQL(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()
	const marker = "SELECT 'r1_no_sql_marker'"

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(marker).Error; err != nil {
			return err
		}
		if _, err := writer.LoadTargetsForProjectTx(ctx, tx, txTestProjectA, "", []string{"x"}); err != nil {
			return err
		}
		if _, err := writer.LoadDetailsForProjectTx(ctx, tx, txTestProjectA, txTestLang, []string{}); err != nil {
			return err
		}
		var current string
		if err := tx.Raw("SELECT current_query()").Scan(&current).Error; err != nil {
			// current_query() 不可用（被 PostgreSQL 移除的版本）时退化为只验证返回值。
			t.Logf("current_query() 不可用，本用例只验证返回值：%v", err)
			return nil
		}
		if !strings.Contains(current, "r1_no_sql_marker") {
			t.Fatalf("空输入后事务内最近语句被覆盖，说明方法发了 SQL：%q", current)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("空输入零 SQL 用例失败: %v", err)
	}
}

// ── 写入锁 ─────────────────────────────────────────────────────────────────

// TestLockProjectContentWriteBlocksSameProjectUntilCommit 同工程跨事务阻塞，
// 提交后放行；等待方拿到锁后反过来证明持有方的锁已释放（双向 try-lock 握手）。
func TestLockProjectContentWriteBlocksSameProjectUntilCommit(t *testing.T) {
	db := txTestDB(t)
	ctx := context.Background()
	base := rtTakeSnapshot(t, db)
	project := txTestProjectA
	key := "i18n.content:" + project

	// 持有方：Begin 后取锁，用通道请求「释放」，再提交。
	txA := db.Begin()
	if txA.Error != nil {
		t.Fatalf("开启事务 A 失败: %v", txA.Error)
	}
	defer func() {
		if txA != nil {
			_ = txA.Rollback().Error
		}
	}()
	if err := i18n.LockProjectContentWrite(ctx, txA, project); err != nil {
		t.Fatalf("事务 A 取锁失败: %v", err)
	}
	rtWaitLockHeld(t, db, key)

	release := make(chan string, 1)
	releaseResp := make(chan error, 1)
	acquired := make(chan error, 1) // B 取到锁后发 nil
	probed := make(chan bool, 1)    // B 拿到锁后对 A 的锁做的反向探针结果
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := db.Transaction(func(tx *gorm.DB) error {
			if lerr := i18n.LockProjectContentWrite(ctx, tx, project); lerr != nil {
				acquired <- lerr
				return lerr
			}
			acquired <- nil
			// B 已持锁：同键 try-lock 必须为 false（锁在 B 手上），以此观测「B 确实拿到了锁」。
			var got bool
			if qerr := tx.Raw("SELECT pg_try_advisory_xact_lock(hashtextextended($1, $2))", key, 0).
				Scan(&got).Error; qerr != nil {
				return qerr
			}
			probed <- !got
			// 收到主协程的释放请求后再结束事务：提交释放锁，回滚同样释放。
			if action := <-release; action != "commit" {
				return errR1Rollback
			}
			return nil
		})
		releaseResp <- err
	}()

	// 等待方确实处于「锁等待」：本连接上出现未授予的顾问锁（不是靠等待时间）。
	rtWaitAnyLockWaiting(t, db)
	select {
	case err := <-acquired:
		t.Fatalf("持有方还在事务中，等待方不应拿到锁（实际 %v）", err)
	case resp := <-releaseResp:
		t.Fatalf("等待方提前结束: %v", resp)
	default:
	}

	release <- "commit"
	if err := txA.Commit().Error; err != nil {
		t.Fatalf("提交事务 A 失败: %v", err)
	}
	txA = nil

	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("提交后等待方应拿到锁，实际 %v", err)
		}
	case <-time.After(rtTestProbeTimeout):
		t.Fatalf("等 %s 超时：提交后等待方仍拿不到锁", rtTestProbeTimeout)
	}
	if selfHeld := <-probed; selfHeld {
		t.Fatal("B 已持锁时同键 try-lock 应为 false（DB 层可重入），说明锁键或探针不符")
	}
	if err := <-releaseResp; err != nil {
		t.Fatalf("等待方事务应成功结束，实际 %v", err)
	}
	wg.Wait()
	rtWaitLockFree(t, db, key)
	rtAssertNoLeak(t, db, base)
}

// errR1Rollback 等待方用「业务错误回滚」结束事务（锁随回滚释放）。
var errR1Rollback = errors.New("r1: 用例要求回滚")

// TestLockProjectContentWriteReleasedByRollback 持有方回滚 → 等待方放行。
func TestLockProjectContentWriteReleasedByRollback(t *testing.T) {
	db := txTestDB(t)
	ctx := context.Background()
	base := rtTakeSnapshot(t, db)
	project := txTestProjectB
	key := "i18n.content:" + project

	txA := db.Begin()
	if txA.Error != nil {
		t.Fatalf("开启事务 A 失败: %v", txA.Error)
	}
	if err := i18n.LockProjectContentWrite(ctx, txA, project); err != nil {
		_ = txA.Rollback().Error
		t.Fatalf("事务 A 取锁失败: %v", err)
	}
	rtWaitLockHeld(t, db, key)

	acquired := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		done <- db.Transaction(func(tx *gorm.DB) error {
			if lerr := i18n.LockProjectContentWrite(ctx, tx, project); lerr != nil {
				return lerr
			}
			acquired <- nil
			return nil
		})
	}()

	rtWaitAnyLockWaiting(t, db)
	select {
	case <-acquired:
		t.Fatal("持有方还在事务中，等待方不应拿到锁")
	default:
	}
	if err := txA.Rollback().Error; err != nil {
		t.Fatalf("回滚事务 A 失败: %v", err)
	}
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("回滚后等待方应拿到锁，实际 %v", err)
		}
	case <-time.After(rtTestProbeTimeout):
		t.Fatalf("等 %s 超时：回滚后等待方仍拿不到锁", rtTestProbeTimeout)
	}
	if err := <-done; err != nil {
		t.Fatalf("等待方事务失败: %v", err)
	}
	rtWaitLockFree(t, db, key)
	rtAssertNoLeak(t, db, base)
}

// TestLockProjectContentWriteDifferentProjectsDoNotBlock 不同工程互不阻塞。
//
// 手上持有 A 的锁时，B 必须在**没有等待**的情况下立刻取到 B 的锁（try 语义可观测：
// 取锁期间 pg_locks 里不应出现未授予的顾问锁）。
func TestLockProjectContentWriteDifferentProjectsDoNotBlock(t *testing.T) {
	db := txTestDB(t)
	ctx := context.Background()
	base := rtTakeSnapshot(t, db)

	txA := db.Begin()
	if txA.Error != nil {
		t.Fatalf("开启事务 A 失败: %v", txA.Error)
	}
	defer func() { _ = txA.Rollback().Error }()
	if err := i18n.LockProjectContentWrite(ctx, txA, txTestProjectA); err != nil {
		t.Fatalf("事务 A 取锁失败: %v", err)
	}
	rtWaitLockHeld(t, db, "i18n.content:"+txTestProjectA)

	var bErr error // 取锁结果：在测试协程里取回、在主协程断言（t.Fatalf 不能跨协程）
	acquired := make(chan error, 1)
	go func() {
		acquired <- db.Transaction(func(tx *gorm.DB) error {
			return i18n.LockProjectContentWrite(ctx, tx, txTestProjectB)
		})
	}()
	select {
	case bErr = <-acquired:
	case <-time.After(rtTestProbeTimeout):
		t.Fatalf("等 %s 超时：不同工程被互相阻塞", rtTestProbeTimeout)
	}
	if bErr != nil {
		t.Fatalf("不同工程应立刻取到锁，实际 %v", bErr)
	}
	if n := rtPGCount(t, db, "SELECT COUNT(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted"); n != 0 {
		t.Fatalf("不同工程取锁期间不应有等待中的顾问锁，实际 %d", n)
	}
	_ = txA.Rollback().Error
	rtAssertNoLeak(t, db, base)
}

// TestLockProjectContentWriteUppercaseProjectSameLock uuid 大小写不同 → 同一把锁。
//
// 持有方用大写形式取锁；等待方用小写形式取同一工程，必须被阻塞 —— 若键按原始字符串
// 拼，两边会算出两把不同的锁，等待方会立刻放行（本用例即失败）。
func TestLockProjectContentWriteUppercaseProjectSameLock(t *testing.T) {
	db := txTestDB(t)
	ctx := context.Background()
	base := rtTakeSnapshot(t, db)

	upper := strings.ToUpper(txTestProjectA)
	if upper == txTestProjectA {
		t.Fatalf("测试常量应含可大写的字母：%q", txTestProjectA)
	}
	lowerKey := "i18n.content:" + strings.ToLower(txTestProjectA)

	txA := db.Begin()
	if txA.Error != nil {
		t.Fatalf("开启事务 A 失败: %v", txA.Error)
	}
	defer func() { _ = txA.Rollback().Error }()
	if err := i18n.LockProjectContentWrite(ctx, txA, upper); err != nil {
		t.Fatalf("大写形式取锁失败: %v", err)
	}
	// 大小写规范化后应落在**同一个小写键**上（探针按小写键能观测到占用）。
	rtWaitLockHeld(t, db, lowerKey)

	acquired := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		done <- db.Transaction(func(tx *gorm.DB) error {
			if lerr := i18n.LockProjectContentWrite(ctx, tx, strings.ToLower(txTestProjectA)); lerr != nil {
				return lerr
			}
			acquired <- nil
			return nil
		})
	}()
	rtWaitAnyLockWaiting(t, db)
	select {
	case <-acquired:
		t.Fatal("大小写不同但同一工程：等待方不应拿到锁（锁键未规范化）")
	default:
	}
	if err := txA.Rollback().Error; err != nil {
		t.Fatalf("回滚事务 A 失败: %v", err)
	}
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatalf("回滚后等待方应拿到锁，实际 %v", err)
		}
	case <-time.After(rtTestProbeTimeout):
		t.Fatalf("等 %s 超时：回滚后等待方仍拿不到锁", rtTestProbeTimeout)
	}
	if err := <-done; err != nil {
		t.Fatalf("等待方事务失败: %v", err)
	}
	rtWaitLockFree(t, db, lowerKey)
	rtAssertNoLeak(t, db, base)
}

// TestLockProjectContentWriteProjectParamValidation 工程入参非法（空白 / 非合法 uuid）统一报
// ErrContentProjectRequired；带首尾空白的合法 uuid 去空白后正常取锁。
func TestLockProjectContentWriteProjectParamValidation(t *testing.T) {
	db := txTestDB(t)
	ctx := t.Context()

	var err error
	defer recoverErrAssert(&err)
	err = db.Transaction(func(tx *gorm.DB) error {
		if lerr := i18n.LockProjectContentWrite(ctx, tx, "   "); !errors.Is(lerr, i18n.ErrContentProjectRequired) {
			errAssert("空工程应返回 ErrContentProjectRequired，实际 %v", lerr)
		}
		for _, bad := range []string{"not-a-uuid", "1234", txTestProjectA + "0"} {
			if lerr := i18n.LockProjectContentWrite(ctx, tx, bad); !errors.Is(lerr, i18n.ErrContentProjectRequired) {
				errAssert("非法 uuid %q 应返回 ErrContentProjectRequired，实际 %v", bad, lerr)
			}
		}
		// 合法（且带首尾空白）的工程 id 可通过：与 rls 的 Trim 口径一致。
		if lerr := i18n.LockProjectContentWrite(ctx, tx, "  "+txTestProjectA+"  "); lerr != nil {
			errAssert("合法工程（带空白）应取锁成功，实际 %v", lerr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("工程入参校验用例失败: %v", err)
	}
}

// TestLockProjectContentWriteCanceledContextReleasesWait 取消 ctx → 等待方正常退出并释放等待。
//
// 三步：A 持锁 → B 同工程取锁并在 ctx 取消后返回错误 → 取消后 C 能拿到锁
// （证明 B 没有把事务/锁留在半路），全程无 goroutine 泄漏。
func TestLockProjectContentWriteCanceledContextReleasesWait(t *testing.T) {
	db := txTestDB(t)
	base := rtTakeSnapshot(t, db)
	project := txTestProjectA
	key := "i18n.content:" + project

	txA := db.Begin()
	if txA.Error != nil {
		t.Fatalf("开启事务 A 失败: %v", txA.Error)
	}
	defer func() { _ = txA.Rollback().Error }()
	if err := i18n.LockProjectContentWrite(context.Background(), txA, project); err != nil {
		t.Fatalf("事务 A 取锁失败: %v", err)
	}
	rtWaitLockHeld(t, db, key)

	waitCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waitErr := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		err := db.WithContext(waitCtx).Transaction(func(tx *gorm.DB) error {
			return i18n.LockProjectContentWrite(waitCtx, tx, project)
		})
		waitErr <- err
		done <- nil
	}()

	rtWaitAnyLockWaiting(t, db)
	cancel()

	select {
	case err := <-waitErr:
		if err == nil {
			t.Fatal("ctx 取消后等待方应返回错误")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("等待方应返回 context.Canceled，实际 %v", err)
		}
	case <-time.After(rtTestProbeTimeout):
		t.Fatalf("等 %s 超时：ctx 取消后等待方没有退出", rtTestProbeTimeout)
	}
	<-done

	// 取消后：A 的锁仍在（A 还开着事务），但**没有**任何未授予的锁等待残留。
	deadline := time.Now().Add(rtTestProbeTimeout)
	for rtPGCount(t, db, "SELECT COUNT(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted") != 0 {
		if time.Now().After(deadline) {
			t.Fatal("ctx 取消后仍有等待中的顾问锁（等待没有被释放）")
		}
		time.Sleep(rtTestProbeStep)
	}
	// C 在 A 回滚后立刻能拿到锁（B 没有把锁/事务留成半路状态）。
	if err := txA.Rollback().Error; err != nil {
		t.Fatalf("回滚事务 A 失败: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return i18n.LockProjectContentWrite(context.Background(), tx, project)
	}); err != nil {
		t.Fatalf("取消后续取锁失败（说明等待方留下了残留锁）: %v", err)
	}
	rtWaitLockFree(t, db, key)
	rtAssertNoLeak(t, db, base)
}

// TestLockProjectContentWriteReentrantForWriteWithinTx 同一事务内取锁后可正常写入并读回。
//
// 顾问锁是 xact 级、按（会话, 键）可重入：同一事务里先取锁、再设 scope、再写、
// 再读回，不应自锁（写路径本身也要拿表级锁，若自锁这里会永远不返回）。
func TestLockProjectContentWriteReentrantForWriteWithinTx(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()
	base := rtTakeSnapshot(t, db)

	project := txTestProjectA
	src := "提交成功"
	hash := i18n.ContentHash(src)
	key := i18n.ContentIndexKey(hash, txTestContext)

	err := db.Transaction(func(tx *gorm.DB) error {
		if lerr := i18n.LockProjectContentWrite(ctx, tx, project); lerr != nil {
			return lerr
		}
		// 同一事务内重复取锁（可重入）。
		if lerr := i18n.LockProjectContentWrite(ctx, tx, project); lerr != nil {
			return lerr
		}
		if serr := rls.ScopeTx(tx, project); serr != nil {
			return serr
		}
		if _, werr := writer.UpsertTx(ctx, tx, []i18n.ContentWriteItem{
			txItem(project, src, "Submitted"),
		}); werr != nil {
			return werr
		}
		details, derr := writer.LoadDetailsForProjectTx(ctx, tx, project, txTestLang, []string{hash})
		if derr != nil {
			return derr
		}
		if got := details[key].TargetText; got != "Submitted" {
			t.Fatalf("锁内读回应为 %q，实际 %q", "Submitted", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("锁内写读失败: %v", err)
	}
	if n := txRowCount(t, db, project); n != 1 {
		t.Fatalf("提交后应有 1 行，实际 %d", n)
	}
	rtWaitLockFree(t, db, "i18n.content:"+project)
	rtAssertNoLeak(t, db, base)
}

// ── 本地辅助 ───────────────────────────────────────────────────────────────

// errAssert 断言失败的即时回报（在 **事务回调** 里代替 t.Fatalf 使用）。
//
// 事务回调里不能调 t.Fatalf：非测试协程上的 Fatal 只会 Goexit 掉那个协程、
// 而外层 test 协程可能正阻塞在数据库调用上，测试会挂到超时。这里 panic 一个固定值，
// 由 recoverErrAssert 在同一个协程里换回 error 交给事务回滚，再由主协程断言。
func errAssert(format string, args ...any) { panic(rtErrAssert{msg: fmt.Sprintf(format, args...)}) }

// rtErrAssert 事务回调里的断言失败（带格式化后的说明）。
type rtErrAssert struct{ msg string }

// Error 实现 error 接口（便于日志与 unwrap 输出）。
func (e rtErrAssert) Error() string { return e.msg }

// recoverErrAssert 把 errAssert 的 panic 转回 error；其它 panic 原样抛出。
func recoverErrAssert(err *error) {
	if r := recover(); r != nil {
		if aerr, ok := r.(rtErrAssert); ok {
			*err = aerr
			return
		}
		panic(r)
	}
}

// rtFailConnPool 任何写/查都返回错误的连接池替身：
// 用来证明「判定非事务句柄时零 SQL」（若发出语句，错误会变成 rtErrFailNoSQL）。
type rtFailConnPool struct{}

var rtErrFailNoSQL = errors.New("r1: 非事务句柄上不应发出任何 SQL")

// ExecContext 记录「方法在本该零 SQL 的路径上发了语句」。
func (rtFailConnPool) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, rtErrFailNoSQL
}

// PrepareContext 同上。
func (rtFailConnPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, rtErrFailNoSQL
}

// QueryContext 同上（返回 *sql.Rows，与 gorm.ConnPool 签名一致）。
func (rtFailConnPool) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, rtErrFailNoSQL
}

// QueryRowContext 同上。
func (rtFailConnPool) QueryRowContext(context.Context, string, ...any) *sql.Row { return nil }
