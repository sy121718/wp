package unit

// rollback_ledger_identity_test.go — 产物回滚的**账本身份**判据（真实 PostgreSQL + 生产 DDL）。
//
// RollbackArtifact 只切指针、不重编译，所以它唯一能证明"这次切换仍然成立"的东西就是
// 账本行的身份：presentation_publications.receipt_id（迁移 700）+ artifact_id。
//
// 判据是 (receipt_id, artifact_id) 这一**对**：
//   · 两次不同的成功 attempt 可以命中同一份产物（内容寻址 + 同版本重建），
//     只比 artifact 分不出"镜像里的账本是我写的"还是"另一次成功发布写的" —— 必须拒绝；
//   · 700 之前的账本行 receipt_id 为空，按历史兼容退化为 artifact 比较。
//
// 本文件造的是**半失效账本**（presentation_publications）而实例行保持新鲜：
// 早先回滚只比实例行（active_artifact_id / artifact_hash），这种形态它是看不出来的，
// 会直接把旧产物盖到线上并让账本与访问面长期不一致。所以负测必须落在账本上。

import (
	"context"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationservice "go_wp/internal/module/presentation/service"
)

// seedLedger 直接写一行账本（半失效账本的唯一可信造法：绕过发布链，只改账本事实）。
func seedLedger(t *testing.T, f *presFixture, instanceID, lang, artifactID, receiptID string) {
	t.Helper()
	var art *string
	if artifactID != "" {
		art = &artifactID
	}
	var rid *string
	if receiptID != "" {
		rid = &receiptID
	}
	if err := f.db.Exec(`INSERT INTO presentation_publications
		(presentation_id, lang, active_path, artifact_id, artifact_hash, published_at, update_time, receipt_id)
		VALUES (?, ?, ?, ?, '', now(), now(), ?)
		ON CONFLICT (presentation_id, lang) DO UPDATE SET
			artifact_id = EXCLUDED.artifact_id,
			receipt_id  = EXCLUDED.receipt_id,
			update_time = now()`,
		instanceID, lang, "/rollback-ledger", art, rid).Error; err != nil {
		t.Fatalf("写账本行失败（lang=%s）: %v", lang, err)
	}
}

// readLedger 读回某语言的账本身份，用于断言"拒绝时账本一行没动"。
func readLedger(t *testing.T, f *presFixture, instanceID, lang string) (artifactID, receiptID string) {
	t.Helper()
	var art, rid *string
	if err := f.db.Raw(`SELECT artifact_id::text, receipt_id::text FROM presentation_publications
		WHERE presentation_id = ? AND lang = ?`, instanceID, lang).Row().Scan(&art, &rid); err != nil {
		t.Fatalf("读账本行失败（lang=%s）: %v", lang, err)
	}
	if art != nil {
		artifactID = *art
	}
	if rid != nil {
		receiptID = *rid
	}
	return artifactID, receiptID
}

// prepareRollbackFixture 建一个已发布的 article 实例，返回实例与其当前产物 hash。
func prepareRollbackFixture(t *testing.T, slug, urlPath string) (*presFixture, string, string) {
	t.Helper()
	f := newPresFixture(t)
	if f == nil {
		return nil, "", ""
	}
	ctx := context.Background()
	f.createTemplate(t)
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: slug, Data: map[string]any{"title": "回滚身份"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: urlPath,
	})
	if err != nil {
		t.Fatalf("CreateInstance 失败: %v", err)
	}
	if inst.ArtifactHash == "" {
		t.Fatal("发布后实例没有产物 hash")
	}
	return f, inst.ID, inst.ArtifactHash
}

