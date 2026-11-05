package feature

import (
	"context"
	"go_wp/internal/adminui"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"

	"go_wp/internal/middleware/adminctx"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/templates"
	text "go_wp/pkg/view/jet"

	"go_wp/public/migrations"
)

func TestCategoryTreeHierarchySortSlugSEO(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	root, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "男装", Slug: "men", Sort: 2,
		SEOTitle: "男装 SEO 标题", SEODescription: "男装 SEO 描述",
		Image: "/storage/img/men.jpg", Description: "男装分类描述",
	})
	if err != nil {
		t.Fatalf("建顶级分类失败: %v", err)
	}
	child, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "衬衫", ParentID: root.ID, Sort: 1,
	})
	if err != nil {
		t.Fatalf("建子分类失败: %v", err)
	}
	if _, err = f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "女装", Slug: "women", Sort: 1,
	}); err != nil {
		t.Fatalf("建第二个顶级分类失败: %v", err)
	}

	if child.Slug == "" || !strings.HasPrefix(child.Slug, "c-") {
		t.Fatalf("中文名分类应派生兜底 slug，实际 %q", child.Slug)
	}

	tree, err := f.svc.ListCategories(ctx, &productdto.ListCategoryReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("分类列表失败: %v", err)
	}
	if len(tree) != 2 {
		t.Fatalf("应有 2 个顶级分类，实际 %d", len(tree))
	}

	if tree[0].Name != "女装" || tree[1].Name != "男装" {
		t.Fatalf("顶级分类应按排序号升序，实际 %s / %s", tree[0].Name, tree[1].Name)
	}
	if len(tree[1].Children) != 1 || tree[1].Children[0].ID != child.ID {
		t.Fatalf("子分类应挂在父级 Children 里: %+v", tree[1])
	}
	if tree[1].Depth != 0 || tree[1].Children[0].Depth != 1 {
		t.Fatalf("Depth 应由服务端填好（顶级 0 / 子级 1），实际 %d / %d",
			tree[1].Depth, tree[1].Children[0].Depth)
	}
	got, err := f.svc.GetCategory(ctx, &productdto.GetCategoryReq{ProjectID: f.projectID, ID: root.ID})
	if err != nil {
		t.Fatalf("分类详情失败: %v", err)
	}
	if got.SEOTitle != "男装 SEO 标题" || got.SEODescription != "男装 SEO 描述" ||
		got.Image != testMediaBaseURL+"/img/men.jpg" || got.Description != "男装分类描述" {
		t.Fatalf("分类 SEO / 图 / 描述字段应按媒体地址规范回读: %+v", got)
	}

	if _, err = f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "男装副本", Slug: "men",
	}); err == nil || err.Error() != productenums.ErrCategorySlugTaken {
		t.Fatalf("slug 重复应返回 ErrCategorySlugTaken，实际 %v", err)
	}
}

