package producthttp

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"go_wp/internal/adminui"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
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

// productPageReaderStub 只实现实际消费的业务查询，不复制生产页面聚合。
type productPageReaderStub struct {
	productcontract.ProductService
	scope           *productcontract.CatalogScope
	scopeErr        error
	product         *productdto.ProductResp
	detailErr       error
	result          *productdto.ProductListResp
	listErr         error
	listReqs        []*productdto.ListReq
	attributes      []*productdto.AttributeResp
	attributesErr   error
	categories      []*productdto.CategoryResp
	categoryErr     error
	brands          []*productdto.BrandResp
	brandErr        error
	tags            []*productdto.TagResp
	tagErr          error
	attributeResult *productdto.TaxonomyListResp[*productdto.AttributeResp]
	categoryResult  *productdto.CategorySearchResp
	brandResult     *productdto.TaxonomyListResp[*productdto.BrandResp]
	tagResult       *productdto.TaxonomyListResp[*productdto.TagResp]
	queryReqs       []*productdto.CatalogTaxonomyReq
}

func (s *productPageReaderStub) ResolveCatalogScope(_ context.Context, id string) (*productcontract.CatalogScope, error) {
	if s.scopeErr != nil {
		return nil, s.scopeErr
	}
	if s.scope != nil {
		return s.scope, nil
	}
	if id != "" && id != "p1" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	return &productcontract.CatalogScope{ProjectID: "p1", Projects: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}}, nil
}
func (s *productPageReaderStub) QueryProducts(_ context.Context, req *productdto.ListReq) (*productdto.ProductListResp, error) {
	s.listReqs = append(s.listReqs, req)
	if s.listErr != nil {
		return nil, s.listErr
	}
	if s.result != nil {
		return s.result, nil
	}
	return &productdto.ProductListResp{Items: []*productdto.ProductResp{}, Page: 1, Size: 20}, nil
}
func (s *productPageReaderStub) Get(_ context.Context, req *productdto.GetReq) (*productdto.ProductResp, error) {
	if s.detailErr != nil {
		return nil, s.detailErr
	}
	if req.ID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	if s.product != nil {
		return s.product, nil
	}
	return productRowForRender(), nil
}
func (s *productPageReaderStub) ListAttributes(context.Context, *productdto.ListAttributeReq) ([]*productdto.AttributeResp, error) {
	return s.attributes, s.attributesErr
}
func (s *productPageReaderStub) CategoryNodes(context.Context, string) ([]*productdto.CategoryResp, error) {
	return s.categories, s.categoryErr
}
func (s *productPageReaderStub) ListBrands(context.Context, *productdto.ListBrandReq) ([]*productdto.BrandResp, error) {
	return s.brands, s.brandErr
}
func (s *productPageReaderStub) ListTags(context.Context, *productdto.ListTagReq) ([]*productdto.TagResp, error) {
	return s.tags, s.tagErr
}
func (s *productPageReaderStub) CatalogBrands(context.Context, string) ([]*productdto.BrandResp, error) {
	return s.brands, s.brandErr
}
func (s *productPageReaderStub) CatalogTags(context.Context, string) ([]*productdto.TagResp, error) {
	return s.tags, s.tagErr
}
func (s *productPageReaderStub) CatalogAttributes(context.Context, string) ([]*productdto.AttributeResp, error) {
	return s.attributes, s.attributesErr
}
func (s *productPageReaderStub) ListTagRuleTypes(context.Context) []*productdto.TagRuleTypeResp {
	return []*productdto.TagRuleTypeResp{}
}
func (s *productPageReaderStub) CatalogWarehouses(context.Context, string) ([]productdto.WarehouseOptionResp, error) {
	return []productdto.WarehouseOptionResp{}, nil
}
func (s *productPageReaderStub) CatalogWarehouseSKUs(context.Context, string, []productdto.WarehouseOptionResp) ([]productdto.WarehouseSKUGroupResp, error) {
	return []productdto.WarehouseSKUGroupResp{}, nil
}
func (s *productPageReaderStub) CatalogEntityTemplates(context.Context, string) (*productdto.EntityTemplateCatalogResp, error) {
	return &productdto.EntityTemplateCatalogResp{}, nil
}
func (s *productPageReaderStub) ListPricingRuleTypes(context.Context) []*productdto.PricingRuleTypeResp {
	return []*productdto.PricingRuleTypeResp{}
}
func (s *productPageReaderStub) ListPricingRoundingOptions(context.Context) []*productdto.PricingRoundingOptionResp {
	return []*productdto.PricingRoundingOptionResp{}
}
func (s *productPageReaderStub) QueryDetailTemplatePanel(context.Context, string, string) (productcontract.DetailTemplatePanelFacts, error) {
	return productcontract.DetailTemplatePanelFacts{}, nil
}
func (s *productPageReaderStub) QueryAttributes(_ context.Context, q *productdto.CatalogTaxonomyReq) (*productdto.TaxonomyListResp[*productdto.AttributeResp], error) {
	s.queryReqs = append(s.queryReqs, q)
	if s.attributesErr != nil {
		return nil, s.attributesErr
	}
	if s.attributeResult != nil {
		return s.attributeResult, nil
	}
	return &productdto.TaxonomyListResp[*productdto.AttributeResp]{Items: s.attributes, Page: 1, Size: 20}, nil
}
func (s *productPageReaderStub) QueryCategories(_ context.Context, q *productdto.CatalogTaxonomyReq) (*productdto.CategorySearchResp, error) {
	s.queryReqs = append(s.queryReqs, q)
	if s.categoryErr != nil {
		return nil, s.categoryErr
	}
	if s.categoryResult != nil {
		return s.categoryResult, nil
	}
	return &productdto.CategorySearchResp{Items: s.categories, Page: 1, Size: 20}, nil
}
func (s *productPageReaderStub) QueryBrands(_ context.Context, q *productdto.CatalogTaxonomyReq) (*productdto.TaxonomyListResp[*productdto.BrandResp], error) {
	s.queryReqs = append(s.queryReqs, q)
	if s.brandErr != nil {
		return nil, s.brandErr
	}
	if s.brandResult != nil {
		return s.brandResult, nil
	}
	return &productdto.TaxonomyListResp[*productdto.BrandResp]{Items: s.brands, Page: 1, Size: 20}, nil
}
func (s *productPageReaderStub) QueryTags(_ context.Context, q *productdto.CatalogTaxonomyReq) (*productdto.TaxonomyListResp[*productdto.TagResp], error) {
	s.queryReqs = append(s.queryReqs, q)
	if s.tagErr != nil {
		return nil, s.tagErr
	}
	if s.tagResult != nil {
		return s.tagResult, nil
	}
	return &productdto.TaxonomyListResp[*productdto.TagResp]{Items: s.tags, Page: 1, Size: 20}, nil
}

