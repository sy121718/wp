// Package unit presentation 模块 feature 测试（真实 PostgreSQL + 生产 DDL，0-A2）：
// 内容实体 + ContentTemplate（真实 service）→ CreateInstance → 编译（binding 经
// ResolverFor 解析）→ 发布 → 产物含实体字段字面量。
//
// 测试基建（本轮修复）：建表走 migrations.Run（真实迁移 SQL），不再用 AutoMigrate。
// 修复前 AutoMigrate 会把 presentation_instances 缺的列（status / artifact_hash）
// 自动补进测试 schema，使「model 与生产 DDL 不对齐」在测试里永远不可见。
package unit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"
	presentationservice "go_wp/internal/module/presentation/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"

	"go_wp/internal/builder/core"
	"go_wp/internal/pipeline"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

// presFixture 一套真实装配（生产 DDL 建表 + 真实工程/模板/内容/presentation service）。
type presFixture struct {
	db        *gorm.DB
	content   contentcontract.ContentService
	templates contenttemplatecontract.ContentTemplateService
	pres      *presentationservice.Service
	projectID string
}

// newPresFixture 装配 fixture；PG 不可用时 t.Skip（返回 nil）。
func newPresFixture(t *testing.T) *presFixture {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用：%v", err)
		return nil
	}
	if err := migrations.Run(db); err != nil {
		t.Fatalf("执行生产迁移建表失败: %v", err)
	}
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "自动发布测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	contentSvc := contentservice.NewService(contentmodel.NewModel(db))
	// 实体类型注册表：与真实装配同款 —— 内容模块注册自己的类型，
	// 模板与发布实例只依赖注册表（不再直接依赖内容模块）。
	registry := core.NewEntitySourceRegistry()
	if err := contentSvc.RegisterEntityTypes(registry); err != nil {
		t.Fatalf("注册实体类型失败: %v", err)
	}
	tplSvc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, registry)
	presSvc := presentationservice.NewService(presentationmodel.NewModel(db), tplSvc, registry, projects, nil)
	return &presFixture{
		db: db, content: contentSvc, templates: tplSvc, pres: presSvc, projectID: project.ID,
	}
}

// createTemplate 用真实 contenttemplate service 建模板（article 类型，binding 到 title）。
func (f *presFixture) createTemplate(t *testing.T) {
	t.Helper()
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h2"}}]}`
	if _, err := f.templates.Create(context.Background(), &contenttemplatedto.CreateReq{
		EntityType: "article", Name: "文章模板", DraftDocument: []byte(doc),
	}); err != nil {
		t.Fatalf("创建模板失败: %v", err)
	}
}

// activeHTML 读取访问面上某路径的已激活 index.html。
func activeHTML(t *testing.T, urlPath string) string {
	t.Helper()
	rel := strings.TrimPrefix(urlPath, "/")
	b, err := os.ReadFile(filepath.Join(pipeline.ActiveRoot(), rel, "index.html"))
	if err != nil {
		t.Fatalf("读取激活产物 %s 失败: %v", urlPath, err)
	}
	return string(b)
}

// TestCreateInstanceEndToEnd 内容→模板→编译→发布 全链路（真实 DDL）。
func TestCreateInstanceEndToEnd(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.createTemplate(t)

	// 建内容实体（article，title 字段）。
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "summer-shirt",
		Data: map[string]any{"title": "夏季衬衫", "excerpt": "夏季摘要"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}

	// 创建自动发布实例（显式传工程；urlPath 由实体 slug 推导）。
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID,
		URLPath: "/products/summer-shirt",
	})
	if err != nil {
		t.Fatalf("CreateInstance 失败: %v", err)
	}
	if inst.ArtifactHash == "" || inst.SnapshotID == "" || inst.ArtifactID == "" {
		t.Fatalf("实例缺少产物/快照: %+v", inst)
	}
	if inst.Status != presentationenums.StatusActive {
		t.Fatalf("实例状态应为 active: %s", inst.Status)
	}
	if inst.Stale {
		t.Fatalf("发布成功后 stale 应为 false")
	}
	if inst.TemplateID == "" {
		t.Fatalf("template_id 未落库（NOT NULL 外键）")
	}

	// 产物已发布到访问面，且 binding 已解析为实体字段字面量。
	html := activeHTML(t, "/products/summer-shirt")
	if !strings.Contains(html, "夏季衬衫") {
		t.Fatalf("激活产物应含实体字段字面量，实际: %s", html)
	}
}

// TestCreateInstanceIdempotent 同实体重复创建幂等（返回已有实例）。
func TestCreateInstanceIdempotent(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h2"}}]}`
	if _, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: "article", Name: "文章模板", DraftDocument: []byte(doc),
	}); err != nil {
		t.Fatalf("创建模板失败: %v", err)
	}
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "summer",
		Data: map[string]any{"title": "夏季"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	req := &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID,
		URLPath: "/articles/summer",
	}
	a, err := f.pres.CreateInstance(ctx, req)
	if err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	b, err := f.pres.CreateInstance(ctx, req)
	if err != nil {
		t.Fatalf("重复创建失败: %v", err)
	}
	if a.ID != b.ID {
		t.Fatalf("重复创建应幂等返回同一实例: %s vs %s", a.ID, b.ID)
	}
}
