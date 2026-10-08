// Package feature product 模块 feature 测试 —— 分类与品牌（issue #10）。
//
// 覆盖本票四条验收（真实 PostgreSQL + 生产 DDL + 真实 service）：
//  1. 分类支持父子层级、排序、slug 与 SEO 字段（列表返回树，Depth 由服务端填好）；
//  2. 品牌为独立实体，含 logo、描述、slug 与 SEO 字段；
//  3. 商品可挂多个分类并指定主分类，可指定品牌（引用必须同工程且真实存在）；
//  4. 后台可管理分类与品牌（页面 GET/POST 链路 + 真实 Jet 渲染）。
//
// 另覆盖三条容易踩的边界：
//
//	· 分类树不许成环（挂到自身 / 自己的后代下一律拒绝），也不许有子级时被删；
//	· 被商品引用（附属或主分类、品牌）的实体不许删，否则商品侧会留下悬空引用；
//	· 「主分类必属于附属分类」的不变量由服务端维持（显式指定则自动纳入，
//	  附属列表被替换掉原主分类时自动解绑）。
package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"

	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/templates"
	"go_wp/internal/shell"

	"go_wp/public/migrations"
)

// TestCategoryTreeHierarchySortSlugSEO 验收 1：父子层级 + 排序 + slug + SEO 字段。
func TestCategoryTreeHierarchySortSlugSEO(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	root, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "男装", Slug: "men", Sort: 2,
		SEOTitle: "男装 SEO 标题", SEODescription: "男装 SEO 描述",
		Image: "/img/men.jpg", Description: "男装分类描述",
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
	// 中文名派生不出 slug 时由服务端兜底随机段，不能落空。
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
	// 顶级按 sort 升序：女装(1) 在 男装(2) 之前。
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
		got.Image != "/storage/img/men.jpg" || got.Description != "男装分类描述" {
		t.Fatalf("分类 SEO / 图 / 描述字段应按媒体地址规范回读: %+v", got)
	}

	// slug 在工程内唯一。
	if _, err = f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "男装副本", Slug: "men",
	}); err == nil || err.Error() != productenums.ErrCategorySlugTaken {
		t.Fatalf("slug 重复应返回 ErrCategorySlugTaken，实际 %v", err)
	}
}

// TestCategoryBoundedPage 校验「按树根分页 + 搜索带路径 + 工程隔离」。
//
// 列表一次渲染整棵树：浏览态一页 = 一页顶级分类及其全部后代，
// 搜索态一页 = 命中所属的根分类（按命中分页会让同一棵树跨页重复出现）。
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
	// 另一个工程里的同名分类：只是用来证明工程作用域把命中数压住（下面断言 MatchTotal == 1）。
	if _, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{ProjectID: otherProject.ID, Name: "命中叶", Slug: "foreign"}); err != nil {
		t.Fatal(err)
	}

	// 分页单位是**树根**：一页 = 一页的树，不是一层节点 ——
	// 列表把整棵子树（含全部后代）一次渲染出来，所以首页带着 root 的两级后代。
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
	// MatchTotal 只数命中项：另一个工程里的同名分类不参与（工程作用域收窄）。
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

// TestCategoryCrossProjectAndCycleGuards 换父级的三条硬约束：跨工程 / 自环 / 环。
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
	// 把「根」挂到自己的后代「子」下面 → 成环，必须拒绝。
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

	// 换父级成功路径：子 → 顶级（空串即提升）。
	top := ""
	upd, err := f.svc.UpdateCategory(ctx, &productdto.UpdateCategoryReq{ProjectID: f.projectID, ID: child.ID, ParentID: &top})
	if err != nil {
		t.Fatalf("提升为顶级失败: %v", err)
	}
	if upd.ParentID != "" {
		t.Fatalf("提升为顶级后 ParentID 应为空，实际 %q", upd.ParentID)
	}
}

// TestCategoryDeleteGuards 有子级 / 被商品引用的分类不许删。
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
	// 本批起守卫的拒绝带**可定位明细**（审计 DB-03 §5.1 第 2 条 / PROD-02）：业务 key 逐字不变，
	// 后面接「引用面 / 工程 / 商品 id」。断言因此是「key 相等 + 明细在」，比原来只认整串更强。
	err = f.svc.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ProjectID: f.projectID, ID: child.ID})
	assertRefGuardError(t, err, productenums.ErrCategoryInUse, "products.category_ids")

	// 解绑后即可删除。
	if _, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, CategoryIDs: []string{}}); err != nil {
		t.Fatalf("解绑分类失败: %v", err)
	}
	if err = f.svc.DeleteCategory(ctx, &productdto.DeleteCategoryReq{ProjectID: f.projectID, ID: child.ID}); err != nil {
		t.Fatalf("解绑后应可删除分类: %v", err)
	}
}

