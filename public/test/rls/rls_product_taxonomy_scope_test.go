package rlstest

// rls_product_taxonomy_scope_test.go — product 域「反查类」入口的工程作用域护栏（DB-009 第三批补遗）。
//
// 上一批补的是「按 id 单查 / 列表 / 计数 / 唯一性判定」，这里补的是另一半 —— **反查类**入口：
// 「哪些商品用了这个属性组 / 品牌 / 分类 / 标签」「某标签命中哪些商品」。它们的语义决定了
// 工程作用域只能由**调用方**给出（作用域就是发起这次删除 / 查询的那个工程），所以这些方法
// 在这一批里都为此加了形参。
//
// 三条断言各自钉住一件事：
//
//   - 同工程作用域反查**必须命中**：这是有失败能力的那一半 —— 摘掉 model 里的
//     InProjectScope，非超级角色下这些查询会 0 行（fail closed 不报错），立刻红；
//   - 跨工程作用域反查**必须查不到**：隔离被破坏（谓词写反、作用域没生效）时红；
//   - 带作用域的入口传空 / 非法工程 id **必须在入口报 ErrInvalidProjectID**：这是
//     「先接线再切角色」的护栏 —— 若退化成裸查询，切角色后同一处会静默返回 0 行，
//     于是占用检查静默放行、删除留下悬空引用，且没有任何错误日志。
//
// 另有一条固定「显式例外入口」的形状（GetXxxWithoutScope）：它们**拿不到**工程上下文，
// 按 DB-009 的口径显式保留现状 —— 不能被作用域校验拦下，也不能假装已被隔离。
//
// 全程跑在**非超级角色**下（rlsFixture 保证并用 rls.BypassedRole 自检），否则断言全绿而无意义。

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"
)

// taxonomySeed 一行「带全部引用面」的商品及其被引用的分类学行。
type taxonomySeed struct {
	productID   string
	brandID     string
	categoryID  string
	attributeID string
	tagID       string
}

