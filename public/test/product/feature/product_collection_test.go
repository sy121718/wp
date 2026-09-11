// Package feature product 模块 feature 测试 —— 商品集合源（issue #9）。
//
// 覆盖本票验收（真实 PostgreSQL + 生产迁移 + 真实 service 装配）：
//  1. 商品集合源出现在集合源元数据里（字段白名单 / 过滤维度 / 排序键）；
//  2. 构建期按商品集合源解析出集合项，字段受白名单约束；
//  3. 现有集合类组件（core.cardstack）绑定商品字段 → 构建产物包含商品数据；
//  4. 白名单外的字段 / 维度 / 未知集合源被拒绝；
//  5. 集合数据按工程隔离（不把别的站点商品渲染进本页）；
//  6. 商品可翻译字段随构建语言切换（语境 product.<字段名>）。
package feature

import (
	"context"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
)

// collectionRegistry 与线上装配同形的集合源注册表：内容模块与商品模块各注册
// 自己的集合源，构建层只认注册表（不认识具体领域模块）。
func (f *detailFixture) collectionRegistry(t *testing.T) core.CollectionRegistry {
	t.Helper()
	reg := core.NewCollectionRegistry()
	if err := reg.Register(contentservice.NewService(contentmodel.NewModel(f.db))); err != nil {
		t.Fatalf("注册内容集合源失败: %v", err)
	}
	if err := reg.Register(f.products); err != nil {
		t.Fatalf("注册商品集合源失败: %v", err)
	}
	return reg
}

// collectionPage 单个 cardstack 节点的最小页面（集合源与字段映射由 props 给出）。
func collectionPage(t *testing.T, props string) *builder.Page {
	t.Helper()
	raw := `{"settings":{"layout":{"mode":"boxed","maxWidth":"1200px"}},"root":[{"id":"n1","type":"core.cardstack","props":` + props + `}]}`
	p, err := builder.ParsePage([]byte(raw))
	if err != nil {
		t.Fatalf("解析页面文档失败: %v", err)
	}
	return p
}

// compileCollection 按线上同形选项编译集合页（工程 ID 与集合解析器都注入）。
func compileCollection(t *testing.T, p *builder.Page, reg core.CollectionRegistry, projectID string, opts ...builder.CompileOption) (*builder.CompiledPage, error) {
	t.Helper()
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("加载组件模板 Set 失败: %v", err)
	}
	base := []builder.CompileOption{
		builder.WithComponentSet(set),
		builder.WithCollectionResolver(reg),
		builder.WithProjectID(projectID),
	}
	return builder.Compile(p, append(base, opts...)...)
}

// collectionProps 商品集合卡的字段映射 props（标题 / 图片 / 价格 / 链接）。
func collectionProps() string {
	return `{"trigger":"hover","shape":"line","collectionSource":"content:product","collectionLimit":4,` +
		`"cardTitleField":"name","cardImageField":"images","cardMetaField":"priceRange",` +
		`"cardLinkField":"slug","cardLinkPrefix":"/products/"}`
}

// TestProductCollectionSourceMetadata 验收 1：商品源出现在集合源元数据里，
// 含字段白名单（与实体类型注册表同一份）、过滤维度、排序键。
func TestProductCollectionSourceMetadata(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	schemas, err := reg.CollectionSchemas(context.Background())
	if err != nil {
		t.Fatalf("取集合源元数据失败: %v", err)
	}
	var found *core.CollectionSchema
	for i := range schemas {
		if schemas[i].Source == productcontract.CollectionSourceProduct {
			found = &schemas[i]
		}
	}
	if found == nil {
		t.Fatalf("集合源元数据里应有 %s：%+v", productcontract.CollectionSourceProduct, schemas)
	}
	want := productcontract.FieldWhitelist(productcontract.EntityTypeProduct)
	if strings.Join(found.Fields, ",") != strings.Join(want, ",") {
		t.Fatalf("字段白名单应与商品 contract 一致：实际 %v / 期望 %v", found.Fields, want)
	}
	if found.Label == "" {
		t.Fatalf("集合源应有展示名（工作台下拉用）")
	}
	// 过滤维度：只开放 status，且带枚举。
	if len(found.Filters) != 1 || found.Filters[0].Key != "status" {
		t.Fatalf("过滤维度应只有 status：%+v", found.Filters)
	}
	if len(found.Filters[0].Enum) != 3 {
		t.Fatalf("status 维度应带 draft/published/archived 枚举：%+v", found.Filters[0])
	}
	// 排序键：确定性默认序（sort → createdAt）。
	if strings.Join(found.OrderBy, ",") != "sort,createdAt" {
		t.Fatalf("排序键白名单不符：%v", found.OrderBy)
	}
	// 内容集合源仍在（注册表是多源聚合，不是替换）。
	hasArticle := false
	for _, s := range schemas {
		if s.Source == "content:article" {
			hasArticle = true
		}
	}
	if !hasArticle {
		t.Fatalf("注册表应同时保留内容集合源：%+v", schemas)
	}
}

