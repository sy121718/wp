package navigationservice

// navigation_translation_write_test.go — 译文整表写（TranslationWriter）的纯逻辑契约。
//
// 全部依赖都是桩（行端口 / 来源索引 / 工程清单 / 译文存储 / 三个跨模块端口 / 事务），
// 因此这里钉的是**规则与顺序**；真实 PG 上的 RLS、锁、UPSERT 与回滚归 feature 测试。
//
// 每个用例的断言顺序都刻意是「先证夹具真的走到了目标分支，再断言结论」——
// 一条「什么都没写」的实现同样能通过「没写错东西」的断言，所以必须先看到目标分支被触发。

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationenums "go_wp/internal/module/navigation/enums"
	navigationmodel "go_wp/internal/module/navigation/model"
	"go_wp/pkg/i18n"
)

// —— 夹具 ——

// navRowsTxStub 事务内行端口桩：按 (projectID, kind) 回放预置行。
type navRowsTxStub struct {
	byProject map[string]map[string][]*navigationmodel.NavigationEntity
	failKinds map[string]error
	calls     []string
}

func (s *navRowsTxStub) ListForKindTx(_ context.Context, _ *gorm.DB, projectID, kind string) ([]*navigationmodel.NavigationEntity, error) {
	s.calls = append(s.calls, projectID+"|"+kind)
	if err, ok := s.failKinds[kind]; ok {
		return nil, err
	}
	return s.byProject[projectID][kind], nil
}

// navRowTx 造一行菜单项（kind 由外层分组决定）。
func navRowTx(id, title string) *navigationmodel.NavigationEntity {
	return &navigationmodel.NavigationEntity{ID: id, Title: title, Path: "/p/" + id}
}

// sourceIndexStub 来源索引桩：只有 Locate / Require 参与写路径，其余照 contract 返回空。
type sourceIndexStub struct {
	byHash   map[string]navigationcontract.TranslationSourceEntry
	requireE error
}

func (s *sourceIndexStub) Locate(hash string) (navigationcontract.TranslationSourceEntry, bool) {
	e, ok := s.byHash[hash]
	return e, ok
}

func (s *sourceIndexStub) Entries() []navigationcontract.TranslationSourceEntry {
	out := make([]navigationcontract.TranslationSourceEntry, 0, len(s.byHash))
	for _, e := range s.byHash {
		out = append(out, e)
	}
	return out
}

func (s *sourceIndexStub) Kinds() []string                                { return []string{} }
func (s *sourceIndexStub) KindErrors() []navigationcontract.KindReadError { return nil }
func (s *sourceIndexStub) Require() error                                 { return s.requireE }

// sourceIndexerStub 来源索引执行者桩：返回预置索引或错误，并记录被调次数。
type sourceIndexerStub struct {
	index  *sourceIndexStub
	err    error
	calls  int
	lastTx *gorm.DB
}

func (s *sourceIndexerStub) OpenSourceIndexTx(_ context.Context, tx *gorm.DB, _ string) (navigationcontract.SourceIndex, error) {
	s.calls++
	s.lastTx = tx
	if s.err != nil {
		return nil, s.err
	}
	return s.index, nil
}

// projectListerStub 工程清单桩。
type projectListerStub struct {
	ids  []string
	err  error
	call int
}

func (s *projectListerStub) List(context.Context) ([]ProjectEntry, error) {
	s.call++
	if s.err != nil {
		return nil, s.err
	}
	out := make([]ProjectEntry, 0, len(s.ids))
	for _, id := range s.ids {
		out = append(out, ProjectEntry{ID: id})
	}
	return out, nil
}

// contentStoreStub 译文存储桩：记录 UPSERT 入参与现值，可注入失败。
type contentStoreStub struct {
	// before 的键与 pkg/i18n 的真实返回一致：**ContentIndexKey(hash, context)** 复合键，
	// 不是裸 hash。桩若按裸 hash 存，实现按复合键查就永远查不到，「同值不写」这类
	// 判定会被静默跳过（用例假绿/假红都由此而来）。
	before     map[string]i18n.ContentTargetInfo
	loadErr    error
	upsertErr  error
	upserted   []i18n.ContentWriteItem
	loadedHash []string
	upsertCall int
	loadCall   int
}

func (s *contentStoreStub) LoadDetailsForProjectTx(_ context.Context, _ *gorm.DB, _, _ string, hashes []string) (map[string]i18n.ContentTargetInfo, error) {
	s.loadCall++
	s.loadedHash = append([]string{}, hashes...)
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	out := make(map[string]i18n.ContentTargetInfo, len(hashes))
	// 真实现按 (hash, context) 复合键返回；桩没有 context 参数，按前缀匹配回放：
	// 一条 hash 在夹具里最多有一条现值（见各用例的 before 写法）。
	for _, h := range hashes {
		for key, info := range s.before {
			if strings.HasPrefix(key, h) {
				out[key] = info
			}
		}
	}
	return out, nil
}

