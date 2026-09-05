package unit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pageenums "go_wp/internal/module/page/enums"
	pagedto "go_wp/internal/module/page/dto"
	pubmodel "go_wp/internal/module/publication/model"
)

// activeDir 返回当前测试的 FS 激活根目录。
func activeDir(t *testing.T) string {
	t.Helper()
	root := os.Getenv("GO_WP_ARTIFACT_ROOT")
	if root == "" {
		t.Fatal("GO_WP_ARTIFACT_ROOT 未设置")
	}
	return filepath.Join(root, "public", "active")
}

// activeRedirectTarget 读取激活路径的 redirect 指令目标（无 redirect 时返回空）。
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

// activeKind 读取激活路径状态（page/redirect/none）。
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

// ---- 构建 ----

// TestPageBuildSuccess 合法草稿构建：状态 ready、staged hash 非空、staged 指针回写。
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

// TestPageBuildNotFound 不存在的页面构建应返回页面不存在。
func TestPageBuildNotFound(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	_, err := svc.Build(context.Background(), &pagedto.BuildReq{ID: "6f2c9d0e-1a2b-3c4d-8e9f-0a1b2c3d4e5f"})
	if err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Fatalf("应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
}

// TestPageBuildNilRequest nil / 空 ID 构建请求应被拒绝。
func TestPageBuildNilRequest(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	if _, err := svc.Build(context.Background(), nil); err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Errorf("nil 请求应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
	if _, err := svc.Build(context.Background(), &pagedto.BuildReq{}); err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Errorf("空 ID 应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
}

// TestPageBuildVersionConflict 构建携带过期 ExpectedVersion 应被拒绝。
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

// ---- 发布 ----

// TestPagePublishNoStagedArtifact 未构建直接发布应返回无暂存产物。
func TestPagePublishNoStagedArtifact(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	created := createPage(t, svc, projectID, "/no-staged", pageDocument)
	_, err := svc.Publish(context.Background(), &pagedto.PublishReq{ID: created.ID})
	if err == nil || err.Error() != pageenums.ErrNoStagedArtifact {
		t.Fatalf("应返回 %q: %v", pageenums.ErrNoStagedArtifact, err)
	}
}

// TestPagePublishNotFound 不存在的页面发布应返回页面不存在。
func TestPagePublishNotFound(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	_, err := svc.Publish(context.Background(), &pagedto.PublishReq{ID: "6f2c9d0e-1a2b-3c4d-8e9f-0a1b2c3d4e5f"})
	if err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Fatalf("应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
}

// TestPagePublishNilRequest nil / 空 ID 发布请求应被拒绝。
func TestPagePublishNilRequest(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	if _, err := svc.Publish(context.Background(), nil); err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Errorf("nil 请求应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
}

// TestPagePublishSuccess 发布后：active hash、路由 active、active_artifact_id 回写。
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

// TestPagePublishRebuildRequired BUG 复现：构建后再保存草稿（未重新构建）直接发布，
// 预期 ErrRebuildRequired；实际发布成功且发布的是旧内容——
// Publish 的二次构建校验用 stagedArt.SourceDocument（构建时冻结文档）重建内核，
// DraftDocumentFor 优先返回冻结文档，恒与暂存 hash 一致，草稿变更永不触发
// ErrRebuildRequired；MarkPublished 把 stale 置 false，界面呈现「已发布且最新」，
// 但线上内容停留在旧版本（数据一致性缺陷，见报告高严重度项）。
func TestPagePublishRebuildRequired(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/rebuild", pageDocument)
	if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID}); err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	// 构建后草稿变更（v2），不重新构建。
	saved, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: created.DraftVersion,
		DraftPath: "/rebuild", DraftDocument: json.RawMessage(headingDocument),
	})
	if err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	// 修复语义：草稿与暂存产物不一致，发布必须拒绝（ErrRebuildRequired），
	// 不得静默发布旧内容后把 stale 置 false 掩盖不一致。
	_, perr := svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID})
	if perr == nil || !strings.Contains(perr.Error(), pageenums.ErrRebuildRequired) {
		t.Fatalf("草稿变更后发布应拒绝 ErrRebuildRequired，实际 %v", perr)
	}
	// 未发布旧内容：active_artifact_id 保持为空（创建后未发布过）。
	var activeArtifact string
	db.Raw(`SELECT active_artifact_id FROM pages WHERE id = ?`, created.ID).Scan(&activeArtifact)
	if activeArtifact != "" {
		t.Errorf("发布被拒后不应写入 active_artifact_id: %q", activeArtifact)
	}
	// 草稿版本保持 2，未被发布流程改动。
	var draftVersion int64
	db.Raw(`SELECT draft_version FROM pages WHERE id = ?`, created.ID).Scan(&draftVersion)
	if draftVersion != saved.DraftVersion {
		t.Errorf("草稿版本应保持 %d: %d", saved.DraftVersion, draftVersion)
	}
	// 补偿路径：重新构建后可正常发布。
	if _, err = svc.Build(ctx, &pagedto.BuildReq{ID: created.ID}); err != nil {
		t.Fatalf("重新构建失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID}); err != nil {
		t.Fatalf("重新构建后发布应成功: %v", err)
	}
}

// ---- 回滚 ----

// TestPageRollbackTargetMiss 不存在的目标 hash 回滚应返回回滚目标缺失。
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

// TestPageRollbackSuccess 两版本发布后可秒级回滚到 v1 产物。
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

// TestPageRollbackNilRequest nil / 空 ID / 空 hash 回滚请求应被拒绝。
func TestPageRollbackNilRequest(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	if _, err := svc.Rollback(context.Background(), nil); err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Errorf("nil 请求应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
	if _, err := svc.Rollback(context.Background(), &pagedto.RollbackReq{ID: "x"}); err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Errorf("空 hash 应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
}

// ---- URL 变更 ----

// TestPageUpdateURLSuccess 改 URL（无重定向）：新路径激活、草稿/激活路径同步、旧路径取消激活。
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
	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ID: created.ID})
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

// TestPageUpdateURLWithRedirect301 改 URL（带重定向）：FS 旧路径 301 指向新路径，
// DB 旧路径行标记 redirect。
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

// TestPageUpdateURLNilRequest nil / 空 ID / 空路径请求应被拒绝。
func TestPageUpdateURLNilRequest(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	if _, err := svc.UpdateURL(context.Background(), nil); err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Errorf("nil 请求应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
	if _, err := svc.UpdateURL(context.Background(), &pagedto.UpdateURLReq{NewPath: "/x"}); err == nil || err.Error() != pageenums.ErrPageNotFound {
		t.Errorf("空 ID 应返回 %q: %v", pageenums.ErrPageNotFound, err)
	}
}

// TestPageUpdateURLRejectsOccupiedPath 新路径被他人占用：改 URL 应在 FS 激活前失败，
// 双方 DB 与 FS 状态均不变（H7）。
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
	// FS 未被越权覆盖：/occ-b 仍直出 B。
	if kind := activeKind(t, "/occ-b"); kind != "page" {
		t.Errorf("占用者 FS 应保持: %s", kind)
	}
	// DB 归属未变。
	if kind := routeKind(t, db, projectID, "/occ-b"); kind != pubmodel.RouteActive {
		t.Errorf("占用者路由应保持 active: %s", kind)
	}
	// 失败方草稿路径未迁移。
	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ID: pageA.ID})
	if err != nil || detail.DraftPath != "/occ-a" {
		t.Errorf("被拒绝方草稿路径不应迁移: %+v err=%v", detail, err)
	}
}
