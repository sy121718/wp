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
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
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

// productPageProductsStub 同上：只实现失败档位经过的那几个方法。
type productPageProductsStub struct {
	productcontract.ProductService
	listRows []*productdto.ProductResp
	listErr  error
	getRes   *productdto.ProductResp
	getErr   error
	// calls 记录被调用过的方法名：失败档要断言「一条派生取数都没发」，只看状态码看不出来。
	calls []string
}

func (s *productPageProductsStub) record(name string) {
	s.calls = append(s.calls, name)
}

func (s *productPageProductsStub) List(context.Context, *productdto.ListReq) ([]*productdto.ProductResp, error) {
	s.record("List")
	return s.listRows, s.listErr
}

func (s *productPageProductsStub) CountProducts(context.Context, *productdto.ListReq) (int64, error) {
	s.record("CountProducts")
	return int64(len(s.listRows)), nil
}

func (s *productPageProductsStub) Get(context.Context, *productdto.GetReq) (*productdto.ProductResp, error) {
	s.record("Get")
	return s.getRes, s.getErr
}

// 列表页在商品行之前要先取好分类 / 品牌 / 标签（每个商品行都要渲染这三样）：
// 这几条要成功返回空清单，「商品详情读不出来」那条路径才走得下去。
func (s *productPageProductsStub) ListCategories(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
	s.record("ListCategories")
	return nil, nil
}

func (s *productPageProductsStub) ListBrands(context.Context, *productdto.ListBrandReq) ([]*productdto.BrandResp, error) {
	s.record("ListBrands")
	return nil, nil
}

func (s *productPageProductsStub) ListTags(context.Context, *productdto.ListTagReq) ([]*productdto.TagResp, error) {
	s.record("ListTags")
	return nil, nil
}

// 属性组勾选列表与评分明细：列表页 / 详情页会取（不是失败路径上的调用），桩给空清单。
func (s *productPageProductsStub) ListAttributes(context.Context, *productdto.ListAttributeReq) ([]*productdto.AttributeResp, error) {
	s.record("ListAttributes")
	return nil, nil
}

func (s *productPageProductsStub) ListRatings(context.Context, *productdto.ListRatingsReq) (*productdto.RatingResp, error) {
	s.record("ListRatings")
	return &productdto.RatingResp{}, nil
}

// 批量改价抽屉的规则清单：列表页无条件取它（不是失败路径上的调用），桩要给空清单。
func (s *productPageProductsStub) ListPricingRuleTypes(context.Context) []*productdto.PricingRuleTypeResp {
	s.record("ListPricingRuleTypes")
	return nil
}

func (s *productPageProductsStub) ListPricingRoundingOptions(context.Context) []*productdto.PricingRoundingOptionResp {
	s.record("ListPricingRoundingOptions")
	return nil
}

// productPageReaderStub 页面读模型桩（新建页的取数编排已下沉到 service）。
//
// 控制器现在只做三步：读参数 → 调读模型 → 渲染。所以这一层的用例验的是**控制器**的三段
// （参数转发 / 渲染 / 档位与 no-store），取数编排本身由 service 的用例
// （product/service/product_page_test.go）与整页链路覆盖
// （public/test/product/feature 有真实库，那边才验得到「合法工程真的取到了属性组」）。
//
// 桩返回的是**完整的视图**（键给齐）：控制器不再补任何键，缺一个就是渲染中断。
type productPageReaderStub struct {
	productcontract.ProductService
	view *productdto.ProductNewPageView
	err  error
	// reqs 记录控制器转下来的入参：参数读丢了要能看出来。
	reqs []*productdto.ProductNewPageReq
}

func (s *productPageReaderStub) ProductNewPage(_ context.Context, req *productdto.ProductNewPageReq) (*productdto.ProductNewPageView, error) {
	s.reqs = append(s.reqs, req)
	return s.view, s.err
}

