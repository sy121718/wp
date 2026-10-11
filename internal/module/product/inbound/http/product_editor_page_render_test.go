package producthttp

// product_editor_page_render_test.go — 组合（bundle）/ 编辑（edit）/ 详情页模板（template）
// 三个整页 GET 的失败出口（task-38）。
//
// 与 product_page_render_test.go（task-28）、product_taxonomy_page_render_test.go（task-37）
// 同一族判据，契约见 docs/rules/template-boundary.md §整页 GET 的失败出口：取数失败的整页 GET
// **也必须是「这一页」**（同一份模板 + 页壳 + 侧边菜单 + `Cache-Control: no-store`），
// 状态码按档位说实话：
//
//	主数据没读到            → 500
//	?project=<不存在的 id>  → 400（判据是**本次请求已加载的工程集合**；工程被删除或并发变化
//	                          会让旧链接里的工程失效，那时 400 是准确的事实）
//	商品不在库里            → 404（只有业务 key = ErrNotFound 才是「不存在」，
//	                          驱动错误一律 500 —— 不把服务错误误报成「商品被删了」）
//
// 这一批的额外难点是**共享 render 函数**：renderBundlePage 同时服务整页
//（ProductBundlePage）与 htmx 片段（ProductBundleSave 写失败重渲染）。片段档必须仍是片段 ——
// 整页失败页塞进片段槽会把外层结构（页头、侧边菜单）一起换掉，所以这里用真实 Jet 把两种边界
// 分开钉住，并顺带钉住「写失败重渲染不丢用户刚配的成员清单」。
//
// 另外钉住一条极易回退的性质：**失败之后不再发任何派生查询**（此前每一次取数失败都各自
// shell.PageError 出口，后面的分类 / 品牌 / 标签 / 仓库 / 属性 / 模板清单照发不误）。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
)

// editorDriverText 驱动原文（驱动前缀 + 表名 + SQLSTATE）：只允许进日志，绝不允许出现在 HTML 里
// （productPageLeakMarkers 逐条盯住这里面的片段）。
const editorDriverText = `pq: relation "products" does not exist (SQLSTATE 42P01)`

// editorStub 只实现三页真正用到的方法；其余方法因嵌入的接口为 nil 而 panic
// （静默零值会让断言测到桩的行为，而不是页面的行为）。calls 让「失败后零派生查询」可断言。
type editorStub struct {
	productcontract.ProductService

	getRes *productdto.ProductResp
	getErr error
	// getNilNoErr 让 Get 返回 (nil, nil)：契约上不该出现，页面不能把它当成功渲染。
	getNilNoErr bool

	categoriesErr error
	brandsErr     error
	tagsErr       error
	attributesErr error
	ratingsErr    error
	listErr       error
	configErr     error
	skusErr       error
	setErr        error

	categories []*productdto.CategoryResp
	brands     []*productdto.BrandResp
	tags       []*productdto.TagResp
	attributes []*productdto.AttributeResp
	config     *productdto.BundleConfigResp
	skus       []*productdto.BundleSKUResp

	// 编辑页已改走读模型（ProductPageReader）：这一页的取数编排与档位都在 service，
	// 桩只负责交回一份视图 / 一个失败 —— 原来的 getErr / categoriesErr 那一族对编辑页不再生效。
	editView *productdto.ProductEditPageView
	editErr  error

	// 「详情页模板」整页同样改走读模型（2026-10-11，第九个方法）。
	detailTemplatePageView *productdto.ProductDetailTemplatePageView
	detailTemplatePageErr  error

	// 捆绑配置页也改走读模型（第十个方法）。
	bundlePageView *productdto.ProductBundlePageView
	bundlePageErr  error

	calls []string
}

func (s *editorStub) record(name string) { s.calls = append(s.calls, name) }

func (s *editorStub) List(context.Context, *productdto.ListReq) ([]*productdto.ProductResp, error) {
	s.record("List")
	return []*productdto.ProductResp{{ID: "prod1", Name: "商品一", Slug: "prod-1"}}, s.listErr
}

func (s *editorStub) Get(context.Context, *productdto.GetReq) (*productdto.ProductResp, error) {
	s.record("Get")
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.getNilNoErr {
		return nil, nil
	}
	return s.getRes, nil
}

// 读模型接口（ProductPageReader）是一个整体：编辑页虽然只用到 ProductEditPage，
// 但**另外几屏的方法必须在**，少一个就是类型断言失败 → 整包走 500 兜底页
// （症状是「控制器根本没被调到」，calls 为空）。这三条在本文件里不会被调用，
// 存在只是为了让 editorStub 满足接口。
func (s *editorStub) ProductNewPage(context.Context, *productdto.ProductNewPageReq) (*productdto.ProductNewPageView, error) {
	s.record("ProductNewPage")
	return productNewPageViewStub(nil), nil
}

func (s *editorStub) ProductListPage(context.Context, *productdto.ProductListPageReq) (*productdto.ProductListPageView, error) {
	s.record("ProductListPage")
	return productListPageViewStub(nil), nil
}

func (s *editorStub) ProductDetailPage(context.Context, *productdto.ProductDetailPageReq) (*productdto.ProductDetailPageView, error) {
	s.record("ProductDetailPage")
	return productDetailPageViewStub(nil), nil
}

func (s *editorStub) ProductCreateForm(context.Context, *productdto.ProductCreateFormReq) (*productdto.ProductCreateFormView, error) {
	s.record("ProductCreateForm")
	return productCreateFormViewStub(nil), nil
}