func TestCategoryBoundedPage(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := t.Context()
	root, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "根", Slug: "root", Sort: 1})
	if err != nil {
		t.Fatal(err)
	}
	otherRoot, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "另一个根", Slug: "other", Sort: 2})
	if err != nil {
		t.Fatal(err)
	}
	child, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, ParentID: root.ID, Name: "子", Slug: "child"})
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, ParentID: child.ID, Name: "命中叶", Slug: "leaf"})
	if err != nil {
		t.Fatal(err)
	}
	otherProject, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "隔离工程"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: otherProject.ID, Name: "命中叶", Slug: "foreign"}); err != nil {
		t.Fatal(err)
	}

	first, err := f.svc.ListCategoryPage(ctx, &productdto.ListCategoryPageReq{ProjectID: f.projectID, Page: 1, Size: 1})
	if err != nil || first.Total != 2 || len(first.Items) != 1 {
		t.Fatalf("根级首页: %+v, %v", first, err)
	}
	tree := first.Items[0]
	if tree.ID != root.ID || tree.Depth != 0 || !tree.HasChildren {
		t.Fatalf("首页根节点不对: %+v", tree)
	}
	if len(tree.Children) != 1 || tree.Children[0].ID != child.ID || tree.Children[0].Depth != 1 {
		t.Fatalf("首页应带出直接子级: %+v", tree)
	}
	if len(tree.Children[0].Children) != 1 || tree.Children[0].Children[0].ID != leaf.ID || tree.Children[0].Children[0].Depth != 2 {
		t.Fatalf("首页应带出整棵子树: %+v", tree)
	}
	second, err := f.svc.ListCategoryPage(ctx, &productdto.ListCategoryPageReq{ProjectID: f.projectID, Page: 2, Size: 1})
	if err != nil || second.Total != 2 || len(second.Items) != 1 || second.Items[0].ID != otherRoot.ID || len(second.Items[0].Children) != 0 {
		t.Fatalf("根级次页: %+v, %v", second, err)
	}
	search, err := f.svc.ListCategoryPage(ctx, &productdto.ListCategoryPageReq{ProjectID: f.projectID, Keyword: "命中叶", Page: 1, Size: 1})
	if err != nil || search.Total != 1 || search.MatchTotal != 1 || len(search.Items) != 1 || search.Items[0].ID != root.ID {
		t.Fatalf("搜索按根分页: %+v, %v", search, err)
	}
	if len(search.Items[0].Children) != 1 || len(search.Items[0].Children[0].Children) != 1 ||
		search.Items[0].Children[0].Children[0].ID != leaf.ID || !search.Items[0].Children[0].Children[0].Matched {
		t.Fatalf("搜索祖先链应成树且只标命中项: %+v", search.Items[0])
	}

	if search.MatchTotal != 1 {
		t.Fatalf("跨工程同名分类不应计入命中: %d", search.MatchTotal)
	}
	secondLeaf, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, ParentID: child.ID, Name: "命中叶乙", Slug: "leaf-two"})
	if err != nil {
		t.Fatal(err)
	}
	search, err = f.svc.ListCategoryPage(ctx, &productdto.ListCategoryPageReq{ProjectID: f.projectID, Keyword: "命中叶", Size: 20})
	if err != nil || search.Total != 1 || search.MatchTotal != 2 || len(search.Items) != 1 ||
		len(search.Items[0].Children) != 1 || len(search.Items[0].Children[0].Children) != 2 ||
		search.Items[0].Children[0].Children[0].ID != leaf.ID || search.Items[0].Children[0].Children[1].ID != secondLeaf.ID {
		t.Fatalf("共享祖先只出现一次且祖先在命中之前: %+v, %v", search, err)
	}
	literal, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "50%_折扣", Slug: "sale-literal"})
	if err != nil {
		t.Fatal(err)
	}
	percent, err := f.svc.ListCategoryPage(ctx, &productdto.ListCategoryPageReq{ProjectID: f.projectID, Keyword: "%_", Size: 20})
	if err != nil || percent.Total != 1 || percent.MatchTotal != 1 || len(percent.Items) != 1 || percent.Items[0].ID != literal.ID {
		t.Fatalf("搜索通配符按字面量匹配: %+v, %v", percent, err)
	}
}

