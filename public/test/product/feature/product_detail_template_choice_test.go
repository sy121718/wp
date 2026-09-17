// Package feature product 模块 feature 测试 —— 详情页模板可选与预览（issue #14）。
//
// 逐条覆盖验收（真实 PostgreSQL + 生产迁移 + 真实 service 装配，与既有
// product_detail_publish_test.go 共用 detailFixture 装配）：
//  1. 同一商品类型可建多套命名模板并各自版本化；
//  2. 发布时能指定使用某套模板；
//  3. 发布前可预览模板渲染效果（只读：不落库、不落盘、不激活）；
//  4. 切换模板重新发布后产物随之变化（且内容更新触发的重建不会把模板换回默认）；
//  5. 不影响既有「按类型取默认模板」的行为；
//  6. 后台页链路：GET 详情模板页 → 新建命名模板 → 预览 → 发布 → 切换重新发布。
package feature

import (
	"context"
	"encoding/json"
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

	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	producthttp "go_wp/internal/module/product/inbound/http"
	"go_wp/internal/pipeline"
	"go_wp/internal/templates"
	"go_wp/internal/web/shell"

	"github.com/google/uuid"
)

// detailDocWithMarker 一套合法商品详情文档：一个版式标记标题 + 商品详情组件。
//
// 标记（marker）是「这套模板渲染出来长什么样」的可断言特征 —— 切换模板时产物里
// 应当出现新标记、旧标记消失。
func detailDocWithMarker(marker string) string {
	return "{\"settings\":{\"layout\":{\"mode\":\"full\"}},\"root\":[" +
		"{\"id\":\"mark\",\"type\":\"core.heading\",\"props\":{\"text\":\"" + marker + "\",\"tag\":\"h2\"}}," +
		"{\"id\":\"pd\",\"type\":\"core.product\",\"props\":{\"titleField\":\"product.name\",\"priceField\":\"product.priceRange\",\"descriptionField\":\"product.description\"}}]}"
}

// createNamedTemplate 建一套命名模板（同一实体类型下的多套模板之一）。
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

// publishWithTemplate 为商品建立发布实例，并显式指定使用的模板。
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

// canonicalOf 取产物里的 canonical href（不存在返回空串）。
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

// urlSEOFragments 只在有 URL 时输出的 SEO 片段前缀：canonical、og:url，
// 以及由 URL 生成面包屑的结构化数据块（JSON-LD 单行输出）。
//
// 预览不激活 URL（urlPath 传空），发布产物带实例线上路径 —— 这是预览与发布在字节上
// 唯一的差异来源（见 internal/module/presentation/service/presentation_seo.go 取舍 2）。
var urlSEOFragments = []string{
	"<link rel=\"canonical\"",
	"<meta property=\"og:url\"",
	"<script type=\"application/ld+json\">",
}

// stripURLTags 去掉产物里 URL 相关的 SEO 片段（比较「预览与发布除 URL 外逐字节一致」用）。
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

// countRows 统计某表的行数（where 可空）。
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

// activeFileExists 访问面上某路径的激活产物是否存在。
func activeFileExists(urlPath string) bool {
	_, err := os.Stat(filepath.Join(pipeline.ActiveRoot(), strings.TrimPrefix(urlPath, "/"), "index.html"))
	return err == nil
}

// instanceTemplateID 直读实例绑定的模板 ID。
func instanceTemplateID(t *testing.T, f *detailFixture, instanceID string) string {
	t.Helper()
	var id string
	if err := f.db.Raw("SELECT template_id FROM presentation_instances WHERE id = ?", instanceID).Scan(&id).Error; err != nil {
		t.Fatalf("读取实例模板绑定失败: %v", err)
	}
	return id
}

