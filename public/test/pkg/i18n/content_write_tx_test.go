package i18n_test

// content_write_tx_test.go — UpsertTx（调用方事务内的译文写入，导航译文改造 T1）在真实
// PostgreSQL 上的链路验证。
//
// 与同目录 content_write_test.go（测旧 Upsert 的写入/幂等/校验）互补，不重复其内容。
//
// 覆盖：
//  1. 外部事务回滚 → UpsertTx 写的行一并消失；
//  2. 外部事务提交 → 读路径读得到，且不串工程；
//  3. ProjectID 为空 → 全局行（project_id IS NULL），不需要作用域；
//  4. 非事务句柄 / PrepareStmt 连接池 / PrepareStmtTX 内部 Tx 为 nil →
//     ErrContentTxRequired、零写、不 panic；
//  5. PrepareStmt 包装的**有效**事务 → 被接受、能写、能回滚；
//  6. 已失败的事务句柄（tx.Error 非 nil）→ 原样交回该错误，不吞、不改写；
//  7. 混工程 / 全局+工程混批 → 写前拒绝；
//  8. 任一条非法 → 零写入（校验先于写）；
//  9. **scope 不被改写（拒绝路径）**：预设工程 A 的 app.project_id、批里是工程 B 的行 →
//     写入被 RLS 的 WITH CHECK 拒绝（42501）；用 SAVEPOINT 离开 aborted 后，事务内
//     scope 仍是 A。跑在**非超级角色**连接上；注意 ROLLBACK TO 本身会还原 savepoint
//     之后的 set_config，所以这条只证明「拒绝越界写」，见下一条；
// 10. **scope 不被改写（成功路径）**：同一非超级角色连接上，ScopeTx(A) → 写 A 的行
//     （不设 savepoint、没有回滚）→ 同事务内 current_setting 仍是 A、行落在 A。这条
//     才是「接受写入时也不动调用方 scope」的直接证据；
// 11. 旧 Upsert 的分组语义保持：工程行 + 全局行一批，各落各作用域。
//
// 表结构一律来自生产迁移（support.NewMigratedPGTestDB）：sys_translation 的 RLS 策略
// 由迁移 215 铺设、工程作用域列由迁移 195 铺设，本文件不手抄任何 DDL。
// PG 不可用时 t.Skip（support 内部口径不变）。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"go_wp/pkg/i18n"
	"go_wp/pkg/rls"
	"go_wp/public/test/support"
)

const (
	// txTestProjectA / txTestProjectB 本文件默认用例的两个工程（固定值，便于与库内容对照）。
	txTestProjectA = "cccccccc-3333-3333-3333-333333333333"
	txTestProjectB = "dddddddd-4444-4444-4444-444444444444"

	txTestContext = "core.button.text"
	txTestLang    = "en-US"

	// txAppRole / txAppPassword 非超级角色名（与 public/test/rls/nonsuperuser 用同一个
	// 固定名：pg_roles 是集群级对象，固定名 + 幂等建/改让并发与重复跑都收敛到同一角色）。
	txAppRole     = "wp_test_rls_regress"
	txAppPassword = "wp_test_rls_regress"
)

// txTestDB 建隔离测试库（模板库 = 生产迁移产物）并准备两个真实工程行。
func txTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newContentWriteDB(t)
	support.SeedProjectRow(t, db, txTestProjectA, "工程A")
	support.SeedProjectRow(t, db, txTestProjectB, "工程B")
	return db
}

// txRowCount 统计译文行数；projectID 为空 → 全局行（project_id IS NULL），否则该工程的行。
func txRowCount(t *testing.T, db *gorm.DB, projectID string) int64 {
	t.Helper()
	var n int64
	q := db.Table("sys_translation")
	if projectID == "" {
		q = q.Where("project_id IS NULL")
	} else {
		q = q.Where("project_id = ?::uuid", projectID)
	}
	if err := q.Count(&n).Error; err != nil {
		t.Fatalf("统计译文行失败: %v", err)
	}
	return n
}

// txCountInTx 在**给定句柄**（通常是事务）上统计本工程译文行数。
//
// 与 txRowCount 的差别：txRowCount 用独立连接（测试库属主）核对落库结果；这里要在
// 事务内核对「刚写进去的行在本事务里看得到」，且不受事务外未提交状态影响。
func txCountInTx(t *testing.T, db *gorm.DB, projectID string) int64 {
	t.Helper()
	var n int64
	if err := db.Table("sys_translation").Where("project_id = ?::uuid", projectID).Count(&n).Error; err != nil {
		t.Fatalf("事务内统计译文行失败: %v", err)
	}
	return n
}

