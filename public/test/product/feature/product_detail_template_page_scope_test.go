package feature

import (
	"context"
	"go_wp/pkg/facing"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/adminctx"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	dtoutbound "go_wp/internal/module/product/outbound/detailtemplate"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
	text "go_wp/pkg/view/jet"
)

func newTemplateScopePageEngine(t *testing.T) (*gin.Engine, *projectScopeFixture, *detailTemplateRecordingPort) {
	t.Helper()
	base := newDetailFixture(t)
	if base == nil {
		return nil, nil, nil
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
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{"contenttemplate:create": true})
		c.Set(adminctx.ButtonsKey, map[string]bool{"contenttemplate.create": true})
	})
	handle := producthttp.NewProductPageHandle(base.products, base.products)
	handle.SetDetailTemplateDeps(base.templates, base.pres)

	port := &detailTemplateRecordingPort{inner: dtoutbound.New(base.templates, base.pres)}
	base.products.SetDetailTemplatePort(port)

	api := producthttp.NewProductHandle(base.products)
	api.SetDetailTemplateWriteDeps(productservice.DetailTemplateDeps{
		Templates: base.templates, Instances: base.pres, Products: base.products, Projects: base.projects,
	})
	engine.GET("/admin/products/template", pageview.UseRenderer(pageview.RenderPage), handle.ProductDetailTemplatePage)
	engine.POST("/api/product/detail-template/create", pageview.HTMLFragment, api.CreateDetailTemplate)
	return engine, scope, port
}

type detailTemplateRecordingPort struct {
	inner productcontract.DetailTemplatePort
	calls []string
}

func (p *detailTemplateRecordingPort) DetailTemplateCandidates(ctx context.Context, projectID, entityType string) ([]productcontract.DetailTemplateCandidateView, error) {
	p.calls = append(p.calls, "Candidates:"+projectID)
	return p.inner.DetailTemplateCandidates(ctx, projectID, entityType)
}

func (p *detailTemplateRecordingPort) DefaultDetailTemplate(ctx context.Context, projectID, entityType string) (string, error) {
	p.calls = append(p.calls, "Default:"+projectID)
	return p.inner.DefaultDetailTemplate(ctx, projectID, entityType)
}

func (p *detailTemplateRecordingPort) BoundDetailTemplate(ctx context.Context, projectID, entityType, entityID string) (productcontract.BoundTemplateView, error) {
	p.calls = append(p.calls, "Bound:"+projectID)
	return p.inner.BoundDetailTemplate(ctx, projectID, entityType, entityID)
}

func (p *detailTemplateRecordingPort) DetailTemplatePanel(ctx context.Context, projectID, entityType, entityID string) (productcontract.DetailTemplatePanelFacts, error) {
	p.calls = append(p.calls, "Panel:"+projectID)
	return p.inner.DetailTemplatePanel(ctx, projectID, entityType, entityID)
}

func (p *detailTemplateRecordingPort) reset() { p.calls = nil }

func (p *detailTemplateRecordingPort) assertAllScopedTo(t *testing.T, want string) {
	t.Helper()
	if len(p.calls) == 0 {
		t.Fatal("详情模板页一条端口取数都没发：端口没接上读模型")
	}
	for _, c := range p.calls {
		if !strings.HasSuffix(c, ":"+want) {
			t.Fatalf("端口方法收到的工程 id 不是页面上解析出的 %q，实际调用 %v", want, p.calls)
		}
	}
	for _, name := range []string{"Candidates", "Default", "Bound"} {
		hit := false
		for _, c := range p.calls {
			if strings.HasPrefix(c, name+":") {
				hit = true
			}
		}
		if !hit {
			t.Fatalf("端口方法 %s 没被调用（三个取数口要一起发），实际 %v", name, p.calls)
		}
	}
}

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

func templateScopeOtherProject(f *projectScopeFixture, projectID string) string {
	if projectID == f.projectID {
		return f.projectB
	}
	return f.projectID
}

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

func templateScopeProductName(t *testing.T, f *projectScopeFixture, projectID, productID string) string {
	t.Helper()
	p, err := f.products.Get(context.Background(), &productdto.GetReq{ID: productID, ProjectID: projectID})
	if err != nil {
		t.Fatalf("读工程 %s 下商品 %s 失败: %v", projectID, productID, err)
	}
	return p.Name
}

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

func TestProductDetailTemplatePageServiceScopesProductLookup(t *testing.T) {
	_, f, port := newTemplateScopePageEngine(t)
	if f == nil {
		return
	}
	f.seedScopeSites(t)

	ctx := context.Background()
	selected := templateScopeResolvedProjectID(t, f)
	mine := templateScopeProductOfProject(t, f, selected)
	others := templateScopeProductOfProject(t, f, templateScopeOtherProject(f, selected))

	scope, err := f.products.ResolveCatalogScope(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	product, err := f.products.Get(ctx, &productdto.GetReq{ID: mine, ProjectID: scope.ProjectID})
	if err != nil || product.ID != mine || scope.ProjectID != selected {
		t.Fatalf("工程范围商品读取失败：scope=%+v product=%+v err=%v", scope, product, err)
	}
	if _, err := f.products.QueryDetailTemplates(ctx, scope.ProjectID); err != nil {
		t.Fatal(err)
	}
	f.products.QueryDefaultDetailTemplate(ctx, scope.ProjectID)
	f.products.QueryBoundDetailTemplate(ctx, scope.ProjectID, mine)

	port.assertAllScopedTo(t, selected)
	port.reset()

	// 跨工程商品必须不可见：按所选工程读取时得到「不存在」，而不是读到别的工程的数据。
	other, otherErr := f.products.Get(ctx, &productdto.GetReq{ID: others, ProjectID: scope.ProjectID})
	if otherErr == nil {
		t.Fatalf("跨工程商品不应可读：%+v", other)
	}
	fact, ok := facing.FactOf(otherErr)
	if !ok || fact.Key != productenums.ErrNotFound {
		t.Fatalf("跨工程读取应回「不存在」，实际：%v", otherErr)
	}

}

func TestProductDetailTemplatePageGETRendersWithoutProjectParam(t *testing.T) {
	engine, f, port := newTemplateScopePageEngine(t)
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

	port.assertAllScopedTo(t, selected)
}

func TestProductDetailTemplateCreateWithoutCopySourceScopesToProject(t *testing.T) {
	engine, f, _ := newTemplateScopePageEngine(t)
	if engine == nil {
		return
	}
	f.seedScopeSites(t)

	beforeA := templateScopeTemplateCount(t, f, f.projectID)
	beforeB := templateScopeTemplateCount(t, f, f.projectB)

	rec := postForm(engine, "/api/product/detail-template/create", url.Values{"projectId": {f.projectID}, "productId": {f.productA}, "name": {"多工程复制默认模板"}})
	assertHTMLSuccess(t, rec, "多工程站点上新建模板应成功，实际 %d：%s")

	afterA := templateScopeTemplateCount(t, f, f.projectID)
	afterB := templateScopeTemplateCount(t, f, f.projectB)
	if afterA != beforeA+1 {
		t.Fatalf("A 站应新增 1 套模板，实际 %d -> %d", beforeA, afterA)
	}
	if afterB != beforeB {
		t.Fatalf("B 站模板数不得变化，实际 %d -> %d", beforeB, afterB)
	}

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
