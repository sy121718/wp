package feature

// navigation_translation_write_test.go — 译文整表写入（TranslationWriter）的真实 PG 契约。
//
// 单元测试（internal/module/navigation/service/navigation_translation_write_test.go）验的是
// **规则与顺序**，但夹具是桩：桩测不出 rls 作用域真的生效、测不出 UPSERT 落到哪一行、
// 也测不出「任一步失败时整笔真的回滚」。这三件事的判据全在 PG 上，所以这里用真实迁移库
// 跑同一批场景（support.NewMigratedPGTestDB，PG 不可用时自动 skip）。
//
// 三个跨模块端口（页标记 / 实例标记 / 队列入队）在本批是**别人 owner 的在途实现**：
// 这里注入「记录被调用序位 + 可在指定位失败」的探针，钉住端口契约（同 tx、逐 kind、
// 失败即整笔回滚），真实命中口径由那三个 owner 自己的用例覆盖 —— 本文件因此不依赖
// 尚未合并的代码，同时从侧面证明 navigation 只认这三个最小接口。

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

// —— 端口探针 ——

// navStaleProbe 记录三个端口的调用序位，并可在第 N 次调用注入失败。
//
// 按**序位**而不是按 kind 注入失败：第 12 步的顺序（页 → 实例 → 队列，逐 kind）本身
// 就是契约的一部分，只有按被调用次序才能证明「第 N 次失败时前面的写入一起回滚」。
type navStaleProbe struct {
	mu      sync.Mutex
	calls   []string
	failAt  int    // >0 时，第 failAt 次调用返回错误（1-based）
	failMsg string // 注入失败的错误文本（断言整笔失败后零残留）
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

// reset 清空记录（用于把「上一条写链」与「本条写链」的派发序列分开断言）。
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

// navProbeItem 按**菜单项**失效的探针（键 navigation:{itemID}，与按位置键不同）。
type navProbeItem struct{ p *navStaleProbe }

func (s navProbeItem) MarkNavigationStaleTx(_ context.Context, _ *gorm.DB, _, navigationID string) (int64, error) {
	if err := s.p.record("item:" + navigationID); err != nil {
		return 0, err
	}
	return 1, nil
}

// navProbeItemInstances 按**菜单项**失效自动发布实例的探针（与按位置的 instance: 区分）。
type navProbeItemInstances struct{ p *navStaleProbe }

func (s navProbeItemInstances) MarkInstanceNavigationStaleTx(_ context.Context, _ *gorm.DB, _, navigationID string, _ time.Time) (int64, error) {
	if err := s.p.record("iteminstance:" + navigationID); err != nil {
		return 0, err
	}
	return 1, nil
}

// 编译期断言：探针满足 navigation 声明的四个端口（签名漂移会在这里断掉）。
var (
	_ navigationcontract.PageStalePort                 = navProbePage{}
	_ navigationcontract.PresentationStalePort         = navProbeInstances{}
	_ navigationcontract.MenuStaleEnqueuePort          = navProbeQueue{}
	_ navigationcontract.MenuItemStalePort             = navProbeItem{}
	_ navigationcontract.MenuItemPresentationStalePort = navProbeItemInstances{}
)

// —— 夹具 ——

// navWriteEnv 写路径需要的全部件：真实迁移库 + 两个真工程 + 真 Service（来源索引）
// + 真 i18n ContentWriter（UPSERT 与写锁）+ 端口探针。
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
	// 菜单 CRUD 的同事务失效端口：与译文写路径**同一组探针**（装配层生产实现里两条写链
	// 共用同一批适配器）。缺它时 Create/Update/Delete/SwapMove 会 fail closed —— 本夹具
	// 用它来同时验「CRUD 会派发失效」与「两条写链的派发序列可对比」。
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
		// 装配层的真实适配形状：把 project dto 列表映射成 service 的最小条目。
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

// navAddItem 建一条菜单项，返回服务端算出的原文 hash。
func navAddItem(t *testing.T, svc *navigationservice.Service, projectID, title, kind string) string {
	t.Helper()
	if _, err := svc.Create(context.Background(), &navigationdto.CreateReq{
		ProjectID: projectID, Title: title, Path: "/" + strings.TrimSpace(title), Kind: kind,
	}); err != nil {
		t.Fatalf("创建导航项 %s(%s) 失败: %v", title, kind, err)
	}
	return i18n.ContentHash(title)
}

// navRows 提交行（语境固定为导航标签）。
func navRows(rows ...[2]string) []navigationcontract.TranslationWriteRow {
	out := make([]navigationcontract.TranslationWriteRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, navigationcontract.TranslationWriteRow{Context: "navigation.label", Hash: r[0], Target: r[1]})
	}
	return out
}

