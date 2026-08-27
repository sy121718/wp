package feature

import (
	"context"
	"testing"

	pubdto "go_wp/internal/module/publication/dto"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

const (
	projectID = "cccccccc-0000-0000-0000-000000000001"
	pageID    = "dddddddd-0000-0000-0000-000000000001"
)

func newPublicationService(t *testing.T) *pubservice.Service {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE page_routes (project_id TEXT NOT NULL, path TEXT NOT NULL, page_id TEXT, presentation_id TEXT, route_kind TEXT NOT NULL, artifact_id TEXT, updated_at DATETIME NOT NULL, PRIMARY KEY(project_id, path))`,
		`CREATE TABLE publication_receipts (id TEXT PRIMARY KEY, source_type TEXT NOT NULL, source_id TEXT NOT NULL, action TEXT NOT NULL, path TEXT NOT NULL, from_artifact_id TEXT, to_artifact_id TEXT, receipt_state TEXT NOT NULL, receipt_data JSON NOT NULL, created_at DATETIME NOT NULL, completed_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("创建测试表失败: %v", err)
		}
	}
	return pubservice.NewService(pubmodel.NewPublicationModel(db))
}

func seedReservedRoute(t *testing.T, svc *pubservice.Service, path string) {
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
	seedReservedRoute(t, svc, "/about")

	route, err := svc.Activate(ctx, &pubdto.ActivateReq{
		ProjectID: projectID, Path: "/about/", PageID: pageID, ArtifactID: "hash-1",
	})
	if err != nil {
		t.Fatalf("激活失败: %v", err)
	}
	if route.RouteKind != pubmodel.RouteActive || route.ArtifactID == nil || *route.ArtifactID != "hash-1" {
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

func TestPublicationRedirectAndNotFound(t *testing.T) {
	svc := newPublicationService(t)
	ctx := context.Background()
	seedReservedRoute(t, svc, "/old")

	route, err := svc.Redirect(ctx, &pubdto.RedirectReq{
		ProjectID: projectID, OldPath: "/old", PageID: pageID, ArtifactID: "redirect-hash",
	})
	if err != nil {
		t.Fatalf("重定向标记失败: %v", err)
	}
	if route.RouteKind != pubmodel.RouteRedirect {
		t.Fatalf("旧路径应为 redirect: %+v", route)
	}
	if _, err = svc.Activate(ctx, &pubdto.ActivateReq{
		ProjectID: projectID, Path: "/missing", PageID: "ffffffff-0000-0000-0000-000000000009", ArtifactID: "x",
	}); err != nil {
		t.Fatalf("幂等激活应成功建立占用: %v", err)
	}
}

func TestPublicationPendingReceiptRollback(t *testing.T) {
	svc := newPublicationService(t)
	ctx := context.Background()
	seedReservedRoute(t, svc, "/pending")
	// 制造 pending 回执：直接插入（模拟进程中断后遗留）。
	if err := svc.Model().ReceiptDB(ctx).Create(&pubmodel.ReceiptEntity{
		ID: "eeeeeeee-0000-0000-0000-000000000001", SourceType: "page", SourceID: pageID,
		Action: "activate", Path: "/pending", ToArtifact: strPtr("hash-x"),
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
