package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	presentationdto "go_wp/internal/module/presentation/dto"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/pkg/builder"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/i18n"
	text "go_wp/pkg/view/jet"
	"go_wp/public/schema"
	"go_wp/public/test/support"
)

const trProductDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pd","type":"core.product","props":{` +
	`"mediaField":"product.defaultImage","mediaAltField":"product.imageAlt",` +
	`"galleryField":"product.images","galleryAltField":"product.imageAlts",` +
	`"titleField":"product.name","subtitleField":"product.subtitle",` +
	`"priceField":"product.priceRange","descriptionField":"product.description",` +
	`"optionsField":"product.options","variantsField":"product.variants"}}]}`

type trFixture struct {
	db        *gorm.DB
	products  *productservice.Service
	projects  *projectservice.Service
	registry  core.EntitySourceRegistry
	projectID string
}

func newTRFixture(t *testing.T) *trFixture {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	// 工作台模板的字段名与状态列都走 t() 取词：迁移只建表，词条由 seed 写进 sys_i18n，
	// 不跑 seed 也不加载词库时，工作台会退成模板兜底（英文请求也只显示中文「名称」）。
	// 同包的属性夹具是同一手法（newAttrFixture）。
	if err := schema.Apply(db); err != nil {
		t.Fatalf("载入 schema 失败：%v", err)
	}
	loadFeatureWords(t, db)
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "商品多语言测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}

	enabled := true
	if _, err := projects.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: project.ID,
		Locales: []projectdto.LocaleItem{
			{Lang: "zh-CN", SortOrder: 1, IsDefault: true, Enabled: &enabled},
			{Lang: "en-US", SortOrder: 2, Enabled: &enabled},
		},
	}); err != nil {
		t.Fatalf("初始化语言清单失败: %v", err)
	}
	registry := core.NewEntitySourceRegistry()
	products := productservice.NewService(productmodel.NewModel(db), projects, masterdataservice.NewService(masterdatamodel.NewModel(db), projects))
	products.SetContentStore(i18n.NewDBContentStore(db))
	if err := products.RegisterEntityTypes(registry); err != nil {
		t.Fatalf("注册商品域实体类型失败: %v", err)
	}
	return &trFixture{db: db, products: products, projects: projects, registry: registry, projectID: project.ID}
}

func (f *trFixture) putTranslation(t *testing.T, source, contextName, lang, target string) {
	t.Helper()
	if err := f.db.Exec(
		"INSERT INTO sys_translation (source_hash, context, lang, source_text, target_text, engine) VALUES (?,?,?,?,?,'manual')",
		i18n.ContentHash(source), contextName, lang, source, target,
	).Error; err != nil {
		t.Fatalf("写入译文失败: %v", err)
	}
}

func (f *trFixture) compileEntityPage(t *testing.T, entityType, entityID, lang, doc string) string {
	t.Helper()
	page, err := builder.ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("解析页面文档失败: %v", err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("加载组件模板 Set 失败: %v", err)
	}
	buildCtx := core.WithBuildLang(core.WithBuildProjectID(context.Background(), f.projectID), lang)
	resolver, err := f.registry.ResolverFor(buildCtx, entityType, entityID)
	if err != nil {
		t.Fatalf("取实体解析器失败（%s/%s）: %v", entityType, entityID, err)
	}
	compiled, err := builder.Compile(page,
		builder.WithComponentSet(set),
		builder.WithContentResolver(resolver),
		builder.WithContext(buildCtx),
		builder.WithLanguage(lang),
		builder.WithProjectID(f.projectID),
	)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	return compiled.HTML
}

func trResolve(t *testing.T, f *trFixture, entityType, entityID, lang, field string) string {
	t.Helper()
	buildCtx := core.WithBuildLang(core.WithBuildProjectID(context.Background(), f.projectID), lang)
	resolver, err := f.registry.ResolverFor(buildCtx, entityType, entityID)
	if err != nil {
		t.Fatalf("取实体解析器失败（%s/%s）: %v", entityType, entityID, err)
	}
	value, err := resolver.ResolveString(field)
	if err != nil {
		t.Fatalf("解析字段 %s 失败: %v", field, err)
	}
	return value
}

type trCandidates struct {
	byContext map[string][]string
}