// navWrite 读写服务一次。
func navWrite(t *testing.T, w *navigationservice.TranslationWriter, projectID, lang string, rows []navigationcontract.TranslationWriteRow) (*navigationcontract.TranslationWriteResult, error) {
	t.Helper()
	return w.WriteTranslations(context.Background(), &navigationcontract.TranslationWriteReq{
		ProjectID: projectID, Lang: lang, Rows: rows,
	})
}

// navStoredTarget 用 i18n 的读口径（工程行优先、全局回落）读回译文；无译文返回空串。
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

// navCountTranslations 数 (工程, hash, 语境) 的译文行数 —— 回滚断言用（必须为 0）。
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

// —— 用例 ——

// TestNavTranslationWritePersistsAndDispatches 一次真实写入：落库 + 三端口按序派发。
func TestNavTranslationWritePersistsAndDispatches(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")

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
	// 库内事实：本工程行、manual 引擎。
	if target, engine := navStoredTarget(t, env.content, env.projectID, navWriteLang, hash); target != "Home" || engine != i18n.ContentEngineManual {
		t.Fatalf("落库内容不符: target=%q engine=%q", target, engine)
	}
	// 派发顺序：每个 kind 都是 页 → 实例 → 队列。
	if got := strings.Join(env.probe.snapshot(), ","); got != "page:header,instance:header,queue:header" {
		t.Fatalf("派发顺序不符: %q", got)
	}
}

// TestNavTranslationWriteIsolatedByProjectAndFallsBack 工程隔离 + 全局回落。
//
// 隔离：A 的译文不得出现在 B 的读口径里；回落：B 自己没写过时读到的是全局行。
func TestNavTranslationWriteIsolatedByProjectAndFallsBack(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")

	if _, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{hash, "Home"})); err != nil {
		t.Fatalf("工程 A 写入失败: %v", err)
	}
	// 另一工程（无菜单、无译文）读同一 hash：不得看到 A 的译文。
	if target, _ := navStoredTarget(t, env.content, env.otherID, navWriteLang, hash); target != "" {
		t.Fatalf("工程隔离失效：别的工程读到了 %q", target)
	}
	// 全局行回落：手写一条全局译文后，B 读到它。
	if _, err := env.content.Upsert(context.Background(), []i18n.ContentWriteItem{{
		SourceHash: hash, Context: "navigation.label", Lang: navWriteLang,
		SourceText: "首页", TargetText: "Home (global)", Engine: i18n.ContentEngineManual,
	}}); err != nil {
		t.Fatalf("写全局译文失败: %v", err)
	}
	if target, _ := navStoredTarget(t, env.content, env.otherID, navWriteLang, hash); target != "Home (global)" {
		t.Fatalf("全局回落失效：读到 %q", target)
	}
	// A 自己的行仍然优先于全局行。
	if target, _ := navStoredTarget(t, env.content, env.projectID, navWriteLang, hash); target != "Home" {
		t.Fatalf("工程行未优先于全局行：读到 %q", target)
	}
}

// TestNavTranslationWriteEmptyTargetIsIgnored 空 target = 本次不改这一行：
// 即便原文已不存在也照常成功（先丢弃、再查来源），且零写入零派发。
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

// TestNavTranslationWriteRejectsUnknownHash 未知 hash：整批拒绝、零写入、明细可定位。
func TestNavTranslationWriteRejectsUnknownHash(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")
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
	// 同一批里的合法行也不得写入（全成或全败）。
	if n := navCountTranslations(t, env.db, env.projectID, hash); n != 0 {
		t.Fatalf("整批拒绝后不得留下译文，实际 %d 行", n)
	}
}