// txPGCode 取错误的 SQLSTATE；不是 PG 驱动错误时返回空串（调用方据此区分环境条件）。
func txPGCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// txSkipOrFatalRole 角色夹具的 Skip/Fatal 判据（SQLSTATE 为准，不做字符串匹配）。
//
// 明确的**运维/环境**条件才 Skip：
//   - 42501 insufficient_privilege：测试库属主不是超级用户、无权建角色（托管 PG 上很常见）；
//   - 55006 object_in_use：并发跑时另一个进程正持有该角色相关的会话；
//   - 28P01 / 28000：口令、认证、pg_hba 拒绝（重复跑或被 DBA 改过口令）；
//   - 连接层不可达（无 SQLSTATE，support.ErrPGUnavailable 那条路径）。
//
// 其余一律 Fatal：语法错误、属性写错、授权失败都必须显式暴露，不能被 Skip 掩盖。
func txSkipOrFatalRole(t *testing.T, what string, err error) {
	t.Helper()
	switch txPGCode(err) {
	case "42501", "55006", "28P01", "28000":
		t.Skipf("%s 被环境拒绝（SQLSTATE %s）：%v", what, txPGCode(err), err)
	case "":
		if errors.Is(err, support.ErrPGUnavailable) {
			t.Skipf("%s 时数据库不可达（环境条件）：%v", what, err)
		}
	}
	t.Fatalf("%s 失败：%v", what, err)
}

// txPGError 把错误解到真实的 PG 驱动错误（pgconn.PgError）并返回。
//
// GORM 打开 TranslateError 时只对 23505/23503/42703/23514 四类包一层 gorm 错误，
// RLS 的 42501 原样返回（gorm.io/driver/postgres@v1.6.3/error_translator.go:29-43、
// gorm@v1.31.2/gorm.go:405-412）；两种形态 errors.As 都能穿过。
func txPGError(t *testing.T, err error) *pgconn.PgError {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("应为真实 PG 驱动错误（pgconn.PgError），实际 %T：%v", err, err)
	}
	return pgErr
}

// txItem 构造一条合法写入项（hash 由原文现算，满足写入口的一致性校验）。
func txItem(projectID, sourceText, target string) i18n.ContentWriteItem {
	return i18n.ContentWriteItem{
		ProjectID: projectID, SourceHash: i18n.ContentHash(sourceText), Context: txTestContext,
		Lang: txTestLang, SourceText: sourceText, TargetText: target,
	}
}

// TestUpsertTxRollbackLeavesNoRows 外部事务回滚 → 本方法写的行一并消失。
//
// 这是 UpsertTx 的存在理由：译文与同一业务动作的其它写入要么一起成功、要么一起没有。
// 事务在属主连接上开始，写工程行仍需先设 app.project_id（sys_translation 带 FORCE 策略，
// 属主同样受 WITH CHECK 约束）—— 这正是 UpsertTx 的调用约定：调用方负责 scope。
func TestUpsertTxRollbackLeavesNoRows(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()
	src := "立即购买"

	err := db.Transaction(func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, txTestProjectA); serr != nil {
			return serr
		}
		written, werr := writer.UpsertTx(ctx, tx, []i18n.ContentWriteItem{txItem(txTestProjectA, src, "Buy now")})
		if werr != nil {
			return werr
		}
		if written != 1 {
			t.Fatalf("应写入 1 条，实际 %d", written)
		}
		if n := txRowCount(t, tx, txTestProjectA); n != 1 {
			t.Fatalf("事务内应看到 1 行，实际 %d", n)
		}
		return errors.New("业务后续步骤失败，整笔回滚")
	})
	if err == nil {
		t.Fatal("外部事务应返回错误并回滚")
	}
	if n := txRowCount(t, db, txTestProjectA); n != 0 {
		t.Fatalf("回滚后不应残留工程行，实际 %d", n)
	}
	if n := txRowCount(t, db, ""); n != 0 {
		t.Fatalf("回滚后不应残留全局行，实际 %d", n)
	}
}

// TestUpsertTxCommitIsVisibleToUpsertReads 事务提交 → 读路径看得到；别的工程读不到。
func TestUpsertTxCommitIsVisibleToUpsertReads(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()
	src := "库存不足"
	hash := i18n.ContentHash(src)

	if err := db.Transaction(func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, txTestProjectA); serr != nil {
			return serr
		}
		_, werr := writer.UpsertTx(ctx, tx, []i18n.ContentWriteItem{txItem(txTestProjectA, src, "Out of stock")})
		return werr
	}); err != nil {
		t.Fatalf("事务提交失败: %v", err)
	}

	targets, err := writer.LoadTargetsForProject(ctx, txTestProjectA, txTestLang, []string{hash})
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if got := targets[i18n.ContentIndexKey(hash, txTestContext)]; got != "Out of stock" {
		t.Fatalf("提交后应读到 UpsertTx 写入的译文，实际 %q", got)
	}
	if n := txRowCount(t, db, ""); n != 0 {
		t.Fatalf("工程行写入不应产生全局行，实际 %d", n)
	}
	if got, gerr := writer.LoadTargetsForProject(ctx, txTestProjectB, txTestLang, []string{hash}); gerr != nil {
		t.Fatalf("工程 B 读回失败: %v", gerr)
	} else if len(got) != 0 {
		t.Fatalf("工程 B 不应读到工程 A 的译文，实际 %v", got)
	}
}

