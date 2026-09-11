// presentation_ddl_alignment_test.go — 生产 DDL 对齐 + 依赖 fan-out 端到端。
//
// 三组断言：
//  1. model 的列集合 ⊆ 生产 DDL 的列集合（presentation 四张表）；
//  2. 真实 DDL 下创建实例，project_id / template_id / stale / active_artifact_id
//     等真实列全部落库（修复前 model 写的是不存在的 status / artifact_hash）；
//  3. 内容变更 → pipeline.Fanout → presentation 精确反查 + 自动重建 + 重新发布
//     （与 PIPE-3 的 page 侧链路同形，docs/03-pipeline.md §8.2/§8.3）。
package unit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationmodel "go_wp/internal/module/presentation/model"

	"go_wp/internal/pipeline"
	"go_wp/public/test/support"
)

// TestPresentationModelColumnsSubsetOfProductionDDL model 列集合必须是生产
// DDL 列集合的子集（四张表逐一校验）。
func TestPresentationModelColumnsSubsetOfProductionDDL(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	for _, tc := range []struct {
		table string
		dest  any
	}{
		{"presentation_instances", &presentationmodel.InstanceEntity{}},
		{"document_snapshots", &presentationmodel.SnapshotEntity{}},
		{"presentation_artifacts", &presentationmodel.ArtifactEntity{}},
		{"presentation_dependencies", &presentationmodel.DependencyEntity{}},
	} {
		t.Run(tc.table, func(t *testing.T) {
			support.AssertModelColumnsSubset(t, f.db, tc.table, tc.dest)
		})
	}
}

// TestCreateInstancePersistsRealColumns 真实 DDL 下创建：NOT NULL 外键与状态列全部落库。
func TestCreateInstancePersistsRealColumns(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.createTemplate(t)
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "ddl-shirt", Data: map[string]any{"title": "DDL 衬衫"},
	})
	if err != nil {
		t.Fatalf("创建内容实体失败: %v", err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: "/ddl/shirt",
	})
	if err != nil {
		t.Fatalf("CreateInstance 在真实 DDL 上失败: %v", err)
	}

	var row struct {
		ProjectID         string
		TemplateID        string
		Stale             bool
		ActiveArtifactID  *string
		StagedArtifactID  *string
		CurrentSnapshotID *string
		PublishedAt       *time.Time
	}
	if err := f.db.Raw(`SELECT project_id, template_id, stale, active_artifact_id,
			staged_artifact_id, current_snapshot_id, published_at
		FROM presentation_instances WHERE id = ?`, inst.ID).Scan(&row).Error; err != nil {
		t.Fatalf("读取实例行失败: %v", err)
	}
	if row.ProjectID != f.projectID {
		t.Errorf("project_id 未落库: %q（期望 %q）", row.ProjectID, f.projectID)
	}
	if row.TemplateID == "" {
		t.Errorf("template_id 未落库（NOT NULL 外键）")
	}
	if row.Stale {
		t.Errorf("发布成功后 stale 应为 false")
	}
	if row.ActiveArtifactID == nil || *row.ActiveArtifactID == "" {
		t.Errorf("active_artifact_id 未落库")
	}
	if row.StagedArtifactID == nil || *row.StagedArtifactID == "" {
		t.Errorf("staged_artifact_id 未落库")
	}
	if row.CurrentSnapshotID == nil || *row.CurrentSnapshotID == "" {
		t.Errorf("current_snapshot_id 未落库")
	}
	if row.PublishedAt == nil {
		t.Errorf("published_at 未落库")
	}

	// 产物行与依赖行必须齐备（fan-out 反查的前提）。
	var artifacts, deps int64
	if err := f.db.Raw("SELECT COUNT(*) FROM presentation_artifacts WHERE presentation_instance_id = ?", inst.ID).
		Scan(&artifacts).Error; err != nil {
		t.Fatalf("统计产物行失败: %v", err)
	}
	if artifacts != 1 {
		t.Errorf("产物行应为 1，实际 %d", artifacts)
	}
	if err := f.db.Raw("SELECT COUNT(*) FROM presentation_dependencies WHERE presentation_id = ?", inst.ID).
		Scan(&deps).Error; err != nil {
		t.Fatalf("统计依赖行失败: %v", err)
	}
	if deps == 0 {
		t.Errorf("依赖行不应为空（direct_content 至少一条）")
	}
}

