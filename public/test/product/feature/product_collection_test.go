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
	// 过滤维度（#21 + #25）：status（带枚举）+ 分类 / 品牌 / 标签三条 id 维度（任意值）
	// + 属性值维度（**前缀维度**：键是命名空间，真实维度是 option.<属性key>）。
	wantFilters := []string{
		productcontract.CollectionFilterStatus,
		productcontract.CollectionFilterCategoryID,
		productcontract.CollectionFilterBrandID,
		productcontract.CollectionFilterTagID,
		// issue #27：多标签（带匹配语义）与「只看在售」。
		productcontract.CollectionFilterTagIDs,
		productcontract.CollectionFilterTagMode,
		productcontract.CollectionFilterOnSale,
		// issue #28：价格区间两维（价格在变体上，EXISTS 下推）。
		productcontract.CollectionFilterMinPrice,
		productcontract.CollectionFilterMaxPrice,
		// issue #29：最低评分。
		productcontract.CollectionFilterMinRating,
		productcontract.CollectionFilterOption,
	}
	if len(found.Filters) != len(wantFilters) {
		t.Fatalf("过滤维度应为 %v，实际 %+v", wantFilters, found.Filters)
	}
	for i, key := range wantFilters {
		if found.Filters[i].Key != key {
			t.Fatalf("第 %d 个过滤维度应是 %s，实际 %+v", i, key, found.Filters)
		}
	}
	// 前缀维度必须被标出来：工作台据此渲染成「属性组多选 + 属性值多选」，
	// 而不是当成一个取值有限的普通下拉（它没有枚举，值由用户建的属性组决定）。
	last := found.Filters[len(found.Filters)-1]
	if !last.Prefix {
		t.Fatalf("属性值维度应标记为前缀维度，实际 %+v", last)
	}
	for _, filter := range found.Filters[:len(found.Filters)-1] {
		if filter.Prefix {
			t.Fatalf("等值维度不该带前缀标记：%+v", filter)
		}
	}
	if len(found.Filters[0].Enum) != 3 {
		t.Fatalf("status 维度应带 draft/published/archived 枚举：%+v", found.Filters[0])
	}
	// 除 status 与 tagMode（取值固定）之外，其余维度都是任意值，不应带枚举。
	for _, f := range found.Filters[1:] {
		if f.Key == productcontract.CollectionFilterTagMode {
			if strings.Join(f.Enum, ",") != "any,all" {
				t.Fatalf("tagMode 应带 any/all 枚举：%+v", f)
			}
			continue
		}
		if len(f.Enum) != 0 {
			t.Fatalf("id / 开关类维度是任意值，不应带枚举：%+v", f)
		}
	}
	// 排序键：确定性默认序（sort → createdAt）。
	if strings.Join(found.OrderBy, ",") != "sort,createdAt,priceAsc,priceDesc,ratingDesc" {
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

// TestProductCollectionFilterByTaxonomy 验收（issue #21）：
// 分类 / 品牌 / 标签三个维度各自与组合都能把集合收敛到正确的一批商品
// （维度彼此 AND，并与 status 叠加）。
func TestProductCollectionFilterByTaxonomy(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := core.WithBuildProjectID(context.Background(), f.projectID)
	catA := colCategory(t, f, "上衣", "tops")
	catB := colCategory(t, f, "裤装", "pants")
	brandX := colBrand(t, f, "山系", "shanshan")
	tag1 := colTag(t, f, "新品", "new-arrival")
	tag2 := colTag(t, f, "清仓", "clearance")

	p1 := f.createProduct(t, "衬衫", "shirt", "", 99, 199)
	p2 := f.createProduct(t, "长裤", "trousers", "", 199, 199)
	p3 := f.createProduct(t, "外套", "coat", "", 299, 299)
	// p1：分类 A + 品牌 X + 标签 T1；p2：分类 B + 品牌 X；p3：分类 A + 标签 T2。
	colAttach(t, f, p1, []string{catA.ID}, &brandX.ID, []string{tag1.ID})
	colAttach(t, f, p2, []string{catB.ID}, &brandX.ID, nil)
	colAttach(t, f, p3, []string{catA.ID}, nil, []string{tag2.ID})

	resolve := func(filter map[string]string) []string {
		t.Helper()
		items, err := f.products.ResolveCollection(ctx, productcontract.CollectionSourceProduct, filter)
		if err != nil {
			t.Fatalf("解析集合失败（filter=%v）: %v", filter, err)
		}
		out := make([]string, 0, len(items))
		for _, it := range items {
			slug, _ := it["slug"].(string)
			out = append(out, slug)
		}
		return out
	}
	assertHits := func(got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("命中数应为 %d（%v），实际 %d（%v）", len(want), want, len(got), got)
		}
		set := make(map[string]bool, len(got))
		for _, s := range got {
			set[s] = true
		}
		for _, w := range want {
			if !set[w] {
				t.Fatalf("应命中 %s，实际 %v", w, got)
			}
		}
	}

	// 单维度：分类 A 命中 p1 / p3。
	assertHits(resolve(map[string]string{productcontract.CollectionFilterCategoryID: catA.ID}), "shirt", "coat")
	// 单维度：品牌 X 命中 p1 / p2。
	assertHits(resolve(map[string]string{productcontract.CollectionFilterBrandID: brandX.ID}), "shirt", "trousers")
	// 单维度：标签 T2 只命中 p3。
	assertHits(resolve(map[string]string{productcontract.CollectionFilterTagID: tag2.ID}), "coat")
	// 组合：分类 A + 标签 T1 → p1（AND 语义）。
	assertHits(resolve(map[string]string{
		productcontract.CollectionFilterCategoryID: catA.ID,
		productcontract.CollectionFilterTagID:      tag1.ID,
	}), "shirt")
	// 组合：分类 A + 品牌 X → p1。
	assertHits(resolve(map[string]string{
		productcontract.CollectionFilterCategoryID: catA.ID,
		productcontract.CollectionFilterBrandID:    brandX.ID,
	}), "shirt")
	// 组合：分类 A + 标签 T2 → p3。
	assertHits(resolve(map[string]string{
		productcontract.CollectionFilterCategoryID: catA.ID,
		productcontract.CollectionFilterTagID:      tag2.ID,
	}), "coat")

	// 与 status 叠加：把 p3 置为已发布后，分类 A + published 只留 p3。
	published := productenums.StatusPublished
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: p3, Status: &published}); err != nil {
		t.Fatalf("置为已发布失败: %v", err)
	}
	assertHits(resolve(map[string]string{
		productcontract.CollectionFilterCategoryID: catA.ID,
		productcontract.CollectionFilterStatus:     productenums.StatusPublished,
	}), "coat")

	// 无命中返回空集合（不是报错）：用一个真实存在但没挂任何商品的分类。
	empty := colCategory(t, f, "空分类", "empty-category")
	if got := resolve(map[string]string{productcontract.CollectionFilterCategoryID: empty.ID}); len(got) != 0 {
		t.Fatalf("无命中应返回空集合，实际 %v", got)
	}
}

