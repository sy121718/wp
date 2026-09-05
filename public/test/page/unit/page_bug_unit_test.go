package unit

import (
	"context"
	"strings"
	"testing"

	"go_wp/internal/module/page/dto"
	pubmodel "go_wp/internal/module/publication/model"
)

// 本文件收录 bug 复现测试：断言「当前行为」并记录缺陷证据，
// 供修复后回归对照（修复后这些断言应翻转）。

// TestPageUpdateURLUnpublishedWithRedirect BUG 复现：
// 未发布页面调用 UpdateURL(WithRedirect=true) 时，service 在最后一步
// ensureRedirectRoute 中用 page.ActivePathValue()（未发布为空串）构造
// 重定向产物，NewRedirectArtifact("") 必然失败（路径不能为空）→
// UpdateURL 返回错误。但此时 FS 激活（publisher.UpdateURL）与 DB 迁移
// （MoveDraftPath/RenameReserved/Activate）已经完成：
//   - FS：新路径已激活为 page、旧路径已激活为 redirect（指向新路径）；
//   - DB：draft_path 已迁移、旧路径路由行已被改名/升级，无 redirect 行。
//
// 结果：调用方收到错误以为失败，实际线上已经迁移 → FS 与 DB 路由表分裂
// （旧路径 FS 有 301 而 DB 无占用记录），且后续基于错误基线继续操作。
func TestPageUpdateURLUnpublishedWithRedirect(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/u-old", pageDocument) // 未构建/未发布

	_, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{
		ID: created.ID, NewPath: "/u-new", WithRedirect: true,
	})
	if err == nil {
		// 若未来修复，此分支才是正确行为。
		t.Logf("UpdateURL 成功（修复后预期）")
		return
	}
	t.Logf("BUG 复现：UpdateURL 返回错误 %v", err)

	// 证据 1：DB 草稿路径已被迁移（MoveDraftPath 先于失败点执行）。
	detail, derr := svc.Detail(ctx, &pagedto.DetailReq{ID: created.ID})
	if derr != nil || detail.DraftPath != "/u-new" {
		t.Errorf("分裂证据：DB draft_path 已被迁移到 %q（err=%v）", detail.DraftPath, derr)
	}
	// 证据 2：FS 新路径已激活为 page 产物。
	if kind := activeKind(t, "/u-new"); kind != "page" {
		t.Errorf("分裂证据：FS /u-new 已激活为 %s", kind)
	}
	// 证据 3：FS 旧路径是 redirect 而 DB 无对应占用行（不一致）。
	if kind := activeKind(t, "/u-old"); kind != "redirect" {
		t.Errorf("分裂证据：FS /u-old 应为 redirect，实际 %s", kind)
	}
	var routeCount int64
	db.Table("page_routes").Where("project_id = ? AND path = ?", projectID, "/u-old").Count(&routeCount)
	if routeCount != 0 {
		t.Errorf("分裂证据：DB 应无 /u-old 占用行，实际 %d 行", routeCount)
	}
	// 证据 4：重试 UpdateURL 因路径重复而失败，操作不可恢复。
	if _, rerr := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{
		ID: created.ID, NewPath: "/u-old", WithRedirect: false,
	}); rerr == nil {
		t.Logf("重试可恢复（修复后预期）")
	}
}

// TestPagePublishAfterDraftPathChangeLeavesOldActiveRoute BUG 复现：
// 已发布页面先 SaveDraft 改草稿路径、再 Build+Publish 的路径变更，
// 只激活新路径的 reserved→active，旧路径已激活的 active 路由行不迁移、
// 不转 redirect、也不取消——同一页面在 page_routes 同时持有两条 active
// 占用，且旧路径继续指向旧产物（陈旧内容 + 路径永久占用）。
// 与专用流程 UpdateURL（旧路径 301/取消激活）行为不一致。
func TestPagePublishAfterDraftPathChangeLeavesOldActiveRoute(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/leave-a", headingDocument)
	h1 := buildAndPublish(t, svc, created.ID)
	_ = h1

	// 改草稿路径到 /leave-b 再发布。
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

	// 证据：旧路径 active 行残留（同页双 active 占用）。
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
	if olds != 1 || news != 1 {
		t.Errorf("分裂证据：期望旧路径与新路径各一条 active，实际 old=%d new=%d (rows=%+v)",
			olds, news, activeRows)
	}
}

// TestPagePublishThenUpdateURLBack 已发布页面 A、B：A 改 URL 到 B 的已取消路径
// 可复用（UpdateURL 取消激活会释放占用），验证 Deactivate 分支释放路径。
func TestPagePublishThenUpdateURLBack(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/swap-a", headingDocument)
	buildAndPublish(t, svc, created.ID)

	// A 从 /swap-a 改到 /swap-b。
	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: created.ID, NewPath: "/swap-b", WithRedirect: false}); err != nil {
		t.Fatalf("首次改 URL 失败: %v", err)
	}
	// 旧路径占用已释放，可再创建新页面占用。
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

// TestPageUpdateURLSamePath 新路径与当前路径相同应被拒绝（publisher 层校验）。
func TestPageUpdateURLSamePath(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/same", pageDocument)
	buildAndPublish(t, svc, created.ID)
	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: created.ID, NewPath: "/same", WithRedirect: false}); err == nil {
		t.Fatalf("相同路径应被拒绝")
	}
}

// TestPageBuildEmptyDocForced 通过 DB 注入脏文档（绕过 service 校验）后构建应失败
// 而不是污染产物（构建期二次校验兜底）。
func TestPageBuildEmptyDocForced(t *testing.T) {
	db, svc, _, projectID := newPageService(t)
	ctx := context.Background()
	created := createPage(t, svc, projectID, "/dirty", pageDocument)
	// 模拟历史脏数据：文档缺失 layout.mode。
	if err := db.Table("pages").Where("id = ?", created.ID).
		Update("draft_document", `{"root":[]}`).Error; err != nil {
		t.Fatalf("注入脏文档失败: %v", err)
	}
	if _, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID}); err == nil {
		// 若构建静默成功或产物异常，视为问题。
		t.Logf("注意：脏文档构建未被拦截（观察点）")
	}
}

// TestPageSaveDraftOversizePath 超长路径（>2000 字符）保存行为观察：
// NormalizeURL 无长度上限，超长路径可入库（低严重度观察，见报告）。
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
		// 观察点：超长路径未被拦截即入库（NormalizeURL 无长度限制）。
		t.Logf("观察点：3001 字符路径保存成功（NormalizeURL 无长度限制）")
		return
	}
	t.Logf("超长路径行为: %v", err)
}
