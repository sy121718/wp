package producthttp

// product_page_render_test.go — 商品后台页渲染测试的共用辅助 + 整页 GET 失败出口的用例。
//
// 前两个辅助函数从 dashboard/inbound/http/article_page_render_test.go 搬迁：那两个辅助
// （layout 数据补齐 + 真实 Jet 渲染一个后台模板）本身与文章无关，商品页渲染测试同样需要。
// 模板根目录相对包目录的层数一致（internal/module/<mod>/inbound/http → internal/templates），
// 因此这里的相对路径与 dashboard 侧逐字相同。
//
// 后一组用例（task-28）钉住整页 GET 的**失败出口**：契约见
// docs/rules/template-boundary.md §整页 GET 的失败出口 —— 取数失败的整页 GET 也必须是
// 「这一页」（同一模板 + 页壳 + 侧边菜单 + no-store），状态码按档位说实话
//（主数据没读到 = 500；请求参数不合法 = 400；资源不存在 = 404），
// 且驱动原文（pq: / 约束名 / 表名）只进日志。
//
// 这一组此前是空的：三个整页 GET 的取数失败全部走 shell.PageError —— 浏览器收到的是
// `{"code":500,…}`，DOM 里没有标题栏与侧边菜单，用户只能手改地址栏回去。
// 那类缺陷静态门禁看不见（形状合法），只有把**真实模板渲染出来**才看得见。

import (
	"context"
	"errors"
	"go_wp/internal/adminui"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/adminctx"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productservice "go_wp/internal/module/product/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/pagination"
)

// productPageLayoutData 补齐 layout 需要的键（与线上 adminui.Prepare 注入的一致）。
func productPageLayoutData(base gin.H) gin.H {
	lang := "zh-CN"
	base["csrf_token"] = "test-token"
	base["lang"] = lang
	base["t"] = templates.TranslateFunc(lang)
	base["langs"] = templates.LanguageOptions(lang)
	base["lang_redirect"] = "/admin/products"
	return base
}

// renderAdminTemplate 用真实 Jet 渲染器渲染一个后台模板并返回 HTML。
func renderAdminTemplate(t *testing.T, name string, data gin.H) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 模板根目录相对包目录：internal/module/product/inbound/http → internal/templates。
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/page", func(c *gin.Context) { c.HTML(http.StatusOK, name, data) })
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s 渲染失败，状态 %d，body=%s", name, rec.Code, headOf(rec.Body.String()))
	}
	return rec.Body.String()
}

// —— 整页 GET 失败出口：桩 ——

// productPageProjectsStub 只实现被测路径用到的方法；其余方法因嵌入的接口为 nil 而 panic。
//
// 比「什么都返回零值」的桩安全：静默零值会让断言测到的是桩的行为，而不是页面的行为。
type productPageProjectsStub struct {
	projectcontract.ProjectService
	rows []projectdto.ProjectResp
	err  error
}

func (s productPageProjectsStub) List(context.Context) ([]projectdto.ProjectResp, error) {
	return s.rows, s.err
}

// productPageReaderStub 页面读模型桩（新建页 / 列表页 / 详情页 / 编辑页的取数编排都已在 service）。
//
// 控制器现在只做三步：读参数 → 调读模型 → 渲染。所以这一层的用例验的是**控制器**的三段
// （参数转发 / 渲染 / 档位与 no-store），取数编排本身由 service 的用例
// （product/service/product_page_test.go）与整页链路覆盖
// （public/test/product/feature 有真实库，那边才验得到「合法工程真的取到了属性组」）。
//
// 桩返回的是**完整的视图**（键给齐）：控制器不再补任何键，缺一个就是渲染中断。
type productPageReaderStub struct {
	productcontract.ProductService
	view *productdto.ProductNewPageResp
	err  error
	// reqs 记录控制器转下来的入参：参数读丢了要能看出来。
	reqs []*productdto.ProductNewPageReq

	// 列表页 / 详情页 / 编辑页 / 建表单的桩：同一个接口的另外六个方法，用例各自只填自己那一份。
	// **七个方法都要在** —— 接口是一个整体，少一个就是断言失败、整包 500。
	listView *productdto.ProductListPageResp
	listErr  error
	listReqs []*productdto.ProductListPageReq

	detailView *productdto.ProductDetailPageResp
	detailErr  error
	detailReqs []*productdto.ProductDetailPageReq

	editView *productdto.ProductEditPageResp
	editErr  error
	editReqs []*productdto.ProductEditPageReq

	createFormView *productdto.ProductCreateFormResp
	createFormErr  error
	createFormReqs []*productdto.ProductCreateFormReq

	pricingView *productdto.ProductPricingPageResp
	pricingErr  error
	pricingReqs []*productdto.ProductPricingPageReq

	attributesPageView *productdto.ProductAttributesPageResp
	attributesPageErr  error
	attributesPageReqs []*productdto.ProductAttributesPageReq

	detailTemplatePageView *productdto.ProductDetailTemplatePageResp
	detailTemplatePageErr  error
	detailTemplatePageReqs []*productdto.ProductDetailTemplatePageReq

	bundleView *productdto.ProductBundlePageResp
	bundleErr  error
	bundleReqs []*productdto.ProductBundlePageReq

	categoriesView *productdto.ProductCategoriesPageResp
	categoriesErr  error
	categoriesReqs []*productdto.ProductCategoriesPageReq

	brandsView *productdto.ProductBrandsPageResp
	brandsErr  error
	brandsReqs []*productdto.ProductBrandsPageReq

	tagsView *productdto.ProductTagsPageResp
	tagsErr  error
	tagsReqs []*productdto.ProductTagsPageReq

	// 父级候选片段：控制器只绑定 + 转交，取数与判据在 service —— 桩只记入参。
	parentOptions    productdto.CategoryParentOptionsResp
	parentOptionsErr error
	parentOptionReqs []*productdto.ProductCategoryParentsReq
}

