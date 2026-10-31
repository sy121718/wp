package pagemodel_test

// page_input_version_model_test.go — 实体输入代次（迁移 699）的真库行为。
//
// 为什么必须真库：这里的每一条都是 SQL 谓词与原子表达式，
//   - input_version + 1 写成 gorm.Expr 才是原子的；写成读改写会丢版本，而丢失只在并发下现形；
//   - 激活谓词 WHERE input_version = $expect 必须真的拦住「旧输入晚到」的写入；
//   - 收敛谓词多一条「账本仍是从前那个」，漏掉它会把别的成功激活覆盖成陈旧产物。
// 三者在纯函数测试里都看不见（SQL 不执行就等于没测）。
//
// 表结构来自生产迁移（support.NewMigratedPGTestDB 复制模板库），只补真实父行。
// 连接用超级用户，RLS 不生效 —— 作用域断言属 public/test/rls 的范围。

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	pagemodel "go_wp/internal/module/page/model"
	"go_wp/internal/pipeline"
	"go_wp/public/test/support"
)

const versionProject = "11111111-2222-3333-4444-555555555555"

// versionFixture 一个工程 + 一张页面。
type versionFixture struct {
	m  *pagemodel.Model
	db *gorm.DB
}

func newVersionFixture(t *testing.T) *versionFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	support.SeedProjectRow(t, db, versionProject, "代次测试站")
	return &versionFixture{m: pagemodel.NewPageModel(db), db: db}
}

// insertVersionPage 插一条真实 pages 行，返回其初始 input_version。
func (f *versionFixture) insertVersionPage(t *testing.T, id, path string) int64 {
	t.Helper()
	if err := f.db.Exec(`
		INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document,
			draft_version, stale, create_time, update_time)
		VALUES (?, ?, 'home', 'none', ?, '{"root":[]}'::jsonb, 1, false, NOW(), NOW())`,
		id, versionProject, path).Error; err != nil {
		t.Fatalf("准备页面行失败：%v", err)
	}
	var v int64
	if err := f.db.Raw("SELECT input_version FROM pages WHERE id = ?", id).Scan(&v).Error; err != nil {
		t.Fatalf("读取初始代次失败：%v", err)
	}
	return v
}

// readInputVersion 读当前代次。
func (f *versionFixture) readInputVersion(t *testing.T, id string) int64 {
	t.Helper()
	var v int64
	if err := f.db.Raw("SELECT input_version FROM pages WHERE id = ?", id).Scan(&v).Error; err != nil {
		t.Fatalf("读取代次失败：%v", err)
	}
	return v
}

// readStale 读 stale 标记。
func (f *versionFixture) readStale(t *testing.T, id string) bool {
	t.Helper()
	var stale bool
	if err := f.db.Raw("SELECT stale FROM pages WHERE id = ?", id).Scan(&stale).Error; err != nil {
		t.Fatalf("读取 stale 失败：%v", err)
	}
	return stale
}

// insertVersionArtifact 插一条产物行（只给 NOT NULL 列）。
//
// 三个 hash 都用**完整** artifactID 派生，不能截前 8 位：表上有
// uk_page_artifacts_page_hash UNIQUE (page_id, artifact_hash)（迁移 691 ——
// 产物身份按内容确定，同页不同内容必须换 hash）。截 8 位时同一页的多份产物
// （artOld / artNewer / artMine）前缀相同 → 撞唯一键，夹具准备阶段就失败，
// 被测的收敛语义一行都没跑到。用完整 id 既不撞键，又满足「不同产物不同 hash」。
func (f *versionFixture) insertVersionArtifact(t *testing.T, artifactID, pageID string, version int) {
	t.Helper()
	if err := f.db.Exec(`
		INSERT INTO page_artifacts (id, page_id, version, source_document, page_document_schema_version,
			source_hash, build_input_manifest, build_input_hash, artifact_provider, artifact_key,
			artifact_hash, compiler_version, registry_version, manifest, created_by, create_time)
		VALUES (?, ?, ?, '{}'::jsonb, 1, ?, '{}'::jsonb, ?, 'local', ?, ?, 'c1', 'r1', '{}'::jsonb, ?, NOW())`,
		artifactID, pageID, version, "src-"+artifactID, "bin-"+artifactID,
		"key-"+artifactID, "hash-"+artifactID, uuid.NewString()).Error; err != nil {
		t.Fatalf("准备产物行失败：%v", err)
	}
}

