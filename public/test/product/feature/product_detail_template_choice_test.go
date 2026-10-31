package feature

import (
	"context"
	"errors"
	"go_wp/internal/adminui"
	json "go_wp/pkg/jsonx"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/middleware/adminctx"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	productcontract "go_wp/internal/module/product/contract"
	producthttp "go_wp/internal/module/product/inbound/http"
	productservice "go_wp/internal/module/product/service"
	"go_wp/internal/pipeline"
	"go_wp/internal/templates"
	"go_wp/public/test/support"

	"github.com/google/uuid"
)

func detailDocWithMarker(marker string) string {
	return "{\"settings\":{\"layout\":{\"mode\":\"full\"}},\"root\":[" +
		"{\"id\":\"mark\",\"type\":\"core.heading\",\"props\":{\"text\":\"" + marker + "\",\"tag\":\"h2\"}}," +
		"{\"id\":\"pd\",\"type\":\"core.product\",\"props\":{\"titleField\":\"product.name\",\"priceField\":\"product.priceRange\",\"descriptionField\":\"product.description\"}}]}"
}

func createNamedTemplate(t *testing.T, f *detailFixture, name, marker string) *contenttemplatedto.TemplateResp {
	t.Helper()
	res, err := f.templates.Create(context.Background(), &contenttemplatedto.CreateReq{
		EntityType: "product", Name: name, ProjectID: f.projectID,
		DraftDocument: json.RawMessage(detailDocWithMarker(marker)),
	})
	if err != nil {
		t.Fatalf("创建命名模板 %s 失败: %v", name, err)
	}
	return res
}

func (f *detailFixture) publishWithTemplate(t *testing.T, productID, urlPath, templateID string) *presentationdto.InstanceResp {
	t.Helper()
	inst, err := f.pres.CreateInstance(context.Background(), &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "product", EntityID: productID,
		URLPath: urlPath, TemplateID: templateID,
	})
	if err != nil {
		t.Fatalf("按指定模板创建发布实例失败: %v", err)
	}
	return inst
}

func canonicalOf(html string) string {
	const prefix = "<link rel=\"canonical\" href=\""
	i := strings.Index(html, prefix)
	if i < 0 {
		return ""
	}
	rest := html[i+len(prefix):]
	j := strings.Index(rest, "\">")
	if j < 0 {
		return ""
	}
	return rest[:j]
}

var urlSEOFragments = []string{
	"<link rel=\"canonical\"",
	"<meta property=\"og:url\"",
	"<script type=\"application/ld+json\">",
}

