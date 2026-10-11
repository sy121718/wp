package producthttp

// product_taxonomy_page_render_test.go — 属性 / 标签 / 分类 / 品牌四个整页 GET 的失败出口（task-37）。
//
// 与 product_page_render_test.go（task-28）同一族判据，契约见
// docs/rules/template-boundary.md §整页 GET 的失败出口：取数失败的整页 GET **也必须是「这一页」**
//（同一份模板 + 页壳 + 侧边菜单 + `Cache-Control: no-store`），状态码按档位说实话：
//
//	主数据没读到            → 500
//	?project=<不存在的 id>  → 400（请求参数不合法：判据是**本次请求已加载的工程集合** ——
//	                          h.projects.List 无分页/无过滤，且与页面里的工程选择器同源；
//	                          工程被删除或并发变化会让旧链接的工程失效，那时 400 是准确的事实。
//	                          本批四页没有单资源参数，因此没有 404 档）
//
// 这四页此前把每一次取数失败都交给 shell.PageError：浏览器收到 `{"code":500,…}`，
// DOM 里没有标题栏与左侧菜单，用户只能手改地址栏回去 —— 形状合法，静态门禁看不见，
// 只有把真实模板渲染出来才看得见。
//
// 另外钉住一条极易回退的性质：**失败之后不再发任何派生查询**
//（此前 projects/计数失败后，后面的列表、抽屉数据、规则清单照发不误；
//  在已被判非法的工程上继续查，正是把「工程不存在」渲染成「工程是空的」的成因）。

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
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
)

// taxonomyDriverText 驱动原文（驱动前缀 + 表名 + SQLSTATE + 关系名）：
// 它只允许进日志，绝不允许出现在 HTML 里（productPageLeakMarkers 逐条盯住这里面的片段）。
const taxonomyDriverText = `pq: relation "product_attributes" does not exist (SQLSTATE 42P01)`

// taxonomyStub 只实现四页用到的方法；其余方法因嵌入的接口为 nil 而 panic。
//
// 比「什么都返回零值」的桩安全：静默零值会让断言测到的是桩的行为，而不是页面的行为。
// calls 让「失败后零派生查询」成为可断言的事实，而不是靠读代码推断。
type taxonomyStub struct {
	productcontract.ProductService
	projects      []projectdto.ProjectResp
	projectsErr   error
	countErr      error
	listErr       error
	pageErr       error
	categoriesErr error

	attrs      []*productdto.AttributeResp
	tags       []*productdto.TagResp
	brands     []*productdto.BrandResp
	categories []*productdto.CategoryResp
	pageRows   []*productdto.CategoryResp

	calls []string
}

func (s *taxonomyStub) record(name string) { s.calls = append(s.calls, name) }

func (s *taxonomyStub) CountAttributes(context.Context, *productdto.ListAttributeReq) (int64, error) {
	s.record("CountAttributes")
	return int64(len(s.attrs)), s.countErr
}

func (s *taxonomyStub) ListAttributes(context.Context, *productdto.ListAttributeReq) ([]*productdto.AttributeResp, error) {
	s.record("ListAttributes")
	return s.attrs, s.listErr
}

func (s *taxonomyStub) ListTagRuleTypes(context.Context) []*productdto.TagRuleTypeResp {
	s.record("ListTagRuleTypes")
	return nil
}

func (s *taxonomyStub) CountTags(context.Context, *productdto.ListTagReq) (int64, error) {
	s.record("CountTags")
	return int64(len(s.tags)), s.countErr
}

func (s *taxonomyStub) ListTags(context.Context, *productdto.ListTagReq) ([]*productdto.TagResp, error) {
	s.record("ListTags")
	return s.tags, s.listErr
}

func (s *taxonomyStub) CountBrands(context.Context, *productdto.ListBrandReq) (int64, error) {
	s.record("CountBrands")
	return int64(len(s.brands)), s.countErr
}

func (s *taxonomyStub) ListBrands(context.Context, *productdto.ListBrandReq) ([]*productdto.BrandResp, error) {
	s.record("ListBrands")
	return s.brands, s.listErr
}

func (s *taxonomyStub) ListCategoryPage(context.Context, *productdto.ListCategoryPageReq) (*productdto.CategoryPageResp, error) {
	s.record("ListCategoryPage")
	if s.pageErr != nil {
		return nil, s.pageErr
	}
	return &productdto.CategoryPageResp{Items: s.pageRows, Total: int64(len(s.pageRows))}, nil
}