func (s *editorStub) ProductPricingPage(context.Context, *productdto.ProductPricingPageReq) (*productdto.ProductPricingPageView, error) {
	s.record("ProductPricingPage")
	return productPricingPageViewStub(nil), nil
}

// ProductBundlePage 读模型桩：**按契约**把提交快照渲染回来（Submitted 非 nil 时成员行与
// 配置数值一律取它）。契约本身（快照优先于库里的配置）的判据在真实库的
// public/test/product/feature/product_bundle_page_test.go（那里「非法配置 → 原请求 200 +
// value=9 回填」跑的是真 service）；本桩只保证控制器那一层拿到的是契约形状的视图。
func (s *editorStub) ProductBundlePage(_ context.Context, req *productdto.ProductBundlePageReq) (*productdto.ProductBundlePageView, error) {
	s.record("ProductBundlePage")
	view := s.bundlePageView
	if view == nil {
		view = productBundlePageViewStub(func(v *productdto.ProductBundlePageView) {
			if req != nil {
				v.SelectedProject = req.ProjectID
				v.SelectedProduct = req.ProductID
				if req.Submitted != nil {
					v.Rows = stubSubmittedRows(req.Submitted)
					v.CfgMaxOptions = req.Submitted.MaxOptions
					v.CfgMinTotalQty = req.Submitted.MinTotalQty
					v.CfgMaxTotalQty = req.Submitted.MaxTotalQty
					v.Submitted = true
				}
			}
			if s.bundlePageErr != nil {
				v.HTTPStatus = http.StatusInternalServerError
				v.LoadFailed = true
				v.SaveBlocked = true
			}
		})
	}
	return view, s.bundlePageErr
}

// stubSubmittedRows 把提交快照摊成渲染行，与读模型的装配同形（含**原样占位**的空白行与尾部空槽）。
func stubSubmittedRows(form *productdto.BundleSubmittedForm) []productdto.BundleOptionRow {
	rows := make([]productdto.BundleOptionRow, 0, len(form.VariantIDs)+1)
	hasBlank := false
	for i, rawID := range form.VariantIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			hasBlank = true
		}
		rows = append(rows, productdto.BundleOptionRow{
			VariantID: id, Required: stubAt(form.Required, i) == "1",
			DefaultQty: stubAtoi(form.DefaultQty, i), MinQty: stubAtoi(form.MinQty, i), MaxQty: stubAtoi(form.MaxQty, i),
			SourceKind: stubAt(form.SourceKinds, i), WarehouseID: stubAt(form.WarehouseIDs, i),
			WarehouseSKU: stubAt(form.WarehouseSKUs, i), ExternalSKU: stubAt(form.ExternalSKUs, i),
			SourceParts: []productdto.BundleSourcePart{},
		})
	}
	if len(rows) < productdto.BundleMaxOptionsLimit && !hasBlank {
		rows = append(rows, productdto.BundleOptionRow{
			Required: true, DefaultQty: 1, MinQty: 1, SourceParts: []productdto.BundleSourcePart{},
		})
	}
	return rows
}

func stubAt(values []string, i int) string {
	if i < 0 || i >= len(values) {
		return ""
	}
	return strings.TrimSpace(values[i])
}

func stubAtoi(values []string, i int) int {
	n, err := strconv.Atoi(stubAt(values, i))
	if err != nil {
		return 0
	}
	return n
}

func (s *editorStub) ProductDetailTemplatePage(_ context.Context, req *productdto.ProductDetailTemplatePageReq) (*productdto.ProductDetailTemplatePageView, error) {
	s.record("ProductDetailTemplatePage")
	view := s.detailTemplatePageView
	if view == nil {
		view = detailTemplatePageViewStub(func(v *productdto.ProductDetailTemplatePageView) {
			if req != nil {
				v.SelectedProject = req.ProjectID
				// 菜单入口不带 product：读模型据此置 NoProduct，控制器走引导出口。
				if req.ProductID == "" {
					v.NoProduct = true
					v.Ready = false
				} else {
					v.Product = productdto.DetailTemplateProductView{ID: req.ProductID, Name: "商品一", Slug: "prod-1"}
				}
			}
			if s.detailTemplatePageErr != nil {
				v.HTTPStatus = http.StatusInternalServerError
				v.LoadFailed = true
				v.Ready = false
			}
		})
	}
	return view, s.detailTemplatePageErr
}

func (s *editorStub) ProductAttributesPage(context.Context, *productdto.ProductAttributesPageReq) (*productdto.ProductAttributesPageView, error) {
	s.record("ProductAttributesPage")
	return productAttributesPageViewStub(nil), nil
}

// 分类 / 品牌 / 标签三页的读模型（同为接口的一部分，见上面那段注释）。
func (s *editorStub) ProductCategoriesPage(context.Context, *productdto.ProductCategoriesPageReq) (*productdto.ProductCategoriesPageView, error) {
	s.record("ProductCategoriesPage")
	return productCategoriesPageViewStub(nil), nil
}

func (s *editorStub) ProductBrandsPage(context.Context, *productdto.ProductBrandsPageReq) (*productdto.ProductBrandsPageView, error) {
	s.record("ProductBrandsPage")
	return productBrandsPageViewStub(nil), nil
}

func (s *editorStub) ProductTagsPage(context.Context, *productdto.ProductTagsPageReq) (*productdto.ProductTagsPageView, error) {
	s.record("ProductTagsPage")
	return productTagsPageViewStub(nil), nil
}

