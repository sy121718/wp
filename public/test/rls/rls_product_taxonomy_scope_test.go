package rlstest

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"
)

type taxonomySeed struct {
	productID   string
	brandID     string
	categoryID  string
	attributeID string
	tagID       string
}

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
		Description: []byte("{}"), Images: []byte("[]"), ImageAlts: []byte("[]"),
		AttributeIDs: attrs, CategoryIDs: cats, TagIDs: tags, RelatedIDs: []byte("[]"),
		Metadata:  []byte("{}"),
		CreatedAt: now, UpdatedAt: now,
	}, nil); err != nil {
		t.Fatalf("写入商品失败: %v", err)
	}
	return s
}

func TestRLS_ProductTaxonomyScope_ReverseLookupsScopedToProject(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pm := productmodel.NewModel(db)

	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")

	a := seedTaxonomyProduct(t, db, pA, "工程 A 的商品")
	b := seedTaxonomyProduct(t, db, pB, "工程 B 的商品")

	if hit, err := pm.ProductUsingBrand(ctx, b.brandID, pB); err != nil || hit == nil || hit.ID != b.productID {
		t.Fatalf("本工程作用域反查品牌应命中 B 的商品，实际 err=%v hit=%+v", err, hit)
	}
	if _, err := pm.ProductUsingBrand(ctx, b.brandID, pA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("跨工程作用域反查品牌应查不到（隔离生效），实际 %v", err)
	}

	if hit, err := pm.ProductUsingCategory(ctx, b.categoryID, pB); err != nil || hit == nil || hit.ID != b.productID {
		t.Fatalf("本工程作用域反查分类应命中 B 的商品，实际 err=%v hit=%+v", err, hit)
	}
	if _, err := pm.ProductUsingCategory(ctx, b.categoryID, pA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("跨工程作用域反查分类应查不到，实际 %v", err)
	}

	if hit, err := pm.ProductUsingAttribute(ctx, b.attributeID, pB); err != nil || hit == nil || hit.ID != b.productID {
		t.Fatalf("本工程作用域反查属性组应命中 B 的商品，实际 err=%v hit=%+v", err, hit)
	}
	if _, err := pm.ProductUsingAttribute(ctx, b.attributeID, pA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("跨工程作用域反查属性组应查不到，实际 %v", err)
	}

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

		if err := c.run("not-a-uuid"); !errors.Is(err, rls.ErrInvalidProjectID) {
			t.Errorf("%s 传非法工程 id 应返回 ErrInvalidProjectID，实际 %v", c.name, err)
		}
	}

	if hit, err := pm.ProductUsingBrand(ctx, a.brandID, pA); err != nil || hit == nil {
		t.Fatalf("给对工程 id 时应正常命中，实际 err=%v", err)
	}
}

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
