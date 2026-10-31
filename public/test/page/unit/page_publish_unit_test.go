package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pubmodel "go_wp/internal/module/publication/model"
)

func activeDir(t *testing.T) string {
	t.Helper()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	if root == "" {
		t.Fatal("GO_WP_ARTIFACT_ROOT 未设置")
	}
	return filepath.Join(root, "public", "active")
}

func activeRedirectTarget(t *testing.T, path string) string {
	t.Helper()
	link := filepath.Join(activeDir(t), strings.TrimPrefix(path, "/"))
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("解析激活链接失败: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(resolved, "redirect.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("读取 redirect.json 失败: %v", err)
	}
	var d struct {
		TargetPath string `json:"targetPath"`
	}
	if err := json.Unmarshal(data, &d); err != nil {
		t.Fatalf("解析 redirect.json 失败: %v", err)
	}
	return d.TargetPath
}

func activeKind(t *testing.T, path string) string {
	t.Helper()
	link := filepath.Join(activeDir(t), strings.TrimPrefix(path, "/"))
	if _, err := os.Lstat(link); err != nil {
		if os.IsNotExist(err) {
			return "none"
		}
		t.Fatalf("Lstat 失败: %v", err)
	}
	if target := activeRedirectTarget(t, path); target != "" {
		return "redirect"
	}
	return "page"
}

func activeLinkTarget(t *testing.T, path string) string {
	t.Helper()
	link := filepath.Join(activeDir(t), strings.TrimPrefix(path, "/"))
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("读取访问面链接失败: %v", err)
	}
	return target
}

func TestPageBuildSuccess(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/build", headingDocument)

	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if built.Status != "ready" || built.StagedHash == "" {
		t.Errorf("构建后应 ready 且带 hash: %+v", built)
	}
	var stagedID string
	db.Raw(`SELECT staged_artifact_id FROM pages WHERE id = ?`, created.ID).Scan(&stagedID)
	if stagedID == "" {
		t.Errorf("构建后 staged_artifact_id 应回写")
	}
}

func TestPageBuildNotFound(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	_, err := svc.Build(context.Background(), &pagedto.BuildReq{ID: "6f2c9d0e-1a2b-3c4d-8e9f-0a1b2c3d4e5f"})
	if err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Fatalf("应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
}

func TestPageBuildNilRequest(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	if _, err := svc.Build(context.Background(), nil); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("nil 请求应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
	if _, err := svc.Build(context.Background(), &pagedto.BuildReq{}); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("空 ID 应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
}

func TestPageBuildVersionConflict(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/build-vc", pageDocument)
	saved, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: created.DraftVersion,
		DraftPath: "/build-vc", DraftDocument: json.RawMessage(headingDocument),
	})
	if err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID, ExpectedVersion: saved.DraftVersion - 1}); err == nil ||
		err.Error() != pageenums.ErrDraftVersionConflict {
		t.Fatalf("过期版本构建应被拒绝: %v", err)
	}
}

func TestPagePublishNoStagedArtifact(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	created := createPage(t, svc, projectID, "/no-staged", pageDocument)
	_, err := svc.Publish(context.Background(), &pagedto.PublishReq{ID: created.ID})
	if err == nil || err.Error() != pageenums.ErrNoStagedArtifact {
		t.Fatalf("应返回 %q: %v", pageenums.ErrNoStagedArtifact, err)
	}
}

func TestPagePublishNotFound(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	_, err := svc.Publish(context.Background(), &pagedto.PublishReq{ID: "6f2c9d0e-1a2b-3c4d-8e9f-0a1b2c3d4e5f"})
	if err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Fatalf("应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
}

func TestPagePublishNilRequest(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	if _, err := svc.Publish(context.Background(), nil); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("nil 请求应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
	if _, err := svc.Publish(context.Background(), &pagedto.PublishReq{}); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("空 ID 应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
}

func TestPagePublishSuccess(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/pub", headingDocument)

	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	published, err := svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID})
	if err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	if published.ActiveHash != built.StagedHash || published.Status != "published" {
		t.Errorf("发布结果错误: %+v", published)
	}
	if kind := routeKind(t, db, projectID, "/pub"); kind != pubmodel.RouteActive {
		t.Errorf("发布后路由应为 active: %s", kind)
	}
	var activeArtifact, activePath string
	db.Raw(`SELECT active_artifact_id FROM pages WHERE id = ?`, created.ID).Scan(&activeArtifact)
	db.Raw(`SELECT active_path FROM pages WHERE id = ?`, created.ID).Scan(&activePath)
	if activeArtifact == "" || activePath != "/pub" {
		t.Errorf("发布后指针未回写: artifact=%s path=%s", activeArtifact, activePath)
	}
	if kind := activeKind(t, "/pub"); kind != "page" {
		t.Errorf("FS 应激活 page 产物: %s", kind)
	}
}

func TestPagePublishRebuildRequired(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/rebuild", pageDocument)
	if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID}); err != nil {
		t.Fatalf("构建失败: %v", err)
	}

	saved, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: created.DraftVersion,
		DraftPath: "/rebuild", DraftDocument: json.RawMessage(headingDocument),
	})
	if err != nil {
		t.Fatalf("保存失败: %v", err)
	}

	_, perr := svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID})
	if perr == nil || !strings.Contains(perr.Error(), pageenums.ErrRebuildRequired) {
		t.Fatalf("草稿变更后发布应拒绝 ErrRebuildRequired，实际 %v", perr)
	}

	var activeArtifact string
	db.Raw(`SELECT active_artifact_id FROM pages WHERE id = ?`, created.ID).Scan(&activeArtifact)
	if activeArtifact != "" {
		t.Errorf("发布被拒后不应写入 active_artifact_id: %q", activeArtifact)
	}

	var draftVersion int64
	db.Raw(`SELECT draft_version FROM pages WHERE id = ?`, created.ID).Scan(&draftVersion)
	if draftVersion != saved.DraftVersion {
		t.Errorf("草稿版本应保持 %d: %d", saved.DraftVersion, draftVersion)
	}

	if _, err = svc.Build(ctx, &pagedto.BuildReq{ID: created.ID}); err != nil {
		t.Fatalf("重新构建失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID}); err != nil {
		t.Fatalf("重新构建后发布应成功: %v", err)
	}
}