// TestProductDetailMultiNamedTemplatesVersioned 验收 1：同一商品类型可建多套命名模板，
// 每套各自版本化 —— 改一套只推进它自己的版本号。
func TestProductDetailMultiNamedTemplatesVersioned(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	// 迁移 085 已种下类型默认模板（「商品详情页」），本票再建两套命名模板。
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

	// 各自版本化：改一套 → 只有它递增到 v2，另一套仍 v1。
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

	// 按 ID 解析取各自当前版本（不是「同类型最新」）。
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
	// 不存在的模板 ID 明确报「不存在」，不静默回落到类型默认模板。
	if _, err = f.templates.ResolveTemplateByID(ctx, uuid.NewString()); err == nil {
		t.Fatalf("不存在的模板 ID 应报错")
	} else if !strings.Contains(err.Error(), contenttemplateenums.ErrNotFound) {
		t.Fatalf("错误应为未找到: %v", err)
	}
}

// TestProductPublishWithChosenTemplate 验收 2：发布时能指定使用某套模板。
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

// TestProductPreviewBeforePublish 验收 3：发布前可预览模板渲染效果，
// 且预览全程只读 —— 不写实例/产物/指针、不激活 URL。
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
	// 只读：库表与访问面都不因预览而改变。
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

	// 预览字节与随后发布的产物字节一致（预览看到的就是发布出来的）；唯一例外是
	// URL 相关的 SEO 片段 —— 预览不激活 URL，只有发布产物带实例线上路径
	// （见 internal/module/presentation/service/presentation_seo.go 的取舍 2）。
	f.publishWithTemplate(t, productID, "/products/summer-shirt", chosen.ID)
	published := activeHTML(t, "/products/summer-shirt")
	if stripURLTags(published) != stripURLTags(res.HTML) {
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

	// 不指定模板 → 预览走类型默认模板（既有行为）。
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

// TestProductPreviewRejectsForeignTemplateType 预览/发布都不接受跨实体类型的模板：
// 拿文章模板渲染商品会在构建期产出错误数据，必须在入口拒绝。
func TestProductPreviewRejectsForeignTemplateType(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	// 直写一张 article 模板（绕过注册表校验，模拟存量/绕过接口写入的数据）。
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

// TestProductSwitchTemplateChangesArtifact 验收 4：切换模板重新发布后产物随之变化；
// 且随后「内容更新触发的重建」沿用实例绑定的模板，不会退回类型默认模板。
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

	// 切换到第二套并重新发布。
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

	// 随后不带 templateId 的重建（内容更新触发的自动重建走同一路径）必须沿用绑定模板。
	if _, err = f.pres.Rebuild(ctx, &presentationdto.RebuildReq{EntityID: productID}); err != nil {
		t.Fatalf("内容更新重建失败: %v", err)
	}
	html = activeHTML(t, "/products/summer-shirt")
	if !strings.Contains(html, "极简版式") || strings.Contains(html, "活动版式") {
		t.Fatalf("未显式切换模板的重建应沿用实例绑定模板: %s", html)
	}
	// 依赖失效触发的自动重建（RebuildStale）同样沿用绑定模板。
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

// TestProductPublishFallsBackToTypeDefault 验收 5：不影响既有「按类型取默认模板」的行为。
//
// 不指定模板创建实例时仍按实体类型解析默认模板；「类型默认」的口径也未改变
// （该类型最近更新的一套），新建命名模板不会让既有调用方拿到空模板。
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

	// 既有调用形态：CreateInstance 不带 templateId。
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
	// 默认解析口径未变：仍取该类型最近更新的一套。
	if now, rerr := f.templates.ResolveTemplate(ctx, "product"); rerr != nil || now.TemplateID != seeded.TemplateID {
		t.Fatalf("未新增模板时默认解析应保持同一套: %v / %+v", rerr, now)
	}
}

// newDetailTemplatePageEngine 装配一个只挂详情模板页的测试引擎（真实 Jet 模板 + 真实 service）。
func newDetailTemplatePageEngine(t *testing.T) (*gin.Engine, *detailFixture) {
	t.Helper()
	f := newDetailFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	// 页面的「新建命名模板」入口按权限显隐（shell.Prepare 读 PermSetKey），而这条链路
	// 不走鉴权中间件：注入一份全权限，让用例聚焦页面本身的行为。
	engine.Use(func(c *gin.Context) {
		c.Set(shell.PermSetKey, map[string]bool{"contenttemplate:create": true})
	})
	handle := producthttp.NewProductPageHandle(f.products, f.projects)
	handle.SetDetailTemplateDeps(f.templates, f.pres)
	engine.GET("/admin/products/template", handle.ProductDetailTemplatePage)
	engine.POST("/admin/products/template/create", handle.ProductDetailTemplateCreate)
	engine.POST("/admin/products/template/publish", handle.ProductDetailTemplatePublish)
	engine.POST("/admin/products/template/apply", handle.ProductDetailTemplateApply)
	engine.POST("/admin/products/template/preview", handle.ProductDetailTemplatePreview)
	return engine, f
}

// TestProductDetailTemplatePageFlow 验收 6：后台页链路 ——
// 页面列出多套命名模板并可新建、可预览（新标签页渲染结果）、可发布、可切换重新发布。
func TestProductDetailTemplatePageFlow(t *testing.T) {
	engine, f := newDetailTemplatePageEngine(t)
	if engine == nil {
		return
	}
	ctx := context.Background()
	first := createNamedTemplate(t, f, "活动版式详情页", "活动版式")
	second := createNamedTemplate(t, f, "极简详情页", "极简版式")
	productID := f.createProduct(t, "夏季衬衫", "summer-shirt", "纯棉", 99, 99)

	// 1) 页面渲染：多套命名模板（含版本号）与「新建命名模板」表单都在。
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/products/template?project="+f.projectID+"&product="+productID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("详情模板页应 200，实际 %d", rec.Code)
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

	// 2) 页面预览：返回渲染好的整页 HTML（新标签页里看到的就是产物字节），且不落库。
	rec = postForm(engine, "/admin/products/template/preview", url.Values{
		"projectId": {f.projectID}, "productId": {productID}, "templateId": {second.ID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("页面预览应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("预览应返回 HTML，实际 Content-Type=%q", ct)
	}
	if got := rec.Header().Get("X-Preview-Template"); !strings.Contains(got, "极简详情页") {
		t.Fatalf("预览响应头应标明实际使用的模板，实际 %q", got)
	}
	if !strings.Contains(rec.Body.String(), "极简版式") {
		t.Fatalf("页面预览应渲染选中模板: %s", rec.Body.String())
	}
	if n := countRows(t, f.db, "presentation_instances", "entity_id = ?", productID); n != 0 {
		t.Fatalf("页面预览不应创建实例，实际 %d 条", n)
	}

	// 3) 页面新建命名模板：类型下多出一套，页面立刻能选到它。
	rec = postForm(engine, "/admin/products/template/create", url.Values{
		"projectId": {f.projectID}, "productId": {productID},
		"name": {"节庆详情页"}, "copyFrom": {first.ID},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("新建模板应 302 回页面，实际 %d：%s", rec.Code, rec.Body.String())
	}
	list, err := f.templates.List(ctx, &contenttemplatedto.ListReq{EntityType: "product"})
	if err != nil {
		t.Fatalf("列模板失败: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("新建后应有 4 套命名模板，实际 %d", len(list))
	}

	// 4) 页面发布：不带 urlPath 时按商品 slug 派生，产物进访问面。
	rec = postForm(engine, "/admin/products/template/publish", url.Values{
		"projectId": {f.projectID}, "productId": {productID}, "templateId": {first.ID},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("页面发布应 302 回页面，实际 %d：%s", rec.Code, rec.Body.String())
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
	// 页面改为展示「切换模板并重新发布」（实例已存在）。
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/products/template?project="+f.projectID+"&product="+productID, nil))
	if body = rec.Body.String(); !strings.Contains(body, "切换模板并重新发布") || strings.Contains(body, "发布详情页") {
		t.Fatalf("实例已存在时应展示切换入口")
	}

	// 5) 页面切换模板并重新发布：产物随之变化。
	rec = postForm(engine, "/admin/products/template/apply", url.Values{
		"projectId": {f.projectID}, "productId": {productID}, "templateId": {second.ID},
	})
	if rec.Code != http.StatusFound {
		t.Fatalf("切换模板应 302 回页面，实际 %d：%s", rec.Code, rec.Body.String())
	}
	html := activeHTML(t, "/products/summer-shirt")
	if !strings.Contains(html, "极简版式") || strings.Contains(html, "活动版式") {
		t.Fatalf("切换后产物应换成第二套模板: %s", html)
	}
}
