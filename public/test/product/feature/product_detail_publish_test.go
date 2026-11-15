package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	publish "go_wp/pkg/publish"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
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
	"go_wp/pkg/builder/core"

	"go_wp/pkg/i18n"
	"go_wp/public/schema"
	"go_wp/public/test/support"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type detailFixture struct {
	db        *gorm.DB
	products  *productservice.Service
	templates contenttemplatecontract.ContentTemplateService
	blocks    *blockservice.Service
	pres      *presentationservice.Service
	projects  *projectservice.Service
	projectID string
}

func newDetailFixture(t *testing.T) *detailFixture {
	t.Helper()
	t.Setenv("GO_WP_ARTIFACT_ROOT", t.TempDir())
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "商品详情测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}

	if err := schema.Apply(db); err != nil {
		t.Fatalf("载入 schema 失败：%v", err)
	}

	registry := core.NewEntitySourceRegistry()
	products := productservice.NewService(productmodel.NewModel(db), projects, masterdataservice.NewService(masterdatamodel.NewModel(db), projects))
	products.SetContentStore(i18n.NewDBContentStore(db))
	if err := products.RegisterEntityTypes(registry); err != nil {
		t.Fatalf("注册商品实体类型失败: %v", err)
	}
	templates := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, registry)
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	pres := presentationservice.NewService(presentationmodel.NewModel(db), templates, registry, projects, blocks,
		pubservice.NewService(pubmodel.NewPublicationModel(db)))
	return &detailFixture{
		db: db, products: products, templates: templates, blocks: blocks,
		pres: pres, projects: projects, projectID: project.ID,
	}
}

func (f *detailFixture) createProduct(t *testing.T, name, slug, desc string, price, highPrice float64) string {
	t.Helper()
	ctx := context.Background()
	payload, err := json.Marshal(map[string]string{"html": "<p>" + desc + "</p>"})
	if err != nil {
		t.Fatalf("描述编码失败: %v", err)
	}
	res, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: name, Slug: slug,
		Subtitle:     name + "卖点",
		Description:  payload,
		Images:       []string{"/storage/" + slug + "-1.jpg", "/storage/" + slug + "-2.jpg"},
		DefaultImage: "/storage/" + slug + "-cover.jpg",
		DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}

	if _, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProductID: res.ID, SKUCode: slug + "-L", Price: &highPrice,
	}); err != nil {
		t.Fatalf("新增变体失败: %v", err)
	}
	return res.ID
}

func (f *detailFixture) publish(t *testing.T, productID, urlPath string) *presentationdto.InstanceResp {
	t.Helper()
	inst, err := f.pres.CreateInstance(context.Background(), &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "product", EntityID: productID, URLPath: urlPath,
	})
	if err != nil {
		t.Fatalf("创建商品发布实例失败: %v", err)
	}
	return inst
}

func activeHTML(t *testing.T, urlPath string) string {
	t.Helper()
	rel := strings.TrimPrefix(urlPath, "/")
	b, err := os.ReadFile(filepath.Join(publish.ActiveRoot(), rel, "index.html"))
	if err != nil {
		t.Fatalf("读取激活产物 %s 失败: %v", urlPath, err)
	}
	return string(b)
}

func TestProductDetailTemplateSeeded(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	tpl, err := f.templates.ResolveTemplate(context.Background(), "product")
	if err != nil {
		t.Fatalf("应按实体类型解析到默认商品详情模板: %v", err)
	}
	if tpl.EntityType != "product" {
		t.Fatalf("模板实体类型应为 product: %q", tpl.EntityType)
	}
	doc := string(tpl.Document)
	if !strings.Contains(doc, "core.product") {
		t.Fatalf("默认模板应使用商品详情组件: %s", doc)
	}
	if !strings.Contains(doc, "product.name") || !strings.Contains(doc, "product.priceRange") {
		t.Fatalf("默认模板应声明所需商品字段: %s", doc)
	}
}

func TestProductDetailPublishEndToEnd(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	productID := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉透气", 99, 199)
	inst := f.publish(t, productID, "/products/summer-shirt")
	if inst.ArtifactHash == "" || inst.ArtifactID == "" || inst.Status == "" {
		t.Fatalf("实例缺少产物: %+v", inst)
	}

	html := activeHTML(t, "/products/summer-shirt")
	for _, want := range []string{
		"夏季衬衫",
		"夏季衬衫卖点",
		"¥99 ~ 199",
		testMediaBaseURL + "/summer-shirt-cover.jpg",
		testMediaBaseURL + "/summer-shirt-1.jpg",
		"纯棉透气",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("激活产物应含 %q，实际: %s", want, html)
		}
	}

	if strings.Contains(html, "product.") || strings.Contains(html, "core.product") {
		t.Fatalf("产物不应残留绑定字段名: %s", html)
	}
}

