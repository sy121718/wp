// product_cross_project_delete_guard_test.go — 删除守卫的**跨工程**可见性（审计 DB-03 §1.2 / §5.1 第 2 条，PROD-02）。
//
// 每个守卫一条**失败能力**用例：构造「A 工程的分类 / 品牌 / 属性 / 标签 / 变体被 B 工程
// 的商品引用」这一种存量形态，然后从 A 工程发起删除。
//
//	修改前：守卫用 rls.InProjectScope(ctx, db, A) 把作用域收在 A，命中 0 行 ⇒ 删除放行
//	       ⇒ B 工程商品的 JSONB 引用永久悬空（本条用例在这一版上是红的）；
//	修改后：拒绝删除，并在错误里给出可定位信息（引用面 / 工程 / 商品 id），
//	       数据一行都不许动（本条用例在这一版上是绿的）。
//
// 为什么用**直接写库**造跨工程引用：写入路径（resolveCategoryIDs / resolveBrandID /
// resolveAttributeIDs / 捆绑成员解析）一律拒绝跨工程引用，所以这种行只可能来自
// ①校验上线前的存量 ②绕过 service 的写入（插件 / 直连 / 导入）—— 审计 DB-03 §3.2 的夹具库
// 也是这么造的。本文件不假装 API 能写进去。
//
// 最后一个用例把同一条守卫放进**非超级角色**里跑：超级用户绕过 RLS，只有非超级角色能证明
// 「换成 go_wp_app 之后守卫的跨工程能力仍然成立」（这正是任务要求讲清楚的那件事）。
package feature

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/rls"
	"go_wp/public/test/support"
)

// crossRefFixture 隔离 PG schema + 生产迁移 + **两个工程**（跨工程引用检查需要第二个工程）。
type crossRefFixture struct {
	*attrFixture
	otherProjectID string
}