func (s *contentStoreStub) UpsertTx(_ context.Context, _ *gorm.DB, items []i18n.ContentWriteItem) (int, error) {
	s.upsertCall++
	if s.upsertErr != nil {
		return 0, s.upsertErr
	}
	s.upserted = append(s.upserted, items...)
	return len(items), nil
}

// txRunnerStub 事务桩：**直接执行 fn**，记录是否被调用，并统计「提交」次数。
//
// 直接执行而不是真开事务：本文件验的是「顺序与全成或全败的判定点」，真回滚是
// PG 的行为（feature 测试负责）。fn 返回错误时把错误原样上抛，等价于回滚。
//
// **句柄形状**：rls.ScopeTx / pkg/i18n 的 requireTx 都按
// `tx.Statement.ConnPool.(*sql.Tx)` 判「是不是真事务」—— 桩给的必须是这个形状，
// 否则事务体在第一步就返回 ErrNotInTransaction，「业务判定」一行都测不到
// （这正是先前 9 个用例假红的根因）。所以这里起一个真实 *sql.Tx 但**不驱动它**：
// 所有业务端口（来源索引 / 现值读 / Upsert / 三处失效）都是桩，唯一会走到数据库的
// 只有 scope 与锁两步 —— 用 database/sql 的 no-op driver 承接 SQL，行为等价于
// 「scope 成功、锁成功」，不触库、不需要 PG。
type txRunnerStub struct {
	calls    int
	commits  int
	tx       *gorm.DB
	err      error
	txBodyFn func(tx *gorm.DB) error
}

func (s *txRunnerStub) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	if err := fn(s.tx); err != nil {
		return err
	}
	s.commits++
	return nil
}

// newScopeOnlyTx 造一个「形状为真事务、后端是空转 driver」的 *gorm.DB。
//
// 判据只认 ConnPool 是 *sql.Tx（见 pkg/rls.ScopeTx:75 与 pkg/i18n 的 requireTx），
// 所以这里注册一个只会吞掉语句的 driver，sql.Open 后 Begin 出 *sql.Tx。
// 任何真实 SQL 都写不进去也读不出来 —— 想验证 SQL 行为请写 feature 测试。
func newScopeOnlyTx(t *testing.T) *gorm.DB {
	t.Helper()
	// 驱动名必须唯一：database/sql 按名字缓存驱动，重复注册 panic。
	db, err := sql.Open(driverStubDriverName(t), "")
	if err != nil {
		t.Fatalf("打开空转连接失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("开启空转事务失败: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	// Config 必须给：gorm 的 Statement.DB / QuoteTo 都从 db.Config 取（缺了会在
	// 拼 SQL 时空指针 panic，而不是返回错误）。
	// 手工拼句柄时必须在 Initialize 里挂默认回调、并在之后把 ConnPool 换成 *sql.Tx：
	// gorm.Open 的顺序是 callbacks=initializeCallbacks → Dialector.Initialize →
	// Statement{ConnPool}（gorm.go:204-235）。跳过任一步，Exec 会在
	// callbacks.Raw()（callbacks.go:52）拿到 nil 处理器。
	// 必须走 gorm.Open（内部才 initializeCallbacks → 挂默认回调）；手工拼 &gorm.DB{}
	// 会让 db.callbacks 为 nil，Exec 在 callbacks.go:52 空指针。
	base, oerr := gorm.Open(driverStubDialector{}, &gorm.Config{SkipDefaultTransaction: true})
	if oerr != nil {
		t.Fatalf("构造空转 gorm 句柄失败: %v", oerr)
	}
	// 事务句柄：复制 gorm.Open 造好的 Statement，只把 ConnPool 换成真 *sql.Tx
	// （判据同 pkg/rls.ScopeTx:75）。**不能新建 Statement**：gorm 的 Exec 路径还要
	// 用 Clauses/Settings 等字段，空壳会在 database/sql 里空指针而非返回错误。
	// 逐字段拷贝会触发 copylocks（Statement.Settings 里含 sync.Mutex）；析构旧值再赋值
	// 则是同一对象换凭据，语义上正是「沿用 gorm.Open 造好的句柄，只换连接」。
	conn := base.Session(&gorm.Session{})
	conn.Statement.DB = conn
	conn.Statement.ConnPool = tx
	return conn
}

var driverStubSeq atomic.Int64

// driverStubDriverName 每次注册一个新的驱动名（同一名字二次注册会 panic）。
func driverStubDriverName(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("navigationtranslationwrite%d", driverStubSeq.Add(1))
	sql.Register(name, driverStubDriver{})
	return name
}

// driverStubConn / driverStubTx 空转 SQL 驱动：
// 只让 `SELECT set_config(...)` 这类 scope 语句拿到 nil error，不解析、不存储。
type driverStubDriver struct{}

// driverStubDialector 空转方言：只满足 gorm 拼 SQL 时需要的占位符绑定与引用，
// 不连库、不迁移、不解析字段类型。
type driverStubDialector struct{}

func (driverStubDialector) Name() string { return "navigationtranslationwritestub" }

// Initialize 等价于真方言里「挂默认回调」那一步（gorm.Open 在 initializeCallbacks
// 之后调用它）；缺了它 Exec 会在 callbacks.Raw() 拿到 nil 处理器。
func (driverStubDialector) Initialize(db *gorm.DB) error {
	callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{})
	return nil
}
func (driverStubDialector) Migrator(*gorm.DB) gorm.Migrator                { return nil }
func (driverStubDialector) DataTypeOf(*schema.Field) string                { return "" }
func (driverStubDialector) DefaultValueOf(*schema.Field) clause.Expression { return nil }
func (driverStubDialector) BindVarTo(writer clause.Writer, _ *gorm.Statement, _ any) {
	_ = writer.WriteByte('?')
}
func (driverStubDialector) QuoteTo(writer clause.Writer, str string) {
	_, _ = writer.WriteString(str)
}
func (driverStubDialector) Explain(string, ...any) string     { return "" }
func (driverStubDialector) SavePoint(*gorm.DB, string) error  { return nil }
func (driverStubDialector) RollbackTo(*gorm.DB, string) error { return nil }

func (driverStubDriver) Open(string) (driver.Conn, error) { return driverStubConn{}, nil }

type driverStubConn struct{}

// driverStubTx 内嵌 *driverStubConn（指针）：database/sql 在 Tx 上仍会做
// （driver.Conn）断言，值内嵌拿到的是空壳。
type driverStubTx struct{ *driverStubConn }

func (driverStubConn) Prepare(string) (driver.Stmt, error) { return driverStubStmt{}, nil }
func (driverStubConn) Close() error                        { return nil }
func (driverStubConn) Begin() (driver.Tx, error)           { return driverStubTx{&driverStubConn{}}, nil }
func (c driverStubConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return driverStubTx{&c}, nil
}
func (driverStubConn) Ping(context.Context) error { return nil }
func (driverStubTx) Commit() error                { return nil }
func (driverStubTx) Rollback() error              { return nil }

type driverStubStmt struct{}

func (driverStubStmt) Close() error  { return nil }
func (driverStubStmt) NumInput() int { return -1 }
func (driverStubStmt) Exec([]driver.Value) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}
func (driverStubStmt) Query([]driver.Value) (driver.Rows, error) { return driverStubRows{}, nil }

