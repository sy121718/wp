package feature

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

	"gorm.io/gorm"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/public/test/support"
)

type navStaleProbe struct {
	mu      sync.Mutex
	calls   []string
	failAt  int
	failMsg string
}

func (p *navStaleProbe) record(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, name)
	if p.failAt > 0 && len(p.calls) == p.failAt {
		return errors.New(p.failMsg)
	}
	return nil
}

func (p *navStaleProbe) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = nil
}

func (p *navStaleProbe) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string{}, p.calls...)
}

type navProbePage struct{ p *navStaleProbe }

func (s navProbePage) MarkMenuStaleTx(_ context.Context, _ *gorm.DB, _, kind string, _ time.Time) (int64, error) {
	if err := s.p.record("page:" + kind); err != nil {
		return 0, err
	}
	return 1, nil
}

type navProbeInstances struct{ p *navStaleProbe }

func (s navProbeInstances) MarkInstanceMenuStaleTx(_ context.Context, _ *gorm.DB, _, kind string, _ time.Time) (int64, error) {
	if err := s.p.record("instance:" + kind); err != nil {
		return 0, err
	}
	return 1, nil
}

type navProbeQueue struct{ p *navStaleProbe }

func (s navProbeQueue) EnqueueMenuStaleTx(_ context.Context, _ *gorm.DB, _, kind string) (int64, error) {
	if err := s.p.record("queue:" + kind); err != nil {
		return 0, err
	}
	return 1, nil
}

type navProbeItem struct{ p *navStaleProbe }

func (s navProbeItem) MarkNavigationStaleTx(_ context.Context, _ *gorm.DB, _, navigationID string) (int64, error) {
	if err := s.p.record("item:" + navigationID); err != nil {
		return 0, err
	}
	return 1, nil
}

type navProbeItemInstances struct{ p *navStaleProbe }

func (s navProbeItemInstances) MarkInstanceNavigationStaleTx(_ context.Context, _ *gorm.DB, _, navigationID string, _ time.Time) (int64, error) {
	if err := s.p.record("iteminstance:" + navigationID); err != nil {
		return 0, err
	}
	return 1, nil
}

var (
	_ navigationcontract.PageStalePort                 = navProbePage{}
	_ navigationcontract.PresentationStalePort         = navProbeInstances{}
	_ navigationcontract.MenuStaleEnqueuePort          = navProbeQueue{}
	_ navigationcontract.MenuItemStalePort             = navProbeItem{}
	_ navigationcontract.MenuItemPresentationStalePort = navProbeItemInstances{}
)

type navWriteEnv struct {
	db        *gorm.DB
	projectID string
	otherID   string
	navSvc    *navigationservice.Service
	writer    *navigationservice.TranslationWriter
	probe     *navStaleProbe
	content   *i18n.ContentWriter
}

const navWriteLang = "en-US"

func newNavWriteEnv(t *testing.T) *navWriteEnv {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project := mustCreateProject(t, projects, "译文写入工程")
	other := mustCreateProject(t, projects, "译文写入隔离工程")
	return newNavWriteEnvWith(t, db, projects, project, other)
}

func mustCreateProject(t *testing.T, projects projectcontract.ProjectService, name string) string {
	t.Helper()
	p, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: name})
	if err != nil {
		t.Fatalf("创建工程 %s 失败: %v", name, err)
	}
	return p.ID
}

