package unit

import (
	"context"
	"testing"

	"gorm.io/gorm"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"
)

func newDegradedPageService(t *testing.T, db *gorm.DB) pagecontract.PageService {
	t.Helper()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	return pageservice.NewService(
		pagemodel.NewPageModel(db),
		artifactservice.NewService(artifactmodel.NewArtifactModel(db)),
		pubservice.NewService(pubmodel.NewPublicationModel(db)),
		nil,
		blocks, nil, nil, nil, nil,
	)
}

func TestUpdateURLCannotMoveAccessFaceWithoutLanguageSet(t *testing.T) {
	db, svc, projects, projectID := newPageService(t)
	ctx := context.Background()
	withDefaultPlain(t)
	twoLocaleProject(t, projects, projectID)

	pageID := createPage(t, svc, projectID, "/degraded/old", pageDocument).ID
	buildAndPublish(t, svc, pageID)
	oldPath := "/degraded/old"
	newPath := "/degraded/new"

	reservedBefore := reservedPaths(t, db, projectID, pageID)
	if !equalStrings(reservedBefore, []string{"/en/degraded/old"}) {
		t.Fatalf("发布 zh-CN 后应只剩 en-US 的保留路由，实际 %v", reservedBefore)
	}

	degraded := newDegradedPageService(t, db)
	if _, err := degraded.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: pageID, NewPath: newPath}); err == nil {
		t.Fatal("站点语言集合不可读时改 URL 必须失败 —— 否则会带着不完整的语言集去切访问面")
	}

	if kind := activeKind(t, newPath); kind != "none" {
		t.Fatalf("改 URL 失败后新路径不得有激活链接，实际 %q", kind)
	}
	if kind := activeKind(t, oldPath); kind != "page" {
		t.Fatalf("改 URL 失败后旧路径应仍是激活页面，实际 %q", kind)
	}

	if got := pageColumnOf(t, db, pageID, "draft_path"); got != oldPath {
		t.Fatalf("改 URL 失败后 pages.draft_path 应保持 %q，实际 %q", oldPath, got)
	}
	if got := publicationOf(t, db, pageID, "zh-CN").ActivePath; got != oldPath {
		t.Fatalf("改 URL 失败后激活路径应保持 %q，实际 %q", oldPath, got)
	}
	if got := reservedPaths(t, db, projectID, pageID); !equalStrings(got, reservedBefore) {
		t.Fatalf("改 URL 失败后保留路由不得被迁移（半迁移的表现）：期望 %v，实际 %v", reservedBefore, got)
	}

	if n := receiptsAt(t, db, newPath); n != 0 {
		t.Fatalf("改 URL 未开始就不该在目标路径留下任何回执，实际 %d 条", n)
	}
}

func receiptsAt(t *testing.T, db *gorm.DB, path string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM publication_receipts WHERE path = ?", path).Scan(&n).Error; err != nil {
		t.Fatalf("统计回执失败: %v", err)
	}
	return n
}
