package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/siteurl"
	"go_wp/internal/templates"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

const (
	scopePathA    = "/products/scope-shirt"
	scopePathB    = "/products/scope-bag"
	scopePathANew = "/products/scope-shirt-new"
)

type projectScopeFixture struct {
	*detailFixture
	projectB string

	productA, productB          string
	tplAFirst, tplASwitch, tplB string
	instA, instB                string
}

func newProjectScopeEngine(t *testing.T) (*gin.Engine, *projectScopeFixture) {
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
	engine.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })

	api := producthttp.NewProductHandle(base.products)
	api.SetDetailTemplateWriteDeps(productservice.DetailTemplateDeps{
		Templates: base.templates, Instances: base.pres, Products: base.products, Projects: base.projects,
	})
	engine.POST("/api/product/detail-template/update-url", pageview.HTMLFragment, api.UpdateDetailTemplateURL)
	engine.POST("/api/product/detail-template/apply", pageview.HTMLFragment, api.ApplyDetailTemplate)
	engine.POST("/api/product/detail-template/publish", pageview.HTMLFragment, api.PublishDetailTemplate)
	return engine, scope
}

func (f *projectScopeFixture) seedScopeSites(t *testing.T) {
	t.Helper()
	f.tplAFirst = f.scopeTemplate(t, f.projectID, "A站初版详情页", "A站初版")
	f.tplASwitch = f.scopeTemplate(t, f.projectID, "A站切换版详情页", "A站切换版")
	f.tplB = f.scopeTemplate(t, f.projectB, "B站版式详情页", "B站版式")
	f.productA = f.scopeProduct(t, f.projectID, "A站衬衫", "scope-shirt")
	f.productB = f.scopeProduct(t, f.projectB, "B站手提包", "scope-bag")
	f.instA = f.scopeInstance(t, f.projectID, f.productA, scopePathA, f.tplAFirst).ID
	f.instB = f.scopeInstance(t, f.projectB, f.productB, scopePathB, f.tplB).ID
}

func (f *projectScopeFixture) scopeTemplate(t *testing.T, projectID, name, marker string) string {
	t.Helper()
	res, err := f.templates.Create(context.Background(), &contenttemplatedto.CreateReq{
		EntityType: "product", Name: name, ProjectID: projectID,
		DraftDocument: json.RawMessage(detailDocWithMarker(marker)),
	})
	if err != nil {
		t.Fatalf("在工程 %s 下建模板 %s 失败: %v", projectID, name, err)
	}
	return res.ID
}

func (f *projectScopeFixture) scopeProduct(t *testing.T, projectID, name, slug string) string {
	t.Helper()
	ctx := context.Background()
	desc, err := json.Marshal(map[string]string{"html": "<p>" + name + "的描述</p>"})
	if err != nil {
		t.Fatalf("描述编码失败: %v", err)
	}
	price := 199.0
	res, err := f.products.Create(ctx, &productdto.CreateReq{
		ProjectID: projectID, Name: name, Slug: slug,
		Description:  desc,
		DefaultImage: "/storage/" + slug + "-cover.jpg",
		DefaultPrice: &price,
	})
	if err != nil {
		t.Fatalf("在工程 %s 下建商品 %s 失败: %v", projectID, name, err)
	}

	if _, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProductID: res.ID, ProjectID: projectID, SKUCode: slug + "-L", Price: &price,
	}); err != nil {
		t.Fatalf("建变体失败: %v", err)
	}
	return res.ID
}

func (f *projectScopeFixture) scopeInstance(t *testing.T, projectID, productID, urlPath, templateID string) *presentationdto.InstanceResp {
	t.Helper()
	inst, err := f.pres.CreateInstance(context.Background(), &presentationdto.CreateInstanceReq{
		ProjectID: projectID, EntityType: "product", EntityID: productID,
		URLPath: urlPath, TemplateID: templateID,
	})
	if err != nil {
		t.Fatalf("在工程 %s 下发布商品 %s 失败: %v", projectID, productID, err)
	}
	return inst
}

func (f *projectScopeFixture) instanceOf(t *testing.T, projectID, productID string) *presentationdto.InstanceResp {
	t.Helper()
	inst, err := f.pres.GetByEntity(context.Background(), &presentationdto.GetByEntityReq{
		EntityType: "product", EntityID: productID, ProjectID: projectID,
	})
	if err != nil {
		t.Fatalf("读工程 %s 下商品 %s 的发布实例失败: %v", projectID, productID, err)
	}
	return inst
}