// TestRollbackRejectsLedgerTakenByAnotherAttempt 同一份产物、不同成功 attempt → 拒绝且不动访问面。
//
// 这是"只比 artifact 会漏掉"的那一类：artifact_id 与实例行完全一致，
// 只有 receipt_id 变了（另一次成功发布写了账本）。回滚必须视为受控冲突。
func TestRollbackRejectsLedgerTakenByAnotherAttempt(t *testing.T) {
	f, instanceID, hash := prepareRollbackFixture(t, "rollback-identity", "/rollback-identity")
	if f == nil {
		return
	}
	ctx := context.Background()

	// 造"另一次成功 attempt 写了账本"：artifact 不变，receipt_id 变成一个更晚的 id。
	var ownArtifact string
	if err := f.db.Raw(`SELECT artifact_id::text FROM presentation_publications
		WHERE presentation_id = ? AND lang = 'zh-CN'`, instanceID).Scan(&ownArtifact).Error; err != nil {
		t.Fatalf("读当前账本失败: %v", err)
	}
	if ownArtifact == "" {
		t.Fatal("发布后账本应已有 artifact_id")
	}
	// 账本换成"另一次成功 attempt"那一份：artifact 完全相同，只有 receipt_id 变了。
	// 只比 artifact 的实现会在这里放行，并让账本与线上不一致。
	seedLedger(t, f, instanceID, "zh-CN", ownArtifact, "700002")

	// 该形态能否被拒，取决于 From 与 Own 是否真的不同 —— 单线程下两次读都在锁外/锁内
	// 同一份数据上，所以这里直接验证判据函数本身（真实调用路径见交错测试）。
	from := []presentationservice.LedgerIdentity{{Lang: "zh-CN", ReceiptID: "700001", ArtifactID: ownArtifact}}
	own := []presentationservice.LedgerIdentity{{Lang: "zh-CN", ReceiptID: "700002", ArtifactID: ownArtifact}}
	if cerr := presentationservice.CompareRollbackLedger(from, own); cerr == nil {
		t.Fatal("同产物、不同 receipt_id 的账本必须被判为受控冲突")
	}

	_, err := f.pres.RollbackArtifact(ctx, &presentationdto.RollbackArtifactReq{
		ProjectID: f.projectID, InstanceID: instanceID, TargetHash: hash,
	})
	// 单线程下 From == Own（都是 700002），所以这里**允许**成功：回滚确实基于同一代。
	// 断言点仍在"账本没被回滚改写"，不是"被拒"。
	if err != nil {
		t.Logf("单线程形态下回滚也可以被其它判据拒绝: %v", err)
	}

	// 无论拒绝与否，账本必须还是"另一次 attempt"那一份：不许被回滚改写。
	art, rid := readLedger(t, f, instanceID, "zh-CN")
	if rid != "700002" {
		t.Fatalf("账本 receipt_id 被回滚改写：想要 700002，实际 %q（artifact=%s）", rid, art)
	}
	if art != ownArtifact {
		t.Fatalf("账本 artifact_id 被回滚改写：想要 %s，实际 %s", ownArtifact, art)
	}
}

// TestRollbackRejectsArtifactDriftWithLegacyLedger 历史账本（无 receipt_id）退化为产物比较。
func TestRollbackRejectsArtifactDriftWithLegacyLedger(t *testing.T) {
	f, instanceID, hash := prepareRollbackFixture(t, "rollback-legacy", "/rollback-legacy")
	if f == nil {
		return
	}
	ctx := context.Background()

	// 历史行：receipt_id 为空（700 之前写的账本），artifact 指向另一个产物。
	seedLedger(t, f, instanceID, "zh-CN", "00000000-0000-0000-0000-0000000070a1", "")

	// 单线程下 From 与 Own 是同一次调用里读到的同一份数据，判据不可能因此变红 ——
	// 这里直接验证判据函数本身（真实交错路径见移动窗口类用例），回滚调用本身允许成功。
	if cerr := presentationservice.CompareRollbackLedger(
		[]presentationservice.LedgerIdentity{{Lang: "zh-CN", ArtifactID: "00000000-0000-0000-0000-0000000070a1"}},
		[]presentationservice.LedgerIdentity{{Lang: "zh-CN", ArtifactID: "00000000-0000-0000-0000-0000000070a2"}},
	); cerr == nil {
		t.Fatal("历史账本的产物在回滚期间漂移，判据必须拒绝（退化档只比 artifact）")
	}
	_, err := f.pres.RollbackArtifact(ctx, &presentationdto.RollbackArtifactReq{
		ProjectID: f.projectID, InstanceID: instanceID, TargetHash: hash,
	})
	_ = err // 判据通过与否不影响"账本不得被回滚改写"这条断言
	art, rid := readLedger(t, f, instanceID, "zh-CN")
	if art != "00000000-0000-0000-0000-0000000070a1" {
		t.Fatalf("账本 artifact_id 被回滚改写：实际 %s", art)
	}
	if rid != "" {
		t.Fatalf("历史账本的 receipt_id 不应被回滚补造：实际 %q", rid)
	}
}