func TestProductDetailTemplateReusedForAnyProduct(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	first := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉", 99, 199)
	second := f.createProduct(t, "冬季外套", "winter-coat", "加厚", 299, 499)
	tplBefore, err := f.templates.ResolveTemplate(context.Background(), "product")
	if err != nil {
		t.Fatalf("解析模板失败: %v", err)
	}

	f.publish(t, first, "/products/summer-shirt")
	f.publish(t, second, "/products/winter-coat")

	firstHTML := activeHTML(t, "/products/summer-shirt")
	secondHTML := activeHTML(t, "/products/winter-coat")
	if !strings.Contains(firstHTML, "夏季衬衫") || strings.Contains(firstHTML, "冬季外套") {
		t.Fatalf("首个产物应只含自己的数据: %s", firstHTML)
	}
	if !strings.Contains(secondHTML, "冬季外套") || strings.Contains(secondHTML, "夏季衬衫") {
		t.Fatalf("第二个产物应只含自己的数据: %s", secondHTML)
	}

	tplAfter, err := f.templates.ResolveTemplate(context.Background(), "product")
	if err != nil {
		t.Fatalf("再次解析模板失败: %v", err)
	}
	if tplAfter.VersionID != tplBefore.VersionID || string(tplAfter.Document) != string(tplBefore.Document) {
		t.Fatalf("复用不应改动模板（版本与文档应保持一致）")
	}
}

func TestProductDetailTemplateInlinesBlockRef(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	headerDoc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"hdr","type":"core.container","props":{"tag":"header","layout":{"engine":"flex","flex":{"direction":"column"}}},"children":[{"id":"hdr-t","type":"core.heading","props":{"text":"全站页眉","tag":"span"}}]}]}`
	block, err := f.blocks.Create(ctx, &blockdto.CreateReq{
		ProjectID: f.projectID, Name: "全站页眉", Kind: "header",
		Document: json.RawMessage(headerDoc),
	})
	if err != nil {
		t.Fatalf("创建页眉区块失败: %v", err)
	}

	doc := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"g1","type":"core.globalref","props":{"blockId":"` + block.ID + `"}},{"id":"pd","type":"core.product","props":{"titleField":"product.name","priceField":"product.priceRange"}}]}`
	if _, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{
		EntityType: "product", Name: "带页眉的商品模板", DraftDocument: json.RawMessage(doc),
	}); err != nil {
		t.Fatalf("创建带区块引用的商品模板失败: %v", err)
	}

	productID := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉", 99, 99)
	f.publish(t, productID, "/products/summer-shirt")
	html := activeHTML(t, "/products/summer-shirt")
	if !strings.Contains(html, "全站页眉") {
		t.Fatalf("模板内引用的区块应在构建期内联展开: %s", html)
	}
	if !strings.Contains(html, "夏季衬衫") {
		t.Fatalf("商品字段应同时静态填入: %s", html)
	}
}

func TestProductTemplateRejectsForeignFieldBinding(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	cases := map[string]struct {
		doc  string
		want string
	}{

		"跨数据源绑定": {`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pd","type":"core.product","props":{"titleField":"article.title"}}]}`, contenttemplateenums.ErrDataInvalid},

		"白名单外字段": {`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pd","type":"core.product","props":{"titleField":"product.bogus"}}]}`, contenttemplateenums.ErrFieldBindingInvalid},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := f.templates.Create(ctx, &contenttemplatedto.CreateReq{
				EntityType: "product", Name: "非法模板-" + name, DraftDocument: json.RawMessage(c.doc),
			})
			if err == nil {
				t.Fatalf("越界字段绑定应被拒绝")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("错误应指向字段绑定校验（期望含 %s）: %v", c.want, err)
			}
		})
	}
}