// TestProductCollectionFilterRejectsInvalidID 非法 id 形状必须在解析期报错：
// 下推到 SQL 只会得到「空集合」，那是把配置错误伪装成「这个分类下没有商品」。
func TestProductCollectionFilterRejectsInvalidID(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := core.WithBuildProjectID(context.Background(), f.projectID)
	f.createProduct(t, "衬衫", "shirt", "", 99, 199)

	for _, key := range []string{
		productcontract.CollectionFilterCategoryID,
		productcontract.CollectionFilterBrandID,
		productcontract.CollectionFilterTagID,
	} {
		t.Run(key, func(t *testing.T) {
			_, err := f.products.ResolveCollection(ctx, productcontract.CollectionSourceProduct, map[string]string{key: "not-a-uuid"})
			if err == nil {
				t.Fatalf("%s 传非法 id 应报错", key)
			}
			if !strings.Contains(err.Error(), productenums.ErrCollectionFilterInvalid) || !strings.Contains(err.Error(), key) {
				t.Fatalf("报错应指向该维度：%v", err)
			}
		})
	}

	// 空值 = 该维度不参与过滤（不是「匹配不到」）。
	items, err := f.products.ResolveCollection(ctx, productcontract.CollectionSourceProduct, map[string]string{
		productcontract.CollectionFilterCategoryID: "",
	})
	if err != nil {
		t.Fatalf("空值维度不应报错: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("空值维度应不参与过滤，实际命中 %d", len(items))
	}
}

// —— 集合源过滤测试的小工具（走真实 service，不直接写库）——

func colCategory(t *testing.T, f *detailFixture, name, slug string) *productdto.CategoryResp {
	t.Helper()
	res, err := f.products.CreateCategory(context.Background(), &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: name, Slug: slug,
	})
	if err != nil {
		t.Fatalf("建分类 %s 失败: %v", name, err)
	}
	return res
}

func colBrand(t *testing.T, f *detailFixture, name, slug string) *productdto.BrandResp {
	t.Helper()
	res, err := f.products.CreateBrand(context.Background(), &productdto.CreateBrandReq{
		ProjectID: f.projectID, Name: name, Slug: slug,
	})
	if err != nil {
		t.Fatalf("建品牌 %s 失败: %v", name, err)
	}
	return res
}

func colTag(t *testing.T, f *detailFixture, name, slug string) *productdto.TagResp {
	t.Helper()
	res, err := f.products.CreateTag(context.Background(), &productdto.CreateTagReq{
		ProjectID: f.projectID, Name: name, Slug: slug,
	})
	if err != nil {
		t.Fatalf("建标签 %s 失败: %v", name, err)
	}
	return res
}

// colAttach 把分类 / 品牌 / 标签挂到商品上（走商品更新接口，与后台同一路径）。
func colAttach(t *testing.T, f *detailFixture, productID string, categories []string, brandID *string, tags []string) {
	t.Helper()
	req := &productdto.UpdateReq{ID: productID}
	if categories != nil {
		req.CategoryIDs = categories
	}
	if brandID != nil {
		req.BrandID = brandID
	}
	if tags != nil {
		req.TagIDs = tags
	}
	if _, err := f.products.Update(context.Background(), req); err != nil {
		t.Fatalf("挂载分类 / 品牌 / 标签失败: %v", err)
	}
}