func (s *editorStub) ProductEditPage(_ context.Context, req *productdto.ProductEditPageReq) (*productdto.ProductEditPageView, error) {
	s.record("ProductEditPage")
	view := s.editView
	if view == nil {
		// 契约：视图恒非 nil（失败也要给齐模板必需键）—— 失败档只置档位，键留桩的默认值。
		view = productEditPageViewStub(func(v *productdto.ProductEditPageView) {
			if req != nil {
				v.ProductID = req.ProductID
				if req.ProjectID != "" {
					v.SelectedProject = req.ProjectID
				}
			}
			if s.editErr != nil {
				v.HTTPStatus = http.StatusInternalServerError
				v.LoadFailed = true
				v.HasProduct = false
				v.Detail = nil
			}
		})
	}
	return view, s.editErr
}

func (s *editorStub) ListCategories(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
	s.record("ListCategories")
	return s.categories, s.categoriesErr
}

func (s *editorStub) ListBrands(context.Context, *productdto.ListBrandReq) ([]*productdto.BrandResp, error) {
	s.record("ListBrands")
	return s.brands, s.brandsErr
}

func (s *editorStub) ListTags(context.Context, *productdto.ListTagReq) ([]*productdto.TagResp, error) {
	s.record("ListTags")
	return s.tags, s.tagsErr
}

func (s *editorStub) ListAttributes(context.Context, *productdto.ListAttributeReq) ([]*productdto.AttributeResp, error) {
	s.record("ListAttributes")
	return s.attributes, s.attributesErr
}

func (s *editorStub) ListRatings(context.Context, *productdto.ListRatingsReq) (*productdto.RatingResp, error) {
	s.record("ListRatings")
	// 读不到评分按「没有评分」处理（页面照常渲染），所以这一条不属于失败档。
	return nil, s.ratingsErr
}

func (s *editorStub) GetBundleConfig(context.Context, *productdto.GetBundleConfigReq) (*productdto.BundleConfigResp, error) {
	s.record("GetBundleConfig")
	if s.configErr != nil {
		return nil, s.configErr
	}
	return s.config, nil
}

func (s *editorStub) ListBundleSKUs(context.Context, *productdto.ListBundleSKUReq) ([]*productdto.BundleSKUResp, error) {
	s.record("ListBundleSKUs")
	return s.skus, s.skusErr
}

func (s *editorStub) SetBundleConfig(context.Context, *productdto.SetBundleConfigReq) (*productdto.BundleConfigResp, error) {
	s.record("SetBundleConfig")
	return nil, s.setErr
}

// editorProjectsStub 工程契约：List 供页面装载，Detail 供 siteurl.DetailPathForProject 派生详情页 URL
// （派生失败会回落到默认模式，所以 Detail 返回 (nil, nil) 就够 —— 这不是本文件要测的判据）。
//
// 不复用 product_page_render_test.go 的 productPageProjectsStub：那个桩只实现 List，
// 被 DetailPathForProject 调用 Detail 时会 panic（嵌入的接口为 nil）。
type editorProjectsStub struct {
	projectcontract.ProjectService
	rows  []projectdto.ProjectResp
	err   error
	calls []string
}

func (s *editorProjectsStub) List(context.Context) ([]projectdto.ProjectResp, error) {
	s.calls = append(s.calls, "List")
	return s.rows, s.err
}

func (s *editorProjectsStub) Detail(context.Context, *projectdto.DetailReq) (*projectdto.ProjectResp, error) {
	s.calls = append(s.calls, "Detail")
	return nil, nil
}

// editorTemplatesStub 详情页模板页的**装配在位**标记。
//
// 这一屏的模板清单取数已下沉到 service 的读模型（这里不再直接 List），所以本桩不再提供
// listErr 之类的失败注入；它只负责让 h.templates 非 nil（否则页面走「未装配」档），
// 并记录调用用于「工程清单失败时模板侧一条都不发」那条反面断言。
type editorTemplatesStub struct {
	contenttemplatecontract.ContentTemplateService
	rows  []*contenttemplatecontract.TemplateResp
	calls []string
}

func (s *editorTemplatesStub) List(context.Context, *contenttemplatecontract.ListReq) ([]*contenttemplatecontract.TemplateResp, error) {
	s.calls = append(s.calls, "List")
	return s.rows, nil
}

func (s *editorTemplatesStub) ResolveTemplate(context.Context, string) (*contenttemplatecontract.ResolvedTemplate, error) {
	s.calls = append(s.calls, "ResolveTemplate")
	return &contenttemplatecontract.ResolvedTemplate{TemplateID: "tpl1"}, nil
}

// editorInstancesStub 实例绑定是辅助信息（读不到就按「未绑定」渲染），记录调用即可。
type editorInstancesStub struct {
	ProductDetailTemplatePort
	calls []string
}

func (s *editorInstancesStub) GetByEntity(context.Context, *presentationcontract.GetByEntityReq) (*presentationcontract.InstanceResp, error) {
	s.calls = append(s.calls, "GetByEntity")
	return nil, nil
}

// editorPage 一页的路径、页面标记（h1 文案，成功与失败档都必须渲染出来）与成功档取数。
type editorPage struct {
	name string
	path string
	// marker 页面上必须出现的字（失败档与成功档共用：失败页也是「这一页」）。
	marker string
	// successCalls 成功档必须发生的取数（反面断言用：没有这条，把取数整段删掉也能绿）。
	successCalls []string
	// breakLoad 让这一页的主数据取数失败。
	breakLoad func(s *editorStub)
	// loadFailCalls 主数据失败时的**全部**调用：失败那一条之后不许再发任何查询。
	loadFailCalls []string
	// skipProjectCases 跳过「工程清单失败」与「未知工程」两条（下同 `?project=<不存在的 id>`）。
	// 编辑页已把取数编排交给读模型：控制器不再自己读工程清单，那两条判据因此落在
	// service（工程清单读失败 / 未知工程的取数与档位都由 ProductEditPage 决定）。
	skipProjectCases bool
}

