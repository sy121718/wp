// Package unit contenttemplate 模块 feature 测试（真实 PostgreSQL + 生产 DDL，0-A2）：
// 模板创建 → 初始版本、更新 → 版本递增、ResolveTemplate 取最新版本、非法类型拒绝。
//
// 测试基建（本轮修复）：建表走 migrations.Run（真实迁移 SQL），不再用 AutoMigrate
// ——AutoMigrate 会把 model 里多出来的列自动补进测试 schema，掩盖 DDL 漂移；
// 另加「model 列集合 ⊆ 生产 DDL 列集合」断言（support.AssertModelColumnsSubset）。
package unit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"

	"go_wp/internal/builder/core"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// pageDocument 最小合法页面文档（空 root，version=1）。
const pageDocument = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

// headingDocument 含标题组件的合法页面文档（用于验证 ResolveTemplate 取最新内容）。
const headingDocument = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"text":"你好"}}]}`

// newService 隔离 PG schema + 按生产迁移建表 + 真实工程行 + 装配 service。
// 返回 (service, db, projectID)。
func newService(t *testing.T) (*contenttemplateservice.Service, *gorm.DB, string) {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil, nil, ""
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移建表失败: %v", err)
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "模板测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	return contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, testRegistry(t)), db, project.ID
}

// stubEntitySource 测试桩：只提供类型标识，不提供字段解析器。
type stubEntitySource struct{ entityType string }

func (s stubEntitySource) EntityType() string       { return s.entityType }
func (s stubEntitySource) FieldWhitelist() []string { return nil }
func (s stubEntitySource) ResolverFor(_ context.Context, _ string) (core.ContentResolver, error) {
	return nil, errors.New("测试桩不提供字段解析器")
}

// testRegistry 带内容实体类型的注册表（迁移 080 后内容只有 article）。
//
// 模板模块只关心「类型合法性来自注册表」，因此不引内容模块实现，保持本包测试隔离。
func testRegistry(t *testing.T) core.EntitySourceRegistry {
	t.Helper()
	reg := core.NewEntitySourceRegistry()
	for _, k := range []string{"article"} {
		if err := reg.Register(stubEntitySource{entityType: k}); err != nil {
			t.Fatalf("注册实体类型 %q 失败: %v", k, err)
		}
	}
	return reg
}

// TestContentTemplateModelColumnsSubsetOfProductionDDL model 列集合必须是生产
// DDL 列集合的子集（content_templates / content_template_versions）。
func TestContentTemplateModelColumnsSubsetOfProductionDDL(t *testing.T) {
	_, db, _ := newService(t)
	if db == nil {
		return
	}
	support.AssertModelColumnsSubset(t, db, "content_templates", &contenttemplatemodel.TemplateEntity{})
	support.AssertModelColumnsSubset(t, db, "content_template_versions", &contenttemplatemodel.VersionEntity{})
}

// TestContentTemplateCreatePersistsRealColumns 真实 DDL 下创建必须落库
// project_id / current_version_id 与版本行的 source_hash / created_by。
func TestContentTemplateCreatePersistsRealColumns(t *testing.T) {
	svc, db, projectID := newService(t)
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

	var tpl struct {
		ProjectID        string
		CurrentVersionID *string
	}
	if err := db.Raw("SELECT project_id, current_version_id FROM content_templates WHERE id = ?", res.ID).
		Scan(&tpl).Error; err != nil {
		t.Fatalf("读取模板行失败: %v", err)
	}
	if tpl.ProjectID != projectID {
		t.Errorf("project_id 未落库: %q（期望 %q）", tpl.ProjectID, projectID)
	}
	if tpl.CurrentVersionID == nil || *tpl.CurrentVersionID == "" {
		t.Errorf("current_version_id 未回填")
	}

	var ver struct {
		SourceHash string
		CreatedBy  string
	}
	if err := db.Raw("SELECT source_hash, created_by FROM content_template_versions WHERE template_id = ?", res.ID).
		Scan(&ver).Error; err != nil {
		t.Fatalf("读取版本行失败: %v", err)
	}
	if ver.SourceHash == "" {
		t.Errorf("版本行 source_hash 未落库（NOT NULL 列）")
	}
	if ver.CreatedBy == "" {
		t.Errorf("版本行 created_by 未落库（NOT NULL 列）")
	}
	if tpl.CurrentVersionID != nil && *tpl.CurrentVersionID == "" {
		t.Errorf("current_version_id 为空")
	}
}

// TestContentTemplateCreate 创建 → draft_version=1 且 LatestVersion 存在。
func TestContentTemplateCreate(t *testing.T) {
	svc, db, _ := newService(t)
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
	svc, db, _ := newService(t)
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
	svc, _, _ := newService(t)
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
	if _, err := svc.Update(ctx, &contenttemplatedto.UpdateReq{
		ID:            res.ID,
		DraftDocument: json.RawMessage(headingDocument),
	}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}

	got, err := svc.ResolveTemplate(ctx, "article")
	if err != nil {
		t.Fatalf("ResolveTemplate 失败: %v", err)
	}
	if got.Version != 2 {
		t.Fatalf("应解析到最新版本 2: %d", got.Version)
	}
	if got.VersionID == "" {
		t.Fatalf("VersionID 不应为空")
	}
	if got.TemplateID != res.ID {
		t.Fatalf("TemplateID 应为模板 ID %s，实际 %q", res.ID, got.TemplateID)
	}
	if got.EntityType != "article" {
		t.Fatalf("EntityType 应为 article: %q", got.EntityType)
	}
	if !strings.Contains(string(got.Document), "core.heading") {
		t.Fatalf("最新版本 document 应包含标题组件: %s", got.Document)
	}
}

// TestContentTemplateRejectInvalidType 非法 EntityType 拒绝。
func TestContentTemplateRejectInvalidType(t *testing.T) {
	svc, _, _ := newService(t)
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
	svc, _, _ := newService(t)
	if svc == nil {
		return
	}
	ctx := context.Background()

	if _, err := svc.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType:    "article",
		Name:          "坏文档模板",
		DraftDocument: json.RawMessage(`{"settings":`),
	}); err == nil || !strings.Contains(err.Error(), contenttemplateenums.ErrDataInvalid) {
		t.Fatalf("非法文档应返回 ErrDataInvalid: %v", err)
	}
}