// productNewPageViewStub 成功档视图（与 service 产出的同型：列表键一律给空列表而不是缺键）。
// mutate 用于把某个用例改成失败档（改档位 / 失败槽）。
func productNewPageViewStub(mutate func(*productdto.ProductNewPageView)) *productdto.ProductNewPageView {
	v := &productdto.ProductNewPageView{
		Title:               productenums.ProductsCreateSubmit,
		Menu:                "products",
		Projects:            []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}},
		SelectedProject:     "p1",
		WarehouseOptions:    []productdto.WarehouseOptionView{},
		AttributeOptions:    []productdto.AttributeOptionView{},
		WarehouseSKUOptions: []productdto.WarehouseSKUGroupView{},
		Templates:           []productdto.EntityTemplateOptionView{},
		HTTPStatus:          http.StatusOK,
	}
	if mutate != nil {
		mutate(v)
	}
	return v
}

// productPageRequest 挂上三个整页 GET（真 Jet + 真模板）并发一个 GET，返回 recorder。
func productPageRequest(t *testing.T, h *productPageHandle, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/products", h.ProductsPage)
	engine.GET("/admin/products/new", h.ProductNewPage)
	engine.GET("/admin/products/detail", h.ProductDetailPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// productPageLeakMarkers 失败页上绝不能出现的内部片段与协议 key。
var productPageLeakMarkers = []string{
	"pq:", "SQLSTATE", "does not exist", "duplicate key", "constraint",
	"relation", `"code":`, "LoadError", "LoadFailed",
}

// assertProductPageFailure 断言「整页失败出口」的形状（500 / 400 / 404 三档共用）。
func assertProductPageFailure(t *testing.T, step string, rec *httptest.ResponseRecorder, wantStatus int, wantText string) string {
	t.Helper()
	body := rec.Body.String()
	if rec.Code != wantStatus {
		t.Fatalf("%s：状态码应为 %d，实际 %d，body=%s", step, wantStatus, rec.Code, headOf(body))
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("%s：失败页不该被缓存（重试必须真的回到服务端），Cache-Control=%q", step, got)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("%s：失败出口必须是页面，Content-Type=%q", step, ct)
	}
	// 页壳完整：不是半截模板，也不是 JSON 报文。
	for _, want := range []string{"<html", "</body>", "</html>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s：页壳不完整，缺 %q，body=%s", step, want, headOf(body))
		}
	}
	if !strings.Contains(body, wantText) {
		t.Fatalf("%s：页面上应有 %q，body=%s", step, wantText, headOf(body))
	}
	for _, bad := range productPageLeakMarkers {
		if strings.Contains(body, bad) {
			t.Fatalf("%s：页面上出现了不该出现的内容 %q，body=%s", step, bad, headOf(body))
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
func TestProductsPageLoadFailureRendersSamePage(t *testing.T) {
	h := &productPageHandle{
		projects: productPageProjectsStub{err: errors.New(`pq: relation "products" does not exist`)},
		products: &productPageProductsStub{},
	}
	rec := productPageRequest(t, h, "/admin/products")
	assertProductPageFailure(t, "列表页主数据失败", rec, http.StatusInternalServerError, "这一页的数据没能读出来")
}

// TestProductsPageBadProjectParamIsFullPage 参数级失败：整页 + 400（不是 JSON，也不是裸文本）。
//
// ?project=<不存在的工程 id> 此前会渲染成「这个工程下没有商品」——把「工程不存在」说成了
// 「工程是空的」，运营照着这句话去建商品，就建到另一个工程上了。
func TestProductsPageBadProjectParamIsFullPage(t *testing.T) {
	h := &productPageHandle{
		projects: productPageProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
		products: &productPageProductsStub{},
	}
	rec := productPageRequest(t, h, "/admin/products?project=does-not-exist")
	assertProductPageFailure(t, "列表页工程参数不合法", rec, http.StatusBadRequest, "请求参数无效")
}

// TestProductsPageRowDetailFailureIsNotSwallowed 单个商品详情读不出来：整页失败，不静默少一行。
//
// 此前这里是 `if derr != nil { continue }`：页面少一行却仍写着「共 N 条」，用户据此以为
// 商品已经被删了 —— 而它只是这一行读不出来。主数据取不到就不该渲染成一张「完整」的列表。
func TestProductsPageRowDetailFailureIsNotSwallowed(t *testing.T) {
	h := &productPageHandle{
		projects: productPageProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
		products: &productPageProductsStub{
			listRows: []*productdto.ProductResp{{ID: "prod-1"}},
			getErr:   errors.New(`pq: canceling statement due to statement timeout`),
		},
	}
	rec := productPageRequest(t, h, "/admin/products?project=p1")
	assertProductPageFailure(t, "列表页某一行详情失败", rec, http.StatusInternalServerError, "这一页的数据没能读出来")
}

// TestProductNewPageLoadFailureRendersSamePage 新建页主数据没读到：同页 + 500 + no-store。
func TestProductNewPageLoadFailureRendersSamePage(t *testing.T) {
	reader := &productPageReaderStub{
		view: productNewPageViewStub(func(v *productdto.ProductNewPageView) {
			v.HTTPStatus = http.StatusInternalServerError
			v.LoadFailed = true
		}),
		err: errors.New(`pq: relation "products" does not exist`),
	}
	h := &productPageHandle{projects: productPageProjectsStub{}, products: reader}
	rec := productPageRequest(t, h, "/admin/products/new")
	assertProductPageFailure(t, "新建页主数据失败", rec, http.StatusInternalServerError, "这一页的数据没能读出来")
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
		getErr     error
		wantStatus int
		wantText   string // 页面上应出现的文案
	}{
		{
			name:       "没给 product 参数 → 404",
			target:     "/admin/products/detail",
			wantStatus: http.StatusNotFound,
			wantText:   "商品不存在",
		},
		{
			name:       "业务上不存在 → 404",
			target:     "/admin/products/detail?project=p1&product=prod-1",
			getErr:     errors.New(productenums.ErrNotFound),
			wantStatus: http.StatusNotFound,
			wantText:   "商品不存在",
		},
		{
			name:       "参数不合法 → 400",
			target:     "/admin/products/detail?project=p1&product=not-a-uuid",
			getErr:     errors.New(productenums.ErrInvalidParam),
			wantStatus: http.StatusBadRequest,
			wantText:   "请求参数无效",
		},
		{
			name:       "读不出来 → 500（不是「不存在」）",
			target:     "/admin/products/detail?project=p1&product=prod-1",
			getErr:     errors.New(`pq: relation "products" does not exist`),
			wantStatus: http.StatusInternalServerError,
			wantText:   "这一页的数据没能读出来",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &productPageHandle{
				projects: productPageProjectsStub{rows: projectRows},
				products: &productPageProductsStub{getErr: tc.getErr},
			}
			rec := productPageRequest(t, h, tc.target)
			body := assertProductPageFailure(t, tc.name, rec, tc.wantStatus, tc.wantText)
			// 两档的文案不能互相串：404 说「商品不存在」，500 说「没能读出来」。
			if tc.wantStatus == http.StatusNotFound && strings.Contains(body, "这一页的数据没能读出来") {
				t.Fatalf("%s：404 档渲染成了装载失败卡 —— 「不存在」与「没读到」必须分开", tc.name)
			}
			if tc.wantStatus == http.StatusInternalServerError && strings.Contains(body, "商品不存在") {
				t.Fatalf("%s：500 档渲染成了「商品不存在」空态 —— 读不出来被吞成了不存在", tc.name)
			}
		})
	}
}

// TestProductDetailPageRendersProductWhenDetailReadable 成功档回归：详情取到了就正常渲染。
//
// 只断言「不再是失败页」这一层：整页 200、页壳完整、失败槽与 404 空态都不出现。
// （字段级明细由 productRow 自己的用例覆盖，这里要钉的是失败出口没有把成功路径一起改坏。）
func TestProductDetailPageRendersProductWhenDetailReadable(t *testing.T) {
	h := &productPageHandle{
		projects: productPageProjectsStub{rows: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}},
		products: &productPageProductsStub{
			getRes: &productdto.ProductResp{ID: "prod-1", Name: "测试商品", Status: "active", Type: "variant"},
		},
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
	for _, bad := range []string{"这一页的数据没能读出来", "商品不存在"} {
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
	h := &productPageHandle{
		projects: productPageProjectsStub{},
		products: &productPageProductsStub{},
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
	if strings.Contains(body, "这一页的数据没能读出来") {
		t.Fatalf("成功页渲染出了失败卡，body=%s", headOf(body))
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
		view: productNewPageViewStub(func(v *productdto.ProductNewPageView) {
			v.HTTPStatus = http.StatusBadRequest
			v.LoadFailed = true
			v.SelectedProject = "does-not-exist"
		}),
		err: errors.New(productenums.ErrInvalidParam),
	}
	h := &productPageHandle{projects: productPageProjectsStub{}, products: reader}
	rec := productPageRequest(t, h, "/admin/products/new?project=does-not-exist")
	assertProductPageFailure(t, "新建页 ?project=不存在的 id", rec, http.StatusBadRequest, "请求参数无效")
}

// TestProductNewPageForwardsProjectToReader 控制器把 query 里的工程原样交给读模型。
//
// 「合法工程时派生取数必须发出」那一半随之沉到 service（同上）：控制器这一层能验的、
// 也是它现在唯一的职责 —— **参数有没有被正确转下去**。没有这条，控制器把 project 读丢
// （永远读默认工程）也能全绿，而症状是「切换工程后新建页仍然是第一个工程的数据」。
func TestProductNewPageForwardsProjectToReader(t *testing.T) {
	reader := &productPageReaderStub{view: productNewPageViewStub(nil)}
	h := &productPageHandle{projects: productPageProjectsStub{}, products: reader}
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
	h := &productPageHandle{projects: productPageProjectsStub{}, products: reader}
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
	if strings.Contains(body, "这一页的数据没能读出来") {
		t.Fatalf("成功页渲染出了失败卡，body=%s", headOf(body))
	}
}

// TestProductTemplatesSplitFailureFromEmpty 模板层：失败槽与空态是两个分支，先判失败。
//
// 三份模板都用同一份契约：{{if loadError}} … {{else if 空态}} … —— 顺序反了会出现
// 「取数失败时页面显示『还没有数据』」，把失败说成了空。
func TestProductTemplatesSplitFailureFromEmpty(t *testing.T) {
	cases := []struct {
		template string
		data     gin.H
		// 失败档渲染出来必须有的东西 / 必须没有的东西。
		wantOnFailure []string
	}{
		{
			template:      "admin/product/products.html",
			data:          productPageLayoutData(gin.H{"title": "商品", "menu": "products"}),
			wantOnFailure: []string{"这一页的数据没能读出来", `role="alert"`, "</html>"},
		},
		{
			template:      "admin/product/products_new.html",
			data:          productPageLayoutData(gin.H{"title": "新建商品", "menu": "products"}),
			wantOnFailure: []string{"这一页的数据没能读出来", `role="alert"`, "</html>"},
		},
		{
			template:      "admin/product/product_detail.html",
			data:          productPageLayoutData(gin.H{"title": "商品详情", "menu": "products"}),
			wantOnFailure: []string{"这一页的数据没能读出来", `role="alert"`, "</html>"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.template, func(t *testing.T) {
			// 失败档：只有页壳键 + 失败槽（与 Go 侧 productPageFailure 的零值数据同形）。
			failed := gin.H{}
			for k, v := range tc.data {
				failed[k] = v
			}
			failed["LoadFailed"] = true
			failed["LoadError"] = "操作失败，请稍后重试（细节只进日志）"
			html := renderAdminTemplate(t, tc.template, failed)
			for _, want := range tc.wantOnFailure {
				if !strings.Contains(html, want) {
					t.Fatalf("失败档应有 %q，html=%s", want, headOf(html))
				}
			}
			// 失败档绝不能渲染成空态：空态是「读到了、没有数据」，两者混在一起
			// 会让运营以为数据被删了。
			for _, bad := range []string{"还没有站点工程", "商品不存在", "tpl-product-create"} {
				if strings.Contains(html, bad) {
					t.Fatalf("失败档渲染出了正常内容 %q —— 失败与空态没有分开，html=%s", bad, headOf(html))
				}
			}
			// 反向（「正常档不该出现失败卡」）不在这里：三份模板的正常档需要各自完整的
			// 页面数据（列表页要 Projects/Statuses/分页…），手搭一份就是第二份 handler。
			// 那一层由 handler 级用例覆盖（TestProductsPageEmptyProjectsRendersPage、
			// TestProductDetailPageRendersProductWhenDetailReadable）。
		})
	}
}