func newCrossRefFixture(t *testing.T) *crossRefFixture {
	t.Helper()
	base := newAttrFixture(t)
	if base == nil {
		return nil
	}
	other, err := base.projects.Create(context.Background(), &projectdto.CreateReq{Name: "跨工程引用的对端工程"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}
	if other.ID == base.projectID {
		t.Fatalf("第二个工程 id 与第一个相同：%s", other.ID)
	}
	return &crossRefFixture{attrFixture: base, otherProjectID: other.ID}
}

// mkProduct 在某工程下建一个商品（走真实 service；变体商品会带一个默认变体）。
func (f *crossRefFixture) mkProduct(t *testing.T, projectID, name, slug string) *productdto.ProductResp {
	t.Helper()
	p, err := f.svc.Create(context.Background(), &productdto.CreateReq{
		ProjectID: projectID, Name: name, Slug: slug,
	})
	if err != nil {
		t.Fatalf("建商品 %s（工程 %s）失败: %v", name, projectID, err)
	}
	return p
}

// firstVariantID 取商品的首个变体（直读表：变体列表接口不在本用例的验证范围里）。
func (f *crossRefFixture) firstVariantID(t *testing.T, productID string) string {
	t.Helper()
	var id string
	if err := f.db.Raw("SELECT id FROM product_variants WHERE product_id = ? ORDER BY create_time ASC, id ASC LIMIT 1", productID).
		Scan(&id).Error; err != nil {
		t.Fatalf("读变体 id 失败: %v", err)
	}
	if id == "" {
		t.Fatalf("商品 %s 没有变体", productID)
	}
	return id
}

// setProductRef 把引用**直接写进 products**（绕过 service 的跨工程校验，见文件头）。
func (f *crossRefFixture) setProductRef(t *testing.T, productID, column, value string) {
	t.Helper()
	var (
		sql string
		arg any = value
	)
	switch column {
	case "category_ids", "attribute_ids", "tag_ids":
		sql = "UPDATE products SET " + column + " = jsonb_build_array(?::text) WHERE id = ?"
	case "brand_id":
		sql = "UPDATE products SET brand_id = ?::uuid WHERE id = ?"
	case "bundle_items":
		sql = "UPDATE products SET bundle_items = jsonb_build_object('options', jsonb_build_array(jsonb_build_object('variantId', ?::text))) WHERE id = ?"
	default:
		t.Fatalf("未知引用列 %s", column)
	}
	res := f.db.Exec(sql, arg, productID)
	if res.Error != nil {
		t.Fatalf("直接写入跨工程引用（%s）失败: %v", column, res.Error)
	}
	if res.RowsAffected != 1 {
		t.Fatalf("直接写入跨工程引用（%s）影响行数应为 1，实际 %d（product=%s）", column, res.RowsAffected, productID)
	}
}

// refGuardErrKey 把「key：明细」形态的业务错误拆成 key 与明细（删除守卫的拒绝口径）。
func refGuardErrKey(msg string) (key, tail string) {
	if i := strings.Index(msg, "："); i >= 0 {
		return msg[:i], strings.TrimSpace(msg[i+len("："):])
	}
	return msg, ""
}

// assertRefGuardError 断言删除守卫的拒绝形态：业务 key **逐字相等** + 明细里带引用面。
//
// 这是既有断言（整串等于 key）的**加强**而不是放宽：原来只认「有没有拦住」，
// 现在还认「说清了是哪张表在引用」—— 少任何一半都红。
func assertRefGuardError(t *testing.T, err error, key, refColumn string) {
	t.Helper()
	if err == nil {
		t.Fatalf("必须拒绝删除并返回 %s，实际 nil", key)
	}
	gotKey, tail := refGuardErrKey(err.Error())
	if gotKey != key {
		t.Fatalf("应返回 %s，实际 %v", key, err)
	}
	if tail == "" || !strings.Contains(tail, refColumn) {
		t.Fatalf("%s 的拒绝必须带可定位明细（含引用面 %s），实际 %v", key, refColumn, err)
	}
}

// assertCrossProjectRef 断言错误同时给出四类可定位信息，并且整条文案仍在读侧白名单的
// 字节上限内（shell.NoticeMaxBytes = 512；超了会被判成伪造，明细整条丢失）。
func assertCrossProjectRef(t *testing.T, err error, key, refColumn, projectID, productID string) {
	t.Helper()
	if err == nil {
		t.Fatalf("必须拒绝删除：key=%s（修改前这里的删除会成功并留下跨工程悬空引用）", key)
	}
	msg := err.Error()
	for _, want := range []string{key, refColumn, projectID, productID} {
		if !strings.Contains(msg, want) {
			t.Errorf("可定位信息缺少 %q：%s", want, msg)
		}
	}
	if len(msg) > 512 {
		t.Errorf("文案 %d 字节超过回执上限 512（读侧会判为伪造）：%s", len(msg), msg)
	}
	t.Logf("%s → %s", key, msg)
}

// assertStillThere 断言被拒绝的删除没有改动任何数据。
func (f *crossRefFixture) assertCategoryStillThere(t *testing.T, categoryID string) {
	t.Helper()
	if _, err := f.svc.GetCategory(context.Background(), &productdto.GetCategoryReq{ProjectID: f.projectID, ID: categoryID}); err != nil {
		t.Fatalf("被拒绝的删除不得改动数据（分类已不在）: %v", err)
	}
}

// TestCrossProjectCategoryDeleteGuard 分类被另一个工程的商品引用时必须拒绝删除。
func TestCrossProjectCategoryDeleteGuard(t *testing.T) {
	f := newCrossRefFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	category, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "跨工程分类", Slug: "cross-project-category",
	})
	if err != nil {
		t.Fatalf("建分类失败: %v", err)
	}
	// 对端工程的商品引用本工程的分类（存量形态）。
	pB := f.mkProduct(t, f.otherProjectID, "B 工程商品", "cross-project-category-product")
	f.setProductRef(t, pB.ID, "category_ids", category.ID)

	derr := f.svc.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ProjectID: f.projectID, ID: category.ID})
	assertCrossProjectRef(t, derr, productenums.ErrCategoryInUse, "products.category_ids", f.otherProjectID, pB.ID)
	f.assertCategoryStillThere(t, category.ID)

	// 解绑后必须能正常删除（守卫不能变成「一律拒绝」）。
	if err := f.db.Exec("UPDATE products SET category_ids = '[]'::jsonb WHERE id = ?", pB.ID).Error; err != nil {
		t.Fatalf("解绑失败: %v", err)
	}
	if err := f.svc.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ProjectID: f.projectID, ID: category.ID}); err != nil {
		t.Fatalf("解绑后应可删除: %v", err)
	}
}