func (s *taxonomyStub) ListCategories(context.Context, *productdto.ListCategoryReq) ([]*productdto.CategoryResp, error) {
	s.record("ListCategories")
	return s.categories, s.categoriesErr
}

// GetCategory 只在「父级不在候选列表里」时才被调用（missingParentLabels）：给它一个不存在，
// 与它真实面对的输入一致，也让未被预期的调用不至于静默返回零值。
func (s *taxonomyStub) GetCategory(context.Context, *productdto.GetCategoryReq) (*productdto.CategoryResp, error) {
	s.record("GetCategory")
	return nil, errors.New("product.category.notFound")
}

// taxonomyRequest 把四页挂到一个最小引擎上，用**真实 Jet 渲染器**跑完整个请求。
func taxonomyRequest(t *testing.T, h *productPageHandle, target string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.GET("/admin/product-attributes", h.ProductAttributesPage)
	engine.GET("/admin/product-tags", h.ProductTagsPage)
	engine.GET("/admin/product-categories", h.ProductCategoriesPage)
	engine.GET("/admin/product-brands", h.ProductBrandsPage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// taxonomyPage 一页的路径、成功档数据与「主数据失败」注入点。
type taxonomyPage struct {
	name string
	path string
	// mainDataCalls 成功档里必须发生的取数（反面断言用：没有这条，「跳过所有取数」
	// 也能让失败档全绿）。
	mainDataCalls []string
	// breakMain 让这一页的**主数据**取数失败。
	breakMain func(s *taxonomyStub)
	// mainFailCalls 主数据失败时的**全部**调用：失败那一条之后不许再发任何查询
	//（这条断言是「失败后停止派生查询」的核心 —— 少了它，把守卫去掉也测不出来）。
	mainFailCalls []string
	// breakSecond 让这一页的**第二阶段**取数失败（计数成功之后的那次列表 / 父级候选取数）。
	breakSecond func(s *taxonomyStub)
	// secondStageCalls 第二阶段失败时**已经发生过**的调用（断言它没有把前面的成功取数也跳过）。
	secondStageCalls []string
	// seed 给成功档铺一行数据（页面要真的把这行渲染出来）。
	seed func(s *taxonomyStub)
}

var taxonomyPages = []taxonomyPage{
	{
		name:             "属性页",
		path:             "/admin/product-attributes",
		mainDataCalls:    []string{"CountAttributes", "ListAttributes"},
		mainFailCalls:    []string{"CountAttributes"},
		breakMain:        func(s *taxonomyStub) { s.countErr = errors.New(taxonomyDriverText) },
		breakSecond:      func(s *taxonomyStub) { s.listErr = errors.New(taxonomyDriverText) },
		secondStageCalls: []string{"CountAttributes", "ListAttributes"},
		seed: func(s *taxonomyStub) {
			s.attrs = []*productdto.AttributeResp{{ID: "a1", ProjectID: "p1", Key: "color", Name: "颜色"}}
		},
	},
	{
		name:             "标签页",
		path:             "/admin/product-tags",
		mainDataCalls:    []string{"ListTagRuleTypes", "CountTags", "ListTags"},
		mainFailCalls:    []string{"ListTagRuleTypes", "CountTags"},
		breakMain:        func(s *taxonomyStub) { s.countErr = errors.New(taxonomyDriverText) },
		breakSecond:      func(s *taxonomyStub) { s.listErr = errors.New(taxonomyDriverText) },
		secondStageCalls: []string{"ListTagRuleTypes", "CountTags", "ListTags"},
		seed: func(s *taxonomyStub) {
			s.tags = []*productdto.TagResp{{ID: "t1", ProjectID: "p1", Name: "手工标签", Kind: "manual"}}
		},
	},
	{
		name:             "分类页",
		path:             "/admin/product-categories",
		mainDataCalls:    []string{"ListCategoryPage", "ListCategories"},
		mainFailCalls:    []string{"ListCategoryPage"},
		breakMain:        func(s *taxonomyStub) { s.pageErr = errors.New(taxonomyDriverText) },
		breakSecond:      func(s *taxonomyStub) { s.categoriesErr = errors.New(taxonomyDriverText) },
		secondStageCalls: []string{"ListCategoryPage", "ListCategories"},
		seed: func(s *taxonomyStub) {
			s.pageRows = []*productdto.CategoryResp{{ID: "c1", ProjectID: "p1", Name: "上衣"}}
			s.categories = []*productdto.CategoryResp{{ID: "c1", ProjectID: "p1", Name: "上衣"}}
		},
	},
	{
		name:             "品牌页",
		path:             "/admin/product-brands",
		mainDataCalls:    []string{"CountBrands", "ListBrands"},
		mainFailCalls:    []string{"CountBrands"},
		breakMain:        func(s *taxonomyStub) { s.countErr = errors.New(taxonomyDriverText) },
		breakSecond:      func(s *taxonomyStub) { s.listErr = errors.New(taxonomyDriverText) },
		secondStageCalls: []string{"CountBrands", "ListBrands"},
		seed: func(s *taxonomyStub) {
			s.brands = []*productdto.BrandResp{{ID: "b1", ProjectID: "p1", Name: "品牌一"}}
		},
	},
}

// taxonomyEnv 一页一个环境：工程清单里有一个 p1（?project 缺省时取它）。
func taxonomyEnv(p taxonomyPage) (*productPageHandle, *taxonomyStub) {
	stub := &taxonomyStub{projects: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}}
	h := &productPageHandle{
		projects: productPageProjectsStub{rows: stub.projects},
		products: stub,
	}
	return h, stub
}

// TestTaxonomyPagesLoadFailureRendersSamePage 主数据没读到：同页完整 HTML + 500 + no-store。
func TestTaxonomyPagesLoadFailureRendersSamePage(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			h, stub := taxonomyEnv(p)
			p.breakMain(stub)
			rec := taxonomyRequest(t, h, p.path)
			assertProductPageFailure(t, p.name+"主数据没读到", rec, http.StatusInternalServerError, "这一页的数据没能读出来")
			// 失败那一条之后就停：主数据没读到，后面的派生查询一条都不该发
			//（在没读到的工程上继续查，正是「工程不存在」被渲染成「工程是空的」的成因）。
			if got := strings.Join(stub.calls, ","); got != strings.Join(p.mainFailCalls, ",") {
				t.Fatalf("%s：调用序列应为 %v，实际 %v", p.name, p.mainFailCalls, stub.calls)
			}
			// 驱动原文（SQLSTATE / 关系名 / 驱动前缀）只进日志：正文里连它的片段都不许有。
			//
			// 这里**不能**拿裸表名 `product_attributes` 当判据：改造后 title 交的是 i18n key
			// （admin.product_attributes.title），测试环境没有词条时 Prepare 会原样回填 key，
			// 于是这条断言会对着一个合法 key 报假红。判据改成只在驱动原文里出现的片段。
			body := rec.Body.String()
			for _, part := range []string{"42P01", `relation "`, "does not exist", "pq:"} {
				if strings.Contains(body, part) {
					t.Fatalf("%s：正文里出现了驱动原文 %q，body=%s", p.name, part, headOf(body))
				}
			}
		})
	}
}

