package feature

import (
	"context"
	"strings"
	"testing"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/public/test/support"
)

// uuid 列与生产 DDL 对齐（init_builder_schema.sql：page_routes/publication_receipts），
// 用例值一律使用合法 uuid。
const (
	projectID = "cccccccc-0000-0000-0000-000000000001"
	pageID    = "dddddddd-0000-0000-0000-000000000001"
	// otherPageID 其他页面的占用归属（占用冲突用例）。
	otherPageID = "dddddddd-0000-0000-0000-000000000002"
	// missingPageID 从未建立占用的页面。
	missingPageID = "ffffffff-0000-0000-0000-000000000009"

	artifactUUID         = "11111111-1111-1111-1111-111111111101"
	redirectArtifactUUID = "11111111-1111-1111-1111-111111111102"
	pendingArtifactUUID  = "11111111-1111-1111-1111-111111111103"
)

func newPublicationService(t *testing.T) *pubservice.Service {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil
	}
	for _, statement := range []string{
		`CREATE TABLE page_routes (project_id UUID NOT NULL, path TEXT NOT NULL, page_id UUID, presentation_id UUID, route_kind TEXT NOT NULL, artifact_id UUID, updated_at TIMESTAMPTZ NOT NULL, PRIMARY KEY(project_id, path))`,
		`CREATE TABLE publication_receipts (id UUID PRIMARY KEY, source_type TEXT NOT NULL, source_id UUID NOT NULL, action TEXT NOT NULL, path TEXT NOT NULL, from_artifact_id UUID, to_artifact_id UUID, receipt_state TEXT NOT NULL, receipt_data JSON NOT NULL, created_at TIMESTAMPTZ NOT NULL, completed_at TIMESTAMPTZ)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("创建测试表失败: %v", err)
		}
	}
	return pubservice.NewService(pubmodel.NewPublicationModel(db))
}

func seedReservedRoute(t *testing.T, svc *pubservice.Service, path, pageID string) {
	t.Helper()
	if err := svc.Model().RouteDB(context.Background()).Create(&pubmodel.RouteEntity{
		ProjectID: projectID, Path: path, PageID: strPtr(pageID),
		RouteKind: pubmodel.RouteReserved, UpdatedAt: timeNow(),
	}).Error; err != nil {
		t.Fatalf("准备 reserved 占用失败: %v", err)
	}
}

func TestPublicationActivateAndDeactivate(t *testing.T) {
	svc := newPublicationService(t)
	ctx := context.Background()
	seedReservedRoute(t, svc, "/about", pageID)

	route, err := svc.Activate(ctx, &pubdto.ActivateReq{
		ProjectID: projectID, Path: "/about/", PageID: pageID, ArtifactID: artifactUUID,
	})
	if err != nil {
		t.Fatalf("激活失败: %v", err)
	}
	if route.RouteKind != pubmodel.RouteActive || route.ArtifactID == nil || *route.ArtifactID != artifactUUID {
		t.Fatalf("激活后路由状态错误: %+v", route)
	}
	var committed int64
	checkDB(ctx, t, svc, "publication_receipts", "receipt_state = ?", pubmodel.ReceiptCommitted, &committed)
	if committed != 1 {
		t.Fatalf("应有 1 条 committed 回执: %d", committed)
	}

	if err = svc.Deactivate(ctx, &pubdto.DeactivateReq{ProjectID: projectID, Path: "/about"}); err != nil {
		t.Fatalf("取消激活失败: %v", err)
	}
	var remaining int64
	checkDB(ctx, t, svc, "page_routes", "path = ?", "/about", &remaining)
	if remaining != 0 {
		t.Fatalf("取消激活后路由应删除: %d", remaining)
	}
}

// TestPublicationActivateRejectsForeignRoute 目标路径已被其他页面占用时，
// Activate 必须拒绝（ErrRouteOccupied），pending 回执补偿为 rolled_back，
// 且原占用行保持不变（H2/H7 的 DB 层兜底 + H6 两段回执）。
func TestPublicationActivateRejectsForeignRoute(t *testing.T) {
	svc := newPublicationService(t)
	ctx := context.Background()
	seedReservedRoute(t, svc, "/taken", otherPageID)

	_, err := svc.Activate(ctx, &pubdto.ActivateReq{
		ProjectID: projectID, Path: "/taken", PageID: pageID, ArtifactID: artifactUUID,
	})
	if err == nil || !strings.Contains(err.Error(), pubenums.ErrRouteOccupied) {
		t.Fatalf("他人占用应被拒绝: %v", err)
	}
	var rolledBack int64
	checkDB(ctx, t, svc, "publication_receipts", "receipt_state = ?", pubmodel.ReceiptRolledBack, &rolledBack)
	if rolledBack != 1 {
		t.Fatalf("路由事务失败后 pending 回执应补偿为 rolled_back: %d", rolledBack)
	}
	var pending int64
	checkDB(ctx, t, svc, "publication_receipts", "receipt_state = ?", pubmodel.ReceiptPending, &pending)
	if pending != 0 {
		t.Fatalf("不应残留 pending 回执: %d", pending)
	}
	// 原占用行保持不变。
	route, gerr := svc.Model().GetRoute(ctx, projectID, "/taken")
	if gerr != nil || route.RouteKind != pubmodel.RouteReserved || route.PageID == nil || *route.PageID != otherPageID {
		t.Fatalf("原占用应保持不变: %+v err=%v", route, gerr)
	}
}

// TestPublicationRenameReservedConflict 新路径已被其他页面占用时
// RenameReserved 必须返回错误并保持原占用不变（M8：改名失败上游中止流程）。
func TestPublicationRenameReservedConflict(t *testing.T) {
	svc := newPublicationService(t)
	ctx := context.Background()
	seedReservedRoute(t, svc, "/old", pageID)
	seedReservedRoute(t, svc, "/new", otherPageID)

	err := svc.RenameReserved(ctx, &pubdto.RenameReservedReq{
		ProjectID: projectID, PageID: pageID, OldPath: "/old", NewPath: "/new",
	})
	if err == nil {
		t.Fatal("新路径被他人占用时改名应失败")
	}
	route, gerr := svc.Model().GetRoute(ctx, projectID, "/old")
	if gerr != nil || route.RouteKind != pubmodel.RouteReserved || route.PageID == nil || *route.PageID != pageID {
		t.Fatalf("改名失败后原占用应保持不变: %+v err=%v", route, gerr)
	}
}

func TestPublicationRedirectAndNotFound(t *testing.T) {
	svc := newPublicationService(t)
	ctx := context.Background()
	seedReservedRoute(t, svc, "/old", pageID)

	route, err := svc.Redirect(ctx, &pubdto.RedirectReq{
		ProjectID: projectID, OldPath: "/old", PageID: pageID, ArtifactID: redirectArtifactUUID,
	})
	if err != nil {
		t.Fatalf("重定向标记失败: %v", err)
	}
	if route.RouteKind != pubmodel.RouteRedirect {
		t.Fatalf("旧路径应为 redirect: %+v", route)
	}
	if _, err = svc.Activate(ctx, &pubdto.ActivateReq{
		ProjectID: projectID, Path: "/missing", PageID: missingPageID, ArtifactID: artifactUUID,
	}); err != nil {
		t.Fatalf("幂等激活应成功建立占用: %v", err)
	}
}

func TestPublicationPendingReceiptRollback(t *testing.T) {
	svc := newPublicationService(t)
	ctx := context.Background()
	seedReservedRoute(t, svc, "/pending", pageID)
	// 制造 pending 回执：直接插入（模拟进程中断后遗留）。
	if err := svc.Model().ReceiptDB(ctx).Create(&pubmodel.ReceiptEntity{
		ID: "eeeeeeee-0000-0000-0000-000000000001", SourceType: "page", SourceID: pageID,
		Action: "activate", Path: "/pending", ToArtifact: strPtr(pendingArtifactUUID),
		ReceiptState: pubmodel.ReceiptPending,
		ReceiptData:  []byte(`{}`), CreatedAt: timeNow(),
	}).Error; err != nil {
		t.Fatalf("制造 pending 回执失败: %v", err)
	}
	count, err := svc.RollbackReceipts(ctx)
	if err != nil || count != 1 {
		t.Fatalf("恢复应处理 1 条回执: count=%d err=%v", count, err)
	}
	pending, err := svc.Model().ListPendingReceipts(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("恢复后不应再有 pending: %+v err=%v", pending, err)
	}
}