// TestUpsertTxEmptyProjectWritesGlobalRow ProjectID 为空 → 全局行，且**不需要**任何作用域。
//
// 同时钉住「调用方负责 scope」：全局行在无变量的连接上也必须能写进去
// （策略谓词对 project_id IS NULL 恒真），否则说明 scope 责任被误挪回了本方法。
func TestUpsertTxEmptyProjectWritesGlobalRow(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()
	src := "联系客服"
	hash := i18n.ContentHash(src)

	// 刻意不调用 rls.ScopeTx。
	if err := db.Transaction(func(tx *gorm.DB) error {
		written, werr := writer.UpsertTx(ctx, tx, []i18n.ContentWriteItem{txItem("", src, "Contact us")})
		if werr != nil {
			return werr
		}
		if written != 1 {
			t.Fatalf("应写入 1 条，实际 %d", written)
		}
		return nil
	}); err != nil {
		t.Fatalf("全局行写入失败: %v", err)
	}

	if n := txRowCount(t, db, ""); n != 1 {
		t.Fatalf("全局行应为 project_id IS NULL 的一行，实际 %d", n)
	}
	if n := txRowCount(t, db, txTestProjectA); n != 0 {
		t.Fatalf("不应写工程行，实际 %d", n)
	}
	targets, err := writer.LoadTargets(ctx, txTestLang, []string{hash})
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if got := targets[i18n.ContentIndexKey(hash, txTestContext)]; got != "Contact us" {
		t.Fatalf("全局译文应可读回，实际 %q", got)
	}
}

// TestUpsertTxRejectsNonTxHandle 非事务 / typed-nil 句柄 → ErrContentTxRequired、零写、不 panic。
func TestUpsertTxRejectsNonTxHandle(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()
	item := txItem(txTestProjectA, "加入购物车", "Add to cart")

	// 真实打开 PrepareStmt 的连接池句柄（不是事务）。
	psDB := txOpenPrepared(t, db)

	for _, tc := range []struct {
		name string
		tx   *gorm.DB
	}{
		{"nil 句柄", nil},
		{"连接池句柄", db},
		{"连接池的 Session 句柄", db.Session(&gorm.Session{})},
		{"PrepareStmt 连接池句柄", psDB.Session(&gorm.Session{})},
		// GORM 导出的预备语句事务包装，内部 Tx 为 nil：`== nil` 为假、解引用会 panic ——
		// 判别必须在入口挡住它，而不是放行后让写入路径崩掉。
		{"PrepareStmtTX 且内部 Tx 为 nil", &gorm.DB{Config: db.Config,
			Statement: &gorm.Statement{DB: db, ConnPool: &gorm.PreparedStmtTX{}}}},
	} {
		written, werr := writer.UpsertTx(ctx, tc.tx, []i18n.ContentWriteItem{item})
		if !errors.Is(werr, i18n.ErrContentTxRequired) {
			t.Fatalf("%s 应返回 ErrContentTxRequired，实际 (%d, %v)", tc.name, written, werr)
		}
		if written != 0 {
			t.Fatalf("%s 不应报告写入条数，实际 %d", tc.name, written)
		}
	}

	// 全局行 + 连接池句柄同样拒绝：不是「没东西要写」就能放宽事务判据。
	if _, werr := writer.UpsertTx(ctx, db, []i18n.ContentWriteItem{txItem("", "联系客服", "Contact us")}); !errors.Is(werr, i18n.ErrContentTxRequired) {
		t.Fatalf("全局行 + 连接池句柄应返回 ErrContentTxRequired，实际 %v", werr)
	}
	if n := txRowCount(t, db, txTestProjectA); n != 0 {
		t.Fatalf("非事务句柄不应写入任何工程行，实际 %d", n)
	}
	if n := txRowCount(t, db, ""); n != 0 {
		t.Fatalf("非事务句柄不应写入任何全局行，实际 %d", n)
	}
}

