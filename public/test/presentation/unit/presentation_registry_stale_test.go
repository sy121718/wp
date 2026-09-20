package unit

// presentation_registry_stale_test.go — 自动发布实例的组件升级收敛入口（报告 ARCH-03）。
//
// 报告指出：实例的产物行保存了 registry_version，但启动期没有任何比对入口 ——
// 手工 Page 有 MarkStaleByRegistryVersion，自动实例没有，于是组件升级后商品详情页
// 一直是旧组件渲染的字节且日志里什么都没有。本用例守住这条对等入口的判定口径：
// 只看语言账本（presentation_publications）当前指向的产物，历史 available 行不参与。

import (
	"context"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"

	"go_wp/internal/builder"
	"go_wp/pkg/i18n"
)

// instanceStale 实例当前的 stale 标记。
func instanceStale(t *testing.T, f *presFixture, instanceID string) bool {
	t.Helper()
	var stale bool
	if err := f.db.Raw("SELECT stale FROM presentation_instances WHERE id = ?", instanceID).Scan(&stale).Error; err != nil {
		t.Fatalf("查询实例 stale 失败: %v", err)
	}
	return stale
}

// publishedArtifactID 实例某语言语言账本指向的产物行 id。
func publishedArtifactID(t *testing.T, f *presFixture, instanceID, lang string) string {
	t.Helper()
	var id string
	if err := f.db.Raw("SELECT artifact_id::text FROM presentation_publications WHERE presentation_id = ? AND lang = ?",
		instanceID, lang).Scan(&id).Error; err != nil {
		t.Fatalf("查询语言账本失败: %v", err)
	}
	if id == "" {
		t.Fatalf("实例 %s 的 %s 语言账本没有产物指针", instanceID, lang)
	}
	return id
}

// TestMarkStaleByRegistryVersionOnInstance 自动实例的启动收敛：只看账本当前产物。
func TestMarkStaleByRegistryVersionOnInstance(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain)
	t.Cleanup(func() { i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain) })

	if err := f.db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, create_time, update_time) "+
			"VALUES (?, ?, 0, true, true, now(), now()), (?, ?, 1, false, true, now(), now())",
		f.projectID, "zh-CN", f.projectID, "en-US").Error; err != nil {
		t.Fatalf("写入语言清单失败: %v", err)
	}
	f.createTemplate(t)
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "registry-stale", Data: map[string]any{"title": "组件升级"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: "/products/registry-stale",
	})
	if err != nil {
		t.Fatalf("CreateInstance 失败: %v", err)
	}
	current := builder.RegistryVersion()
	zhArtifact := publishedArtifactID(t, f, inst.ID, "zh-CN")
	enArtifact := publishedArtifactID(t, f, inst.ID, "en-US")

	// 历史产物：上一轮组件产出、未被账本指向、仍 available（未 GC）。
	if err := f.db.Exec(`INSERT INTO presentation_artifacts (
		id, presentation_instance_id, snapshot_id, version, lang, source_hash,
		build_input_hash, artifact_provider, artifact_key, artifact_hash,
		compiler_version, registry_version, manifest, payload_state, payload_deleted_at, note, created_by, create_time)
	SELECT gen_random_uuid(), presentation_instance_id, snapshot_id, version + 100, lang, source_hash,
		build_input_hash, artifact_provider, artifact_key || '-history', artifact_hash || '-history',
		compiler_version, 'old-registry-history', manifest, payload_state, payload_deleted_at, note, created_by, create_time
	FROM presentation_artifacts WHERE id = ?`, zhArtifact).Error; err != nil {
		t.Fatalf("插入历史产物失败: %v", err)
	}

	// ① 历史 available 行不参与判定：两语言账本都指向当前版本产物 → 不该命中。
	ids, err := f.pres.MarkStaleByRegistryVersion(ctx, current)
	if err != nil {
		t.Fatalf("启动收敛失败: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("账本指向当前版本产物，历史行不该参与判定，实际命中 %v", ids)
	}
	if instanceStale(t, f, inst.ID) {
		t.Fatal("实例被误标为 stale（历史产物行仍在 available，但账本已指向新产物）")
	}

	// ② 仅另一语言（en-US）的产物由旧组件产出 → 仍必须命中。
	if err := f.db.Exec("UPDATE presentation_artifacts SET registry_version = ? WHERE id = ?", "old-registry-en", enArtifact).Error; err != nil {
		t.Fatalf("改写产物版本失败: %v", err)
	}
	ids, err = f.pres.MarkStaleByRegistryVersion(ctx, current)
	if err != nil {
		t.Fatalf("启动收敛失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != inst.ID {
		t.Fatalf("仅 en-US 由旧组件产出也必须命中实例 %s，实际 %v", inst.ID, ids)
	}
	if !instanceStale(t, f, inst.ID) {
		t.Fatal("实例应被标记为 stale")
	}

	// ③ 重建收敛：模拟 en-US 用新组件重建（新产物行 + 账本改指它），再跑一次不该标记。
	if err := f.db.Exec("UPDATE presentation_instances SET stale = false WHERE id = ?", inst.ID).Error; err != nil {
		t.Fatalf("清理 stale 失败: %v", err)
	}
	var enRebuilt string
	if err := f.db.Raw(`INSERT INTO presentation_artifacts (
		id, presentation_instance_id, snapshot_id, version, lang, source_hash,
		build_input_hash, artifact_provider, artifact_key, artifact_hash,
		compiler_version, registry_version, manifest, payload_state, payload_deleted_at, note, created_by, create_time)
	SELECT gen_random_uuid(), presentation_instance_id, snapshot_id, version + 200, lang, source_hash,
		build_input_hash, artifact_provider, artifact_key || '-rebuilt', artifact_hash || '-rebuilt',
		compiler_version, ?, manifest, payload_state, payload_deleted_at, note, created_by, create_time
	FROM presentation_artifacts WHERE id = ?
	RETURNING id`, current, enArtifact).Scan(&enRebuilt).Error; err != nil {
		t.Fatalf("插入重建后的产物失败: %v", err)
	}
	if err := f.db.Exec("UPDATE presentation_publications SET artifact_id = ? WHERE presentation_id = ? AND lang = 'en-US'",
		enRebuilt, inst.ID).Error; err != nil {
		t.Fatalf("改指语言账本失败: %v", err)
	}
	ids, err = f.pres.MarkStaleByRegistryVersion(ctx, current)
	if err != nil {
		t.Fatalf("启动收敛失败: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("各语言账本都已指向当前版本产物，不该再标记，实际 %v", ids)
	}
	if instanceStale(t, f, inst.ID) {
		t.Fatal("收敛后实例不该再是 stale")
	}
}