// productPageRequest 挂上三个整页 GET（真 Jet + 真模板）并发一个 GET，返回 recorder。
func productPageRequest(t *testing.T, h *ProductPageHandle, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/products", pageview.UseRenderer(adminui.RenderPage), h.ProductsPage)
	engine.GET("/admin/products/new", pageview.UseRenderer(adminui.RenderPage), h.ProductNewPage)
	engine.GET("/admin/products/detail", pageview.UseRenderer(adminui.RenderPage), h.ProductDetailPage)
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
	engine.GET("/admin/products", pageview.UseRenderer(adminui.RenderPage), h.ProductsPage)
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
		engine.GET(route, pageview.UseRenderer(adminui.RenderPage), h.ProductAttributesPage)
	case "/admin/product-categories":
		engine.GET(route, pageview.UseRenderer(adminui.RenderPage), h.ProductCategoriesPage)
	case "/admin/product-brands":
		engine.GET(route, pageview.UseRenderer(adminui.RenderPage), h.ProductBrandsPage)
	case "/admin/product-tags":
		engine.GET(route, pageview.UseRenderer(adminui.RenderPage), h.ProductTagsPage)
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

func TestProductPageQueriesKeepJSONFailures(t *testing.T) {
	for _, target := range []string{"/admin/products", "/admin/products/new", "/admin/products/detail?product=prod-1"} {
		t.Run(target, func(t *testing.T) {
			h := &ProductPageHandle{products: &productPageReaderStub{scopeErr: errors.New("pq: scope unavailable")}}
			rec := productPageRequest(t, h, target)
			assertProductJSONFailure(t, "工程读取失败", rec, 500, "内部错误")
		})
	}
	for _, tc := range []struct {
		err    error
		status int
		text   string
	}{
		{productenums.Err(productenums.ErrNotFound), 404, "商品或变体不存在"},
		{productenums.Err(productenums.ErrInvalidParam), 400, "请求参数无效"},
		{errors.New("pq: product query failed"), 500, "内部错误"},
	} {
		h := &ProductPageHandle{products: &productPageReaderStub{detailErr: tc.err}}
		assertProductJSONFailure(t, "商品读取失败", productPageRequest(t, h, "/admin/products/detail?product=prod-1"), tc.status, tc.text)
	}
}

func TestProductPagesRenderOriginalObjects(t *testing.T) {
	p := productRowForRender()
	s := &productPageReaderStub{product: p, result: &productdto.ProductListResp{Items: []*productdto.ProductResp{p}, Total: 1, Page: 1, Size: 20}, attributes: p.Attributes}
	h := &ProductPageHandle{products: s}
	for _, target := range []string{"/admin/products?project=p1", "/admin/products/new?project=p1", "/admin/products/detail?project=p1&product=prod-1"} {
		rec := productPageRequest(t, h, target)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "</html>") {
			t.Fatalf("%s 渲染不完整: %d %s", target, rec.Code, headOf(rec.Body.String()))
		}
	}
	body := productPageRequest(t, h, "/admin/products?project=p1").Body.String()
	for _, want := range []string{"测试商品", "data-write-url=\"/api/product/delete\"", "data-write-url=\"/api/product/bulk-delete\""} {
		if !strings.Contains(body, want) {
			t.Fatalf("列表缺少 %q", want)
		}
	}
}