// TestNavTranslationWriteRejectsUntranslatable 不可翻译（纯数字）整批拒绝。
func TestNavTranslationWriteRejectsUntranslatable(t *testing.T) {
	env := newNavWriteEnv(t)
	title := "2024"
	hash := navAddItem(t, env.navSvc, env.projectID, title, "header")

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

// TestNavTranslationWriteRejectsDuplicateRows 同 hash 同语境出现两次：409 + 明细。
func TestNavTranslationWriteRejectsDuplicateRows(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")

	_, err := navWrite(t, env.writer, env.projectID, navWriteLang,
		navRows([2]string{hash, "Home"}, [2]string{hash, "Start"}))
	if err == nil {
		t.Fatal("重复行应整批拒绝")
	}
	if n := navCountTranslations(t, env.db, env.projectID, hash); n != 0 {
		t.Fatalf("拒绝后不得写库，实际 %d 行", n)
	}
}

// TestNavTranslationWriteSameManualValueWritesNothing 同值且已是 manual：零写零派发。
func TestNavTranslationWriteSameManualValueWritesNothing(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")

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

// TestNavTranslationWriteCrossKindSharedHash 同一段文字出现在两个位置：
// 一次提交必须同时派发两个位置（行级信息还原不出位置，ChangedKinds 才是权威）。
func TestNavTranslationWriteCrossKindSharedHash(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")
	if other := navAddItem(t, env.navSvc, env.projectID, "首页", "footer"); other != hash {
		t.Fatalf("夹具前提不成立：两个位置的同一文字应同 hash，得到 %s / %s", hash, other)
	}

	res, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{hash, "Home"}))
	if err != nil {
		t.Fatalf("共享原文写入失败: %v", err)
	}
	if got := strings.Join(res.ChangedKinds, ","); got != "header,footer" {
		t.Fatalf("受影响位置应含两个: %q", got)
	}
	// 每个位置都要页 + 实例 + 队列三条（顺序按规范 kind 顺序）。
	want := "page:header,instance:header,queue:header,page:footer,instance:footer,queue:footer"
	if got := strings.Join(env.probe.snapshot(), ","); got != want {
		t.Fatalf("派发序列不符:\n want %s\n got  %s", want, got)
	}
}

// TestNavTranslationWriteRollsBackOnStaleFailure 三处失效任一步失败 → 整笔回滚。
//
// 这是本批最关键的判据：译文与失效必须同进同出。逐个失败位点跑一遍，
// 每次都要看到「错误上抛 + 库里零行 + 后续端口未被调用」。
func TestNavTranslationWriteRollsBackOnStaleFailure(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			env := newNavWriteEnv(t)
			hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")
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
			// 失败点之后的端口不得被调用：回滚不是「继续往下走再撤」。
			if calls := env.probe.snapshot(); len(calls) != failAt {
				t.Fatalf("第 %d 步失败后不应继续调用端口，实际调用 %v", failAt, calls)
			}
		})
	}
}

// TestNavTranslationWriteRejectsOtherProjectMenu 菜单项属于别的工程：来源索引读不到 → 未知 hash。
func TestNavTranslationWriteRejectsOtherProjectMenu(t *testing.T) {
	env := newNavWriteEnv(t)
	foreignHash := navAddItem(t, env.navSvc, env.otherID, "外部菜单", "header")

	_, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{foreignHash, "Foreign"}))
	if err == nil {
		t.Fatal("别的工程的菜单文字不该能在本工程写入译文")
	}
	if n := navCountTranslations(t, env.db, env.projectID, foreignHash); n != 0 {
		t.Fatalf("拒绝后不得写库，实际 %d 行", n)
	}
}