// TestRollbackRejectsLedgerLanguageSetDrift 账本语言集合在回滚期间变化 → 拒绝。
//
// 语言集合是判据的一部分：新增语言说明"这次回滚基于的账本形态"已经不成立，
// 继续切指针会让新语言的账本与指针脱节。
func TestRollbackRejectsLedgerLanguageSetDrift(t *testing.T) {
	f, instanceID, hash := prepareRollbackFixture(t, "rollback-langs", "/rollback-langs")
	if f == nil {
		return
	}
	ctx := context.Background()

	// 在锁外读 From 之后新增一条语言账本：这里用一个必定被清理的额外语言模拟。
	seedLedger(t, f, instanceID, "de-DE", "", "700003")
	defer func() {
		_ = f.db.Exec(`DELETE FROM presentation_publications WHERE presentation_id = ? AND lang = 'de-DE'`, instanceID).Error
	}()

	// 同上：语言集合漂移的判据在单线程形态下测不到，直接验证判据函数。
	if cerr := presentationservice.CompareRollbackLedger(
		[]presentationservice.LedgerIdentity{{Lang: "zh-CN", ReceiptID: "700001", ArtifactID: "00000000-0000-0000-0000-0000000070b1"}},
		[]presentationservice.LedgerIdentity{
			{Lang: "zh-CN", ReceiptID: "700001", ArtifactID: "00000000-0000-0000-0000-0000000070b1"},
			{Lang: "de-DE", ReceiptID: "700003", ArtifactID: "00000000-0000-0000-0000-0000000070b2"},
		},
	); cerr == nil {
		t.Fatal("账本语言集合在回滚期间变化，判据必须拒绝")
	}
	_, err := f.pres.RollbackArtifact(ctx, &presentationdto.RollbackArtifactReq{
		ProjectID: f.projectID, InstanceID: instanceID, TargetHash: hash,
	})
	_ = err
	if _, rid := readLedger(t, f, instanceID, "de-DE"); rid != "700003" {
		t.Fatalf("额外语言账本被回滚改写：实际 %q", rid)
	}
}

// TestRollbackAcceptsOwnLedgerIdentity 同一身份（receipt_id + artifact 都对得上）→ 允许。
//
// 正对照：负测必须能区分"因为身份不符被拒"与"回滚本来就跑不通"。
func TestRollbackAcceptsOwnLedgerIdentity(t *testing.T) {
	f, instanceID, hash := prepareRollbackFixture(t, "rollback-own", "/rollback-own")
	if f == nil {
		return
	}
	ctx := context.Background()

	var ownArtifact string
	if err := f.db.Raw(`SELECT artifact_id::text FROM presentation_publications
		WHERE presentation_id = ? AND lang = 'zh-CN'`, instanceID).Scan(&ownArtifact).Error; err != nil {
		t.Fatalf("读当前账本失败: %v", err)
	}
	seedLedger(t, f, instanceID, "zh-CN", ownArtifact, "700004")

	if _, err := f.pres.RollbackArtifact(ctx, &presentationdto.RollbackArtifactReq{
		ProjectID: f.projectID, InstanceID: instanceID, TargetHash: hash,
	}); err != nil {
		t.Fatalf("身份一致的产物回滚应当成功: %v", err)
	}
	if got := activeHTML(t, "/rollback-own"); got == "" {
		t.Fatal("回滚后访问面应有产物")
	}
}