func newNavWriteEnvWith(t *testing.T, db *gorm.DB, projects projectcontract.ProjectService, projectID, otherID string) *navWriteEnv {
	t.Helper()
	navModel := navigationmodel.NewModel(db)
	navSvc := navigationservice.NewService(navModel, projects)
	content := i18n.NewContentWriter(db)
	probe := &navStaleProbe{}

	navSvc.SetWritePorts(navigationservice.NavigationWriteDeps{
		Pages:         navProbePage{p: probe},
		Instances:     navProbeInstances{p: probe},
		Items:         navProbeItem{p: probe},
		ItemInstances: navProbeItemInstances{p: probe},
		Queue:         navProbeQueue{p: probe},
	})
	writer := navigationservice.NewTranslationWriter(navigationservice.TranslationWriterDeps{
		Rows:    navigationservice.NewNavigationRowsTx(navModel),
		Sources: navSvc,

		Projects: navigationservice.ProjectListerFunc(func(ctx context.Context) ([]navigationservice.ProjectEntry, error) {
			list, err := projects.List(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]navigationservice.ProjectEntry, 0, len(list))
			for _, p := range list {
				out = append(out, navigationservice.ProjectEntry{ID: p.ID})
			}
			return out, nil
		}),
		Content:   content,
		Pages:     navProbePage{p: probe},
		Instances: navProbeInstances{p: probe},
		Queue:     navProbeQueue{p: probe},
		Tx:        navModel,
	})
	return &navWriteEnv{db: db, projectID: projectID, otherID: otherID, navSvc: navSvc, writer: writer, probe: probe, content: content}
}

func navAddItem(t *testing.T, svc *navigationservice.Service, projectID, title, kind string) string {
	t.Helper()
	if _, err := svc.Create(context.Background(), &navigationdto.CreateReq{
		ProjectID: projectID, Title: title, Path: "/" + strings.TrimSpace(title), Kind: kind,
	}); err != nil {
		t.Fatalf("创建导航项 %s(%s) 失败: %v", title, kind, err)
	}
	return i18n.ContentHash(title)
}

func navRows(rows ...[2]string) []navigationcontract.TranslationWriteRow {
	out := make([]navigationcontract.TranslationWriteRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, navigationcontract.TranslationWriteRow{Context: "navigation.label", Hash: r[0], Target: r[1]})
	}
	return out
}

func navWrite(t *testing.T, w *navigationservice.TranslationWriter, projectID, lang string, rows []navigationcontract.TranslationWriteRow) (*navigationcontract.TranslationWriteResult, error) {
	t.Helper()
	return w.WriteTranslations(context.Background(), &navigationcontract.TranslationWriteReq{
		ProjectID: projectID, Lang: lang, Rows: rows,
	})
}

func navStoredTarget(t *testing.T, content *i18n.ContentWriter, projectID, lang, hash string) (string, string) {
	t.Helper()
	info, err := content.LoadDetailsForProject(context.Background(), projectID, lang, []string{hash})
	if err != nil {
		t.Fatalf("读回译文失败: %v", err)
	}
	v, ok := info[i18n.ContentIndexKey(hash, "navigation.label")]
	if !ok {
		return "", ""
	}
	return v.TargetText, v.Engine
}

func navCountTranslations(t *testing.T, db *gorm.DB, projectID, hash string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(
		"SELECT count(*) FROM sys_translation WHERE project_id = ? AND source_hash = ? AND context = ?",
		projectID, hash, "navigation.label").Scan(&n).Error; err != nil {
		t.Fatalf("统计译文行失败: %v", err)
	}
	return n
}

func TestNavTranslationWritePersistsAndDispatches(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")
	env.probe.reset()

	res, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{hash, "Home"}))
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if res.Written != 1 {
		t.Fatalf("写入条数不符: %+v", res)
	}
	if got := strings.Join(res.ChangedKinds, ","); got != "header" {
		t.Fatalf("受影响位置不符: %q", got)
	}

	if target, engine := navStoredTarget(t, env.content, env.projectID, navWriteLang, hash); target != "Home" || engine != i18n.ContentEngineManual {
		t.Fatalf("落库内容不符: target=%q engine=%q", target, engine)
	}

	if got := strings.Join(env.probe.snapshot(), ","); got != "page:header,instance:header,queue:header" {
		t.Fatalf("派发顺序不符: %q", got)
	}
}

