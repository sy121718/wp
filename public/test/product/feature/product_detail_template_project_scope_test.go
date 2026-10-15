// product_detail_template_project_scope_test.go —— 「详情页模板」两个写入口的**工程作用域**（P167）。
//
// 被钉住的缺陷形态：internal/module/product/inbound/http/product_detail_template.go 的两个写入口
// 读出了表单里的 projectId 却没往下传 —— UpdateURLReq.ProjectID / RebuildReq.ProjectID 收到空串，
// presentation 侧只能走「工程唯一时取该工程」的兜底
// （internal/module/presentation/service/presentation_render.go:503 的 resolveProjectID）。
//
// 为什么既有用例照不出它：既有 feature 用例都跑在**单工程**夹具上，兜底解析出的那个工程恰好
// 就是唯一的那个，于是「传了」与「没传」行为等价，差异不可观测。本文件造**两个工程**：
// 工程不唯一 -> resolveProjectID 直接回 ErrProjectRequired -> 漏传时写操作走不到实例定位，
// 端点渲染失败态提示页（data-jump-state="err"）。
//
// 钉住的接线（变红实验就是把这两处实参改成 ""）：
// 1) POST /admin/products/template/url -> ProductDetailTemplateUpdateURL 的 projectID 实参
// -> productservice.UpdateDetailTemplateURL -> UpdateURLReq.ProjectID -> Service.UpdateURL；
// 2) POST /admin/products/template/apply -> ProductDetailTemplateApply 的 projectID 实参
// -> productservice.ApplyDetailTemplate -> RebuildReq.ProjectID -> Service.Rebuild。
//
// 每条用例都同时断言两件事：「A 站真的改了」与「B 站一个字节都没动」—— 只断言前者时，
// 「写到了别的工程上」这种失败仍然是绿的。
package feature

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	productdto "go_wp/internal/module/product/dto"
	producthttp "go_wp/internal/module/product/inbound/http"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
)

// 两个站点的详情页线上路径。互不相干是刻意的：任何一个落到对方身上都是可观测的差异。
const (
	scopePathA    = "/products/scope-shirt"
	scopePathB    = "/products/scope-bag"
	scopePathANew = "/products/scope-shirt-new"
)

// projectScopeFixture 双工程夹具：detailFixture（工程 A + 生产迁移 + 四个真实 service）之上
// 再建**第二个工程**，并补上两站各自的商品 / 命名模板 / 已发布的详情页实例。
type projectScopeFixture struct {
	*detailFixture
	projectB string

	productA, productB          string
	tplAFirst, tplASwitch, tplB string
	instA, instB                string
}

// newProjectScopeEngine 装配双工程引擎：只挂「详情页模板」的两个写入口。
//
// 不挂鉴权中间件（与 product_detail_template_choice_test.go 的页面引擎同一取舍）：
// 本文件验的是工程作用域，不是权限链。
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
	handle := producthttp.NewProductPageHandle(base.products, base.products, base.projects)
	handle.SetDetailTemplateDeps(base.templates, base.pres)
	engine.POST("/admin/products/template/url", handle.ProductDetailTemplateUpdateURL)
	engine.POST("/admin/products/template/apply", handle.ProductDetailTemplateApply)
	return engine, scope
}

// seedScopeSites 造「两个站点各自有商品 + 命名模板 + 已发布的详情页实例」。
//
// 三套模板的标记互不相同（A 站初版 / A 站切换版 / B 站版式）：产物里出现谁的标记可断言。
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

// scopeTemplate 在指定工程下建一套命名商品详情模板。
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

// scopeProduct 在指定工程下建一个可用商品（含一个变体，价格区间由变体派生）。
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
	// ProjectID 必须显式带上：本夹具里工程不唯一，product 侧的「唯一工程」兜底同样解析不出来。
	if _, err := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
		ProductID: res.ID, ProjectID: projectID, SKUCode: slug + "-L", Price: &price,
	}); err != nil {
		t.Fatalf("建变体失败: %v", err)
	}
	return res.ID
}

// scopeInstance 为指定工程下的商品建立发布实例（显式模板，各自用自己工程的模板）。
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

// instanceOf 读实例（**显式带工程作用域**）：本文件的夹具里工程不唯一，不带作用域的读
// 会先撞 ErrProjectRequired —— 那是另一条链路的事，不该混进本文件的断言。
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

// TestProductDetailTemplateUpdateURLKeepsProjectScope 「改 URL」写入口必须把表单里的工程作用域
// 传给 presentation：A 站的实例路径真的改、A 站新路径上有产物，而 B 站的实例与产物逐字未动。
func TestProductDetailTemplateUpdateURLKeepsProjectScope(t *testing.T) {
	engine, f := newProjectScopeEngine(t)
	if engine == nil {
		return
	}
	f.seedScopeSites(t)

	// 写之前：两站各自的产物都在，且各带自己的标记。
	if html := activeHTML(t, scopePathA); !strings.Contains(html, "A站初版") {
		t.Fatalf("A 站初始产物应使用 A 站初版模板：%s", html)
	}
	bBefore := activeHTML(t, scopePathB)
	if !strings.Contains(bBefore, "B站版式") {
		t.Fatalf("B 站初始产物应使用 B 站版式模板：%s", bBefore)
	}

	rec := postForm(engine, "/admin/products/template/url", url.Values{
		"projectId": {f.projectID}, "productId": {f.productA},
		"newPath": {scopePathANew},
	})
	// 漏传工程作用域时这里就是红的：工程不唯一 -> resolveProjectID 回 ErrProjectRequired
	// -> 端点渲染失败态提示页而不是成功态。
	assertJumpOK(t, rec)

	// A 站：实例路径真的改了，新路径上有产物。
	if got := f.instanceOf(t, f.projectID, f.productA).URLPath; got != scopePathANew {
		t.Fatalf("A 站实例路径应改为 %s，实际 %s", scopePathANew, got)
	}
	if !activeFileExists(scopePathANew) {
		t.Fatalf("A 站新路径 %s 上没有激活产物", scopePathANew)
	}
	if html := activeHTML(t, scopePathANew); !strings.Contains(html, "A站初版") {
		t.Fatalf("A 站新路径的产物仍应是该站自己的模板：%s", html)
	}

	// B 站：实例与产物一个字段都没动。
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

// TestProductDetailTemplateApplyKeepsProjectScope 「切换模板」写入口同理：A 站换了模板且产物
// 随之变化，B 站的绑定与产物一个字节都没动。
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

	rec := postForm(engine, "/admin/products/template/apply", url.Values{
		"projectId": {f.projectID}, "productId": {f.productA}, "templateId": {f.tplASwitch},
	})
	// 同 UpdateURL：漏传工程作用域时这一步红。
	assertJumpOK(t, rec)

	// A 站：绑定换成切换目标，产物随模板变化（旧标记消失）。
	if got := instanceTemplateID(t, f.detailFixture, f.instA); got != f.tplASwitch {
		t.Fatalf("A 站实例应绑定切换后的模板 %s，实际 %s", f.tplASwitch, got)
	}
	html := activeHTML(t, scopePathA)
	if !strings.Contains(html, "A站切换版") || strings.Contains(html, "A站初版") {
		t.Fatalf("A 站产物应换成切换后的模板：%s", html)
	}

	// B 站：绑定 / 路径 / 产物都不许动。
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
