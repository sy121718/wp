// presentation_url_test.go — 详情页改 URL 的链路测试（真实 PostgreSQL + 生产 DDL + 真实访问面激活）。
//
// 覆盖四条只有真链路才能证伪的性质：
//  1. 新路径已激活且 canonical 重烘到新路径 —— 这正是「改 URL 必须重建产物」的理由
//     （路径被编译进了 HTML 字节，改路径不重编就会留下「页面仍宣称住在旧地址」）；
//  2. url_path 落库，且随后的重建不回退旧路径（否则下一次内容更新会把产物搬回旧地址）；
//  3. 旧路径按 WithRedirect 变成 301（目标指向新路径）或直接取消激活；
//  4. 占用预检同时看 page_routes 与 presentation_instances 两个真源 ——
//     本次之前创建的实例从没登记过路由，只查路由表会放行抢路径。
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

// mkArticle 建一篇文章并发布到指定路径，返回 (实体 id, 实例 id)。
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

// routeOwnedByInstance 断言路径在 page_routes 里由该展示实例占用。
//
// 用占用查询的两个视角验证，不新增只为测试存在的读取接口：
// 不排除时「有人占」+ 排除本实例后「没人占」= 占的就是它自己。
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

// activeState 读取某路径在访问面上的激活状态。
func activeState(t *testing.T, urlPath string) *pipeline.PublicationState {
	t.Helper()
	state, err := (&pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()}).Inspect(urlPath)
	if err != nil {
		t.Fatalf("读取 %s 的激活状态失败: %v", urlPath, err)
	}
	return state
}

// TestPresentationUpdateURLEndToEnd 改 URL 全链路：新路径激活 + canonical 重烘 +
// url_path 落库 + 路由占用迁移 + 旧路径 301 + 重build 不回退。
func TestPresentationUpdateURLEndToEnd(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.createTemplate(t)
	entityID, instanceID := mkArticle(t, f, "summer-shirt", "夏季衬衫", "/blog/old-name")

	// 发布即登记路由占用（本次补的那一半：详情页此前从不写 page_routes）。
	routeOwnedByInstance(t, f, "/blog/old-name", instanceID)

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		EntityType: "article", EntityID: entityID,
		NewPath: "/blog/new-name", WithRedirect: true,
	}); err != nil {
		t.Fatalf("改 URL 失败: %v", err)
	}

	// 1) 新路径可访问，内容仍是同一篇文章。
	html := activeHTML(t, "/blog/new-name")
	if !strings.Contains(html, "夏季衬衫") {
		t.Fatalf("新路径产物应含实体字段字面量，实际: %s", html)
	}
	// 2) canonical 已重烘到新路径。
	if got := canonicalOf(html); got != "/blog/new-name" {
		t.Fatalf("新路径 canonical 应为 /blog/new-name，实际 %q", got)
	}
	// 3) 旧路径变 301，目标指向新路径。
	old := activeState(t, "/blog/old-name")
	if old.Kind != pipeline.PublicationRedirect {
		t.Fatalf("旧路径应为 301 重定向，实际 kind=%q", old.Kind)
	}
	if old.Redirect == nil || old.Redirect.TargetPath != "/blog/new-name" {
		t.Fatalf("旧路径 301 目标应为新路径，实际 %+v", old.Redirect)
	}
	// 4) 实例 url_path 与新路径路由占用都已就位。
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

	// 5) 重建（内容更新触发的自动重建走同一条路）不得把路径退回旧值：
	// url_path 必须参与重建输入，否则一次内容更新就会把产物搬回旧地址。
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

// TestPresentationUpdateURLRejectsBadTargets 改 URL 的拒绝路径。
func TestPresentationUpdateURLRejectsBadTargets(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.createTemplate(t)
	aID, _ := mkArticle(t, f, "post-a", "文章甲", "/blog/post-a")
	_, bInstance := mkArticle(t, f, "post-b", "文章乙", "/blog/post-b")

	// 同路径：没有需要修改的地方，拒绝而不是静默成功。
	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		EntityType: "article", EntityID: aID, NewPath: "/blog/post-a",
	}); err == nil || err.Error() != presentationenums.ErrSamePath {
		t.Fatalf("同路径应报 %s，实际 %v", presentationenums.ErrSamePath, err)
	}

	// 目标被另一个实例占用：路由表与实例表两条都命中。
	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		EntityType: "article", EntityID: aID, NewPath: "/blog/post-b",
	}); err == nil || err.Error() != presentationenums.ErrPathOccupied {
		t.Fatalf("目标被占用应报 %s，实际 %v", presentationenums.ErrPathOccupied, err)
	}

	// 目标被「没有路由登记的历史实例」占用：抹掉它的路由行，模拟本次之前
	// 创建的实例（占用只存在于 presentation_instances.url_path）。
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

	// 非法路径：必须带前导斜杠。
	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		EntityType: "article", EntityID: aID, NewPath: "blog/no-slash",
	}); err == nil || !strings.HasPrefix(err.Error(), presentationenums.ErrInvalidPath) {
		t.Fatalf("非法路径应报 %s，实际 %v", presentationenums.ErrInvalidPath, err)
	}

	// 实例不存在（实体从未发布）。
	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		EntityType: "article", EntityID: uuid.NewString(), NewPath: "/blog/ghost",
	}); err == nil || err.Error() != presentationenums.ErrNotFound {
		t.Fatalf("实例不存在应报 %s，实际 %v", presentationenums.ErrNotFound, err)
	}
}

// TestPresentationUpdateURLWithoutRedirect 不勾选重定向时旧路径直接失效
// （旧链接 404，而不是留一个指向新路径的 301）。
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
	// 旧路径的路由占用也要释放：留着 active 行会让这条路径永远无法被别人使用。
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
