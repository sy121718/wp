package unit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	buildmodel "go_wp/internal/module/build/model"
	buildservice "go_wp/internal/module/build/service"
	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	navigationmodel "go_wp/internal/module/navigation/model"
	navigationservice "go_wp/internal/module/navigation/service"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
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

	"go_wp/pkg/builder/core"
	"go_wp/internal/pipeline"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

type presFixture struct {
	db        *gorm.DB
	content   contentcontract.ContentService
	templates contenttemplatecontract.ContentTemplateService
	pres      *presentationservice.Service

	routes    pubcontract.PublicationService
	projectID string
}

func newPresFixture(t *testing.T) *presFixture {
	t.Helper()
	return newPresFixtureWith(t, nil, nil)
}

func newPresFixtureWithRoutes(t *testing.T,
	wrap func(pubcontract.PublicationService) pubcontract.PublicationService) *presFixture {
	t.Helper()
	return newPresFixtureWith(t, wrap, nil)
}

func newPresFixtureWithRegistry(t *testing.T,
	wrap func(core.EntitySourceRegistry) core.EntitySourceRegistry) *presFixture {
	t.Helper()
	return newPresFixtureWith(t, nil, wrap)
}

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

func (f *presFixture) buildService() *buildservice.Service {
	return buildservice.NewService(buildmodel.NewModel(f.db))
}

func (f *presFixture) navWriteDeps() navigationservice.NavigationWriteDeps {
	pages := pageservice.NewService(pagemodel.NewPageModel(f.db), nil, nil,
		projectservice.NewService(projectmodel.NewProjectModel(f.db)), nil, nil, nil, nil, nil)
	return navigationservice.NavigationWriteDeps{
		Pages: pages, Instances: f.pres, Items: pages,
		ItemInstances: f.pres, Queue: f.buildService(),
	}
}

func (f *presFixture) newNavService() *navigationservice.Service {
	navSvc := navigationservice.NewService(navigationmodel.NewModel(f.db),
		projectservice.NewService(projectmodel.NewProjectModel(f.db)))
	navSvc.SetWritePorts(f.navWriteDeps())
	return navSvc
}

func (f *presFixture) createTemplate(t *testing.T) {
	t.Helper()
	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"binding":{"field":"article.title"},"tag":"h2"}}]}`
	if _, err := f.templates.Create(context.Background(), &contenttemplatedto.CreateReq{
		EntityType: "article", Name: "文章模板", DraftDocument: []byte(doc),
	}); err != nil {
		t.Fatalf("创建模板失败: %v", err)
	}
}

func activeHTML(t *testing.T, urlPath string) string {
	t.Helper()
	rel := strings.TrimPrefix(urlPath, "/")
	b, err := os.ReadFile(filepath.Join(pipeline.ActiveRoot(), rel, "index.html"))
	if err != nil {
		t.Fatalf("读取激活产物 %s 失败: %v", urlPath, err)
	}
	return string(b)
}

func TestCreateInstanceEndToEnd(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.createTemplate(t)

	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "summer-shirt",
		Data: map[string]any{"title": "夏季衬衫", "excerpt": "夏季摘要"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}

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

	html := activeHTML(t, "/products/summer-shirt")
	if !strings.Contains(html, "夏季衬衫") {
		t.Fatalf("激活产物应含实体字段字面量，实际: %s", html)
	}
}

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

	if !strings.Contains(preview.HTML, `name="site-preview-mode" content="readonly"`) || strings.Contains(published, `name="site-preview-mode"`) {
		t.Fatal("预览必须声明只读，正式产物不能携带模拟上下文")
	}
	if stripURLTags(support.ComparableSiteHTML(t, published)) != stripURLTags(support.ComparableSiteHTML(t, preview.HTML)) {
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