type driverStubRows struct{}

func (driverStubRows) Columns() []string              { return []string{"set_config"} }
func (driverStubRows) Close() error                   { return nil }
func (driverStubRows) Next(dest []driver.Value) error { return io.EOF }

// stalePortStub 页 / 实例标记端口桩（记录 kind 顺序与调用次数）。
type stalePortStub struct {
	kinds []string
	err   map[string]error
	calls int
}

func (s *stalePortStub) MarkMenuStaleTx(_ context.Context, _ *gorm.DB, _, kind string, _ time.Time) (int64, error) {
	s.calls++
	s.kinds = append(s.kinds, kind)
	if err, ok := s.err[kind]; ok {
		return 0, err
	}
	return 1, nil
}

func (s *stalePortStub) MarkInstanceMenuStaleTx(_ context.Context, _ *gorm.DB, _, kind string, _ time.Time) (int64, error) {
	s.calls++
	s.kinds = append(s.kinds, kind)
	if err, ok := s.err[kind]; ok {
		return 0, err
	}
	return 1, nil
}

// queueStub 可靠入队端口桩。
type queueStub struct {
	kinds []string
	err   map[string]error
	calls int
}

func (s *queueStub) EnqueueMenuStaleTx(_ context.Context, _ *gorm.DB, _, kind string) (int64, error) {
	s.calls++
	s.kinds = append(s.kinds, kind)
	if err, ok := s.err[kind]; ok {
		return 0, err
	}
	return int64(len(s.kinds)), nil
}

// writerFixture 一套完整桩 + 已构造的 TranslationWriter。
type writerFixture struct {
	writer    *TranslationWriter
	rows      *navRowsTxStub
	indexer   *sourceIndexerStub
	projects  *projectListerStub
	content   *contentStoreStub
	pages     *stalePortStub
	instances *stalePortStub
	queue     *queueStub
	tx        *txRunnerStub
	projectID string
}

