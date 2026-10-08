package feature

// content_template_error_leak_test.go — 内容模板页不直出内部错误（第三波 CQ-009 形态 ②③）。
//
// 这一页的错误有两条出口，都被原样渲染：
//   - 形态③：模板数据的 SampleErr（admin/content_templates.html 的 {{t.SampleErr}}）——
//     它来自 sampleEntityID 去读**别的模块**的表（products / contents），那些错误
//     带着表名与 SQLSTATE；
//   - 形态②：批量删除的受控上限提示（shell.BulkIDs）—— 改由提示页在响应体里渲染，
//     本文件断言它保持可见且不夹带驱动原文。
//
// 本文件把 products 表改名制造真实故障（relation "products" does not exist，SQLSTATE 42P01），
// 先反证 service 层原始错误确实带这些指纹，再断言页面里没有它们、只有归口文案，
// 并且整页渲染完整（错误分支缺键会让 200 响应只剩半页 —— 见 internal/templates/CLAUDE.md）。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	contenttemplatehttp "go_wp/internal/module/contenttemplate/inbound/http"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/internal/shell"
	"go_wp/public/test/support"
)

const contentTemplateInternalLeakToken = "SQLSTATE"

// contentTemplateInternalLeakTokens 内部细节指纹：出现任一即视为泄漏。
var contentTemplateInternalLeakTokens = []string{"SQLSTATE", "uq_", "pg_", "relation \"", "constraint", "does not exist"}

func assertNoContentTemplateInternalLeak(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range contentTemplateInternalLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, text)
		}
	}
}

// contentTemplateEnv 内容模板页的测试环境（真实 PG + 真实三个 service）。
type contentTemplateEnv struct {
	engine    *gin.Engine
	db        *gorm.DB
	projectID string
	products  *productservice.Service
}

// newContentTemplateErrorLeakEnv 装配 GET 列表页 + POST 批量删除两个端点。
//
// registry 传 nil：本用例只走 List（不做实体类型合法性校验），
// 校验路径由 contenttemplate 模块自己的单测覆盖。
func newContentTemplateErrorLeakEnv(t *testing.T) *contentTemplateEnv {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	project, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "内容模板泄漏测试工程"})
	if err != nil {
		t.Fatalf("创建测试工程失败：%v", err)
	}
	products := productservice.NewService(productmodel.NewModel(db), projects)
	templatesSvc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, nil)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	handle := contenttemplatehttp.NewContentTemplatePageHandle(templatesSvc, projects, products, nil)
	engine.GET("/admin/content-templates", handle.ContentTemplatesPage)
	engine.POST("/admin/content-templates/bulk-delete", handle.ContentTemplatesBulkDelete)
	return &contentTemplateEnv{engine: engine, db: db, projectID: project.ID, products: products}
}

// TestContentTemplatesHidesSampleErrInternalError 形态③：SampleErr 只能出归口文案。
func TestContentTemplatesHidesSampleErrInternalError(t *testing.T) {
	env := newContentTemplateErrorLeakEnv(t)
	if env == nil {
		return
	}
	// 补一条真实父行（content_templates 的 project_id 是 NOT NULL 外键）：
	// 行本身不是伪造结构，走的是 model 的 Create。
	tm := contenttemplatemodel.NewModel(env.db)
	if err := tm.Create(context.Background(), &contenttemplatemodel.TemplateEntity{
		ID: "66666666-6666-6666-6666-666666666666", ProjectID: env.projectID,
		Name: "商品详情模板", EntityType: "product", TemplateRole: "detail",
		DraftDocument: json.RawMessage("{}"), DraftVersion: 1,
	}); err != nil {
		t.Fatalf("准备模板行失败：%v", err)
	}

	// 把样例实体要读的 products 表改名 → sampleEntityID 拿到真实基础设施错误。
	if err := env.db.Exec("ALTER TABLE products RENAME TO products_hidden").Error; err != nil {
		t.Fatalf("改名 products 表失败：%v", err)
	}
	// 反证：原始错误确实带表名与 SQLSTATE。
	_, rawErr := env.products.List(context.Background(), &productdto.ListReq{ProjectID: env.projectID, Page: 1, Size: 1})
	if rawErr == nil {
		t.Fatalf("products 表不存在时列表应失败")
	}
	if !strings.Contains(rawErr.Error(), "products") || !strings.Contains(rawErr.Error(), contentTemplateInternalLeakToken) {
		t.Fatalf("反证失败：原始错误不含表名 / SQLSTATE：%v", rawErr)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/content-templates?project="+env.projectID, nil)
	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("列表页应 200，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	assertNoContentTemplateInternalLeak(t, "内容模板列表页 HTML", body)
	if !strings.Contains(body, "系统内部错误") {
		t.Fatalf("SampleErr 应是归口文案，body=%s", body)
	}
	// 错误分支也要把整页渲染完（缺键会 200 + 半页）。
	if !strings.Contains(body, "</html>") {
		t.Fatalf("页面未渲染完整（缺 </html>）：%s", body)
	}
}

// TestContentTemplatesKeepsActionableSampleHint 可行动提示原样可见：
// 「工程内还没有商品」是运营照着做的下一步，收口不能把它也吞成通用文案。
func TestContentTemplatesKeepsActionableSampleHint(t *testing.T) {
	env := newContentTemplateErrorLeakEnv(t)
	if env == nil {
		return
	}
	tm := contenttemplatemodel.NewModel(env.db)
	if err := tm.Create(context.Background(), &contenttemplatemodel.TemplateEntity{
		ID: "99999999-9999-9999-9999-999999999999", ProjectID: env.projectID,
		Name: "商品详情模板", EntityType: "product", TemplateRole: "detail",
		DraftDocument: json.RawMessage("{}"), DraftVersion: 1,
	}); err != nil {
		t.Fatalf("准备模板行失败：%v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/content-templates?project="+env.projectID, nil)
	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("列表页应 200，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	assertNoContentTemplateInternalLeak(t, "内容模板列表页 HTML", body)
	if !strings.Contains(body, "工程内还没有商品") {
		t.Fatalf("可行动提示应原样可见，body=%s", body)
	}
	if strings.Contains(body, "系统内部错误") {
		t.Fatalf("可行动提示被吞成归口文案，body=%s", body)
	}
}

// TestContentTemplatesBulkDeleteKeepsControlledLimitText 形态②：受控上限提示保持可见。
func TestContentTemplatesBulkDeleteKeepsControlledLimitText(t *testing.T) {
	env := newContentTemplateErrorLeakEnv(t)
	if env == nil {
		return
	}
	form := url.Values{}
	// 用高于上限一条即触发整批拒绝（不依赖 @10 这类经验数字）。
	for i := 0; i < shell.MaxBulkIDs+1; i++ {
		form.Add("ids", fmt.Sprintf("00000000-0000-0000-0000-%012d", i))
	}
	req := httptest.NewRequest(http.MethodPost,
		"/admin/content-templates/bulk-delete?project="+env.projectID,
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("超限应渲染提示页（200），实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	assertNoContentTemplateInternalLeak(t, "批量超限提示页", body)
	if !strings.Contains(body, `data-jump-state="err"`) {
		t.Fatalf("超限应渲染失败态提示页，body=%s", body)
	}
	if !strings.Contains(body, "一次最多操作") {
		t.Fatalf("受控提示应保持可见，body=%s", body)
	}
}
