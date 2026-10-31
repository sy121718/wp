package navigationservice

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

func navRowTx(id, title string) *navigationmodel.NavigationEntity {
	return &navigationmodel.NavigationEntity{ID: id, Title: title, Path: "/p/" + id}
}

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

type contentStoreStub struct {
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

func newScopeOnlyTx(t *testing.T) *gorm.DB {
	t.Helper()

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

	base, oerr := gorm.Open(driverStubDialector{}, &gorm.Config{SkipDefaultTransaction: true})
	if oerr != nil {
		t.Fatalf("构造空转 gorm 句柄失败: %v", oerr)
	}

	conn := base.Session(&gorm.Session{})
	conn.Statement.DB = conn
	conn.Statement.ConnPool = tx
	return conn
}

var driverStubSeq atomic.Int64

func driverStubDriverName(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("navigationtranslationwrite%d", driverStubSeq.Add(1))
	sql.Register(name, driverStubDriver{})
	return name
}

type driverStubDriver struct{}

type driverStubDialector struct{}

func (driverStubDialector) Name() string { return "navigationtranslationwritestub" }

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

func row(hash, target string) navigationcontract.TranslationWriteRow {
	return navigationcontract.TranslationWriteRow{Context: navigationLabelContext(), Hash: hash, Target: target}
}

func req(projectID, lang string, rows ...navigationcontract.TranslationWriteRow) *navigationcontract.TranslationWriteReq {
	return &navigationcontract.TranslationWriteReq{ProjectID: projectID, Lang: lang, Rows: rows}
}

func blockedOf(t *testing.T, err error) *TranslationRejectedError {
	t.Helper()
	var rejected *TranslationRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("期望 TranslationRejectedError（可定位明细），实际 %T: %v", err, err)
	}
	return rejected
}

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

func TestWriteTranslationsSameManualValueWritesNothing(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)
	hash := i18n.ContentHash("首页")

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

func TestWriteTranslationsUpsertsCanonicalProject(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)
	upper := strings.ToUpper(f.projectID)

	res, err := f.writer.WriteTranslations(context.Background(), req(upper, "en-US", row(i18n.ContentHash("首页"), "Home")))
	if err != nil {
		t.Fatalf("规范形态归一失败: %v", err)
	}
	if res.Written != 1 {
		t.Fatalf("应写入一条，实际 %+v", res)
	}

	if f.projects.call != 1 {
		t.Fatalf("工程校验应执行一次，实际 %d", f.projects.call)
	}
	if len(f.content.upserted) != 1 || f.content.upserted[0].Lang != "en-US" {
		t.Fatalf("写入项语言应为提交语言，实际 %+v", f.content.upserted)
	}
}

func TestWriteTranslationsMarksAllKindsOfSharedHash(t *testing.T) {

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

	if f.pages.calls != 2 || f.instances.calls != 2 || f.queue.calls != 2 {
		t.Fatalf("两个位置应各派发一次：pages=%d instances=%d queue=%d",
			f.pages.calls, f.instances.calls, f.queue.calls)
	}
	if !reflect.DeepEqual(f.queue.kinds, []string{"header", "footer"}) {
		t.Fatalf("入队 kind 应与变化位置一致且有序，实际 %v", f.queue.kinds)
	}
}

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

			if !errors.Is(err, navigationenums.Err(navigationenums.ErrTranslationWriteFailed)) {
				t.Fatalf("非业务错误应归口成 ErrTranslationWriteFailed，实际 %v", err)
			}
			if strings.Contains(err.Error(), "boom") {
				t.Fatalf("归口后不该透出原始错误文本，实际 %q", err.Error())
			}

			if f.queue.calls > 1 {
				t.Fatalf("失败后不该继续派发后续位置，实际 queue 调用 %d 次", f.queue.calls)
			}
		})
	}
}

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

func TestWriteTranslationsProjectIsolation(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)
	other := uuid.NewString()
	f.projects.ids = append(f.projects.ids, other)

	f.rows.byProject[other] = map[string][]*navigationmodel.NavigationEntity{"header": {navRowTx("x1", "产品")}}

	_, err := f.writer.WriteTranslations(context.Background(), req(f.projectID, "en-US", row(i18n.ContentHash("产品"), "Products")))
	if err == nil {
		t.Fatal("别的工程的菜单文字不该被当前工程认作来源")
	}
	if !errors.Is(err, navigationenums.Err(navigationenums.ErrSourceChanged)) {
		t.Fatalf("应报「原文已变化」，实际 %v", err)
	}

	for _, call := range f.rows.calls {
		if strings.HasPrefix(call, other+"|") {
			t.Fatalf("来源索引不该读别的工程的行，实际读了 %s", call)
		}
	}
	if f.content.upsertCall != 0 {
		t.Fatalf("工程隔离失败时应零写入，实际 %d", f.content.upsertCall)
	}
}

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

func TestWriteTranslationsFailsClosedWithoutPorts(t *testing.T) {
	projectID := uuid.NewString()

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

func TestWriteTranslationsNilRequest(t *testing.T) {
	f := newWriterFixture(t, []string{"首页"}, nil)
	if _, err := f.writer.WriteTranslations(context.Background(), nil); err == nil {
		t.Fatal("nil 请求应报参数错误")
	}
}

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
