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
	"fmt"
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

// seedFactProducts 播两个工程的商品（只有 id / 工程 / 名 / 状态参与窄投影）。
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

// TestNarrowFacts_AreProjectScoped 六条窄读（B9 之后的唯一读法）在**真实非超级角色**下同样隔离。
//
// 与上层 public/test/rls 的分工：那边用 SET ROLE 钉「作用域接对了」，这边再换一条真的
// 非超级登录连接钉一遍。加这一条的理由不是重复 —— 窄投影是「只读几列」的读法，最容易在
// 评审时被当成「不用管作用域」；而归属于 foreign project 的行在非超级角色下有两种可见形态：
// 策略挡掉（空集）或根本没挡（读到），两种都必须在这里有断言，不能只靠上层。
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
		// B 的 id 在 A 的作用域下必须是「不存在」——不是「读到了但字段为空」。
		if _, err := s.read(pA, prodB); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("%s 作用域 A 点名 B 的商品应 ErrRecordNotFound，实际 %v", s.name, err)
		}
		// 反向对照：B 的作用域读得到 B 自己那一行（证明上面的红不是「行没插进去」）。
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
		// 只点名外国工程的 id：空集，且不报错（批量读的未命中语义）。
		got, err = b.read(pA, []string{prodB})
		if err != nil || len(got) != 0 {
			t.Fatalf("%s 作用域 A 点名 B 的商品应空集，实际 %v / %v", b.name, got, err)
		}
		// 反向对照。
		got, err = b.read(pB, both)
		if err != nil || len(got) != 1 || got[0] != prodB {
			t.Fatalf("%s 作用域 B 混合入参应只命中 prodB（%s），实际 %v / %v", b.name, prodB, got, err)
		}
	}
}

// factIDs 批量窄读结果 → 命中 id（同时校验每行 project_id 与作用域一致）。
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

// TestLookupIDOwners_IsScopePredicated 归属诊断（写入口拒绝明细用）在非超级角色下照样工作。
//
// 这个方法存在的全部意义就是「本工程查不到时，问一句它到底属于谁」：它必须能看见别的工程
// 的行，否则跨工程明细永远说不出来。同时它不能变成通用跨工程读 —— 清单里没有的工程，
// 那个工程的商品一概不回答（空集，不是「返回全部」）。
//
// 用真实非超级角色跑（不是 SET ROLE）：诊断靠的是「逐工程 ScopeTx + 显式 project_id 谓词」，
// 策略生效时照样成立的证据只在这条连接上。
func TestLookupIDOwners_IsScopePredicated(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	pA, pB, prodA, prodB := seedFactProducts(t, f.admin)
	pC, prodC := uuid.NewString(), uuid.NewString()
	support.SeedProjectRow(t, f.admin, pC, "归属诊断工程 C")
	seedRow(t, f.admin, "INSERT INTO products (id, project_id, name, slug, status) VALUES (?,?,?,?,'published')", prodC, pC, "C 商品", "c-fact")
	m := productmodel.NewModel(f.app)
	ghost := uuid.NewString()

	// 清单只有 A：B / C 的商品一件都不回答（诊断的范围由清单决定，不由「库里有啥」决定）。
	owners, err := m.LookupIDOwners(ctx, []string{pA}, []string{prodA, prodB, prodC, ghost})
	if err != nil {
		t.Fatalf("归属诊断失败：%v", err)
	}
	if len(owners) != 1 || owners[prodA] != pA {
		t.Fatalf("清单只有 A 时应只回答 A 的商品，实际 %v", owners)
	}

	// 清单 A + B：两条都回答，且各自归到自己的工程（写入口的拒绝明细要的就是这个）。
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

	// 空清单 / 空 ids：不查库、空结论（也不能退化成「全库可见」）。
	if owners, err := m.LookupIDOwners(ctx, nil, []string{prodA}); err != nil || len(owners) != 0 {
		t.Fatalf("空工程清单应返回空结论，实际 %v / %v", owners, err)
	}
	if owners, err := m.LookupIDOwners(ctx, []string{pA}, nil); err != nil || len(owners) != 0 {
		t.Fatalf("空 id 集应返回空结论，实际 %v / %v", owners, err)
	}

	// 非 uuid 的工程 id 必须显式报错（不能静默 0 行）：诊断失败要 fail closed。
	if _, err := m.LookupIDOwners(ctx, []string{"not-uuid"}, []string{prodA}); !errorsIsInvalidProjectID(err) {
		t.Fatalf("非法工程 id 应返回 rls.ErrInvalidProjectID，实际 %v", err)
	}
}