func TestNavTranslationWriteIsolatedByProjectAndFallsBack(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")
	env.probe.reset()

	if _, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{hash, "Home"})); err != nil {
		t.Fatalf("工程 A 写入失败: %v", err)
	}

	if target, _ := navStoredTarget(t, env.content, env.otherID, navWriteLang, hash); target != "" {
		t.Fatalf("工程隔离失效：别的工程读到了 %q", target)
	}

	if _, err := env.content.Upsert(context.Background(), []i18n.ContentWriteItem{{
		SourceHash: hash, Context: "navigation.label", Lang: navWriteLang,
		SourceText: "首页", TargetText: "Home (global)", Engine: i18n.ContentEngineManual,
	}}); err != nil {
		t.Fatalf("写全局译文失败: %v", err)
	}
	if target, _ := navStoredTarget(t, env.content, env.otherID, navWriteLang, hash); target != "Home (global)" {
		t.Fatalf("全局回落失效：读到 %q", target)
	}

	if target, _ := navStoredTarget(t, env.content, env.projectID, navWriteLang, hash); target != "Home" {
		t.Fatalf("工程行未优先于全局行：读到 %q", target)
	}
}

func TestNavTranslationWriteEmptyTargetIsIgnored(t *testing.T) {
	env := newNavWriteEnv(t)
	unknownHash := strings.Repeat("f", 64)

	res, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{unknownHash, "   "}))
	if err != nil {
		t.Fatalf("空 target 不该失败: %v", err)
	}
	if res.Written != 0 || len(res.ChangedKinds) != 0 {
		t.Fatalf("空 target 应零写入: %+v", res)
	}
	if calls := env.probe.snapshot(); len(calls) != 0 {
		t.Fatalf("空 target 不该派发: %v", calls)
	}
	if n := navCountTranslations(t, env.db, env.projectID, unknownHash); n != 0 {
		t.Fatalf("空 target 不得写库，实际 %d 行", n)
	}
}

func TestNavTranslationWriteRejectsUnknownHash(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")
	env.probe.reset()
	unknown := strings.Repeat("a", 64)

	_, err := navWrite(t, env.writer, env.projectID, navWriteLang,
		navRows([2]string{hash, "Home"}, [2]string{unknown, "Ghost"}))
	if err == nil {
		t.Fatal("未知 hash 应整批拒绝")
	}
	var blocked facing.BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("期望可定位明细（facing.BlockedError），实际 %T", err)
	}
	items := blocked.BlockedItems()
	if len(items) != 1 || items[0].ID != unknown {
		t.Fatalf("明细应只含未知 hash 那一行: %+v", items)
	}

	if n := navCountTranslations(t, env.db, env.projectID, hash); n != 0 {
		t.Fatalf("整批拒绝后不得留下译文，实际 %d 行", n)
	}
}

func TestNavTranslationWriteRejectsUntranslatable(t *testing.T) {
	env := newNavWriteEnv(t)
	title := "2024"
	hash := navAddItem(t, env.navSvc, env.projectID, title, "header")
	env.probe.reset()

	_, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{hash, "2024 EN"}))
	if err == nil {
		t.Fatal("不可翻译的原文应整批拒绝")
	}
	if n := navCountTranslations(t, env.db, env.projectID, hash); n != 0 {
		t.Fatalf("拒绝后不得写库，实际 %d 行", n)
	}
	if calls := env.probe.snapshot(); len(calls) != 0 {
		t.Fatalf("拒绝时不得派发: %v", calls)
	}
}

func TestNavTranslationWriteRejectsDuplicateRows(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")
	env.probe.reset()

	_, err := navWrite(t, env.writer, env.projectID, navWriteLang,
		navRows([2]string{hash, "Home"}, [2]string{hash, "Start"}))
	if err == nil {
		t.Fatal("重复行应整批拒绝")
	}
	if n := navCountTranslations(t, env.db, env.projectID, hash); n != 0 {
		t.Fatalf("拒绝后不得写库，实际 %d 行", n)
	}
}

