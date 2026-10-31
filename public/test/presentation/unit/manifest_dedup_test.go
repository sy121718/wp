package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
)

func TestPresentationArtifactsHasSingleManifestSource(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	var cols int64
	if err := f.db.Raw("SELECT COUNT(*) FROM information_schema.columns " +
		"WHERE table_schema = current_schema() AND table_name = 'presentation_artifacts' " +
		"AND column_name = 'build_input_manifest'").Scan(&cols).Error; err != nil {
		t.Fatalf("查询列存在性失败: %v", err)
	}
	if cols != 0 {
		t.Fatalf("presentation_artifacts.build_input_manifest 应已在迁移 305 删除，实际仍有 %d 列", cols)
	}

	f.createTemplate(t)
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "dedup-shirt", Data: map[string]any{"title": "去重衬衫"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: "/dedup/shirt",
	})
	if err != nil {
		t.Fatalf("CreateInstance 失败: %v", err)
	}

	var manifestJSON string
	if err := f.db.Raw("SELECT manifest::text FROM presentation_artifacts WHERE presentation_instance_id = ?",
		inst.ID).Scan(&manifestJSON).Error; err != nil {
		t.Fatalf("读取产物清单失败: %v", err)
	}
	if strings.TrimSpace(manifestJSON) == "" {
		t.Fatal("产物清单真源为空：合并重复列不能连清单一起丢掉")
	}
	var parsed struct {
		CanonicalPath string            `json:"canonicalPath"`
		SourceType    string            `json:"sourceType"`
		Files         map[string]string `json:"files"`
	}
	if err := json.Unmarshal([]byte(manifestJSON), &parsed); err != nil {
		t.Fatalf("清单不是合法 JSON（回滚与依赖反查都依赖它）: %v", err)
	}
	if parsed.CanonicalPath == "" || parsed.Files["index.html"] == "" {
		t.Fatalf("清单缺 canonicalPath 或 files.index.html，真源不完整: %s", manifestJSON)
	}

	if _, err := f.pres.RollbackArtifact(ctx, &presentationdto.RollbackArtifactReq{
		ProjectID: f.projectID, InstanceID: inst.ID, TargetHash: inst.ArtifactHash,
	}); err != nil {
		t.Fatalf("产物回滚失败（本次列收敛不得破坏回滚）: %v", err)
	}

	if _, err := f.pres.Rebuild(ctx, &presentationdto.RebuildReq{EntityID: entity.ID}); err != nil {
		t.Fatalf("重建失败（本次列收敛不得破坏重建）: %v", err)
	}
	if html := activeHTML(t, "/dedup/shirt"); !strings.Contains(html, "去重衬衫") {
		t.Fatalf("重建后访问面应含实体字段，实际: %s", html)
	}
}