// TestBrandCRUDAndGuards 验收 2：品牌是独立实体（logo / 描述 / slug / SEO），
// 且被商品引用时不许删。
func TestBrandCRUDAndGuards(t *testing.T) {
	f := newAttrFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	brand, err := f.svc.CreateBrand(ctx, &productdto.CreateBrandReq{
		ProjectID: f.projectID, Name: "山野", Slug: "shanye",
		Logo: "/img/logo.svg", Description: "户外品牌",
		SEOTitle: "山野 SEO 标题", SEODescription: "山野 SEO 描述", Sort: 3,
	})
	if err != nil {
		t.Fatalf("建品牌失败: %v", err)
	}
	if brand.Logo != "/storage/img/logo.svg" || brand.Description != "户外品牌" ||
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
	// 同上：品牌守卫的拒绝也带可定位明细（跨工程引用此前不可见 ⇒ 外键静默解绑）。
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

// TestProductCategoryRefsAndInvariant 验收 3：商品挂多个分类 + 指定主分类 + 指品牌，
// 以及「主分类必属于附属分类」不变的维护规则。
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

	// 显式主分类不在附属列表里 → 自动纳入（不变量成立，不报错）。
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

	// 未显式改主分类，但附属列表被整体替换掉原主分类 → 主分类自动解绑。
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

	// 只改主分类（不给 CategoryIDs）时同样自动纳入。
	upd, err = f.svc.Update(ctx, &productdto.UpdateReq{ProjectID: f.projectID, ID: p.ID, PrimaryCategoryID: &catB.ID})
	if err != nil {
		t.Fatalf("指定主分类失败: %v", err)
	}
	if upd.PrimaryCategoryID != catB.ID || len(upd.CategoryIDs) != 2 {
		t.Fatalf("只改主分类应把它纳入附属列表: primary=%q ids=%v", upd.PrimaryCategoryID, upd.CategoryIDs)
	}

	// 不存在的分类 / 跨工程分类 / 不存在的品牌一律拒绝。
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

// TestTaxonomyPermissionsAndMenusSeeded 迁移 089/090：权限点与后台菜单已 seed
// （未 seed 权限点时 Casbin Enforce 无策略匹配 → 含超管在内全员 403）。
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

// newTaxonomyPageEngine 装配只挂分类 / 品牌 / 商品页的测试引擎（真实 Jet 模板 + 真实 service）。
func newTaxonomyPageEngine(t *testing.T) (*gin.Engine, *attrFixture) {
	t.Helper()
	f := newAttrFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 页头主行动与行内编辑 / 删除按钮按权限渲染（shell.Prepare 读 PermSetKey）：
	// 这条链路不挂鉴权中间件，注入一份权限，让「页面里存在写入口」这类断言保持有效。
	engine.Use(func(c *gin.Context) {
		c.Set(shell.PermSetKey, map[string]bool{
			"product:brand_create": true, "product:brand_update": true, "product:brand_delete": true,
			"product:category_create": true, "product:category_update": true, "product:category_delete": true,
		})
		c.Set(shell.ButtonsKey, map[string]bool{
			"product.brand_create": true, "product.brand_update": true, "product.brand_delete": true,
			"product.category_create": true, "product.category_update": true, "product.category_delete": true,
		})

	})
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	handle := producthttp.NewProductPageHandle(f.svc, f.projects)
	engine.GET("/admin/product-categories", handle.ProductCategoriesPage)
	engine.POST("/admin/product-categories/create", handle.ProductCategoriesCreate)
	engine.POST("/admin/product-categories/delete", handle.ProductCategoriesDelete)
	engine.GET("/admin/product-brands", handle.ProductBrandsPage)
	engine.POST("/admin/product-brands/create", handle.ProductBrandsCreate)
	engine.GET("/admin/products", handle.ProductsPage)
	// 商品级表单（分类 / 品牌 / 标签）已整块移到商品详情页，这里一并注册。
	engine.GET("/admin/products/detail", handle.ProductDetailPage)
	engine.GET("/admin/products/edit", handle.ProductEditPage)
	engine.POST("/admin/products/taxonomy", handle.ProductsTaxonomySet)
	return engine, f
}

// TestTaxonomyAdminPages 验收 4：后台可管理分类与品牌（真实模板渲染 + 表单写链路）。
func TestTaxonomyAdminPages(t *testing.T) {
	engine, f := newTaxonomyPageEngine(t)
	if engine == nil {
		return
	}
	// 表单建分类：顶级 + 子级（子级的 parentId 取自表单下拉里的 id）。
	rec := postForm(engine, "/admin/product-categories/create", url.Values{
		"projectId": {f.projectID}, "name": {"男装"}, "slug": {"men"},
		"seoTitle": {"男装 SEO"}, "description": {"描述"},
	})
	assertJumpOK(t, rec)
	tree, err := f.svc.ListCategories(t.Context(), &productdto.ListCategoryReq{ProjectID: f.projectID})
	if err != nil || len(tree) != 1 {
		t.Fatalf("表单建分类失败: %v %+v", err, tree)
	}
	rootID := tree[0].ID
	rec = postForm(engine, "/admin/product-categories/create", url.Values{
		"projectId": {f.projectID}, "name": {"衬衫"}, "slug": {"shirts"}, "parentId": {rootID},
	})
	assertJumpOK(t, rec)
	rec = httptestGet(engine, "/admin/product-categories?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("分类页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// 行内按钮文案随改造按「动词 + 对象」收紧：行内不再重复实体名，
	// 「删除分类」= 行内「删除」（「新建分类」是页头主行动的按钮文案，保留）。
	for _, want := range []string{"商品分类", "男装", "men", "新建分类", ">删除</button>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("分类根页缺少 %q", want)
		}
	}
	// 列表一次渲染整棵树：顶级行下面直接跟着子级（缩进 + 树枝前缀），
	// 不再「点父级进入下一层」，也不预载成隐藏行等懒加载。
	if !strings.Contains(body, "shirts</code>") || !strings.Contains(body, `<span class="tree-elbow"`) {
		t.Fatal("分类页应把子级直接渲染在顶级下面（缩进成树）")
	}
	if strings.Contains(body, "parentId="+rootID) {
		t.Fatal("逐层导航已退役：列表本身就是树，不该再出现「进入父级」的链接")
	}

	// 品牌：表单建品牌 → 页面能看到 logo / slug / 描述。
	rec = postForm(engine, "/admin/product-brands/create", url.Values{
		"projectId": {f.projectID}, "name": {"山野"}, "slug": {"shanye"},
		"logo": {"/img/logo.svg"}, "seoTitle": {"山野 SEO"},
	})
	assertJumpOK(t, rec)
	rec = httptestGet(engine, "/admin/product-brands?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("品牌页应 200，实际 %d", rec.Code)
	}
	brandBody := rec.Body.String()
	for _, want := range []string{"商品品牌", "山野", "shanye", "/storage/img/logo.svg", "新建品牌", ">删除</button>"} {
		if !strings.Contains(brandBody, want) {
			t.Fatalf("品牌页缺少 %q", want)
		}
	}

	// 商品页：勾选分类 + 选主分类 + 选品牌（表单链路）。
	p, err := f.svc.Create(t.Context(), &productdto.CreateReq{ProjectID: f.projectID, Name: "衬衫 A"})
	if err != nil {
		t.Fatalf("建商品失败: %v", err)
	}
	brands, err := f.svc.ListBrands(t.Context(), &productdto.ListBrandReq{ProjectID: f.projectID})
	if err != nil || len(brands) != 1 {
		t.Fatalf("品牌列表异常: %v %+v", err, brands)
	}
	form := url.Values{"projectId": {f.projectID}, "id": {p.ID}, "brandId": {brands[0].ID}}
	// 只勾父级，主分类却选子级 —— 服务端应把主分类自动纳入挂载列表。
	form.Add("categoryIds", rootID)
	childID := treeIDByName(t, f, "衬衫")
	form.Set("primaryCategoryId", childID)
	rec = postForm(engine, "/admin/products/taxonomy", form)
	assertJumpOK(t, rec)
	// 保存后留在该商品的**编辑页**（表单隐藏域是 id，其值就是商品 id）。
	assertEditRedirect(t, jumpBackHref(t, rec), f.projectID, p.ID)
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

	// 商品 HTML 里能看到分类名与品牌名（验证后台可读回）：表单在**编辑页**，
	// 详情页只读展示（分类名 / 品牌名两处都能看到）。
	rec = httptestGet(engine, "/admin/products/edit?project="+f.projectID+"&product="+p.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("商品编辑页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	pageBody := rec.Body.String()
	// 编辑页是一个表单一次保存：分类与品牌随「保存」一起提交（没有分节按钮）。
	for _, want := range []string{"分类与品牌", "山野", "衬衫", `name="primaryCategoryId"`, `name="brandId"`} {
		if !strings.Contains(pageBody, want) {
			t.Fatalf("商品编辑页缺少 %q", want)
		}
	}
}

// httptestGet 发一个 GET 请求（页面渲染断言用）。
func httptestGet(engine *gin.Engine, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// treeIDByName 在分类树里按名字找 id（测试内的小工具）。
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
