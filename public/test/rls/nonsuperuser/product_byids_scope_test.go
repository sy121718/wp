package nonsuperuser

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"
	"go_wp/public/test/support"
)

func seedRow(t *testing.T, admin *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := admin.Exec(sql, args...).Error; err != nil {
		t.Fatalf("播种失败（%s）：%v", sql, err)
	}
}

func assertOwnOnly[T any](t *testing.T, rows []T, err error, wantID, wantProject, label string,
	idOf func(T) string, projectOf func(T) string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s 读取失败：%v", label, err)
	}
	if len(rows) != 1 {
		t.Fatalf("%s 应只返回本工程的 1 行（另一工程的 id 必须不可见），实际 %d 行", label, len(rows))
	}
	if got := idOf(rows[0]); got != wantID {
		t.Fatalf("%s 返回的应是本工程的 %s，实际 %s", label, wantID, got)
	}
	if projectOf != nil {
		if got := projectOf(rows[0]); got != wantProject {
			t.Fatalf("%s 返回行的 project_id 应为 %s，实际 %s", label, wantProject, got)
		}
	}
}

func TestListByIDsMethods_AreProjectScoped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	pA, pB := uuid.NewString(), uuid.NewString()
	support.SeedProjectRow(t, f.admin, pA, "工程 A")
	support.SeedProjectRow(t, f.admin, pB, "工程 B")
	m := productmodel.NewModel(f.app)

	catA, catB := uuid.NewString(), uuid.NewString()
	seedRow(t, f.admin, "INSERT INTO product_categories (id, project_id, name, slug) VALUES (?,?,?,?)", catA, pA, "A 分类", "a-cat")
	seedRow(t, f.admin, "INSERT INTO product_categories (id, project_id, name, slug) VALUES (?,?,?,?)", catB, pB, "B 分类", "b-cat")
	tagA, tagB := uuid.NewString(), uuid.NewString()
	seedRow(t, f.admin, "INSERT INTO product_tags (id, project_id, name, slug) VALUES (?,?,?,?)", tagA, pA, "A 标签", "a-tag")
	seedRow(t, f.admin, "INSERT INTO product_tags (id, project_id, name, slug) VALUES (?,?,?,?)", tagB, pB, "B 标签", "b-tag")
	brandA, brandB := uuid.NewString(), uuid.NewString()
	seedRow(t, f.admin, "INSERT INTO product_brands (id, project_id, name, slug) VALUES (?,?,?,?)", brandA, pA, "A 品牌", "a-brand2")
	seedRow(t, f.admin, "INSERT INTO product_brands (id, project_id, name, slug) VALUES (?,?,?,?)", brandB, pB, "B 品牌", "b-brand2")
	attrA, attrB := uuid.NewString(), uuid.NewString()
	seedRow(t, f.admin, "INSERT INTO product_attributes (id, project_id, key, name) VALUES (?,?,?,?)", attrA, pA, "a-color", "A 颜色")
	seedRow(t, f.admin, "INSERT INTO product_attributes (id, project_id, key, name) VALUES (?,?,?,?)", attrB, pB, "b-color", "B 颜色")
	prodA, prodB := uuid.NewString(), uuid.NewString()
	seedRow(t, f.admin, "INSERT INTO products (id, project_id, name, slug) VALUES (?,?,?,?)", prodA, pA, "A 商品", "a-prod")
	seedRow(t, f.admin, "INSERT INTO products (id, project_id, name, slug) VALUES (?,?,?,?)", prodB, pB, "B 商品", "b-prod")
	varA, varB := uuid.NewString(), uuid.NewString()
	seedRow(t, f.admin, "INSERT INTO product_variants (id, product_id, sku_code) VALUES (?,?,?)", varA, prodA, "SKU-A")
	seedRow(t, f.admin, "INSERT INTO product_variants (id, product_id, sku_code) VALUES (?,?,?)", varB, prodB, "SKU-B")

	both := func(idA, idB string) []string { return []string{idA, idB} }

	t.Run("ListCategoriesByIDs", func(t *testing.T) {
		rows, err := m.ListCategoriesByIDs(ctx, both(catA, catB), pA)
		assertOwnOnly(t, rows, err, catA, pA, "作用域 A",
			func(e *productmodel.ProductCategoryEntity) string { return e.ID },
			func(e *productmodel.ProductCategoryEntity) string { return e.ProjectID })
		rows, err = m.ListCategoriesByIDs(ctx, both(catA, catB), pB)
		assertOwnOnly(t, rows, err, catB, pB, "作用域 B",
			func(e *productmodel.ProductCategoryEntity) string { return e.ID },
			func(e *productmodel.ProductCategoryEntity) string { return e.ProjectID })
	})

	t.Run("ListTagsByIDs", func(t *testing.T) {
		rows, err := m.ListTagsByIDs(ctx, both(tagA, tagB), pA)
		assertOwnOnly(t, rows, err, tagA, pA, "作用域 A",
			func(e *productmodel.ProductTagEntity) string { return e.ID },
			func(e *productmodel.ProductTagEntity) string { return e.ProjectID })
		rows, err = m.ListTagsByIDs(ctx, both(tagA, tagB), pB)
		assertOwnOnly(t, rows, err, tagB, pB, "作用域 B",
			func(e *productmodel.ProductTagEntity) string { return e.ID },
			func(e *productmodel.ProductTagEntity) string { return e.ProjectID })
	})

	t.Run("ListBrandsByIDs", func(t *testing.T) {
		rows, err := m.ListBrandsByIDs(ctx, both(brandA, brandB), pA)
		assertOwnOnly(t, rows, err, brandA, pA, "作用域 A",
			func(e *productmodel.ProductBrandEntity) string { return e.ID },
			func(e *productmodel.ProductBrandEntity) string { return e.ProjectID })
		rows, err = m.ListBrandsByIDs(ctx, both(brandA, brandB), pB)
		assertOwnOnly(t, rows, err, brandB, pB, "作用域 B",
			func(e *productmodel.ProductBrandEntity) string { return e.ID },
			func(e *productmodel.ProductBrandEntity) string { return e.ProjectID })
	})

	t.Run("ListAttributesByIDs", func(t *testing.T) {
		rows, err := m.ListAttributesByIDs(ctx, both(attrA, attrB), pA)
		assertOwnOnly(t, rows, err, attrA, pA, "作用域 A",
			func(e *productmodel.ProductAttributeEntity) string { return e.ID },
			func(e *productmodel.ProductAttributeEntity) string { return e.ProjectID })
		rows, err = m.ListAttributesByIDs(ctx, both(attrA, attrB), pB)
		assertOwnOnly(t, rows, err, attrB, pB, "作用域 B",
			func(e *productmodel.ProductAttributeEntity) string { return e.ID },
			func(e *productmodel.ProductAttributeEntity) string { return e.ProjectID })
	})

	t.Run("ListVariantsByIDs", func(t *testing.T) {

		rows, err := m.ListVariantsByIDs(ctx, both(varA, varB), pA)
		if err != nil {
			t.Fatalf("读变体失败：%v", err)
		}
		if len(rows) != 2 {
			t.Fatalf("变体表当前无策略、作用域对它不构成过滤（应返回 2 行），实际 %d 行", len(rows))
		}

		owners, err := m.ListProductsByIDs(ctx, []string{prodA, prodB}, pA)
		if err != nil {
			t.Fatalf("解析变体归属商品失败：%v", err)
		}
		if len(owners) != 1 || owners[0].ID != prodA {
			t.Fatalf("作用域 A 下按归属商品解析应只拿到工程 A 的商品，实际 %+v", owners)
		}
	})

	pOther := uuid.NewString()
	support.SeedProjectRow(t, f.admin, pOther, "工程 C")
	if rows, err := m.ListCategoriesByIDs(ctx, both(catA, catB), pOther); err != nil || len(rows) != 0 {
		t.Fatalf("作用域 C 读分类应 0 行，实际 %d 行（err=%v）", len(rows), err)
	}
	if rows, err := m.ListTagsByIDs(ctx, both(tagA, tagB), pOther); err != nil || len(rows) != 0 {
		t.Fatalf("作用域 C 读标签应 0 行，实际 %d 行（err=%v）", len(rows), err)
	}
	if rows, err := m.ListBrandsByIDs(ctx, both(brandA, brandB), pOther); err != nil || len(rows) != 0 {
		t.Fatalf("作用域 C 读品牌应 0 行，实际 %d 行（err=%v）", len(rows), err)
	}
	if rows, err := m.ListAttributesByIDs(ctx, both(attrA, attrB), pOther); err != nil || len(rows) != 0 {
		t.Fatalf("作用域 C 读属性组应 0 行，实际 %d 行（err=%v）", len(rows), err)
	}
}