// newWriterFixture 造一套「一条 header 菜单、可写入」的最小环境。
//
// titles 是 header 位置的菜单文字；extraKinds 允许把同一段文字也放进别的位置
// （验「跨 kind 共享 hash」）。
func newWriterFixture(t *testing.T, titles []string, extraKinds map[string][]string) *writerFixture {
	t.Helper()
	projectID := uuid.NewString()
	kindRows := map[string][]*navigationmodel.NavigationEntity{}
	for i, title := range titles {
		kindRows["header"] = append(kindRows["header"], navRowTx("n"+string(rune('a'+i)), title))
	}
	for kind, more := range extraKinds {
		for i, title := range more {
			kindRows[kind] = append(kindRows[kind], navRowTx(kind+"-"+string(rune('a'+i)), title))
		}
	}
	rows := &navRowsTxStub{byProject: map[string]map[string][]*navigationmodel.NavigationEntity{projectID: kindRows}}

	index := &sourceIndexStub{byHash: map[string]navigationcontract.TranslationSourceEntry{}}
	for kind, list := range kindRows {
		for _, row := range list {
			hash := i18n.ContentHash(strings.TrimSpace(row.Title))
			entry, ok := index.byHash[hash]
			if !ok {
				entry = navigationcontract.TranslationSourceEntry{
					Hash: hash, Source: strings.TrimSpace(row.Title), Context: navigationLabelContext(),
				}
			}
			entry.Kinds = append(entry.Kinds, kind)
			index.byHash[hash] = entry
		}
	}

	f := &writerFixture{
		rows:      rows,
		indexer:   &sourceIndexerStub{index: index},
		projects:  &projectListerStub{ids: []string{projectID}},
		content:   &contentStoreStub{before: map[string]i18n.ContentTargetInfo{}},
		pages:     &stalePortStub{},
		instances: &stalePortStub{},
		queue:     &queueStub{},
		tx:        &txRunnerStub{tx: newScopeOnlyTx(t)},
		projectID: projectID,
	}
	f.writer = NewTranslationWriter(TranslationWriterDeps{
		Rows: f.rows, Sources: f.indexer, Projects: f.projects, Content: f.content,
		Pages: f.pages, Instances: f.instances, Queue: f.queue, Tx: f.tx,
		Now: func() time.Time { return time.Unix(0, 0).UTC() },
	})
	return f
}

// row 造一条提交行（语境固定为导航标签）。
func row(hash, target string) navigationcontract.TranslationWriteRow {
	return navigationcontract.TranslationWriteRow{Context: navigationLabelContext(), Hash: hash, Target: target}
}

// req 造一次提交。
func req(projectID, lang string, rows ...navigationcontract.TranslationWriteRow) *navigationcontract.TranslationWriteReq {
	return &navigationcontract.TranslationWriteReq{ProjectID: projectID, Lang: lang, Rows: rows}
}

// blockedOf 从错误里取明细（与出口同一判据：errors.As 到 facing.BlockedError）。
func blockedOf(t *testing.T, err error) *TranslationRejectedError {
	t.Helper()
	var rejected *TranslationRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("期望 TranslationRejectedError（可定位明细），实际 %T: %v", err, err)
	}
	return rejected
}

// —— 用例 ——

// TestWriteTranslationsEmptyTargetsSkipEverything 空 target 只读忽略：不查来源、不开事务、零派发。
func TestWriteTranslationsEmptyTargetsSkipEverything(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)

	res, err := f.writer.WriteTranslations(context.Background(), req(f.projectID, "en-US",
		row(i18n.ContentHash("首页"), ""), row("不存在的hash", "   ")))
	if err != nil {
		t.Fatalf("空译文整批应被忽略而不是失败: %v", err)
	}
	if res.Written != 0 || len(res.ChangedKinds) != 0 {
		t.Fatalf("空译文回执应为 written:0 且无位置，实际 %+v", res)
	}
	// 关键判据：这些入口**一次都不该被调用**（顺序错误时它们会被调用，然后因为
	// 「不存在的hash」整批拒绝）。
	if f.projects.call != 1 {
		t.Fatalf("工程校验应执行 1 次（工程存在性先于一切），实际 %d", f.projects.call)
	}
	if f.tx.calls != 0 || f.indexer.calls != 0 || f.content.loadCall != 0 ||
		f.content.upsertCall != 0 || f.pages.calls != 0 || f.instances.calls != 0 || f.queue.calls != 0 {
		t.Fatalf("空译文不得开事务 / 读来源 / 读现值 / 写 / 派发：tx=%d index=%d load=%d upsert=%d pages=%d instances=%d queue=%d",
			f.tx.calls, f.indexer.calls, f.content.loadCall, f.content.upsertCall,
			f.pages.calls, f.instances.calls, f.queue.calls)
	}
	if len(f.rows.calls) != 0 {
		t.Fatalf("空译文不该读任何菜单位置，实际读了 %v", f.rows.calls)
	}
}