// TestProductCollectionResolveItems 验收 2：构建期解析出集合项，字段受白名单约束。
func TestProductCollectionResolveItems(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	summerID := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉透气", 99, 199)
	f.createProduct(t, "冬季外套", "winter-coat", "加厚保暖", 299, 299)

	ctx := core.WithBuildProjectID(context.Background(), f.projectID)
	items, err := reg.ResolveCollection(ctx, productcontract.CollectionSourceProduct, nil)
	if err != nil {
		t.Fatalf("解析商品集合失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("应解析出 2 个商品，实际 %d", len(items))
	}
	// 按 slug 定位（同 sort / 同创建时刻时次序由 id 兜底，测试不依赖具体排序）。
	var first map[string]any
	for _, item := range items {
		if item["slug"] == "summer-shirt" {
			first = item
		}
	}
	if first == nil || first["name"] != "夏季衬衫" {
		t.Fatalf("集合项应含商品标识字段与名称：%+v", items)
	}
	if first["id"] == "" || first["id"] == nil {
		t.Fatalf("集合项应含 id 系统字段：%+v", first)
	}
	// 价格由变体派生（商品主体不存价格）：最低价 + 区间。
	if first["priceRange"] != "99 ~ 199" {
		t.Fatalf("价格区间应由变体派生：%+v", first["priceRange"])
	}
	// 图片给真数组（集合卡的既有渲染约定是「数组取首元素」）。
	images, ok := first["images"].([]any)
	if !ok || len(images) != 2 || images[0] != "/storage/summer-shirt-1.jpg" {
		t.Fatalf("images 应为 URL 数组：%+v", first["images"])
	}
	// 白名单之外的字段不进集合项（集合项只暴露可渲染字段）。
	if _, leaked := first["status"]; leaked {
		t.Fatalf("集合项不应夹带白名单外字段：%+v", first)
	}
	// 过滤维度：status 白名单内可用（只留已发布的那一个商品）。
	published := productenums.StatusPublished
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: summerID, Status: &published}); err != nil {
		t.Fatalf("置为已发布失败: %v", err)
	}
	filtered, err := reg.ResolveCollection(ctx, productcontract.CollectionSourceProduct, map[string]string{"status": "published"})
	if err != nil {
		t.Fatalf("按 status 过滤失败: %v", err)
	}
	if len(filtered) != 1 || filtered[0]["name"] != "夏季衬衫" {
		t.Fatalf("status 过滤应只留已发布商品：%+v", filtered)
	}
}

// TestProductCollectionBuildsIntoArtifact 验收 3：既有集合类组件绑定商品字段后，
// 构建产物包含商品数据（且绑定字段名不残留、同输入同字节）。
func TestProductCollectionBuildsIntoArtifact(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉透气", 99, 199)
	f.createProduct(t, "冬季外套", "winter-coat", "加厚保暖", 299, 299)
	page := collectionPage(t, collectionProps())

	compiled, err := compileCollection(t, page, reg, f.projectID)
	if err != nil {
		t.Fatalf("编译集合页失败: %v", err)
	}
	for _, want := range []string{
		"夏季衬衫", "冬季外套", // 商品名（标题字段）
		"99 ~ 199", "299", // 价格区间（变体派生）
		"/storage/summer-shirt-1.jpg", // 图集首图（数组取首元素）
		"/products/summer-shirt",      // 链接前缀 + slug
		"sky-cardstack-card",          // 卡片结构确实渲染了
	} {
		if !strings.Contains(compiled.HTML, want) {
			t.Fatalf("构建产物应含 %q", want)
		}
	}
	// 商品字段是构建期静态填入：产物里不应残留绑定字段名或集合源标识。
	for _, unwanted := range []string{"content:product", "cardTitleField", "collectionSource"} {
		if strings.Contains(compiled.HTML, unwanted) {
			t.Fatalf("产物不应残留 %q", unwanted)
		}
	}
	// 确定性：同输入两次编译字节一致。
	again, err := compileCollection(t, page, reg, f.projectID)
	if err != nil {
		t.Fatalf("二次编译失败: %v", err)
	}
	if again.HTML != compiled.HTML || again.CSS != compiled.CSS {
		t.Fatalf("集合卡产物不确定：同输入两次编译字节不一致")
	}
}