func seedFactProducts(t *testing.T, admin *gorm.DB) (pA, pB, prodA, prodB string) {
	t.Helper()
	pA, pB = uuid.NewString(), uuid.NewString()
	support.SeedProjectRow(t, admin, pA, "窄投影工程 A")
	support.SeedProjectRow(t, admin, pB, "窄投影工程 B")
	prodA, prodB = uuid.NewString(), uuid.NewString()
	seedRow(t, admin, "INSERT INTO products (id, project_id, name, slug, status) VALUES (?,?,?,?,'published')", prodA, pA, "A 窄投影商品", "a-fact")
	seedRow(t, admin, "INSERT INTO products (id, project_id, name, slug, status) VALUES (?,?,?,?,'published')", prodB, pB, "B 窄投影商品", "b-fact")
	return pA, pB, prodA, prodB
}

func TestNarrowFacts_AreProjectScoped(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	pA, pB, prodA, prodB := seedFactProducts(t, f.admin)
	m := productmodel.NewModel(f.app)

	singles := []struct {
		name string
		read func(projectID, id string) (*productmodel.ProductEntity, error)
	}{
		{"GetRefFacts", func(pid, id string) (*productmodel.ProductEntity, error) { return m.GetRefFacts(ctx, id, pid) }},
		{"GetSummaryFacts", func(pid, id string) (*productmodel.ProductEntity, error) { return m.GetSummaryFacts(ctx, id, pid) }},
		{"GetPublicOptionFacts", func(pid, id string) (*productmodel.ProductEntity, error) {
			return m.GetPublicOptionFacts(ctx, id, pid)
		}},
	}
	for _, s := range singles {
		row, err := s.read(pA, prodA)
		if err != nil || row == nil || row.ID != prodA || row.ProjectID != pA {
			t.Fatalf("%s 作用域 A 读本工程商品应命中，实际 %+v / %v", s.name, row, err)
		}

		if _, err := s.read(pA, prodB); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("%s 作用域 A 点名 B 的商品应 ErrRecordNotFound，实际 %v", s.name, err)
		}

		if row, err := s.read(pB, prodB); err != nil || row == nil || row.ID != prodB {
			t.Fatalf("%s 作用域 B 读自己的商品应命中，实际 %+v / %v", s.name, row, err)
		}
	}

	batches := []struct {
		name string
		read func(projectID string, ids []string) ([]string, error)
	}{
		{"ListStatusFactsByIDs", func(pid string, ids []string) ([]string, error) {
			rows, err := m.ListStatusFactsByIDs(ctx, ids, pid)
			return factIDs(rows, pid, err)
		}},
		{"ListNameFactsByIDs", func(pid string, ids []string) ([]string, error) {
			rows, err := m.ListNameFactsByIDs(ctx, ids, pid)
			return factIDs(rows, pid, err)
		}},
		{"ListIDFactsByIDs", func(pid string, ids []string) ([]string, error) {
			rows, err := m.ListIDFactsByIDs(ctx, ids, pid)
			return factIDs(rows, pid, err)
		}},
	}
	for _, b := range batches {
		both := []string{prodA, prodB}
		got, err := b.read(pA, both)
		if err != nil {
			t.Fatalf("%s 作用域 A 混合入参读取失败：%v", b.name, err)
		}
		if len(got) != 1 || got[0] != prodA {
			t.Fatalf("%s 作用域 A 混合入参应只命中 prodA（%s），实际 %v", b.name, prodA, got)
		}

		got, err = b.read(pA, []string{prodB})
		if err != nil || len(got) != 0 {
			t.Fatalf("%s 作用域 A 点名 B 的商品应空集，实际 %v / %v", b.name, got, err)
		}

		got, err = b.read(pB, both)
		if err != nil || len(got) != 1 || got[0] != prodB {
			t.Fatalf("%s 作用域 B 混合入参应只命中 prodB（%s），实际 %v / %v", b.name, prodB, got, err)
		}
	}
}