// TestTaxonomyPagesSecondStageFailureRendersPage 第二阶段取数失败（计数成功、列表失败）：同样是 500 整页。
//
// 两个方向都钉住：①不是「只有第一处失败才给失败页」（第二处失败也必须是同一张失败页）；
// ②前面已经成功的取数不该被回滚成跳过（调用清单必须停在失败的那一条，且含此前的成功调用）。
func TestTaxonomyPagesSecondStageFailureRendersPage(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			h, stub := taxonomyEnv(p)
			p.breakSecond(stub)
			rec := taxonomyRequest(t, h, p.path+"?project=p1")
			assertProductPageFailure(t, p.name+"第二阶段失败", rec, http.StatusInternalServerError, "这一页的数据没能读出来")
			if got := strings.Join(stub.calls, ","); got != strings.Join(p.secondStageCalls, ",") {
				t.Fatalf("%s：调用序列应为 %v，实际 %v（失败之后不该再发派生取数）", p.name, p.secondStageCalls, stub.calls)
			}
		})
	}
}

// TestTaxonomyPagesUnknownProjectIsBadRequest 未知工程：整页 + 400 + no-store，且一条派生查询都不发。
//
// 这一档是「参数不合法」，不是「读不到数据」：把它做成 500 会让运营去重试，
// 而正确动作是换回一个存在的工程。
func TestTaxonomyPagesUnknownProjectIsBadRequest(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			h, stub := taxonomyEnv(p)
			rec := taxonomyRequest(t, h, p.path+"?project=does-not-exist")
			assertProductPageFailure(t, p.name+"未知工程", rec, http.StatusBadRequest, "请求参数无效")
			if len(stub.calls) != 0 {
				t.Fatalf("%s：未知工程不该发任何派生取数，实际调用了 %v", p.name, stub.calls)
			}
		})
	}
}