func (s *productPageReaderStub) CategoryParentOptions(_ context.Context, req *productdto.ProductCategoryParentsReq) (productdto.CategoryParentOptionsResp, error) {
	s.parentOptionReqs = append(s.parentOptionReqs, req)
	if s.parentOptions != nil || s.parentOptionsErr != nil {
		return s.parentOptions, s.parentOptionsErr
	}
	return productdto.CategoryParentOptionsResp{"Options": []productdto.PickOptionResp{}}, nil
}

func (s *productPageReaderStub) ProductNewPage(_ context.Context, req *productdto.ProductNewPageReq) (*productdto.ProductNewPageResp, error) {
	s.reqs = append(s.reqs, req)
	return s.view, s.err
}

func (s *productPageReaderStub) ProductListPage(_ context.Context, req *productdto.ProductListPageReq) (*productdto.ProductListPageResp, error) {
	s.listReqs = append(s.listReqs, req)
	return s.listView, s.listErr
}

func (s *productPageReaderStub) ProductDetailPage(_ context.Context, req *productdto.ProductDetailPageReq) (*productdto.ProductDetailPageResp, error) {
	s.detailReqs = append(s.detailReqs, req)
	return s.detailView, s.detailErr
}

func (s *productPageReaderStub) ProductEditPage(_ context.Context, req *productdto.ProductEditPageReq) (*productdto.ProductEditPageResp, error) {
	s.editReqs = append(s.editReqs, req)
	return s.editView, s.editErr
}

func (s *productPageReaderStub) ProductCreateForm(_ context.Context, req *productdto.ProductCreateFormReq) (*productdto.ProductCreateFormResp, error) {
	s.createFormReqs = append(s.createFormReqs, req)
	return s.createFormView, s.createFormErr
}

func (s *productPageReaderStub) ProductPricingPage(_ context.Context, req *productdto.ProductPricingPageReq) (*productdto.ProductPricingPageResp, error) {
	s.pricingReqs = append(s.pricingReqs, req)
	return s.pricingView, s.pricingErr
}

func (s *productPageReaderStub) ProductAttributesPage(_ context.Context, req *productdto.ProductAttributesPageReq) (*productdto.ProductAttributesPageResp, error) {
	s.attributesPageReqs = append(s.attributesPageReqs, req)
	if s.attributesPageErr != nil || s.attributesPageView == nil {
		return s.attributesPageView, s.attributesPageErr
	}
	// 行与两个抽屉初值在生产读模型里装（service.AttrPageRows / AttrCreateDrawerForm）；桩照同一形装一遍，
	// 否则抽屉 include 拿到空 map，模板报 there is no field or method 'IsCreate'。
	s.attributesPageView.Attributes = productservice.AttrPageRows(s.attributesPageView.SelectedProject, s.attributesPageView.AttributeDetails)
	s.attributesPageView.AttrCreateForm = productservice.AttrCreateDrawerForm(s.attributesPageView.SelectedProject)
	return s.attributesPageView, nil
}

func (s *productPageReaderStub) ProductDetailTemplatePage(_ context.Context, req *productdto.ProductDetailTemplatePageReq) (*productdto.ProductDetailTemplatePageResp, error) {
	s.detailTemplatePageReqs = append(s.detailTemplatePageReqs, req)
	return s.detailTemplatePageView, s.detailTemplatePageErr
}

func (s *productPageReaderStub) ProductBundlePage(_ context.Context, req *productdto.ProductBundlePageReq) (*productdto.ProductBundlePageResp, error) {
	s.bundleReqs = append(s.bundleReqs, req)
	return s.bundleView, s.bundleErr
}

func (s *productPageReaderStub) ProductCategoriesPage(_ context.Context, req *productdto.ProductCategoriesPageReq) (*productdto.ProductCategoriesPageResp, error) {
	s.categoriesReqs = append(s.categoriesReqs, req)
	if s.categoriesErr != nil || s.categoriesView == nil {
		return s.categoriesView, s.categoriesErr
	}
	// 生产读模型在末尾装候选 / 新建抽屉 / 行；桩照同一形装一遍，否则行断言会因为桩跳过装配而假红。
	v := s.categoriesView
	v.ListQuery = req.ListQuery
	v.Options = productservice.CategoryPickOptions(productservice.CategoriesInProject(v.PickFlat, v.SelectedProject))
	v.CategoryCreateForm = productservice.CategoryDrawerForm("create", v.SelectedProject, nil, v.Options, nil)
	v.Categories = productservice.CategoryTreeRows(v.SelectedProject, v.CategoryDetails, v.Options, v.Filtered, v.ParentLabels)
	return v, nil
}

func (s *productPageReaderStub) ProductBrandsPage(_ context.Context, req *productdto.ProductBrandsPageReq) (*productdto.ProductBrandsPageResp, error) {
	s.brandsReqs = append(s.brandsReqs, req)
	if s.brandsErr != nil || s.brandsView == nil {
		return s.brandsView, s.brandsErr
	}
	s.brandsView.ListQuery = req.ListQuery
	s.brandsView.Brands = productservice.BrandPageRows(s.brandsView.BrandDetails)
	return s.brandsView, nil
}

func (s *productPageReaderStub) ProductTagsPage(_ context.Context, req *productdto.ProductTagsPageReq) (*productdto.ProductTagsPageResp, error) {
	s.tagsReqs = append(s.tagsReqs, req)
	if s.tagsErr != nil || s.tagsView == nil {
		return s.tagsView, s.tagsErr
	}
	// 行与抽屉初值在生产读模型里装（service.TagPageRows / TagCreateForm）；桩照同一形装一遍，
	// 否则「页面有没有渲染出写入口」这类断言会因为桩跳过装配而假红。
	s.tagsView.Tags = productservice.TagPageRows(s.tagsView.SelectedProject, s.tagsView.TagDetails, s.tagsView.RuleTypes)
	s.tagsView.TagCreateForm = productservice.TagCreateForm(s.tagsView.SelectedProject, s.tagsView.RuleTypes)
	return s.tagsView, nil
}