func TestNavTranslationWriteSameManualValueWritesNothing(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")
	env.probe.reset()

	if _, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{hash, "Home"})); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	before := len(env.probe.snapshot())

	res, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{hash, "Home"}))
	if err != nil {
		t.Fatalf("同值提交不该失败: %v", err)
	}
	if res.Written != 0 || len(res.ChangedKinds) != 0 {
		t.Fatalf("同值 manual 不算变化: %+v", res)
	}
	if after := env.probe.snapshot(); len(after) != before {
		t.Fatalf("同值不得再派发: 前 %d 次，后 %d 次", before, len(after))
	}
}

func TestNavTranslationWriteCrossKindSharedHash(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")
	env.probe.reset()
	if other := navAddItem(t, env.navSvc, env.projectID, "首页", "footer"); other != hash {
		t.Fatalf("夹具前提不成立：两个位置的同一文字应同 hash，得到 %s / %s", hash, other)
	}
	env.probe.reset()

	res, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{hash, "Home"}))
	if err != nil {
		t.Fatalf("共享原文写入失败: %v", err)
	}
	if got := strings.Join(res.ChangedKinds, ","); got != "header,footer" {
		t.Fatalf("受影响位置应含两个: %q", got)
	}

	want := "page:header,instance:header,queue:header,page:footer,instance:footer,queue:footer"
	if got := strings.Join(env.probe.snapshot(), ","); got != want {
		t.Fatalf("派发序列不符:\n want %s\n got  %s", want, got)
	}
}

func TestNavTranslationWriteRollsBackOnStaleFailure(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			env := newNavWriteEnv(t)
			hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")
			env.probe.reset()
			env.probe.failAt = failAt
			env.probe.failMsg = "注入失败"

			_, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{hash, "Home"}))
			if err == nil {
				t.Fatalf("第 %d 步失败应整笔失败", failAt)
			}
			if n := navCountTranslations(t, env.db, env.projectID, hash); n != 0 {
				t.Fatalf("第 %d 步失败后译文必须回滚，实际剩 %d 行", failAt, n)
			}
			if target, _ := navStoredTarget(t, env.content, env.projectID, navWriteLang, hash); target != "" {
				t.Fatalf("第 %d 步失败后读口径应为空，实际 %q", failAt, target)
			}

			if calls := env.probe.snapshot(); len(calls) != failAt {
				t.Fatalf("第 %d 步失败后不应继续调用端口，实际调用 %v", failAt, calls)
			}
		})
	}
}

func TestNavTranslationWriteRejectsOtherProjectMenu(t *testing.T) {
	env := newNavWriteEnv(t)
	foreignHash := navAddItem(t, env.navSvc, env.otherID, "外部菜单", "header")
	env.probe.reset()

	_, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{foreignHash, "Foreign"}))
	if err == nil {
		t.Fatal("别的工程的菜单文字不该能在本工程写入译文")
	}
	if n := navCountTranslations(t, env.db, env.projectID, foreignHash); n != 0 {
		t.Fatalf("拒绝后不得写库，实际 %d 行", n)
	}
}