// TestProductCollectionRejectsOutOfWhitelist 验收 4：白名单外的字段 / 维度 /
// 未知集合源在构建期被拒绝（不静默渲染成空白）。
func TestProductCollectionRejectsOutOfWhitelist(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉透气", 99, 199)
	ctx := core.WithBuildProjectID(context.Background(), f.projectID)

	t.Run("白名单外字段", func(t *testing.T) {
		page := collectionPage(t, `{"trigger":"hover","collectionSource":"content:product","cardTitleField":"status"}`)
		_, err := compileCollection(t, page, reg, f.projectID)
		if err == nil {
			t.Fatalf("白名单外字段应编译失败")
		}
		if !strings.Contains(err.Error(), "白名单") || !strings.Contains(err.Error(), "status") {
			t.Fatalf("报错应指出白名单越界并列出可用字段: %v", err)
		}
	})
	t.Run("未知集合源", func(t *testing.T) {
		page := collectionPage(t, `{"trigger":"hover","collectionSource":"content:order"}`)
		_, err := compileCollection(t, page, reg, f.projectID)
		if err == nil {
			t.Fatalf("未知集合源应编译失败")
		}
		if !strings.Contains(err.Error(), "content:order") || !strings.Contains(err.Error(), productcontract.CollectionSourceProduct) {
			t.Fatalf("报错应列出可用集合源: %v", err)
		}
	})
	t.Run("白名单外过滤维度", func(t *testing.T) {
		_, err := reg.ResolveCollection(ctx, productcontract.CollectionSourceProduct, map[string]string{"price": "1"})
		if err == nil {
			t.Fatalf("白名单外过滤维度应报错")
		}
		if !strings.Contains(err.Error(), productenums.ErrCollectionFilterInvalid) {
			t.Fatalf("报错应指向过滤维度校验: %v", err)
		}
	})
	t.Run("非本模块集合源", func(t *testing.T) {
		_, err := f.products.ResolveCollection(ctx, "content:article", nil)
		if err == nil {
			t.Fatalf("非本模块集合源应报错")
		}
		if !strings.Contains(err.Error(), productenums.ErrCollectionSourceInvalid) {
			t.Fatalf("报错应指向集合源标识: %v", err)
		}
	})
}

// TestProductCollectionScopedByProject 集合数据按工程隔离：
// 商品是分工程的数据，构建上下文里的工程 ID 决定取哪一批。
func TestProductCollectionScopedByProject(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉", 99, 99)
	other, err := f.projects.Create(context.Background(), &projectdto.CreateReq{Name: "另一个工程"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}
	price := 88.0
	if _, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: other.ID, Name: "别站商品", Slug: "other-product", DefaultPrice: &price,
	}); err != nil {
		t.Fatalf("创建第二个工程的商品失败: %v", err)
	}

	items, err := reg.ResolveCollection(core.WithBuildProjectID(context.Background(), f.projectID),
		productcontract.CollectionSourceProduct, nil)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(items) != 1 || items[0]["name"] != "夏季衬衫" {
		t.Fatalf("应只取本工程的商品：%+v", items)
	}
	others, err := reg.ResolveCollection(core.WithBuildProjectID(context.Background(), other.ID),
		productcontract.CollectionSourceProduct, nil)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(others) != 1 || others[0]["name"] != "别站商品" {
		t.Fatalf("应只取指定工程的商品：%+v", others)
	}
}

// TestProductCollectionFollowsBuildLanguage 商品可翻译字段随构建语言切换：
// 同一份集合数据在英文构建下输出译文（语境 product.<字段名>）。
func TestProductCollectionFollowsBuildLanguage(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	if err := f.db.Exec(
		"INSERT INTO sys_translation (source_hash, context, lang, source_text, target_text, engine) VALUES (?,?,?,?,?,'manual')",
		i18n.ContentHash("夏季衬衫"), "product.name", "en-US", "夏季衬衫", "Summer Shirt",
	).Error; err != nil {
		t.Fatalf("写入译文失败: %v", err)
	}
	f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉透气", 99, 99)
	page := collectionPage(t, collectionProps())

	zh, err := compileCollection(t, page, reg, f.projectID)
	if err != nil {
		t.Fatalf("中文编译失败: %v", err)
	}
	if !strings.Contains(zh.HTML, "夏季衬衫") {
		t.Fatalf("默认语言产物应为原文: %s", zh.HTML)
	}
	en, err := compileCollection(t, page, reg, f.projectID, builder.WithLanguage("en-US"))
	if err != nil {
		t.Fatalf("英文编译失败: %v", err)
	}
	if !strings.Contains(en.HTML, "Summer Shirt") {
		t.Fatalf("英文构建应使用译文: %s", en.HTML)
	}
}
