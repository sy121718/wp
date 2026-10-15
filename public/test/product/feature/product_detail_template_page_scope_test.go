// product_detail_template_page_scope_test.go —— 「详情页模板」整页读模型与「新建模板」的工程作用域（P169）。
//
// 被钉住的两类缺陷同形：**调用方手里有工程 id，却没把它交给下游的取数一跳**。
//
//	A) internal/module/product/service/product_detail_template_page.go 的 ProductDetailTemplatePage：
//	   取商品那一跳原先不带作用域。多工程站点上 product 侧 resolveProjectID 解析不出唯一工程、
//	   直接回 ErrInvalidParam，于是被该函数的 ErrInvalidParam 分支判成 HTTP 400 —— 不带 projectId
//	   打开 /admin/products/template 整页打不开，而那个商品本来取得到（它就在该函数已算出的
//	   selected 工程里）。
//	B) internal/module/product/service/product_detail_template_ops.go 的 DetailTemplateDocument：
//	   原先走**无工程**的 ResolveTemplate；contenttemplate 侧 resolveProjectID("") 在多工程下回
//	   ErrProjectRequired —— 于是「新建模板但不指定复制来源」这条最常用的路径整条失败。
//
// 为什么既有用例照不出：既有 feature 用例都跑在**单工程**夹具上，兜底解析出的那一个恰好就是
// 唯一的那一个，「传了」与「没传」行为等价。本文件复用同包 product_detail_template_project_scope_test.go
// 的 projectScopeFixture（detailFixture 之上再建第二个工程），并断言两条**方向相反**的事实：
//   - 商品属于「解析出的工程」时，不带 projectId 也必须取到（漏传作用域 -> 400，红）；
//   - 商品只属于**另一个**工程时，也必须走到「取商品」而不是在解析工程时就以 400 断掉
//     （RLS 未切换前超级用户连接下跨工程仍可见，故不断言 404 —— 见用例内的说明）。
package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/adminctx"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
)

// newTemplateScopePageEngine 在双工程夹具上装配「详情模板页 + 新建模板」两个入口。
//
// 权限键照 choice_test 的做法注入（页面的「新建命名模板」入口按权限显隐）：本文件验的是
// 工程作用域，不是权限链，不挂鉴权中间件。
func newTemplateScopePageEngine(t *testing.T) (*gin.Engine, *projectScopeFixture) {
	t.Helper()
	base := newDetailFixture(t)
	if base == nil {
		return nil, nil
	}
	other, err := base.projects.Create(context.Background(), &projectdto.CreateReq{Name: "第二站点工程"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}
	if other.ID == base.projectID {
		t.Fatalf("第二个工程 id 与第一个相同：%s", other.ID)
	}
	scope := &projectScopeFixture{detailFixture: base, projectB: other.ID}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{"contenttemplate:create": true})
		c.Set(adminctx.ButtonsKey, map[string]bool{"contenttemplate.create": true})
	})
	handle := producthttp.NewProductPageHandle(base.products, base.products, base.projects)
	handle.SetDetailTemplateDeps(base.templates, base.pres)
	// 把「详情页模板」整页的**只读端口**退回未注入档（SetDetailTemplateDeps 会顺带注入它）。
	//
	// 为什么：本文件钉的是「取商品那一跳」的工程作用域，而该端口自己的三条读口
	//（DetailTemplateCandidates / DefaultDetailTemplate / BoundDetailTemplate）**同样没有工程参数**
	// ——多工程下它们在取商品之后才失败，把本判据掩盖成 500。那是同一缺陷形态的另一处，
	// 修它要动 product 契约 + outbound 适配器 + 两处既有测试的 fake（不在本批清单内，
	// 已作为清单外发现上报）。端口未注入时这一跳降级成空清单（page.go 文件头第 1 条之外
	// 的既有降级口径），于是本用例只测它要测的那一跳。
	base.products.SetDetailTemplatePort(nil)
	engine.GET("/admin/products/template", handle.ProductDetailTemplatePage)
	engine.POST("/admin/products/template/create", handle.ProductDetailTemplateCreate)
	return engine, scope
}

