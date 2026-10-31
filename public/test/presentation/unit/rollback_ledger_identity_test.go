package unit

import (
	"context"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationservice "go_wp/internal/module/presentation/service"
)

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

func TestRollbackRejectsLedgerTakenByAnotherAttempt(t *testing.T) {
	f, instanceID, hash := prepareRollbackFixture(t, "rollback-identity", "/rollback-identity")
	if f == nil {
		return
	}
	ctx := context.Background()

	var ownArtifact string
	if err := f.db.Raw(`SELECT artifact_id::text FROM presentation_publications
		WHERE presentation_id = ? AND lang = 'zh-CN'`, instanceID).Scan(&ownArtifact).Error; err != nil {
		t.Fatalf("读当前账本失败: %v", err)
	}
	if ownArtifact == "" {
		t.Fatal("发布后账本应已有 artifact_id")
	}

	seedLedger(t, f, instanceID, "zh-CN", ownArtifact, "700002")

	from := []presentationservice.LedgerIdentity{{Lang: "zh-CN", ReceiptID: "700001", ArtifactID: ownArtifact}}
	own := []presentationservice.LedgerIdentity{{Lang: "zh-CN", ReceiptID: "700002", ArtifactID: ownArtifact}}
	if cerr := presentationservice.CompareRollbackLedger(from, own); cerr == nil {
		t.Fatal("同产物、不同 receipt_id 的账本必须被判为受控冲突")
	}

	_, err := f.pres.RollbackArtifact(ctx, &presentationdto.RollbackArtifactReq{
		ProjectID: f.projectID, InstanceID: instanceID, TargetHash: hash,
	})

	if err != nil {
		t.Logf("单线程形态下回滚也可以被其它判据拒绝: %v", err)
	}

	art, rid := readLedger(t, f, instanceID, "zh-CN")
	if rid != "700002" {
		t.Fatalf("账本 receipt_id 被回滚改写：想要 700002，实际 %q（artifact=%s）", rid, art)
	}
	if art != ownArtifact {
		t.Fatalf("账本 artifact_id 被回滚改写：想要 %s，实际 %s", ownArtifact, art)
	}
}

func TestRollbackRejectsArtifactDriftWithLegacyLedger(t *testing.T) {
	f, instanceID, hash := prepareRollbackFixture(t, "rollback-legacy", "/rollback-legacy")
	if f == nil {
		return
	}
	ctx := context.Background()

	seedLedger(t, f, instanceID, "zh-CN", "00000000-0000-0000-0000-0000000070a1", "")

	if cerr := presentationservice.CompareRollbackLedger(
		[]presentationservice.LedgerIdentity{{Lang: "zh-CN", ArtifactID: "00000000-0000-0000-0000-0000000070a1"}},
		[]presentationservice.LedgerIdentity{{Lang: "zh-CN", ArtifactID: "00000000-0000-0000-0000-0000000070a2"}},
	); cerr == nil {
		t.Fatal("历史账本的产物在回滚期间漂移，判据必须拒绝（退化档只比 artifact）")
	}
	_, err := f.pres.RollbackArtifact(ctx, &presentationdto.RollbackArtifactReq{
		ProjectID: f.projectID, InstanceID: instanceID, TargetHash: hash,
	})
	_ = err
	art, rid := readLedger(t, f, instanceID, "zh-CN")
	if art != "00000000-0000-0000-0000-0000000070a1" {
		t.Fatalf("账本 artifact_id 被回滚改写：实际 %s", art)
	}
	if rid != "" {
		t.Fatalf("历史账本的 receipt_id 不应被回滚补造：实际 %q", rid)
	}
}

func TestRollbackRejectsLedgerLanguageSetDrift(t *testing.T) {
	f, instanceID, hash := prepareRollbackFixture(t, "rollback-langs", "/rollback-langs")
	if f == nil {
		return
	}
	ctx := context.Background()

	seedLedger(t, f, instanceID, "de-DE", "", "700003")
	defer func() {
		_ = f.db.Exec(`DELETE FROM presentation_publications WHERE presentation_id = ? AND lang = 'de-DE'`, instanceID).Error
	}()

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
