package presentationmodel_test

// presentation_receipt_identity_test.go —— 成功 attempt 身份（receipt_id）与输入意图持久化。
//
// 三条形态约束，都用真实 PG（生产迁移建表，不 AutoMigrate）：
//  1. presmodel.MarkPublishedLangTx 在**调用方事务**里写 receipt_id；空串写 NULL（不写空值），
//     因为空串会被读成"某个 id 为空的回执的成功尝试"。
//  2. PublishedReceiptIDTx 对"无行"与"历史行(NULL)"都返回空串而不是错误 —— 前者是
//     "切换成功、账本没写"，后者是"本列上线前的历史"，两者都不是失败。
//  3. PersistPublishInputURLTx 在一条语句里写路径并推进版本，返回 RETURNING 的真实新值。

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	presmodel "go_wp/internal/module/presentation/model"
	"go_wp/internal/pipeline"
	"go_wp/public/test/support"
)

func newPresentationModelDB(t *testing.T) (*gorm.DB, *presmodel.Model) {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	return db, presmodel.NewModel(db)
}

// seedInstance 用真实表插入一条最小实例行（不经过 service，测试只关心 model 形态）。
func seedInstance(t *testing.T, db *gorm.DB, projectID, id string) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO presentation_instances
		(id, project_id, entity_type, entity_id, template_id, url_path, render_mode,
		 stale, input_version, create_time, update_time)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		id, projectID, "article", "e-"+id, "t-"+id, "/seed", "template",
		false, 0, now, now).Error; err != nil {
		t.Fatalf("插入实例失败: %v", err)
	}
}