func trIndex(cands []productcontract.TranslationCandidate) trCandidates {
	idx := trCandidates{byContext: map[string][]string{}}
	for _, c := range cands {
		idx.byContext[c.Context] = append(idx.byContext[c.Context], c.SourceText)
	}
	return idx
}

func (idx trCandidates) has(contextName, source string) bool {
	for _, s := range idx.byContext[contextName] {
		if s == source {
			return true
		}
	}
	return false
}

func (idx trCandidates) hasContext(contextName string) bool {
	_, ok := idx.byContext[contextName]
	return ok
}

func trWorkbench(t *testing.T, f *trFixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	pages := pageservice.NewService(pagemodel.NewPageModel(f.db), nil, nil, f.projects, nil, nil, nil, nil, nil)
	handle := producthttp.NewProductTranslationHandle(f.products, pages, nil)
	handle.SetContentTranslationStore(i18n.NewContentWriter(f.db))
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	// 写成功走共享成功出口（生产 assembly 同款接线）。
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.GET("/admin/products/translations", pageview.UseRenderer(pageview.RenderPage), handle.ProductTranslations)

	engine.POST("/api/product/translations/save", pageview.HTMLFragment, pageview.UseRenderer(pageview.RenderPage), handle.SaveProductTranslations)
	return engine
}

