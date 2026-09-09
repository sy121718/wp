// Package unit contenttemplate 模块 feature 测试（真实 PostgreSQL，0-A2）：
// 模板创建 → 初始版本、更新 → 版本递增、ResolveTemplate 取最新版本、非法类型拒绝。
package unit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"gorm.io/gorm"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"

	"go_wp/public/test/support"
)

// pageDocument 最小合法页面文档（空 root，version=1）。
const pageDocument = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

// headingDocument 含标题组件的合法页面文档（用于验证 ResolveTemplate 取最新内容）。
const headingDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"你好"}}]}`

// newService 隔离 PG schema + AutoMigrate 两张表 + 装配 service。
func newService(t *testing.T) (*contenttemplateservice.Service, *gorm.DB) {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil, nil
	}
	if err := db.AutoMigrate(&contenttemplatemodel.TemplateEntity{}, &contenttemplatemodel.VersionEntity{}); err != nil {
		t.Fatalf("AutoMigrate 失败: %v", err)
	}
	return contenttemplateservice.NewService(contenttemplatemodel.NewModel(db)), db
}

// TestContentTemplateCreate 创建 → draft_version=1 且 LatestVersion 存在。
func TestContentTemplateCreate(t *testing.T) {
	svc, db := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	res, err := svc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType:    "product",
		Name:          "商品模板",
		DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if res.DraftVersion != 1 {
		t.Fatalf("初始 draft_version 应为 1: %d", res.DraftVersion)
	}

	// LatestVersion 应立即存在（version=1 快照）。
	ver, err := contenttemplatemodel.NewModel(db).LatestVersion(ctx, res.ID)
	if err != nil {
		t.Fatalf("LatestVersion 应存在: %v", err)
	}
	if ver.Version != 1 {
		t.Fatalf("初始版本号应为 1: %d", ver.Version)
	}
}

// TestContentTemplateUpdate 更新 → draft_version 递增 + 版本数=2。
func TestContentTemplateUpdate(t *testing.T) {
	svc, db := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	res, err := svc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType:    "article",
		Name:          "文章模板",
		DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	res2, err := svc.Update(ctx, &contenttemplatedto.UpdateReq{
		ID:            res.ID,
		DraftDocument: json.RawMessage(headingDocument),
	})
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if res2.DraftVersion != 2 {
		t.Fatalf("draft_version 应递增为 2: %d", res2.DraftVersion)
	}

	// 版本表应产生 2 条记录。
	var versions []contenttemplatemodel.VersionEntity
	if err := db.Where("template_id = ?", res.ID).Find(&versions).Error; err != nil {
		t.Fatalf("查询版本失败: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("版本数应为 2: %d", len(versions))
	}
}

// TestContentTemplateResolve 解析最新模板的最新版本 document。
func TestContentTemplateResolve(t *testing.T) {
	svc, _ := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	res, err := svc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType:    "product",
		Name:          "商品模板",
		DraftDocument: json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if _, err := svc.Update(ctx, &contenttemplatedto.UpdateReq{
		ID:            res.ID,
		DraftDocument: json.RawMessage(headingDocument),
	}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}

	got, err := svc.ResolveTemplate(ctx, "product")
	if err != nil {
		t.Fatalf("ResolveTemplate 失败: %v", err)
	}
	if got.Version != 2 {
		t.Fatalf("应解析到最新版本 2: %d", got.Version)
	}
	if got.VersionID == "" {
		t.Fatalf("VersionID 不应为空")
	}
	if got.EntityType != "product" {
		t.Fatalf("EntityType 应为 product: %q", got.EntityType)
	}
	if !strings.Contains(string(got.Document), "core.heading") {
		t.Fatalf("最新版本 document 应包含标题组件: %s", got.Document)
	}
}

// TestContentTemplateRejectInvalidType 非法 EntityType 拒绝。
func TestContentTemplateRejectInvalidType(t *testing.T) {
	svc, _ := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	// Create 非法类型拒绝。
	if _, err := svc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType:    "tag",
		Name:          "非法模板",
		DraftDocument: json.RawMessage(pageDocument),
	}); err == nil {
		t.Fatalf("非法类型应拒绝")
	}

	// ResolveTemplate 非法类型拒绝。
	if _, err := svc.ResolveTemplate(ctx, "tag"); err == nil {
		t.Fatalf("ResolveTemplate 非法类型应拒绝")
	}
}

// TestContentTemplateRejectInvalidDocument 非法文档拒绝（ErrDataInvalid）。
func TestContentTemplateRejectInvalidDocument(t *testing.T) {
	svc, _ := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	if _, err := svc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType:    "product",
		Name:          "坏文档模板",
		DraftDocument: json.RawMessage(`{"settings":`),
	}); err == nil || !strings.Contains(err.Error(), contenttemplateenums.ErrDataInvalid) {
		t.Fatalf("非法文档应返回 ErrDataInvalid: %v", err)
	}
}