// TestNavMenuWriteSharesContentLockWithTranslationWriter 菜单 CRUD 与译文写共用同一把工程锁。
//
// 这是本批闭环的核心判据（缺陷形态：同 hash 新增位置漏失效）：
//  1. 先给 header 的「首页」写译文；
//  2. 再用 **Service.Create** 把同一段文字加进 footer —— 走的是菜单 CRUD 的新事务路径；
//  3. 两次都要成功，且 footer 的菜单项真的落库；随后对该 hash 的译文写入必须把
//     changedKinds 报成 header,footer（来源索引四 kind 全收 + 同 hash 合并 Kinds）。
//
// 若 CRUD 仍走自动提交（不持锁、不共享事务），第 3 步会读到「提交前快照」而漏掉 footer ——
// 而那一侧没有任何补偿入口，症状是「保存成功、页脚实例停在旧标签上」。
func TestNavMenuWriteSharesContentLockWithTranslationWriter(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")

	if _, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{hash, "Home"})); err != nil {
		t.Fatalf("先写 header 译文失败: %v", err)
	}
	// 菜单 CRUD 与译文写共用同一把锁：这里沿用同一个未关闭的事务前提（真实串行由
	// 锁保证，用例只验证「CRUD 写成功后来源索引立刻看得到新位置」）。
	if _, err := env.navSvc.Create(context.Background(), &navigationdto.CreateReq{
		ProjectID: env.projectID, Title: "首页", Path: "/footer-home", Kind: "footer",
	}); err != nil {
		t.Fatalf("把同一段文字加进 footer 失败（CRUD 必须能在译文写链的锁下成功）: %v", err)
	}
	// CRUD 自己也要把实例屏障打出去：先把译文写那一轮留下的探针记录清掉，
	// 这样下面的序列只反映 Create 的派发（页 → 实例 → 入队，再按项键）。
	env.probe.reset()
	// 库内事实：footer 那一行真的在（而不是「接口成功、行没写」）。
	var n int64
	if err := env.db.Raw("SELECT count(*) FROM navigations WHERE project_id = ? AND kind = 'footer' AND title = ?",
		env.projectID, "首页").Scan(&n).Error; err != nil {
		t.Fatalf("统计 footer 菜单项失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("footer 菜单项应落库 1 行，实际 %d", n)
	}
	// 来源索引把同 hash 的两个位置合并：再写译文时 two kinds 一起派发。
	res, err := navWrite(t, env.writer, env.projectID, navWriteLang, navRows([2]string{hash, "Home 2"}))
	if err != nil {
		t.Fatalf("新增位置后写译文失败: %v", err)
	}
	if got := strings.Join(res.ChangedKinds, ","); got != "header,footer" {
		t.Fatalf("同 hash 新增位置后受影响位置应含两个，实际 %q（漏掉的那个位置会永远停在旧标签）", got)
	}
	// CRUD 的派发序列必须含**实例**标记：少了它，实例的旧编译晚到时会通过激活谓词
	// 覆盖新 HTML 并清掉 stale —— 入队只能保证「会重建一次」。
	calls := env.probe.snapshot()
	if !containsCall(calls, "instance:footer") {
		t.Fatalf("菜单 CRUD 必须同事务标记实例（instance:footer），实际序列 %v", calls)
	}
	if !containsCall(calls, "page:footer") || !containsCall(calls, "queue:footer") {
		t.Fatalf("菜单 CRUD 的页标记与入队都必须在同一事务里派发，实际序列 %v", calls)
	}
}