// TestCrossProjectBrandDeleteGuard 品牌被另一个工程的商品引用时必须拒绝删除
// （漏看跨工程引用时的后果不是悬空 id，而是外键 ON DELETE SET NULL 的**静默解绑**）。
func TestCrossProjectBrandDeleteGuard(t *testing.T) {
	f := newCrossRefFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	brand, err := f.svc.CreateBrand(ctx, &productdto.CreateBrandReq{
		ProjectID: f.projectID, Name: "跨工程品牌", Slug: "cross-project-brand",
	})
	if err != nil {
		t.Fatalf("建品牌失败: %v", err)
	}
	pB := f.mkProduct(t, f.otherProjectID, "B 工程商品", "cross-project-brand-product")
	f.setProductRef(t, pB.ID, "brand_id", brand.ID)

	derr := f.svc.DeleteBrand(ctx, &productdto.DeleteBrandReq{ProjectID: f.projectID, ID: brand.ID})
	assertCrossProjectRef(t, derr, productenums.ErrBrandInUse, "products.brand_id", f.otherProjectID, pB.ID)

	// 反证「静默解绑」确实会发生：绕开守卫直接删品牌行，对端商品的 brand_id 被置空。
	var stillThere string
	if err := f.db.Raw("SELECT brand_id::text FROM products WHERE id = ?", pB.ID).Scan(&stillThere).Error; err != nil {
		t.Fatalf("读 brand_id 失败: %v", err)
	}
	if stillThere != brand.ID {
		t.Fatalf("守卫拒绝后 brand_id 不应变化：want %s got %q", brand.ID, stillThere)
	}
}

// TestCrossProjectAttributeDeleteGuard 属性组被另一个工程的商品引用时必须拒绝删除。
func TestCrossProjectAttributeDeleteGuard(t *testing.T) {
	f := newCrossRefFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	attr, err := f.svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "跨工程属性", Key: "cross-project-attribute",
	})
	if err != nil {
		t.Fatalf("建属性组失败: %v", err)
	}
	pB := f.mkProduct(t, f.otherProjectID, "B 工程商品", "cross-project-attribute-product")
	f.setProductRef(t, pB.ID, "attribute_ids", attr.ID)

	derr := f.svc.DeleteAttribute(ctx, &productdto.DeleteAttributeReq{ProjectID: f.projectID, ID: attr.ID})
	assertCrossProjectRef(t, derr, productenums.ErrAttrInUse, "products.attribute_ids", f.otherProjectID, pB.ID)
}

