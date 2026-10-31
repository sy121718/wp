package unit

import (
	"context"
	"strings"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/pkg/i18n"
)

func TestPresentationBilingualSiteOnline(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	withGlobalSiteLangURLMode(t, i18n.SiteLangURLModeDefaultPlain)

	if err := f.db.Exec(
		"INSERT INTO project_locales (project_id, lang, sort_order, is_default, enabled, create_time, update_time) VALUES (?, ?, 0, true, true, now(), now()), (?, ?, 1, false, true, now(), now())",
		f.projectID, "zh-CN", f.projectID, "en-US").Error; err != nil {
		t.Fatalf("写入语言清单失败: %v", err)
	}

	f.createTemplate(t)
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "bilingual-shirt",
		Data: map[string]any{"title": "双语衬衫", "excerpt": "摘要"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}

	logicalPath := "/products/bilingual-shirt"
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: logicalPath,
	})
	if err != nil {
		t.Fatalf("CreateInstance 失败: %v", err)
	}
	if inst.URLPath != logicalPath {
		t.Fatalf("实例 url_path 应存逻辑路径 %q，实际 %q", logicalPath, inst.URLPath)
	}

	type pubRow struct {
		Lang         string
		ActivePath   string
		ArtifactHash string
	}
	var pubs []pubRow
	if err = f.db.Raw(
		"SELECT lang, active_path, artifact_hash FROM presentation_publications WHERE presentation_id = ? ORDER BY lang",
		inst.ID).Scan(&pubs).Error; err != nil {
		t.Fatalf("查询 presentation_publications 失败: %v", err)
	}
	if len(pubs) != 2 {
		t.Fatalf("应有两语言各一行 publication，实际 %+v", pubs)
	}
	if pubs[0].Lang != "en-US" || pubs[0].ActivePath != "/en/products/bilingual-shirt" {
		t.Fatalf("en-US 激活路径应为 /en/products/bilingual-shirt，实际 %+v", pubs[0])
	}
	if pubs[1].Lang != "zh-CN" || pubs[1].ActivePath != logicalPath {
		t.Fatalf("zh-CN 激活路径应为 %q，实际 %+v", logicalPath, pubs[1])
	}
	if pubs[0].ArtifactHash == pubs[1].ArtifactHash {
		t.Fatalf("两语言产物 hash 不应相同（canonical 路径不同）")
	}

	type artRow struct {
		Lang string
	}
	var arts []artRow
	if err = f.db.Raw(
		"SELECT lang FROM presentation_artifacts WHERE presentation_instance_id = ? ORDER BY lang",
		inst.ID).Scan(&arts).Error; err != nil {
		t.Fatalf("查询 presentation_artifacts 失败: %v", err)
	}
	if len(arts) != 2 || arts[0].Lang != "en-US" || arts[1].Lang != "zh-CN" {
		t.Fatalf("应有两语言产物行，实际 %+v", arts)
	}

	zhHTML := activeHTML(t, logicalPath)
	enHTML := activeHTML(t, "/en/products/bilingual-shirt")
	if !strings.Contains(zhHTML, "双语衬衫") || !strings.Contains(enHTML, "双语衬衫") {
		t.Fatalf("两语言产物均应含实体标题")
	}
	if got := canonicalOf(zhHTML); got != logicalPath {
		t.Fatalf("zh 产物 canonical 应为 %q，实际 %q", logicalPath, got)
	}
	if got := canonicalOf(enHTML); got != "/en/products/bilingual-shirt" {
		t.Fatalf("en 产物 canonical 应为 /en/products/bilingual-shirt，实际 %q", got)
	}

	routeOwnedByInstance(t, f, logicalPath, inst.ID)
	routeOwnedByInstance(t, f, "/en/products/bilingual-shirt", inst.ID)

	occupied, err := f.routes.IsPathOccupied(ctx, &pubcontract.IsOccupiedReq{
		ProjectID: f.projectID, Path: logicalPath, ExcludePresentationID: inst.ID,
	})
	if err != nil {
		t.Fatalf("占用查询失败: %v", err)
	}
	if occupied {
		t.Fatal("逻辑路径本身不应被其他实例占用")
	}

	paths, err := f.pres.PublishedEntityPaths(ctx, f.projectID, "article", "en-US", []string{entity.ID})
	if err != nil {
		t.Fatalf("PublishedEntityPaths 失败: %v", err)
	}
	if paths[entity.ID] != "/en/products/bilingual-shirt" {
		t.Fatalf("en-US locator 应返回 /en/products/bilingual-shirt，实际 %q", paths[entity.ID])
	}
	zhPaths, err := f.pres.PublishedEntityPaths(ctx, f.projectID, "article", "zh-CN", []string{entity.ID})
	if err != nil {
		t.Fatalf("PublishedEntityPaths zh 失败: %v", err)
	}
	if zhPaths[entity.ID] != logicalPath {
		t.Fatalf("zh-CN locator 应返回 %q，实际 %q", logicalPath, zhPaths[entity.ID])
	}
}

func withGlobalSiteLangURLMode(t *testing.T, mode i18n.SiteLangURLMode) {
	t.Helper()
	i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) {
		return i18n.RuntimeValues{SiteLangURLMode: string(mode)}, nil
	})
	t.Cleanup(func() {

		i18n.SetValueLoader(func(context.Context) (i18n.RuntimeValues, error) { return i18n.RuntimeValues{}, nil })
		i18n.SetValueLoader(nil)
	})
}
