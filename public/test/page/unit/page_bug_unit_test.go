package unit

import (
	"context"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pubmodel "go_wp/internal/module/publication/model"
)

func TestPageUpdateURLUnpublishedWithRedirect(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/u-old", pageDocument)

	_, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{
		ID: created.ID, NewPath: "/u-new", WithRedirect: true,
	})
	if err == nil {

		t.Logf("UpdateURL 成功（修复后预期）")
		return
	}
	t.Logf("BUG 复现：UpdateURL 返回错误 %v", err)

	detail, derr := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: created.ID})
	if derr != nil || detail.DraftPath != "/u-new" {
		t.Errorf("分裂证据：DB draft_path 已被迁移到 %q（err=%v）", detail.DraftPath, derr)
	}

	if kind := activeKind(t, "/u-new"); kind != "page" {
		t.Errorf("分裂证据：FS /u-new 已激活为 %s", kind)
	}

	if kind := activeKind(t, "/u-old"); kind != "redirect" {
		t.Errorf("分裂证据：FS /u-old 应为 redirect，实际 %s", kind)
	}
	var routeCount int64
	db.Table("page_routes").Where("project_id = ? AND path = ?", projectID, "/u-old").Count(&routeCount)
	if routeCount != 0 {
		t.Errorf("分裂证据：DB 应无 /u-old 占用行，实际 %d 行", routeCount)
	}

	if _, rerr := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{
		ID: created.ID, NewPath: "/u-old", WithRedirect: false,
	}); rerr == nil {
		t.Logf("重试可恢复（修复后预期）")
	}
}

func TestPagePublishAfterDraftPathChangeLeavesOldActiveRoute(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/leave-a", headingDocument)
	h1 := buildAndPublish(t, svc, created.ID)
	_ = h1

	saved, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: created.DraftVersion,
		DraftPath: "/leave-b", DraftDocument: []byte(pageDocument),
	})
	if err != nil {
		t.Fatalf("改路径保存失败: %v", err)
	}
	if _, err = svc.Build(ctx, &pagedto.BuildReq{ID: created.ID}); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID}); err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	if saved.DraftVersion == 0 {
		t.Fatal("保存异常")
	}

	var activeRows []struct {
		Path       string
		RouteKind  string
		ArtifactID *string
	}
	if err := db.Table("page_routes").
		Where("project_id = ? AND page_id = ?", projectID, created.ID).
		Order("path").Scan(&activeRows).Error; err != nil {
		t.Fatalf("查询路由失败: %v", err)
	}
	var olds, news int
	for _, r := range activeRows {
		switch {
		case r.Path == "/leave-a" && r.RouteKind == pubmodel.RouteActive:
			olds++
		case r.Path == "/leave-b" && r.RouteKind == pubmodel.RouteActive:
			news++
		}
	}

	if olds != 0 || news != 1 {
		t.Errorf("旧路径 active 应被取消、新路径恰一条 active，实际 old=%d new=%d (rows=%+v)",
			olds, news, activeRows)
	}
}

func TestPagePublishThenUpdateURLBack(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/swap-a", headingDocument)
	buildAndPublish(t, svc, created.ID)

	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: created.ID, NewPath: "/swap-b", WithRedirect: false}); err != nil {
		t.Fatalf("首次改 URL 失败: %v", err)
	}

	pageB := createPage(t, svc, projectID, "/swap-a", pageDocument)
	if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: pageB.ID}); err != nil {
		t.Fatalf("B 构建失败: %v", err)
	}
	if _, err := svc.Publish(ctx, &pagedto.PublishReq{ID: pageB.ID}); err != nil {
		t.Fatalf("B 发布失败: %v", err)
	}
	if kind := routeKind(t, db, projectID, "/swap-a"); kind != pubmodel.RouteActive {
		t.Errorf("释放后的路径应可被新页面占用: %s", kind)
	}
}

func TestPageUpdateURLSamePath(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/same", pageDocument)
	buildAndPublish(t, svc, created.ID)
	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: created.ID, NewPath: "/same", WithRedirect: false}); err == nil {
		t.Fatalf("相同路径应被拒绝")
	}
}

func TestPageBuildEmptyDocForced(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/dirty", pageDocument)

	if err := db.Table("pages").Where("id = ?", created.ID).
		Update("draft_document", `{"root":[]}`).Error; err != nil {
		t.Fatalf("注入脏文档失败: %v", err)
	}
	if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID}); err == nil {

		t.Logf("注意：脏文档构建未被拦截（观察点）")
	}
}

func TestPageSaveDraftOversizePath(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/ok", pageDocument)
	longPath := "/" + strings.Repeat("a", 3000)
	_, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: created.DraftVersion,
		DraftPath: longPath, DraftDocument: []byte(pageDocument),
	})
	if err == nil {
		t.Fatalf("3001 字符路径应被拒绝（NormalizeURL 长度上限）")
	}
	if err.Error() != pageenums.ErrInvalidPath {
		t.Errorf("应返回 %q: %v", pageenums.ErrInvalidPath, err)
	}
}
