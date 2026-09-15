package unit

// page_locales_test.go — 站点语言清单驱动的路由登记（多语言 P3，docs/06-D §14 D10）。
//
// 消费点：建页/改草稿/改 URL 按「站点启用语言」逐语言登记与迁移 page_routes 占用行，
// 使每种语言的访问路径都有独立占用；sitemap 与 hreflang 也以同一清单为真源。

import (
	"context"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	projectdto "go_wp/internal/module/project/dto"
	pubmodel "go_wp/internal/module/publication/model"

	"gorm.io/gorm"
)

// reservedPaths 读取页面全部 reserved 占用路径（升序）。
func reservedPaths(t *testing.T, db *gorm.DB, projectID, pageID string) []string {
	t.Helper()
	var paths []string
	if err := db.Table("page_routes").Select("path").
		Where("project_id = ? AND page_id = ? AND route_kind = ?", projectID, pageID, pubmodel.RouteReserved).
		Order("path").Pluck("path", &paths).Error; err != nil {
		t.Fatalf("查询保留路由失败: %v", err)
	}
	return paths
}

// TestPageRoutesRegisteredPerLocale 启用 zh-CN + en-US 后，
// 建页登记两行 reserved、改草稿/改 URL 两行一起迁移。
func TestPageRoutesRegisteredPerLocale(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	withLangPrefix(t)

	if _, err := projects.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: projectID,
		Locales: []projectdto.LocaleItem{
			{Lang: "zh-CN", IsDefault: true},
			{Lang: "en-US"},
		},
	}); err != nil {
		t.Fatalf("保存语言清单失败: %v", err)
	}

	page := createPage(t, svc, projectID, "/about", headingDocument)
	got := reservedPaths(t, db, projectID, page.ID)
	want := []string{"/about", "/en/about"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("建页应为每语言登记一行 reserved: 期望 %v，实际 %v", want, got)
	}

	// 改草稿路径：两语言占用一起迁移。
	if _, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: page.ID, ExpectedVersion: page.DraftVersion,
		DraftPath: "/about-us", DraftDocument: []byte(headingDocument),
	}); err != nil {
		t.Fatalf("改草稿路径失败: %v", err)
	}
	got = reservedPaths(t, db, projectID, page.ID)
	want = []string{"/about-us", "/en/about-us"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("改草稿后应迁移全部语言占用: 期望 %v，实际 %v", want, got)
	}

	// 两语言各自构建 + 发布：各自 active 行。
	// 构建返回值不必留存：激活的产物以 page_artifacts 为准（见下方断言）。
	_, err := svc.Build(ctx, &pagedto.BuildReq{ID: page.ID})
	if err != nil {
		t.Fatalf("zh 构建失败: %v", err)
	}
	_, err = svc.Build(ctx, &pagedto.BuildReq{ID: page.ID, Lang: "en-US"})
	if err != nil {
		t.Fatalf("en 构建失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID}); err != nil {
		t.Fatalf("zh 发布失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: page.ID, Lang: "en-US"}); err != nil {
		t.Fatalf("en 发布失败: %v", err)
	}
	if kind := routeKind(t, db, projectID, "/about-us"); kind != pubmodel.RouteActive {
		t.Fatalf("/about-us 应 active，实际 %q", kind)
	}
	if kind := routeKind(t, db, projectID, "/en/about-us"); kind != pubmodel.RouteActive {
		t.Fatalf("/en/about-us 应 active，实际 %q", kind)
	}
	// 期望值取产物表：多语言逐个发布时，后发布语言会让先前构建的产物「落后于
	// 站点级状态」，发布时会按当前草稿重新构建并落新行 —— 路由指向的是最新那份。
	var arts []struct{ Lang, ArtifactHash string }
	if err := db.Raw("SELECT lang, artifact_hash FROM page_artifacts WHERE page_id = ? ORDER BY lang", page.ID).
		Scan(&arts).Error; err != nil {
		t.Fatalf("查询产物行失败: %v", err)
	}
	latest := map[string]string{}
	for _, a := range arts {
		latest[a.Lang] = a.ArtifactHash
	}
	if hash := activeRouteArtifactHash(t, db, projectID, "/about-us"); hash != latest["zh-CN"] {
		t.Fatalf("zh 路由产物错误: %s（期望 %s）", hash, latest["zh-CN"])
	}
	if hash := activeRouteArtifactHash(t, db, projectID, "/en/about-us"); hash != latest["en-US"] {
		t.Fatalf("en 路由产物错误: %s（期望 %s）", hash, latest["en-US"])
	}

	// 删除页面：全部语言占用释放。
	if err = svc.Delete(ctx, &pagedto.DeleteReq{ID: page.ID}); err != nil {
		t.Fatalf("删除页面失败: %v", err)
	}
	var remaining int64
	if err = db.Table("page_routes").Where("project_id = ? AND page_id = ?", projectID, page.ID).
		Count(&remaining).Error; err != nil {
		t.Fatalf("统计路由失败: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("删除页面应释放全部语言占用，剩余 %d 行", remaining)
	}
}