// instanceUpdatedAt 读取实例行的 updated_at。
func instanceUpdatedAt(t *testing.T, f *presFixture, id string) time.Time {
	t.Helper()
	var at time.Time
	if err := f.db.Raw("SELECT updated_at FROM presentation_instances WHERE id = ?", id).Scan(&at).Error; err != nil {
		t.Fatalf("读取实例 updated_at 失败: %v", err)
	}
	return at
}

// countInstanceArtifacts 统计实例的产物行数。
func countInstanceArtifacts(t *testing.T, f *presFixture, id string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM presentation_artifacts WHERE presentation_instance_id = ?", id).
		Scan(&n).Error; err != nil {
		t.Fatalf("统计产物行失败: %v", err)
	}
	return n
}

// TestContentChangeAutoRebuildAndPublish 内容变更 → 精确 fan-out → 自动重建 + 发布。
func TestContentChangeAutoRebuildAndPublish(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	// 接入依赖扇出（编排层装配的同形调用）。
	fanout := pipeline.NewFanout()
	fanout.Register(pipeline.SourceTypePresentation, f.pres)
	fanout.SetRebuilder(pipeline.SourceTypePresentation, f.pres)
	f.content.SetDependencyInvalidator(fanout)

	f.createTemplate(t)
	entityA, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "auto-a", Data: map[string]any{"title": "自动发布 A"},
	})
	if err != nil {
		t.Fatalf("创建实体 A 失败: %v", err)
	}
	entityB, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "auto-b", Data: map[string]any{"title": "自动发布 B"},
	})
	if err != nil {
		t.Fatalf("创建实体 B 失败: %v", err)
	}
	instA, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entityA.ID, URLPath: "/auto/a",
	})
	if err != nil {
		t.Fatalf("创建实例 A 失败: %v", err)
	}
	instB, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entityB.ID, URLPath: "/auto/b",
	})
	if err != nil {
		t.Fatalf("创建实例 B 失败: %v", err)
	}

	// 1) 精确反查：direct_content 键只命中绑定该实体的实例。
	ids, err := f.pres.MarkStaleByDependency(ctx, pipeline.DepKindDirectContent, "article:"+entityA.ID)
	if err != nil {
		t.Fatalf("依赖反查失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != instA.ID {
		t.Fatalf("受影响集合应只含实例 A(%s)，实际 %v", instA.ID, ids)
	}
	// 反查把 A 标成 stale，重建前先复原（后续自动链路会再次标记）。
	if _, err = f.pres.Rebuild(ctx, &presentationdto.RebuildReq{EntityID: entityA.ID}); err != nil {
		t.Fatalf("复原重建失败: %v", err)
	}

	beforeA := instanceUpdatedAt(t, f, instA.ID)
	beforeB := instanceUpdatedAt(t, f, instB.ID)
	artifactsABefore := countInstanceArtifacts(t, f, instA.ID)
	artifactsBBefore := countInstanceArtifacts(t, f, instB.ID)
	if html := activeHTML(t, "/auto/a"); !strings.Contains(html, "自动发布 A") {
		t.Fatalf("初始产物应含实体字段，实际: %s", html)
	}
	time.Sleep(5 * time.Millisecond)

	// 2) 内容 A 变更 → 自动链路（content → fanout → presentation.RebuildStale）。
	if _, err = f.content.Update(ctx, &contentdto.UpdateReq{
		ID: entityA.ID, Data: map[string]any{"title": "自动发布 A v2"},
	}); err != nil {
		t.Fatalf("更新实体 A 失败: %v", err)
	}

	// 3) 受影响实例被重建并重新发布。
	if now := instanceUpdatedAt(t, f, instA.ID); !now.After(beforeA) {
		t.Fatalf("受影响实例未重建：updated_at 未前进（%v → %v）", beforeA, now)
	}
	if html := activeHTML(t, "/auto/a"); !strings.Contains(html, "自动发布 A v2") {
		t.Fatalf("自动重建后访问面应更新为新内容，实际: %s", html)
	}
	// 内容内联进产物字节 → 新 hash → 新增一条产物行。
	if n := countInstanceArtifacts(t, f, instA.ID); n != artifactsABefore+1 {
		t.Fatalf("内容变更应新增一条产物行（%d → %d）", artifactsABefore, n)
	}
	// stale 收敛为 false。
	var staleA bool
	if err = f.db.Raw("SELECT stale FROM presentation_instances WHERE id = ?", instA.ID).Scan(&staleA).Error; err != nil {
		t.Fatalf("读取 stale 失败: %v", err)
	}
	if staleA {
		t.Fatalf("自动重建成功后 stale 应为 false")
	}
	// 4) 无关实例完全不受影响。
	if now := instanceUpdatedAt(t, f, instB.ID); !now.Equal(beforeB) {
		t.Fatalf("无关实例 B 被误重建（updated_at %v → %v）", beforeB, now)
	}
	if n := countInstanceArtifacts(t, f, instB.ID); n != artifactsBBefore {
		t.Fatalf("无关实例 B 产物行数变化（%d → %d）", artifactsBBefore, n)
	}
	if html := activeHTML(t, "/auto/b"); !strings.Contains(html, "自动发布 B") {
		t.Fatalf("无关实例 B 访问面内容不应变化，实际: %s", html)
	}
}