// templateScopeResolvedProjectID 复现 service 的工程解析口径：不带 projectId 时取**工程清单第一个**。
//
// 不硬编码「工程 A」：工程清单的顺序由 project service 决定，写死顺序会让判据跟着夹具一起漂。
func templateScopeResolvedProjectID(t *testing.T, f *projectScopeFixture) string {
	t.Helper()
	projects, err := f.projects.List(context.Background())
	if err != nil {
		t.Fatalf("读工程清单失败: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("本文件的判据要求多工程夹具，实际 %d 个工程", len(projects))
	}
	return projects[0].ID
}

// templateScopeOtherProject 夹具里除 projectID 之外的另一个工程。
func templateScopeOtherProject(f *projectScopeFixture, projectID string) string {
	if projectID == f.projectID {
		return f.projectB
	}
	return f.projectID
}

// templateScopeProductOfProject 指定工程下的商品 id（夹具每个工程各一个）。
func templateScopeProductOfProject(t *testing.T, f *projectScopeFixture, projectID string) string {
	t.Helper()
	switch projectID {
	case f.projectID:
		return f.productA
	case f.projectB:
		return f.productB
	}
	t.Fatalf("未知工程 %s", projectID)
	return ""
}

// templateScopeProductName 读指定工程下商品的名字（页面正文里应出现它）。
func templateScopeProductName(t *testing.T, f *projectScopeFixture, projectID, productID string) string {
	t.Helper()
	p, err := f.products.Get(context.Background(), &productdto.GetReq{ID: productID, ProjectID: projectID})
	if err != nil {
		t.Fatalf("读工程 %s 下商品 %s 失败: %v", projectID, productID, err)
	}
	return p.Name
}

// templateScopeTemplateCount 指定工程下的模板套数（List 显式带工程，多工程夹具下才读得出来）。
func templateScopeTemplateCount(t *testing.T, f *projectScopeFixture, projectID string) int {
	t.Helper()
	list, err := f.templates.List(context.Background(), &contenttemplatedto.ListReq{
		EntityType: "product", ProjectID: projectID,
	})
	if err != nil {
		t.Fatalf("列工程 %s 的模板失败: %v", projectID, err)
	}
	return len(list)
}

// TestProductDetailTemplatePageServiceScopesProductLookup 服务层：整页读模型取商品那一跳必须带
// 解析出的工程作用域。两条方向相反的断言 —— 只断言「取到了」时，「取的是另一个工程的商品」
// 仍然是绿的。
func TestProductDetailTemplatePageServiceScopesProductLookup(t *testing.T) {
	_, f := newTemplateScopePageEngine(t)
	if f == nil {
		return
	}
	f.seedScopeSites(t)

	ctx := context.Background()
	selected := templateScopeResolvedProjectID(t, f)
	mine := templateScopeProductOfProject(t, f, selected)
	others := templateScopeProductOfProject(t, f, templateScopeOtherProject(f, selected))

	// 1) 商品属于解析出的工程：不带 projectId 也必须取到它。
	//    漏给作用域时 product 侧 resolveProjectID 在多工程下回 ErrInvalidParam -> HTTPStatus 400。
	view, err := f.products.ProductDetailTemplatePage(ctx, &productdto.ProductDetailTemplatePageReq{ProductID: mine})
	if err != nil {
		t.Fatalf("不带 projectId 打开详情模板页读模型报错：%v", err)
	}
	if view.SelectedProject != selected {
		t.Fatalf("解析出的工程 = %q，want %q", view.SelectedProject, selected)
	}
	if view.HTTPStatus != 0 {
		t.Fatalf("商品属于解析出的工程，HTTPStatus 应为 0（200），实际 %d（400 = 取商品那一跳没带作用域）", view.HTTPStatus)
	}
	if !view.Ready || view.Product.ID != mine {
		t.Fatalf("页面读模型未取到该商品：Ready=%v Product=%+v，want %s", view.Ready, view.Product, mine)
	}

	// 2) 商品只属于**另一个**工程：同样必须走到「取商品」这一步，而不是在解析工程时以 400 断掉
	//    （400 只可能来自 product 侧 resolveProjectID —— 那正是「没带作用域」的症状）。
	//
	//    这里**不**断言 404：本夹具用的是超级用户连接，RLS 策略不生效（AGENTS.md 的 DB-009
	//    切换顺序：先包 rls.InProjectScope、再把 database.user 换成非超级角色），跨工程可见性
	//    由 RLS 那一层负责 —— 断言 404 等于在这里测「RLS 切完了没有」，那是另一条链路的事。
	otherView, otherErr := f.products.ProductDetailTemplatePage(ctx, &productdto.ProductDetailTemplatePageReq{ProductID: others})
	if otherErr != nil {
		t.Fatalf("读另一个工程的商品不该报参数错（那是「没带作用域」的症状）：%v", otherErr)
	}
	if otherView.HTTPStatus == http.StatusBadRequest {
		t.Fatalf("HTTPStatus = 400：取商品那一跳没把解析出的工程传下去（多工程下 resolveProjectID 回 ErrInvalidParam）")
	}
}

// TestProductDetailTemplatePageGETRendersWithoutProjectParam 端点层：多工程站点上**不带** projectId
// 打开 GET /admin/products/template 必须成功渲染（不是 400）。
func TestProductDetailTemplatePageGETRendersWithoutProjectParam(t *testing.T) {
	engine, f := newTemplateScopePageEngine(t)
	if engine == nil {
		return
	}
	f.seedScopeSites(t)

	selected := templateScopeResolvedProjectID(t, f)
	mine := templateScopeProductOfProject(t, f, selected)
	name := templateScopeProductName(t, f, selected, mine)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/products/template?product="+mine, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("多工程站点上不带 projectId 打开页面应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"商品详情页模板", name} {
		if !strings.Contains(body, want) {
			t.Fatalf("详情模板页缺少 %q：%s", want, body)
		}
	}
}

