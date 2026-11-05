package producthttp

import (
	"context"
	"errors"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go_wp/internal/adminui"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
	text "go_wp/pkg/view/jet"
)

type translationFactsReader struct {
	productcontract.ProductService
	facts *productcontract.ProductTranslations
	err   error
}

func (s *translationFactsReader) ResolveCatalogScope(context.Context, string) (*productcontract.CatalogScope, error) {
	return &productcontract.CatalogScope{ProjectID: "p1", Projects: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}, {ID: "p2", Name: "工程二"}}}, nil
}
func (s *translationFactsReader) QueryProductTranslations(context.Context, *productdto.ProductTranslationsPageReq) (*productcontract.ProductTranslations, error) {
	return s.facts, s.err
}
func (s *translationFactsReader) SaveProductTranslations(context.Context, *productdto.SaveProductTranslationsReq) (*productcontract.TranslationSaveResult, error) {
	panic("此测试不执行写入")
}

func translationObjectsRequest(t *testing.T, facts *productcontract.ProductTranslations, readErr error) *httptest.ResponseRecorder {
	t.Helper()
	h := &ProductTranslationHandle{products: &translationFactsReader{facts: facts, err: readErr}}
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.GET("/admin/products/translations", pageview.UseRenderer(adminui.RenderPage), h.ProductTranslations)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/products/translations?project=p1", nil))
	return rec
}
func TestProductTranslationsRendersBusinessObjects(t *testing.T) {
	facts := &productcontract.ProductTranslations{ProjectID: "p1", ProductID: "prod-1", Lang: "en-US", Langs: []string{"en-US", "ja-JP"}, SourceLang: "zh-CN", Done: 1, Total: 2, Groups: []productcontract.TranslationEntity{{EntityType: "product", EntityID: "prod-1", EntityName: "亚麻衬衫", Rows: []productcontract.TranslationValue{{Context: "product.name", Field: "name", Source: "亚麻衬衫", SourceHash: "h1", Target: "Linen shirt", Engine: "manual"}, {Context: "product.description", Field: "description", Source: "舒适透气", SourceHash: "h2"}}}}}
	rec := translationObjectsRequest(t, facts, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{"value=\"p1\" selected", "value=\"en-US\" selected", "Linen shirt", "name=\"rowTarget\" rows=\"4\"", "name=\"rowHash\" value=\"h2\"", "/api/product/translations/save", "保存全部"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("真实 HTML 缺少 %q：%s", want, rec.Body.String())
		}
	}
}
func TestProductTranslationsNoTargetLanguageRendersAction(t *testing.T) {
	rec := translationObjectsRequest(t, &productcontract.ProductTranslations{ProjectID: "p1", SourceLang: "zh-CN", Langs: []string{}, Groups: []productcontract.TranslationEntity{}}, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "去站点设置启用更多语言") || strings.Contains(rec.Body.String(), "name=\"rowTarget\"") {
		t.Fatalf("无目标语言应只有设置入口：%s", rec.Body.String())
	}
}
func TestProductTranslationsReadFailureIsJSON(t *testing.T) {
	rec := translationObjectsRequest(t, nil, errors.New("private database detail"))
	assertProductJSONFailure(t, "译文读取失败", rec, http.StatusInternalServerError, "内部错误")
}

type templateCatalogReader struct {
	productcontract.ProductService
	productcontract.ProductPageReader
	defaultErr error
	boundErr   error
}