// seedTaxonomyProduct 经 model 的写入路径落一行商品 + 一行品牌 / 分类 / 属性组 / 标签，
// 并让商品把这四者都引用上（反查类断言需要真实的引用关系）。
//
// 商品与四个引用面同属一个工程：这是反查的语义前提（跨工程引用在写入期就被 service 拒了）。
func seedTaxonomyProduct(t *testing.T, db *gorm.DB, projectID, name string) taxonomySeed {
	t.Helper()
	ctx := context.Background()
	m := productmodel.NewModel(db)
	now := time.Now()

	s := taxonomySeed{
		productID:   uuid.NewString(),
		brandID:     uuid.NewString(),
		categoryID:  uuid.NewString(),
		attributeID: uuid.NewString(),
		tagID:       uuid.NewString(),
	}

	if err := m.CreateBrand(ctx, &productmodel.ProductBrandEntity{
		ID: s.brandID, ProjectID: projectID, Name: name + " 品牌", Slug: "brand-" + s.brandID,
		Logo: "", Description: "", SEOTitle: "", SEODescription: "", Sort: 0,
		Metadata: []byte("{}"), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("写入品牌失败（RLS 生效时写入必须经 InProjectScope）: %v", err)
	}
	if err := m.CreateCategory(ctx, &productmodel.ProductCategoryEntity{
		ID: s.categoryID, ProjectID: projectID, Name: name + " 分类", Slug: "cat-" + s.categoryID,
		Description: "", Image: "", SEOTitle: "", SEODescription: "", Sort: 0,
		Metadata: []byte("{}"), CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("写入分类失败: %v", err)
	}
	if err := m.CreateAttribute(ctx, &productmodel.ProductAttributeEntity{
		ID: s.attributeID, ProjectID: projectID, Key: "attr-" + s.attributeID, Name: name + " 属性",
		IsVariation: false, Sort: 0, Values: []byte("[]"), Metadata: []byte("{}"),
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("写入属性组失败: %v", err)
	}
	if err := m.CreateTag(ctx, &productmodel.ProductTagEntity{
		ID: s.tagID, ProjectID: projectID, Name: name + " 标签", Slug: "tag-" + s.tagID,
		Kind: "manual", RuleType: "", RuleParams: []byte("{}"), Metadata: []byte("{}"),
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("写入标签失败: %v", err)
	}

	attrs, _ := json.Marshal([]string{s.attributeID})
	cats, _ := json.Marshal([]string{s.categoryID})
	tags, _ := json.Marshal([]string{s.tagID})
	if err := m.CreateWithVariants(ctx, &productmodel.ProductEntity{
		ID: s.productID, ProjectID: projectID, Name: name, Slug: "tax-" + s.productID,
		Status: productenums.StatusDraft, BrandID: &s.brandID, PrimaryCategoryID: &s.categoryID,
		Type:        productmodel.TypeVariant, // 同上：空串会撞 products_type_check，必须显式给
		Description: []byte("{}"), Images: []byte("[]"), ImageAlts: []byte("[]"),
		AttributeIDs: attrs, CategoryIDs: cats, TagIDs: tags, RelatedIDs: []byte("[]"),
		Metadata: []byte("{}"), BundleItems: []byte("{\"options\":[]}"),
		CreatedAt: now, UpdatedAt: now,
	}, nil); err != nil {
		t.Fatalf("写入商品失败: %v", err)
	}
	return s
}

// TestRLS_ProductTaxonomyScope_ReverseLookupsScopedToProject 反查类入口只在工程作用域内命中。
//
// 每一类都断言两次：**本工程作用域命中**（证明作用域被正确设置，摘掉 InProjectScope 即红）
// 与**跨工程作用域查不到**（证明隔离真的生效，而不是「碰巧没有这个 id」）。
func TestRLS_ProductTaxonomyScope_ReverseLookupsScopedToProject(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)

	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")
	// 工程 A 也有自己的商品与引用面：这样「跨工程查不到」就不会被误读成
	// 「A 工程根本没有数据」—— 同一个查询在 A 的作用域下必须只看到 A 自己的商品。
	a := seedTaxonomyProduct(t, db, pA, "工程 A 的商品")
	b := seedTaxonomyProduct(t, db, pB, "工程 B 的商品")

	// 1) 品牌反查。
	if hit, err := pm.ProductUsingBrand(ctx, b.brandID, pB); err != nil || hit == nil || hit.ID != b.productID {
		t.Fatalf("本工程作用域反查品牌应命中 B 的商品，实际 err=%v hit=%+v", err, hit)
	}
	if _, err := pm.ProductUsingBrand(ctx, b.brandID, pA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("跨工程作用域反查品牌应查不到（隔离生效），实际 %v", err)
	}

	// 2) 分类反查（两个引用面：category_ids 与 primary_category_id）。
	if hit, err := pm.ProductUsingCategory(ctx, b.categoryID, pB); err != nil || hit == nil || hit.ID != b.productID {
		t.Fatalf("本工程作用域反查分类应命中 B 的商品，实际 err=%v hit=%+v", err, hit)
	}
	if _, err := pm.ProductUsingCategory(ctx, b.categoryID, pA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("跨工程作用域反查分类应查不到，实际 %v", err)
	}

	// 3) 属性组反查（jsonb 包含谓词）。
	if hit, err := pm.ProductUsingAttribute(ctx, b.attributeID, pB); err != nil || hit == nil || hit.ID != b.productID {
		t.Fatalf("本工程作用域反查属性组应命中 B 的商品，实际 err=%v hit=%+v", err, hit)
	}
	if _, err := pm.ProductUsingAttribute(ctx, b.attributeID, pA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("跨工程作用域反查属性组应查不到，实际 %v", err)
	}

	// 4) 标签反查（列表 + 计数）。
	rows, err := pm.ListProductsByTag(ctx, b.tagID, pB, 0)
	if err != nil {
		t.Fatalf("本工程作用域按标签取商品失败: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != b.productID {
		t.Fatalf("本工程作用域按标签取商品应只命中 B 的那一行，实际 %d 行", len(rows))
	}
	if cross, err := pm.ListProductsByTag(ctx, b.tagID, pA, 0); err != nil {
		t.Fatalf("跨工程作用域按标签取商品失败: %v", err)
	} else if len(cross) != 0 {
		t.Errorf("跨工程作用域按标签取商品应 0 行，实际 %d 行", len(cross))
	}
	if n, err := pm.CountProductsByTag(ctx, b.tagID, pB); err != nil || n != 1 {
		t.Fatalf("本工程作用域按标签计数应为 1，实际 n=%d err=%v", n, err)
	}
	if n, err := pm.CountProductsByTag(ctx, b.tagID, pA); err != nil || n != 0 {
		t.Errorf("跨工程作用域按标签计数应为 0，实际 n=%d err=%v", n, err)
	}

	// 5) 批量按 id 取商品（捆绑引用校验 / 工程过滤都靠它）。
	rows, err = pm.ListProductsByIDs(ctx, []string{b.productID, a.productID}, pB)
	if err != nil {
		t.Fatalf("本工程作用域批量取商品失败: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != b.productID {
		t.Fatalf("本工程作用域批量取商品应只返回 B 的行（A 的行对 B 不可见），实际 %d 行", len(rows))
	}
	if rows, err = pm.ListProductsByIDs(ctx, []string{b.productID}, pA); err != nil {
		t.Fatalf("跨工程作用域批量取商品失败: %v", err)
	} else if len(rows) != 0 {
		t.Errorf("跨工程作用域批量取商品应 0 行，实际 %d 行", len(rows))
	}

	// 6) 只读一列（products.attribute_ids）。
	raw, err := pm.ListProductAttributeIDs(ctx, b.productID, pB)
	if err != nil {
		t.Fatalf("本工程作用域读 attribute_ids 失败: %v", err)
	}
	if string(raw) != string(func() []byte { v, _ := json.Marshal([]string{b.attributeID}); return v }()) {
		t.Errorf("attribute_ids 应含 B 自己的属性组 id，实际 %s", string(raw))
	}
	if _, err = pm.ListProductAttributeIDs(ctx, b.productID, pA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("跨工程作用域读 attribute_ids 应查不到，实际 %v", err)
	}

	// 7) 新品规则的取数来源：先把 B 的商品置为上架，再按时间窗取 id。
	row, gerr := pm.Get(ctx, b.productID, pB)
	if gerr != nil {
		t.Fatalf("读 B 的商品失败: %v", gerr)
	}
	since := time.Now().UTC().Add(-time.Hour)
	publishedAt := time.Now().UTC()
	row.Status, row.PublishedAt, row.UpdatedAt = productenums.StatusPublished, &publishedAt, publishedAt
	if uerr := pm.Update(ctx, row); uerr != nil {
		t.Fatalf("置为上架失败: %v", uerr)
	}
	ids, err := pm.ListProductIDsPublishedSince(ctx, pB, productenums.StatusPublished, since)
	if err != nil {
		t.Fatalf("本工程作用域取已上架商品 id 失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != b.productID {
		t.Fatalf("本工程作用域应取到 B 的已上架商品，实际 %v", ids)
	}
	if ids, err = pm.ListProductIDsPublishedSince(ctx, pA, productenums.StatusPublished, since); err != nil {
		t.Fatalf("跨工程作用域取已上架商品 id 失败: %v", err)
	} else if len(ids) != 0 {
		t.Errorf("跨工程作用域应取不到已上架商品，实际 %v", ids)
	}
}

// TestRLS_ProductTaxonomyScope_ScopedEntrypointsRejectEmptyProject 带作用域的入口在工程 id
// 缺失 / 非法时**报错**，而不是静默返回 0 行。
//
// 这是本组方法统一的口径：作用域是必填参数，空串会被 rls 在入口挡掉（ErrInvalidProjectID）。
// 若某处将来退化成裸查询（m.DB(ctx) 直查），切到非超级角色后同一处会静默 0 行 ——
// 占用检查放行、标签列表计数全为 0、商品名整列为空，且没有任何错误日志。
func TestRLS_ProductTaxonomyScope_ScopedEntrypointsRejectEmptyProject(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)

	pA := uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	a := seedTaxonomyProduct(t, db, pA, "工程 A 的商品")
	since := time.Now().UTC().Add(-time.Hour)

	cases := []struct {
		name string
		run  func(projectID string) error
	}{
		{"ProductUsingBrand", func(pid string) error { _, err := pm.ProductUsingBrand(ctx, a.brandID, pid); return err }},
		{"ProductUsingCategory", func(pid string) error { _, err := pm.ProductUsingCategory(ctx, a.categoryID, pid); return err }},
		{"ProductUsingAttribute", func(pid string) error { _, err := pm.ProductUsingAttribute(ctx, a.attributeID, pid); return err }},
		{"ListProductsByTag", func(pid string) error { _, err := pm.ListProductsByTag(ctx, a.tagID, pid, 0); return err }},
		{"CountProductsByTag", func(pid string) error { _, err := pm.CountProductsByTag(ctx, a.tagID, pid); return err }},
		{"ListProductsByIDs", func(pid string) error { _, err := pm.ListProductsByIDs(ctx, []string{a.productID}, pid); return err }},
		{"ListProductAttributeIDs", func(pid string) error { _, err := pm.ListProductAttributeIDs(ctx, a.productID, pid); return err }},
		{"ListProductIDsPublishedSince", func(pid string) error {
			_, err := pm.ListProductIDsPublishedSince(ctx, pid, productenums.StatusPublished, since)
			return err
		}},
	}

	for _, c := range cases {
		if err := c.run(""); !errors.Is(err, rls.ErrInvalidProjectID) {
			t.Errorf("%s 传空工程 id 应返回 ErrInvalidProjectID（在入口挡掉，不是静默 0 行），实际 %v", c.name, err)
		}
		// 非 uuid 同样被拒：策略谓词里有 ::uuid 强转，放行会让 PG 抛「类型转换失败」，
		// 错误归属从「入参错」变成「像数据库故障」。
		if err := c.run("not-a-uuid"); !errors.Is(err, rls.ErrInvalidProjectID) {
			t.Errorf("%s 传非法工程 id 应返回 ErrInvalidProjectID，实际 %v", c.name, err)
		}
	}

	// 对照：给对工程 id 就正常 —— 上面的红不是「这些方法一律报错」。
	if hit, err := pm.ProductUsingBrand(ctx, a.brandID, pA); err != nil || hit == nil {
		t.Fatalf("给对工程 id 时应正常命中，实际 err=%v", err)
	}
}

// TestRLS_ProductTaxonomyScope_ExplicitExceptionsUnaffected 显式例外入口不受作用域校验影响。
//
// product 域有一类入口**结构上拿不到工程上下文**（presentation 的 renderHTML 在
// builder.Compile 之前就调它们，那时 ctx 里还没有工程 id），按 DB-009 的口径它们被显式
// 命名成 GetXxxWithoutScope 并保留现状。这条断言把那个「现状」的**形状**固定住：
//
//   - 不能被作用域校验拦下（报 ErrInvalidProjectID = 把「拿不到工程上下文」换成了
//     一个更难懂的错误，而不是诚实地表达「这条路径没有隔离」）；
//   - 在非超级角色下必须 fail closed（0 行 ⇒ ErrRecordNotFound）：这是换角色后
//     那批路径的真实表现，也是「presentation 侧要补 WithBuildProjectID」这条待办的依据。
func TestRLS_ProductTaxonomyScope_ExplicitExceptionsUnaffected(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)

	pA := uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	a := seedTaxonomyProduct(t, db, pA, "工程 A 的商品")

	exceptions := []struct {
		name string
		run  func() error
	}{
		{"GetWithoutScope", func() error { _, err := pm.GetWithoutScope(ctx, a.productID); return err }},
		{"GetAttributeWithoutScope", func() error { _, err := pm.GetAttributeWithoutScope(ctx, a.attributeID); return err }},
		{"GetBrandWithoutScope", func() error { _, err := pm.GetBrandWithoutScope(ctx, a.brandID); return err }},
		{"GetCategoryWithoutScope", func() error { _, err := pm.GetCategoryWithoutScope(ctx, a.categoryID); return err }},
		{"GetTagWithoutScope", func() error { _, err := pm.GetTagWithoutScope(ctx, a.tagID); return err }},
	}
	for _, c := range exceptions {
		err := c.run()
		if errors.Is(err, rls.ErrInvalidProjectID) {
			t.Errorf("%s 是显式例外入口（拿不到工程上下文），不应被作用域校验拦下，实际 %v", c.name, err)
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Errorf("%s 在非超级角色下应 fail closed（ErrRecordNotFound），实际 %v —— 例外口径被悄悄改掉了？", c.name, err)
		}
	}
}