// productCategoriesPageViewStub 分类页成功档视图（键给齐，与 service 同型）。
func productCategoriesPageViewStub(mutate func(*productdto.ProductCategoriesPageResp)) *productdto.ProductCategoriesPageResp {
	v := &productdto.ProductCategoriesPageResp{
		Projects: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}, SelectedProject: "p1",
		CategoryDetails: []*productdto.CategoryResp{{ID: "c1", ProjectID: "p1", Name: "男装", Slug: "men"}},
		PickFlat:        []*productdto.CategoryResp{{ID: "c1", ProjectID: "p1", Name: "男装", Slug: "men"}},
		Total:           1, Limit: productservice.ProductSubListPageSize,
	}
	if mutate != nil {
		mutate(v)
	}
	v.Pagination = pagination.Page{Total: v.Total, Page: v.Page, Limit: v.Limit}
	return v
}

// productBrandsPageViewStub 品牌页成功档视图。
func productBrandsPageViewStub(mutate func(*productdto.ProductBrandsPageResp)) *productdto.ProductBrandsPageResp {
	v := &productdto.ProductBrandsPageResp{
		Projects: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}, SelectedProject: "p1",
		BrandDetails: []*productdto.BrandResp{{ID: "b1", Name: "品牌一", Slug: "brand-1"}},
		Total:        1, Limit: productservice.ProductSubListPageSize,
	}
	if mutate != nil {
		mutate(v)
	}
	v.Pagination = pagination.Page{Total: v.Total, Page: v.Page, Limit: v.Limit}
	return v
}

// productTagsPageViewStub 标签页成功档视图。
func productTagsPageViewStub(mutate func(*productdto.ProductTagsPageResp)) *productdto.ProductTagsPageResp {
	v := &productdto.ProductTagsPageResp{
		Projects: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}, SelectedProject: "p1",
		TagDetails: []*productdto.TagResp{{ID: "t1", Name: "手工标签", Slug: "manual", Kind: productenums.TagKindManual}},
		Total:      1, Limit: productservice.ProductSubListPageSize,
	}
	if mutate != nil {
		mutate(v)
	}
	v.Pagination = pagination.Page{Total: v.Total, Page: v.Page, Limit: v.Limit}
	return v
}

// productBundlePageViewStub 捆绑配置页的成功档视图（键给齐，与 service 同型）。
//
// Rows 给一行**已配置成员**（不是空白行）：成员表是这一屏里最贵的内容，
// 渲染用例要能看出「行真的装出来了」——只给空白行时，把装配整段删掉也照样绿。
func productBundlePageViewStub(mutate func(*productdto.ProductBundlePageResp)) *productdto.ProductBundlePageResp {
	v := &productdto.ProductBundlePageResp{
		Projects:        []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}},
		SelectedProject: "p1",
		Products:        []*productdto.ProductResp{{ID: "prod1", Name: "商品一"}},
		SelectedProduct: "prod1",
		Detail:          &productdto.BundleConfigResp{ProductID: "prod1", ProductName: "商品一"},
		Skus:            []*productdto.BundleSKUResp{},
		Rows: []productdto.BundleOptionResp{{
			VariantID: "v1", SKULabel: "商品一 · SKU-1", Required: true,
			DefaultQty: 2, MinQty: 1, MaxQty: 5, Available: 9,
			SourceParts: []productdto.BundleSourcePart{{Kind: productdto.BundleSourcePartExternalSKU, Value: "EXT-1"}},
		}},
		CandidateSKUs:    []productdto.BundleCandidateResp{{ID: "v1", Label: "商品一 · SKU-1"}},
		SkuCount:         1,
		MaxOptions:       productdto.BundleMaxOptionsLimit,
		Sources:          []productdto.PickOptionResp{{Value: productenums.BundleSourceProduct, Selected: true}},
		Source:           productenums.BundleSourceProduct,
		SourceProducts:   []productdto.PickOptionResp{{Selected: true, IsNone: true}},
		SourceAttributes: []*productdto.AttributeResp{},
		SourceWarehouses: []productdto.BundleSourceWarehouseGroupResp{},
	}
	if mutate != nil {
		mutate(v)
	}
	return v
}

// detailTemplatePageViewStub 「详情页模板」整页的成功档视图（键给齐，与 service 同型）。
func detailTemplatePageViewStub(mutate func(*productdto.ProductDetailTemplatePageResp)) *productdto.ProductDetailTemplatePageResp {
	v := &productdto.ProductDetailTemplatePageResp{
		Projects:          []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}},
		SelectedProject:   "p1",
		Ready:             true,
		Product:           productdto.DetailTemplateProductResp{ID: "prod-1", Name: "商品一", Slug: "p1"},
		Templates:         []productdto.DetailTemplateOptionResp{{ID: "tpl-1", Name: "默认详情模板", DraftVersion: 3, IsBound: true, IsDefault: true}},
		TemplateCount:     1,
		BoundTemplateID:   "tpl-1",
		DefaultTemplateID: "tpl-1",
		InstanceExists:    true,
		InstanceStatus:    "published",
		InstanceURL:       "/site/p1/p1",
	}
	if mutate != nil {
		mutate(v)
	}
	return v
}

// ListRatings 读模型装配行时会读一次评分（service/product_row.go 的 ratingsOf，issue #33）：
// 嵌入的 ProductService 是 nil 时那一次调用会 panic，所以成功档要给它一个空结果。
func (s *productPageReaderStub) ListRatings(context.Context, *productdto.ListRatingsReq) (*productdto.RatingResp, error) {
	return &productdto.RatingResp{}, nil
}

// productNewPageViewStub 成功档视图（与 service 产出的同型：列表键一律给空列表而不是缺键）。
// mutate 用于把某个用例改成失败档（改档位 / 失败槽）。
func productNewPageViewStub(mutate func(*productdto.ProductNewPageResp)) *productdto.ProductNewPageResp {
	v := &productdto.ProductNewPageResp{
		Projects:            []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}},
		SelectedProject:     "p1",
		WarehouseOptions:    []productdto.WarehouseOptionResp{},
		AttributeOptions:    []productdto.AttributeOptionResp{},
		WarehouseSKUOptions: []productdto.WarehouseSKUGroupResp{},
		Templates:           []productdto.EntityTemplateOptionResp{},
	}
	if mutate != nil {
		mutate(v)
	}
	return v
}