// TestMarkPublishedLangTxWritesReceiptID 事务内写成功 attempt 身份。
func TestMarkPublishedLangTxWritesReceiptID(t *testing.T) {
	db, m := newPresentationModelDB(t)
	ctx := context.Background()
	const pid = "11111111-1111-1111-1111-111111111111"
	const iid = "22222222-2222-2222-2222-222222222222"
	seedInstance(t, db, pid, iid)
	now := time.Now().UTC()

	err := m.Transaction(ctx, func(tx *gorm.DB) error {
		if merr := m.MarkPublishedLangTx(tx, presmodel.PublicationRecord{
			PresentationID: iid, Lang: "zh-CN", ActivePath: "/seed",
			ArtifactID: "a1", ArtifactHash: "h1", ReceiptID: "42", PublishedAt: now,
		}); merr != nil {
			return merr
		}
		got, rerr := m.PublishedReceiptIDTx(tx, iid, "zh-CN")
		if rerr != nil {
			return rerr
		}
		if got != "42" {
			t.Fatalf("事务内读回的 receipt_id 应为 42，实际 %q", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("写入语言账本失败: %v", err)
	}
}

// TestMarkPublishedLangTxWithoutReceiptWritesNull 空串必须写 NULL（不是空字符串）。
func TestMarkPublishedLangTxWithoutReceiptWritesNull(t *testing.T) {
	db, m := newPresentationModelDB(t)
	ctx := context.Background()
	const pid = "33333333-3333-3333-3333-333333333333"
	const iid = "44444444-4444-4444-4444-444444444444"
	seedInstance(t, db, pid, iid)
	now := time.Now().UTC()

	err := m.Transaction(ctx, func(tx *gorm.DB) error {
		if merr := m.MarkPublishedLangTx(tx, presmodel.PublicationRecord{
			PresentationID: iid, Lang: "zh-CN", ActivePath: "/seed",
			ArtifactID: "a2", ArtifactHash: "h2", PublishedAt: now,
		}); merr != nil {
			return merr
		}
		got, rerr := m.PublishedReceiptIDTx(tx, iid, "zh-CN")
		if rerr != nil {
			return rerr
		}
		if got != "" {
			t.Fatalf("没有回执时应读回空串，实际 %q", got)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("写入语言账本失败: %v", err)
	}
	var isNull bool
	if err := db.Raw(`SELECT receipt_id IS NULL FROM presentation_publications
		WHERE presentation_id = ? AND lang = ?`, iid, "zh-CN").Scan(&isNull).Error; err != nil {
		t.Fatalf("读取 receipt_id 空值形态失败: %v", err)
	}
	if !isNull {
		t.Fatalf("无回执必须写 NULL 而不是空字符串")
	}
}

// seedArtifact 用真实产物表插入一条最小产物行（依赖表的复合外键指向它）。
//
// 这里照抄真实列集而不是走 AutoMigrate：表结构来自生产迁移，测试只负责满足 NOT NULL
// 与外键（快照行同样先插一条）。
func seedArtifact(t *testing.T, db *gorm.DB, presentationID, artifactID string) {
	t.Helper()
	now := time.Now().UTC()
	snapshotID := "5ee5ee5e-0000-4000-8000-0000000000" + artifactID[len(artifactID)-2:]
	templateVersionID := "7e17e17e-0000-4000-8000-0000000000" + artifactID[len(artifactID)-2:]
	if err := db.Exec(`INSERT INTO document_snapshots
		(id, presentation_instance_id, source_template_version_id, source_entity_revision_id,
		 document, created_at)
		VALUES (?,?,?,?,?,?)`, snapshotID, presentationID, templateVersionID,
		"00000000-0000-4000-8000-0000000000"+artifactID[len(artifactID)-2:],
		[]byte(`{}`), now).Error; err != nil {
		t.Fatalf("插入快照行失败: %v", err)
	}
	if err := db.Exec(`INSERT INTO presentation_artifacts
		(id, presentation_instance_id, snapshot_id, version, source_hash, build_input_manifest,
		 build_input_hash, artifact_provider, artifact_key, artifact_hash, compiler_version,
		 registry_version, manifest, created_by, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, artifactID, presentationID, snapshotID, 1,
		"src-"+artifactID, []byte(`{}`), "bih-"+artifactID, "local", "artifacts/"+artifactID,
		"hash-"+artifactID, "c1", "r1", []byte(`{}`), artifactID, now).Error; err != nil {
		t.Fatalf("插入产物行失败: %v", err)
	}
}

// seedDependency 用真实依赖表登记"某实例的某条产物消费了 (kind,key)"。
//
// 走真实表而不是给 model 加测试后门：按项/按位置标记的命中口径就是这张表 +
// presentationDepScope 的连接，伪造一份内存映射证明不了真实 SQL。
func seedDependency(t *testing.T, db *gorm.DB, presentationID, artifactID, kind, key string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO presentation_dependencies
		(presentation_id, artifact_id, dependency_kind, dependency_key, create_time)
		VALUES (?,?,?,?,?)`, presentationID, artifactID, kind, key, time.Now().UTC()).Error; err != nil {
		t.Fatalf("登记依赖失败: %v", err)
	}
}

// TestMarkInstanceNavigationStaleTxMarksOnlyDeclaringInstances 按项导航标记：只命中声明了
// 该 navigation id 的实例，键必须与 pipeline.NavigationKey 逐字一致，且同 tx 原子 +1 版本。
func TestMarkInstanceNavigationStaleTxMarksOnlyDeclaringInstances(t *testing.T) {
	db, m := newPresentationModelDB(t)
	ctx := context.Background()
	const pid = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	const hit = "cccccccc-cccc-cccc-cccc-cccccccccccc"
	const miss = "dddddddd-dddd-dddd-dddd-dddddddddddd"
	const navID = "12312312-1231-1231-1231-123123123123"
	seedInstance(t, db, pid, hit)
	seedInstance(t, db, pid, miss)
	key := pipeline.NavigationKey(navID)
	if key.Kind != pipeline.DepKindNavigation {
		t.Fatalf("依赖种类必须来自 pipeline 常量，实际 %q", key.Kind)
	}
	// 常量一致性由**行为**证明：下面按 pipeline.NavigationKey 造依赖行、再调 model 的
	// MarkInstanceNavigationStaleTx，若 model 里的种类字面量与 pipeline 不同，命中 0 行、
	// 断言必然失败。不为这条断言在生产代码里开测试后门（导出只剩给测试用的常量）。
	// 依赖表对 (artifact_id, presentation_id) 有复合外键：先插一条真实的产物行。
	seedArtifact(t, db, hit, "art-"+hit)
	seedDependency(t, db, hit, "art-"+hit, key.Kind, key.Key)

	var before int64
	if err := db.Raw("SELECT input_version FROM presentation_instances WHERE id = ?", hit).
		Scan(&before).Error; err != nil {
		t.Fatalf("读取版本失败: %v", err)
	}

	var n int64
	err := m.Transaction(ctx, func(tx *gorm.DB) error {
		var merr error
		n, merr = m.MarkInstanceNavigationStaleTx(ctx, tx, pid, navID, time.Now().UTC())
		return merr
	})
	if err != nil {
		t.Fatalf("按项导航标记失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("只应命中声明了该导航项的实例，实际命中 %d 行", n)
	}
	var after struct {
		Version int64
		Stale   bool
	}
	if err := db.Raw("SELECT input_version AS version, stale FROM presentation_instances WHERE id = ?", hit).
		Scan(&after).Error; err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if !after.Stale || after.Version != before+1 {
		t.Fatalf("标记必须 stale=true 且版本 +1：stale=%v before=%d after=%d",
			after.Stale, before, after.Version)
	}
	var missRow struct {
		Stale   bool
		Version int64
	}
	if err := db.Raw("SELECT stale, input_version AS version FROM presentation_instances WHERE id = ?", miss).
		Scan(&missRow).Error; err != nil {
		t.Fatalf("回读未声明实例失败: %v", err)
	}
	if missRow.Stale || missRow.Version != 0 {
		t.Fatalf("未声明该导航项的实例不得被标记：stale=%v version=%d", missRow.Stale, missRow.Version)
	}
}

// TestMarkInstanceNavigationStaleTxRejectsBadInput 参数校验：空工程/空 id 报错，不静默全站标记。
func TestMarkInstanceNavigationStaleTxRejectsBadInput(t *testing.T) {
	db, m := newPresentationModelDB(t)
	ctx := context.Background()
	const pid = "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	const iid = "ffffffff-ffff-ffff-ffff-ffffffffffff"
	seedInstance(t, db, pid, iid)

	err := m.Transaction(ctx, func(tx *gorm.DB) error {
		if _, nerr := m.MarkInstanceNavigationStaleTx(ctx, tx, "", "nav", time.Now().UTC()); nerr == nil {
			t.Fatalf("空工程必须报错")
		}
		if _, nerr := m.MarkInstanceNavigationStaleTx(ctx, tx, pid, "  ", time.Now().UTC()); nerr == nil {
			t.Fatalf("空导航 id 必须报错")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("事务失败: %v", err)
	}
}

// TestPersistPublishInputURLTxAdvancesVersion 一条语句写入意图并推进版本，返回真实新值。
func TestPersistPublishInputURLTxAdvancesVersion(t *testing.T) {
	db, m := newPresentationModelDB(t)
	ctx := context.Background()
	const pid = "55555555-5555-5555-5555-555555555555"
	const iid = "66666666-6666-6666-6666-666666666666"
	seedInstance(t, db, pid, iid)

	var got int64
	err := m.Transaction(ctx, func(tx *gorm.DB) error {
		v, perr := m.PersistPublishInputURLTx(tx, pid, iid, "/moved", time.Now().UTC())
		got = v
		return perr
	})
	if err != nil {
		t.Fatalf("持久化输入意图失败: %v", err)
	}
	if got != 1 {
		t.Fatalf("首次推进应从 0 到 1，实际 %d", got)
	}
	var row struct {
		URLPath      string
		InputVersion int64
	}
	if err := db.Raw(`SELECT url_path, input_version FROM presentation_instances WHERE id = ?`, iid).
		Scan(&row).Error; err != nil {
		t.Fatalf("回读实例失败: %v", err)
	}
	if row.URLPath != "/moved" || row.InputVersion != got {
		t.Fatalf("落库事实应与返回值一致：path=%q version=%d（返回 %d）", row.URLPath, row.InputVersion, got)
	}
}

// TestPersistPublishInputURLTxRequiresRange 缺工程/路径必须失败（作用域是安全边界，不是可选项）。
func TestPersistPublishInputURLTxRequiresRange(t *testing.T) {
	db, m := newPresentationModelDB(t)
	const pid = "77777777-7777-7777-7777-777777777777"
	const iid = "88888888-8888-8888-8888-888888888888"
	seedInstance(t, db, pid, iid)

	if _, err := m.PersistPublishInputURLTx(db, pid, iid, "   ", time.Now().UTC()); err == nil {
		t.Fatalf("空路径必须报错")
	}
	if _, err := m.PersistPublishInputURLTx(db, "", iid, "/x", time.Now().UTC()); err == nil {
		t.Fatalf("缺工程作用域必须报错")
	}
}

// TestMarkStaleWithoutVersionKeepsVersion 补偿标记只动 stale，不动 input_version。
func TestMarkStaleWithoutVersionKeepsVersion(t *testing.T) {
	db, m := newPresentationModelDB(t)
	ctx := context.Background()
	const pid = "99999999-9999-9999-9999-999999999999"
	const iid = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	seedInstance(t, db, pid, iid)

	if _, err := m.MarkStale(ctx, pid, []string{iid}, time.Now().UTC()); err != nil {
		t.Fatalf("MarkStale 失败: %v", err)
	}
	var v1 int64
	if err := db.Raw("SELECT input_version FROM presentation_instances WHERE id = ?", iid).Scan(&v1).Error; err != nil {
		t.Fatalf("读取版本失败: %v", err)
	}
	if _, err := m.MarkStaleWithoutVersion(ctx, pid, []string{iid}, time.Now().UTC()); err != nil {
		t.Fatalf("MarkStaleWithoutVersion 失败: %v", err)
	}
	var row struct {
		V     int64
		Stale bool
	}
	if err := db.Raw("SELECT input_version AS v, stale FROM presentation_instances WHERE id = ?", iid).
		Scan(&row).Error; err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if !row.Stale {
		t.Fatalf("补偿标记必须让实例保持 stale")
	}
	if row.V != v1 {
		t.Fatalf("补偿标记不得推进版本：before=%d after=%d", v1, row.V)
	}
}