// TestUpsertTxAcceptsPreparedStmtWrappedTx PrepareStmt 包装的**有效**事务句柄必须被接受。
//
// 本仓默认 PrepareStmt=false（事务句柄是 *sql.Tx），但判据若写成「只认 *sql.Tx」，
// 打开 PrepareStmt 后真事务会被误判成非事务。这条用例把两种形状都钉住：
// 包装句柄上能写、且写入不越过事务边界（回滚后零行）。
func TestUpsertTxAcceptsPreparedStmtWrappedTx(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()
	psDB := txOpenPrepared(t, db)
	src := "加入收藏"

	err := psDB.Transaction(func(tx *gorm.DB) error {
		// 不用 rls.ScopeTx：它只认 *sql.Tx，而 PrepareStmt=true 下的事务句柄是
		// *gorm.PreparedStmtTX（pkg/rls/rls.go:75-77 会返回 ErrNotInTransaction，
		// 这条用例会变成「测 rls 的已知缺口」而不是「测 UpsertTx 接受包装事务」）。
		// 作用域在这里只作为写工程行的前置条件，用参数化 set_config 设 —— 不拼字符串，
		// 也不改 pkg/rls（那是独立范围）。
		if serr := tx.Exec("SELECT set_config('app.project_id', ?, true)", txTestProjectA).Error; serr != nil {
			t.Fatalf("PrepareStmt 事务内设置 app.project_id 失败: %v", serr)
		}
		written, werr := writer.UpsertTx(ctx, tx, []i18n.ContentWriteItem{txItem(txTestProjectA, src, "Add to wishlist")})
		if werr != nil {
			return werr
		}
		if written != 1 {
			t.Fatalf("PrepareStmt 事务应写入 1 条，实际 %d", written)
		}
		return errors.New("回滚：验证包装事务里的写入不会自行提交")
	})
	if err == nil {
		t.Fatal("PrepareStmt 事务应以业务错误回滚")
	}
	if n := txRowCount(t, db, txTestProjectA); n != 0 {
		t.Fatalf("包装事务回滚后不应残留行，实际 %d", n)
	}

	// 再提交一次，确认写入本身成立（不是「恰好因为回滚而没落库」）。
	if cerr := psDB.Transaction(func(tx *gorm.DB) error {
		if serr := tx.Exec("SELECT set_config('app.project_id', ?, true)", txTestProjectA).Error; serr != nil {
			t.Fatalf("PrepareStmt 事务内设置 app.project_id 失败: %v", serr)
		}
		_, werr := writer.UpsertTx(ctx, tx, []i18n.ContentWriteItem{txItem(txTestProjectA, src, "Add to wishlist")})
		return werr
	}); cerr != nil {
		t.Fatalf("PrepareStmt 事务提交失败: %v", cerr)
	}
	if n := txRowCount(t, db, txTestProjectA); n != 1 {
		t.Fatalf("提交后应有 1 行，实际 %d", n)
	}
}

// TestUpsertTxReturnsTxError 已失败的事务句柄：把既有错误原样交回，不吞、不改写。
//
// 形状：ConnPool 不是事务、tx.Error 带真实失败原因（如 Begin 失败 / 会话已断）。
// 若把这种情况归成 ErrContentTxRequired，调用方会丢掉真实原因。
func TestUpsertTxReturnsTxError(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()

	failed := db.Session(&gorm.Session{NewDB: true})
	failed.Statement.ConnPool = &brokenConnPool{}
	failed.Error = errBrokenConn

	_, err := writer.UpsertTx(ctx, failed, []i18n.ContentWriteItem{txItem(txTestProjectA, "继续购物", "Continue shopping")})
	if !errors.Is(err, errBrokenConn) {
		t.Fatalf("应原样返回句柄上的既有错误，实际 %v", err)
	}
	if errors.Is(err, i18n.ErrContentTxRequired) {
		t.Fatalf("真实错误不应被改写成 ErrContentTxRequired，实际 %v", err)
	}
	if n := txRowCount(t, db, txTestProjectA); n != 0 {
		t.Fatalf("失败句柄不应写入任何行，实际 %d", n)
	}
}

// errBrokenConn 测试用驱动错误（模拟连接层失败）。
var errBrokenConn = errors.New("测试用驱动错误：连接已断开")

// brokenConnPool 一个非事务 ConnPool（本用例里不会被真正调用：tx.Error 已非 nil）。
type brokenConnPool struct{}

// ExecContext 返回驱动错误。
func (brokenConnPool) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errBrokenConn
}

// PrepareContext 返回驱动错误。
func (brokenConnPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errBrokenConn
}

// QueryContext 返回驱动错误。
func (brokenConnPool) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errBrokenConn
}

// QueryRowContext 返回空行。
func (brokenConnPool) QueryRowContext(context.Context, string, ...any) *sql.Row { return &sql.Row{} }

