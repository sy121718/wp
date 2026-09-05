// Package unit 覆盖 publication service 层状态机单元测试。
// 依赖注入仅需 *pubmodel.Model（Service 不依赖 ArtifactStore / 文件系统），
// 全部场景均可在隔离 PG schema 内纯 DB 验证。
package unit

import (
	"context"
	"strings"
	"testing"

	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/public/test/support"

	"gorm.io/gorm"
)

// uuid 用例值，与 feature 测试保持一致的合法 uuid 风格。
const (
	projectID    = "cccccccc-0000-0000-0000-000000000001"
	pageID       = "dddddddd-0000-0000-0000-000000000001"
	otherPageID  = "dddddddd-0000-0000-0000-000000000002"
	artifactUUID = "11111111-1111-1111-1111-111111111101"
)

// newUnitService 打开隔离 PG schema、AutoMigrate 两张 publication 表并装配 Service。
func newUnitService(t *testing.T) *pubservice.Service {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
		return nil
	}
	if err := db.AutoMigrate(&pubmodel.RouteEntity{}, &pubmodel.ReceiptEntity{}); err != nil {
		t.Fatalf("AutoMigrate publication 表失败: %v", err)
	}
	return pubservice.NewService(pubmodel.NewPublicationModel(db))
}

// seedRoute 直接插入一行路由占用（reserved/active/redirect 均可）。
func seedRoute(t *testing.T, svc *pubservice.Service, path, kind string, pageID *string, artifactID *string) {
	t.Helper()
	row := &pubmodel.RouteEntity{
		ProjectID: projectID, Path: path, RouteKind: kind,
	}
	if pageID != nil {
		p := *pageID
		row.PageID = &p
	}
	if artifactID != nil {
		a := *artifactID
		row.ArtifactID = &a
	}
	if err := svc.Model().RouteDB(context.Background()).Create(row).Error; err != nil {
		t.Fatalf("seed 路由 %s/%s 失败: %v", path, kind, err)
	}
}

// countRoutes 统计 page_routes 中满足条件的行数。
func countRoutes(t *testing.T, svc *pubservice.Service, cond string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := svc.Model().RouteDB(context.Background()).Where(cond, args...).Count(&n).Error; err != nil {
		t.Fatalf("统计 page_routes 失败: %v", err)
	}
	return n
}

// countReceipts 统计 publication_receipts 中满足条件的行数。
func countReceipts(t *testing.T, svc *pubservice.Service, cond string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := svc.Model().ReceiptDB(context.Background()).Where(cond, args...).Count(&n).Error; err != nil {
		t.Fatalf("统计 publication_receipts 失败: %v", err)
	}
	return n
}

// mustRoute 查询路由，不存在则 Fatal。
func mustRoute(t *testing.T, svc *pubservice.Service, path string) *pubmodel.RouteEntity {
	t.Helper()
	route, err := svc.Model().GetRoute(context.Background(), projectID, path)
	if err != nil {
		t.Fatalf("查询路由 %s 失败: %v", path, err)
	}
	return route
}

// routeMissing 断言路径不存在（GetRoute 返回 RecordNotFound）。
func routeMissing(t *testing.T, svc *pubservice.Service, path string) bool {
	t.Helper()
	_, err := svc.Model().GetRoute(context.Background(), projectID, path)
	return err == gorm.ErrRecordNotFound
}

// containsErr 断言 err 非 nil 且包含期望子串。
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