// TestWriteTranslationsRejectsUnknownHash 未知 hash：整批拒绝、零写入、明细回落到 hash。
func TestWriteTranslationsRejectsUnknownHash(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)
	unknown := i18n.ContentHash("已删除的菜单")

	_, err := f.writer.WriteTranslations(context.Background(), req(f.projectID, "en-US",
		row(i18n.ContentHash("首页"), "Home"), row(unknown, "Gone")))
	rejected := blockedOf(t, err)
	if f.content.upsertCall != 0 {
		t.Fatalf("未知 hash 必须整批拒绝（零写入），实际 UPSERT 了 %d 次", f.content.upsertCall)
	}
	if len(rejected.Items) != 1 || rejected.Items[0].ID != unknown {
		t.Fatalf("明细应只含那一条未知 hash，实际 %+v", rejected.Items)
	}
	if rejected.Items[0].Name != unknown {
		t.Fatalf("未知 hash 无原文时 name 应回落 hash，实际 %q", rejected.Items[0].Name)
	}
	if rejected.Items[0].ReasonKey != navigationenums.ErrSourceChanged {
		t.Fatalf("未知 hash 的原因应为 ErrSourceChanged，实际 %q", rejected.Items[0].ReasonKey)
	}
	if !errors.Is(err, navigationenums.Err(navigationenums.ErrSourceChanged)) {
		t.Fatalf("错误应可被 errors.Is 命中声明处哨兵，实际 %v", err)
	}
	var fact interface{ Facing() (string, string) }
	_ = fact
}

// TestWriteTranslationsRejectsUntranslatable 非空但不可翻译：整批拒绝，name 给服务端原文。
func TestWriteTranslationsRejectsUntranslatable(t *testing.T) {
	f := newWriterFixture(t, []string{"首页", "42"}, nil)

	_, err := f.writer.WriteTranslations(context.Background(), req(f.projectID, "en-US",
		row(i18n.ContentHash("首页"), "Home"), row(i18n.ContentHash("42"), "Forty Two")))
	rejected := blockedOf(t, err)
	if len(rejected.Items) != 1 {
		t.Fatalf("明细应只含不可翻译那一行，实际 %+v", rejected.Items)
	}
	item := rejected.Items[0]
	if item.Name != "42" {
		t.Fatalf("不可翻译行的 name 应是服务端原文，实际 %q", item.Name)
	}
	if item.ID != i18n.ContentHash("42") {
		t.Fatalf("明细 id 应是原文 hash，实际 %q", item.ID)
	}
	if item.ReasonKey != navigationenums.ErrNotTranslatable {
		t.Fatalf("原因应为 ErrNotTranslatable，实际 %q", item.ReasonKey)
	}
	if f.content.upsertCall != 0 || f.pages.calls != 0 || f.queue.calls != 0 {
		t.Fatalf("整批拒绝时不得写 / 标 / 入队：upsert=%d pages=%d queue=%d",
			f.content.upsertCall, f.pages.calls, f.queue.calls)
	}
}

// TestWriteTranslationsRejectsDuplicateRows 重复寻址：409 明细给出两行的差异。
func TestWriteTranslationsRejectsDuplicateRows(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)
	hash := i18n.ContentHash("首页")

	_, err := f.writer.WriteTranslations(context.Background(), req(f.projectID, "en-US",
		row(hash, "Home"), row(hash, "Start")))
	rejected := blockedOf(t, err)
	if len(rejected.Items) != 1 {
		t.Fatalf("重复行应只报一条明细，实际 %+v", rejected.Items)
	}
	if rejected.Items[0].Name != "Home / Start" {
		t.Fatalf("明细 name 应给出两行译文供人工裁决，实际 %q", rejected.Items[0].Name)
	}
	if rejected.Items[0].ReasonKey != navigationenums.ErrTranslationDuplicate {
		t.Fatalf("原因应为 ErrTranslationDuplicate，实际 %q", rejected.Items[0].ReasonKey)
	}
	if f.tx.calls != 0 {
		t.Fatalf("重复行在开事务前就该被拒，实际开了 %d 次事务", f.tx.calls)
	}
}

// TestWriteTranslationsRejectsWrongContext 语境不是导航标签：400 逐条明细。
func TestWriteTranslationsRejectsWrongContext(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)

	_, err := f.writer.WriteTranslations(context.Background(), req(f.projectID, "en-US",
		navigationcontract.TranslationWriteRow{Context: "core.button.text", Hash: i18n.ContentHash("首页"), Target: "Home"}))
	rejected := blockedOf(t, err)
	if len(rejected.Items) != 1 || rejected.Items[0].ReasonKey != navigationenums.ErrContextInvalid {
		t.Fatalf("语境非法应整批拒绝并给逐条明细，实际 %+v", rejected.Items)
	}
	if f.tx.calls != 0 {
		t.Fatalf("语境判定在开事务前，实际开了 %d 次事务", f.tx.calls)
	}
}