func TestCategoryCrossProjectAndCycleGuards(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	other, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "另一个工程"})
	if err != nil {
		t.Fatalf("建第二工程失败: %v", err)
	}

	root, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "根", Slug: "root"})
	if err != nil {
		t.Fatalf("建根分类失败: %v", err)
	}
	child, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "子", Slug: "child", ParentID: root.ID,
	})
	if err != nil {
		t.Fatalf("建子分类失败: %v", err)
	}

	self := root.ID
	if _, err = f.svc.UpdateCategory(ctx, &productdto.UpdateCategoryReq{ProjectID: f.projectID, ID: root.ID, ParentID: &self}); err == nil ||
		err.Error() != productenums.ErrCategoryCycle {
		t.Fatalf("挂到自身应返回 ErrCategoryCycle，实际 %v", err)
	}

	if _, err = f.svc.UpdateCategory(ctx, &productdto.UpdateCategoryReq{ProjectID: f.projectID, ID: root.ID, ParentID: &child.ID}); err == nil ||
		err.Error() != productenums.ErrCategoryCycle {
		t.Fatalf("挂到自己的后代应返回 ErrCategoryCycle，实际 %v", err)
	}

	foreign, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: other.ID, Name: "别家分类", Slug: "foreign"})
	if err != nil {
		t.Fatalf("建他工程分类失败: %v", err)
	}
	if _, err = f.svc.UpdateCategory(ctx, &productdto.UpdateCategoryReq{ProjectID: f.projectID, ID: root.ID, ParentID: &foreign.ID}); err == nil ||
		err.Error() != productenums.ErrCategoryParentMismatch {
		t.Fatalf("跨工程父级应返回 ErrCategoryParentMismatch，实际 %v", err)
	}

	top := ""
	upd, err := f.svc.UpdateCategory(ctx, &productdto.UpdateCategoryReq{ProjectID: f.projectID, ID: child.ID, ParentID: &top})
	if err != nil {
		t.Fatalf("提升为顶级失败: %v", err)
	}
	if upd.ParentID != "" {
		t.Fatalf("提升为顶级后 ParentID 应为空，实际 %q", upd.ParentID)
	}
}

func TestCategoryDeleteGuards(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	root, _ := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "根", Slug: "root"})
	child, _ := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "子", Slug: "child", ParentID: root.ID,
	})
	if err := f.svc.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ProjectID: f.projectID, ID: root.ID}); err == nil ||
		err.Error() != productenums.ErrCategoryHasChildren {
		t.Fatalf("有子级应返回 ErrCategoryHasChildren，实际 %v", err)
	}

	p, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "挂分类的商品", CategoryIDs: []string{child.ID},
	})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}

	err = f.svc.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ProjectID: f.projectID, ID: child.ID})
	assertRefGuardError(t, err, productenums.ErrCategoryInUse, "products.category_ids")

	if _, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, CategoryIDs: []string{}}); err != nil {
		t.Fatalf("解绑分类失败: %v", err)
	}
	if err = f.svc.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ProjectID: f.projectID, ID: child.ID}); err != nil {
		t.Fatalf("解绑后应可删除分类: %v", err)
	}
}

func TestBrandCRUDAndGuards(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	brand, err := f.svc.CreateBrand(ctx, &productdto.CreateBrandReq{
		ProjectID: f.projectID, Name: "山野", Slug: "shanye",
		Logo: "/storage/img/logo.svg", Description: "户外品牌",
		SEOTitle: "山野 SEO 标题", SEODescription: "山野 SEO 描述", Sort: 3,
	})
	if err != nil {
		t.Fatalf("建品牌失败: %v", err)
	}
	if brand.Logo != testMediaBaseURL+"/img/logo.svg" || brand.Description != "户外品牌" ||
		brand.SEOTitle != "山野 SEO 标题" || brand.SEODescription != "山野 SEO 描述" || brand.Sort != 3 {
		t.Fatalf("品牌字段应按媒体地址规范回读: %+v", brand)
	}
	if _, err = f.svc.CreateBrand(ctx, &productdto.CreateBrandReq{
		ProjectID: f.projectID, Name: "山野副本", Slug: "shanye",
	}); err == nil || err.Error() != productenums.ErrBrandSlugTaken {
		t.Fatalf("品牌 slug 重复应返回 ErrBrandSlugTaken，实际 %v", err)
	}
	name := "山野户外"
	got, err := f.svc.UpdateBrand(ctx, &productdto.UpdateBrandReq{ProjectID: f.projectID, ID: brand.ID, Name: &name})
	if err != nil {
		t.Fatalf("改品牌失败: %v", err)
	}
	if got.Name != "山野户外" {
		t.Fatalf("品牌改名未生效: %+v", got)
	}

	p, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "带品牌的商品", BrandID: brand.ID,
	})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	if p.BrandID != brand.ID {
		t.Fatalf("商品应挂上品牌，实际 %q", p.BrandID)
	}

	err = f.svc.DeleteBrand(ctx, &productdto.DeleteBrandReq{ProjectID: f.projectID, ID: brand.ID})
	assertRefGuardError(t, err, productenums.ErrBrandInUse, "products.brand_id")

	empty := ""
	if _, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, BrandID: &empty}); err != nil {
		t.Fatalf("解绑品牌失败: %v", err)
	}
	if err = f.svc.DeleteBrand(ctx, &productdto.DeleteBrandReq{ProjectID: f.projectID, ID: brand.ID}); err != nil {
		t.Fatalf("解绑后应可删除品牌: %v", err)
	}
}