func stripURLTags(html string) string {
	lines := strings.Split(html, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		skip := false
		for _, frag := range urlSEOFragments {
			if strings.HasPrefix(trimmed, frag) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func countRows(t *testing.T, db *gorm.DB, table, where string, args ...any) int {
	t.Helper()
	var n int64
	q := db.Table(table)
	if where != "" {
		q = q.Where(where, args...)
	}
	if err := q.Count(&n).Error; err != nil {
		t.Fatalf("统计 %s 失败: %v", table, err)
	}
	return int(n)
}

func activeFileExists(urlPath string) bool {
	_, err := os.Stat(filepath.Join(pipeline.ActiveRoot(), strings.TrimPrefix(urlPath, "/"), "index.html"))
	return err == nil
}

func instanceTemplateID(t *testing.T, f *detailFixture, instanceID string) string {
	t.Helper()
	var id string
	if err := f.db.Raw("SELECT template_id FROM presentation_instances WHERE id = ?", instanceID).Scan(&id).Error; err != nil {
		t.Fatalf("读取实例模板绑定失败: %v", err)
	}
	return id
}

func TestProductDetailMultiNamedTemplatesVersioned(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	activity := createNamedTemplate(t, f, "夏季活动详情页", "活动版式")
	minimal := createNamedTemplate(t, f, "极简详情页", "极简版式")

	list, err := f.templates.List(ctx, &contenttemplatedto.ListReq{EntityType: "product"})
	if err != nil {
		t.Fatalf("按类型列模板失败: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("同一商品类型下应有 3 套命名模板，实际 %d", len(list))
	}
	names := map[string]bool{}
	for _, tpl := range list {
		names[tpl.Name] = true
	}
	for _, want := range []string{"商品详情页", "夏季活动详情页", "极简详情页"} {
		if !names[want] {
			t.Fatalf("模板清单应含 %q，实际 %v", want, names)
		}
	}

	if _, err = f.templates.Update(ctx, &contenttemplatedto.UpdateReq{
		ID: activity.ID, DraftDocument: json.RawMessage(detailDocWithMarker("活动版式 v2")),
	}); err != nil {
		t.Fatalf("更新命名模板失败: %v", err)
	}
	updated, err := f.templates.Get(ctx, &contenttemplatedto.GetReq{ID: activity.ID})
	if err != nil {
		t.Fatalf("读取命名模板失败: %v", err)
	}
	if updated.DraftVersion != 2 {
		t.Fatalf("被修改的模板应到 v2，实际 v%d", updated.DraftVersion)
	}
	untouched, err := f.templates.Get(ctx, &contenttemplatedto.GetReq{ID: minimal.ID})
	if err != nil {
		t.Fatalf("读取另一套模板失败: %v", err)
	}
	if untouched.DraftVersion != 1 {
		t.Fatalf("未被修改的模板应保持 v1，实际 v%d", untouched.DraftVersion)
	}

	got, err := f.templates.ResolveTemplateByID(ctx, minimal.ID)
	if err != nil {
		t.Fatalf("ResolveTemplateByID 失败: %v", err)
	}
	if got.TemplateID != minimal.ID || got.Version != 1 || got.TemplateName != "极简详情页" {
		t.Fatalf("按 ID 解析应得极简详情页 v1，实际 %+v", got)
	}
	if !strings.Contains(string(got.Document), "极简版式") {
		t.Fatalf("按 ID 解析应取该模板自己的文档: %s", got.Document)
	}

	if _, err = f.templates.ResolveTemplateByID(ctx, uuid.NewString()); err == nil {
		t.Fatalf("不存在的模板 ID 应报错")
	} else if !strings.Contains(err.Error(), contenttemplateenums.ErrNotFound) {
		t.Fatalf("错误应为未找到: %v", err)
	}
}

func TestProductPublishWithChosenTemplate(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	chosen := createNamedTemplate(t, f, "极简详情页", "极简版式")
	productID := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉", 99, 99)

	inst := f.publishWithTemplate(t, productID, "/products/summer-shirt", chosen.ID)
	if inst.TemplateID != chosen.ID {
		t.Fatalf("实例应绑定指定模板 %s，实际 %s", chosen.ID, inst.TemplateID)
	}
	if got := instanceTemplateID(t, f, inst.ID); got != chosen.ID {
		t.Fatalf("落库的模板绑定应为指定模板，实际 %s", got)
	}
	html := activeHTML(t, "/products/summer-shirt")
	if !strings.Contains(html, "极简版式") {
		t.Fatalf("产物应使用指定模板渲染: %s", html)
	}
	if !strings.Contains(html, "夏季衬衫") {
		t.Fatalf("产物仍应填入商品数据: %s", html)
	}
}

func TestProductPreviewBeforePublish(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	chosen := createNamedTemplate(t, f, "极简详情页", "极简版式")
	productID := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉", 99, 99)

	res, err := f.pres.PreviewInstance(ctx, &presentationdto.PreviewInstanceReq{
		ProjectID: f.projectID, EntityType: "product", EntityID: productID, TemplateID: chosen.ID,
	})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if res.TemplateID != chosen.ID || res.TemplateVersion != 1 || res.TemplateName != "极简详情页" {
		t.Fatalf("预览响应应带上实际使用的模板与版本: %+v", res)
	}
	for _, want := range []string{"极简版式", "夏季衬衫", "¥99"} {
		if !strings.Contains(res.HTML, want) {
			t.Fatalf("预览 HTML 应含 %q: %s", want, res.HTML)
		}
	}

	if n := countRows(t, f.db, "presentation_instances", "entity_id = ?", productID); n != 0 {
		t.Fatalf("预览不应创建实例，实际 %d 条", n)
	}
	if n := countRows(t, f.db, "presentation_artifacts", ""); n != 0 {
		t.Fatalf("预览不应写产物行，实际 %d 条", n)
	}
	if n := countRows(t, f.db, "document_snapshots", ""); n != 0 {
		t.Fatalf("预览不应写快照，实际 %d 条", n)
	}
	if activeFileExists("/products/summer-shirt") {
		t.Fatalf("预览不应激活访问面产物")
	}

	f.publishWithTemplate(t, productID, "/products/summer-shirt", chosen.ID)
	published := activeHTML(t, "/products/summer-shirt")
	if !strings.Contains(res.HTML, `name="site-preview-mode" content="readonly"`) || strings.Contains(published, `name="site-preview-mode"`) {
		t.Fatal("预览必须声明只读，正式产物不能携带模拟上下文")
	}
	if stripURLTags(support.ComparableSiteHTML(t, published)) != stripURLTags(support.ComparableSiteHTML(t, res.HTML)) {
		t.Fatalf("发布产物应与预览渲染（除 URL 相关 SEO 片段外）一致\n预览: %s\n产物: %s", res.HTML, published)
	}
	if got := canonicalOf(published); got != "/products/summer-shirt" {
		t.Fatalf("发布产物应带实例路径的 canonical，实际 %q", got)
	}
	if !strings.Contains(published, "<meta property=\"og:url\" content=\"/products/summer-shirt\">") {
		t.Fatal("发布产物应带 og:url")
	}
	if got := canonicalOf(res.HTML); got != "" {
		t.Fatalf("预览不应输出 canonical，实际 %q", got)
	}
	if !strings.Contains(res.HTML, "<script type=\"application/ld+json\">") {
		t.Fatal("预览仍应输出结构化数据（只是不含 URL）")
	}

	def, err := f.templates.ResolveTemplate(ctx, "product")
	if err != nil {
		t.Fatalf("解析类型默认模板失败: %v", err)
	}
	fallback, err := f.pres.PreviewInstance(ctx, &presentationdto.PreviewInstanceReq{
		ProjectID: f.projectID, EntityType: "product", EntityID: productID,
	})
	if err != nil {
		t.Fatalf("按默认模板预览失败: %v", err)
	}
	if fallback.TemplateID != def.TemplateID {
		t.Fatalf("未指定模板的预览应使用类型默认模板 %s，实际 %s", def.TemplateID, fallback.TemplateID)
	}
}

func TestProductPreviewRejectsForeignTemplateType(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	doc := json.RawMessage(detailDocWithMarker("文章版式"))
	now := time.Now().UTC()
	m := contenttemplatemodel.NewModel(f.db)
	tplID, verID := uuid.NewString(), uuid.NewString()
	if err := m.CreateWithVersion(ctx, &contenttemplatemodel.TemplateEntity{
		ID: tplID, ProjectID: f.projectID, Name: "文章模板", EntityType: "article",
		DraftDocument: doc, DraftVersion: 1, CurrentVersionID: &verID, CreatedAt: now, UpdatedAt: now,
	}, &contenttemplatemodel.VersionEntity{
		ID: verID, TemplateID: tplID, Version: 1, Document: doc, SourceHash: "article-doc",
		CreatedBy: uuid.Nil.String(), CreatedAt: now,
	}); err != nil {
		t.Fatalf("直写文章模板失败: %v", err)
	}
	productID := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉", 99, 99)

	_, err := f.pres.PreviewInstance(ctx, &presentationdto.PreviewInstanceReq{
		ProjectID: f.projectID, EntityType: "product", EntityID: productID, TemplateID: tplID,
	})
	if err == nil {
		t.Fatalf("跨类型模板预览应被拒绝")
	}
	if !strings.Contains(err.Error(), presentationenums.ErrTemplateTypeMismatch) {
		t.Fatalf("错误应说明模板与内容类型不匹配: %v", err)
	}
	_, err = f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "product", EntityID: productID,
		URLPath: "/products/summer-shirt", TemplateID: tplID,
	})
	if err == nil {
		t.Fatalf("跨类型模板发布应被拒绝")
	}
}

func TestProductSwitchTemplateChangesArtifact(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	first := createNamedTemplate(t, f, "活动版式详情页", "活动版式")
	second := createNamedTemplate(t, f, "极简详情页", "极简版式")
	productID := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉", 99, 99)

	inst := f.publishWithTemplate(t, productID, "/products/summer-shirt", first.ID)
	if html := activeHTML(t, "/products/summer-shirt"); !strings.Contains(html, "活动版式") {
		t.Fatalf("首个产物应使用第一套模板: %s", html)
	}

	switched, err := f.pres.Rebuild(ctx, &presentationdto.RebuildReq{EntityID: productID, TemplateID: second.ID})
	if err != nil {
		t.Fatalf("切换模板重新发布失败: %v", err)
	}
	if switched.TemplateID != second.ID {
		t.Fatalf("切换后实例应绑定第二套模板，实际 %s", switched.TemplateID)
	}
	if got := instanceTemplateID(t, f, inst.ID); got != second.ID {
		t.Fatalf("切换应落库到实例绑定，实际 %s", got)
	}
	html := activeHTML(t, "/products/summer-shirt")
	if !strings.Contains(html, "极简版式") {
		t.Fatalf("切换后产物应使用第二套模板渲染: %s", html)
	}
	if strings.Contains(html, "活动版式") {
		t.Fatalf("切换后产物不应残留第一套模板的版式: %s", html)
	}

	if _, err = f.pres.Rebuild(ctx, &presentationdto.RebuildReq{EntityID: productID}); err != nil {
		t.Fatalf("内容更新重建失败: %v", err)
	}
	html = activeHTML(t, "/products/summer-shirt")
	if !strings.Contains(html, "极简版式") || strings.Contains(html, "活动版式") {
		t.Fatalf("未显式切换模板的重建应沿用实例绑定模板: %s", html)
	}

	ids, err := f.pres.MarkStaleByDependency(ctx, pipeline.DepKindDirectContent, "product:"+productID)
	if err != nil {
		t.Fatalf("标记依赖失效失败: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("应命中 1 个实例，实际 %v", ids)
	}
	if err = f.pres.RebuildStale(ctx, ids); err != nil {
		t.Fatalf("自动重建失败: %v", err)
	}
	if got := instanceTemplateID(t, f, inst.ID); got != second.ID {
		t.Fatalf("自动重建后模板绑定应保持第二套，实际 %s", got)
	}
}

func TestProductPublishFallsBackToTypeDefault(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	seeded, err := f.templates.ResolveTemplate(ctx, "product")
	if err != nil {
		t.Fatalf("解析类型默认模板失败: %v", err)
	}
	productID := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉", 99, 99)

	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "product", EntityID: productID,
		URLPath: "/products/summer-shirt",
	})
	if err != nil {
		t.Fatalf("不指定模板发布失败: %v", err)
	}
	if inst.TemplateID != seeded.TemplateID {
		t.Fatalf("不指定模板应落到类型默认模板 %s，实际 %s", seeded.TemplateID, inst.TemplateID)
	}
	if !strings.Contains(activeHTML(t, "/products/summer-shirt"), "夏季衬衫") {
		t.Fatalf("默认模板产物应含商品数据")
	}

	if now, rerr := f.templates.ResolveTemplate(ctx, "product"); rerr != nil || now.TemplateID != seeded.TemplateID {
		t.Fatalf("未新增模板时默认解析应保持同一套: %v / %+v", rerr, now)
	}
}

func newDetailTemplatePageEngine(t *testing.T) (*gin.Engine, *detailFixture) {
	t.Helper()
	f := newDetailFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)

	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{"contenttemplate:create": true})
		c.Set(adminctx.ButtonsKey, map[string]bool{"contenttemplate.create": true})
	})
	handle := producthttp.NewProductPageHandle(f.products, f.products)
	handle.SetDetailTemplateDeps(f.templates, f.pres)

	api := producthttp.NewProductHandle(f.products)
	api.SetDetailTemplateWriteDeps(productservice.DetailTemplateDeps{
		Templates: f.templates, Instances: f.pres, Products: f.products, Projects: f.projects,
	})
	engine.GET("/admin/products/template", pageview.UseRenderer(adminui.RenderPage), handle.ProductDetailTemplatePage)
	engine.POST("/admin/products/template/preview", handle.ProductDetailTemplatePreview)
	engine.GET("/admin/products/preview-frame", handle.ProductPreviewFrame)
	engine.POST("/api/product/detail-template/create", api.CreateDetailTemplate)
	engine.POST("/api/product/detail-template/publish", api.PublishDetailTemplate)
	engine.POST("/api/product/detail-template/apply", api.ApplyDetailTemplate)
	return engine, f
}