// TestUpsertTxRejectsMixedProjectsBeforeWrite 一批跨工程 / 全局+工程 → 整批拒绝，且写前拒绝。
func TestUpsertTxRejectsMixedProjectsBeforeWrite(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()

	err := db.Transaction(func(tx *gorm.DB) error {
		written, werr := writer.UpsertTx(ctx, tx, []i18n.ContentWriteItem{
			txItem(txTestProjectA, "继续购物", "Continue shopping"),
			txItem(txTestProjectB, "继续购物", "Keep shopping"),
		})
		if werr != nil {
			return werr
		}
		t.Fatalf("混工程批应被拒绝，实际写入 %d 条", written)
		return nil
	})
	if !errors.Is(err, i18n.ErrContentProjectMixed) {
		t.Fatalf("应返回 ErrContentProjectMixed，实际 %v", err)
	}
	for _, pid := range []string{"", txTestProjectA, txTestProjectB} {
		if n := txRowCount(t, db, pid); n != 0 {
			t.Fatalf("混工程拒绝后不应有任何行（project=%q），实际 %d", pid, n)
		}
	}

	// 全局行与工程行混在一批同样是混工程（"" 与 uuid 是两个作用域）。
	err = db.Transaction(func(tx *gorm.DB) error {
		_, werr := writer.UpsertTx(ctx, tx, []i18n.ContentWriteItem{
			txItem("", "继续购物", "Continue shopping"),
			txItem(txTestProjectA, "继续购物", "Keep shopping"),
		})
		return werr
	})
	if !errors.Is(err, i18n.ErrContentProjectMixed) {
		t.Fatalf("全局行 + 工程行应返回 ErrContentProjectMixed，实际 %v", err)
	}
	if n := txRowCount(t, db, ""); n != 0 {
		t.Fatalf("混工程拒绝时全局行也不应落库，实际 %d", n)
	}
	if n := txRowCount(t, db, txTestProjectA); n != 0 {
		t.Fatalf("混工程拒绝时工程行不应落库，实际 %d", n)
	}
}

// TestUpsertTxInvalidItemWritesNothing 批中任一条不合法 → 整体零写入（与 Upsert 同规则）。
func TestUpsertTxInvalidItemWritesNothing(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()
	good := txItem(txTestProjectA, "了解更多", "Learn more")

	bad := []struct {
		name string
		item i18n.ContentWriteItem
		want error
	}{
		{"空译文", i18n.ContentWriteItem{ProjectID: txTestProjectA, SourceHash: i18n.ContentHash("了解更多"), Context: txTestContext, Lang: txTestLang, SourceText: "了解更多", TargetText: "   "}, i18n.ErrContentTargetEmpty},
		{"hash 不一致", i18n.ContentWriteItem{ProjectID: txTestProjectA, SourceHash: i18n.ContentHash("另一段原文"), Context: txTestContext, Lang: txTestLang, SourceText: "了解更多", TargetText: "Learn more"}, i18n.ErrContentHashMismatch},
		{"非法 engine", i18n.ContentWriteItem{ProjectID: txTestProjectA, SourceHash: i18n.ContentHash("了解更多"), Context: txTestContext, Lang: txTestLang, SourceText: "了解更多", TargetText: "Learn more", Engine: "machine"}, i18n.ErrContentEngineInvalid},
		{"空语言", i18n.ContentWriteItem{ProjectID: txTestProjectA, SourceHash: i18n.ContentHash("了解更多"), Context: txTestContext, SourceText: "了解更多", TargetText: "Learn more"}, i18n.ErrContentLangEmpty},
		{"空语境", i18n.ContentWriteItem{ProjectID: txTestProjectA, SourceHash: i18n.ContentHash("了解更多"), Lang: txTestLang, SourceText: "了解更多", TargetText: "Learn more"}, i18n.ErrContentContextEmpty},
	}
	for _, tc := range bad {
		err := db.Transaction(func(tx *gorm.DB) error {
			_, werr := writer.UpsertTx(ctx, tx, []i18n.ContentWriteItem{good, tc.item})
			return werr
		})
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s 应返回 %v，实际 %v", tc.name, tc.want, err)
		}
		if n := txRowCount(t, db, txTestProjectA); n != 0 {
			t.Fatalf("%s：前一条合法行不应落库，实际 %d 行", tc.name, n)
		}
	}
}