func TestProductCategoryRefsAndInvariant(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	other, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "另一个工程"})
	if err != nil {
		t.Fatalf("建第二工程失败: %v", err)
	}
	catA, _ := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "A", Slug: "a"})
	catB, _ := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "B", Slug: "b"})
	catC, _ := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: f.projectID, Name: "C", Slug: "c"})
	brand, _ := f.svc.CreateBrand(ctx, &productdto.CreateBrandReq{ProjectID: f.projectID, Name: "品牌X", Slug: "brandx"})

	p, err := f.svc.Create(ctx, &productdto.CreateReq{
		ProjectID: f.projectID, Name: "多分类商品",
		CategoryIDs: []string{catA.ID, catB.ID}, PrimaryCategoryID: catC.ID,
		BrandID: brand.ID,
	})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	if p.PrimaryCategoryID != catC.ID {
		t.Fatalf("主分类应为 C，实际 %q", p.PrimaryCategoryID)
	}
	if !strings.Contains(strings.Join(p.CategoryIDs, ","), catC.ID) {
		t.Fatalf("主分类应被自动纳入附属分类，实际 %v", p.CategoryIDs)
	}
	if p.BrandID != brand.ID {
		t.Fatalf("商品应挂上品牌，实际 %q", p.BrandID)
	}

	onlyA := []string{catA.ID}
	upd, err := f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, CategoryIDs: onlyA})
	if err != nil {
		t.Fatalf("替换附属分类失败: %v", err)
	}
	if len(upd.CategoryIDs) != 1 || upd.CategoryIDs[0] != catA.ID {
		t.Fatalf("附属分类应被整体替换为 A，实际 %v", upd.CategoryIDs)
	}
	if upd.PrimaryCategoryID != "" {
		t.Fatalf("附属列表里已无原主分类，主分类应自动解绑，实际 %q", upd.PrimaryCategoryID)
	}

	upd, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, PrimaryCategoryID: &catB.ID})
	if err != nil {
		t.Fatalf("指定主分类失败: %v", err)
	}
	if upd.PrimaryCategoryID != catB.ID || len(upd.CategoryIDs) != 2 {
		t.Fatalf("只改主分类应把它纳入附属列表: primary=%q ids=%v", upd.PrimaryCategoryID, upd.CategoryIDs)
	}

	missing := "00000000-0000-0000-0000-000000000000"
	if _, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, CategoryIDs: []string{missing}}); err == nil ||
		err.Error() != productenums.ErrCategoryNotFound {
		t.Fatalf("不存在的分类应返回 ErrCategoryNotFound，实际 %v", err)
	}
	foreign, _ := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: other.ID, Name: "别家", Slug: "foreign"})
	if _, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, CategoryIDs: []string{foreign.ID}}); err == nil ||
		err.Error() != productenums.ErrCategoryProjectMismatch {
		t.Fatalf("跨工程分类应返回 ErrCategoryProjectMismatch，实际 %v", err)
	}
	if _, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, PrimaryCategoryID: &missing}); err == nil ||
		err.Error() != productenums.ErrCategoryNotFound {
		t.Fatalf("不存在的主分类应返回 ErrCategoryNotFound，实际 %v", err)
	}
	brandID := missing
	if _, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, BrandID: &brandID}); err == nil ||
		err.Error() != productenums.ErrBrandNotFound {
		t.Fatalf("不存在的品牌应返回 ErrBrandNotFound，实际 %v", err)
	}
	foreignBrand, _ := f.svc.CreateBrand(ctx, &productdto.CreateBrandReq{ProjectID: other.ID, Name: "别家品牌", Slug: "fb"})
	fb := foreignBrand.ID
	if _, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, BrandID: &fb}); err == nil ||
		err.Error() != productenums.ErrBrandProjectMismatch {
		t.Fatalf("跨工程品牌应返回 ErrBrandProjectMismatch，实际 %v", err)
	}
}