var editorPages = []editorPage{
	{
		name:   "组合页",
		path:   "/admin/products/bundle?project=p1&product=prod1",
		marker: "捆绑配置",
		// 取数编排（工程清单 → 工程商品 → 配置三档 → 候选 SKU → 来源面板三条）已整体进
		// service 的读模型：控制器只调一次 ProductBundlePage，失败之后不会再发任何派生查询。
		successCalls:  []string{"ProductBundlePage"},
		breakLoad:     func(s *editorStub) { s.bundlePageErr = errors.New(editorDriverText) },
		loadFailCalls: []string{"ProductBundlePage"},
		// 工程清单 / 未知工程两条已随取数编排归 service（同编辑页）。
		skipProjectCases: true,
	},
	{
		name:   "编辑页",
		path:   "/admin/products/edit?project=p1&product=prod1",
		marker: "编辑商品",
		// 编辑页的取数编排与档位都在 service 的读模型里：控制器只调一次 ProductEditPage，
		// 失败之后不会再发任何派生查询（那一条属于 service 的判据，见 product/service 的用例）。
		successCalls:  []string{"ProductEditPage"},
		breakLoad:     func(s *editorStub) { s.editErr = errors.New(editorDriverText) },
		loadFailCalls: []string{"ProductEditPage"},
		// 工程清单 / 未知工程两条已随取数编排归 service。
		skipProjectCases: true,
	},
	{
		name:   "详情页模板",
		path:   "/admin/products/template?project=p1&product=prod1",
		marker: "商品详情页模板",
		// 取数编排（工程清单 → 商品三档 → 模板清单 → 默认 / 绑定标注）已整体进 service 的
		// 读模型：控制器只调一次 ProductDetailTemplatePage，失败之后不会再发任何派生查询。
		successCalls:  []string{"ProductDetailTemplatePage"},
		breakLoad:     func(s *editorStub) { s.detailTemplatePageErr = errors.New(editorDriverText) },
		loadFailCalls: []string{"ProductDetailTemplatePage"},
		// 工程清单 / 未知工程两条已随取数编排归 service（同编辑页）。
		skipProjectCases: true,
	},
}

// editorEnv 三页共用的环境：一个工程 p1、一个商品 prod1、一份可读的捆绑配置，
// 并注入模板 / 实例端口（详情页模板页缺这两个会走「未装配」档，那是另一条判据）。
func editorEnv() (*productPageHandle, *editorStub, *editorTemplatesStub, *editorInstancesStub) {
	stub := &editorStub{
		getRes: &productdto.ProductResp{ID: "prod1", Name: "商品一", Slug: "prod-1"},
		config: &productdto.BundleConfigResp{ProductID: "prod1", ProductName: "商品一", BasePrice: 9.9},
	}
	tpls := &editorTemplatesStub{rows: []*contenttemplatecontract.TemplateResp{{ID: "tpl1", Name: "默认模板"}}}
	insts := &editorInstancesStub{}
	h := &productPageHandle{
		projects:  &editorProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
		products:  stub,
		templates: tpls,
		instances: insts,
	}
	return h, stub, tpls, insts
}

