package unit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"

	"gorm.io/gorm"
)

func insertActiveRoute(t *testing.T, db *gorm.DB, projectID, path, ownerPageID string) {
	t.Helper()
	if err := db.Exec(
		`INSERT INTO page_routes (project_id, path, page_id, presentation_id, route_kind, update_time)
		 VALUES (?, ?, ?, NULL, 'active', now())`,
		projectID, path, ownerPageID).Error; err != nil {
		t.Fatalf("插入 active 路由行失败: %v", err)
	}
}

func routeRowsAt(t *testing.T, db *gorm.DB, projectID, path string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM public.page_routes WHERE project_id = ? AND path = ?", projectID, path).
		Scan(&n).Error; err != nil {
		t.Fatalf("统计路由行失败: %v", err)
	}
	return n
}

func activeLinkExists(t *testing.T, urlPath string) bool {
	t.Helper()
	_, err := os.Lstat(filepath.Join(activeDir(t), strings.TrimPrefix(urlPath, "/")))
	return err == nil
}

func TestUpdateURLRefusesRenameWhenOldPathTakenByRedirect(t *testing.T) {
	db, svc, _, projectID := newPageServiceWithRoutes(t)
	ctx := context.Background()

	page := createPage(t, svc, projectID, "/owner/old", headingDocument)
	buildAndPublish(t, svc, page.ID)
	intruder := createPage(t, svc, projectID, "/owner/intruder", headingDocument).ID

	buildAndPublish(t, svc, intruder)

	if err := db.Exec("DELETE FROM public.page_routes WHERE project_id = ? AND path = ? AND page_id = ?",
		projectID, "/owner/old", page.ID).Error; err != nil {
		t.Fatalf("释放本页旧路径占用失败: %v", err)
	}
	if _, err := svc.CreateRedirect(ctx, &pagedto.RedirectCreateReq{
		ProjectID: projectID, SourcePath: "/owner/old", TargetPath: "/owner/intruder",
	}); err != nil {
		t.Fatalf("让 intruder 以重定向形式接管 /owner/old 失败: %v", err)
	}
	if kind := routeOccupiedAt(t, db, projectID, "/owner/old"); kind != "redirect" {
		t.Fatalf("前置失败：/owner/old 应是 intruder 的 redirect 占用，实际 %q", kind)
	}
	if kind := activeKind(t, "/owner/old"); kind != "redirect" {
		t.Fatalf("前置失败：访问面应已是重定向，实际 %q", kind)
	}
	foreignLinkTarget := activeLinkTarget(t, "/owner/old")

	_, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: page.ID, NewPath: "/owner/new", WithRedirect: false})
	if err == nil {
		t.Fatal("旧路径被他人 redirect 占用：改 URL 必须被拒绝（不得把他人占用改名）")
	}

	if !strings.Contains(err.Error(), "ErrRouteActiveRename") {
		t.Fatalf("期望在迁移一步被拒（ErrRouteActiveRename），实际 %v", err)
	}

	if !activeLinkExists(t, "/owner/old") {
		t.Fatal("旧路径已归他人：本次处置不得删除那条链接")
	}
	if got := activeLinkTarget(t, "/owner/old"); got != foreignLinkTarget {
		t.Fatalf("别人的链接被改指向了：期望仍是 %q，实际 %q", foreignLinkTarget, got)
	}

	var owner string
	if err := db.Raw("SELECT page_id FROM public.page_routes WHERE project_id = ? AND path = ?", projectID, "/owner/old").
		Scan(&owner).Error; err != nil {
		t.Fatalf("读取路由归属失败: %v", err)
	}
	if owner != intruder {
		t.Fatalf("旧路径归属应仍是 %s，实际 %q", intruder, owner)
	}

	if got := draftPathOf(t, db, page.ID); got != "/owner/old" {
		t.Fatalf("被拒绝的改址不得迁移草稿路径，实际 %q", got)
	}
}

func TestUpdateURLRefusesToRenameAnotherOwnersActiveRoute(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()

	page := createPage(t, svc, projectID, "/owner2/old", headingDocument)
	buildAndPublish(t, svc, page.ID)

	if err := db.Exec("DELETE FROM page_routes WHERE project_id = ? AND path = ? AND page_id = ?",
		projectID, "/owner2/old", page.ID).Error; err != nil {
		t.Fatalf("释放 page 在 /owner2/old 的占用失败: %v", err)
	}
	intruder := createPage(t, svc, projectID, "/owner2/intruder", headingDocument).ID
	buildAndPublish(t, svc, intruder)

	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: intruder, NewPath: "/owner2/old", WithRedirect: false}); err != nil {
		t.Fatalf("让 intruder 发布到 /owner2/old 失败: %v", err)
	}
	if kind := activeKind(t, "/owner2/old"); kind != "page" {
		t.Fatalf("前置失败：/owner2/old 访问面应是 intruder 的页面，实际 %q", kind)
	}
	if kind := routeOccupiedAt(t, db, projectID, "/owner2/old"); kind != "active" {
		t.Fatalf("前置失败：/owner2/old 应是 intruder 的 active 占用，实际 %q", kind)
	}
	var occupyOwner string
	if err := db.Raw("SELECT page_id FROM public.page_routes WHERE project_id = ? AND path = ?", projectID, "/owner2/old").
		Scan(&occupyOwner).Error; err != nil {
		t.Fatalf("读取路由归属失败: %v", err)
	}
	if occupyOwner != intruder {
		t.Fatalf("前置失败：/owner2/old 应归 intruder，实际 %q", occupyOwner)
	}

	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: page.ID, NewPath: "/owner2/new", WithRedirect: false}); err == nil {
		t.Fatal("旧路径上是别人的 active 行：改 URL 必须拒绝（不得把别人线上路径改名）")
	}

	var owner string
	if err := db.Raw("SELECT page_id FROM public.page_routes WHERE project_id = ? AND path = ?", projectID, "/owner2/old").
		Scan(&owner).Error; err != nil {
		t.Fatalf("读取路由归属失败: %v", err)
	}
	if owner != intruder {
		t.Fatalf("旧路径归属应仍是 %s，实际 %q", intruder, owner)
	}
	if !activeLinkExists(t, "/owner2/old") {
		t.Fatal("拒绝时不得撤掉别人正在服务的链接")
	}

	if got := draftPathOf(t, db, page.ID); got != "/owner2/old" {
		t.Fatalf("被拒绝的改址不得迁移草稿路径，实际 %q", got)
	}
}

func TestDeactivateOldPathKeepsForeignOwner(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()

	page := createPage(t, svc, projectID, "/cond/old", headingDocument)
	buildAndPublish(t, svc, page.ID)
	intruder := createPage(t, svc, projectID, "/cond/intruder", headingDocument).ID
	insertActiveRoute(t, db, projectID, "/cond/shared", intruder)

	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: page.ID, NewPath: "/cond/new", WithRedirect: false}); err != nil {
		t.Fatalf("改 URL 失败: %v", err)
	}
	if got := routeRowsAt(t, db, projectID, "/cond/shared"); got != 1 {
		t.Fatalf("别人的 active 行不得被本页的取消操作删掉，实际剩 %d 行", got)
	}
}