func (*templateCatalogReader) ResolveCatalogScope(context.Context, string) (*productcontract.CatalogScope, error) {
	return &productcontract.CatalogScope{ProjectID: "p1", Projects: []projectdto.ProjectResp{{ID: "p1", Name: "工程一"}}}, nil
}
func (*templateCatalogReader) Get(context.Context, *productdto.GetReq) (*productdto.ProductResp, error) {
	return &productdto.ProductResp{ID: "prod-1", Name: "亚麻衬衫", Slug: "linen"}, nil
}
func (*templateCatalogReader) QueryDetailTemplates(context.Context, string) ([]productcontract.DetailTemplateCandidateView, error) {
	return []productcontract.DetailTemplateCandidateView{{ID: "tpl-1", Name: "夏季详情", DraftVersion: 3, UpdatedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}, {ID: "tpl-2", Name: "普通详情", DraftVersion: 1}}, nil
}
func (s *templateCatalogReader) QueryDefaultDetailTemplate(context.Context, string) (string, error) {
	return "tpl-1", s.defaultErr
}
func (s *templateCatalogReader) QueryBoundDetailTemplate(context.Context, string, string) (productcontract.BoundTemplateView, error) {
	return productcontract.BoundTemplateView{Exists: true, TemplateID: "tpl-2", Status: "active", URLPath: "/products/linen"}, s.boundErr
}
func (*templateCatalogReader) ProductDetailPath(context.Context, string, *productdto.ProductResp) string {
	return "/products/linen"
}

type templateCatalogPort struct {
	contenttemplatecontract.ContentTemplateService
}
type templateInstancePort struct{ ProductDetailTemplatePort }

func TestDetailTemplateRendersBindingFromOriginalObjects(t *testing.T) {
	reader := &templateCatalogReader{}
	h := &ProductPageHandle{products: reader, reader: reader, templates: &templateCatalogPort{}, instances: &templateInstancePort{}}
	engine := gin.New()
	engine.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	engine.GET("/admin/products/template", pageview.UseRenderer(adminui.RenderPage), h.ProductDetailTemplatePage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/products/template?project=p1&product=prod-1", nil))
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("状态=%d body=%s", rec.Code, body)
	}
	for _, want := range []string{"夏季详情", "普通详情", "value=\"tpl-2\" selected", "value=\"/products/linen\"", "当前使用", "类型默认", "2026-10-20", "命名模板（2"} {
		if !strings.Contains(body, want) {
			t.Fatalf("真实 HTML 缺少 %q：%s", want, body)
		}
	}
}

func TestDetailTemplateReadFailuresAreJSON(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reader *templateCatalogReader
	}{{"默认模板", &templateCatalogReader{defaultErr: errors.New("private template query")}}, {"绑定实例", &templateCatalogReader{boundErr: errors.New("private instance query")}}} {
		t.Run(tc.name, func(t *testing.T) {
			h := &ProductPageHandle{products: tc.reader, reader: tc.reader}
			engine := gin.New()
			engine.GET("/admin/products/template", h.ProductDetailTemplatePage)
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/products/template?project=p1&product=prod-1", nil))
			assertProductJSONFailure(t, tc.name, rec, http.StatusInternalServerError, "内部错误")
			if strings.Contains(rec.Body.String(), "private") {
				t.Fatalf("响应不得包含内部查询细节：%s", rec.Body.String())
			}
		})
	}
}

func TestDetailTemplateMissingProductIsJSON400(t *testing.T) {
	h := &ProductPageHandle{}
	engine := gin.New()
	engine.GET("/admin/products/template", h.ProductDetailTemplatePage)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/products/template?project=p1", nil))
	if rec.Code != http.StatusBadRequest || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") || rec.Header().Get("Location") != "" {
		t.Fatalf("缺少商品应返回 JSON 400：status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDetailTemplateWriteKeepsDeclaredErrorStatus(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{{"查询故障", errors.New("private query"), http.StatusInternalServerError}, {"商品不存在", productenums.Err(productenums.ErrNotFound), http.StatusNotFound}, {"冲突", productenums.Err(productenums.ErrSkuTaken), http.StatusConflict}} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/product/detail-template/publish", nil)
			detailTemplateFinish(c, tc.err)
			if rec.Code != tc.status || strings.Contains(rec.Body.String(), "private query") {
				t.Fatalf("错误状态应沿声明传递：status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
