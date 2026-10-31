package feature

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	presentationcontract "go_wp/internal/module/presentation/contract"
	producthttp "go_wp/internal/module/product/inbound/http"
)

// 仅替换构建器故障点，商品查询与工程作用域仍来自生产迁移和真实 PG。
type failedProductPreview struct {
	producthttp.ProductDetailTemplatePort
	result *presentationcontract.PreviewInstanceResp
	err    error
}

func (p failedProductPreview) PreviewInstance(context.Context, *presentationcontract.PreviewInstanceReq) (*presentationcontract.PreviewInstanceResp, error) {
	return p.result, p.err
}

func TestProductPreviewFailureIsJSONWithoutNavigation(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	productID := f.createProduct(t, "预览失败商品", "preview-json-error", "正文", 88, 99)
	for _, tc := range []struct {
		name, id string
		port     producthttp.ProductDetailTemplatePort
		status   int
		message  string
	}{
		{"缺少商品", "", f.pres, 400, "请求参数无效"},
		{"商品不存在", uuid.NewString(), f.pres, 404, "不存在"},
		{"构建器错误", productID, failedProductPreview{ProductDetailTemplatePort: f.pres, err: errors.New("private SQLSTATE preview failure")}, 500, "内部错误"},
		{"空构建结果", productID, failedProductPreview{ProductDetailTemplatePort: f.pres}, 500, "预览未生成有效内容"},
		{"依赖缺失", productID, nil, 500, "详情页模板能力尚未就绪"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := producthttp.NewProductPageHandle(f.products, f.products)
			h.SetDetailTemplateDeps(f.templates, tc.port)
			engine := gin.New()
			engine.GET("/admin/products/preview-frame", h.ProductPreviewFrame)
			engine.POST("/admin/products/template/preview", h.ProductDetailTemplatePreview)
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				var req *http.Request
				if method == http.MethodGet {
					req = httptest.NewRequest(method, "/admin/products/preview-frame?"+url.Values{"project": {f.projectID}, "productId": {tc.id}}.Encode(), nil)
				} else {
					req = httptest.NewRequest(method, "/admin/products/template/preview", strings.NewReader(url.Values{"projectId": {f.projectID}, "productId": {tc.id}}.Encode()))
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
				rec := httptest.NewRecorder()
				engine.ServeHTTP(rec, req)
				if rec.Code != tc.status || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") || !strings.Contains(rec.Body.String(), tc.message) {
					t.Fatalf("%s 状态或文案错误：%d %s", method, rec.Code, rec.Body.String())
				}
				for _, header := range []string{"Location", "HX-Redirect", "HX-Location"} {
					if rec.Header().Get(header) != "" {
						t.Fatalf("不应输出导航头%s", header)
					}
				}
				for _, forbidden := range []string{"private", "SQLSTATE", "<html", "data-jump-state"} {
					if strings.Contains(rec.Body.String(), forbidden) {
						t.Fatalf("不应返回内部细节或提示HTML：%s", rec.Body.String())
					}
				}
			}
		})
	}
}

func TestProductPreviewFrameUsesPublishedInstanceWithoutWriting(t *testing.T) {
	engine, f := newDetailTemplatePageEngine(t)
	if f == nil {
		return
	}
	productID := f.createProduct(t, "实例预览商品", "bound-preview-json", "正文", 88, 99)
	chosen := createNamedTemplate(t, f, "已选模板", "绑定实例版式")
	f.publishWithTemplate(t, productID, "/products/bound-preview-json", chosen.ID)
	beforeInstances := countRows(t, f.db, "presentation_instances", "")
	beforeArtifacts := countRows(t, f.db, "presentation_artifacts", "")
	beforeSnapshots := countRows(t, f.db, "document_snapshots", "")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/products/preview-frame?"+url.Values{"project": {f.projectID}, "productId": {productID}}.Encode(), nil))
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), "绑定实例版式") {
		t.Fatalf("应预览绑定实例的模板：%d %s", rec.Code, rec.Body.String())
	}
	if countRows(t, f.db, "presentation_instances", "") != beforeInstances || countRows(t, f.db, "presentation_artifacts", "") != beforeArtifacts || countRows(t, f.db, "document_snapshots", "") != beforeSnapshots {
		t.Fatal("预览不得写入实例、产物或快照")
	}
}

func TestProductTemplateCopyRejectsOtherProjectSource(t *testing.T) {
	engine, f, _ := newTemplateScopePageEngine(t)
	if f == nil {
		return
	}
	f.seedScopeSites(t)
	beforeA := templateScopeTemplateCount(t, f, f.projectID)
	beforeB := templateScopeTemplateCount(t, f, f.projectB)
	rec := postJSON(t, engine, "/api/product/detail-template/create", map[string]any{"projectId": f.projectID, "productId": f.productA, "copyFrom": f.tplB, "name": "跨工程复制不得创建"})
	if rec.Code != 404 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") || !strings.Contains(rec.Body.String(), "不存在") {
		t.Fatalf("跨工程来源应404：%d %s", rec.Code, rec.Body.String())
	}
	if templateScopeTemplateCount(t, f, f.projectID) != beforeA || templateScopeTemplateCount(t, f, f.projectB) != beforeB {
		t.Fatal("拒绝复制时不能在任一工程创建模板")
	}
}