// TestCrossProjectTagDeleteGuard 标签被另一个工程的商品引用时必须拒绝删除
// （本工程内的引用照旧解绑，只有跨工程引用打回给人：跨工程写不该由本工程的删除动作代劳）。
func TestCrossProjectTagDeleteGuard(t *testing.T) {
	f := newCrossRefFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	tag, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{
		ProjectID: f.projectID, Name: "跨工程标签", Slug: "cross-project-tag", Kind: "manual",
	})
	if err != nil {
		t.Fatalf("建标签失败: %v", err)
	}
	pB := f.mkProduct(t, f.otherProjectID, "B 工程商品", "cross-project-tag-product")
	f.setProductRef(t, pB.ID, "tag_ids", tag.ID)

	derr := f.svc.DeleteTag(ctx, &productdto.DeleteTagReq{ProjectID: f.projectID, ID: tag.ID})
	assertCrossProjectRef(t, derr, productenums.ErrTagCrossProject, "products.tag_ids", f.otherProjectID, pB.ID)

	// 本工程内的引用仍然照旧解绑后删除（既有语义不变）。
	tagB, err := f.svc.CreateTag(ctx, &productdto.CreateTagReq{
		ProjectID: f.projectID, Name: "本工程标签", Slug: "same-project-tag", Kind: "manual",
	})
	if err != nil {
		t.Fatalf("建本工程标签失败: %v", err)
	}
	pA := f.mkProduct(t, f.projectID, "A 工程商品", "same-project-tag-product")
	if _, uerr := f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: pA.ID, TagIDs: []string{tagB.ID}}); uerr != nil {
		t.Fatalf("给本工程商品挂标签失败: %v", uerr)
	}
	if err := f.svc.DeleteTag(ctx, &productdto.DeleteTagReq{ProjectID: f.projectID, ID: tagB.ID}); err != nil {
		t.Fatalf("只有本工程引用时应照旧解绑并删除: %v", err)
	}
}

