package unit

import (
	"context"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	projectdto "go_wp/internal/module/project/dto"
	pubmodel "go_wp/internal/module/publication/model"

	"gorm.io/gorm"
)

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

	for _, c := range []struct{ lang, path string }{
		{"zh-CN", "/about-us"},
		{"en-US", "/en/about-us"},
	} {
		wantHash := activeHashOfLang(t, db, page.ID, c.lang)
		if hash := activeRouteArtifactHash(t, db, projectID, c.path); hash != wantHash {
			t.Fatalf("%s 路由产物错误: %s（当前激活 %s）", c.lang, hash, wantHash)
		}
	}

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