func TestPageRollbackTargetMiss(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	created := createPage(t, svc, projectID, "/rb-miss", pageDocument)
	_, err := svc.Rollback(context.Background(), &pagedto.RollbackReq{
		ID: created.ID, TargetHash: strings.Repeat("0", 64),
	})
	if err == nil || err.Error() != pageenums.ErrRollbackTargetMiss {
		t.Fatalf("应返回 %q: %v", pageenums.ErrRollbackTargetMiss, err)
	}
}

func TestPageRollbackSuccess(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/rb", pageDocument)
	h1 := buildAndPublish(t, svc, created.ID)

	saved, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: created.DraftVersion,
		DraftPath: "/rb", DraftDocument: json.RawMessage(headingDocument),
	})
	if err != nil {
		t.Fatalf("保存 v2 失败: %v", err)
	}
	_ = saved
	buildAndPublish(t, svc, created.ID)

	rolled, err := svc.Rollback(ctx, &pagedto.RollbackReq{ID: created.ID, TargetHash: h1})
	if err != nil {
		t.Fatalf("回滚失败: %v", err)
	}
	if rolled.ActiveHash != h1 {
		t.Errorf("回滚目标错误: %+v", rolled)
	}
	var activeArtifact string
	db.Raw(`SELECT active_artifact_id FROM pages WHERE id = ?`, created.ID).Scan(&activeArtifact)
	if activeArtifact == "" {
		t.Errorf("回滚后 active_artifact_id 应回写")
	}
}

func TestPageRollbackNilRequest(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	if _, err := svc.Rollback(context.Background(), nil); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("nil 请求应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
	if _, err := svc.Rollback(context.Background(), &pagedto.RollbackReq{ID: "x"}); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("空 hash 应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
	if _, err := svc.Rollback(context.Background(), &pagedto.RollbackReq{TargetHash: "hash"}); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("空 ID 应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
}

func TestPageUpdateURLSuccess(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/old-url", headingDocument)
	buildAndPublish(t, svc, created.ID)

	moved, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: created.ID, NewPath: "/new-url", WithRedirect: false})
	if err != nil {
		t.Fatalf("URL 更新失败: %v", err)
	}
	if moved.OldPath != "/old-url" || moved.DraftPath != "/new-url" {
		t.Errorf("URL 结果错误: %+v", moved)
	}
	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: created.ID})
	if err != nil || detail.DraftPath != "/new-url" || detail.ActivePath == nil || *detail.ActivePath != "/new-url" {
		t.Errorf("改 URL 后详情错误: %+v err=%v", detail, err)
	}
	if kind := routeKind(t, db, projectID, "/new-url"); kind != pubmodel.RouteActive {
		t.Errorf("新路径应为 active: %s", kind)
	}
	if activeKind(t, "/old-url") != "none" {
		t.Errorf("旧路径应取消激活")
	}
}

func TestPageUpdateURLWithRedirect301(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/r-old", headingDocument)
	buildAndPublish(t, svc, created.ID)

	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: created.ID, NewPath: "/r-new", WithRedirect: true}); err != nil {
		t.Fatalf("带重定向的 URL 更新失败: %v", err)
	}
	if target := activeRedirectTarget(t, "/r-old"); target != "/r-new" {
		t.Errorf("旧路径 FS 301 应指向新路径: %q", target)
	}
	if kind := routeKind(t, db, projectID, "/r-old"); kind != "redirect" {
		t.Errorf("旧路径 DB 行应为 redirect: %s", kind)
	}
	if activeKind(t, "/r-new") != "page" {
		t.Errorf("新路径应激活 page 产物")
	}
}

func TestPageUpdateURLNilRequest(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	if _, err := svc.UpdateURL(context.Background(), nil); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("nil 请求应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
	if _, err := svc.UpdateURL(context.Background(), &pagedto.UpdateURLReq{NewPath: "/x"}); err == nil || err.Error() != pageenums.ErrInvalidParam {
		t.Errorf("空 ID 应返回 %q: %v", pageenums.ErrInvalidParam, err)
	}
}

func TestPageUpdateURLRejectsOccupiedPath(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	pageA := createPage(t, svc, projectID, "/occ-a", headingDocument)
	buildAndPublish(t, svc, pageA.ID)
	pageB := createPage(t, svc, projectID, "/occ-b", headingDocument)
	buildAndPublish(t, svc, pageB.ID)

	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: pageA.ID, NewPath: "/occ-b", WithRedirect: false}); err == nil ||
		!strings.Contains(err.Error(), pageenums.ErrPathOccupied) {
		t.Fatalf("占用路径应被拒绝: %v", err)
	}

	if kind := activeKind(t, "/occ-b"); kind != "page" {
		t.Errorf("占用者 FS 应保持: %s", kind)
	}

	if kind := routeKind(t, db, projectID, "/occ-b"); kind != pubmodel.RouteActive {
		t.Errorf("占用者路由应保持 active: %s", kind)
	}

	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: pageA.ID})
	if err != nil || detail.DraftPath != "/occ-a" {
		t.Errorf("被拒绝方草稿路径不应迁移: %+v err=%v", detail, err)
	}
}