// TestUpsertTxDoesNotRewriteCallerScope 拒绝路径：UpsertTx 不会替调用方改 scope 来
// 让越界写入成功。
//
// 跑在**非超级角色**（NOSUPERUSER NOBYPASSRLS）上：超级用户无条件绕过 RLS，
// 用 root 跑这套断言会全绿而没有一行是真的。
//
// 判据：
//  1. 事务内先按工程 A 设 app.project_id，再写工程 B 的行 → 写入必须**失败**，
//     且失败是数据库的 RLS WITH CHECK 违规（真实 pgconn.PgError，SQLSTATE 42501），
//     不是 pkg 自己的入参判定。（修复前 UpsertTx 会自己 set_config 到 B，写入成功 ——
//     调用方以为还在 A，实际已切走。）
//  2. **必须用 savepoint**：RLS 违规会把事务打成 aborted，此后任何语句都报 25P02
//     （current transaction is aborted），直读 current_setting 会假绿 —— 断言永远执行不到。
//     SAVEPOINT 必须在 UpsertTx 之前建，ROLLBACK TO 之后才能在同事务内继续查询。
//  3. ROLLBACK TO 之后同事务读 current_setting 严格等于 A。
//  4. 事务外（管理连接）两个作用域都零残留。
//
// **这条路径的证明力有边界**：PostgreSQL 的 ROLLBACK TO SAVEPOINT 会把 savepoint 之后
// 的 set_config(..., is_local=true) 一并回退（它属于事务内的会话设置变更），所以本用例
// **只证明「拒绝越界写」**，不能单独证明「UpsertTx 不改 scope」。后者由
// TestUpsertTxKeepCallerScopeOnSuccessfulWrite 的成功路径直接证明。
func TestUpsertTxDoesNotRewriteCallerScope(t *testing.T) {
	admin, app := txNonSuperuserFixture(t)
	ctx := context.Background()

	projectA, projectB := uuid.NewString(), uuid.NewString()
	support.SeedProjectRow(t, admin, projectA, "作用域工程A")
	support.SeedProjectRow(t, admin, projectB, "作用域工程B")

	writer := i18n.NewContentWriter(app)
	src := "限时优惠"

	// savepoint 名固定即可：每个用例独占一个测试库、独占一条事务。
	const scopeProbe = "tx_scope_probe"

	var writeErr error
	err := app.Transaction(func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectA); serr != nil {
			return serr
		}
		// 先在 scope=A 下建 savepoint，再写 B 的行：违规后事务进入 aborted，
		// 只有 ROLLBACK TO 能离开该状态并让后续断言真正执行。
		if serr := tx.SavePoint(scopeProbe).Error; serr != nil {
			t.Fatalf("建立 savepoint %s 失败: %v", scopeProbe, serr)
		}
		_, werr := writer.UpsertTx(ctx, tx, []i18n.ContentWriteItem{txItem(projectB, src, "Limited offer")})
		if werr == nil {
			t.Fatal("调用方 scope 是工程 A，写入工程 B 的行必须被 RLS 拒绝（UpsertTx 不得替调用方改 scope）")
		}
		writeErr = werr
		// 失败必须是数据库策略拒的，且必须是 42501（insufficient_privilege）——
		// 不是入参判定，也不是事务已 aborted 或其它 SQL 错误。
		if errors.Is(werr, i18n.ErrContentTxRequired) || errors.Is(werr, i18n.ErrContentProjectMixed) {
			t.Fatalf("拒绝必须来自数据库的 RLS 策略，而不是 pkg 自己的入参判定，实际 %v", werr)
		}
		if pgErr := txPGError(t, werr); pgErr.Code != "42501" {
			t.Fatalf("RLS 拒绝应为 SQLSTATE 42501（insufficient_privilege），实际 %s：%v", pgErr.Code, pgErr)
		}
		// 离开 aborted：ROLLBACK TO 撤销 savepoint 之后的语句（含那条被拒的 INSERT）。
		if rerr := tx.RollbackTo(scopeProbe).Error; rerr != nil {
			t.Fatalf("回滚到 savepoint %s 失败（事务仍处于 aborted 时会报 25P02）: %v", scopeProbe, rerr)
		}
		// ROLLBACK TO 之后同一事务里读调用方 scope，必须严格是 A。
		var current string
		if qerr := tx.Raw("SELECT current_setting('app.project_id', true)").Scan(&current).Error; qerr != nil {
			t.Fatalf("读取事务内 app.project_id 失败: %v", qerr)
		}
		if current != projectA {
			t.Fatalf("调用方 scope 应仍是工程 A，实际 %q", current)
		}
		// 用业务错误触发回滚，保持事务边界干净。
		return errors.New("回滚：本用例只验证越界写被拒、scope 不被改写")
	})
	if err == nil {
		t.Fatal("事务应回滚（事务体返回了业务错误）")
	}
	if writeErr == nil {
		t.Fatal("应记录到写入错误")
	}

	// 管理连接（超级用户）确认：两个作用域都没有残留行。
	for _, pid := range []string{projectA, projectB} {
		if n := txRowCount(t, admin, pid); n != 0 {
			t.Fatalf("scope 校验失败后工程 %s 不应残留行，实际 %d", pid, n)
		}
	}
}