func TestProductDetailTemplatePageFlow(t *testing.T) {
	engine, f := newDetailTemplatePageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	first := createNamedTemplate(t, f, "活动版式详情页", "活动版式")
	second := createNamedTemplate(t, f, "极简详情页", "极简版式")
	productID := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉", 99, 99)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/products/template?project="+f.projectID+"&product="+productID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("详情模板页应 200，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"商品详情页模板", "夏季衬衫",
		"活动版式详情页", "极简详情页", "商品详情页",
		"v1", "新建命名模板", "预览渲染效果", "发布详情页",
		"action=\"/admin/products/template/preview\"",
		"name=\"csrf_token\"",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("详情模板页缺少 %q", want)
		}
	}

	rec = postForm(engine, "/admin/products/template/preview", url.Values{
		"projectId": {f.projectID}, "productId": {productID}, "templateId": {second.ID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("页面预览应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("预览应返回 HTML，实际 Content-Type=%q", ct)
	}
	if !strings.Contains(rec.Body.String(), "极简版式") {
		t.Fatalf("页面预览应渲染选中模板: %s", rec.Body.String())
	}
	if n := countRows(t, f.db, "presentation_instances", "entity_id = ?", productID); n != 0 {
		t.Fatalf("页面预览不应创建实例，实际 %d 条", n)
	}

	rec = postJSON(t, engine, "/api/product/detail-template/create", map[string]any{
		"projectId": f.projectID, "productId": productID,
		"name": "节庆详情页", "copyFrom": first.ID,
	})
	if env := decodeJSONEnvelope(t, rec); env.Code != http.StatusOK {
		t.Fatalf("新建命名模板应成功，实际 %d：%s", env.Code, env.Message)
	}
	list, err := f.templates.List(ctx, &contenttemplatedto.ListReq{EntityType: "product"})
	if err != nil {
		t.Fatalf("列模板失败: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("新建后应有 4 套命名模板，实际 %d", len(list))
	}

	rec = postJSON(t, engine, "/api/product/detail-template/publish", map[string]any{
		"projectId": f.projectID, "productId": productID, "templateId": first.ID,
	})
	if env := decodeJSONEnvelope(t, rec); env.Code != http.StatusOK {
		t.Fatalf("首次发布应成功，实际 %d：%s", env.Code, env.Message)
	}
	if !strings.Contains(activeHTML(t, "/products/summer-shirt"), "活动版式") {
		t.Fatalf("页面发布应使用选中模板")
	}
	inst, err := f.pres.GetByEntity(ctx, &presentationdto.GetByEntityReq{EntityType: "product", EntityID: productID})
	if err != nil {
		t.Fatalf("读实例绑定失败: %v", err)
	}
	if inst.TemplateID != first.ID {
		t.Fatalf("实例应绑定第一套模板，实际 %s", inst.TemplateID)
	}

	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/products/template?project="+f.projectID+"&product="+productID, nil))
	if body = rec.Body.String(); !strings.Contains(body, "切换模板并重新发布") || strings.Contains(body, "发布详情页") {
		t.Fatalf("实例已存在时应展示切换入口")
	}

	rec = postJSON(t, engine, "/api/product/detail-template/apply", map[string]any{
		"projectId": f.projectID, "productId": productID, "templateId": second.ID,
	})
	if env := decodeJSONEnvelope(t, rec); env.Code != http.StatusOK {
		t.Fatalf("切换模板应成功，实际 %d：%s", env.Code, env.Message)
	}
	html := activeHTML(t, "/products/summer-shirt")
	if !strings.Contains(html, "极简版式") || strings.Contains(html, "活动版式") {
		t.Fatalf("切换后产物应换成第二套模板: %s", html)
	}
}

type detailTemplateFailingPort struct {
	candidates int
	derived    int
}

func (p *detailTemplateFailingPort) DetailTemplateCandidates(context.Context, string, string) ([]productcontract.DetailTemplateCandidateView, error) {
	p.candidates++
	return nil, errors.New("template list unavailable")
}

func (p *detailTemplateFailingPort) DefaultDetailTemplate(context.Context, string, string) (string, error) {
	p.derived++
	return "", nil
}

func (p *detailTemplateFailingPort) BoundDetailTemplate(context.Context, string, string, string) (productcontract.BoundTemplateView, error) {
	p.derived++
	return productcontract.BoundTemplateView{}, nil
}

func (p *detailTemplateFailingPort) DetailTemplatePanel(context.Context, string, string, string) (productcontract.DetailTemplatePanelFacts, error) {
	p.derived++
	return productcontract.DetailTemplatePanelFacts{}, nil
}

func TestProductDetailTemplatePageTemplateListFailureStopsDerivedQueries(t *testing.T) {
	engine, f := newDetailTemplatePageEngine(t)
	if engine == nil {
		return
	}
	productID := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉", 99, 99)
	port := &detailTemplateFailingPort{}
	f.products.SetDetailTemplatePort(port)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/products/template?project="+f.projectID+"&product="+productID, nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("模板清单读不到应 500，实际 %d，body=%s", rec.Code, headOfBody(rec.Body.String()))
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("失败出口必须是 JSON，Content-Type=%q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "内部错误") {
		t.Fatalf("报文应给归口文案，body=%s", headOfBody(body))
	}
	if strings.Contains(body, "template list unavailable") {
		t.Fatal("报文不得直出内部错误原文")
	}
	if port.candidates != 1 {
		t.Fatalf("模板清单应被读一次，实际 %d", port.candidates)
	}
	if port.derived != 0 {
		t.Fatalf("清单失败后不该再发派生取数（默认模板 / 绑定实例），实际 %d 次", port.derived)
	}
}

func TestProductDetailTemplatePageEmptyFactsStillRenderPage(t *testing.T) {
	engine, f := newDetailTemplatePageEngine(t)
	if engine == nil {
		return
	}
	productID := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉", 99, 99)
	f.products.SetDetailTemplatePort(&detailTemplateEmptyPort{})

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/products/template?project="+f.projectID+"&product="+productID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("空事实时页面应 200，实际 %d，body=%s", rec.Code, headOfBody(rec.Body.String()))
	}
	body := rec.Body.String()
	if !strings.Contains(body, "夏季衬衫") {
		t.Fatalf("页面主体（商品）仍应渲染，body=%s", headOfBody(body))
	}
	if strings.HasPrefix(strings.TrimSpace(body), "{") {
		t.Fatal("空事实仍应是页面，不是 JSON 报文")
	}

	if !strings.Contains(body, "未发布") {
		t.Fatalf("没有实例时应提示未发布，body=%s", headOfBody(body))
	}
}

type detailTemplateEmptyPort struct{}

func (p *detailTemplateEmptyPort) DetailTemplateCandidates(context.Context, string, string) ([]productcontract.DetailTemplateCandidateView, error) {
	return []productcontract.DetailTemplateCandidateView{}, nil
}

func (p *detailTemplateEmptyPort) DefaultDetailTemplate(context.Context, string, string) (string, error) {
	return "", nil
}

func (p *detailTemplateEmptyPort) BoundDetailTemplate(context.Context, string, string, string) (productcontract.BoundTemplateView, error) {
	return productcontract.BoundTemplateView{}, nil
}

func (p *detailTemplateEmptyPort) DetailTemplatePanel(context.Context, string, string, string) (productcontract.DetailTemplatePanelFacts, error) {
	return productcontract.DetailTemplatePanelFacts{}, nil
}

func headOfBody(s string) string {
	if len(s) > 600 {
		return s[:600]
	}
	return s
}