func factIDs(rows []*productmodel.ProductEntity, projectID string, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row == nil {
			continue
		}
		if row.ProjectID != projectID {
			return nil, fmt.Errorf("读到工程 %q 的行（作用域 %q）", row.ProjectID, projectID)
		}
		ids = append(ids, row.ID)
	}
	return ids, nil
}

func TestListByIDsMethods_RejectEmptyProject(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := productmodel.NewModel(f.app)
	ids := []string{uuid.NewString()}

	if _, err := m.ListCategoriesByIDs(ctx, ids, ""); !errorsIsInvalidProjectID(err) {
		t.Fatalf("空工程 id 应返回 rls.ErrInvalidProjectID，实际 %v", err)
	}
	if _, err := m.ListTagsByIDs(ctx, ids, ""); !errorsIsInvalidProjectID(err) {
		t.Fatalf("空工程 id 应返回 rls.ErrInvalidProjectID，实际 %v", err)
	}
	if _, err := m.ListBrandsByIDs(ctx, ids, ""); !errorsIsInvalidProjectID(err) {
		t.Fatalf("空工程 id 应返回 rls.ErrInvalidProjectID，实际 %v", err)
	}
	if _, err := m.ListAttributesByIDs(ctx, ids, ""); !errorsIsInvalidProjectID(err) {
		t.Fatalf("空工程 id 应返回 rls.ErrInvalidProjectID，实际 %v", err)
	}
	if _, err := m.ListVariantsByIDs(ctx, ids, ""); !errorsIsInvalidProjectID(err) {
		t.Fatalf("空工程 id 应返回 rls.ErrInvalidProjectID，实际 %v", err)
	}
}