func trWorkbenchGet(t *testing.T, engine *gin.Engine, projectID, productID, lang string) string {
	t.Helper()
	target := "/admin/products/translations?project=" + projectID + "&lang=" + lang
	if productID != "" {
		target += "&product=" + productID
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET 商品翻译工作台 -> %d：%s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func trInsertPage(t *testing.T, db *gorm.DB, id, projectID, path string) {
	t.Helper()
	if err := db.Exec("INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, stale, create_time, update_time) "+
		"VALUES (?, ?, 'home', 'none', ?, '{}'::jsonb, 1, false, now(), now())",
		id, projectID, path).Error; err != nil {
		t.Fatalf("插入页面失败: %v", err)
	}
}

func trPageStale(t *testing.T, db *gorm.DB, id string) bool {
	t.Helper()
	var stale bool
	if err := db.Raw("SELECT stale FROM pages WHERE id = ?", id).Scan(&stale).Error; err != nil {
		t.Fatalf("读取 stale 失败: %v", err)
	}
	return stale
}

func trInstanceStale(t *testing.T, db *gorm.DB, id string) bool {
	t.Helper()
	var stale bool
	if err := db.Raw("SELECT stale FROM presentation_instances WHERE id = ?", id).Scan(&stale).Error; err != nil {
		t.Fatalf("读取实例 stale 失败: %v", err)
	}
	return stale
}

func trTranslationCount(t *testing.T, db *gorm.DB, lang string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM sys_translation WHERE lang = ?", lang).Scan(&n).Error; err != nil {
		t.Fatalf("统计译文失败: %v", err)
	}
	return n
}

func trTranslationOf(t *testing.T, db *gorm.DB, source, contextName, lang string) (target, engineName string) {
	t.Helper()
	row := struct {
		TargetText string
		Engine     string
	}{}
	if err := db.Raw("SELECT target_text, engine FROM sys_translation WHERE source_hash = ? AND context = ? AND lang = ?",
		i18n.ContentHash(source), contextName, lang).Scan(&row).Error; err != nil {
		t.Fatalf("读取译文失败: %v", err)
	}
	return row.TargetText, row.Engine
}

func TestProductTranslationCandidatesScopeAndNonTranslatable(t *testing.T) {
	f := newTRFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	category, err := f.products.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "夏季新品", Slug: "summer-new",
		Description: "清凉一夏的当季商品", Image: "/storage/cat.jpg",
		SEOTitle: "夏季新品 SEO", SEODescription: "夏季新品 SEO 描述",
	})
	if err != nil {
		t.Fatalf("创建分类失败: %v", err)
	}
	orphan, err := f.products.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "未挂载分类", Slug: "orphan-cat",
	})
	if err != nil {
		t.Fatalf("创建未挂载分类失败: %v", err)
	}
	brand, err := f.products.CreateBrand(ctx, &productdto.CreateBrandReq{
		ProjectID: f.projectID, Name: "晴山", Slug: "qingshan",
		Logo: "/storage/logo.png", Description: "山里的颜色", SEOTitle: "晴山 SEO",
	})
	if err != nil {
		t.Fatalf("创建品牌失败: %v", err)
	}
	tag, err := f.products.CreateTag(ctx, &productdto.CreateTagReq{
		ProjectID: f.projectID, Name: "当季", Slug: "season",
	})
	if err != nil {
		t.Fatalf("创建标签失败: %v", err)
	}
	attr, err := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "颜色", Key: "color", IsVariation: boolPtr(true),
		Values: []productdto.AttributeValueReq{
			{Label: "红色", Key: "red"},
			{Label: "蓝色", Key: "blue"},
		},
	})
	if err != nil {
		t.Fatalf("创建属性组失败: %v", err)
	}
	price := 99.0
	created, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "夏季衬衫", Subtitle: "纯棉透气",
		Description: json.RawMessage(`{"html":"<p>纯棉透气</p>"}`),
		Slug:        "summer-shirt",
		Images:      []string{"/storage/a.jpg", "/storage/b.jpg"},
		ImageAlts:   []string{"正面图", "背面图"},
		CategoryIDs: []string{category.ID}, PrimaryCategoryID: category.ID,
		TagIDs: []string{tag.ID}, AttributeIDs: []string{attr.ID}, BrandID: brand.ID,
		DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}

	cands, err := f.products.ProductTranslationCandidates(ctx, f.projectID, created.ID)
	if err != nil {
		t.Fatalf("收集翻译候选失败: %v", err)
	}
	idx := trIndex(cands)

	want := map[string][]string{
		"product.name":                 {"夏季衬衫"},
		"product.subtitle":             {"纯棉透气"},
		"product.description":          {"<p>纯棉透气</p>"},
		"product.imageAlts":            {"正面图", "背面图"},
		"product_category.name":        {"夏季新品"},
		"product_category.description": {"清凉一夏的当季商品"},
		"product_brand.name":           {"晴山"},
		"product_brand.description":    {"山里的颜色"},
		"product_tag.name":             {"当季"},
		"product_attribute.name":       {"颜色"},
		"product_attribute.values":     {"红色", "蓝色"},
	}
	for contextName, sources := range want {
		for _, src := range sources {
			if !idx.has(contextName, src) {
				t.Fatalf("候选缺少 %s / %q；实际候选：%+v", contextName, src, idx.byContext)
			}
		}
	}

	for _, forbidden := range []string{
		"product.slug", "product.sku", "product.unit", "product.images", "product.defaultImage",
		"product.price", "product.comparePrice", "product.priceRange", "product.minPrice", "product.maxPrice",
		"product.options", "product.variants", "product.related",
		"product.seoTitle", "product.seoDescription",
		"product_category.slug", "product_category.image",
		"product_category.seoTitle", "product_category.seoDescription",
		"product_brand.slug", "product_brand.logo",
		"product_brand.seoTitle", "product_brand.seoDescription",
		"product_tag.slug", "product_attribute.key",
	} {
		if idx.hasContext(forbidden) {
			t.Fatalf("非翻译字段 %s 不应进候选（实际 %q）", forbidden, idx.byContext[forbidden])
		}
	}

	if productcontract.IsValidField(productcontract.EntityTypeProduct, "barcode") ||
		productcontract.IsTranslatableField(productcontract.EntityTypeProduct, "barcode") {
		t.Fatal("条码不应在商品字段白名单 / 可翻译字段里")
	}

	for _, c := range cands {
		if c.Context != c.EntityType+"."+c.Field {
			t.Fatalf("候选语境 %q 与实体类型 / 字段不符（%s / %s）", c.Context, c.EntityType, c.Field)
		}
		if c.SourceHash != i18n.ContentHash(c.SourceText) {
			t.Fatalf("候选 %s（%q）的 source_hash 与原文不符", c.Context, c.SourceText)
		}
		if c.EntityName == "" || c.Field == "" {
			t.Fatalf("候选 %s 缺少实体名 / 字段标识", c.Context)
		}
	}

	numeric, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "编号商品", Subtitle: "2024", Slug: "numeric-product",
		DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("创建数字副标题商品失败: %v", err)
	}
	numCands, err := f.products.ProductTranslationCandidates(ctx, f.projectID, numeric.ID)
	if err != nil {
		t.Fatalf("收集数字商品候选失败: %v", err)
	}
	numIdx := trIndex(numCands)
	if numIdx.hasContext("product.subtitle") {
		t.Fatalf("纯数字副标题不应进候选：%+v", numIdx.byContext)
	}
	if !numIdx.has("product.name", "编号商品") {
		t.Fatalf("商品名仍应是候选：%+v", numIdx.byContext)
	}

	projectCands, err := f.products.ProjectTranslationCandidates(ctx, f.projectID)
	if err != nil {
		t.Fatalf("收集工程级候选失败: %v", err)
	}
	pIdx := trIndex(projectCands)
	if !pIdx.has("product_category.name", "未挂载分类") {
		t.Fatalf("工程级候选应含未被商品引用的分类（%s）：%+v", orphan.ID, pIdx.byContext)
	}
	seen := map[string]bool{}
	for _, c := range projectCands {
		key := i18n.ContentIndexKey(c.SourceHash, c.Context)
		if seen[key] {
			t.Fatalf("工程级候选应按 (hash, context) 去重，出现重复：%s", key)
		}
		seen[key] = true
	}
}