// TestUpsertTxKeepCallerScopeOnSuccessfulWrite 成功路径：接受写入时也不动调用方 scope。
//
// 与 TestUpsertTxDoesNotRewriteCallerScope 的分工：那条走「被拒」分支，且依赖
// ROLLBACK TO SAVEPOINT —— 而 ROLLBACK TO 会把 savepoint 之后的 set_config 一并回退，
// 所以它证明不了 UpsertTx 有没有设过变量。这条**不设 savepoint、不回滚**，让
// UpsertTx 在 scope=A 下真的写成功，然后断言：
//  1. 写入成功（1 条）；
//  2. 同事务内 current_setting('app.project_id') 严格等于 A —— 若 UpsertTx 在写前/写后
//     设过任何值，这里读到的是它的值而不是调用方的 A；
//  3. 行确实落在工程 A，且本事务内可见。
//
// 同样跑在非超级角色连接上：超级用户下 scope 断言同样成立但写路径绕过 RLS，证明力更弱。
func TestUpsertTxKeepCallerScopeOnSuccessfulWrite(t *testing.T) {
	admin, app := txNonSuperuserFixture(t)
	ctx := context.Background()

	projectA := uuid.NewString()
	support.SeedProjectRow(t, admin, projectA, "成功路径工程A")

	writer := i18n.NewContentWriter(app)
	src := "库存紧张"

	err := app.Transaction(func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectA); serr != nil {
			return serr
		}
		written, werr := writer.UpsertTx(ctx, tx, []i18n.ContentWriteItem{txItem(projectA, src, "Low stock")})
		if werr != nil {
			t.Fatalf("scope=A 下写 A 的行必须成功: %v", werr)
		}
		if written != 1 {
			t.Fatalf("应写入 1 条，实际 %d", written)
		}
		var current string
		if qerr := tx.Raw("SELECT current_setting('app.project_id', true)").Scan(&current).Error; qerr != nil {
			t.Fatalf("读取事务内 app.project_id 失败: %v", qerr)
		}
		if current != projectA {
			t.Fatalf("UpsertTx 改写了调用方 scope：期望 %q，实际 %q", projectA, current)
		}
		if n := txCountInTx(t, tx, projectA); n != 1 {
			t.Fatalf("事务内工程 A 应有 1 行，实际 %d", n)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("事务提交失败: %v", err)
	}
	if n := txRowCount(t, admin, projectA); n != 1 {
		t.Fatalf("提交后工程 A 应有 1 行，实际 %d", n)
	}
	if n := txRowCount(t, admin, ""); n != 0 {
		t.Fatalf("工程行写入不应产生全局行，实际 %d", n)
	}
}

// TestUpsertGroupedMixedBatchScopesEachGroup 旧 Upsert 的「分组 + 逐组 scope」语义保持。
//
// 一批里同时有工程行与全局行：在非超级角色下必须整批成功、各落各的作用域，
// 且**不需要**调用方预先 scope（Upsert 自己开事务并逐组 set_config）。
func TestUpsertGroupedMixedBatchScopesEachGroup(t *testing.T) {
	admin, app := txNonSuperuserFixture(t)
	ctx := context.Background()

	projectA := uuid.NewString()
	support.SeedProjectRow(t, admin, projectA, "分组工程A")

	writer := i18n.NewContentWriter(app)
	const srcScoped = "立即购买"
	const srcGlobal = "联系客服"
	written, err := writer.Upsert(ctx, []i18n.ContentWriteItem{
		txItem(projectA, srcScoped, "Buy now"),
		txItem("", srcGlobal, "Contact us"),
	})
	if err != nil {
		t.Fatalf("混组写入失败（Upsert 应当自己逐组 scope）: %v", err)
	}
	if written != 2 {
		t.Fatalf("应写入 2 条，实际 %d", written)
	}
	if n := txRowCount(t, admin, projectA); n != 1 {
		t.Fatalf("工程 A 应有 1 行，实际 %d", n)
	}
	if n := txRowCount(t, admin, ""); n != 1 {
		t.Fatalf("全局应有 1 行，实际 %d", n)
	}

	keyScoped := i18n.ContentIndexKey(i18n.ContentHash(srcScoped), txTestContext)
	gotA, err := writer.LoadTargetsForProject(ctx, projectA, txTestLang, []string{i18n.ContentHash(srcScoped)})
	if err != nil {
		t.Fatalf("工程 A 读回失败: %v", err)
	}
	if gotA[keyScoped] != "Buy now" {
		t.Fatalf("工程 A 应读回自己的译法，实际 %q", gotA[keyScoped])
	}
}

// ── 非超级角色与 PrepareStmt 夹具 ────────────────────────────────────────────

// txOpenPrepared 在同一个测试库上另开一条 PrepareStmt=true 的管理连接。
//
// 连接池本身不是事务句柄（ConnPool 是 *gorm.PreparedStmtDB）；它开出来的事务才是
// *gorm.PreparedStmtTX —— 正是生产打开 PrepareStmt 后事务句柄的形状。
// 表结构/数据都在同一个库，所以另开连接只是为了换 gorm.Config。
func txOpenPrepared(t *testing.T, admin *gorm.DB) *gorm.DB {
	t.Helper()
	psDB, err := gorm.Open(postgres.Open(txDSN(t, admin, adminUser(), adminPassword())),
		&gorm.Config{PrepareStmt: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatalf("打开 PrepareStmt 连接失败: %v", err)
	}
	if sqlDB, derr := psDB.DB(); derr == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	return psDB
}

// txDSN 拼当前测试库的 DSN（角色/mode 由调用方给）。
func txDSN(t *testing.T, admin *gorm.DB, user, password string) string {
	t.Helper()
	var dbName string
	if err := admin.Raw("SELECT current_database()").Scan(&dbName).Error; err != nil {
		t.Fatalf("读取当前库名失败：%v", err)
	}
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai search_path=public,ext_shared",
		txPGEnv("PGHOST", support.DefaultPGHost), user, password, dbName,
		txPGEnv("PGPORT", support.DefaultPGPort))
}