// editorRequest 真实引擎 + 真实 Jet 渲染三个整页 GET（失败档只有把模板渲染出来才看得见）。
func editorRequest(t *testing.T, h *productPageHandle, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/products/bundle", h.ProductBundlePage)
	engine.GET("/admin/products/edit", h.ProductEditPage)
	engine.GET("/admin/products/template", h.ProductDetailTemplatePage)
	engine.POST("/admin/products/bundle/save", h.ProductBundleSave)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// editorSaveRequest 走真实的保存入口（写失败原地重渲染）：htmx 档换片段、原生档换整页。
func editorSaveRequest(t *testing.T, h *productPageHandle, htmx bool) *httptest.ResponseRecorder {
	return editorSaveRequestWith(t, h, htmx, editorSubmittedForm())
}

// editorSubmittedForm 两个成员 + 三个配置数值 + 来源字段：重渲染必须把它们**逐字**带回，
// 所以这里刻意让每一列的值都不相同（同值会让「少回填一列」测不出来）。
func editorSubmittedForm() url.Values {
	form := url.Values{}
	form.Set("csrf_token", "t")
	form.Set("productId", "prod1")
	form.Set("projectId", "p1")
	form.Set("maxOptions", "7")
	form.Set("minTotalQty", "2")
	form.Set("maxTotalQty", "9")
	// 一个成员一行：成员清单就是这几个并行数组（顺序即 DOM 顺序）。
	for _, m := range []struct{ id, required, def, min, max, kind, warehouse, wsku, esku string }{
		{"v1", "1", "2", "1", "3", "BundleSourceProduct", "", "", "SKU-EXT-1"},
		{"v2", "0", "1", "0", "5", "BundleSourceWarehouse", "wh1", "WSKU-1", ""},
	} {
		form.Add("variantId", m.id)
		form.Add("required", m.required)
		form.Add("defaultQty", m.def)
		form.Add("minQty", m.min)
		form.Add("maxQty", m.max)
		form.Add("sourceKind", m.kind)
		form.Add("memberWarehouseId", m.warehouse)
		form.Add("memberWarehouseSku", m.wsku)
		form.Add("memberExternalSku", m.esku)
	}
	return form
}

func editorSaveRequestWith(t *testing.T, h *productPageHandle, htmx bool, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.POST("/admin/products/bundle/save", h.ProductBundleSave)
	req := httptest.NewRequest(http.MethodPost, "/admin/products/bundle/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// TestEditorPagesLoadFailureRendersSamePage 主数据没读到：同页完整 HTML + 500 + no-store + 零派生查询。
func TestEditorPagesLoadFailureRendersSamePage(t *testing.T) {
	for _, p := range editorPages {
		t.Run(p.name, func(t *testing.T) {
			h, stub, _, _ := editorEnv()
			p.breakLoad(stub)
			rec := editorRequest(t, h, p.path)
			assertProductPageFailure(t, p.name+"主数据失败", rec, http.StatusInternalServerError, p.marker)
			assertCalls(t, p.name+"主数据失败", stub.calls, p.loadFailCalls)
		})
	}
}

// TestEditorPagesProjectListFailureSkipsDerivedQueries 工程清单本身就失败：500 + 零派生查询。
func TestEditorPagesProjectListFailureSkipsDerivedQueries(t *testing.T) {
	for _, p := range editorPages {
		if p.skipProjectCases {
			continue
		}
		t.Run(p.name, func(t *testing.T) {
			h, stub, tpls, insts := editorEnv()
			h.projects = &editorProjectsStub{err: errors.New(editorDriverText)}
			rec := editorRequest(t, h, p.path)
			assertProductPageFailure(t, p.name+"工程清单失败", rec, http.StatusInternalServerError, p.marker)
			assertCalls(t, p.name+"工程清单失败", stub.calls, nil)
			assertCalls(t, p.name+"工程清单失败", tpls.calls, nil)
			assertCalls(t, p.name+"工程清单失败", insts.calls, nil)
		})
	}
}

// TestEditorPagesUnknownProjectIsBadRequest ?project 不在本次请求加载到的工程集合里：400 + 零派生查询。
func TestEditorPagesUnknownProjectIsBadRequest(t *testing.T) {
	for _, p := range editorPages {
		if p.skipProjectCases {
			continue
		}
		t.Run(p.name, func(t *testing.T) {
			h, stub, _, _ := editorEnv()
			rec := editorRequest(t, h, replaceProject(p.path, "does-not-exist"))
			assertProductPageFailure(t, p.name+"未知工程", rec, http.StatusBadRequest, "请求参数无效")
			assertCalls(t, p.name+"未知工程", stub.calls, nil)
		})
	}
}

// TestEditorPagesSuccessRendersPage 成功档：200、完整页壳、不出失败槽、不该带 no-store，且该发的取数真的发了。
func TestEditorPagesSuccessRendersPage(t *testing.T) {
	for _, p := range editorPages {
		t.Run(p.name, func(t *testing.T) {
			h, stub, _, _ := editorEnv()
			rec := editorRequest(t, h, p.path)
			body := rec.Body.String()
			if rec.Code != http.StatusOK {
				t.Fatalf("%s：成功档应为 200，实际 %d，body=%s", p.name, rec.Code, headOf(body))
			}
			if got := rec.Header().Get("Cache-Control"); got == "no-store" {
				t.Fatalf("%s：成功档不该标 no-store（那是失败页的重试语义）", p.name)
			}
			for _, want := range []string{"<html", "</body>", "</html>", p.marker} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s：成功页缺 %q，body=%s", p.name, want, headOf(body))
				}
			}
			if strings.Contains(body, "这一页的数据没能读出来") {
				t.Fatalf("%s：成功档不该渲染失败槽，body=%s", p.name, headOf(body))
			}
			for _, want := range p.successCalls {
				if !containsCall(stub.calls, want) {
					t.Fatalf("%s：成功档必须调用 %s（否则「跳过所有取数」也能让失败档全绿），实际 %v",
						p.name, want, stub.calls)
				}
			}
		})
	}
}

// TestBundleConfigErrorGrades 捆绑配置读取的错误分档**由读模型给出**，控制器只原样透出。
//
// 「哪种错误落哪一档」（不存在 404 / 参数非法 400 / 其余 500）的判据已经下沉到
// service/product_bundle_page.go（对着真实 PG 的用例在 public/test/product/feature），
// 这里验的是控制器那一层的两件事：不改判档位、按这一次的 err 归口当前语言的失败槽。
func TestBundleConfigErrorGrades(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		status   int
		wantText string
	}{
		{"商品不存在", errors.New(productenums.ErrNotFound), http.StatusNotFound, "商品或变体不存在"},
		{"参数非法", errors.New(productenums.ErrInvalidParam), http.StatusBadRequest, "请求参数无效"},
		{"驱动错误", errors.New(editorDriverText), http.StatusInternalServerError, "这一页的数据没能读出来"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, stub, _, _ := editorEnv()
			stub.bundlePageView = productBundlePageViewStub(func(v *productdto.ProductBundlePageView) {
				v.HTTPStatus = tc.status
				v.LoadFailed = true
				v.SaveBlocked = true
			})
			stub.bundlePageErr = tc.err
			rec := editorRequest(t, h, "/admin/products/bundle?project=p1&product=prod1")
			assertProductPageFailure(t, tc.name, rec, tc.status, tc.wantText)
			// 取数编排在 service：控制器只调一次读模型。
			assertCalls(t, tc.name, stub.calls, []string{"ProductBundlePage"})
		})
	}
}