func TestProductDetailTemplateUpdateURLKeepsProjectScope(t *testing.T) {
	engine, f := newProjectScopeEngine(t)
	if engine == nil {
		return
	}
	f.seedScopeSites(t)

	if html := activeHTML(t, scopePathA); !strings.Contains(html, "A站初版") {
		t.Fatalf("A 站初始产物应使用 A 站初版模板：%s", html)
	}
	bBefore := activeHTML(t, scopePathB)
	if !strings.Contains(bBefore, "B站版式") {
		t.Fatalf("B 站初始产物应使用 B 站版式模板：%s", bBefore)
	}

	rec := postForm(engine, "/api/product/detail-template/update-url", url.Values{
		"projectId": {f.projectID}, "productId": {f.productA}, "newPath": {scopePathANew},
	})
	assertHTMLSuccess(t, rec, "改 URL")

	if got := f.instanceOf(t, f.projectID, f.productA).URLPath; got != scopePathANew {
		t.Fatalf("A 站实例路径应改为 %s，实际 %s", scopePathANew, got)
	}
	if !activeFileExists(scopePathANew) {
		t.Fatalf("A 站新路径 %s 上没有激活产物", scopePathANew)
	}
	if html := activeHTML(t, scopePathANew); !strings.Contains(html, "A站初版") {
		t.Fatalf("A 站新路径的产物仍应是该站自己的模板：%s", html)
	}

	bInst := f.instanceOf(t, f.projectB, f.productB)
	if bInst.URLPath != scopePathB {
		t.Fatalf("B 站实例路径不得被改动，实际 %s", bInst.URLPath)
	}
	if got := instanceTemplateID(t, f.detailFixture, f.instB); got != f.tplB {
		t.Fatalf("B 站实例模板绑定不得被改动，实际 %s", got)
	}
	if bAfter := activeHTML(t, scopePathB); bAfter != bBefore {
		t.Fatalf("B 站产物不得被改动:\n改前 %s\n改后 %s", bBefore, bAfter)
	}
}

func TestProductDetailTemplateApplyKeepsProjectScope(t *testing.T) {
	engine, f := newProjectScopeEngine(t)
	if engine == nil {
		return
	}
	f.seedScopeSites(t)

	bBefore := activeHTML(t, scopePathB)
	if !strings.Contains(bBefore, "B站版式") {
		t.Fatalf("B 站初始产物应使用 B 站版式模板：%s", bBefore)
	}

	rec := postForm(engine, "/api/product/detail-template/apply", url.Values{
		"projectId": {f.projectID}, "productId": {f.productA}, "templateId": {f.tplASwitch},
	})
	assertHTMLSuccess(t, rec, "切换模板")

	if got := instanceTemplateID(t, f.detailFixture, f.instA); got != f.tplASwitch {
		t.Fatalf("A 站实例应绑定切换后的模板 %s，实际 %s", f.tplASwitch, got)
	}
	html := activeHTML(t, scopePathA)
	if !strings.Contains(html, "A站切换版") || strings.Contains(html, "A站初版") {
		t.Fatalf("A 站产物应换成切换后的模板：%s", html)
	}

	bInst := f.instanceOf(t, f.projectB, f.productB)
	if bInst.TemplateID != f.tplB {
		t.Fatalf("B 站实例模板绑定不得被改动，实际 %s", bInst.TemplateID)
	}
	if bInst.URLPath != scopePathB {
		t.Fatalf("B 站实例路径不得被改动，实际 %s", bInst.URLPath)
	}
	if bAfter := activeHTML(t, scopePathB); bAfter != bBefore {
		t.Fatalf("B 站产物不得被改动:\n改前 %s\n改后 %s", bBefore, bAfter)
	}
}

func TestProductDetailTemplatePublishKeepsProjectScope(t *testing.T) {
	engine, f := newProjectScopeEngine(t)
	if engine == nil {
		return
	}
	f.seedScopeSites(t)

	const pubSlug = "scope-cap"
	pubA := f.scopeProduct(t, f.projectID, "A站发布用帽", pubSlug)

	bBefore := activeHTML(t, scopePathB)
	if !strings.Contains(bBefore, "B站版式") {
		t.Fatalf("B 站初始产物应使用 B 站版式模板：%s", bBefore)
	}

	rec := postForm(engine, "/api/product/detail-template/publish", url.Values{
		"projectId": {f.projectID}, "productId": {pubA}, "templateId": {f.tplAFirst},
	})
	assertHTMLSuccess(t, rec, "首次发布")

	want := siteurl.DetailPathForProject(context.Background(), f.projects, f.projectID,
		siteurl.KindProduct, pubSlug, pubA)
	if want == "" || !strings.Contains(want, pubSlug) {
		t.Fatalf("派生出的默认路径无效（%q）：判据失效，先查 siteurl 的默认模式", want)
	}
	if got := f.instanceOf(t, f.projectID, pubA).URLPath; got != want {
		t.Fatalf("A 站新实例的 URLPath = %q，want %q（空 urlPath 时应按站点规则派生）", got, want)
	}
	if !activeFileExists(want) {
		t.Fatalf("A 站派生路径 %s 上没有激活产物", want)
	}

	bInst := f.instanceOf(t, f.projectB, f.productB)
	if bInst.URLPath != scopePathB {
		t.Fatalf("B 站实例路径不得被改动，实际 %s", bInst.URLPath)
	}
	if got := instanceTemplateID(t, f.detailFixture, f.instB); got != f.tplB {
		t.Fatalf("B 站实例模板绑定不得被改动，实际 %s", got)
	}
	if bAfter := activeHTML(t, scopePathB); bAfter != bBefore {
		t.Fatalf("B 站产物不得被改动:\n改前 %s\n改后 %s", bBefore, bAfter)
	}
}
