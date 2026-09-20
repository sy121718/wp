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
	pubcontract "go_wp/internal/module/publication/contract"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

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
	// routes URL 占用登记（page_routes）：详情页的占用是否真的落库，只能从
	// 这个契约的外部视角验证（不新增只为测试存在的读取接口）。
	routes    pubcontract.PublicationService
	projectID string
}

// newPresFixture 装配 fixture；PG 不可用时 t.Skip（返回 nil）。
func newPresFixture(t *testing.T) *presFixture {
	t.Helper()
	return newPresFixtureWith(t, nil, nil)
}

// newPresFixtureWithRoutes 同 newPresFixture，但允许在装配时包装路由契约（故障注入用：
// 验证「路由登记失败时发布必须失败且可恢复」，审计 AR2-004）。
func newPresFixtureWithRoutes(t *testing.T,
	wrap func(pubcontract.PublicationService) pubcontract.PublicationService) *presFixture {
	t.Helper()
	return newPresFixtureWith(t, wrap, nil)
}

// newPresFixtureWithRegistry 同 newPresFixture，但允许在装配时包装实体类型注册表
// （PERF-01 用：在**编译阶段**插一个会合闸门，证明发布会话的编译段不再整段持实例锁）。
//
// 只包装注入给 presentation service 的那一个注册表：模板服务继续用原注册表，
// 于是闸门只在发布/预览的编译路径上生效。
func newPresFixtureWithRegistry(t *testing.T,
	wrap func(core.EntitySourceRegistry) core.EntitySourceRegistry) *presFixture {
	t.Helper()
	return newPresFixtureWith(t, nil, wrap)
}

// newPresFixtureWith 装配 fixture 的公共实现；PG 不可用时 t.Skip（返回 nil）。
func newPresFixtureWith(t *testing.T,
	wrap func(pubcontract.PublicationService) pubcontract.PublicationService,
	wrapRegistry func(core.EntitySourceRegistry) core.EntitySourceRegistry) *presFixture {
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
	pubSvc := pubservice.NewService(pubmodel.NewPublicationModel(db))
	var routes pubcontract.PublicationService = pubSvc
	if wrap != nil {
		routes = wrap(routes)
	}
	presRegistry := core.EntitySourceRegistry(registry)
	if wrapRegistry != nil {
		presRegistry = wrapRegistry(presRegistry)
	}
	presSvc := presentationservice.NewService(presentationmodel.NewModel(db), tplSvc, presRegistry, projects, nil, routes)
	return &presFixture{
		db: db, content: contentSvc, templates: tplSvc, pres: presSvc,
		routes: routes, projectID: project.ID,
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

// 模板中同时使用公共控件与组件增强，验证真实预览和激活文件都包含所需资源。
func TestPresentationPreviewAndPublishedAssets(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"heading","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h2"}},{"id":"form","type":"core.form","props":{"fields":[{"type":"select","name":"city","label":"城市","options":["北京","上海"]}]}},{"id":"counter","type":"core.counter","props":{"end":12.5,"decimals":1}}]}`
	if _, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: "article", Name: "交互资源模板", DraftDocument: []byte(doc),
	}); err != nil {
		t.Fatal(err)
	}
	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "interactive-assets", Data: map[string]any{"title": "交互资源"},
	})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := f.pres.PreviewInstance(ctx, &presentationdto.PreviewInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID, URLPath: "/interactive-assets",
	})
	if err != nil {
		t.Fatal(err)
	}
	published := activeHTML(t, "/interactive-assets")
	// 预览不激活 URL，发布产物带实例线上路径 —— canonical / og:url / 由 URL 生成面包屑
	// 的 JSON-LD 是两者唯一的有意差异（见 presentation_seo.go 取舍 2）：
	// 剥掉这些 URL 相关片段后必须逐字节一致，并各自断言 SEO 头的有无。
	if stripURLTags(published) != stripURLTags(preview.HTML) {
		t.Fatal("预览与激活产物（除 URL 相关 SEO 片段外）字节不一致")
	}
	if got := canonicalOf(published); got != "/interactive-assets" {
		t.Fatalf("发布产物应带实例路径的 canonical，实际 %q", got)
	}
	if !strings.Contains(published, "<meta property=\"og:url\" content=\"/interactive-assets\">") {
		t.Fatal("发布产物应带 og:url")
	}
	if got := canonicalOf(preview.HTML); got != "" {
		t.Fatalf("预览不应输出 canonical，实际 %q", got)
	}
	if !strings.Contains(preview.HTML, "<script type=\"application/ld+json\">") {
		t.Fatal("预览仍应输出结构化数据（只是不含 URL）")
	}
	for _, required := range []string{"<select data-ui-select", "WBUI.select", ".wbs-trigger", "function initCounters"} {
		if !strings.Contains(published, required) {
			t.Errorf("激活产物缺少 %s", required)
		}
	}
	for _, unused := range []string{"WBUI.modal", "function initSliders", "function initCountdowns"} {
		if strings.Contains(published, unused) {
			t.Errorf("激活产物不应携带未使用的 %s", unused)
		}
	}
}
