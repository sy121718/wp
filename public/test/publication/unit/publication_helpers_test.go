package unit

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/public/test/support"

	"gorm.io/gorm"
)

const (
	projectID    = "cccccccc-0000-0000-0000-000000000001"
	pageID       = "dddddddd-0000-0000-0000-000000000001"
	otherPageID  = "dddddddd-0000-0000-0000-000000000002"
	artifactUUID = "11111111-1111-1111-1111-111111111101"
)

func newUnitService(t *testing.T) *pubservice.Service {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	support.SeedProjectRow(t, db, projectID, "publication 测试站点")
	return pubservice.NewService(pubmodel.NewPublicationModel(db))
}

var presentationFixtureSeq atomic.Int64

func seedPresentationInstance(t *testing.T, svc *pubservice.Service) string {
	t.Helper()
	seq := presentationFixtureSeq.Add(1)
	db := svc.Model().RouteDB(context.Background())
	templateID := fmt.Sprintf("eeeeeeee-0000-0000-0000-%012d", seq)
	instanceID := fmt.Sprintf("ffffffff-0000-0000-0000-%012d", seq)
	{
		if err := db.Exec(`INSERT INTO content_templates
			(id, project_id, name, entity_type, draft_document, create_time, update_time)
			VALUES (?, ?, 'publication 测试模板', 'article', '{}'::jsonb, NOW(), NOW())`,
			templateID, projectID).Error; err != nil {
			t.Fatalf("准备内容模板失败：%v", err)
		}
		if err := db.Exec(`INSERT INTO presentation_instances
			(id, project_id, entity_type, entity_id, url_path, template_id, create_time, update_time)
			VALUES (?, ?, 'article', '11111111-1111-1111-1111-1111111111ff', '/presentation-fixture', ?, NOW(), NOW())`,
			instanceID, projectID, templateID).Error; err != nil {
			t.Fatalf("准备展示实例失败：%v", err)
		}
	}
	return instanceID
}

func seedRoute(t *testing.T, svc *pubservice.Service, path, kind string, pageID *string, artifactID *string) {
	t.Helper()
	row := &pubmodel.RouteEntity{
		ProjectID: projectID, Path: path, RouteKind: kind,
	}
	if pageID != nil {
		p := *pageID
		row.PageID = &p
	} else {

		id := seedPresentationInstance(t, svc)
		row.PresentationID = &id
	}
	if artifactID != nil {
		a := *artifactID
		row.ArtifactID = &a
	}
	if err := svc.Model().RouteDB(context.Background()).Create(row).Error; err != nil {
		t.Fatalf("seed 路由 %s/%s 失败: %v", path, kind, err)
	}
}

func countRoutes(t *testing.T, svc *pubservice.Service, cond string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := svc.Model().RouteDB(context.Background()).Where(cond, args...).Count(&n).Error; err != nil {
		t.Fatalf("统计 page_routes 失败: %v", err)
	}
	return n
}

func countReceipts(t *testing.T, svc *pubservice.Service, cond string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := svc.Model().ReceiptDB(context.Background()).Where(cond, args...).Count(&n).Error; err != nil {
		t.Fatalf("统计 publication_receipts 失败: %v", err)
	}
	return n
}

func mustRoute(t *testing.T, svc *pubservice.Service, path string) *pubmodel.RouteEntity {
	t.Helper()
	route, err := svc.Model().GetRoute(context.Background(), projectID, path)
	if err != nil {
		t.Fatalf("查询路由 %s 失败: %v", path, err)
	}
	return route
}

func routeMissing(t *testing.T, svc *pubservice.Service, path string) bool {
	t.Helper()
	_, err := svc.Model().GetRoute(context.Background(), projectID, path)
	return err == gorm.ErrRecordNotFound
}

func containsErr(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望错误包含 %q，实际为 nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("期望错误包含 %q，实际为: %v", want, err)
	}
}

func strPtr(s string) *string { return &s }
