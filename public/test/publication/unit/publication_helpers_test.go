// Package unit 覆盖 publication service 层状态机单元测试。
// 依赖注入仅需 *pubmodel.Model（Service 不依赖 ArtifactStore / 文件系统），
// 全部场景均可在隔离 PG schema 内纯 DB 验证。
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

// uuid 用例值，与 feature 测试保持一致的合法 uuid 风格。
const (
	projectID    = "cccccccc-0000-0000-0000-000000000001"
	pageID       = "dddddddd-0000-0000-0000-000000000001"
	otherPageID  = "dddddddd-0000-0000-0000-000000000002"
	artifactUUID = "11111111-1111-1111-1111-111111111101"
)

// newUnitService 建隔离测试库（表结构来自生产迁移）并装配 Service。
//
// 不再 AutoMigrate：它照 model 的 gorm 标签建列，与生产 DDL 分叉 —— 实测 receipt_data
// 在 AutoMigrate 下不是 jsonb，宽松到能收下拼接出来的非法 JSON，于是「畸形 ArtifactID
// 必须让事务整体回滚」这条断言在测试里失败、在生产上却成立（生产是 jsonb）。
func newUnitService(t *testing.T) *pubservice.Service {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	support.SeedProjectRow(t, db, projectID, "publication 测试站点")
	return pubservice.NewService(pubmodel.NewPublicationModel(db))
}

// presentationFixtureSeq 为夹具行生成唯一后缀：每个测试各自一个空库，
// 但不能用包级 once 缓存 ID —— 缓存下来的行只存在于第一个测试的库里。
var presentationFixtureSeq atomic.Int64

// seedPresentationInstance 准备一条展示实例占位行并返回其 ID。
//
// 生产 schema 下这不是可有可无的装饰：page_routes_check 要求 page_id 与 presentation_id
// 恰好有一个非空，而 presentation_id → presentation_instances(id) → content_templates(id)
// → projects(id) 是一条真实的引用链。过去这里 AutoMigrate 自建表，既没有外键也没有该
// check，于是「展示实例占用」这类场景只验证了 service 的意图、没验证它落库的形状。
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

// seedRoute 直接插入一行路由占用（reserved/active/redirect 均可）。
func seedRoute(t *testing.T, svc *pubservice.Service, path, kind string, pageID *string, artifactID *string) {
	t.Helper()
	row := &pubmodel.RouteEntity{
		ProjectID: projectID, Path: path, RouteKind: kind,
	}
	if pageID != nil {
		p := *pageID
		row.PageID = &p
	} else {
		// page_id 为空即展示实例占用（page_routes_check 要求二者恰有一个非空）。
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