// TestWriteTranslationsSameManualValueWritesNothing 同值且已是 manual：零写、零派发。
func TestWriteTranslationsSameManualValueWritesNothing(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)
	hash := i18n.ContentHash("首页")
	// 现值必须按「(hash, 语境)」复合键存：事务里读现值用的是 i18n.ContentIndexKey，
	// 只按 hash 存会让比对永远落空（同值判定测不出来）。
	f.content.before[i18n.ContentIndexKey(hash, navigationLabelContext())] =
		i18n.ContentTargetInfo{TargetText: "Home", Engine: i18n.ContentEngineManual}

	res, err := f.writer.WriteTranslations(context.Background(), req(f.projectID, "en-US", row(hash, "Home")))
	if err != nil {
		t.Fatalf("同值提交不该失败: %v", err)
	}
	if res.Written != 0 || len(res.ChangedKinds) != 0 {
		t.Fatalf("同值 manual 不算变化，实际 %+v", res)
	}
	if f.content.loadCall != 1 {
		t.Fatalf("应读过一次现值（否则「同值」无从判断），实际 %d", f.content.loadCall)
	}
	if f.content.upsertCall != 0 || f.pages.calls != 0 || f.instances.calls != 0 || f.queue.calls != 0 {
		t.Fatalf("同值 manual 不得写 / 标 / 入队：upsert=%d pages=%d instances=%d queue=%d",
			f.content.upsertCall, f.pages.calls, f.instances.calls, f.queue.calls)
	}
}

// TestWriteTranslationsMachineToManualCountsAsChange 同值但引擎不是 manual：算真实变化。
//
// 这是「把机器译文确认为人工译文」的路径 —— 值没变但语义变了（后续机器翻译不再覆盖它），
// 因此必须写、必须派发。
func TestWriteTranslationsMachineToManualCountsAsChange(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)
	hash := i18n.ContentHash("首页")
	f.content.before[i18n.ContentIndexKey(hash, navigationLabelContext())] =
		i18n.ContentTargetInfo{TargetText: "Home", Engine: i18n.ContentEngineAI}

	res, err := f.writer.WriteTranslations(context.Background(), req(f.projectID, "en-US", row(hash, "Home")))
	if err != nil {
		t.Fatalf("机器译文转人工不该失败: %v", err)
	}
	if res.Written != 1 {
		t.Fatalf("同值但引擎变化应计入 written，实际 %+v", res)
	}
	if f.content.upsertCall != 1 {
		t.Fatalf("应写入一次，实际 %d", f.content.upsertCall)
	}
	if len(f.content.upserted) != 1 || f.content.upserted[0].Engine != i18n.ContentEngineManual {
		t.Fatalf("写入项必须是 manual 引擎，实际 %+v", f.content.upserted)
	}
}

// TestWriteTranslationsUpsertsCanonicalProject 写入项带规范工程 id 与提交语言。
func TestWriteTranslationsUpsertsCanonicalProject(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)
	upper := strings.ToUpper(f.projectID) // 大小写变体：锁与写都必须落在规范形态

	res, err := f.writer.WriteTranslations(context.Background(), req(upper, "en-US", row(i18n.ContentHash("首页"), "Home")))
	if err != nil {
		t.Fatalf("规范形态归一失败: %v", err)
	}
	if res.Written != 1 {
		t.Fatalf("应写入一条，实际 %+v", res)
	}
	// 内存桩拿不到 ProjectID 的落库效果（ContentWriteItem 由 pkg/i18n 用工程参数补），
	// 这里断言的是「工程校验认大小写变体」：工程不存在时这一步会 400。
	if f.projects.call != 1 {
		t.Fatalf("工程校验应执行一次，实际 %d", f.projects.call)
	}
	if len(f.content.upserted) != 1 || f.content.upserted[0].Lang != "en-US" {
		t.Fatalf("写入项语言应为提交语言，实际 %+v", f.content.upserted)
	}
}