func TestProductArtifactTranslatesFieldsWithByteFallback(t *testing.T) {
	f := newTRFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	attr, err := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "颜色", Key: "color", IsVariation: boolPtr(true),
		Values: []productdto.AttributeValueReq{
			{Label: "红色", Key: "red"},
			{Label: "蓝色", Key: "blue"},
		},
	})
	if err != nil {
		t.Fatalf("创建属性组失败: %v", err)
	}
	price := 99.0
	created, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "夏季衬衫", Subtitle: "纯棉透气",
		Description:  json.RawMessage(`{"html":"<p>纯棉透气</p>"}`),
		Slug:         "summer-shirt",
		Unit:         "件",
		Images:       []string{"/storage/a.jpg", "/storage/b.jpg"},
		ImageAlts:    []string{"正面图", "背面图"},
		AttributeIDs: []string{attr.ID},
		DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	if _, err := f.products.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: created.ID}); err != nil {
		t.Fatalf("生成变体组合失败: %v", err)
	}

	f.putTranslation(t, "夏季衬衫", "product.name", "en-US", "Summer Shirt")
	f.putTranslation(t, "<p>纯棉透气</p>", "product.description", "en-US", "<p>Breathable cotton</p>")
	f.putTranslation(t, "正面图", "product.imageAlts", "en-US", "Front view")
	f.putTranslation(t, "颜色", "product_attribute.name", "en-US", "Color")
	f.putTranslation(t, "红色", "product_attribute.values", "en-US", "Red")

	zh := f.compileEntityPage(t, "product", created.ID, "zh-CN", trProductDoc)
	en := f.compileEntityPage(t, "product", created.ID, "en-US", trProductDoc)
	if zh == en {
		t.Fatalf("切换语言产物文本应不同")
	}
	for _, want := range []string{
		"夏季衬衫", "纯棉透气", `alt="正面图"`, `alt="背面图"`,
		"颜色", "红色", `value="red"`, "¥99",
	} {
		if !strings.Contains(zh, want) {
			t.Fatalf("中文产物应含 %q：%s", want, zh)
		}
	}
	for _, want := range []string{
		"Summer Shirt",
		"Breathable cotton",
		`alt="Front view"`,
		"Color", "Red",
		"纯棉透气",
		`alt="背面图"`,
		"蓝色",
		`value="blue"`,
		"¥99",
	} {
		if !strings.Contains(en, want) {
			t.Fatalf("英文产物应含 %q：%s", want, en)
		}
	}
	for _, bad := range []string{"夏季衬衫", "正面图", ">红色<"} {
		if strings.Contains(en, bad) {
			t.Fatalf("英文产物不应残留原文 %q：%s", bad, en)
		}
	}

	for _, field := range []string{
		"product.slug", "product.sku", "product.images", "product.defaultImage",
		"product.price", "product.priceRange", "product.minPrice", "product.maxPrice",
	} {
		zhValue := trResolve(t, f, "product", created.ID, "zh-CN", field)
		enValue := trResolve(t, f, "product", created.ID, "en-US", field)
		if zhValue != enValue {
			t.Fatalf("%s 不应随语言变化：%q vs %q", field, zhValue, enValue)
		}
	}
	if zhName, enName := trResolve(t, f, "product", created.ID, "zh-CN", "product.name"),
		trResolve(t, f, "product", created.ID, "en-US", "product.name"); zhName == enName {
		t.Fatalf("商品名应随语言变化（%q / %q）", zhName, enName)
	}
}