// TestProductCollectionTaxonomyFilterUsesIndex 验收（issue #21）：
// 三个维度的谓词都要能走索引 —— 分类 / 标签的 JSONB 包含走 GIN，
// 品牌走 idx_products_brand_id（081 的外键不会自动建索引，迁移 117 补的）。
//
// 小表上 planner 选 Seq Scan 是**正确**的成本选择，不能当成缺陷；因此关掉
// enable_seqscan 后再看计划：索引可用，计划里就会出现索引名。断言的是
// 「索引能支撑这个谓词」，而不是「planner 此刻偏要用它」。
func TestProductCollectionTaxonomyFilterUsesIndex(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	cat := colCategory(t, f, "上衣", "tops")
	brand := colBrand(t, f, "山系", "shanshan")
	tag := colTag(t, f, "新品", "new-arrival")

	// 谓词与 model.ListForCollection 的写法逐字一致（改一边忘另一边就测不出来了）。
	cases := []struct{ name, sql, arg, index string }{
		{"分类", "SELECT id FROM products WHERE category_ids @> jsonb_build_array(?::text)", cat.ID, "idx_products_category_ids"},
		{"标签", "SELECT id FROM products WHERE tag_ids @> jsonb_build_array(?::text)", tag.ID, "idx_products_tag_ids"},
		{"品牌", "SELECT id FROM products WHERE brand_id = ?::uuid", brand.ID, "idx_products_brand_id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertPlanUsesIndex(t, f, c.sql, c.arg, c.index)
		})
	}
}

// assertPlanUsesIndex 在「关掉顺序扫描」的事务里 EXPLAIN 一条查询，
// 断言计划里出现了期望的索引名（事务随即回滚，不留副作用）。
func assertPlanUsesIndex(t *testing.T, f *detailFixture, sql string, arg any, indexName string) {
	t.Helper()
	tx := f.db.Begin()
	if tx.Error != nil {
		t.Fatalf("开启事务失败: %v", tx.Error)
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.Exec("SET LOCAL enable_seqscan = off").Error; err != nil {
		t.Fatalf("关闭顺序扫描失败: %v", err)
	}
	rows, err := tx.Raw("EXPLAIN "+sql, arg).Rows()
	if err != nil {
		t.Fatalf("EXPLAIN 失败: %v", err)
	}
	defer func() { _ = rows.Close() }()
	plan := make([]string, 0, 8)
	for rows.Next() {
		var line string
		if serr := rows.Scan(&line); serr != nil {
			t.Fatalf("读取执行计划失败: %v", serr)
		}
		plan = append(plan, line)
	}
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, indexName) {
		t.Fatalf("该谓词应能走索引 %s，实际计划：\n%s", indexName, joined)
	}
}

// TestProductCollectionRelatedFieldsNotBlank 回归（issue #22 发现的 #9 遗留缺陷）：
// 集合项的 related / tags / imageAlt 都由 ListForCollection 的投影列派生 ——
// 漏取某一列会让对应字段**恒为空**：看起来像「这个商品没填」，实际是查询根本没取那一列。
// 这条用例挂上分类 / 品牌 / 标签并填好图集 alt 后，再断言值真的到了集合项里。
func TestProductCollectionRelatedFieldsNotBlank(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := core.WithBuildProjectID(context.Background(), f.projectID)
	pid := f.createProduct(t, "夏季衬衫", "summer-shirt", "", 99, 199)
	cat := colCategory(t, f, "上衣", "tops")
	brand := colBrand(t, f, "山系", "shanshan")
	tag := colTag(t, f, "新品", "new-arrival")
	colAttach(t, f, pid, []string{cat.ID}, &brand.ID, []string{tag.ID})
	// 图集 alt 落在 images_alt 列上（与 images 逐位对应）。
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{
		ID: pid, ImageAlts: []string{"蓝色衬衫", ""},
	}); err != nil {
		t.Fatalf("设置图集 alt 失败: %v", err)
	}

	items, err := f.products.ResolveCollection(ctx, productcontract.CollectionSourceProduct, nil)
	if err != nil {
		t.Fatalf("解析集合失败: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("应命中 1 个商品，实际 %d", len(items))
	}
	item := items[0]

	if tags, _ := item["tags"].(string); !strings.Contains(tags, "新品") {
		t.Fatalf("tags 不该为空（投影列漏取会让它恒为空）：%v", item["tags"])
	}
	related, _ := item["related"].(string)
	for _, want := range []string{"tops", "shanshan", "new-arrival"} {
		if !strings.Contains(related, want) {
			t.Fatalf("related 应含 %s，实际 %s", want, related)
		}
	}
	if alt, _ := item["imageAlt"].(string); alt != "蓝色衬衫" {
		t.Fatalf("imageAlt 应取图集首张的 alt，实际 %q", alt)
	}
}
