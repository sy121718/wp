package feature

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"

	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
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

func refGuardErrKey(msg string) (key, tail string) {

	if i := strings.Index(msg, ": "); i >= 0 {
		return msg[:i], strings.TrimSpace(msg[i+2:])
	}
	if i := strings.Index(msg, "："); i >= 0 {
		return msg[:i], strings.TrimSpace(msg[i+len("："):])
	}
	return msg, ""
}

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

func (f *crossRefFixture) assertCategoryStillThere(t *testing.T, categoryID string) {
	t.Helper()
	if _, err := f.svc.GetCategory(context.Background(), &productdto.GetCategoryReq{ProjectID: f.projectID, ID: categoryID}); err != nil {
		t.Fatalf("被拒绝的删除不得改动数据（分类已不在）: %v", err)
	}
}

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

	pB := f.mkProduct(t, f.otherProjectID, "B 工程商品", "cross-project-category-product")
	f.setProductRef(t, pB.ID, "category_ids", category.ID)

	derr := f.svc.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ProjectID: f.projectID, ID: category.ID})
	assertCrossProjectRef(t, derr, productenums.ErrCategoryInUse, "products.category_ids", f.otherProjectID, pB.ID)
	f.assertCategoryStillThere(t, category.ID)

	if err := f.db.Exec("UPDATE products SET category_ids = '[]'::jsonb WHERE id = ?", pB.ID).Error; err != nil {
		t.Fatalf("解绑失败: %v", err)
	}
	if err := f.svc.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ProjectID: f.projectID, ID: category.ID}); err != nil {
		t.Fatalf("解绑后应可删除: %v", err)
	}
}

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

	var stillThere string
	if err := f.db.Raw("SELECT brand_id::text FROM products WHERE id = ?", pB.ID).Scan(&stillThere).Error; err != nil {
		t.Fatalf("读 brand_id 失败: %v", err)
	}
	if stillThere != brand.ID {
		t.Fatalf("守卫拒绝后 brand_id 不应变化：want %s got %q", brand.ID, stillThere)
	}
}

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
	svc := productservice.NewService(productmodel.NewModel(db), projects, masterdataservice.NewService(masterdatamodel.NewModel(db), projects))
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
	pBProd, err := svc.Create(ctx, &productdto.CreateReq{ProjectID: pB.ID, Name: "非超级角色对端商品", Slug: "nonsuper-b"})
	if err != nil {
		t.Fatalf("建对端商品失败: %v", err)
	}

	for _, stmt := range []struct {
		sql string
		arg string
	}{
		{"UPDATE products SET category_ids = jsonb_build_array(?::text) WHERE id = ?", category.ID},
		{"UPDATE products SET tag_ids = jsonb_build_array(?::text) WHERE id = ?", tag.ID},
		{"UPDATE products SET attribute_ids = jsonb_build_array(?::text) WHERE id = ?", attribute.ID},
		{"UPDATE products SET brand_id = ?::uuid WHERE id = ?", brand.ID},
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

		var visible int64
		if qerr := conn.Raw("SELECT count(*) FROM products").Scan(&visible).Error; qerr != nil {
			return fmt.Errorf("未设作用域读 products 失败: %w", qerr)
		}
		if visible != 0 {
			return fmt.Errorf("未设作用域竟读到 %d 行 products：本角色没有受 RLS 约束，用例会失真", visible)
		}

		scopeProjects := projectservice.NewService(projectmodel.NewProjectModel(conn))
		scoped := productservice.NewService(productmodel.NewModel(conn), scopeProjects, masterdataservice.NewService(masterdatamodel.NewModel(conn), scopeProjects))
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
		}

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

		t.Logf("ext_shared 授权跳过（可选）: %v", err)
	}
	return role
}
