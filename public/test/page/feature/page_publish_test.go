package feature

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pubmodel "go_wp/internal/module/publication/model"

	"github.com/google/uuid"
)

const docV2 = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"你好"}}]}`

const docAlpha = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"Alpha页面"}}]}`

const docBeta = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"Beta页面"}}]}`

// TestPagePublishLifecycle 构建 → 发布 → 回滚全链路（0-A1 §2）。
func TestPagePublishLifecycle(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/about", DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建 Page 失败: %v", err)
	}

	// 构建产物就绪。
	built, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if built.Status != "ready" || built.StagedHash == "" {
		t.Fatalf("构建后应 ready: %+v", built)
	}

	// 发布激活。
	published, err := svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID})
	if err != nil {
		t.Fatalf("发布失败: %v", err)
	}
	if published.ActiveHash != built.StagedHash || published.Status != "published" {
		t.Fatalf("发布结果错误: %+v", published)
	}
	var routeKind, activeArtifact string
	db.Raw(`SELECT route_kind FROM page_routes WHERE project_id = ? AND path = ?`, projectID, "/about").Scan(&routeKind)
	db.Raw(`SELECT active_artifact_id FROM pages WHERE id = ?`, created.ID).Scan(&activeArtifact)
	if routeKind != pubmodel.RouteActive || activeArtifact == "" {
		t.Fatalf("发布后路由与指针错误: kind=%s active=%s", routeKind, activeArtifact)
	}

	// 草稿 v2 → 构建 → 发布（旧版本转历史）。
	saved, err := svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: created.DraftVersion,
		DraftPath: "/about", DraftDocument: json.RawMessage(docV2),
	})
	if err != nil {
		t.Fatalf("保存 v2 失败: %v", err)
	}
	built2, err := svc.Build(ctx, &pagedto.BuildReq{ID: created.ID})
	if err != nil {
		t.Fatalf("构建 v2 失败: %v", err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID}); err != nil {
		t.Fatalf("发布 v2 失败: %v", err)
	}

	// 秒级回滚到 v1。
	rolledBack, err := svc.Rollback(ctx, &pagedto.RollbackReq{ID: created.ID, TargetHash: built.StagedHash})
	if err != nil {
		t.Fatalf("回滚失败: %v", err)
	}
	if rolledBack.ActiveHash != built.StagedHash || rolledBack.ActiveHash == built2.StagedHash {
		t.Fatalf("回滚目标错误: %+v", rolledBack)
	}

	// 版本冲突保护依旧生效。
	if _, err = svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: saved.DraftVersion - 1,
		DraftPath: "/about", DraftDocument: json.RawMessage(pageDocument),
	}); err == nil || err.Error() != pageenums.ErrDraftVersionConflict {
		t.Fatalf("旧版本保存应被拒绝: %v", err)
	}

	// URL 变更：新路径激活 + 草稿路径同步。
	moved, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: created.ID, NewPath: "/about-us/", WithRedirect: false})
	if err != nil {
		t.Fatalf("URL 更新失败: %v", err)
	}
	if moved.OldPath != "/about" || moved.DraftPath != "/about-us" {
		t.Fatalf("URL 结果错误: %+v", moved)
	}
	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ID: created.ID})
	if err != nil || detail.DraftPath != "/about-us" || detail.ActivePath == nil || *detail.ActivePath != "/about-us" {
		t.Fatalf("改 URL 后详情错误: %+v err=%v", detail, err)
	}
}

// createAndPublish 创建页面并完成 构建 → 发布，返回页面实体投影。
func createAndPublish(t *testing.T, svc pagecontract.PageService, projectID, path, doc string) *pagedto.PageResp {
	t.Helper()
	ctx := context.Background()
	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: path, DraftDocument: json.RawMessage(doc),
	})
	if err != nil {
		t.Fatalf("创建 Page(%s) 失败: %v", path, err)
	}
	if _, err = svc.Build(ctx, &pagedto.BuildReq{ID: created.ID}); err != nil {
		t.Fatalf("构建 Page(%s) 失败: %v", path, err)
	}
	if _, err = svc.Publish(ctx, &pagedto.PublishReq{ID: created.ID}); err != nil {
		t.Fatalf("发布 Page(%s) 失败: %v", path, err)
	}
	return created
}

// TestPageUpdateURLBindsArtifactRowUUID C3 回归：改 URL 后路由 artifact_id
// 必须是 page_artifacts 行主键（合法 uuid），而非 64 位内容 hash——
// 生产 DDL 中该列为 uuid，写 hash 必然 22P02 失败。
func TestPageUpdateURLBindsArtifactRowUUID(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()
	created := createAndPublish(t, svc, projectID, "/about", docV2)

	moved, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: created.ID, NewPath: "/about-us", WithRedirect: false})
	if err != nil {
		t.Fatalf("URL 更新失败: %v", err)
	}

	var routeArtifactID string
	if err := db.Raw(`SELECT artifact_id FROM page_routes WHERE project_id = ? AND path = ?`,
		projectID, "/about-us").Scan(&routeArtifactID).Error; err != nil || routeArtifactID == "" {
		t.Fatalf("读取路由 artifact_id 失败: %q err=%v", routeArtifactID, err)
	}
	if _, perr := uuid.Parse(routeArtifactID); perr != nil {
		t.Fatalf("路由 artifact_id 应为合法 uuid，实际: %q (%v)", routeArtifactID, perr)
	}
	// 且等于该内容 hash 对应的 page_artifacts 行主键。
	var rowID string
	if err := db.Raw(`SELECT id FROM page_artifacts WHERE page_id = ? AND artifact_hash = ?`,
		created.ID, moved.ActiveHash).Scan(&rowID).Error; err != nil {
		t.Fatalf("查询产物行失败: %v", err)
	}
	if rowID == "" || rowID != routeArtifactID {
		t.Fatalf("路由应绑定产物行 ID: route=%s artifactRow=%s", routeArtifactID, rowID)
	}
}

// TestPageUpdateURLRejectsOccupiedPath H7 回归：新路径已被其他页面占用时，
// 改 URL 必须在 FS 激活前失败——占用者的线上内容、DB 归属与失败者草稿
// 均保持不变，不出现「FS 先覆盖、DB 后报错」的状态分裂。
func TestPageUpdateURLRejectsOccupiedPath(t *testing.T) {
	db, svc, projectID := newPageService(t)
	ctx := context.Background()
	pageA := createAndPublish(t, svc, projectID, "/a", docAlpha)
	createAndPublish(t, svc, projectID, "/b", docBeta)

	if _, err := svc.UpdateURL(ctx, &pagedto.UpdateURLReq{ID: pageA.ID, NewPath: "/b", WithRedirect: false}); err == nil ||
		!strings.Contains(err.Error(), pageenums.ErrPathOccupied) {
		t.Fatalf("新路径被他人占用应被拒绝: %v", err)
	}

	// FS 未被越权覆盖：/b 仍直出 B 的内容。
	activeDir := filepath.Join(artifactRootOf(t), "public", "active")
	data, rerr := os.ReadFile(filepath.Join(activeDir, "b", "index.html"))
	if rerr != nil || !strings.Contains(string(data), "Beta页面") {
		t.Fatalf("FS 应保持 B 的激活产物: %v %q", rerr, data)
	}
	// DB 归属未变：/b 仍为 active 且归属 B。
	row := db.Table("page_routes").Select("route_kind", "page_id").
		Where("project_id = ? AND path = ?", projectID, "/b").Row()
	var kind string
	var ownerID *string
	if err := row.Scan(&kind, &ownerID); err != nil {
		t.Fatalf("读取路由归属失败: %v", err)
	}
	if kind != pubmodel.RouteActive || ownerID == nil {
		t.Fatalf("/b 路由应保持 active: kind=%s owner=%v", kind, ownerID)
	}
	detailB, err := svc.Detail(ctx, &pagedto.DetailReq{ID: *ownerID})
	if err != nil || detailB.DraftPath != "/b" {
		t.Fatalf("占用者页面详情错误: %+v err=%v", detailB, err)
	}
	// 失败者草稿路径保持原值，未发生迁移。
	detailA, err := svc.Detail(ctx, &pagedto.DetailReq{ID: pageA.ID})
	if err != nil || detailA.DraftPath != "/a" {
		t.Fatalf("被拒绝方草稿路径不应迁移: %+v err=%v", detailA, err)
	}
}