func TestProductPublishRejectsForeignFieldBinding(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	doc := json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pd","type":"core.product","props":{"titleField":"product.bogus"}}]}`)
	now := time.Now().UTC()
	m := contenttemplatemodel.NewModel(f.db)
	tplID, verID := uuid.NewString(), uuid.NewString()
	if err := m.CreateWithVersion(ctx, &contenttemplatemodel.TemplateEntity{
		ID: tplID, ProjectID: f.projectID, Name: "越界绑定模板", EntityType: "product",
		DraftDocument: doc, DraftVersion: 1, CurrentVersionID: &verID, CreatedAt: now, UpdatedAt: now,
	}, &contenttemplatemodel.VersionEntity{
		ID: verID, TemplateID: tplID, Version: 1, Document: doc, SourceHash: "bogus",
		CreatedBy: uuid.Nil.String(), CreatedAt: now,
	}); err != nil {
		t.Fatalf("直写模板失败: %v", err)
	}
	productID := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉", 99, 99)
	_, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "product", EntityID: productID, URLPath: "/products/summer-shirt",
	})
	if err == nil {
		t.Fatalf("越界字段绑定应在构建期被拒绝")
	}
	if !strings.Contains(err.Error(), "白名单") {
		t.Fatalf("错误应说明白名单越界: %v", err)
	}
}

func TestProductDetailRendersSpecSelectorForMultiVariant(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	color, err := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "颜色", Key: "color",
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
		ProjectID: f.projectID, Name: "夏季衬衫", Slug: "summer-shirt",
		AttributeIDs: []string{color.ID}, DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	if _, err := f.products.GenerateVariants(ctx, &productdto.GenerateVariantsReq{ProductID: created.ID}); err != nil {
		t.Fatalf("生成变体组合失败: %v", err)
	}
	f.publish(t, created.ID, "/products/summer-shirt")

	html := activeHTML(t, "/products/summer-shirt")

	for _, want := range []string{
		`<div class="sky-product-options">`,
		`<input type="radio" class="sky-product-option-radio sky-sr-only"`,
		`<label class="sky-product-option-value"`,
		`<div class="sky-product-variant">`,
		"颜色", "红色", "蓝色",
		"¥99",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("激活产物应含 %q，实际: %s", want, html)
		}
	}
	if strings.Contains(html, "product.options") || strings.Contains(html, "product.variants") {
		t.Fatalf("产物不应残留绑定字段名: %s", html)
	}
}

func TestProductDetailHidesSpecSelectorForSingleVariant(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	price := 99.0
	created, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID, Name: "孤品衬衫", Slug: "solo-shirt", DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	if created.VariantCount != 1 {
		t.Fatalf("前置条件：单变体商品，实际 %d 个变体", created.VariantCount)
	}
	f.publish(t, created.ID, "/products/solo-shirt")

	html := activeHTML(t, "/products/solo-shirt")

	for _, unwanted := range []string{
		`<div class="sky-product-options">`,
		`<input type="radio"`,
	} {
		if strings.Contains(html, unwanted) {
			t.Fatalf("单变体商品不应输出规格选择器标记 %q: %s", unwanted, html)
		}
	}
	if !strings.Contains(html, "孤品衬衫") || !strings.Contains(html, "¥99") {
		t.Fatalf("单变体商品仍应正常输出标题与价格: %s", html)
	}
}

func TestProductTranslatableFieldsFollowLanguage(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	if err := f.db.Exec(
		"INSERT INTO sys_translation (source_hash, context, lang, source_text, target_text, engine) VALUES (?,?,?,?,?,'manual')",
		i18n.ContentHash("夏季衬衫"), "product.name", "en-US", "夏季衬衫", "Summer Shirt",
	).Error; err != nil {
		t.Fatalf("写入译文失败: %v", err)
	}
	productID := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉", 99, 99)
	f.publish(t, productID, "/products/summer-shirt")
	if html := activeHTML(t, "/products/summer-shirt"); !strings.Contains(html, "夏季衬衫") {
		t.Fatalf("默认语言产物应为原文: %s", html)
	}

	enabled := true
	if _, err := f.projects.SaveLocales(ctx, &projectdto.LocalesSaveReq{
		ProjectID: f.projectID,
		Locales: []projectdto.LocaleItem{
			{Lang: "zh-CN", SortOrder: 1, Enabled: &enabled},
			{Lang: "en-US", SortOrder: 2, IsDefault: true, Enabled: &enabled},
		},
	}); err != nil {
		t.Fatalf("保存语言清单失败: %v", err)
	}
	if _, err := f.pres.Rebuild(ctx, &presentationdto.RebuildReq{EntityID: productID}); err != nil {
		t.Fatalf("重建失败: %v", err)
	}
	html := activeHTML(t, "/products/summer-shirt")
	if !strings.Contains(html, "Summer Shirt") {
		t.Fatalf("英文构建应使用译文: %s", html)
	}

	if !strings.Contains(html, "夏季衬衫卖点") {
		t.Fatalf("无译文字段应回退原文: %s", html)
	}
}