// TestCrossProjectBundleVariantDeleteGuard 变体被**另一个工程**的捆绑商品引用时，
// 单条删除与清单保存两条路径都必须拒绝，并给出可定位明细。
func TestCrossProjectBundleVariantDeleteGuard(t *testing.T) {
	f := newCrossRefFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	host := f.mkProduct(t, f.projectID, "A 工程宿主商品", "cross-project-bundle-host")
	variantID := f.firstVariantID(t, host.ID)

	bundle := f.mkProduct(t, f.otherProjectID, "B 工程捆绑商品", "cross-project-bundle-product")
	f.setProductRef(t, bundle.ID, "bundle_items", variantID)

	// 单条删除路径：错误里带引用面 / 工程 / 商品。
	derr := f.svc.DeleteVariant(ctx, &productdto.DeleteVariantReq{ID: variantID, ProjectID: f.projectID})
	assertCrossProjectRef(t, derr, productenums.VariantSkipBundleReferenced,
		"products.bundle_items.options[].variantId", f.otherProjectID, bundle.ID)

	// 变体必须还在库里（守卫是「不改状态」）。
	var n int64
	if err := f.db.Raw("SELECT count(*) FROM product_variants WHERE id = ?", variantID).Scan(&n).Error; err != nil {
		t.Fatalf("读变体行数失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("被拦下的变体必须留在库里，实际 %d 行", n)
	}
}

// TestCrossProjectRefGuardUnderNonSuperRole 把同一条守卫放进**非超级角色**里跑。
//
// 为什么必须单独有这一条：超级用户无条件绕过 RLS（FORCE 只约束表属主），在它下面
// 连「不带作用域的查询」都能看见全部工程 —— 那证明不了换成 go_wp_app（DB-04）之后
// 守卫还成立。本用例用 SET ROLE 切到 NOSUPERUSER NOBYPASSRLS 角色（并断言 rls.BypassedRole
// 为 false），再执行「删 A 工程的分类 / 标签 / 变体」，跨工程引用仍然可见 ⇒ 守卫成立。
//
// 连接固定：db.Connection 把整段跑在**同一条连接**上（SET ROLE 是会话级状态，
// 连接池里多一条连接就会让后续语句落在没切角色的那条上）。
func TestCrossProjectRefGuardUnderNonSuperRole(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return
	}
	ctx := context.Background()
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	pA, err := projects.Create(ctx, &projectdto.CreateReq{Name: "守卫 A 工程"})
	if err != nil {
		t.Fatalf("建工程 A 失败: %v", err)
	}
	pB, err := projects.Create(ctx, &projectdto.CreateReq{Name: "守卫 B 工程"})
	if err != nil {
		t.Fatalf("建工程 B 失败: %v", err)
	}
	svc := productservice.NewService(productmodel.NewModel(db), projects)
	category, err := svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: pA.ID, Name: "非超级角色分类", Slug: "nonsuper-category"})
	if err != nil {
		t.Fatalf("建分类失败: %v", err)
	}
	tag, err := svc.CreateTag(ctx, &productdto.CreateTagReq{ProjectID: pA.ID, Name: "非超级角色标签", Slug: "nonsuper-tag", Kind: "manual"})
	if err != nil {
		t.Fatalf("建标签失败: %v", err)
	}
	brand, err := svc.CreateBrand(ctx, &productdto.CreateBrandReq{ProjectID: pA.ID, Name: "非超级角色品牌", Slug: "nonsuper-brand"})
	if err != nil {
		t.Fatalf("建品牌失败: %v", err)
	}
	attribute, err := svc.CreateAttribute(ctx, &productdto.CreateAttributeReq{ProjectID: pA.ID, Name: "非超级角色属性组", Key: "nonsuper-attr"})
	if err != nil {
		t.Fatalf("建属性组失败: %v", err)
	}
	host, err := svc.Create(ctx, &productdto.CreateReq{ProjectID: pA.ID, Name: "非超级角色宿主商品", Slug: "nonsuper-host"})
	if err != nil {
		t.Fatalf("建宿主商品失败: %v", err)
	}
	var variantID string
	if err := db.Raw("SELECT id FROM product_variants WHERE product_id = ? ORDER BY create_time ASC, id ASC LIMIT 1", host.ID).Scan(&variantID).Error; err != nil || variantID == "" {
		t.Fatalf("读变体 id 失败（id=%q err=%v）", variantID, err)
	}
	pBProd, err := svc.Create(ctx, &productdto.CreateReq{ProjectID: pB.ID, Name: "非超级角色对端商品", Slug: "nonsuper-b"})
	if err != nil {
		t.Fatalf("建对端商品失败: %v", err)
	}
	// 跨工程引用（直连写入，见文件头）—— 全部在切换角色**之前**准备好。
	for _, stmt := range []struct {
		sql string
		arg string
	}{
		{"UPDATE products SET category_ids = jsonb_build_array(?::text) WHERE id = ?", category.ID},
		{"UPDATE products SET tag_ids = jsonb_build_array(?::text) WHERE id = ?", tag.ID},
		{"UPDATE products SET attribute_ids = jsonb_build_array(?::text) WHERE id = ?", attribute.ID},
		{"UPDATE products SET brand_id = ?::uuid WHERE id = ?", brand.ID},
		{"UPDATE products SET bundle_items = jsonb_build_object('options', jsonb_build_array(jsonb_build_object('variantId', ?::text))) WHERE id = ?", variantID},
	} {
		if err := db.Exec(stmt.sql, stmt.arg, pBProd.ID).Error; err != nil {
			t.Fatalf("准备跨工程引用失败: %v", err)
		}
	}

	role := ensureNonSuperRole(t, db)
	if err := db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("SET ROLE " + role).Error; err != nil {
			return fmt.Errorf("切换角色失败: %w", err)
		}
		defer func() { _ = conn.Exec("RESET ROLE").Error }()
		bypass, berr := rls.BypassedRole(ctx, conn)
		if berr != nil {
			return fmt.Errorf("读角色属性失败: %w", berr)
		}
		if bypass {
			return errors.New("测试跑在绕过 RLS 的角色上（superuser / BYPASSRLS），本用例会失真")
		}
		// 反证环境（与 public/test/rls 同口径）：未设作用域时 products 一行都读不到 ——
		// 也就是说「不带作用域的一条查询」在这个角色下必然得出「没有任何人引用」。
		// 本用例守的正是这一点：守卫的跨工程能力不能建立在那条路上。
		var visible int64
		if qerr := conn.Raw("SELECT count(*) FROM products").Scan(&visible).Error; qerr != nil {
			return fmt.Errorf("未设作用域读 products 失败: %w", qerr)
		}
		if visible != 0 {
			return fmt.Errorf("未设作用域竟读到 %d 行 products：本角色没有受 RLS 约束，用例会失真", visible)
		}

		// 角色之下：同一批对象、同一条守卫。
		scopeProjects := projectservice.NewService(projectmodel.NewProjectModel(conn))
		scoped := productservice.NewService(productmodel.NewModel(conn), scopeProjects)
		checks := []struct {
			name string
			key  string
			col  string
			call func() error
		}{
			{"分类", productenums.ErrCategoryInUse, "products.category_ids", func() error {
				return scoped.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ProjectID: pA.ID, ID: category.ID})
			}},
			{"品牌", productenums.ErrBrandInUse, "products.brand_id", func() error {
				return scoped.DeleteBrand(ctx, &productdto.DeleteBrandReq{ProjectID: pA.ID, ID: brand.ID})
			}},
			{"属性组", productenums.ErrAttrInUse, "products.attribute_ids", func() error {
				return scoped.DeleteAttribute(ctx, &productdto.DeleteAttributeReq{ProjectID: pA.ID, ID: attribute.ID})
			}},
			{"标签", productenums.ErrTagCrossProject, "products.tag_ids", func() error {
				return scoped.DeleteTag(ctx, &productdto.DeleteTagReq{ProjectID: pA.ID, ID: tag.ID})
			}},
			{"捆绑成员", productenums.VariantSkipBundleReferenced, "products.bundle_items", func() error {
				return scoped.DeleteVariant(ctx, &productdto.DeleteVariantReq{ID: variantID, ProjectID: pA.ID})
			}},
		}
		// 五个守卫**逐条报告**（不在第一条失败处停下）：这一版在「守卫只看本工程」的代码上
		// 应当五条全红，只报第一条会让人以为另外四条没问题。
		var failures []string
		for _, c := range checks {
			derr := c.call()
			if derr == nil {
				failures = append(failures, fmt.Sprintf("%s：未拦住跨工程引用（跨工程行因策略不可见 ⇒ 静默放行）", c.name))
				continue
			}
			msg := derr.Error()
			if !strings.Contains(msg, c.key) || !strings.Contains(msg, c.col) ||
				!strings.Contains(msg, pB.ID) || !strings.Contains(msg, pBProd.ID) {
				failures = append(failures, fmt.Sprintf("%s：可定位信息不完整（%s）", c.name, msg))
				continue
			}
			t.Logf("非超级角色下 %s 守卫拦住了跨工程引用：%s", c.name, msg)
		}
		if len(failures) > 0 {
			return fmt.Errorf("非超级角色下守卫失效 %d/%d：%s", len(failures), len(checks), strings.Join(failures, "；"))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// ensureNonSuperRole 建 / 复用非超级测试角色（集群级对象，固定名 + 幂等；与
// public/test/rls/nonsuperuser 同口径：随机名每跑一次就在集群里留一个角色）。
func ensureNonSuperRole(t *testing.T, db *gorm.DB) string {
	t.Helper()
	const role = "wp_test_prod02_guard"
	if err := db.Exec("DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '" + role + "') THEN CREATE ROLE " + role + " NOLOGIN; END IF; END $$;").Error; err != nil {
		t.Fatalf("建非超级测试角色失败: %v", err)
	}
	if err := db.Exec("ALTER ROLE " + role + " NOSUPERUSER NOBYPASSRLS NOLOGIN").Error; err != nil {
		t.Fatalf("收敛测试角色属性失败: %v", err)
	}
	stmts := []string{
		"GRANT USAGE ON SCHEMA public TO " + role,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO " + role,
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO " + role,
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("授权失败（%s）: %v", stmt, err)
		}
	}
	if err := db.Exec("GRANT USAGE ON SCHEMA ext_shared TO " + role).Error; err != nil {
		// ext_shared（迁移 210 的 pg_trgm 专用 schema）可能不存在；本用例的查询不需要它。
		t.Logf("ext_shared 授权跳过（可选）: %v", err)
	}
	return role
}
