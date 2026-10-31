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
//
// 引用链必须是真的：presentation_instances.template_id → content_templates(id) →
// projects(id)，且 entity_id 是 uuid 列。以前这里塞 "e-"+id / "t-"+id 这类假 id，
// 插第一行就报 `invalid input syntax for type uuid: "e-cccccccc-…"` —— 断言还没跑到。
// 真实父行由 support.SeedProjectRow + seedTemplate 补，不手抄 CREATE TABLE。
//
// url_path 取 id 派生值而不是固定 "/seed"：表上有 UNIQUE (project_id, url_path)，
// 同一工程插两个实例（命中/未命中两次 seedInstance）时固定路径会撞唯一键，
// 那与本用例要验的行为无关。
func seedInstance(t *testing.T, db *gorm.DB, projectID, id string) {
	t.Helper()
	seedProjectOnce(t, db, projectID)
	templateID := seedTemplate(t, db, projectID)
	now := time.Now().UTC()
	// entity_id 是 uuid 列，用固定前缀 + id 后 12 位派生（与 id 一一对应，不依赖随机数）。
	entityID := "e0000000-0000-4000-8000-" + id[len(id)-12:]
	if err := db.Exec(`INSERT INTO presentation_instances
		(id, project_id, entity_type, entity_id, template_id, url_path, render_mode,
		 stale, input_version, create_time, update_time)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		id, projectID, "article", entityID, templateID, "/seed/"+id, "template",
		false, 0, now, now).Error; err != nil {
		t.Fatalf("插入实例失败: %v", err)
	}
}

// seedProjectOnce 保证同一工程的父行只种一次（存在即复用）。
//
// 一个用例里会为同一工程插多个实例（命中/未命中），如果每次都调
// support.SeedProjectRow 就会撞 projects_pkey：那是夹具重复，不是隔离语义的问题。
// 刻意**不**换成 ON CONFLICT DO NOTHING —— 那会把「工程本该已存在却没被创建」这类
// 真实夹具错误一起吞掉，看不出到底是复用还是插入失败。
func seedProjectOnce(t *testing.T, db *gorm.DB, projectID string) {
	t.Helper()
	var exists bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM projects WHERE id = ?)", projectID).
		Scan(&exists).Error; err != nil {
		t.Fatalf("查询工程行失败：%v", err)
	}
	if exists {
		return
	}
	support.SeedProjectRow(t, db, projectID, "")
}

// seedTemplate 给工程补一条最小的 content_templates 行（实例的 template_id 外键指向它）。
//
// id 由工程 id 派生 + 存在即复用：一个用例的工程只种一个模板，同一工程插两个实例时
// 第二次不能重复插（主键/默认模板唯一索引都会挡）。
//
// 时间列名按**最终** schema：迁移 205_db019_time_columns_full.sql:20-47 与 :49-66
// （DB-019，YG 拍板「时间列一律 *_time，放弃 *_at」）把 content_templates 的
// created_at/updated_at 都 RENAME 成 create_time/update_time。init_builder_schema.sql:64-65
// 的 created_at/updated_at 只是初始定义，照它写会报
// `column "created_at" of relation "content_templates" does not exist (SQLSTATE 42703)`。
func seedTemplate(t *testing.T, db *gorm.DB, projectID string) string {
	t.Helper()
	templateID := "d0000000-0000-4000-8000-" + projectID[len(projectID)-12:]
	var exists bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM content_templates WHERE id = ?)", templateID).
		Scan(&exists).Error; err != nil {
		t.Fatalf("查询内容模板失败：%v", err)
	}
	if exists {
		return templateID
	}
	if err := db.Exec(`INSERT INTO content_templates
		(id, project_id, name, entity_type, draft_document, create_time, update_time)
		VALUES (?, ?, 'presentation 测试模板', 'article', '{}'::jsonb, NOW(), NOW())`,
		templateID, projectID).Error; err != nil {
		t.Fatalf("准备内容模板失败：%v", err)
	}
	return templateID
}

// seedTemplateVersion 给模板补一条 version=1 的真实版本行，返回其 id。
//
// 为什么必须补：document_snapshots.source_template_version_id 是
// `uuid NOT NULL REFERENCES content_template_versions(id)`（init_builder_schema.sql:153），
// 列**非空**，所以「非模板来源的快照就写 NULL」这条更贴合语义的写法在这里不可行；
// 只派生一个假 uuid 会报
// `violates foreign key constraint "document_snapshots_source_template_version_id_fkey" (SQLSTATE 23503)`。
//
// 列集按**最终** schema 抄最小集：init_builder_schema.sql:68-78 给了 7 列，迁移
// 205_db019_time_columns_full.sql 的 created_at 改名块（:20-47）含本表、updated_at 改名块
// （:49-66）**不含**本表 —— 所以最终只有 create_time，写 update_time 会报 42703。
// created_by 是裸 uuid 列（无外键），固定值即可，与实例/产物的派生前缀区分开。
func seedTemplateVersion(t *testing.T, db *gorm.DB, templateID string) string {
	t.Helper()
	versionID := "7e17e17e-0000-4000-8000-" + templateID[len(templateID)-12:]
	var exists bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM content_template_versions WHERE id = ?)", versionID).
		Scan(&exists).Error; err != nil {
		t.Fatalf("查询模板版本失败：%v", err)
	}
	if exists {
		return versionID
	}
	if err := db.Exec(`INSERT INTO content_template_versions
		(id, template_id, version, document, source_hash, created_by, create_time)
		VALUES (?, ?, 1, '{}'::jsonb, ?, ?, NOW())`,
		versionID, templateID, "seed-hash-"+templateID,
		"c0000000-0000-4000-8000-000000000000").Error; err != nil {
		t.Fatalf("准备模板版本失败：%v", err)
	}
	return versionID
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
			PresentationID: iid, Lang: "zh-CN", ActivePath: "/seed/" + iid,
			ArtifactID:   "a0000001-0000-4000-8000-000000000001",
			ArtifactHash: "h1", ReceiptID: "42", PublishedAt: now,
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
			PresentationID: iid, Lang: "zh-CN", ActivePath: "/seed/" + iid,
			ArtifactID:   "a0000002-0000-4000-8000-000000000002",
			ArtifactHash: "h2", PublishedAt: now,
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
	// artifactID 也必须是合法 uuid：调用点传的是可读别名（"art-"+实例 id），
	// 而 presentation_artifacts.id / created_by 都是 uuid 列 —— 直接塞别名会报
	// invalid input syntax for type uuid。这里在夹具边缘归一成 uuid，
	// 让调用点继续用「可读且唯一」的名字，不必各自记派生规则。
	artifactID = artifactUUID(artifactID)
	// 三个 uuid 一律从 artifactID 的后 12 位派生，且每段前缀不同：
	// 快照表上有 UNIQUE (id, presentation_instance_id)，同一实例种两条产物时
	// snapshotID 撞主键会报 `duplicate key value violates unique constraint` ——
	// 那是夹具的 id 生成问题，不是被测语义。
	suffix := artifactID[len(artifactID)-12:]
	snapshotID := "5ee5ee5e-0000-4000-8000-" + suffix
	// 版本行必须是真实存在的行（见 seedTemplateVersion）：派生 uuid 只是格式合法，外键会挡。
	// 工程从实例行回读，而不是再加参数打穿调用点 —— 夹具只为满足外键，不必扩签名。
	var projectID string
	if err := db.Raw("SELECT project_id::text FROM presentation_instances WHERE id = ?", presentationID).
		Scan(&projectID).Error; err != nil {
		t.Fatalf("读取实例所属工程失败：%v", err)
	}
	templateVersionID := seedTemplateVersion(t, db, seedTemplate(t, db, projectID))
	entityRevisionID := "00000000-0000-4000-8000-" + suffix
	if err := db.Exec(`INSERT INTO document_snapshots
		(id, presentation_instance_id, source_template_version_id, source_entity_revision_id,
		 document, create_time)
		VALUES (?,?,?,?,?,?)`, snapshotID, presentationID, templateVersionID,
		entityRevisionID,
		[]byte(`{}`), now).Error; err != nil {
		t.Fatalf("插入快照行失败: %v", err)
	}
	// 列清单对着**最终** schema 核过，逐项依据见下方注释；照 init_builder_schema.sql:266-287
	// 抄会踩两个坑：build_input_manifest 已被 305 删列，时间列已被 205 改成 create_time。
	//
	//   id / presentation_instance_id / snapshot_id / version / source_hash：初始定义即最终形态
	//     （snapshot_id 由 UNIQUE 复合外键 (id, presentation_instance_id) 指向快照行）。
	//   lang：迁移 155_presentation_i18n.sql:28 加列、:48 SET NOT NULL、:47 默认 'zh-CN'；
	//     有默认值，这里显式给 'zh-CN' 让意图可见（不依赖默认）。
	//   build_input_hash / artifact_provider / artifact_key / artifact_hash /
	//     compiler_version / registry_version / manifest：初始定义即最终形态。
	//   build_input_manifest：**305_manifest_dedup.sql:39 已 DROP COLUMN**（DB-02 判定它与
	//     manifest 同字节且零读取者），所以本 INSERT 不再写它 —— 写了报 42703。
	//   created_by / create_time：created_by 初始即有；时间列由 205_db019_time_columns_full.sql
	//     的 created_at 改名块（:20-47，含 presentation_artifacts）改为 create_time。
	//   payload_state / payload_deleted_at / note：有 DEFAULT 且本用例不关心，不写（也不该写：
	//     payload_state 的 DEFAULT 是 'available'，重复一遍只会多一个可漂移的点）。
	if err := db.Exec(`INSERT INTO presentation_artifacts
		(id, presentation_instance_id, snapshot_id, version, lang, source_hash,
		 build_input_hash, artifact_provider, artifact_key, artifact_hash, compiler_version,
		 registry_version, manifest, created_by, create_time)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, artifactID, presentationID, snapshotID, 1,
		"zh-CN", "src-"+artifactID, "bih-"+artifactID, "local", "artifacts/"+artifactID,
		"hash-"+artifactID, "c1", "r1", []byte(`{}`), artifactID, now).Error; err != nil {
		t.Fatalf("插入产物行失败: %v", err)
	}
}