// TestProductDetailTemplateCreateWithoutCopySourceScopesToProject 端点层：「新建模板但不指定复制来源」
// 在多工程站点上必须成功，且复制的是**该工程**的默认模板文档（不是空文档、也不是别的工程的模板）。
func TestProductDetailTemplateCreateWithoutCopySourceScopesToProject(t *testing.T) {
	engine, f := newTemplateScopePageEngine(t)
	if engine == nil {
		return
	}
	f.seedScopeSites(t)

	beforeA := templateScopeTemplateCount(t, f, f.projectID)
	beforeB := templateScopeTemplateCount(t, f, f.projectB)

	// 刻意**不传 copyFrom**：缺省来源 = 该工程下 product 类型的详情模板 —— 这正是原先走无工程
	// ResolveTemplate 的那条分支（多工程下 ErrProjectRequired -> 失败态提示页）。
	rec := postForm(engine, "/admin/products/template/create", url.Values{
		"projectId": {f.projectID}, "productId": {f.productA}, "name": {"多工程复制默认模板"},
	})
	assertJumpOK(t, rec)

	afterA := templateScopeTemplateCount(t, f, f.projectID)
	afterB := templateScopeTemplateCount(t, f, f.projectB)
	if afterA != beforeA+1 {
		t.Fatalf("A 站应新增 1 套模板，实际 %d -> %d", beforeA, afterA)
	}
	if afterB != beforeB {
		t.Fatalf("B 站模板数不得变化，实际 %d -> %d", beforeB, afterB)
	}

	// 复制来源取的是**本工程**的默认模板文档：新建的那套必须带非空文档（空文档 = 复制那一跳没取到）。
	list, err := f.templates.List(context.Background(), &contenttemplatedto.ListReq{
		EntityType: "product", ProjectID: f.projectID,
	})
	if err != nil {
		t.Fatalf("列 A 站模板失败: %v", err)
	}
	newID := ""
	for _, tpl := range list {
		if tpl.Name == "多工程复制默认模板" {
			newID = tpl.ID
		}
	}
	if newID == "" {
		t.Fatalf("新建的模板没出现在 A 站的模板清单里：%+v", list)
	}
	got, err := f.templates.Get(context.Background(), &contenttemplatedto.GetReq{ID: newID})
	if err != nil {
		t.Fatalf("读新建模板文档失败: %v", err)
	}
	if got == nil || strings.TrimSpace(string(got.DraftDocument)) == "" || strings.TrimSpace(string(got.DraftDocument)) == "null" {
		t.Fatalf("新建模板的文档为空：复制来源那一跳没取到本工程的默认模板（文档 = %q）", string(got.DraftDocument))
	}
}
