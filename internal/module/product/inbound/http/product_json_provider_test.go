package producthttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/internal/templates"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

// jsonProviderStub 记录 JSON 对象是否真的按字段到达 service。
type jsonProviderStub struct {
	productcontract.ProductService
	tag     *productdto.CreateTagReq
	cat     *productdto.CreateCategoryReq
	attr    *productdto.CreateAttributeReq
	values  *productdto.SetAttributeValuesReq
	pricing *productdto.BulkPricingJSONReq
}

func (s *jsonProviderStub) CreateTag(_ context.Context, r *productdto.CreateTagReq) (*productdto.TagResp, error) {
	s.tag = r
	return &productdto.TagResp{}, nil
}

func (s *jsonProviderStub) CreateCategory(_ context.Context, r *productdto.CreateCategoryReq) (*productdto.CategoryResp, error) {
	s.cat = r
	return &productdto.CategoryResp{}, nil
}

func (s *jsonProviderStub) CreateAttribute(_ context.Context, r *productdto.CreateAttributeReq) (*productdto.AttributeResp, error) {
	s.attr = r
	return &productdto.AttributeResp{}, nil
}

func (s *jsonProviderStub) SetAttributeValues(_ context.Context, r *productdto.SetAttributeValuesReq) (*productdto.AttributeResp, error) {
	s.values = r
	return &productdto.AttributeResp{}, nil
}

func (s *jsonProviderStub) BulkApplyPricing(_ context.Context, r *productdto.BulkPricingReq) (*productdto.BulkPricingResultResp, error) {
	s.pricing = &productdto.BulkPricingJSONReq{IDs: r.IDs, ProjectID: r.ProjectID, Note: r.Note}
	return &productdto.BulkPricingResultResp{}, nil
}

func jsonProviderEngine(svc productcontract.ProductService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.HTMLRender = templates.NewJetHTMLRender("../../../../templates", true)
	e.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	e.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	e.Use(func(c *gin.Context) { text.AttachScope(c); c.Set("user_id", int64(9)); c.Next() })
	h := NewProductHandle(svc)
	e.POST("/api/product/tag/create", pageview.HTMLFragment, h.CreateTag)
	e.POST("/api/product/category/create", pageview.HTMLFragment, h.CreateCategory)
	e.POST("/api/product/attribute/create", pageview.HTMLFragment, h.CreateAttribute)
	e.POST("/api/product/attribute/set-values", pageview.HTMLFragment, h.SetAttributeValues)
	e.POST("/api/product/bulk-pricing", pageview.HTMLFragment, h.BulkPricing)
	return e
}

func postJSON(e *gin.Engine, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// TestProductJSONObjectProvidersReachService 证明 data-write-builder 提交的 JSON 对象
// 能按字段到达 service：前端协议是 JSON，后端绑定也必须走 JSON，不能按普通表单绑定。
func TestProductJSONObjectProvidersReachService(t *testing.T) {
	svc := &jsonProviderStub{}
	e := jsonProviderEngine(svc)

	rec := postJSON(e, "/api/product/tag/create",
		`{"projectId":"p1","name":"标签A","slug":"a","kind":"manual","ruleParams":{"days":"7"},"sort":3}`)
	if rec.Code != http.StatusOK || svc.tag == nil || svc.tag.Name != "标签A" || svc.tag.ProjectID != "p1" ||
		svc.tag.Kind != "manual" || string(svc.tag.RuleParams) != `{"days":"7"}` {
		t.Fatalf("标签 JSON 未按字段到达 service：%d %s %+v", rec.Code, rec.Body.String(), svc.tag)
	}

	rec = postJSON(e, "/api/product/category/create",
		`{"projectId":"p1","name":"分类A","slug":"c","parentId":"","sort":1}`)
	if rec.Code != http.StatusOK || svc.cat == nil || svc.cat.Name != "分类A" || svc.cat.ParentID != "" {
		t.Fatalf("分类 JSON 未按字段到达 service：%d %s %+v", rec.Code, rec.Body.String(), svc.cat)
	}

	rec = postJSON(e, "/api/product/attribute/create",
		`{"projectId":"p1","name":"颜色","key":"color","isVariation":true,"sort":2,"values":[{"label":"红","key":"red","sort":1,"enabled":true}]}`)
	if rec.Code != http.StatusOK || svc.attr == nil || svc.attr.Name != "颜色" || len(svc.attr.Values) != 1 ||
		svc.attr.Values[0].Label != "红" {
		t.Fatalf("属性组 JSON 未按字段到达 service：%d %s %+v", rec.Code, rec.Body.String(), svc.attr)
	}

	rec = postJSON(e, "/api/product/attribute/set-values",
		`{"projectId":"p1","id":"a1","values":[{"id":"v1","label":"蓝","sort":0,"enabled":false}]}`)
	if rec.Code != http.StatusOK || svc.values == nil || svc.values.ID != "a1" || len(svc.values.Values) != 1 ||
		svc.values.Values[0].Label != "蓝" || svc.values.Values[0].Enabled == nil || *svc.values.Values[0].Enabled {
		t.Fatalf("属性值 JSON 未按字段到达 service：%d %s %+v", rec.Code, rec.Body.String(), svc.values)
	}

	rec = postJSON(e, "/api/product/bulk-pricing",
		`{"projectId":"p1","ids":["x","y"],"note":"n","ruleType":"fixed_price","amount":"12.5"}`)
	if rec.Code != http.StatusOK || svc.pricing == nil || len(svc.pricing.IDs) != 2 || svc.pricing.Note != "n" {
		t.Fatalf("批量定价 JSON 未按字段到达 service：%d %s %+v", rec.Code, rec.Body.String(), svc.pricing)
	}
}

// TestProductJSONProviderRejectsFormBody 证明协议不双轨：
// 这些端点只接受 JSON 对象，普通表单体不会被静默接受成半份结构。
func TestProductJSONProviderRejectsFormBody(t *testing.T) {
	svc := &jsonProviderStub{}
	e := jsonProviderEngine(svc)
	req := httptest.NewRequest(http.MethodPost, "/api/product/tag/create",
		strings.NewReader("projectId=p1&name=tagA"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if svc.tag != nil {
		t.Fatalf("表单体不该被 JSON 端点接受：%+v", svc.tag)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("协议不符应回 400，实际 %d：%s", rec.Code, rec.Body.String())
	}
}