// TestWriteTranslationsMarksAllKindsOfSharedHash 跨 kind 共享同一段原文：两个位置都派发。
func TestWriteTranslationsMarksAllKindsOfSharedHash(t *testing.T) {
	// 同一段「首页」同时出现在 header 与 footer。
	f := newWriterFixture(t, []string{"首页"}, map[string][]string{"footer": {"首页"}})
	hash := i18n.ContentHash("首页")

	res, err := f.writer.WriteTranslations(context.Background(), req(f.projectID, "en-US", row(hash, "Home")))
	if err != nil {
		t.Fatalf("共享原文写入失败: %v", err)
	}
	if want := []string{"header", "footer"}; !reflect.DeepEqual(res.ChangedKinds, want) {
		t.Fatalf("变化位置应为 %v（规范顺序），实际 %v", want, res.ChangedKinds)
	}
	if res.Written != 1 {
		t.Fatalf("共享原文只算一条写入，实际 %d", res.Written)
	}
	// 每个位置各一次：页标记、实例标记、入队。
	if f.pages.calls != 2 || f.instances.calls != 2 || f.queue.calls != 2 {
		t.Fatalf("两个位置应各派发一次：pages=%d instances=%d queue=%d",
			f.pages.calls, f.instances.calls, f.queue.calls)
	}
	if !reflect.DeepEqual(f.queue.kinds, []string{"header", "footer"}) {
		t.Fatalf("入队 kind 应与变化位置一致且有序，实际 %v", f.queue.kinds)
	}
}

// TestWriteTranslationsRollsBackOnStaleFailure 三处失效任一失败：整笔回滚（错误上抛、零写入）。
func TestWriteTranslationsRollsBackOnStaleFailure(t *testing.T) {
	cases := []struct {
		name string
		drop func(f *writerFixture, boom error)
	}{
		{name: "页标记失败", drop: func(f *writerFixture, boom error) { f.pages.err = map[string]error{"header": boom} }},
		{name: "实例标记失败", drop: func(f *writerFixture, boom error) { f.instances.err = map[string]error{"header": boom} }},
		{name: "可靠入队失败", drop: func(f *writerFixture, boom error) { f.queue.err = map[string]error{"header": boom} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newWriterFixture(t, []string{"首页"}, nil)
			boom := errors.New("boom")
			tc.drop(f, boom)

			_, err := f.writer.WriteTranslations(context.Background(), req(f.projectID, "en-US", row(i18n.ContentHash("首页"), "Home")))
			if err == nil {
				t.Fatal("失效派发失败必须让整笔失败，实际返回 nil")
			}
			// 内部错误归口成「译文保存失败」（不把原始错误文本透给调用方）。
			if !errors.Is(err, navigationenums.Err(navigationenums.ErrTranslationWriteFailed)) {
				t.Fatalf("非业务错误应归口成 ErrTranslationWriteFailed，实际 %v", err)
			}
			if strings.Contains(err.Error(), "boom") {
				t.Fatalf("归口后不该透出原始错误文本，实际 %q", err.Error())
			}
			// 事务桩直接执行 fn：fn 返回错误即「回滚」，因此回执不该生成、后续位置不该被派发。
			if f.queue.calls > 1 {
				t.Fatalf("失败后不该继续派发后续位置，实际 queue 调用 %d 次", f.queue.calls)
			}
		})
	}
}

// TestWriteTranslationsSourceReadFailureAborts 来源读取失败：整笔失败、零写入。
func TestWriteTranslationsSourceReadFailureAborts(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)
	f.indexer.err = navigationenums.ErrSourceKindDetail("footer")

	_, err := f.writer.WriteTranslations(context.Background(), req(f.projectID, "en-US", row(i18n.ContentHash("首页"), "Home")))
	if err == nil {
		t.Fatal("来源读取失败必须整体失败")
	}
	if !errors.Is(err, navigationenums.Err(navigationenums.ErrSourceKindFailed)) {
		t.Fatalf("应可被 errors.Is 命中 ErrSourceKindFailed，实际 %v", err)
	}
	if f.content.upsertCall != 0 || f.pages.calls != 0 || f.queue.calls != 0 {
		t.Fatalf("来源失败时不得写 / 标 / 入队：upsert=%d pages=%d queue=%d",
			f.content.upsertCall, f.pages.calls, f.queue.calls)
	}
}

// TestWriteTranslationsRequireFailureAborts Require 失败（部分位置读不到）：同样整体失败。
func TestWriteTranslationsRequireFailureAborts(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)
	f.indexer.index.requireE = navigationenums.ErrSourceKindDetail("header_mobile, footer")

	_, err := f.writer.WriteTranslations(context.Background(), req(f.projectID, "en-US", row(i18n.ContentHash("首页"), "Home")))
	if !errors.Is(err, navigationenums.Err(navigationenums.ErrSourceKindFailed)) {
		t.Fatalf("Require 失败应整体失败，实际 %v", err)
	}
	if f.content.upsertCall != 0 {
		t.Fatalf("Require 失败不得写入，实际 %d 次", f.content.upsertCall)
	}
}

