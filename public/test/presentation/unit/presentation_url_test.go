package unit

import (
	"context"
	"strings"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"

	"github.com/google/uuid"
)

func mkArticle(t *testing.T, f *presFixture, slug, title, urlPath string) (entityID, instanceID string) {
	t.Helper()
	ctx := context.Background()
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: slug, Data: map[string]any{"title": title},
	})
	if err != nil {
		t.Fatalf("创建实体 %s 失败: %v", slug, err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: urlPath,
	})
	if err != nil {
		t.Fatalf("发布 %s 到 %s 失败: %v", slug, urlPath, err)
	}
	return entity.ID, inst.ID
}

func routeOwnedByInstance(t *testing.T, f *presFixture, path, instanceID string) {
	t.Helper()
	ctx := context.Background()
	occupied, err := f.routes.IsPathOccupied(ctx, &pubcontract.IsOccupiedReq{
		ProjectID: f.projectID, Path: path,
	})
	if err != nil {
		t.Fatalf("占用查询失败: %v", err)
	}
	if !occupied {
		t.Fatalf("路径 %s 应已被登记占用（详情页占用此前从不落 page_routes）", path)
	}
	occupied, err = f.routes.IsPathOccupied(ctx, &pubcontract.IsOccupiedReq{
		ProjectID: f.projectID, Path: path, ExcludePresentationID: instanceID,
	})
	if err != nil {
		t.Fatalf("占用查询（排除自身）失败: %v", err)
	}
	if occupied {
		t.Fatalf("路径 %s 的占用者不是本实例 %s", path, instanceID)
	}
}

func activeState(t *testing.T, urlPath string) *pipeline.PublicationState {
	t.Helper()
	state, err := (&pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()}).Inspect(urlPath)
	if err != nil {
		t.Fatalf("读取 %s 的激活状态失败: %v", urlPath, err)
	}
	return state
}

func TestPresentationUpdateURLEndToEnd(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.createTemplate(t)
	entityID, instanceID := mkArticle(t, f, "summer-shirt", "夏季衬衫", "/blog/old-name")

	routeOwnedByInstance(t, f, "/blog/old-name", instanceID)

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		EntityType: "article", EntityID: entityID,
		NewPath: "/blog/new-name", WithRedirect: true,
	}); err != nil {
		t.Fatalf("改 URL 失败: %v", err)
	}

	html := activeHTML(t, "/blog/new-name")
	if !strings.Contains(html, "夏季衬衫") {
		t.Fatalf("新路径产物应含实体字段字面量，实际: %s", html)
	}

	if got := canonicalOf(html); got != "/blog/new-name" {
		t.Fatalf("新路径 canonical 应为 /blog/new-name，实际 %q", got)
	}

	old := activeState(t, "/blog/old-name")
	if old.Kind != pipeline.PublicationRedirect {
		t.Fatalf("旧路径应为 301 重定向，实际 kind=%q", old.Kind)
	}
	if old.Redirect == nil || old.Redirect.TargetPath != "/blog/new-name" {
		t.Fatalf("旧路径 301 目标应为新路径，实际 %+v", old.Redirect)
	}

	got, err := f.pres.GetByEntity(ctx, &presentationdto.GetByEntityReq{
		EntityType: "article", EntityID: entityID,
	})
	if err != nil {
		t.Fatalf("回读实例失败: %v", err)
	}
	if got.URLPath != "/blog/new-name" {
		t.Fatalf("url_path 未随改 URL 落库: %q", got.URLPath)
	}
	routeOwnedByInstance(t, f, "/blog/new-name", instanceID)

	rebuilt, err := f.pres.Rebuild(ctx, &presentationdto.RebuildReq{EntityID: entityID})
	if err != nil {
		t.Fatalf("重建失败: %v", err)
	}
	if rebuilt.URLPath != "/blog/new-name" {
		t.Fatalf("重建后路径回退到 %q（url_path 没有参与重建输入）", rebuilt.URLPath)
	}
	if gotCanonical := canonicalOf(activeHTML(t, "/blog/new-name")); gotCanonical != "/blog/new-name" {
		t.Fatalf("重建后 canonical 应为新路径，实际 %q", gotCanonical)
	}
}

func TestPresentationUpdateURLRejectsBadTargets(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.createTemplate(t)
	aID, _ := mkArticle(t, f, "post-a", "文章甲", "/blog/post-a")
	_, bInstance := mkArticle(t, f, "post-b", "文章乙", "/blog/post-b")

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		EntityType: "article", EntityID: aID, NewPath: "/blog/post-a",
	}); err == nil || err.Error() != presentationenums.ErrSamePath {
		t.Fatalf("同路径应报 %s，实际 %v", presentationenums.ErrSamePath, err)
	}

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		EntityType: "article", EntityID: aID, NewPath: "/blog/post-b",
	}); err == nil || err.Error() != presentationenums.ErrPathOccupied {
		t.Fatalf("目标被占用应报 %s，实际 %v", presentationenums.ErrPathOccupied, err)
	}

	if err := f.db.Exec("DELETE FROM page_routes WHERE presentation_id = ?", bInstance).Error; err != nil {
		t.Fatalf("删除路由行失败: %v", err)
	}
	occupied, err := f.routes.IsPathOccupied(ctx, &pubcontract.IsOccupiedReq{
		ProjectID: f.projectID, Path: "/blog/post-b",
	})
	if err != nil {
		t.Fatalf("占用查询失败: %v", err)
	}
	if occupied {
		t.Fatal("前置条件失败：该路径的路由行应已删除（否则这条用例没打到第二个真源）")
	}
	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		EntityType: "article", EntityID: aID, NewPath: "/blog/post-b",
	}); err == nil || err.Error() != presentationenums.ErrPathOccupied {
		t.Fatalf("历史实例占用的路径也应被拒（%s），实际 %v", presentationenums.ErrPathOccupied, err)
	}

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		EntityType: "article", EntityID: aID, NewPath: "blog/no-slash",
	}); err == nil || !strings.HasPrefix(err.Error(), presentationenums.ErrInvalidPath) {
		t.Fatalf("非法路径应报 %s，实际 %v", presentationenums.ErrInvalidPath, err)
	}

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		EntityType: "article", EntityID: uuid.NewString(), NewPath: "/blog/ghost",
	}); err == nil || err.Error() != presentationenums.ErrNotFound {
		t.Fatalf("实例不存在应报 %s，实际 %v", presentationenums.ErrNotFound, err)
	}
}

func TestPresentationUpdateURLWithoutRedirect(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.createTemplate(t)
	entityID, instanceID := mkArticle(t, f, "post-c", "文章丙", "/blog/post-c")

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		EntityType: "article", EntityID: entityID, NewPath: "/blog/post-c-moved",
	}); err != nil {
		t.Fatalf("改 URL 失败: %v", err)
	}
	if state := activeState(t, "/blog/post-c"); state.Kind != pipeline.PublicationNone {
		t.Fatalf("不勾选重定向时旧路径应失效（kind=%q）", state.Kind)
	}

	occupied, err := f.routes.IsPathOccupied(ctx, &pubcontract.IsOccupiedReq{
		ProjectID: f.projectID, Path: "/blog/post-c",
	})
	if err != nil {
		t.Fatalf("占用查询失败: %v", err)
	}
	if occupied {
		t.Fatal("不勾选重定向时旧路径的 active 占用应已释放")
	}
	routeOwnedByInstance(t, f, "/blog/post-c-moved", instanceID)
}