func TestTaxonomyPermissionsAndMenusSeeded(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	if err := migrations.RunSeeds(f.db); err != nil {
		t.Fatalf("执行数据种子失败: %v", err)
	}
	var n int64
	if err := f.db.Raw(`SELECT COUNT(*) FROM sys_permission
		WHERE module = 'product'
		  AND (permission_code LIKE 'product:category_%' OR permission_code LIKE 'product:brand_%')`).Scan(&n).Error; err != nil {
		t.Fatalf("查询权限点失败: %v", err)
	}
	if n != 10 {
		t.Fatalf("迁移 089 应 seed 10 个分类/品牌权限点，实际 %d", n)
	}
	if err := f.db.Raw(`SELECT COUNT(*) FROM sys_menus
		WHERE type = 2 AND title IN ('商品分类', '商品品牌') AND deleted_at IS NULL`).Scan(&n).Error; err != nil {
		t.Fatalf("查询菜单失败: %v", err)
	}
	if n != 2 {
		t.Fatalf("迁移 090 应 seed 分类与品牌两个后台菜单，实际 %d", n)
	}
}

func newTaxonomyPageEngine(t *testing.T) (*gin.Engine, *attrFixture) {
	t.Helper()
	f := newAttrFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{
			"product:brand_create": true, "product:brand_update": true, "product:brand_delete": true,
			"product:category_create": true, "product:category_update": true, "product:category_delete": true,
		})
		c.Set(adminctx.ButtonsKey, map[string]bool{
			"product.brand_create": true, "product.brand_update": true, "product.brand_delete": true,
			"product.category_create": true, "product.category_update": true, "product.category_delete": true,
		})

	})
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.Use(templates.FailedRenderMiddleware(func(c *gin.Context, err error) { pageview.FailScene(c, "template", err) }))
	handle := producthttp.NewProductPageHandle(f.svc, f.svc)

	engine.GET("/admin/product-categories", pageview.UseRenderer(adminui.RenderPage), handle.ProductCategoriesPage)

	engine.GET("/admin/product-brands", pageview.UseRenderer(adminui.RenderPage), handle.ProductBrandsPage)
	engine.GET("/admin/products", pageview.UseRenderer(adminui.RenderPage), handle.ProductsPage)

	engine.GET("/admin/products/detail", pageview.UseRenderer(adminui.RenderPage), handle.ProductDetailPage)
	engine.GET("/admin/products/edit", pageview.UseRenderer(adminui.RenderPage), handle.ProductEditPage)

	engine.POST("/api/product/update", pageview.HTMLFragment, producthttp.NewProductHandle(f.svc).Update)
	return engine, f
}