// TestSaveDraftAdvancesInputVersion 草稿保存推进代次并标 stale（同一事务）。
func TestSaveDraftAdvancesInputVersion(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000001"
	before := f.insertVersionPage(t, pageID, "/about")

	// 修订体不能传裸 nil：SaveDraftWithRevisionTx 结尾无条件 tx.Create(revision)，
	// nil 会落进 gorm 的 "invalid value, should be pointer..." —— 那是调用方少给了参数，
	// 不是「同事务里修订可以不写」。这里给一条真实修订行（生产路径同样必写）。
	revision := &pagemodel.RevisionEntity{
		ID:            uuid.NewString(),
		PageID:        pageID,
		Version:       2,
		DraftPath:     "/about",
		DraftDocument: []byte(`{"root":[{"id":"n1"}]}`),
		SourceHash:    "src-about-2",
		CreatedAt:     time.Now().UTC(),
	}
	ctx := context.Background()
	if err := f.m.SaveDraftWithRevision(ctx, versionProject, pageID, 1, "/about",
		[]byte(`{"root":[{"id":"n1"}]}`), 2, time.Now().UTC(), revision); err != nil {
		t.Fatalf("保存草稿失败：%v", err)
	}

	if got := f.readInputVersion(t, pageID); got != before+1 {
		t.Fatalf("草稿保存应把代次推进 1（%d → %d），实际 %d", before, before+1, got)
	}
	if !f.readStale(t, pageID) {
		t.Fatal("草稿保存后页面应为 stale")
	}
}

// TestMoveDraftPathAdvancesInputVersion 改 URL 改的是编译输入（canonical 地址），必须推进代次。
func TestMoveDraftPathAdvancesInputVersion(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000002"
	before := f.insertVersionPage(t, pageID, "/about")

	if err := f.m.MoveDraftPath(context.Background(), versionProject, pageID, "/company", time.Now().UTC()); err != nil {
		t.Fatalf("改 URL 失败：%v", err)
	}
	if got := f.readInputVersion(t, pageID); got != before+1 {
		t.Fatalf("改 URL 应推进代次（%d → %d），实际 %d", before, before+1, got)
	}
}

// TestExcludedLangsAdvancesInputVersion 语言排除决定产出范围，属输入变更。
func TestExcludedLangsAdvancesInputVersion(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000003"
	before := f.insertVersionPage(t, pageID, "/about")

	if err := f.m.UpdateExcludedLangs(context.Background(), versionProject, pageID, []string{"en-US"}); err != nil {
		t.Fatalf("写语言排除失败：%v", err)
	}
	if got := f.readInputVersion(t, pageID); got != before+1 {
		t.Fatalf("语言排除应推进代次（%d → %d），实际 %d", before, before+1, got)
	}
}

// TestThemeSnapshotAdvancesInputVersion 主题快照写进 draft_document，属输入变更。
func TestThemeSnapshotAdvancesInputVersion(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000004"
	before := f.insertVersionPage(t, pageID, "/about")

	theme := []byte(`{"tokens":{"accent":"#000"}}`)
	if err := f.m.UpdateThemeSnapshot(context.Background(), versionProject, pageID, theme); err != nil {
		t.Fatalf("写主题快照失败：%v", err)
	}
	if got := f.readInputVersion(t, pageID); got != before+1 {
		t.Fatalf("主题快照应推进代次（%d → %d），实际 %d", before, before+1, got)
	}

	structure := []byte(`{"header":"block-1"}`)
	if err := f.m.UpdateStructureSnapshot(context.Background(), versionProject, pageID, structure); err != nil {
		t.Fatalf("写结构快照失败：%v", err)
	}
	if got := f.readInputVersion(t, pageID); got != before+2 {
		t.Fatalf("结构快照应再推进一次（%d → %d），实际 %d", before+1, before+2, got)
	}
}