func TestNavMenuWriteSharesContentLockWithTranslationWriter(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")
	env.probe.reset()

	if _, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{hash, "Home"})); err != nil {
		t.Fatalf("先写 header 译文失败: %v", err)
	}

	if _, err := env.navSvc.Create(context.Background(), &navigationdto.CreateReq{
		ProjectID: env.projectID, Title: "首页", Path: "/footer-home", Kind: "footer",
	}); err != nil {
		t.Fatalf("把同一段文字加进 footer 失败（CRUD 必须能在译文写链的锁下成功）: %v", err)
	}

	env.probe.reset()

	var n int64
	if err := env.db.Raw("SELECT count(*) FROM navigations WHERE project_id = ? AND kind = 'footer' AND title = ?",
		env.projectID, "首页").Scan(&n).Error; err != nil {
		t.Fatalf("统计 footer 菜单项失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("footer 菜单项应落库 1 行，实际 %d", n)
	}

	res, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{hash, "Home 2"}))
	if err != nil {
		t.Fatalf("新增位置后写译文失败: %v", err)
	}
	if got := strings.Join(res.ChangedKinds, ","); got != "header,footer" {
		t.Fatalf("同 hash 新增位置后受影响位置应含两个，实际 %q（漏掉的那个位置会永远停在旧标签）", got)
	}

	calls := env.probe.snapshot()
	if !containsCall(calls, "instance:footer") {
		t.Fatalf("菜单 CRUD 必须同事务标记实例（instance:footer），实际序列 %v", calls)
	}
	if !containsCall(calls, "page:footer") || !containsCall(calls, "queue:footer") {
		t.Fatalf("菜单 CRUD 的页标记与入队都必须在同一事务里派发，实际序列 %v", calls)
	}
}

func TestNavMenuUpdateRereadsKindUnderRealInterleaving(t *testing.T) {
	env := newNavWriteEnv(t)
	navAddItem(t, env.navSvc, env.projectID, "首页", "header")
	env.probe.reset()
	rows, err := env.navSvc.List(context.Background(), &navigationdto.ListReq{ProjectID: env.projectID, Kind: "header"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("夹具前提不成立: %v %d", err, len(rows))
	}
	itemID := rows[0].ID

	ctx := context.Background()
	holder, err := newAdvisoryLockHolder(ctx, t, env.db, env.projectID, "footer", itemID)
	if err != nil {
		t.Fatalf("准备持锁连接失败: %v", err)
	}
	defer holder.close()

	type updResult struct {
		res *navigationdto.NavigationResp
		err error
	}
	done := make(chan updResult, 1)
	newTitle := "改名（B）"
	go func() {
		res, uerr := env.navSvc.Update(ctx, &navigationdto.UpdateReq{ID: itemID, Title: &newTitle})
		done <- updResult{res: res, err: uerr}
	}()

	if err := waitForWaitingOnLock(ctx, t, env.db, holder.key); err != nil {
		t.Fatalf("B 未进入等锁状态: %v", err)
	}

	if err := holder.commit(); err != nil {
		t.Fatalf("连接 1 提交失败: %v", err)
	}

	got := <-done
	if got.err != nil {
		t.Fatalf("B 的更新失败: %v", got.err)
	}
	calls := env.probe.snapshot()
	if containsCall(calls, "page:header") || containsCall(calls, "instance:header") {
		t.Fatalf("B 必须按锁内当前 kind（footer）失效，不该打 header —— 说明判定用的是锁外快照：%v", calls)
	}
	for _, want := range []string{"page:footer", "instance:footer", "queue:footer"} {
		if !containsCall(calls, want) {
			t.Fatalf("B 必须打当前位置 footer 的失效，缺 %q，实际 %v", want, calls)
		}
	}

	if got.res == nil || got.res.Title != newTitle {
		t.Fatalf("返回值应为 B 写入的标题，实际 %+v", got.res)
	}
}

type advisoryLockHolder struct {
	conn *sql.Conn
	tx   *sql.Tx

	key string
}

func newAdvisoryLockHolder(ctx context.Context, t *testing.T, db *gorm.DB, projectID, kind, itemID string) (*advisoryLockHolder, error) {
	t.Helper()
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}

	key := "i18n.content:" + strings.ToLower(strings.TrimSpace(projectID))
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, $2))", key, 0); err != nil {
		tx.Rollback()
		conn.Close()
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE navigations SET kind = $1 WHERE id = $2", kind, itemID); err != nil {
		tx.Rollback()
		conn.Close()
		return nil, err
	}
	return &advisoryLockHolder{conn: conn, tx: tx, key: key}, nil
}

