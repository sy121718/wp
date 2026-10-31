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

func collectionPage(t *testing.T, props string) *builder.Page {
	t.Helper()
	raw := `{"settings":{"layout":{"mode":"boxed","maxWidth":"1200px"}},"root":[{"id":"n1","type":"core.cardstack","props":` + props + `}]}`
	p, err := builder.ParsePage([]byte(raw))
	if err != nil {
		t.Fatalf("解析页面文档失败: %v", err)
	}
	return p
}

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

func collectionProps() string {
	return `{"trigger":"hover","shape":"line","collectionSource":"content:product","collectionLimit":4,` +
		`"cardTitleField":"name","cardImageField":"images","cardMetaField":"priceRange",` +
		`"cardLinkField":"slug","cardLinkPrefix":"/products/"}`
}

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

	wantFilters := []string{
		productcontract.CollectionFilterStatus,
		productcontract.CollectionFilterCategoryID,
		productcontract.CollectionFilterCategoryIDs,
		productcontract.CollectionFilterCategoryMode,
		productcontract.CollectionFilterBrandID,
		productcontract.CollectionFilterBrandIDs,
		productcontract.CollectionFilterBrandMode,
		productcontract.CollectionFilterTagID,

		productcontract.CollectionFilterTagIDs,
		productcontract.CollectionFilterTagMode,
		productcontract.CollectionFilterOnSale,

		productcontract.CollectionFilterMinPrice,
		productcontract.CollectionFilterMaxPrice,

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

	for _, filter := range found.Filters[1:] {
		switch filter.Key {
		case productcontract.CollectionFilterCategoryMode, productcontract.CollectionFilterBrandMode, productcontract.CollectionFilterTagMode:
			if strings.Join(filter.Enum, ",") != "any,all" {
				t.Fatalf("%s 应带 any/all 枚举：%+v", filter.Key, filter)
			}
		default:
			if len(filter.Enum) != 0 {
				t.Fatalf("id / 开关 / 数值维度是任意值，不应带枚举：%+v", filter)
			}
		}
	}

	if strings.Join(found.OrderBy, ",") != "sort,createdAt,priceAsc,priceDesc,ratingDesc" {
		t.Fatalf("排序键白名单不符：%v", found.OrderBy)
	}

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

	if first["priceRange"] != "99 ~ 199" {
		t.Fatalf("价格区间应由变体派生：%+v", first["priceRange"])
	}

	images, ok := first["images"].([]any)
	if !ok || len(images) != 2 || images[0] != testMediaBaseURL+"/summer-shirt-1.jpg" {
		t.Fatalf("images 应为 URL 数组：%+v", first["images"])
	}

	if _, leaked := first["status"]; leaked {
		t.Fatalf("集合项不应夹带白名单外字段：%+v", first)
	}

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
		"夏季衬衫", "冬季外套",
		"99 ~ 199", "299",
		testMediaBaseURL + "/summer-shirt-1.jpg",
		"/products/summer-shirt",
		"sky-cardstack-card",
	} {
		if !strings.Contains(compiled.HTML, want) {
			t.Fatalf("构建产物应含 %q", want)
		}
	}

	for _, unwanted := range []string{"content:product", "cardTitleField", "collectionSource"} {
		if strings.Contains(compiled.HTML, unwanted) {
			t.Fatalf("产物不应残留 %q", unwanted)
		}
	}

	again, err := compileCollection(t, page, reg, f.projectID)
	if err != nil {
		t.Fatalf("二次编译失败: %v", err)
	}
	if again.HTML != compiled.HTML || again.CSS != compiled.CSS {
		t.Fatalf("集合卡产物不确定：同输入两次编译字节不一致")
	}
}

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

	assertHits(resolve(map[string]string{productcontract.CollectionFilterCategoryID: catA.ID}), "shirt", "coat")

	assertHits(resolve(map[string]string{productcontract.CollectionFilterBrandID: brandX.ID}), "shirt", "trousers")

	assertHits(resolve(map[string]string{productcontract.CollectionFilterTagID: tag2.ID}), "coat")

	assertHits(resolve(map[string]string{
		productcontract.CollectionFilterCategoryID: catA.ID,
		productcontract.CollectionFilterTagID:      tag1.ID,
	}), "shirt")

	assertHits(resolve(map[string]string{
		productcontract.CollectionFilterCategoryID: catA.ID,
		productcontract.CollectionFilterBrandID:    brandX.ID,
	}), "shirt")

	assertHits(resolve(map[string]string{
		productcontract.CollectionFilterCategoryID: catA.ID,
		productcontract.CollectionFilterTagID:      tag2.ID,
	}), "coat")

	published := productenums.StatusPublished
	if _, err := f.products.Update(ctx, &productdto.UpdateReq{ID: p3, Status: &published}); err != nil {
		t.Fatalf("置为已发布失败: %v", err)
	}
	assertHits(resolve(map[string]string{
		productcontract.CollectionFilterCategoryID: catA.ID,
		productcontract.CollectionFilterStatus:     productenums.StatusPublished,
	}), "coat")

	empty := colCategory(t, f, "空分类", "empty-category")
	if got := resolve(map[string]string{productcontract.CollectionFilterCategoryID: empty.ID}); len(got) != 0 {
		t.Fatalf("无命中应返回空集合，实际 %v", got)
	}
}

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

func TestProductCollectionTaxonomyFilterUsesIndex(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	cat := colCategory(t, f, "上衣", "tops")
	brand := colBrand(t, f, "山系", "shanshan")
	tag := colTag(t, f, "新品", "new-arrival")

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