// TestMarkMenuStaleTxAdvancesAndRollsBack 菜单依赖标记：命中页面 stale + 代次 +1，
// 且**外层事务回滚时代次一起回退**（同事务是这套闸门的前提）。
func TestMarkMenuStaleTxAdvancesAndRollsBack(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000005"
	artifactID := "bbbbbbbb-0000-0000-0000-000000000005"
	before := f.insertVersionPage(t, pageID, "/about")
	f.insertVersionArtifact(t, artifactID, pageID, 1)
	if err := f.db.Exec("UPDATE pages SET staged_artifact_id = ? WHERE id = ?", artifactID, pageID).Error; err != nil {
		t.Fatalf("挂暂存产物失败：%v", err)
	}
	// 依赖行必须写**生产依赖键**：构建期登记的是 pipeline.MenuKey(projectID, kind)
	// （"menu:{projectID}:{kind}"），不是裸位置名。用裸 "header" 自造夹具会把
	// 「实现拿裸 kind 做精确等式」这个缺陷测成绿色 —— 真实站点上一条都命中不了。
	depKey := pipeline.MenuKey(versionProject, "header")
	if err := f.db.Exec(`
		INSERT INTO page_dependencies (page_id, artifact_id, dependency_kind, dependency_key, last_checked)
		VALUES (?, ?, ?, ?, NOW())`, pageID, artifactID, depKey.Kind, depKey.Key).Error; err != nil {
		t.Fatalf("准备依赖行失败：%v", err)
	}

	ctx := context.Background()
	var hit int64
	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		n, err := f.m.MarkMenuStaleTx(ctx, tx, versionProject, "header", time.Now().UTC())
		hit = n
		return err
	}); err != nil {
		t.Fatalf("菜单标记失败：%v", err)
	}
	if hit != 1 {
		t.Fatalf("应命中 1 个页面，实际 %d", hit)
	}
	if got := f.readInputVersion(t, pageID); got != before+1 {
		t.Fatalf("菜单标记应推进代次（%d → %d），实际 %d", before, before+1, got)
	}
	if !f.readStale(t, pageID) {
		t.Fatal("菜单标记后页面应为 stale")
	}
	// 回滚断言的目标值必须取**第一次标记提交之后**的代次：上面的 before 是标记前的
	// 初始值，第一次标记已把代次推到 before+1。拿 before 当「回滚后应恢复到多少」，
	// 写的是一条永远不成立的条件，回滚正确时照样报红（实测 before=1、回滚后 1）。
	committed := f.readInputVersion(t, pageID)
	committedStale := f.readStale(t, pageID)

	// 回滚：外层事务失败时代次与 stale 一起回到事务开始前的状态。
	wantErr := gorm.ErrInvalidTransaction
	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		if _, err := f.m.MarkMenuStaleTx(ctx, tx, versionProject, "header", time.Now().UTC()); err != nil {
			return err
		}
		return wantErr
	}); err == nil {
		t.Fatal("外层应返回注入的错误")
	}
	if got := f.readInputVersion(t, pageID); got != committed {
		t.Fatalf("回滚后代次必须回到 %d，实际 %d", committed, got)
	}
	// 回滚后 stale 必须回到**标记前**的值，而不是一律 false：
	// 上面的第一次标记已提交、页面此刻就是 stale=true，若硬写 false 就是把
	// 「已提交的标记」当成应该被撤销的东西 —— 回滚只该撤销本事务的写入。
	// 用 committedStale 记住提交后的真实值，回滚后再比一次，两边都是事务边界两侧的事实。
	if got := f.readStale(t, pageID); got != committedStale {
		t.Fatalf("回滚后 stale 必须回到标记前（%v），实际 %v", committedStale, got)
	}
}