// TestDeleteInstanceCascadesOnRealDDL 删除实例必须级联清理本模块从属行。
//
// 真实 DDL 下 presentation_artifacts / document_snapshots /
// presentation_dependencies 都以复合外键引用实例行且无 ON DELETE CASCADE，
// 直接 DELETE 实例会被外键拒绝——旧实现（GORM Delete 单表）在真实库上必然失败，
// 同样被 AutoMigrate 的测试环境掩盖。
func TestDeleteInstanceCascadesOnRealDDL(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.createTemplate(t)
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "del-shirt", Data: map[string]any{"title": "删除衬衫"},
	})
	if err != nil {
		t.Fatalf("创建内容实体失败: %v", err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: "/del/shirt",
	})
	if err != nil {
		t.Fatalf("CreateInstance 失败: %v", err)
	}
	if err = f.pres.Delete(ctx, &presentationdto.DeleteReq{ID: inst.ID}); err != nil {
		t.Fatalf("删除实例失败（真实 DDL 下必须级联清理从属行）: %v", err)
	}
	for _, q := range []struct{ table, column string }{
		{"presentation_instances", "id"},
		{"document_snapshots", "presentation_instance_id"},
		{"presentation_artifacts", "presentation_instance_id"},
		{"presentation_dependencies", "presentation_id"},
	} {
		var n int64
		if err = f.db.Raw("SELECT COUNT(*) FROM "+q.table+" WHERE "+q.column+" = ?", inst.ID).Scan(&n).Error; err != nil {
			t.Fatalf("统计 %s 失败: %v", q.table, err)
		}
		if n != 0 {
			t.Errorf("删除后 %s 残留 %d 行", q.table, n)
		}
	}
	if _, err = os.Lstat(filepath.Join(pipeline.ActiveRoot(), "del/shirt")); !os.IsNotExist(err) {
		t.Errorf("删除实例后访问面链接应被移除，Lstat err=%v", err)
	}
}
