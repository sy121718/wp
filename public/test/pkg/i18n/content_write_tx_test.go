package i18n_test

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
	txTestProjectA = "cccccccc-3333-3333-3333-333333333333"
	txTestProjectB = "dddddddd-4444-4444-4444-444444444444"

	txTestContext = "core.button.text"
	txTestLang    = "en-US"

	txAppRole     = "wp_test_rls_regress"
	txAppPassword = "wp_test_rls_regress"
)

func txTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newContentWriteDB(t)
	support.SeedProjectRow(t, db, txTestProjectA, "工程A")
	support.SeedProjectRow(t, db, txTestProjectB, "工程B")
	return db
}

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

func txCountInTx(t *testing.T, db *gorm.DB, projectID string) int64 {
	t.Helper()
	var n int64
	if err := db.Table("sys_translation").Where("project_id = ?::uuid", projectID).Count(&n).Error; err != nil {
		t.Fatalf("事务内统计译文行失败: %v", err)
	}
	return n
}

func txPGCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

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

func txPGError(t *testing.T, err error) *pgconn.PgError {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("应为真实 PG 驱动错误（pgconn.PgError），实际 %T：%v", err, err)
	}
	return pgErr
}

func txItem(projectID, sourceText, target string) i18n.ContentWriteItem {
	return i18n.ContentWriteItem{
		ProjectID: projectID, SourceHash: i18n.ContentHash(sourceText), Context: txTestContext,
		Lang: txTestLang, SourceText: sourceText, TargetText: target,
	}
}

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

func TestUpsertTxEmptyProjectWritesGlobalRow(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()
	src := "联系客服"
	hash := i18n.ContentHash(src)

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

func TestUpsertTxRejectsNonTxHandle(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()
	item := txItem(txTestProjectA, "加入购物车", "Add to cart")

	psDB := txOpenPrepared(t, db)

	for _, tc := range []struct {
		name string
		tx   *gorm.DB
	}{
		{"nil 句柄", nil},
		{"连接池句柄", db},
		{"连接池的 Session 句柄", db.Session(&gorm.Session{})},
		{"PrepareStmt 连接池句柄", psDB.Session(&gorm.Session{})},

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

func TestUpsertTxAcceptsPreparedStmtWrappedTx(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()
	psDB := txOpenPrepared(t, db)
	src := "加入收藏"

	err := psDB.Transaction(func(tx *gorm.DB) error {

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

func TestUpsertTxReturnsTxError(t *testing.T) {
	db := txTestDB(t)
	writer := i18n.NewContentWriter(db)
	ctx := t.Context()

	failed := db.Session(&gorm.Session{NewDB: true, Context: ctx})
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

var errBrokenConn = errors.New("测试用驱动错误：连接已断开")

type brokenConnPool struct{}

func (brokenConnPool) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errBrokenConn
}

func (brokenConnPool) PrepareContext(context.Context, string) (*sql.Stmt, error) {
	return nil, errBrokenConn
}

func (brokenConnPool) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errBrokenConn
}

func (brokenConnPool) QueryRowContext(context.Context, string, ...any) *sql.Row { return &sql.Row{} }

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

func TestUpsertTxDoesNotRewriteCallerScope(t *testing.T) {
	admin, app := txNonSuperuserFixture(t)
	ctx := context.Background()

	projectA, projectB := uuid.NewString(), uuid.NewString()
	support.SeedProjectRow(t, admin, projectA, "作用域工程A")
	support.SeedProjectRow(t, admin, projectB, "作用域工程B")

	writer := i18n.NewContentWriter(app)
	src := "限时优惠"

	const scopeProbe = "tx_scope_probe"

	var writeErr error
	err := app.Transaction(func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, projectA); serr != nil {
			return serr
		}

		if serr := tx.SavePoint(scopeProbe).Error; serr != nil {
			t.Fatalf("建立 savepoint %s 失败: %v", scopeProbe, serr)
		}
		_, werr := writer.UpsertTx(ctx, tx, []i18n.ContentWriteItem{txItem(projectB, src, "Limited offer")})
		if werr == nil {
			t.Fatal("调用方 scope 是工程 A，写入工程 B 的行必须被 RLS 拒绝（UpsertTx 不得替调用方改 scope）")
		}
		writeErr = werr

		if errors.Is(werr, i18n.ErrContentTxRequired) || errors.Is(werr, i18n.ErrContentProjectMixed) {
			t.Fatalf("拒绝必须来自数据库的 RLS 策略，而不是 pkg 自己的入参判定，实际 %v", werr)
		}
		if pgErr := txPGError(t, werr); pgErr.Code != "42501" {
			t.Fatalf("RLS 拒绝应为 SQLSTATE 42501（insufficient_privilege），实际 %s：%v", pgErr.Code, pgErr)
		}

		if rerr := tx.RollbackTo(scopeProbe).Error; rerr != nil {
			t.Fatalf("回滚到 savepoint %s 失败（事务仍处于 aborted 时会报 25P02）: %v", scopeProbe, rerr)
		}

		var current string
		if qerr := tx.Raw("SELECT current_setting('app.project_id', true)").Scan(&current).Error; qerr != nil {
			t.Fatalf("读取事务内 app.project_id 失败: %v", qerr)
		}
		if current != projectA {
			t.Fatalf("调用方 scope 应仍是工程 A，实际 %q", current)
		}

		return errors.New("回滚：本用例只验证越界写被拒、scope 不被改写")
	})
	if err == nil {
		t.Fatal("事务应回滚（事务体返回了业务错误）")
	}
	if writeErr == nil {
		t.Fatal("应记录到写入错误")
	}

	for _, pid := range []string{projectA, projectB} {
		if n := txRowCount(t, admin, pid); n != 0 {
			t.Fatalf("scope 校验失败后工程 %s 不应残留行，实际 %d", pid, n)
		}
	}
}

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

func adminUser() string {
	if v := strings.TrimSpace(os.Getenv("PGUSER")); v != "" {
		return v
	}
	return support.DefaultPGUser
}

func adminPassword() string {
	if v := strings.TrimSpace(os.Getenv("PGPASSWORD")); v != "" {
		return v
	}
	return support.DefaultPGPassword
}

func txPGEnv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func txNonSuperuserFixture(t *testing.T) (admin, app *gorm.DB) {
	t.Helper()
	admin = support.NewMigratedPGTestDB(t)

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