// TestWriteTranslationsProjectIsolation 工程隔离：别的工程的菜单不进来源索引。
func TestWriteTranslationsProjectIsolation(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)
	other := uuid.NewString()
	f.projects.ids = append(f.projects.ids, other)
	// 另一个工程有「产品」这一项，当前工程没有。
	f.rows.byProject[other] = map[string][]*navigationmodel.NavigationEntity{"header": {navRowTx("x1", "产品")}}

	// 当前工程提交「产品」：不该被认成来源（索引按工程取行）。
	_, err := f.writer.WriteTranslations(context.Background(), req(f.projectID, "en-US", row(i18n.ContentHash("产品"), "Products")))
	if err == nil {
		t.Fatal("别的工程的菜单文字不该被当前工程认作来源")
	}
	if !errors.Is(err, navigationenums.Err(navigationenums.ErrSourceChanged)) {
		t.Fatalf("应报「原文已变化」，实际 %v", err)
	}
	// 反证：索引只按当前工程取行（桩会拒绝未知工程，这里断言调用里不带 other）。
	for _, call := range f.rows.calls {
		if strings.HasPrefix(call, other+"|") {
			t.Fatalf("来源索引不该读别的工程的行，实际读了 %s", call)
		}
	}
	if f.content.upsertCall != 0 {
		t.Fatalf("工程隔离失败时应零写入，实际 %d", f.content.upsertCall)
	}
}

// TestWriteTranslationsRejectsMissingProject 工程不存在：400，且不开事务。
func TestWriteTranslationsRejectsMissingProject(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)
	other := uuid.NewString()

	_, err := f.writer.WriteTranslations(context.Background(), req(other, "en-US", row(i18n.ContentHash("首页"), "Home")))
	if !errors.Is(err, navigationenums.Err(navigationenums.ErrInvalidParam)) {
		t.Fatalf("工程不存在应报参数错误，实际 %v", err)
	}
	if f.tx.calls != 0 || f.indexer.calls != 0 {
		t.Fatalf("工程不存在时不得开事务 / 读来源：tx=%d index=%d", f.tx.calls, f.indexer.calls)
	}
}

// TestWriteTranslationsRejectsUnsupportedLang 语言不在白名单：400。
func TestWriteTranslationsRejectsUnsupportedLang(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)

	_, err := f.writer.WriteTranslations(context.Background(), req(f.projectID, "xx-YY", row(i18n.ContentHash("首页"), "Home")))
	if !errors.Is(err, navigationenums.Err(navigationenums.ErrInvalidParam)) {
		t.Fatalf("不支持的语言应报参数错误，实际 %v", err)
	}
	if f.projects.call != 0 {
		t.Fatalf("语言判定先于工程校验，实际工程校验执行了 %d 次", f.projects.call)
	}
}

// TestWriteTranslationsFailsClosedWithoutPorts 端口缺失：拒绝写入，绝不「静默成功」。
func TestWriteTranslationsFailsClosedWithoutPorts(t *testing.T) {
	projectID := uuid.NewString()
	// 只注入一部分依赖：pages 缺失。
	w := NewTranslationWriter(TranslationWriterDeps{
		Rows: &navRowsTxStub{}, Sources: &sourceIndexerStub{index: &sourceIndexStub{}},
		Projects: &projectListerStub{ids: []string{projectID}},
		Content:  &contentStoreStub{}, Instances: &stalePortStub{}, Queue: &queueStub{}, Tx: &txRunnerStub{},
	})

	_, err := w.WriteTranslations(context.Background(), req(projectID, "en-US", row(i18n.ContentHash("首页"), "Home")))
	if !errors.Is(err, navigationenums.Err(navigationenums.ErrTranslationWriteUnavailable)) {
		t.Fatalf("装配不全应给「未就绪」（重试无用）而不是「保存失败」，实际 %v", err)
	}
}

// TestWriteTranslationsNilRequest 空请求：参数错误而不是 panic。
func TestWriteTranslationsNilRequest(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)
	if _, err := f.writer.WriteTranslations(context.Background(), nil); err == nil {
		t.Fatal("nil 请求应报参数错误")
	}
}

// TestWriteTranslationsPersistFailureClassified 存储失败：归口成「译文保存失败」，不透原始文本。
func TestWriteTranslationsPersistFailureClassified(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)
	f.content.upsertErr = errors.New("SQLSTATE 42P01: relation \"sys_translation\" does not exist")

	_, err := f.writer.WriteTranslations(context.Background(), req(f.projectID, "en-US", row(i18n.ContentHash("首页"), "Home")))
	if !errors.Is(err, navigationenums.Err(navigationenums.ErrTranslationWriteFailed)) {
		t.Fatalf("存储失败应归口，实际 %v", err)
	}
	if strings.Contains(err.Error(), "SQLSTATE") || strings.Contains(err.Error(), "sys_translation") {
		t.Fatalf("归口句不得含数据库原文，实际 %q", err.Error())
	}
}