// TestActivationDoesNotAdvanceInputVersion 激活只验证代次，不推进它。
func TestActivationDoesNotAdvanceInputVersion(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000006"
	artifactID := "bbbbbbbb-0000-0000-0000-000000000006"
	before := f.insertVersionPage(t, pageID, "/about")
	f.insertVersionArtifact(t, artifactID, pageID, 1)

	ctx := context.Background()
	now := time.Now().UTC()
	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		snap, err := f.m.LockPageForActivationTx(ctx, tx, versionProject, pageID)
		if err != nil {
			return err
		}
		written, err := f.m.WritePageActivationTx(ctx, tx, versionProject, pagemodel.PublicationRecord{
			PageID: pageID, Lang: "zh-CN", ActivePath: "/about",
			ArtifactID: artifactID, ArtifactHash: "hash-x", PublishedAt: now,
		}, snap.InputVersion)
		if err != nil {
			return err
		}
		if !written {
			t.Fatal("锁内写入应命中（代次未变）")
		}
		return nil
	}); err != nil {
		t.Fatalf("激活写入失败：%v", err)
	}
	if got := f.readInputVersion(t, pageID); got != before {
		t.Fatalf("激活不得推进代次，期望 %d，实际 %d", before, got)
	}
}

// TestActivationPredicateRejectsStaleInput 「旧输入晚到」：代次推进后，用旧代次激活必须落空。
func TestActivationPredicateRejectsStaleInput(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000007"
	artifactID := "bbbbbbbb-0000-0000-0000-000000000007"
	captured := f.insertVersionPage(t, pageID, "/about")
	f.insertVersionArtifact(t, artifactID, pageID, 1)

	ctx := context.Background()
	// 编译期间输入又变了。
	if err := f.m.MoveDraftPath(ctx, versionProject, pageID, "/company", time.Now().UTC()); err != nil {
		t.Fatalf("改 URL 失败：%v", err)
	}

	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		written, err := f.m.WritePageActivationTx(ctx, tx, versionProject, pagemodel.PublicationRecord{
			PageID: pageID, Lang: "zh-CN", ActivePath: "/about",
			ArtifactID: artifactID, ArtifactHash: "hash-x", PublishedAt: time.Now().UTC(),
		}, captured)
		if err != nil {
			return err
		}
		if written {
			t.Fatal("旧代次的激活必须被谓词拦住（written 应为 false）")
		}
		return nil
	}); err != nil {
		t.Fatalf("激活事务不应报错：%v", err)
	}
}

// TestForwardCompletionRefusesWhenLedgerReplaced 收敛：账本已被别的成功激活写走时，
// 不得把自己的陈旧产物写上去。这就是「恢复撞上新成功 attempt」的模型层事实。
func TestForwardCompletionRefusesWhenLedgerReplaced(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000008"
	artOld := "bbbbbbbb-0000-0000-0000-0000000000a8"
	artMine := "bbbbbbbb-0000-0000-0000-0000000000b8"
	artNewer := "bbbbbbbb-0000-0000-0000-0000000000c8"
	v := f.insertVersionPage(t, pageID, "/about")
	// page_artifacts 上有 UNIQUE (page_id, version)：三条产物必须三个不同版本号。
	f.insertVersionArtifact(t, artOld, pageID, 1)
	f.insertVersionArtifact(t, artNewer, pageID, 2) // 准备：先到的"别的成功激活"
	f.insertVersionArtifact(t, artMine, pageID, 3)  // 收敛要写的那份，版本更晚没问题

	ctx := context.Background()
	now := time.Now().UTC()
	// 账本已经是别的成功激活的结果（artNewer）。
	// 注意：激活本身不推进 input_version，但**标记 stale 的那一步会**；准备之后必须
	// 重新读一次代次当收敛的期望值，否则收敛谓词按过期代次比较必然不命中。
	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		written, err := f.m.WritePageActivationTx(ctx, tx, versionProject, pagemodel.PublicationRecord{
			PageID: pageID, Lang: "zh-CN", ActivePath: "/about",
			ArtifactID: artNewer, ArtifactHash: "hash-newer", PublishedAt: now,
		}, v)
		if err != nil {
			return err
		}
		if !written {
			t.Fatal("准备：新成功激活应写入")
		}
		return nil
	}); err != nil {
		t.Fatalf("准备失败：%v", err)
	}

	expect := f.readInputVersion(t, pageID)

	var written bool
	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		var err error
		written, err = f.m.WritePageForwardCompletionTx(ctx, tx, versionProject, pagemodel.PublicationRecord{
			PageID: pageID, Lang: "zh-CN", ActivePath: "/about",
			ArtifactID: artMine, ArtifactHash: "hash-mine", PublishedAt: now,
		}, expect, artOld)
		return err
	}); err != nil {
		t.Fatalf("收敛写入不应报错：%v", err)
	}
	if written {
		t.Fatal("账本已被替代时必须放弃前进（否则会覆盖更新的成功产物）")
	}

	// 账本必须**原样保持**新成功那一份：page_publications 的主键是 (page_id, lang)，
	// 翻成 map 只取"页面上有几行账本、每行是不是 artNewer"—— 若拒绝路径曾写进别的
	// artifact_id，这里会读到它；只比单行会漏掉"多写了一行别的语言"的形态。
	var ledger []struct {
		Lang       string
		ArtifactID *string
	}
	if err := f.db.Raw("SELECT lang, artifact_id FROM page_publications WHERE page_id = ?", pageID).
		Scan(&ledger).Error; err != nil {
		t.Fatalf("读取账本失败：%v", err)
	}
	if len(ledger) == 0 {
		t.Fatal("账本应已有新成功激活写下的那一行")
	}
	for _, row := range ledger {
		if row.ArtifactID == nil || *row.ArtifactID != artNewer {
			t.Fatalf("账本必须保持新成功的那份 %s，实际 lang=%s artifact=%v", artNewer, row.Lang, row.ArtifactID)
		}
	}
}