func TestCategoryBrandTagAttributeEntityTranslations(t *testing.T) {
	f := newTRFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	category, err := f.products.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "夏季新品", Slug: "summer-new", Description: "清凉一夏",
	})
	if err != nil {
		t.Fatalf("创建分类失败: %v", err)
	}
	brand, err := f.products.CreateBrand(ctx, &productdto.CreateBrandReq{
		ProjectID: f.projectID, Name: "晴山", Slug: "qingshan",
	})
	if err != nil {
		t.Fatalf("创建品牌失败: %v", err)
	}
	tag, err := f.products.CreateTag(ctx, &productdto.CreateTagReq{
		ProjectID: f.projectID, Name: "当季", Slug: "season",
	})
	if err != nil {
		t.Fatalf("创建标签失败: %v", err)
	}
	attr, err := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "颜色", Key: "color",
		Values: []productdto.AttributeValueReq{
			{Label: "红色", Key: "red"},
			{Label: "蓝色", Key: "blue"},
		},
	})
	if err != nil {
		t.Fatalf("创建属性组失败: %v", err)
	}
	f.putTranslation(t, "夏季新品", "product_category.name", "en-US", "Summer New")
	f.putTranslation(t, "清凉一夏", "product_category.description", "en-US", "Cool summer")
	f.putTranslation(t, "晴山", "product_brand.name", "en-US", "Qingshan")
	f.putTranslation(t, "当季", "product_tag.name", "en-US", "In season")
	f.putTranslation(t, "颜色", "product_attribute.name", "en-US", "Color")
	f.putTranslation(t, "红色", "product_attribute.values", "en-US", "Red")

	cases := []struct {
		entityType, entityID, doc, zh, en string
	}{
		{"product_category", category.ID, trHeadingDoc("product_category.name"), "夏季新品", "Summer New"},
		{"product_category", category.ID, trHeadingDoc("product_category.description"), "清凉一夏", "Cool summer"},
		{"product_brand", brand.ID, trHeadingDoc("product_brand.name"), "晴山", "Qingshan"},
		{"product_tag", tag.ID, trHeadingDoc("product_tag.name"), "当季", "In season"},
		{"product_attribute", attr.ID, trHeadingDoc("product_attribute.name"), "颜色", "Color"},
	}
	for _, c := range cases {
		zh := f.compileEntityPage(t, c.entityType, c.entityID, "zh-CN", c.doc)
		if !strings.Contains(zh, c.zh) {
			t.Fatalf("%s 中文产物应含 %q：%s", c.entityType, c.zh, zh)
		}
		en := f.compileEntityPage(t, c.entityType, c.entityID, "en-US", c.doc)
		if !strings.Contains(en, c.en) {
			t.Fatalf("%s 英文产物应含译文 %q：%s", c.entityType, c.en, en)
		}
		if strings.Contains(en, c.zh) {
			t.Fatalf("%s 英文产物不应残留原文 %q：%s", c.entityType, c.zh, en)
		}
	}

	values := trResolve(t, f, "product_attribute", attr.ID, "en-US", "product_attribute.values")
	if !strings.Contains(values, "Red") || !strings.Contains(values, "蓝色") {
		t.Fatalf("属性值应逐元素取译文并回退原文，实际 %s", values)
	}
	if strings.Contains(values, "red") {
		t.Fatalf("属性值数组不应含值 key（它是筛选参数 / 规格组合的稳定标识），实际 %s", values)
	}

	if got := trResolve(t, f, "product_attribute", attr.ID, "en-US", "product_attribute.key"); got != "color" {
		t.Fatalf("属性值 key 不应随语言变化：%q", got)
	}
	if got := trResolve(t, f, "product_category", category.ID, "en-US", "product_category.slug"); got != "summer-new" {
		t.Fatalf("slug 不应随语言变化：%q", got)
	}
}

func trHeadingDoc(field string) string {
	return `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"h1","type":"core.heading","props":{"binding":{"field":"` + field + `"}}}]}`
}