// TestEditProductErrorGrades 编辑页的错误分档：**档位由读模型给出**（判据在 service：
// 商品读不到的三档 + 「没有错误却没有行」的兜底都在那边），控制器这一层只验两件事 ——
// 原样传出视图带的档位（不自己改判）+ 按这一次的 err 归口**当前语言**的失败槽文案。
func TestEditProductErrorGrades(t *testing.T) {
	// failedEditView 读模型交回来的失败档（键给齐：契约上视图恒非 nil）。
	failedEditView := func(status int) *productdto.ProductEditPageView {
		return productEditPageViewStub(func(v *productdto.ProductEditPageView) {
			v.HTTPStatus = status
			v.LoadFailed = true
			v.HasProduct = false
			v.Detail = nil
		})
	}
	cases := []struct {
		name       string
		setup      func(s *editorStub)
		wantStatus int
		wantText   string
	}{
		{
			name: "商品不存在",
			setup: func(s *editorStub) {
				s.editErr = errors.New(productenums.ErrNotFound)
				s.editView = failedEditView(http.StatusNotFound)
			},
			wantStatus: http.StatusNotFound,
			wantText:   "商品或变体不存在",
		},
		{
			name: "参数非法",
			setup: func(s *editorStub) {
				s.editErr = errors.New(productenums.ErrInvalidParam)
				s.editView = failedEditView(http.StatusBadRequest)
			},
			wantStatus: http.StatusBadRequest,
			wantText:   "请求参数无效",
		},
		{
			name: "驱动错误",
			setup: func(s *editorStub) {
				s.editErr = errors.New(editorDriverText)
				s.editView = failedEditView(http.StatusInternalServerError)
			},
			wantStatus: http.StatusInternalServerError,
			wantText:   "这一页的数据没能读出来",
		},
		{
			name: "没有错误却没有行",
			setup: func(s *editorStub) {
				// 读模型给了 404 档但没给 err：这一档与详情页同一口径 ——
				// 「读到了、库里没有」，页面渲染的是**空态**而不是失败槽，
				// 所以断言的是空态文案，不是归口错误文案。
				s.editView = failedEditView(http.StatusNotFound)
			},
			wantStatus: http.StatusNotFound,
			wantText:   "商品不存在",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, stub, _, _ := editorEnv()
			tc.setup(stub)
			rec := editorRequest(t, h, "/admin/products/edit?project=p1&product=prod1")
			assertProductPageFailure(t, tc.name, rec, tc.wantStatus, tc.wantText)
			// 取数编排在 service：控制器只调一次读模型（失败之后不会再发任何派生查询）。
			assertCalls(t, tc.name, stub.calls, []string{"ProductEditPage"})
		})
	}
}

// TestDetailTemplateTemplateListFailure 读模型报装载失败（主数据模板清单读不到）：500 + 完整页壳。
//
// 「模板清单读不到之后一条派生取数都不发」这条判据已经下沉到 service
// （service/product_detail_template_page.go 的文件头第 1 条），在
// service/product_detail_template_page_test.go 里对着端口桩断言 —— 控制器这一层看不到派生调用。
func TestDetailTemplateTemplateListFailure(t *testing.T) {
	h, stub, _, _ := editorEnv()
	stub.detailTemplatePageErr = errors.New(editorDriverText)
	rec := editorRequest(t, h, "/admin/products/template?project=p1&product=prod1")
	assertProductPageFailure(t, "模板清单失败", rec, http.StatusInternalServerError, "商品详情页模板")
	assertCalls(t, "模板清单失败", stub.calls, []string{"ProductDetailTemplatePage"})
}

// TestDetailTemplateDepsMissingIsNotFailure 未装配模板能力是「装配缺陷的可见提示」：200 的就地说明，
// 不是装载失败（这一档此前就有，别在改失败出口时把它一起改成 500）。
func TestDetailTemplateDepsMissingIsNotFailure(t *testing.T) {
	h, stub, _, _ := editorEnv()
	h.templates = nil
	h.instances = nil
	rec := editorRequest(t, h, "/admin/products/template?project=p1&product=prod1")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("未装配档应为 200（就地说明），实际 %d，body=%s", rec.Code, headOf(body))
	}
	if got := rec.Header().Get("Cache-Control"); got == "no-store" {
		t.Fatalf("未装配档不是失败页，不该标 no-store")
	}
	if !strings.Contains(body, "详情页模板能力未装配") {
		t.Fatalf("未装配档应渲染装配缺陷的说明，body=%s", headOf(body))
	}
	if len(stub.calls) != 0 {
		t.Fatalf("未装配档不该发任何商品取数，实际 %v", stub.calls)
	}
}

// TestDetailTemplateNoProductIsGuidance 菜单入口（不带 product）是 200 的引导页，不是 400：
// 本页本来就有「还没选商品」的入口形态。这条钉住「别把入口形态判成参数非法」。
func TestDetailTemplateNoProductIsGuidance(t *testing.T) {
	h, stub, _, _ := editorEnv()
	rec := editorRequest(t, h, "/admin/products/template?project=p1")
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("菜单入口应为 200 的引导页，实际 %d，body=%s", rec.Code, headOf(body))
	}
	if !strings.Contains(body, "请先从商品列表选择一件商品") {
		t.Fatalf("菜单入口应给出「去哪选商品」的引导，body=%s", headOf(body))
	}
	// 读模型**会被调一次**（「没给 product」这个结论由它给：它是取数编排的一部分，
	// 顺带还判了工程存在性）。不该发生的是它之下的派生取数 —— 那条判据在 service。
	assertCalls(t, "菜单入口", stub.calls, []string{"ProductDetailTemplatePage"})
}

