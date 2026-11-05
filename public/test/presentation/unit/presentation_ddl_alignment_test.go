package unit

import (
	"context"
	publish "go_wp/pkg/publish"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationmodel "go_wp/internal/module/presentation/model"

	"go_wp/public/test/support"
)

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
		{"presentation_publications", &presentationmodel.PublicationEntity{}},
	} {
		t.Run(tc.table, func(t *testing.T) {
			support.AssertModelColumnsSubset(t, f.db, tc.table, tc.dest)
		})
	}
}

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

func instanceUpdatedAt(t *testing.T, f *presFixture, id string) time.Time {
	t.Helper()
	var at time.Time
	if err := f.db.Raw("SELECT update_time FROM presentation_instances WHERE id = ?", id).Scan(&at).Error; err != nil {
		t.Fatalf("读取实例 update_time 失败: %v", err)
	}
	return at
}

func countInstanceArtifacts(t *testing.T, f *presFixture, id string) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM presentation_artifacts WHERE presentation_instance_id = ?", id).
		Scan(&n).Error; err != nil {
		t.Fatalf("统计产物行失败: %v", err)
	}
	return n
}

func TestContentChangeAutoRebuildAndPublish(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	fanout := publish.NewFanout()
	fanout.SetSyncRebuild(true)
	fanout.Register(publish.SourceTypePresentation, f.pres)
	fanout.SetRebuilder(publish.SourceTypePresentation, f.pres)
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

	ids, err := f.pres.MarkStaleByDependency(ctx, publish.DepKindDirectContent, "article:"+entityA.ID)
	if err != nil {
		t.Fatalf("依赖反查失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != instA.ID {
		t.Fatalf("受影响集合应只含实例 A(%s)，实际 %v", instA.ID, ids)
	}

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

	if _, err = f.content.Update(ctx, &contentdto.UpdateReq{
		ID: entityA.ID, Data: map[string]any{"title": "自动发布 A v2"},
	}); err != nil {
		t.Fatalf("更新实体 A 失败: %v", err)
	}

	if now := instanceUpdatedAt(t, f, instA.ID); !now.After(beforeA) {
		t.Fatalf("受影响实例未重建：update_time 未前进（%v → %v）", beforeA, now)
	}
	if html := activeHTML(t, "/auto/a"); !strings.Contains(html, "自动发布 A v2") {
		t.Fatalf("自动重建后访问面应更新为新内容，实际: %s", html)
	}

	if n := countInstanceArtifacts(t, f, instA.ID); n != artifactsABefore+1 {
		t.Fatalf("内容变更应新增一条产物行（%d → %d）", artifactsABefore, n)
	}

	var staleA bool
	if err = f.db.Raw("SELECT stale FROM presentation_instances WHERE id = ?", instA.ID).Scan(&staleA).Error; err != nil {
		t.Fatalf("读取 stale 失败: %v", err)
	}
	if staleA {
		t.Fatalf("自动重建成功后 stale 应为 false")
	}

	if now := instanceUpdatedAt(t, f, instB.ID); !now.Equal(beforeB) {
		t.Fatalf("无关实例 B 被误重建（update_time %v → %v）", beforeB, now)
	}
	if n := countInstanceArtifacts(t, f, instB.ID); n != artifactsBBefore {
		t.Fatalf("无关实例 B 产物行数变化（%d → %d）", artifactsBBefore, n)
	}
	if html := activeHTML(t, "/auto/b"); !strings.Contains(html, "自动发布 B") {
		t.Fatalf("无关实例 B 访问面内容不应变化，实际: %s", html)
	}
}

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
	if _, err = os.Lstat(filepath.Join(publish.ActiveRoot(), "del/shirt")); !os.IsNotExist(err) {
		t.Errorf("删除实例后访问面链接应被移除，Lstat err=%v", err)
	}
}
