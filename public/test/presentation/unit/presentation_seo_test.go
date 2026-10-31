package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	contentdto "go_wp/internal/module/content/dto"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationmodel "go_wp/internal/module/presentation/model"
	presentationservice "go_wp/internal/module/presentation/service"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"go_wp/internal/builder/core"
	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

func seoDoc(seoFields string) string {
	settings := "{\"layout\":{\"mode\":\"full\"}"
	if seoFields != "" {
		settings += ",\"seo\":" + seoFields
	}
	settings += "}"
	return "{\"settings\":" + settings + ",\"root\":[{\"id\":\"body\",\"type\":\"core.heading\",\"props\":{\"text\":\"正文标记\",\"tag\":\"h2\"}}]}"
}

func createSEOArticle(t *testing.T, f *presFixture, slug string, data map[string]any) string {
	t.Helper()
	e, err := f.content.Create(context.Background(), &contentdto.CreateReq{
		EntityType: "article", Slug: slug, Data: data,
	})
	if err != nil {
		t.Fatalf("创建文章实体失败: %v", err)
	}
	return e.ID
}

func createSEOTemplate(t *testing.T, templates contenttemplatecontract.ContentTemplateService,
	projectID, entityType, doc string) string {
	t.Helper()
	res, err := templates.Create(context.Background(), &contenttemplatedto.CreateReq{
		EntityType: entityType, Name: "SEO 测试模板", ProjectID: projectID,
		DraftDocument: []byte(doc),
	})
	if err != nil {
		t.Fatalf("创建模板失败: %v", err)
	}
	return res.ID
}

func containsAll(t *testing.T, html string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(html, w) {
			t.Fatalf("产物缺少 %q\n产物: %s", w, html)
		}
	}
}

func canonicalOf(html string) string {
	const prefix = "<link rel=\"canonical\" href=\""
	i := strings.Index(html, prefix)
	if i < 0 {
		return ""
	}
	rest := html[i+len(prefix):]
	j := strings.Index(rest, "\">")
	if j < 0 {
		return ""
	}
	return rest[:j]
}

var urlSEOFragments = []string{
	"<link rel=\"canonical\"",
	"<meta property=\"og:url\"",
	"<script type=\"application/ld+json\">",
}

func stripURLTags(html string) string {
	lines := strings.Split(html, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		skip := false
		for _, frag := range urlSEOFragments {
			if strings.HasPrefix(trimmed, frag) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func TestPresentationSEOArticleUsesEntityFields(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	tplID := createSEOTemplate(t, f.templates, f.projectID, "article", seoDoc(""))
	entityID := createSEOArticle(t, f, "seo-entity-fields", map[string]any{
		"title": "夏季衬衫", "excerpt": "轻薄透气",

		"seoTitle": "夏季衬衫｜官方商城", "seoDescription": "轻薄透气，四季可穿",
	})
	const urlPath = "/articles/summer-shirt"
	if _, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entityID,
		URLPath: urlPath, TemplateID: tplID,
	}); err != nil {
		t.Fatalf("创建文章发布实例失败: %v", err)
	}

	published := activeHTML(t, urlPath)
	containsAll(t, published,
		"<link rel=\"canonical\" href=\""+urlPath+"\">",
		"<meta property=\"og:type\" content=\"article\">",
		"<meta property=\"og:title\" content=\"夏季衬衫\">",
		"<meta name=\"twitter:title\" content=\"夏季衬衫\">",
		"<meta property=\"og:description\" content=\"轻薄透气\">",
		"<script type=\"application/ld+json\">",
		"\"@type\":\"Article\"",
		"\"name\":\"夏季衬衫\"",
		"\"url\":\""+urlPath+"\"",
	)

	for _, stale := range []string{"夏季衬衫｜官方商城", "轻薄透气，四季可穿"} {
		if strings.Contains(published, stale) {
			t.Fatalf("遗留 SEO 字段不该进产物（已与 title / excerpt 合并）：%q\n产物: %s", stale, published)
		}
	}

	preview, err := f.pres.PreviewInstance(ctx, &presentationdto.PreviewInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entityID, TemplateID: tplID,
	})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if got := canonicalOf(preview.HTML); got != "" {
		t.Fatalf("预览不应输出 canonical，实际 %q", got)
	}
	containsAll(t, preview.HTML,
		"<meta property=\"og:type\" content=\"article\">",
		"<script type=\"application/ld+json\">",
	)

	again, err := f.pres.PreviewInstance(ctx, &presentationdto.PreviewInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entityID, TemplateID: tplID,
	})
	if err != nil {
		t.Fatalf("二次预览失败: %v", err)
	}
	if again.HTML != preview.HTML {
		t.Fatal("同一输入的两次预览字节不一致（构建不确定）")
	}
}

func TestPresentationSEOArticleFallsBackToMainFields(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	tplID := createSEOTemplate(t, f.templates, f.projectID, "article", seoDoc(""))
	entityID := createSEOArticle(t, f, "seo-fallback", map[string]any{
		"title": "夏季衬衫", "excerpt": "夏季摘要",
	})
	const urlPath = "/articles/fallback"
	if _, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entityID,
		URLPath: urlPath, TemplateID: tplID,
	}); err != nil {
		t.Fatalf("创建文章发布实例失败: %v", err)
	}
	containsAll(t, activeHTML(t, urlPath),
		"<meta property=\"og:title\" content=\"夏季衬衫\">",
		"<meta property=\"og:description\" content=\"夏季摘要\">",
	)
}