// TestBundleSaveFailureKeepsFragmentBoundary 写失败原地重渲染的**响应边界**：
//
//	htmx 请求  → 200 + 片段（不是整页失败页；成员清单原样带回来）；
//	原生请求  → 200 + 整页（页壳完整）。
//
// 这两档由同一个 renderBundlePage 输出（fragment 形参），所以边界写错不会编译失败 —— 只会让
// 运营点一次保存之后整页被换掉、或者片段槽里塞进一整张页面。
func TestBundleSaveFailureKeepsFragmentBoundary(t *testing.T) {
	t.Run("htmx 档保持片段", func(t *testing.T) {
		h, stub, _, _ := editorEnv()
		stub.setErr = errors.New(productenums.ErrBundlePriceRequired)
		rec := editorSaveRequest(t, h, true)
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("写失败重渲染是 200（就地留住输入），实际 %d，body=%s", rec.Code, headOf(body))
		}
		if strings.Contains(body, "</html>") {
			t.Fatalf("htmx 档必须是片段，实际渲染了整页，body=%s", headOf(body))
		}
		if !strings.Contains(body, "data-bundle-form-host") {
			t.Fatalf("片段必须保留宿主元素（hx-target 靠它定位），body=%s", headOf(body))
		}
		if !strings.Contains(body, `name="variantId" value="v1"`) {
			t.Fatalf("重渲染必须把用户刚配的成员清单带回来，body=%s", headOf(body))
		}
		assertSubmittedSnapshot(t, "htmx 写失败", body, false)
		if strings.Contains(body, "LoadError") || strings.Contains(body, "LoadFailed") {
			t.Fatalf("装载成功、只是保存没通过：不该出现装载失败槽，body=%s", headOf(body))
		}
	})

	t.Run("原生档整页", func(t *testing.T) {
		h, stub, _, _ := editorEnv()
		stub.setErr = errors.New(productenums.ErrBundlePriceRequired)
		rec := editorSaveRequest(t, h, false)
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("原生档写失败也是 200（整页重渲染），实际 %d，body=%s", rec.Code, headOf(body))
		}
		for _, want := range []string{"<html", "</body>", "</html>", `name="variantId" value="v1"`} {
			if !strings.Contains(body, want) {
				t.Fatalf("原生档应整页重渲染且带回成员清单，缺 %q，body=%s", want, headOf(body))
			}
		}
		assertSubmittedSnapshot(t, "原生写失败", body, false)
	})

	t.Run("片段档装载也失败时仍是片段", func(t *testing.T) {
		h, stub, _, _ := editorEnv()
		stub.setErr = errors.New(productenums.ErrBundlePriceRequired)
		// 保存失败之后重建页面又读不到配置：这一档最容易写成「整页失败页 + 500」，
		// 而它必须仍是片段（200）：否则 htmx 会因为 5xx 不 swap，用户看到的是「点了没反应」。
		stub.bundlePageErr = errors.New(editorDriverText)
		rec := editorSaveRequest(t, h, true)
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("片段档装载失败仍是 200（片段响应边界优先），实际 %d，body=%s", rec.Code, headOf(body))
		}
		if strings.Contains(body, "</html>") {
			t.Fatalf("片段档必须是片段，实际渲染了整页，body=%s", headOf(body))
		}
		if !strings.Contains(body, "data-bundle-form-host") {
			t.Fatalf("片段必须保留宿主元素，body=%s", headOf(body))
		}
		if !strings.Contains(body, "这一页的数据没能读出来") {
			t.Fatalf("片段里要说清是装载失败，body=%s", headOf(body))
		}
		// 关键：读不出来**不等于**可以把用户整块表单换掉 —— 提交快照必须原样回来。
		assertSubmittedSnapshot(t, "双失败片段", body, true)
		if !strings.Contains(body, "下面是你刚提交的成员清单与数值") {
			t.Fatalf("双失败档要说清「输入已留下、但本次不能保存」，body=%s", headOf(body))
		}
		for _, bad := range productPageLeakMarkers {
			if strings.Contains(body, bad) {
				t.Fatalf("片段里出现了不该出现的内容 %q，body=%s", bad, headOf(body))
			}
		}
	})
}

// TestBundleSaveNativeDoubleFailureKeepsSubmittedSnapshot 原生 POST 写失败 + 重读也失败：
// 整页把提交快照渲染回来（表单在、保存禁用、给了重新载入入口），不是把整块表单换成一句提示。
func TestBundleSaveNativeDoubleFailureKeepsSubmittedSnapshot(t *testing.T) {
	h, stub, _, _ := editorEnv()
	stub.setErr = errors.New(productenums.ErrBundlePriceRequired)
	stub.bundlePageErr = errors.New(editorDriverText)
	rec := editorSaveRequest(t, h, false)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("原生双失败应整页重渲染（200），实际 %d，body=%s", rec.Code, headOf(body))
	}
	for _, want := range []string{"<html", "</body>", "</html>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("原生档必须是整页，缺 %q，body=%s", want, headOf(body))
		}
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("装载失败的响应不该被缓存，Cache-Control=%q", got)
	}
	assertSubmittedSnapshot(t, "原生双失败", body, true)
	if strings.Contains(body, "data-bundle-form-host") == false {
		t.Fatalf("整页里也要有片段宿主（表单就在里面），body=%s", headOf(body))
	}
}

