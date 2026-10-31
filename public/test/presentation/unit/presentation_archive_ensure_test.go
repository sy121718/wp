package unit

import (
	"context"
	"strings"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
)

func TestEnsureArchiveInstance(t *testing.T) {
	f := newPresFixture(t)
	ctx := context.Background()
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "archive-entity",
		Data: map[string]any{"title": "归档主体", "excerpt": "摘要"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	req := &presentationdto.EnsureArchiveReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, Slug: "electronics",
	}

	resp, err := f.pres.EnsureArchiveInstance(ctx, req)
	if err != nil {
		t.Fatalf("未配置归档模板时应跳过而不是报错: %v", err)
	}
	if resp.Skipped == "" || resp.InstanceID != "" {
		t.Fatalf("未配置归档模板时应返回 Skipped 且不建实例，实际 %+v", resp)
	}

	archiveDoc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"a1","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h2"}}]}`
	if _, err = f.templates.Create(ctx, &contenttemplatedto.CreateReq{
		ProjectID: f.projectID, EntityType: "article", Name: "文章归档模板",
		TemplateRole: "archive", DraftDocument: []byte(archiveDoc),
	}); err != nil {
		t.Fatalf("创建归档模板失败: %v", err)
	}

	resp, err = f.pres.EnsureArchiveInstance(ctx, req)
	if err != nil {
		t.Fatalf("配置归档模板后应创建实例: %v", err)
	}
	if resp.InstanceID == "" || resp.Skipped != "" {
		t.Fatalf("应创建归档实例，实际 %+v", resp)
	}
	inst, err := f.pres.Get(ctx, &presentationdto.GetReq{ID: resp.InstanceID})
	if err != nil {
		t.Fatalf("查询归档实例失败: %v", err)
	}
	if inst.InstanceRole != "archive" {
		t.Fatalf("实例角色应为 archive，实际 %q", inst.InstanceRole)
	}
	if inst.URLPath != "/article/electronics" {
		t.Fatalf("归档路径应按 /{entityType}/{slug} 规则生成，实际 %q", inst.URLPath)
	}

	again, err := f.pres.EnsureArchiveInstance(ctx, req)
	if err != nil {
		t.Fatalf("重复调用应幂等: %v", err)
	}
	if again.InstanceID != resp.InstanceID {
		t.Fatalf("重复调用应返回同一个实例 %s，实际 %s", resp.InstanceID, again.InstanceID)
	}

	noSlug := *req
	noSlug.Slug = ""
	resp, err = f.pres.EnsureArchiveInstance(ctx, &noSlug)
	if err != nil {
		t.Fatalf("无 slug 时应跳过而不是报错: %v", err)
	}
	if !strings.Contains(resp.Skipped, "slug") {
		t.Fatalf("跳过原因应说明缺 slug，实际 %q", resp.Skipped)
	}
}
