package unit

// presentation_archive_ensure_test.go — 归档实例的按需创建（审计 EDT-004 第三层）。
//
// 「新建一个分类 → 自动有对应列表页」这条路的关键是：实体侧只管调用，
// 归档页该不该存在由 presentation 判断 —— 没配归档模板就**静默跳过**。
// 如果这里报错，「新建分类」会变成一个可能失败的操作，而失败原因（没配归档模板）
// 跟分类本身毫无关系。

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

	// 1) 没配归档模板：跳过，且不是错误。
	resp, err := f.pres.EnsureArchiveInstance(ctx, req)
	if err != nil {
		t.Fatalf("未配置归档模板时应跳过而不是报错: %v", err)
	}
	if resp.Skipped == "" || resp.InstanceID != "" {
		t.Fatalf("未配置归档模板时应返回 Skipped 且不建实例，实际 %+v", resp)
	}

	// 2) 配一套归档模板（role=archive）。
	archiveDoc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"a1","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h2"}}]}`
	if _, err = f.templates.Create(ctx, &contenttemplatedto.CreateReq{
		ProjectID: f.projectID, EntityType: "article", Name: "文章归档模板",
		TemplateRole: "archive", DraftDocument: []byte(archiveDoc),
	}); err != nil {
		t.Fatalf("创建归档模板失败: %v", err)
	}

	// 3) 再调用：这次应该建出归档实例，路径按规则生成。
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

	// 4) 幂等：再调一次拿到同一个实例。
	again, err := f.pres.EnsureArchiveInstance(ctx, req)
	if err != nil {
		t.Fatalf("重复调用应幂等: %v", err)
	}
	if again.InstanceID != resp.InstanceID {
		t.Fatalf("重复调用应返回同一个实例 %s，实际 %s", resp.InstanceID, again.InstanceID)
	}

	// 5) 没有 slug：跳过（编一个路径会在实体补上 slug 后变成永远 404 的死链）。
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