// assertSubmittedSnapshot 断言「用户提交的内容原样回到页面上」：三个配置数值（来自提交，
// 不是库里的旧值）、成员行的**全部并行字段与次序**、保存禁用 + 重新载入入口、以及为什么禁止保存。
func assertSubmittedSnapshot(t *testing.T, step, body string, saveBlocked bool) {
	t.Helper()
	for _, re := range []*regexp.Regexp{reCfgMaxOptions, reCfgMinTotalQty, reCfgMaxTotalQty} {
		if !re.MatchString(body) {
			t.Fatalf("%s：配置数值应由提交快照回填（%s），body=%s", step, re.String(), headOf(body))
		}
	}
	if !strings.Contains(body, "保存未完成") {
		t.Fatalf("%s：这次保存为什么没通过也要留在页面上，body=%s", step, headOf(body))
	}
	if saveBlocked {
		// 装载也失败：保存必须禁用（否则会把不完整配置写进去），并给一条现有通道的重新载入入口。
		if !reDisabledSubmit.MatchString(body) {
			t.Fatalf("%s：配置没读出来时保存必须禁用，body=%s", step, headOf(body))
		}
		for _, want := range []string{"重新载入", "本次禁止保存", "/admin/products/bundle?"} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s：双重失败档缺 %q，body=%s", step, want, headOf(body))
			}
		}
	} else if reDisabledSubmit.MatchString(body) {
		t.Fatalf("%s：装载成功、只是保存没通过时不该禁用保存（重试就该能再提交），body=%s", step, headOf(body))
	}
	got := submittedMemberRows(body)
	want := []string{
		"v1|1|2|1|3|BundleSourceProduct|||SKU-EXT-1",
		"v2|0|1|0|5|BundleSourceWarehouse|wh1|WSKU-1|",
	}
	if strings.Join(got, " ; ") != strings.Join(want, " ; ") {
		t.Fatalf("%s：成员行（次序 + 每个并行字段）应为 %v，实际 %v", step, want, got)
	}
}

var (
	reCfgMaxOptions  = regexp.MustCompile(`name="maxOptions"[^>]*value="7"`)
	reCfgMinTotalQty = regexp.MustCompile(`name="minTotalQty"[^>]*value="2"`)
	reCfgMaxTotalQty = regexp.MustCompile(`name="maxTotalQty"[^>]*value="9"`)
	reDisabledSubmit = regexp.MustCompile(`<button type="submit" class="btn btn-primary" disabled>`)

	reMemberRow      = regexp.MustCompile(`(?s)<tr data-bundle-member-row>(.*?)</tr>`)
	reVariantValue   = regexp.MustCompile(`name="variantId" value="([^"]*)"`)
	reSourceValue    = regexp.MustCompile(`name="sourceKind" value="([^"]*)"`)
	reWarehouseValue = regexp.MustCompile(`name="memberWarehouseId" value="([^"]*)"`)
	reWarehouseSku   = regexp.MustCompile(`name="memberWarehouseSku" value="([^"]*)"`)
	reExternalSku    = regexp.MustCompile(`name="memberExternalSku" value="([^"]*)"`)
	reRequiredSelect = regexp.MustCompile(`(?s)name="required"[\s\S]*?</select>`)
	reSelectedOne    = regexp.MustCompile(`value="1"\s*selected`)
)

// submittedMemberRows 按 DOM 顺序抽出成员行的全部并行字段：
// 「提交了什么、就以什么次序回来」是这条 P2 的判据本体，只看「片段在」证明不了它。
func submittedMemberRows(body string) []string {
	var out []string
	for _, chunk := range reMemberRow.FindAllStringSubmatch(body, -1) {
		row := chunk[1]
		id := reVariantValue.FindStringSubmatch(row)
		if id == nil || id[1] == "" {
			// 尾部那一行空槽（「再追加一个成员」）的 variantId 是空值：它不是成员，
			// 报回去也会被服务端跳过（product_page.go 的 `if vid == "" { continue }`）。
			continue
		}
		required := "0"
		if sel := reRequiredSelect.FindString(row); sel != "" && reSelectedOne.MatchString(sel) {
			required = "1"
		}
		out = append(out, strings.Join([]string{
			id[1], required,
			numberValue(row, "defaultQty"), numberValue(row, "minQty"), numberValue(row, "maxQty"),
			firstValue(reSourceValue, row), firstValue(reWarehouseValue, row),
			firstValue(reWarehouseSku, row), firstValue(reExternalSku, row),
		}, "|"))
	}
	return out
}

func numberValue(row, name string) string {
	return firstValue(regexp.MustCompile(`name="`+name+`"[^>]*value="([^"]*)"`), row)
}

func firstValue(re *regexp.Regexp, row string) string {
	if m := re.FindStringSubmatch(row); m != nil {
		return m[1]
	}
	return ""
}

// containsCall 判断取数序列里是否发生过某次调用。
func containsCall(calls []string, want string) bool {
	for _, c := range calls {
		if c == want {
			return true
		}
	}
	return false
}

// assertCalls 断言取数序列**逐字相等**：失败档要的是「失败那一条之后一条都没再发」，
// 只断言「包含」会放过「后面又照发一遍」的回退。
func assertCalls(t *testing.T, step string, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s：取数序列应为 %v，实际 %v", step, want, got)
	}
}

// replaceProject 把 URL 里的 project 参数换成给定值（保留其它参数）。
func replaceProject(target, project string) string {
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	q := u.Query()
	q.Set("project", project)
	u.RawQuery = q.Encode()
	return u.String()
}