func TestTaxonomyAdminPages(t *testing.T) {
	engine, f := newTaxonomyPageEngine(t)
	if engine == nil {
		return
	}

	if _, cerr := f.svc.CreateCategory(t.Context(), &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "男装", Slug: "men", Description: "描述",
	}); cerr != nil {
		t.Fatalf("建分类失败: %v", cerr)
	}
	tree, err := f.svc.ListCategories(t.Context(), &productdto.ListCategoryReq{ProjectID: f.projectID})
	if err != nil || len(tree) != 1 {
		t.Fatalf("表单建分类失败: %v %+v", err, tree)
	}
	rootID := tree[0].ID
	if _, cerr := f.svc.CreateCategory(t.Context(), &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "衬衫", Slug: "shirts", ParentID: rootID,
	}); cerr != nil {
		t.Fatalf("建子分类失败: %v", cerr)
	}
	rec := httptestGet(engine, "/admin/product-categories?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("分类页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	for _, want := range []string{"商品分类", "男装", "men", "新建分类", ">删除</button>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("分类根页缺少 %q", want)
		}
	}

	if !strings.Contains(body, "shirts</code>") || !strings.Contains(body, `<span class="tree-elbow"`) {
		t.Fatal("分类页应把子级直接渲染在顶级下面（缩进成树）")
	}
	if strings.Contains(body, "parentId="+rootID) {
		t.Fatal("逐层导航已退役：列表本身就是树，不该再出现「进入父级」的链接")
	}

	if _, cerr := f.svc.CreateBrand(t.Context(), &productdto.CreateBrandReq{
		ProjectID: f.projectID, Name: "山野", Slug: "shanye", Logo: "/storage/img/logo.svg",
	}); cerr != nil {
		t.Fatalf("建品牌失败: %v", cerr)
	}
	rec = httptestGet(engine, "/admin/product-brands?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("品牌页应 200，实际 %d", rec.Code)
	}
	brandBody := rec.Body.String()
	for _, want := range []string{"商品品牌", "山野", "shanye", testMediaBaseURL + "/img/logo.svg", "新建品牌", ">删除</button>"} {
		if !strings.Contains(brandBody, want) {
			t.Fatalf("品牌页缺少 %q", want)
		}
	}

	p, err := f.svc.Create(t.Context(), &productdto.CreateReq{ProjectID: f.projectID, Name: "衬衫 A"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	brands, err := f.svc.ListBrands(t.Context(), &productdto.ListBrandReq{ProjectID: f.projectID})
	if err != nil || len(brands) != 1 {
		t.Fatalf("品牌列表异常: %v %+v", err, brands)
	}
	childID := treeIDByName(t, f, "衬衫")

	rec = postJSON(t, engine, "/api/product/update", map[string]any{
		"projectId":         f.projectID,
		"id":                p.ID,
		"brandId":           brands[0].ID,
		"categoryIds":       []string{rootID},
		"primaryCategoryId": childID,
	})
	assertProductHTMLResponse(t, rec, http.StatusOK)
	got, err := f.svc.Get(t.Context(), &productdto.GetReq{ProjectID: f.projectID, ID: p.ID})
	if err != nil {
		t.Fatalf("读商品失败: %v", err)
	}
	if got.PrimaryCategoryID != childID || got.BrandID != brands[0].ID {
		t.Fatalf("商品分类/品牌未写入: primary=%q brand=%q", got.PrimaryCategoryID, got.BrandID)
	}
	if len(got.CategoryIDs) != 2 {
		t.Fatalf("主分类应被自动纳入挂载列表（父级 + 子级），实际 %v", got.CategoryIDs)
	}

	rec = httptestGet(engine, "/admin/products/edit?project="+f.projectID+"&product="+p.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("商品编辑页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	pageBody := rec.Body.String()

	for _, want := range []string{"分类与品牌", "山野", "衬衫", `name="primaryCategoryId"`, `name="brandId"`} {
		if !strings.Contains(pageBody, want) {
			t.Fatalf("商品编辑页缺少 %q", want)
		}
	}
}

func httptestGet(engine *gin.Engine, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func treeIDByName(t *testing.T, f *attrFixture, name string) string {
	t.Helper()
	tree, err := f.svc.ListCategories(t.Context(), &productdto.ListCategoryReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("分类列表失败: %v", err)
	}
	for _, root := range tree {
		if root.Name == name {
			return root.ID
		}
		for _, child := range root.Children {
			if child.Name == name {
				return child.ID
			}
		}
	}
	t.Fatalf("找不到分类 %q", name)
	return ""
}