// TestTaxonomyPagesProjectListFailureSkipsDerivedQueries 工程清单本身就失败：500 + 零派生查询。
func TestTaxonomyPagesProjectListFailureSkipsDerivedQueries(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			stub := &taxonomyStub{}
			h := &productPageHandle{
				projects: productPageProjectsStub{err: errors.New(taxonomyDriverText)},
				products: stub,
			}
			rec := taxonomyRequest(t, h, p.path)
			assertProductPageFailure(t, p.name+"工程清单失败", rec, http.StatusInternalServerError, "这一页的数据没能读出来")
			if len(stub.calls) != 0 {
				t.Fatalf("%s：工程清单失败后不该再发派生取数，实际调用了 %v", p.name, stub.calls)
			}
		})
	}
}

// TestTaxonomyPagesSuccessRendersPage 成功档：200、完整页壳、不出失败槽，且该发的取数真的发了。
func TestTaxonomyPagesSuccessRendersPage(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			h, stub := taxonomyEnv(p)
			p.seed(stub)
			rec := taxonomyRequest(t, h, p.path+"?project=p1")
			if rec.Code != http.StatusOK {
				t.Fatalf("%s 成功档应 200，实际 %d，body=%s", p.name, rec.Code, headOf(rec.Body.String()))
			}
			if got := rec.Header().Get("Cache-Control"); got == "no-store" {
				t.Fatalf("%s 成功页不该标 no-store（那是失败页的档）", p.name)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "</html>") {
				t.Fatalf("%s 成功页页壳不完整，body=%s", p.name, headOf(body))
			}
			if strings.Contains(body, "这一页的数据没能读出来") {
				t.Fatalf("%s 成功页渲染出了失败槽，body=%s", p.name, headOf(body))
			}
			// 反面断言：这条用来挡「把取数整段跳过」也能让失败档全绿的假绿。
			for _, want := range p.mainDataCalls {
				if !strings.Contains(strings.Join(stub.calls, ","), want) {
					t.Fatalf("%s：成功档应调用 %s，实际调用 %v", p.name, want, stub.calls)
				}
			}
		})
	}
}

// TestTaxonomyPagesDefaultProjectWhenQueryMissing 缺 ?project：取工程清单第一个（不是空工程空态）。
func TestTaxonomyPagesDefaultProjectWhenQueryMissing(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			h, stub := taxonomyEnv(p)
			p.seed(stub)
			rec := taxonomyRequest(t, h, p.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s 缺参数应取默认工程并 200，实际 %d，body=%s", p.name, rec.Code, headOf(rec.Body.String()))
			}
			if len(stub.calls) == 0 {
				t.Fatalf("%s 缺参数时应按默认工程取数，实际一条都没发", p.name)
			}
		})
	}
}

// TestTaxonomyPagesEmptyResultIsNotFailure 合法工程但没有数据：空态，不是失败（200、不标 no-store）。
func TestTaxonomyPagesEmptyResultIsNotFailure(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			h, _ := taxonomyEnv(p)
			rec := taxonomyRequest(t, h, p.path+"?project=p1")
			if rec.Code != http.StatusOK {
				t.Fatalf("%s 空数据是空态不是失败，应 200，实际 %d，body=%s", p.name, rec.Code, headOf(rec.Body.String()))
			}
			if got := rec.Header().Get("Cache-Control"); got == "no-store" {
				t.Fatalf("%s 空态不该标 no-store", p.name)
			}
			if strings.Contains(rec.Body.String(), "这一页的数据没能读出来") {
				t.Fatalf("%s 空态渲染成了失败槽，body=%s", p.name, headOf(rec.Body.String()))
			}
		})
	}
}

// TestTaxonomyPagesFailureSlotIsNotAnEmptyState 失败槽不是空态：不得出现空态文案与「新建」空态入口。
//
// 空态说的是「读到了、没有数据」，失败槽说的是「没读到」——两者混用会让运营以为数据被删了
// （并照着空态里的「新建」按钮再建一份）。
func TestTaxonomyPagesFailureSlotIsNotAnEmptyState(t *testing.T) {
	for _, p := range taxonomyPages {
		t.Run(p.name, func(t *testing.T) {
			h, stub := taxonomyEnv(p)
			p.breakMain(stub)
			rec := taxonomyRequest(t, h, p.path+"?project=p1")
			body := assertProductPageFailure(t, p.name+"失败槽形态", rec, http.StatusInternalServerError, "这一页的数据没能读出来")
			if strings.Contains(body, "empty-state") {
				t.Fatalf("%s：失败页里出现了空态容器（空态吃表头 / 与「没读到」混用），body=%s", p.name, headOf(body))
			}
		})
	}
}
