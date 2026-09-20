package nonsuperuser

// product_byids_scope_test.go — 商品域五个批量读方法的工程作用域行为断言（审计 DB-03 §2.5 / DB-05）。
//
// 为什么要有这一条：catalog 全扫（TestNonSuperuser_EveryProjectTableIsCovered）只能证明
// 「策略装在表上」，证明不了「调用点真的接对了作用域」—— 方法体里少包一层
// rls.InProjectScope 时，策略照样在、catalog 照样绿，而它读的是会话变量为 NULL 的形态
// （换非超级角色后静默 0 行）。这里对着**真实非超级角色连接**做行为断言：
// 同一批 id 一起传进去，作用域是 A 时只拿得到 A 的行、B 的行一行都拿不到。

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/pkg/rls"
	"go_wp/public/test/support"
)

// seedRow 经管理连接播一行（超级用户不受策略约束）。
func seedRow(t *testing.T, admin *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := admin.Exec(sql, args...).Error; err != nil {
		t.Fatalf("播种失败（%s）：%v", sql, err)
	}
}

// assertOwnOnly 断言批量读只返回目标工程的那一行。
//
// projectOf 传 nil 表示该实体没有 project_id 列（变体表就没有，作用域来自父商品）。
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

// TestListByIDsMethods_AreProjectScoped 五个方法（DB-03 §2.5 点名）都真的带了工程作用域。
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
		// 现状（**刻意钉住**，不是期望的隔离形态）：product_variants 自身没有 project_id 列、
		// 也不在迁移 215 的策略名单里，所以在非超级角色下这条读**不受作用域约束** ——
		// 作用域 A 时两个工程的变体 id 都会返回。审计 db-03 §2.5 把本方法列为
		// 「换角色后会静默 0 行」，实测**不成立**：真实的风险方向相反（跨工程变体仍读得到）。
		//
		// 因此变体的跨工程拦截落在**归属商品**那一步（bundle 的三条路径都这么做）。
		// 这条断言一旦变红，说明有人给 product_variants 加了策略 —— 届时必须同步改
		// model 的注释（product_model.go 的 ListVariantsByIDs）与调用方的兜底假设。
		rows, err := m.ListVariantsByIDs(ctx, both(varA, varB), pA)
		if err != nil {
			t.Fatalf("读变体失败：%v", err)
		}
		if len(rows) != 2 {
			t.Fatalf("变体表当前无策略、作用域对它不构成过滤（应返回 2 行），实际 %d 行", len(rows))
		}
		// 真正生效的隔离：按归属商品取，跨工程的商品读不到 ⇒ 它的变体也进不来。
		owners, err := m.ListProductsByIDs(ctx, []string{prodA, prodB}, pA)
		if err != nil {
			t.Fatalf("解析变体归属商品失败：%v", err)
		}
		if len(owners) != 1 || owners[0].ID != prodA {
			t.Fatalf("作用域 A 下按归属商品解析应只拿到工程 A 的商品，实际 %+v", owners)
		}
	})

	// 负向：作用域是不存在的第三个工程时，四张有策略的字典表一律 0 行（不是「返回全部」）。
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

// TestListByIDsMethods_RejectEmptyProject ID 为空串时必须显式报错，不能静默 fail closed。
//
// InProjectScope 先校验 uuid 再进 SQL（rls.ErrInvalidProjectID）：空串被当作入参错误
// 直接抛出，而不是「查一个不存在的工程」返回 0 行 —— 后者在页面上表现为「功能突然没数据」，
// 排查成本远高于一个能定位到调用方的错误。
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

// errorsIsInvalidProjectID rls 的非法 uuid 判定（单独包一层，避免在用例里散落 errors.Is）。
func errorsIsInvalidProjectID(err error) bool {
	return err != nil && errors.Is(err, rls.ErrInvalidProjectID)
}