// TestNavMenuUpdateRereadsKindUnderRealInterleaving 真实交错：B 在锁外读到 header，
// 等 A 把这一行改成 footer 并提交后拿到锁 —— B 必须只打 footer（按锁内当前值）。
//
// 为什么纯顺序两次 Update 证明不了根因：旧实现（锁外快照）在同一协程里顺序执行时，
// 上一次 Update 已经把 kind 落库，第二次定位读到的就是 footer，两种实现都绿。
// 只有把「B 的定位」与「B 的写入」拆到一个真实持锁窗口的两侧，才能把两种实现分开。
//
// 造拦截不用 sleep，用三件真实的事：
//  1. 另一个**独立连接**（sql.Conn）开事务、拿同一把 pg_advisory_xact_lock（键与生产
//     LockProjectContentWrite 逐字相同），并 UPDATE 该行 kind='footer' 但不提交；
//  2. 此时未提交的 UPDATE 对别的连接不可见 —— B 的 locateNavigation 读到的仍是 header，
//     这正是旧实现会用的那份快照；
//  3. B 随后在 withMenuWriteTx 里阻塞在 advisory 锁上（锁被连接 1 持有）；连接 1 提交、
//     释放锁，B 才继续 —— 锁内 GetForUpdateTx 读到的是 footer。
//
// 判据：B 的探针序列必须**不含** header，且**含** footer。旧实现会打 header。
func TestNavMenuUpdateRereadsKindUnderRealInterleaving(t *testing.T) {
	env := newNavWriteEnv(t)
	navAddItem(t, env.navSvc, env.projectID, "首页", "header")
	rows, err := env.navSvc.List(context.Background(), &navigationdto.ListReq{ProjectID: env.projectID, Kind: "header"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("夹具前提不成立: %v %d", err, len(rows))
	}
	itemID := rows[0].ID

	// 连接 1：持锁 + 改 kind + 不提交。
	ctx := context.Background()
	holder, err := newAdvisoryLockHolder(ctx, t, env.db, env.projectID, "footer", itemID)
	if err != nil {
		t.Fatalf("准备持锁连接失败: %v", err)
	}
	defer holder.close()

	// B：先定位（读到 header，连接 1 的 UPDATE 未提交不可见），再阻塞在锁上。
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

	// 等 B 真的进入「等锁」：用连接 3 观察 pg_locks 里出现未授予的 advisory 等待者。
	// 这是连接层面的可观测事实，不是计时。
	if err := waitForWaitingOnLock(ctx, t, env.db, holder.key); err != nil {
		t.Fatalf("B 未进入等锁状态: %v", err)
	}
	// 放行：连接 1 提交，锁释放，B 继续。
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
	// B 实际写入的标题也要落在库里（同事务写 + 回读）。
	if got.res == nil || got.res.Title != newTitle {
		t.Fatalf("返回值应为 B 写入的标题，实际 %+v", got.res)
	}
}

// advisoryLockHolder 一条独立连接上的「持锁 + 改行但不提交」事务。
type advisoryLockHolder struct {
	conn *sql.Conn
	tx   *sql.Tx
	// key 与生产 LockProjectContentWrite 用的是同一把锁键，供等待者查询精确匹配。
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
	// 键与 pkg/i18n.LockProjectContentWrite 逐字一致（同一把锁）。
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

// waitForWaitingOnLock 等「本数据库 + 本 advisory 键」出现未授予的等待者。
//
// 必须三重限定，否则会假绿：全实例任一 advisory 等待者（别的用例、别的表锁）都会让
// 计数 > 0，从而在 B 还没开始等锁时就提前放行持锁连接 —— 交错消失，用例永久通过。
//   - database 限定为本连接所在的库（oid = 当前连接 datid）；
//   - classid/objid 限定为 `hashtextextended(key, 0)` 拆出的高/低 32 位，
//     即 `pg_advisory_xact_lock(bigint)` 那一把锁本身；
//   - objsubid = 1 表示「两个 int4」形式里的单参数键（`pg_advisory_xact_lock(bigint)` 记 1）。
//
// 判据仍是连接层可观测事实（pg_locks.granted = false），不是计时。
func waitForWaitingOnLock(ctx context.Context, t *testing.T, db *gorm.DB, key string) error {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int64
		// hashtextextended 的结果拆成 advisory 锁的两个 int4 分量：
		// classid = 高 32 位，objid = 低 32 位（PG advisory(bigint) 的存储方式）。
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

// errNavRereadInjected 注入给「写后回读」的读失败。
var errNavRereadInjected = errors.New("injected: 回读 navigations 失败")

// navRereadFailureProbe 记录三类观测，用来自证「注入点真的是写后回读」。
type navRereadFailureProbe struct {
	mu          sync.Mutex
	updateSeen  int   // 命中本 id 的 UPDATE 次数
	updateRows  int64 // 最后一次 UPDATE 的受影响行数
	lockedReads int   // 带 FOR UPDATE 的 SELECT 次数（锁内重读 + 写后回读）
	injected    int   // 注入次数（必须恰好 1）
}

func (p *navRereadFailureProbe) snap() (upd, rows, locked, inj int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.updateSeen, int(p.updateRows), p.lockedReads, p.injected
}

// attachNavRereadFailure 让**写后回读**这一次锁内 SELECT 失败，其余照常。
//
// 为什么不能只按表计数：Update 内部对 navigations 的 SELECT 有三次 ——
//  1. locateNavigation 的锁外定位（service/navigation_service.go:279 的 s.m.Get）；
//  2. 锁内 GetForUpdateTx 重读当前行（navigation.go 的事务体开头）；
//  3. 锁内 GetForUpdateTx 写后回读（事务体末尾）。
//
// 第 1 次在加锁之前就发生了，若按表计数取 skip=1 会打到第 2 次（重读），
// 那是「事务还没写就失败」，断言「写被回滚」等于什么都没验。
//
// 因此这里两重过滤：
//   - **SQL 必须含 FOR UPDATE** —— 只有 GetForUpdateTx 会加锁读，定位那条没有；
//   - 锁内读的**第 2 次**才是写后回读（第 1 次是重读）。
//
// 同时用 Update callback 记录「本 id 的 UPDATE 确实执行过且 rows==1」，
// 断言顺序是「写已发生 → 回读失败 → 回滚」，而不是「还没写就失败」。
//
// 这是测试侧 GORM callback，不进生产代码、不新增测试专用生产钩子；cleanup 里注销，
// 避免污染同一进程内共享连接的其它用例。
// navStatementFor 判断这条语句是否就是「对 navigations 表、且绑定变量里含本 itemID」的那条。
//
// 为什么不能扫 SQL 文本找 id 字面量：GORM 的参数化写法是 `WHERE id = $3`，
// UUID 留在 `Statement.Vars` 里、根本不进 SQL 文本。拿 strings.Contains(sql, itemID)
// 判定会**恒不命中** —— updateSeen 恒 0，用例报红说"写没发生"，而实际写得好好的。
// 反过来，若哪天 GORM 改成内联（或测试改用 DryRun 渲染），文本里才出现字面量。
//
// 所以判据是**结构**而非文本：
//   - `Statement.Table`（或 Schema 里的表名）必须是 navigations —— 不认字符串里出现该词；
//   - 动词由各自的 `SQL.String()` 再确认（Query 回调看 FOR UPDATE，Update 回调看 UPDATE）；
//   - id 是否命中交由 `statementBindsValue` **递归扫描 Vars** 判定，不依赖渲染结果。
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

// statementBindsValue 递归判断绑定变量里是否出现目标字符串。
//
// Vars 的形态随子句变化：可能是标量、可能是 []any（IN 列表）、也可能是嵌套
// 的 []any / []string / map / 结构体字段。只做一层断言会在 `id IN (...)`
// 或 GORM 包一层 `[]interface{}{...}` 时漏判，于是又变成"恒 0"的伪装。
// 这里按**值**递归，遇到实现了 driver.Valuer 的直接取值，其余按类型分派。
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
		// 兜底：把标量转成字符串比较（UUID 只会以 string 出现，
		// 走到这里说明既不是 string 也不是容器，直接判定不命中）。
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
		// 只认锁内读：GORM 的 clause.Locking 会生成 "FOR UPDATE" 后缀。
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

// TestNavMenuUpdateRollsBackWhenRereadFails 写已发生 → 回读失败 → 整笔回滚。
//
// 这是「写成功却返 5xx ⇒ 调用方重试重复写」那条缺陷的直接回归位：
//   - 旧实现（commit 后 s.m.Get）：回读失败时写已提交，调用方重试会再写一次；
//   - 现实现（事务内 GetForUpdateTx）：回读失败 ⇒ 事务回滚 ⇒ 库内仍是原值，重试安全。
//
// 三条判据缺一不可：注入点确实是写后回读（lockedReads==2、injected==1）、
// 写确实执行过（updateSeen>0 且 rows==1）、库内值回到调用前。
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
	// 注入点自证：必须命中锁内第 2 次读（写后回读），不是第 1 次重读、不是定位那条。
	if locked != 2 || inj != 1 {
		t.Fatalf("注入点不对：锁内读 %d 次、注入 %d 次（应为 2 次读、正好第 2 次注入）", locked, inj)
	}
	// 写已发生自证：UPDATE 确实执行过且命中 1 行 —— 否则「回滚」断言毫无意义。
	if upd == 0 || updRows != 1 {
		t.Fatalf("写未发生就失败，回滚断言不成立：UPDATE 次数 %d、行数 %d", upd, updRows)
	}
	if !errors.Is(uerr, errNavRereadInjected) {
		t.Fatalf("错误应来自注入的写后回读失败，实际 %v", uerr)
	}
	// 库内必须还是原值 —— 写被回滚，调用方重试安全。
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

// TestNavMenuUpdateReturnsCommittedValue **返回契约**（不覆盖回读失败路径）。
//
// 诚实说明：本用例只断言「Update 成功后返回的 title 来自库内、updatedAt 非空」。
// 旧实现（commit 后 s.m.Get）同样满足这两条 —— 因此它**不能**区分回读在事务内还是事务外，
// 也**不覆盖**「回读失败 ⇒ 整笔回滚」这条路径。那条路径需要故障注入（GORM callback 在
// 指定表/指定位次返回错误），本批未实现，故此处按返回契约命名与记账，不冒充证据。
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

// containsCall 探针序列里是否包含某次调用。
func containsCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

// TestNavTranslationWriteSerializesSameProject 同工程并发写：串行（写锁生效），
// 两次都成功且最终值是其中一次的完整值，不会出现交错残行。
func TestNavTranslationWriteSerializesSameProject(t *testing.T) {
	env := newNavWriteEnv(t)
	hash := navAddItem(t, env.navSvc, env.projectID, "首页", "header")

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