func TestProductTranslationInvalidatedAfterSourceChange(t *testing.T) {
	f := newTRFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	price := 99.0
	created, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "夏季衬衫", Slug: "summer-shirt", DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	f.putTranslation(t, "夏季衬衫", "product.name", "en-US", "Summer Shirt")
	if en := f.compileEntityPage(t, "product", created.ID, "en-US", trProductDoc); !strings.Contains(en, "Summer Shirt") {
		t.Fatalf("改原文前英文构建应使用译文：%s", en)
	}

	newName := "春季衬衫"
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: created.ID, Name: &newName}); err != nil {
		t.Fatalf("改商品名失败: %v", err)
	}
	en := f.compileEntityPage(t, "product", created.ID, "en-US", trProductDoc)
	if strings.Contains(en, "Summer Shirt") {
		t.Fatalf("改原文后旧译文必须失效：%s", en)
	}
	if !strings.Contains(en, "春季衬衫") {
		t.Fatalf("无译文字段应逐字节回退新原文：%s", en)
	}

	engine := trWorkbench(t, f)
	body := trWorkbenchGet(t, engine, f.projectID, created.ID, "en-US")
	if !strings.Contains(body, `name="rowHash" value="`+i18n.ContentHash("春季衬衫")+`"`) {
		t.Fatalf("工作台应列出新原文的指纹 %s", body)
	}
	if strings.Contains(body, `name="rowHash" value="`+i18n.ContentHash("夏季衬衫")+`"`) {
		t.Fatalf("工作台不应再列出旧原文的指纹 %s", body)
	}
	// 状态列在模板里按**请求语言**取词（trStateMiss），本次请求 ?lang=en-US，
	// 词库已加载，所以是英文的 Missing（缺词时才会回落模板兜底「缺失」）。
	if !strings.Contains(body, "Missing") {
		t.Fatalf("旧译文失效后工作台应显示「缺失」状态 %s", body)
	}

	rec := postForm(engine, "/api/product/translations/save", url.Values{
		"projectId": {f.projectID}, "productId": {created.ID}, "lang": {"en-US"},
		"rowContext": {"product.name"}, "rowHash": {i18n.ContentHash("夏季衬衫")},
		"rowTarget": {"Spring Shirt"},
	})

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("指纹不符应回 HTML 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "原文已变更") {
		t.Fatalf("应提示原文已变更，实际 %q", rec.Body.String())
	}
	if n := trTranslationCount(t, f.db, "en-US"); n != 1 {
		t.Fatalf("旧指纹提交不应写库（应仍只有最初 1 条），实际 %d", n)
	}
}

func TestProductTranslationWorkbenchSaveAndStaleMarking(t *testing.T) {
	f := newTRFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	price := 99.0
	created, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "夏季衬衫", Slug: "summer-shirt", DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	const pageID = "aaaaaaaa-1111-0000-0000-000000000012"
	trInsertPage(t, f.db, pageID, f.projectID, "/about")
	engine := trWorkbench(t, f)

	body := trWorkbenchGet(t, engine, f.projectID, created.ID, "en-US")
	for _, want := range []string{
		`name="rowContext" value="product.name"`,
		`name="rowHash" value="` + i18n.ContentHash("夏季衬衫") + `"`,
		"Product name", "夏季衬衫", "Product translations",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("工作台缺少 %q %s", want, body)
		}
	}

	form := url.Values{
		"projectId": {f.projectID}, "productId": {created.ID}, "lang": {"en-US"},
		"rowContext": {"product.name"}, "rowHash": {i18n.ContentHash("夏季衬衫")},
		"rowTarget": {"Summer Shirt"},
	}
	saved := postForm(engine, "/api/product/translations/save", form)
	assertHTMLSuccess(t, saved, "保存")

	target, engineName := trTranslationOf(t, f.db, "夏季衬衫", "product.name", "en-US")
	if target != "Summer Shirt" || engineName != i18n.ContentEngineManual {
		t.Fatalf("译文 / 来源不符：%q / %q", target, engineName)
	}

	if !trPageStale(t, f.db, pageID) {
		t.Fatalf("译文变更应把受影响页面标记待重建")
	}

	if en := f.compileEntityPage(t, "product", created.ID, "en-US", trProductDoc); !strings.Contains(en, "Summer Shirt") {
		t.Fatalf("工作台写下的译文应在英文产物里生效：%s", en)
	}

	if err := f.db.Exec("UPDATE pages SET stale = false WHERE id = ?", pageID).Error; err != nil {
		t.Fatalf("复位 stale 失败: %v", err)
	}
	again := postForm(engine, "/api/product/translations/save", form)
	assertHTMLSuccess(t, again, "幂等保存")
	if trPageStale(t, f.db, pageID) {
		t.Fatal("译文未变化不应触发重建标记")
	}
	if n := trTranslationCount(t, f.db, "en-US"); n != 1 {
		t.Fatalf("幂等保存不应新增译文行，实际 %d", n)
	}
}

