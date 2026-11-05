package feature

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"

	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"go_wp/internal/adminui"
	contenttemplatehttp "go_wp/internal/module/contenttemplate/inbound/http"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/public/test/support"
)

const contentTemplateInternalLeakToken = "SQLSTATE"

var contentTemplateInternalLeakTokens = []string{"SQLSTATE", "uq_", "pg_", "relation \"", "constraint", "does not exist"}

func assertNoContentTemplateInternalLeak(t *testing.T, where, text string) {
	t.Helper()
	for _, tok := range contentTemplateInternalLeakTokens {
		if strings.Contains(text, tok) {
			t.Errorf("%s 泄漏内部细节 %q：%s", where, tok, text)
		}
	}
}

type contentTemplateEnv struct {
	engine    *gin.Engine
	db        *gorm.DB
	projectID string
	products  *productservice.Service
}

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
	products := productservice.NewService(productmodel.NewModel(db), projects, masterdataservice.NewService(masterdatamodel.NewModel(db), projects))
	templatesSvc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, nil)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	handle := contenttemplatehttp.NewContentTemplatePageHandle(templatesSvc, projects, products, nil)
	engine.GET("/admin/content-templates",
		adminui.PageSlot(contenttemplatehttp.ContentTemplatesPageTemplate),
		handle.ContentTemplatesPage)
	engine.POST("/api/contenttemplate/delete", contenttemplatehttp.NewHandle(templatesSvc).Delete)
	return &contentTemplateEnv{engine: engine, db: db, projectID: project.ID, products: products}
}

func TestContentTemplatesHidesSampleErrInternalError(t *testing.T) {
	env := newContentTemplateErrorLeakEnv(t)
	if env == nil {
		return
	}

	tm := contenttemplatemodel.NewModel(env.db)
	if err := tm.Create(context.Background(), &contenttemplatemodel.TemplateEntity{
		ID: "66666666-6666-6666-6666-666666666666", ProjectID: env.projectID,
		Name: "商品详情模板", EntityType: "product", TemplateRole: "detail",
		DraftDocument: json.RawMessage("{}"), DraftVersion: 1,
	}); err != nil {
		t.Fatalf("准备模板行失败：%v", err)
	}

	if err := env.db.Exec("ALTER TABLE products RENAME TO products_hidden").Error; err != nil {
		t.Fatalf("改名 products 表失败：%v", err)
	}

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

	if !strings.Contains(body, "</html>") {
		t.Fatalf("页面未渲染完整（缺 </html>）：%s", body)
	}
}

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

func TestContentTemplatesBulkDeleteKeepsControlledLimitText(t *testing.T) {
	env := newContentTemplateErrorLeakEnv(t)
	if env == nil {
		return
	}
	ids := []string{}

	for i := 0; i < bulkids.MaxIDs+1; i++ {
		ids = append(ids, fmt.Sprintf("00000000-0000-0000-0000-%012d", i))
	}
	payload, err := json.Marshal(map[string]any{"ids": ids})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/contenttemplate/delete", strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("超限应返回 JSON 400，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	assertNoContentTemplateInternalLeak(t, "批量超限提示页", body)
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatal("expected JSON")
	}
	if !strings.Contains(body, "一次最多操作") {
		t.Fatalf("受控提示应保持可见，body=%s", body)
	}
}