// artifactUUID 把可读别名映射成合法 uuid：别名先做 FNV-1a 摘要，再铺进固定版本位。
//
// 确定性（同一别名必得同一 uuid，便于回读）且不依赖随机数；与 instances 的 entity_id
// 不同段，避免两类实体撞同一个值。
func artifactUUID(alias string) string {
	const hex = "0123456789abcdef"
	var out [12]byte
	h := uint64(14695981039346656037)
	for i := 0; i < len(alias); i++ {
		h ^= uint64(alias[i])
		h *= 1099511628211
	}
	for i := 11; i >= 0; i-- {
		out[i] = hex[h&0xf]
		h >>= 4
	}
	return "a0000000-0000-4000-8000-" + string(out[:])
}

// seedDependency 用真实依赖表登记"某实例的某条产物消费了 (kind,key)"。
//
// 走真实表而不是给 model 加测试后门：按项/按位置标记的命中口径就是这张表 +
// presentationDepScope 的连接，伪造一份内存映射证明不了真实 SQL。
//
// 时间列是 **last_checked**（不是 create_time，也没有 update_time）：本表记的是"这条
// 依赖上次被校验的时刻"，语义与建行时间不同，init_builder_schema.sql:296-303 里就只给了
// 这一列 timestamptz NOT NULL；205 的时间列改名块也不含本表（它没有 created_at 可改）。
// revision 可空且本用例不关心 → 不写。
func seedDependency(t *testing.T, db *gorm.DB, presentationID, artifactID, kind, key string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO presentation_dependencies
		(presentation_id, artifact_id, dependency_kind, dependency_key, last_checked)
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
	seedDependency(t, db, hit, artifactUUID("art-"+hit), key.Kind, key.Key)

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