func (h *advisoryLockHolder) commit() error {
	if err := h.tx.Commit(); err != nil {
		return err
	}
	return h.conn.Close()
}

func (h *advisoryLockHolder) close() {
	_ = h.tx.Rollback()
	_ = h.conn.Close()
}

func waitForWaitingOnLock(ctx context.Context, t *testing.T, db *gorm.DB, key string) error {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int64

		err := db.Raw(`
			SELECT count(*) FROM pg_locks l
			WHERE l.locktype = 'advisory'
			  AND NOT l.granted
			  AND l.database = (SELECT oid FROM pg_database WHERE datname = current_database())
			  AND l.classid = (((hashtextextended($1, $2) >> 32) & 4294967295)::oid)
			  AND l.objid   = ((hashtextextended($1, $2) & 4294967295)::oid)
			  AND l.objsubid = 1`, key, 0).Scan(&n).Error
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("10s 内没有观察到本库本键（%s）的 advisory 等待者", key)
		}
		runtime.Gosched()
		time.Sleep(5 * time.Millisecond)
	}
}

var errNavRereadInjected = errors.New("injected: 回读 navigations 失败")

type navRereadFailureProbe struct {
	mu          sync.Mutex
	updateSeen  int
	updateRows  int64
	lockedReads int
	injected    int
}

func (p *navRereadFailureProbe) snap() (upd, rows, locked, inj int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.updateSeen, int(p.updateRows), p.lockedReads, p.injected
}

func navStatementFor(tx *gorm.DB, itemID string) bool {
	if tx == nil || tx.Statement == nil {
		return false
	}
	table := tx.Statement.Table
	if table == "" && tx.Statement.Schema != nil {
		table = tx.Statement.Schema.Table
	}
	if table != "navigations" {
		return false
	}
	return statementBindsValue(tx.Statement.Vars, itemID)
}

func statementBindsValue(vars any, want string) bool {
	switch v := vars.(type) {
	case nil:
		return false
	case string:
		return v == want
	case fmt.Stringer:
		return v.String() == want
	case []any:
		for _, item := range v {
			if statementBindsValue(item, want) {
				return true
			}
		}
		return false
	case []string:
		for _, item := range v {
			if item == want {
				return true
			}
		}
		return false
	case map[string]any:
		for _, item := range v {
			if statementBindsValue(item, want) {
				return true
			}
		}
		return false
	default:

		return false
	}
}

func attachNavRereadFailure(t *testing.T, db *gorm.DB, itemID string) *navRereadFailureProbe {
	t.Helper()
	probe := &navRereadFailureProbe{}
	locked := 0
	const cbName = "test:nav_reread_failure"

	queryCB := func(tx *gorm.DB) {
		if !navStatementFor(tx, itemID) {
			return
		}

		if !strings.Contains(strings.ToUpper(tx.Statement.SQL.String()), "FOR UPDATE") {
			return
		}
		locked++
		probe.mu.Lock()
		probe.lockedReads = locked
		isReread := locked == 2
		if isReread {
			probe.injected++
		}
		probe.mu.Unlock()
		if isReread {
			tx.AddError(errNavRereadInjected)
		}
	}
	updateCB := func(tx *gorm.DB) {
		if !navStatementFor(tx, itemID) {
			return
		}
		if !strings.Contains(strings.ToUpper(tx.Statement.SQL.String()), "UPDATE") {
			return
		}
		probe.mu.Lock()
		probe.updateSeen++
		probe.updateRows = tx.RowsAffected
		probe.mu.Unlock()
	}
	if err := db.Callback().Query().After("gorm:query").Register(cbName+"_query", queryCB); err != nil {
		t.Fatalf("注册回读失败回调失败: %v", err)
	}
	if err := db.Callback().Update().After("gorm:update").Register(cbName+"_update", updateCB); err != nil {
		t.Fatalf("注册写观测回调失败: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove(cbName + "_query")
		_ = db.Callback().Update().Remove(cbName + "_update")
	})
	return probe
}

