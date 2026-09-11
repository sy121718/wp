// Package feature product 模块 feature 测试 —— 商品详情组件与详情模板（issue #6）。
//
// 覆盖本票验收（真实 PostgreSQL + 生产 DDL + 真实 service 装配）：
//  1. core.product 声明所需商品字段，字段绑定受注册表白名单约束（越界 / 跨数据源被拒绝）；
//  2. 默认商品详情模板作为内容模板（完整文档层）落地，发布实例按实体类型自动取用；
//  3. 同一套模板复用于任意商品 —— 换商品只换数据，不改模板；
//  4. 模板内部引用的全局区块构建期内联展开（走既有的 core.globalref 机制）；
//  5. 建立发布实例 → 编译 → 激活，访问面直读静态文件（请求期零查库）；
//  6. 商品可翻译字段随构建语言切换（语境 product.<字段名>）。
package feature

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go_wp/internal/builder/core"
	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
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

	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/public/migrations"
	"go_wp/public/test/support"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// detailFixture 一套真实装配：隔离 PG schema + 生产迁移 + 数据种子 +
// 商品 / 内容模板 / 全局块 / 自动发布实例四个真实 service（与线上装配同形）。
type detailFixture struct {
	db        *gorm.DB
	products  *productservice.Service
	templates contenttemplatecontract.ContentTemplateService
	blocks    *blockservice.Service
	pres      *presentationservice.Service
	projects  *projectservice.Service
	projectID string
}

// newDetailFixture 装配 fixture；PG 不可用时 t.Skip（返回 nil）。
func newDetailFixture(t *testing.T) *detailFixture {
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
	project, err := projects.Create(ctx, &projectdto.CreateReq{Name: "商品详情测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败: %v", err)
	}
	// 数据种子：默认商品详情模板（迁移 085）需要一个已存在的工程，故在工程创建之后跑。
	if err := migrations.RunSeeds(db); err != nil {
		t.Fatalf("执行数据种子失败: %v", err)
	}
	// 实体类型注册表：与线上装配同款 —— 商品模块注册自己的实体类型，
	// 内容模板与发布实例只依赖注册表（不认识具体领域）。
	registry := core.NewEntitySourceRegistry()
	products := productservice.NewService(productmodel.NewModel(db), projects)
	products.SetContentStore(i18n.NewDBContentStore(db))
	if err := products.RegisterEntityTypes(registry); err != nil {
		t.Fatalf("注册商品实体类型失败: %v", err)
	}
	templates := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, registry)
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	pres := presentationservice.NewService(presentationmodel.NewModel(db), templates, registry, projects, blocks)
	return &detailFixture{
		db: db, products: products, templates: templates, blocks: blocks,
		pres: pres, projects: projects, projectID: project.ID,
	}
}

// createProduct 建一个可用商品（名称 / 卖点 / 图集 / 主图 / 描述 / 两个变体）。
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
	// 第二个变体：让价格区间有跨度（商品主体不存价格，区间由变体派生）。
	if _, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProductID: res.ID, SKUCode: slug + "-L", Price: &highPrice,
	}); err != nil {
		t.Fatalf("新增变体失败: %v", err)
	}
	return res.ID
}

// publish 为商品建立发布实例（URL 由 slug 派生）。
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

// activeHTML 读取访问面上某路径的已激活 index.html（访客面等价读法）。
func activeHTML(t *testing.T, urlPath string) string {
	t.Helper()
	rel := strings.TrimPrefix(urlPath, "/")
	b, err := os.ReadFile(filepath.Join(pipeline.ActiveRoot(), rel, "index.html"))
	if err != nil {
		t.Fatalf("读取激活产物 %s 失败: %v", urlPath, err)
	}
	return string(b)
}

// TestProductDetailTemplateSeeded 默认商品详情模板作为内容模板落地（完整文档层），
// 发布实例按实体类型即可解析到 —— 新建商品无需手工拼装模板。
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

// TestProductDetailPublishEndToEnd 商品 → 模板 → 编译 → 激活 → 访问面直读。
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
		"夏季衬衫",                            // product.name → 标题
		"夏季衬衫卖点",                          // product.subtitle
		"¥99 ~ 199",                       // product.priceRange（多变体区间）
		"/storage/summer-shirt-cover.jpg", // product.defaultImage
		"/storage/summer-shirt-1.jpg",     // product.images（图集）
		"纯棉透气",                            // product.description（富文本清洗后输出）
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("激活产物应含 %q，实际: %s", want, html)
		}
	}
	// 商品字段是构建期静态填入：产物里不应残留绑定字段名与组件类型名。
	if strings.Contains(html, "product.") || strings.Contains(html, "core.product") {
		t.Fatalf("产物不应残留绑定字段名: %s", html)
	}
}

// TestProductDetailTemplateReusedForAnyProduct 同一套模板复用于任意商品：
// 换商品只换数据，不改模板。
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

	// 模板未被改动（版本与文档与发布前一致）。
	tplAfter, err := f.templates.ResolveTemplate(context.Background(), "product")
	if err != nil {
		t.Fatalf("再次解析模板失败: %v", err)
	}
	if tplAfter.VersionID != tplBefore.VersionID || string(tplAfter.Document) != string(tplBefore.Document) {
		t.Fatalf("复用不应改动模板（版本与文档应保持一致）")
	}
}

// TestProductDetailTemplateInlinesBlockRef 模板内部引用全局区块（页眉），构建期内联展开。
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

	// 商品模板：页眉区块引用 + 商品详情组件（内容模板可以引用区块，docs/02-D §1.2）。
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

// TestProductTemplateRejectsForeignFieldBinding 字段绑定受白名单约束：
// 商品数据源之外的绑定（article.*）与白名单外字段（product.bogus）在模板保存时即被拒绝。
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
		// 跨数据源绑定：组件自身的字段路径校验先拒绝（数据源不符）。
		"跨数据源绑定": {`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"pd","type":"core.product","props":{"titleField":"article.title"}}]}`, contenttemplateenums.ErrDataInvalid},
		// 白名单外字段：路径合法 → 由注册表白名单校验拒绝。
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

// TestProductPublishRejectsForeignFieldBinding 构建期兜底：绕过模板保存校验写入的越界绑定，
// 在建立发布实例时同样被拒绝（白名单校验不只有保存一道）。
func TestProductPublishRejectsForeignFieldBinding(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	// 直写模板表（绕过 service 的保存校验），模拟「旧模板 / 绕过接口写入」的存量数据。
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

// TestProductTranslatableFieldsFollowLanguage 商品可翻译字段随构建语言切换：
// 同一商品 + 同一模板，工程默认语言切到 en-US 后产物文本随之变化（语境 product.<字段名>）。
func TestProductTranslatableFieldsFollowLanguage(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	// 写入一条商品名译文（内容寻址：原文哈希 + 语境 + 语言）。
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

	// 切换站点默认语言到 en-US → 重建 → 产物文本随之切换。
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
	// 无译文的字段逐字回退原文（副标题没有 en-US 译文）。
	if !strings.Contains(html, "夏季衬衫卖点") {
		t.Fatalf("无译文字段应回退原文: %s", html)
	}
}