func TestPresentationSEOArticleKeepsTemplateValuesWhenEntityEmpty(t *testing.T) {
	f := newPresFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	tplID := createSEOTemplate(t, f.templates, f.projectID, "article",
		seoDoc("{\"title\":\"模板标题\",\"description\":\"模板描述\",\"canonical\":\"/static-from-template\"}"))

	entityID := createSEOArticle(t, f, "seo-keep-template", map[string]any{"focusKeyword": "衬衫"})
	const urlPath = "/articles/instance-path"
	if _, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entityID,
		URLPath: urlPath, TemplateID: tplID,
	}); err != nil {
		t.Fatalf("创建文章发布实例失败: %v", err)
	}
	published := activeHTML(t, urlPath)
	containsAll(t, published,
		"<meta property=\"og:title\" content=\"模板标题\">",
		"<meta property=\"og:description\" content=\"模板描述\">",
	)
	if got := canonicalOf(published); got != urlPath {
		t.Fatalf("canonical 应为实例线上路径 %q，实际 %q", urlPath, got)
	}
	if strings.Contains(published, "/static-from-template") {
		t.Fatalf("模板里手填的 canonical 不应出现在自动发布产物中\n产物: %s", published)
	}

	const otherPath = "/articles/other-path"
	entity2 := createSEOArticle(t, f, "seo-second", map[string]any{"title": "第二篇"})
	if _, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity2,
		URLPath: otherPath, TemplateID: tplID,
	}); err != nil {
		t.Fatalf("创建第二个发布实例失败: %v", err)
	}
	if got := canonicalOf(activeHTML(t, otherPath)); got != otherPath {
		t.Fatalf("canonical 应随实例路径变化，实际 %q", got)
	}
}

type seoProductFixture struct {
	db        *gorm.DB
	products  *productservice.Service
	templates contenttemplatecontract.ContentTemplateService
	pres      *presentationservice.Service
	projectID string
}

func newSEOProductFixture(t *testing.T) *seoProductFixture {
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
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "商品 SEO 测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	registry := core.NewEntitySourceRegistry()
	products := productservice.NewService(productmodel.NewModel(db), projects)
	products.SetContentStore(i18n.NewDBContentStore(db))
	if err := products.RegisterEntityTypes(registry); err != nil {
		t.Fatalf("注册商品实体类型失败: %v", err)
	}
	tplSvc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, registry)
	presSvc := presentationservice.NewService(presentationmodel.NewModel(db), tplSvc, registry, projects, nil,
		pubservice.NewService(pubmodel.NewPublicationModel(db)))
	return &seoProductFixture{
		db: db, products: products, templates: tplSvc, pres: presSvc, projectID: project.ID,
	}
}

func (f *seoProductFixture) createSEOProduct(t *testing.T, slug string) string {
	t.Helper()
	desc, err := json.Marshal(map[string]string{"html": "<p>纯棉   透气</p><p>四季可穿</p>"})
	if err != nil {
		t.Fatalf("描述编码失败: %v", err)
	}
	price := 99.0
	res, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "夏季衬衫", Slug: slug,
		Description: desc, DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	return res.ID
}

func TestPresentationSEOProductUsesEntityFields(t *testing.T) {
	f := newSEOProductFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	tplID := createSEOTemplate(t, f.templates, f.projectID, "product", seoDoc(""))
	productID := f.createSEOProduct(t, "summer-shirt")
	const urlPath = "/products/summer-shirt"
	if _, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "product", EntityID: productID,
		URLPath: urlPath, TemplateID: tplID,
	}); err != nil {
		t.Fatalf("创建商品发布实例失败: %v", err)
	}
	published := activeHTML(t, urlPath)
	containsAll(t, published,
		"<link rel=\"canonical\" href=\""+urlPath+"\">",
		"<meta property=\"og:type\" content=\"product\">",
		"<meta property=\"og:title\" content=\"夏季衬衫\">",

		"<meta property=\"og:description\" content=\"纯棉 透气 四季可穿\">",
		"<script type=\"application/ld+json\">",
		"\"@type\":\"Product\"",
	)
	if strings.Contains(published, "&lt;p&gt;") {
		t.Fatalf("meta 描述不应带 HTML 标签转义产物\n产物: %s", published)
	}

	preview, err := f.pres.PreviewInstance(ctx, &presentationdto.PreviewInstanceReq{
		ProjectID: f.projectID, EntityType: "product", EntityID: productID, TemplateID: tplID,
	})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if got := canonicalOf(preview.HTML); got != "" {
		t.Fatalf("预览不应输出 canonical，实际 %q", got)
	}
	containsAll(t, preview.HTML, "<meta property=\"og:type\" content=\"product\">")
}

func TestPresentationSEOProductJSONLDContainsOffers(t *testing.T) {
	f := newSEOProductFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	tplID := createSEOTemplate(t, f.templates, f.projectID, "product", seoDoc(""))
	productID := f.createSEOProduct(t, "offer-shirt")
	const urlPath = "/products/offer-shirt"
	if _, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "product", EntityID: productID,
		URLPath: urlPath, TemplateID: tplID,
	}); err != nil {
		t.Fatalf("创建商品发布实例失败: %v", err)
	}
	published := activeHTML(t, urlPath)
	containsAll(t, published,
		"<script type=\"application/ld+json\">",
		"\"@type\":\"Product\"",
		"\"offers\":",
		"\"@type\":\"Offer\"",
		"\"price\":\"99\"",
		"\"priceCurrency\":\"CNY\"",
		"\"availability\":\"https://schema.org/InStock\"",
		"\"url\":\""+urlPath+"\"",
	)
}