// productListPageViewStub 列表页成功档视图（与 service 产出的同型：列表键一律给空列表
// 而不是缺键 —— 控制器不再补任何键）。
//
// 分页条只由 total / page / limit 三个事实决定，所以「25 条 → 两页」这类用例只改 Total。
func productListPageViewStub(mutate func(*productdto.ProductListPageResp)) *productdto.ProductListPageResp {
	v := &productdto.ProductListPageResp{
		Projects:            []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}},
		SelectedProject:     "p1",
		Page:                1,
		Limit:               productListPageSize,
		WarehouseOptions:    []productdto.WarehouseOptionResp{},
		AttributeOptions:    []productdto.AttributeOptionResp{},
		WarehouseSKUOptions: []productdto.WarehouseSKUGroupResp{},
		RowDetails:          []*productdto.ProductResp{},
		Categories:          []*productdto.CategoryResp{},
		Brands:              []*productdto.BrandResp{},
		Tags:                []*productdto.TagResp{},
		Rules:               []*productdto.PricingRuleTypeResp{},
		Roundings:           []*productdto.PricingRoundingOptionResp{},
	}
	if mutate != nil {
		mutate(v)
	}
	v.Pagination = pagination.Page{Total: v.Total, Page: v.Page, Limit: v.Limit}
	return v
}

// productDetailPageViewStub 详情页视图桩（默认「读到、正常」档；失败 / 空态由用例改）：
// 列表键一律给空列表而不是缺键 —— 控制器不再补任何键。
func productDetailPageViewStub(mutate func(*productdto.ProductDetailPageResp)) *productdto.ProductDetailPageResp {
	v := &productdto.ProductDetailPageResp{
		Projects:         []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}},
		SelectedProject:  "p1",
		ProductID:        "prod-1",
		WarehouseOptions: []productdto.WarehouseOptionResp{},
		Categories:       []*productdto.CategoryResp{},
		Brands:           []*productdto.BrandResp{},
		Tags:             []*productdto.TagResp{},
	}
	if mutate != nil {
		mutate(v)
	}
	// 行由读模型装配（service/product_row.go）：桩按同一口径补一条行，字段与 Detail 对齐。
	// 控制器不再拼行 —— 页面渲染要的 Product 只能从读模型来（缺了就是 nil，模板走空态）。
	if v.HasProduct && v.Detail != nil && v.Product == nil {
		row := productRowForRender()
		row.ID, row.Name, row.Status, row.Slug = v.Detail.ID, v.Detail.Name, v.Detail.Status, v.Detail.Slug
		v.Product = row
	}
	return v
}

// productEditPageViewStub 编辑页视图桩（默认「读到、正常」档；失败 / 空态由用例改）。
// 与详情页同一口径：列表键一律给空列表而不是缺键 —— 控制器不再补任何键。
// Detail 给一条最小商品，行（Product）由本桩按读模型口径补（见 return 前那段）；
// 图集文本 / 价格文本这类表单字段仍由控制器算。
func productEditPageViewStub(mutate func(*productdto.ProductEditPageResp)) *productdto.ProductEditPageResp {
	v := &productdto.ProductEditPageResp{
		Projects:         []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}},
		SelectedProject:  "p1",
		ProductID:        "prod1",
		HasProduct:       true,
		Detail:           &productdto.ProductResp{ID: "prod1", Name: "商品一", Slug: "prod-1"},
		WarehouseOptions: []productdto.WarehouseOptionResp{},
		AttributeOptions: []productdto.AttributeOptionResp{},
		Categories:       []*productdto.CategoryResp{},
		Brands:           []*productdto.BrandResp{},
		Tags:             []*productdto.TagResp{},
	}
	if mutate != nil {
		mutate(v)
	}
	// 行由读模型装配（service/product_row.go）：桩按同一口径补一条行，字段与 Detail 对齐。
	// 控制器不再拼行 —— 页面渲染要的 Product 只能从读模型来（缺了就是 nil，模板走空态）。
	if v.HasProduct && v.Detail != nil && v.Product == nil {
		row := productRowForRender()
		row.ID, row.Name, row.Status, row.Slug = v.Detail.ID, v.Detail.Name, v.Detail.Status, v.Detail.Slug
		v.Product = row
	}
	return v
}

// productCreateFormViewStub 建表单视图桩（默认「取数成功」档；回填值由调用方填）。
// 与其它视图桩同一口径：列表键给空列表而不是缺键。
func productCreateFormViewStub(mutate func(*productdto.ProductCreateFormResp)) *productdto.ProductCreateFormResp {
	v := &productdto.ProductCreateFormResp{
		SelectedProject:     "p1",
		WarehouseOptions:    []productdto.WarehouseOptionResp{},
		AttributeOptions:    []productdto.AttributeOptionResp{},
		WarehouseSKUOptions: []productdto.WarehouseSKUGroupResp{},
	}
	if mutate != nil {
		mutate(v)
	}
	return v
}

// productPricingPageViewStub 定价页视图的最小形状：本页三处取数各自降级、没有失败档，
// 所以默认档就是 200 + 空集合（键要给齐 —— 控制器不再补任何键，缺一个就是渲染中断）。
func productPricingPageViewStub(mutate func(*productdto.ProductPricingPageResp)) *productdto.ProductPricingPageResp {
	view := &productdto.ProductPricingPageResp{
		Projects: []projectdto.ProjectResp{}, PreviewSummary: map[string]any{},
		PreviewRows: []map[string]any{}, History: []map[string]any{},
	}
	if mutate != nil {
		mutate(view)
	}
	return view
}

