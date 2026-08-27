package feature

import (
	"context"
	"encoding/json"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pubmodel "go_wp/internal/module/publication/model"
)

const docV2 = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"你好"}}]}`

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