func TestNavMenuUpdateRollsBackWhenRereadFails(t *testing.T) {
	env := newNavWriteEnv(t)
	navAddItem(t, env.navSvc, env.projectID, "首页", "header")
	rows, err := env.navSvc.List(context.Background(), &navigationdto.ListReq{ProjectID: env.projectID, Kind: "header"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("夹具前提不成立: %v %d", err, len(rows))
	}
	id := rows[0].ID
	beforeTitle, beforeUpdatedAt := rows[0].Title, rows[0].UpdatedAt

	probe := attachNavRereadFailure(t, env.db, id)

	newTitle := "改名（回读应失败）"
	_, uerr := env.navSvc.Update(context.Background(), &navigationdto.UpdateReq{ID: id, Title: &newTitle})
	if uerr == nil {
		t.Fatal("写后回读失败必须让 Update 返回错误")
	}
	upd, updRows, locked, inj := probe.snap()

	if locked != 2 || inj != 1 {
		t.Fatalf("注入点不对：锁内读 %d 次、注入 %d 次（应为 2 次读、正好第 2 次注入）", locked, inj)
	}

	if upd == 0 || updRows != 1 {
		t.Fatalf("写未发生就失败，回滚断言不成立：UPDATE 次数 %d、行数 %d", upd, updRows)
	}
	if !errors.Is(uerr, errNavRereadInjected) {
		t.Fatalf("错误应来自注入的写后回读失败，实际 %v", uerr)
	}

	after, err := env.navSvc.List(context.Background(), &navigationdto.ListReq{ProjectID: env.projectID, Kind: "header"})
	if err != nil || len(after) != 1 {
		t.Fatalf("回滚后读取失败: %v %d", err, len(after))
	}
	if after[0].Title != beforeTitle {
		t.Fatalf("回读失败后标题必须回滚到 %q，实际 %q（写已提交说明回读还在事务外）", beforeTitle, after[0].Title)
	}
	if after[0].UpdatedAt != beforeUpdatedAt {
		t.Fatalf("回读失败后 update_time 必须回滚到 %q，实际 %q", beforeUpdatedAt, after[0].UpdatedAt)
	}
}

func TestNavMenuUpdateReturnsCommittedValue(t *testing.T) {
	env := newNavWriteEnv(t)
	navAddItem(t, env.navSvc, env.projectID, "首页", "header")
	rows, err := env.navSvc.List(context.Background(), &navigationdto.ListReq{ProjectID: env.projectID, Kind: "header"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("夹具前提不成立: %v %d", err, len(rows))
	}
	id := rows[0].ID
	title := "提交后可见"
	res, err := env.navSvc.Update(context.Background(), &navigationdto.UpdateReq{ID: id, Title: &title})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if res.Title != title {
		t.Fatalf("返回值必须是库内已提交的标题，实际 %q", res.Title)
	}
	if strings.TrimSpace(res.UpdatedAt) == "" {
		t.Fatal("返回值必须带库内 update_time")
	}
}

func containsCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

func TestNavTranslationWriteSerializesSameProject(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")
	env.probe.reset()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	targets := []string{"Home A", "Home B"}
	for i := range targets {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{hash, targets[i]}))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("并发第 %d 个写失败: %v", i, err)
		}
	}
	if n := navCountTranslations(t, env.db, env.projectID, hash); n != 1 {
		t.Fatalf("同工程同 hash 只该有一行，实际 %d 行", n)
	}
	final, _ := navStoredTarget(t, env.content, env.projectID, navWriteLang, hash)
	if final != "Home A" && final != "Home B" {
		t.Fatalf("最终值应是某一次提交的完整值，实际 %q", final)
	}
}
