package i18n_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go_wp/pkg/i18n"
	"go_wp/pkg/rls"

	"gorm.io/gorm"
)

const (
	rtTestProbeTimeout = 30 * time.Second

	rtTestProbeStep = 10 * time.Millisecond
)

type rtSnapshot struct {
	goroutines   int
	waitingLocks int64
	openTx       int64
}

func rtTakeSnapshot(t *testing.T, db *gorm.DB) rtSnapshot {
	t.Helper()
	var snap rtSnapshot

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

func rtPGCount(t *testing.T, db *gorm.DB, sqlText string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(sqlText).Scan(&n).Error; err != nil {
		t.Fatalf("观测计数失败（%s）: %v", sqlText, err)
	}
	return n
}

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

		if n := txRowCount(t, db, project); n != 0 {
			t.Fatalf("事务外不应看到未提交行，实际 %d", n)
		}

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

	if _, err := writer.Upsert(ctx, []i18n.ContentWriteItem{
		{SourceHash: hash, Context: ctxMain, Lang: txTestLang, SourceText: src, TargetText: "Order now"},
		{SourceHash: hash, Context: ctxAside, Lang: txTestLang, SourceText: src, TargetText: "Order"},
	}); err != nil {
		t.Fatalf("写全局译文失败: %v", err)
	}

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

			if _, err := writer.LoadTargetsForProjectTx(ctx, tc.tx, txTestProjectA, "  ", nil); !errors.Is(err, i18n.ErrContentTxRequired) {
				t.Fatalf("空输入 + 非事务句柄应仍返回 ErrContentTxRequired，实际 %v", err)
			}
			if _, err := writer.LoadDetailsForProjectTx(ctx, tc.tx, txTestProjectA, txTestLang, nil); !errors.Is(err, i18n.ErrContentTxRequired) {
				t.Fatalf("空 hashes + 非事务句柄应仍返回 ErrContentTxRequired，实际 %v", err)
			}
			if err := i18n.LockProjectContentWrite(ctx, tc.tx, txTestProjectA); !errors.Is(err, i18n.ErrContentTxRequired) {
				t.Fatalf("LockProjectContentWrite 应返回 ErrContentTxRequired，实际 %v", err)
			}

			if err := i18n.LockProjectContentWrite(ctx, tc.tx, " "); !errors.Is(err, i18n.ErrContentTxRequired) {
				t.Fatalf("空工程 + 非事务句柄应返回 ErrContentTxRequired，实际 %v", err)
			}
		})
	}

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

func TestContentReadTxEmptyInputIssuesNoSQL(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()
	const marker = "SELECT 'r1_no_sql_marker'"

	var sqls atomic.Int64
	count := func(*gorm.DB) { sqls.Add(1) }

	db.Callback().Query().After("gorm:query").Register("i18n_test:count_sql", count)
	db.Callback().Row().After("gorm:row").Register("i18n_test:count_sql", count)
	db.Callback().Raw().After("gorm:raw").Register("i18n_test:count_sql", count)
	db.Callback().Create().After("gorm:create").Register("i18n_test:count_sql", count)
	db.Callback().Update().After("gorm:update").Register("i18n_test:count_sql", count)
	t.Cleanup(func() {
		db.Callback().Query().Remove("i18n_test:count_sql")
		db.Callback().Row().Remove("i18n_test:count_sql")
		db.Callback().Raw().Remove("i18n_test:count_sql")
		db.Callback().Create().Remove("i18n_test:count_sql")
		db.Callback().Update().Remove("i18n_test:count_sql")
	})

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(marker).Error; err != nil {
			return err
		}
		sqls.Store(0)

		if got, err := writer.LoadTargetsForProjectTx(ctx, tx, txTestProjectA, "", []string{"x"}); err != nil {
			return err
		} else if len(got) != 0 {
			return errors.New("空输入应返回空结果")
		}
		if details, err := writer.LoadDetailsForProjectTx(ctx, tx, txTestProjectA, txTestLang, []string{}); err != nil {
			return err
		} else if len(details) != 0 {
			return errors.New("空 hashes 应返回空 map")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("空输入零 SQL 用例失败: %v", err)
	}
	if n := sqls.Load(); n != 0 {
		t.Fatalf("空输入路径不得发 SQL，实际发了 %d 条", n)
	}
}

func TestLockProjectContentWriteBlocksSameProjectUntilCommit(t *testing.T) {
	db := txTestDB(t)
	ctx := context.Background()
	base := rtTakeSnapshot(t, db)
	project := txTestProjectA
	key := "i18n.content:" + project

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
	acquired := make(chan error, 1)
	probed := make(chan bool, 1)
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

			var got bool
			if qerr := tx.Raw("SELECT pg_try_advisory_xact_lock(hashtextextended($1, $2))", key, 0).
				Scan(&got).Error; qerr != nil {
				return qerr
			}
			probed <- !got

			if action := <-release; action != "commit" {
				return errR1Rollback
			}
			return nil
		})
		releaseResp <- err
	}()

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

var errR1Rollback = errors.New("r1: 用例要求回滚")

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

	var bErr error
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

		if lerr := i18n.LockProjectContentWrite(ctx, tx, "  "+txTestProjectA+"  "); lerr != nil {
			errAssert("合法工程（带空白）应取锁成功，实际 %v", lerr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("工程入参校验用例失败: %v", err)
	}
}

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

	deadline := time.Now().Add(rtTestProbeTimeout)
	for rtPGCount(t, db, "SELECT COUNT(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted") != 0 {
		if time.Now().After(deadline) {
			t.Fatal("ctx 取消后仍有等待中的顾问锁（等待没有被释放）")
		}
		time.Sleep(rtTestProbeStep)
	}

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

func errAssert(format string, args ...any) { panic(rtErrAssert{msg: fmt.Sprintf(format, args...)}) }

type rtErrAssert struct{ msg string }

func (e rtErrAssert) Error() string { return e.msg }

func recoverErrAssert(err *error) {
	if r := recover(); r != nil {
		if aerr, ok := r.(rtErrAssert); ok {
			*err = aerr
			return
		}
		panic(r)
	}
}

type rtFailConnPool struct{}

var rtErrFailNoSQL = errors.New("r1: 非事务句柄上不应发出任何 SQL")

func (rtFailConnPool) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, rtErrFailNoSQL
}

func (rtFailConnPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, rtErrFailNoSQL
}

func (rtFailConnPool) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, rtErrFailNoSQL
}

func (rtFailConnPool) QueryRowContext(context.Context, string, ...any) *sql.Row { return nil }