func TestProductTranslationStalesPresentationInstances(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	enabled := true
	if _, err := f.projects.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: f.projectID,
		Locales: []projectdto.LocaleItem{
			{Lang: "en-US", SortOrder: 1, IsDefault: true, Enabled: &enabled},
			{Lang: "zh-CN", SortOrder: 2, Enabled: &enabled},
		},
	}); err != nil {
		t.Fatalf("启用站点语言失败: %v", err)
	}
	productID := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉", 99, 99)
	inst := f.publish(t, productID, "/products/summer-shirt")
	if trInstanceStale(t, f.db, inst.ID) {
		t.Fatalf("前置条件：刚发布的实例不应是 stale")
	}
	const pageID = "aaaaaaaa-2222-0000-0000-000000000012"
	trInsertPage(t, f.db, pageID, f.projectID, "/about")

	gin.SetMode(gin.TestMode)
	pages := pageservice.NewService(pagemodel.NewPageModel(f.db), nil, nil, f.projects, nil, nil, nil, nil, nil)
	handle := producthttp.NewProductTranslationHandle(f.products, pages, f.pres)
	handle.SetContentTranslationStore(i18n.NewContentWriter(f.db))
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	// 写成功走共享成功出口（生产 assembly 同款接线）。
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.POST("/api/product/translations/save", pageview.HTMLFragment, pageview.UseRenderer(pageview.RenderPage), handle.SaveProductTranslations)

	saved := postForm(engine, "/api/product/translations/save", url.Values{
		"projectId": {f.projectID}, "productId": {productID}, "lang": {"en-US"},
		"rowContext": {"product.name"}, "rowHash": {i18n.ContentHash("夏季衬衫")},
		"rowTarget": {"Summer Shirt"},
	})
	assertHTMLSuccess(t, saved, "保存")
	if !trInstanceStale(t, f.db, inst.ID) {
		t.Fatalf("译文变更应标记该商品的自动发布实例待重建")
	}
	if !trPageStale(t, f.db, pageID) {
		t.Fatalf("译文变更应同时标记手工页面待重建（page 侧链路不变）")
	}

	if _, err := f.pres.Rebuild(ctx, &presentationdto.RebuildReq{EntityID: productID}); err != nil {
		t.Fatalf("重建实例失败: %v", err)
	}
	if html := activeHTML(t, "/products/summer-shirt"); !strings.Contains(html, "Summer Shirt") {
		t.Fatalf("重建后实例产物应使用英文译文：%s", html)
	}
}

func TestProductsPageShowsTranslationEntry(t *testing.T) {
	engine, f := newVariantPageEngine(t)
	if engine == nil {
		return
	}
	price := 99.0
	created, err := f.svc.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "入口商品", Slug: "entry-product", DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	body := getProductsPage(engine, f.projectID)
	wantEdit := editLocation(f.projectID, created.ID)
	if !strings.Contains(body, wantEdit) {
		t.Fatalf("商品行内缺少「编辑」入口（缺 %s） %s", wantEdit, body)
	}
	want := "/admin/products/translations?project=" + f.projectID + "&amp;product=" + created.ID
	if strings.Contains(body, want) {
		t.Fatalf("多语言不该再占列表操作列一格（已移入编辑页）：%s", body)
	}

	editBody := getProductEditPage(engine, f.projectID, created.ID)
	if !strings.Contains(editBody, want) {
		t.Fatalf("编辑页缺少「多语言」入口（缺 %s） %s", want, editBody)
	}
}