// productAttributesPageViewStub 属性库视图的最小形状：取数结果给空集合、装配产物留空
// （控制器会补 Title / Menu / Attributes / AttrCreateForm / Pagination / ListQuery）。
func productAttributesPageViewStub(mutate func(*productdto.ProductAttributesPageResp)) *productdto.ProductAttributesPageResp {
	view := &productdto.ProductAttributesPageResp{
		Projects: []projectdto.ProjectResp{}, AttributeDetails: []*productdto.AttributeResp{},
		Limit: 20,
	}
	if mutate != nil {
		mutate(view)
	}
	view.Pagination = pagination.Page{Total: view.Total, Page: view.Page, Limit: view.Limit}
	return view
}

// productPageRequest 挂上三个整页 GET（真 Jet + 真模板）并发一个 GET，返回 recorder。
func productPageRequest(t *testing.T, h *ProductPageHandle, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/products", adminui.PageSlot(ProductListPageTemplate), h.ProductsPage)
	engine.GET("/admin/products/new", adminui.PageSlot(ProductNewPageTemplate), h.ProductNewPage)
	engine.GET("/admin/products/detail", adminui.PageSlot(ProductDetailPageTemplate), h.ProductDetailPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// productPageRequestWith 同 productPageRequest，但允许挂中间件。
//
// 「页面上有没有这个入口」取决于按钮码（模板写 {{if isset(.Buttons["product.update"])}}），
// 而按钮码来自中间件注入的 adminctx.ButtonsKey —— 不挂就一律渲染成「无权限」，
// 于是断言「抽屉在不在」的测试会永远看不到抽屉。
func productPageRequestWith(t *testing.T, h *ProductPageHandle, target string, mw ...gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	for _, m := range mw {
		engine.Use(m)
	}
	engine.GET("/admin/products", adminui.PageSlot(ProductListPageTemplate), h.ProductsPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// productPageRequestFor 把**指定路径**挂到一条最小引擎上并发一个 GET（真 Jet 渲染器）。
//
// 与 productPageRequest 同一手法，区别只在注册的路径：属性页等已下沉的屏不在那三条固定路由里。
func productPageRequestFor(t *testing.T, h *ProductPageHandle, route, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	// 按路径分派到对应的整页 handler：调用点只给「这是哪一页」，不必再各自搭一个引擎。
	switch route {
	case "/admin/product-attributes":
		engine.GET(route, adminui.PageSlot(ProductAttributesPageTemplate), h.ProductAttributesPage)
	case "/admin/product-categories":
		engine.GET(route, adminui.PageSlot(ProductCategoriesPageTemplate), h.ProductCategoriesPage)
	case "/admin/product-brands":
		engine.GET(route, adminui.PageSlot(ProductBrandsPageTemplate), h.ProductBrandsPage)
	case "/admin/product-tags":
		engine.GET(route, adminui.PageSlot(ProductTagsPageTemplate), h.ProductTagsPage)
	default:
		t.Fatalf("productPageRequestFor 不认识这条路由 %q", route)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// assertProductJSONFailure 页面 GET 失败的唯一形状：{code,message,data} + 真实状态码。
//
// 页面失败不再渲染「同一页 + 失败槽」——模板里没有失败分支，出口是 adminui.Page 的
// JSON 分支（业务错误 400 / 其余 500）。判据三件：状态码、Content-Type、message 文案；
// 另外钉住两件不该出现的：HTML 页壳与驱动原文。
func assertProductJSONFailure(t *testing.T, step string, rec *httptest.ResponseRecorder, wantStatus int, wantText string) string {
	t.Helper()
	body := rec.Body.String()
	if rec.Code != wantStatus {
		t.Fatalf("%s：状态码应为 %d，实际 %d，body=%s", step, wantStatus, rec.Code, headOf(body))
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("%s：失败出口必须是 JSON，Content-Type=%q", step, ct)
	}
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("%s：body 不是 {code,message,data}：%v，body=%s", step, err, headOf(body))
	}
	if !strings.Contains(resp.Message, wantText) {
		t.Fatalf("%s：message 应含 %q，实际 %q", step, wantText, resp.Message)
	}
	if strings.Contains(body, "<html") {
		t.Fatalf("%s：失败出口不该是页面，body=%s", step, headOf(body))
	}
	for _, bad := range []string{"pq:", "SQLSTATE", "does not exist", "duplicate key", "constraint", "relation ", "LoadError", "LoadFailed"} {
		if strings.Contains(body, bad) {
			t.Fatalf("%s：报文里出现了不该出现的内容 %q，body=%s", step, bad, headOf(body))
		}
	}
	return body
}

// headOf 截一段 body 供失败信息阅读（整页 HTML 太长，整串打出来会淹掉断言原因）。
func headOf(body string) string {
	const n = 400
	if len(body) > n {
		return body[:n] + "…"
	}
	return body
}

// —— 整页 GET 失败出口：用例 ——

// TestProductsPageLoadFailureRendersSamePage 主数据没读到：同页完整 HTML + 500 + no-store。
//
// 列表页的取数编排已下沉到 service（读模型）：这里验的是**控制器**拿失败视图之后的出口
// （档位 / no-store / 同页形状），取数失败本身的判据在 service 的用例里。
func TestProductsPageLoadFailureIsJSON(t *testing.T) {
	reader := &productPageReaderStub{
		listView: productListPageViewStub(nil),
		listErr:  errors.New(`pq: relation "products" does not exist`),
	}
	h := &ProductPageHandle{products: reader, reader: reader}
	rec := productPageRequest(t, h, "/admin/products")
	assertProductJSONFailure(t, "列表页主数据失败", rec, http.StatusInternalServerError, "内部错误")
}

// TestProductsPageBadProjectParamIsFullPage 参数级失败：整页 + 400（不是 JSON，也不是裸文本）。
//
// ?project=<不存在的工程 id> 此前会渲染成「这个工程下没有商品」——把「工程不存在」说成了
// 「工程是空的」，运营照着这句话去建商品，就建到另一个工程上了。
// 档位由读模型带出（service 判「工程不存在」= 400），控制器只把它交给渲染出口。
func TestProductsPageBadProjectParamIsFullPage(t *testing.T) {
	reader := &productPageReaderStub{
		listView: productListPageViewStub(nil),
		listErr:  productenums.Err(productenums.ErrInvalidParam),
	}
	h := &ProductPageHandle{products: reader, reader: reader}
	rec := productPageRequest(t, h, "/admin/products?project=does-not-exist")
	assertProductJSONFailure(t, "列表页工程参数不合法", rec, http.StatusBadRequest, "请求参数无效")
}

// TestProductsPageRowDetailFailureIsNotSwallowed 单个商品详情读不出来：整页失败，不静默少一行。
//
// 此前这里是 `if derr != nil { continue }`：页面少一行却仍写着「共 N 条」，用户据此以为
// 商品已经被删了 —— 而它只是这一行读不出来。主数据取不到就不该渲染成一张「完整」的列表。
// 判据现在在 service 侧（逐行取详情失败即整页失败，见 service.ProductListPage），
// 控制器这一层验的是它透出来的档位。
func TestProductsPageRowDetailFailureIsNotSwallowed(t *testing.T) {
	reader := &productPageReaderStub{
		listView: productListPageViewStub(func(v *productdto.ProductListPageResp) { v.Total = 1 }),
		listErr:  errors.New(`pq: canceling statement due to statement timeout`),
	}
	h := &ProductPageHandle{products: reader, reader: reader}
	rec := productPageRequest(t, h, "/admin/products?project=p1")
	assertProductJSONFailure(t, "列表页某一行详情失败", rec, http.StatusInternalServerError, "内部错误")
}

// TestProductNewPageLoadFailureRendersSamePage 新建页主数据没读到：同页 + 500 + no-store。
func TestProductNewPageLoadFailureRendersSamePage(t *testing.T) {
	reader := &productPageReaderStub{
		view: productNewPageViewStub(func(v *productdto.ProductNewPageResp) {
		}),
		err: errors.New(`pq: relation "products" does not exist`),
	}
	h := &ProductPageHandle{projects: productPageProjectsStub{}, products: reader, reader: reader}
	rec := productPageRequest(t, h, "/admin/products/new")
	assertProductJSONFailure(t, "新建页主数据失败", rec, http.StatusInternalServerError, "内部错误")
}

// TestProductDetailPageFailureLadder 详情页四档：不存在 / 没给参数 / 参数不合法 / 读不出来。
//
// 四档此前是同一张脸：`derr == nil && detail != nil` 把「读不出来」一起吞成了
// 「商品不存在」空态（200、可缓存）。现在状态码分开说实话。
func TestProductDetailPageFailureLadder(t *testing.T) {
	projectRows := []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}
	cases := []struct {
		name       string
		target     string
		readerErr  error // 读模型抛的错误：档位由它在声明处带出（404 / 400 / 500）
		wantStatus int
		wantText   string // JSON 报文里应出现的中文兜底
	}{
		{
			name:       "没给 product 参数 → 404",
			target:     "/admin/products/detail",
			readerErr:  productenums.Err(productenums.ErrNotFound),
			wantStatus: http.StatusNotFound,
			wantText:   "商品或变体不存在",
		},
		{
			name:       "业务上不存在 → 404",
			target:     "/admin/products/detail?project=p1&product=prod-1",
			readerErr:  productenums.Err(productenums.ErrNotFound),
			wantStatus: http.StatusNotFound,
			wantText:   "商品或变体不存在",
		},
		{
			name:       "参数不合法 → 400",
			target:     "/admin/products/detail?project=p1&product=not-a-uuid",
			readerErr:  productenums.Err(productenums.ErrInvalidParam),
			wantStatus: http.StatusBadRequest,
			wantText:   "请求参数无效",
		},
		{
			name:       "读不出来 → 500（不是「不存在」）",
			target:     "/admin/products/detail?project=p1&product=prod-1",
			readerErr:  errors.New(`pq: relation "products" does not exist`),
			wantStatus: http.StatusInternalServerError,
			wantText:   "内部错误",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &ProductPageHandle{
				projects: productPageProjectsStub{rows: projectRows},
				// 取数编排已下沉到 service（读模型）：桩给**读模型的产出**（档位 + 是否抛错），
				// 不再 mock Get / ListCategories 这些方法（它们已不在这条渲染路径上）。
				products: &productPageReaderStub{
					detailView: productDetailPageViewStub(func(v *productdto.ProductDetailPageResp) {
					}),
					detailErr: tc.readerErr,
				}, reader: &productPageReaderStub{
					detailView: productDetailPageViewStub(func(v *productdto.ProductDetailPageResp) {
					}),
					detailErr: tc.readerErr,
				},
			}
			rec := productPageRequest(t, h, tc.target)
			body := assertProductJSONFailure(t, tc.name, rec, tc.wantStatus, tc.wantText)
			// 两档的文案不能互相串：404 说「商品不存在」，500 只给归口（原文只进日志）。
			if tc.wantStatus == http.StatusNotFound && strings.Contains(body, "内部错误") {
				t.Fatalf("%s：404 档落成了归口错误 —— 声明处漏了 404 状态", tc.name)
			}
			if tc.wantStatus == http.StatusInternalServerError && strings.Contains(body, "商品不存在") {
				t.Fatalf("%s：500 档说成了「商品不存在」—— 读不出来被吞成了不存在", tc.name)
			}
		})
	}
}

// TestProductDetailPageRendersProductWhenDetailReadable 成功档回归：详情取到了就正常渲染。
//
// 只断言「不再是失败页」这一层：整页 200、页壳完整、失败槽与 404 空态都不出现。
// （字段级明细由 ProductRowResp ↔ 模板的对账判据覆盖，见 product_row_template_test.go；
// 这里要钉的是失败出口没有把成功路径一起改坏。）
func TestProductDetailPageRendersProductWhenDetailReadable(t *testing.T) {
	h := &ProductPageHandle{
		projects: productPageProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
		products: &productPageReaderStub{detailView: productDetailPageViewStub(func(v *productdto.ProductDetailPageResp) {
			v.HasProduct = true
			v.Detail = &productdto.ProductResp{ID: "prod-1", Name: "测试商品", Status: "active", Type: "variant"}
		})}, reader: &productPageReaderStub{detailView: productDetailPageViewStub(func(v *productdto.ProductDetailPageResp) {
			v.HasProduct = true
			v.Detail = &productdto.ProductResp{ID: "prod-1", Name: "测试商品", Status: "active", Type: "variant"}
		})},
	}
	rec := productPageRequest(t, h, "/admin/products/detail?project=p1&product=prod-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("详情读到时应 200，实际 %d，body=%s", rec.Code, headOf(rec.Body.String()))
	}
	if got := rec.Header().Get("Cache-Control"); got == "no-store" {
		t.Fatalf("成功页不该被标成 no-store（那是失败页的档）")
	}
	body := rec.Body.String()
	for _, want := range []string{"<html", "</html>", "测试商品"} {
		if !strings.Contains(body, want) {
			t.Fatalf("成功页缺 %q，body=%s", want, headOf(body))
		}
	}
	for _, bad := range []string{"内部错误", "商品或变体不存在"} {
		if strings.Contains(body, bad) {
			t.Fatalf("成功页出现了失败档文案 %q，body=%s", bad, headOf(body))
		}
	}
}

// TestProductsPageSuccessRendersPage 成功档回归（列表页）：没有任何工程时仍是完整页面。
//
// 与失败档的区别要能一眼看出来：200、不标 no-store、失败卡不出现、页壳完整。
// 这里刻意用「零工程」这个真实空态（不是「没读到」）覆盖「空态不是失败」这一条。
func TestProductsPageSuccessRendersPage(t *testing.T) {
	h := &ProductPageHandle{
		products: &productPageReaderStub{listView: productListPageViewStub(func(v *productdto.ProductListPageResp) {
			// 「零工程」这个真实空态：不是「没读到」，是读到了、一个都没有。
			v.Projects = []projectdto.ProjectResp{}
			v.SelectedProject = ""
		})}, reader: &productPageReaderStub{listView: productListPageViewStub(func(v *productdto.ProductListPageResp) {
			// 「零工程」这个真实空态：不是「没读到」，是读到了、一个都没有。
			v.Projects = []projectdto.ProjectResp{}
			v.SelectedProject = ""
		})},
	}
	rec := productPageRequest(t, h, "/admin/products")
	if rec.Code != http.StatusOK {
		t.Fatalf("空工程是空态不是失败，应 200，实际 %d，body=%s", rec.Code, headOf(rec.Body.String()))
	}
	if got := rec.Header().Get("Cache-Control"); got == "no-store" {
		t.Fatalf("成功页不该标 no-store（那是失败页的档）")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("成功页页壳不完整，body=%s", headOf(body))
	}
	if strings.HasPrefix(strings.TrimSpace(body), "{") {
		t.Fatalf("成功页输出了 JSON 报文，body=%s", headOf(body))
	}
}

// TestProductsListDeleteUsesJSONWriteBase 列表行的删除入口走 ui/write-base.js 基座。
//
// 迁移前是 method="post" action="/admin/products/delete"（页面 POST → 提示页）；迁移后是
// data-write-api + data-write="single" + 点名 projectId。判据取**两者互斥**：
// 两个同时在就是半迁移，而按钮 form="<id>" 指到哪个表单都不对劲（一个发 JSON、一个发表单）。
// 点名的 projectId 单独判：service 的 resolveProjectID 在它为空时只肯走「唯一工程」兜底，
// 多工程部署下删除直接回 ErrInvalidParam —— 那是「点了没反应」而不是报错的那种故障。
func TestProductsListDeleteUsesJSONWriteBase(t *testing.T) {
	view := func() *productdto.ProductListPageResp {
		return productListPageViewStub(func(v *productdto.ProductListPageResp) {
			v.Products = []productdto.ProductRowResp{{ID: "prod-1", Name: "测试商品", SKUCode: "SKU-1"}}
		})
	}
	h := &ProductPageHandle{
		products: &productPageReaderStub{listView: view()},
		reader:   &productPageReaderStub{listView: view()},
	}
	rec := productPageRequest(t, h, "/admin/products")
	if rec.Code != http.StatusOK {
		t.Fatalf("列表页应 200，实际 %d，body=%s", rec.Code, headOf(rec.Body.String()))
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-write-url="/api/product/delete"`,
		`data-write-reload`,
		"data-write-host",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("删除入口缺少 %q（删除未接入 JSON 写基座）；body=%s", want, headOf(body))
		}
	}
	if strings.Contains(body, `action="/admin/products/delete"`) {
		t.Fatal("删除入口仍是页面 POST（半迁移）：应只剩 data-write-api 一种提交方式")
	}
}

// TestProductsListBulkDeleteUsesJSONWriteBase 批量删除走 ui/write-base.js 基座。
//
// 与单行删除是同一条迁移的两半：单行成功后整页刷新（列表派生列由服务端重算），
// 批量删完要**就地**告诉操作者「删了几条、哪几条没删掉、为什么」—— 旧通道是整页提示页，
// 一次只能给一句汇总。判据：「两者互斥」（data-write-api 与旧 action 不能同时出现）
// + 三个关键声明（mode=bulk 决定读 data.items、extra 决定 projectId 会不会被带上）。
func TestProductsListBulkDeleteUsesJSONWriteBase(t *testing.T) {
	view := func() *productdto.ProductListPageResp {
		return productListPageViewStub(func(v *productdto.ProductListPageResp) {
			v.Products = []productdto.ProductRowResp{{ID: "prod-1", Name: "测试商品", SKUCode: "SKU-1"}}
		})
	}
	h := &ProductPageHandle{
		products: &productPageReaderStub{listView: view()},
		reader:   &productPageReaderStub{listView: view()},
	}
	rec := productPageRequest(t, h, "/admin/products")
	if rec.Code != http.StatusOK {
		t.Fatalf("列表页应 200，实际 %d，body=%s", rec.Code, headOf(rec.Body.String()))
	}
	body := rec.Body.String()
	for _, want := range []string{
		`data-write-url="/api/product/bulk-delete"`,
		`data-write-reload`,
		`data-write-extra="projectId"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("批量删除入口缺少 %q（批量删除未接入 JSON 写基座）；body=%s", want, headOf(body))
		}
	}
	if strings.Contains(body, `action="/admin/products/bulk-delete"`) {
		t.Fatal("批量删除入口仍是页面 POST（半迁移）：应只剩 data-write-api 一种提交方式")
	}
}

// TestProductsListBulkPricingUsesJSONWriteBase 按规则改价走 ui/write-base.js 基座。
//
// 这条表单与其它批量动作不同：它在抽屉里（<template> 克隆到 document 级），勾选框却在
// 批量表单里。所以除了 mode=bulk，还必须声明 data-write-ids-scope —— 少了它基座在本表单内
// 收 ids 恒为空，表现是「点了应用调价什么也没发生」。
func TestProductsListBulkPricingUsesJSONWriteBase(t *testing.T) {
	view := func() *productdto.ProductListPageResp {
		return productListPageViewStub(func(v *productdto.ProductListPageResp) {
			v.Products = []productdto.ProductRowResp{{ID: "prod-1", Name: "测试商品", SKUCode: "SKU-1"}}
		})
	}
	h := &ProductPageHandle{
		products: &productPageReaderStub{listView: view()},
		reader:   &productPageReaderStub{listView: view()},
	}
	// 改价抽屉在 {{if canUpdate}} 里：不注入按钮码，整段模板不渲染，断言会假绿。
	rec := productPageRequestWith(t, h, "/admin/products", func(c *gin.Context) {
		c.Set(adminctx.ButtonsKey, map[string]bool{"product.update": true})
	})
	body := rec.Body.String()
	for _, want := range []string{
		// 批量调价：提交走 htmx，但 ids 要从 data-list-bulk（表单之外）收 ——
		// data-write-ids-scope 由 htmx_form_body.js 实现。
		// 判据是「必须有 ids 作用域声明」，不是「必须用哪套基座」。
		`data-write-url="/api/product/bulk-pricing"`,
		`data-write-ids-scope="[data-list-bulk]"`,
		"data-list-bulk",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("按规则改价缺少 %q（未接入 JSON 写基座或 ids 作用域没声明）；body=%s", want, headOf(body))
		}
	}
	// 反向：写表单上不能再挂 hx-post —— htmx 与写基座都接 submit，
	// 同时挂会一次点击发两个请求（写动作就是建两条记录）。
	if strings.Contains(body, `hx-post="/api/product/bulk-pricing"`) {
		t.Fatal("按规则改价在写表单上又挂了 hx-post —— 会双提交")
	}
}

// TestProductNewPageUnknownProjectIsBadRequest 新建页的参数档：?project=<不存在的 id>。
//
// 出口仍是同一档：整页 + 400 + no-store（页壳完整、不泄漏驱动原文）。
// 「非法工程**任何派生取数都不发**」这条判据随取数编排一起下沉到 service ——
// 控制器已经看不到取数了，在它这层断言只会测到桩的行为；
// 现在由 product/service/product_page_test.go 的
// TestProductNewPageInvalidProjectStopsBeforeFanout 承担（桩端口计调用次数）。
func TestProductNewPageUnknownProjectIsBadRequest(t *testing.T) {
	reader := &productPageReaderStub{
		view: productNewPageViewStub(func(v *productdto.ProductNewPageResp) {
			v.SelectedProject = "does-not-exist"
		}),
		err: productenums.Err(productenums.ErrInvalidParam),
	}
	h := &ProductPageHandle{projects: productPageProjectsStub{}, products: reader, reader: reader}
	rec := productPageRequest(t, h, "/admin/products/new?project=does-not-exist")
	assertProductJSONFailure(t, "新建页 ?project=不存在的 id", rec, http.StatusBadRequest, "请求参数无效")
}

// TestProductNewPageForwardsProjectToReader 控制器把 query 里的工程原样交给读模型。
//
// 「合法工程时派生取数必须发出」那一半随之沉到 service（同上）：控制器这一层能验的、
// 也是它现在唯一的职责 —— **参数有没有被正确转下去**。没有这条，控制器把 project 读丢
// （永远读默认工程）也能全绿，而症状是「切换工程后新建页仍然是第一个工程的数据」。
func TestProductNewPageForwardsProjectToReader(t *testing.T) {
	reader := &productPageReaderStub{view: productNewPageViewStub(nil)}
	h := &ProductPageHandle{projects: productPageProjectsStub{}, products: reader, reader: reader}
	rec := productPageRequest(t, h, "/admin/products/new?project=p1")
	if rec.Code != http.StatusOK {
		t.Fatalf("合法工程应 200，实际 %d，body=%s", rec.Code, headOf(rec.Body.String()))
	}
	if len(reader.reqs) != 1 {
		t.Fatalf("控制器应调一次读模型，实际 %d 次", len(reader.reqs))
	}
	if got := reader.reqs[0].ProjectID; got != "p1" {
		t.Fatalf("工程 id 应原样转给读模型，实际 %q", got)
	}
}

// TestProductNewPageSuccessRendersPage 成功档回归（新建页）：同一份模板 + 同一份页数据。
func TestProductNewPageSuccessRendersPage(t *testing.T) {
	reader := &productPageReaderStub{view: productNewPageViewStub(nil)}
	h := &ProductPageHandle{projects: productPageProjectsStub{}, products: reader, reader: reader}
	rec := productPageRequest(t, h, "/admin/products/new")
	if rec.Code != http.StatusOK {
		t.Fatalf("新建页读到数据时应 200，实际 %d，body=%s", rec.Code, headOf(rec.Body.String()))
	}
	if got := rec.Header().Get("Cache-Control"); got == "no-store" {
		t.Fatalf("成功页不该标 no-store（那是失败页的档）")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "</html>") {
		t.Fatalf("成功页页壳不完整，body=%s", headOf(body))
	}
	if strings.HasPrefix(strings.TrimSpace(body), "{") {
		t.Fatalf("成功页输出了 JSON 报文，body=%s", headOf(body))
	}
}