func errorsIsInvalidProjectID(err error) bool {
	return err != nil && errors.Is(err, rls.ErrInvalidProjectID)
}

func TestLookupIDOwners_IsScopePredicated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	pA, pB, prodA, prodB := seedFactProducts(t, f.admin)
	pC, prodC := uuid.NewString(), uuid.NewString()
	support.SeedProjectRow(t, f.admin, pC, "归属诊断工程 C")
	seedRow(t, f.admin, "INSERT INTO products (id, project_id, name, slug, status) VALUES (?,?,?,?,'published')", prodC, pC, "C 商品", "c-fact")
	m := productmodel.NewModel(f.app)
	ghost := uuid.NewString()

	owners, err := m.LookupIDOwners(ctx, []string{pA}, []string{prodA, prodB, prodC, ghost})
	if err != nil {
		t.Fatalf("归属诊断失败：%v", err)
	}
	if len(owners) != 1 || owners[prodA] != pA {
		t.Fatalf("清单只有 A 时应只回答 A 的商品，实际 %v", owners)
	}

	owners, err = m.LookupIDOwners(ctx, []string{pA, pB}, []string{prodA, prodB, prodC, ghost})
	if err != nil {
		t.Fatalf("归属诊断失败：%v", err)
	}
	if owners[prodA] != pA || owners[prodB] != pB {
		t.Fatalf("清单 A+B 时两条都应命中各自的工程，实际 %v", owners)
	}
	if _, ok := owners[prodC]; ok {
		t.Fatalf("清单里没有 C，不该回答 C 的商品，实际 %v", owners)
	}
	if _, ok := owners[ghost]; ok {
		t.Fatalf("不存在的 id 不该有归属，实际 %v", owners)
	}

	if owners, err := m.LookupIDOwners(ctx, nil, []string{prodA}); err != nil || len(owners) != 0 {
		t.Fatalf("空工程清单应返回空结论，实际 %v / %v", owners, err)
	}
	if owners, err := m.LookupIDOwners(ctx, []string{pA}, nil); err != nil || len(owners) != 0 {
		t.Fatalf("空 id 集应返回空结论，实际 %v / %v", owners, err)
	}

	if _, err := m.LookupIDOwners(ctx, []string{"not-uuid"}, []string{prodA}); !errorsIsInvalidProjectID(err) {
		t.Fatalf("非法工程 id 应返回 rls.ErrInvalidProjectID，实际 %v", err)
	}
}