// adminUser / adminPassword 取管理连接的认证信息（support 导出的默认值 + 环境变量覆盖）。
func adminUser() string {
	if v := strings.TrimSpace(os.Getenv("PGUSER")); v != "" {
		return v
	}
	return support.DefaultPGUser
}

// adminPassword 取管理连接口令（与 support 的默认口径一致）。
func adminPassword() string {
	if v := strings.TrimSpace(os.Getenv("PGPASSWORD")); v != "" {
		return v
	}
	return support.DefaultPGPassword
}

// txPGEnv 读 libpq 环境变量，空则回退（默认值取自 support 的导出口径）。
func txPGEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// txNonSuperuserFixture 返回 (管理连接, 非超级角色业务连接)。
//
// 管理连接 = support.NewMigratedPGTestDB（生产迁移结构）；业务连接 = 以
// NOSUPERUSER NOBYPASSRLS 角色登录的第二条连接，并断言它不是 superuser / BYPASSRLS。
//
// Skip 口径（只跳过**明确的**环境条件，其余一律 Fatal）：
//   - 测试库属主本身是超级用户都建不了角色 → 42501 insufficient_privilege / 55006（并发下
//     对象被占用）→ Skip；
//   - 第二条连接失败且 SQLSTATE 是 28P01 / 28000（认证、pg_hba）或连接层不可达
//     （没有 SQLSTATE，即 support.ErrPGUnavailable 那条路径）→ Skip；
//   - 其余（语法错、属性写错、授权错、角色属性漂移）→ Fatal，不许被字符串 contains 吞掉。
//
// 角色名/口令与 public/test/rls/nonsuperuser 共用同一个固定名（pg_roles 是集群级对象），
// 这里只复用、不新建名字，也不动主库（测试库由 support 每次新建并自动 DROP）。
func txNonSuperuserFixture(t *testing.T) (admin, app *gorm.DB) {
	t.Helper()
	admin = support.NewMigratedPGTestDB(t)

	// 建角色用「存在性 + 属性收敛」两步，但**不做字符串匹配**：并发跑时 CREATE ROLE
	// 可能撞 duplicate_object（42710），那是无害竞态，后面的 ALTER 会把属性收敛回去。
	create := fmt.Sprintf("DO $$ BEGIN\n"+
		"    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '%s') THEN\n"+
		"        CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOBYPASSRLS;\n"+
		"    END IF;\n"+
		"END $$", txAppRole, txAppRole, txAppPassword)
	if err := admin.Exec(create).Error; err != nil {
		txSkipOrFatalRole(t, "建立非超级角色 "+txAppRole, err)
		return nil, nil
	}
	alter := fmt.Sprintf("ALTER ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOBYPASSRLS", txAppRole, txAppPassword)
	if err := admin.Exec(alter).Error; err != nil {
		txSkipOrFatalRole(t, "对齐角色 "+txAppRole+" 属性", err)
		return nil, nil
	}
	for _, stmt := range []string{
		"GRANT USAGE ON SCHEMA public TO " + txAppRole,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO " + txAppRole,
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO " + txAppRole,
	} {
		if err := admin.Exec(stmt).Error; err != nil {
			txSkipOrFatalRole(t, "授权（"+stmt+"）", err)
			return nil, nil
		}
	}

	app, err := gorm.Open(postgres.Open(txDSN(t, admin, txAppRole, txAppPassword)), &gorm.Config{})
	if err != nil {
		if txPGCode(err) == "28P01" || txPGCode(err) == "28000" || errors.Is(err, support.ErrPGUnavailable) {
			t.Skipf("以非超级角色建立第二条连接被 pg_hba/认证层拒绝（环境条件）：%v", err)
			return nil, nil
		}
		t.Fatalf("以非超级角色建立第二条连接失败（不是认证层面的环境条件）：%v", err)
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
	if currentUser != txAppRole {
		t.Fatalf("业务连接当前角色应为 %q，实际 %q", txAppRole, currentUser)
	}
	bypass, err := rls.BypassedRole(context.Background(), app)
	if err != nil {
		t.Fatalf("查询角色 RLS 属性失败：%v", err)
	}
	if bypass {
		t.Fatalf("角色 %s 是 superuser / BYPASSRLS，RLS 对它不生效，本文件断言将全部失真", txAppRole)
	}
	return admin, app
}