// TestForwardCompletionWritesWhenLedgerUnchanged 账本仍是回执记录的前一份时，收敛正常前进。
func TestForwardCompletionWritesWhenLedgerUnchanged(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-000000000009"
	artOld := "bbbbbbbb-0000-0000-0000-0000000000a9"
	artMine := "bbbbbbbb-0000-0000-0000-0000000000b9"
	v := f.insertVersionPage(t, pageID, "/about")
	f.insertVersionArtifact(t, artOld, pageID, 1)
	f.insertVersionArtifact(t, artMine, pageID, 2)

	ctx := context.Background()
	now := time.Now().UTC()
	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		if _, err := f.m.WritePageActivationTx(ctx, tx, versionProject, pagemodel.PublicationRecord{
			PageID: pageID, Lang: "zh-CN", ActivePath: "/about",
			ArtifactID: artOld, ArtifactHash: "hash-old", PublishedAt: now,
		}, v); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("准备失败：%v", err)
	}

	// 准备激活之后代次已被标记推进，收敛的期望代次取**当前值**（见上一条用例的说明）。
	expect := f.readInputVersion(t, pageID)

	var written bool
	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		var err error
		written, err = f.m.WritePageForwardCompletionTx(ctx, tx, versionProject, pagemodel.PublicationRecord{
			PageID: pageID, Lang: "zh-CN", ActivePath: "/about",
			ArtifactID: artMine, ArtifactHash: "hash-mine", PublishedAt: now,
		}, expect, artOld)
		return err
	}); err != nil {
		t.Fatalf("收敛写入失败：%v", err)
	}
	if !written {
		t.Fatal("账本未被替代时应向前完成")
	}
}

// TestLockPageForActivationReadsVersion 锁读返回的代次与表内一致（编译期捕获的比对基准）。
func TestLockPageForActivationReadsVersion(t *testing.T) {
	f := newVersionFixture(t)
	pageID := "aaaaaaaa-0000-0000-0000-00000000000a"
	want := f.insertVersionPage(t, pageID, "/about")

	ctx := context.Background()
	if err := f.m.TransactionScoped(ctx, versionProject, func(tx *gorm.DB) error {
		snap, err := f.m.LockPageForActivationTx(ctx, tx, versionProject, pageID)
		if err != nil {
			return err
		}
		if snap.InputVersion != want {
			t.Fatalf("锁读代次 %d，期望 %d", snap.InputVersion, want)
		}
		return nil
	}); err != nil {
		t.Fatalf("锁读失败：%v", err)
	}
}
