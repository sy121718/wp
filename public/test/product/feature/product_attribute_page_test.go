package feature

import (
	pageview "go_wp/pkg/view"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/adminctx"
	producthttp "go_wp/internal/module/product/inbound/http"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	text "go_wp/pkg/view/jet"

	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"

	"go_wp/public/schema"
	"go_wp/public/test/support"
)

func newAttrPageEngine(t *testing.T) (*gin.Engine, *attrFixture) {
	t.Helper()
	f := newAttrFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{
			"product:attribute_create": true, "product:attribute_update": true, "product:attribute_delete": true,
		})
		c.Set(adminctx.ButtonsKey, map[string]bool{
			"product.attribute_create": true, "product.attribute_update": true, "product.attribute_delete": true,
		})

	})
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	engine.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	// 写成功走共享成功出口（生产 assembly 同款接线）。
	engine.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	engine.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	handle := producthttp.NewProductPageHandle(f.svc, f.svc)
	engine.GET("/admin/product-attributes", pageview.UseRenderer(pageview.RenderPage), handle.ProductAttributesPage)
	engine.POST("/api/product/attribute/set-values", pageview.HTMLFragment, producthttp.NewProductHandle(f.svc).SetAttributeValues)
	return engine, f
}

func attrTemplateRoot() string {
	return "../../../../internal/templates"
}

func TestProductAttributesPageRendersGroupsAndValues(t *testing.T) {
	engine, f := newAttrPageEngine(t)
	if engine == nil {
		return
	}
	group, err := f.svc.CreateAttribute(t.Context(), &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "颜色", Key: "color", IsVariation: boolPtr(false),
		Values: []productdto.AttributeValueReq{{Label: "红色", Key: "red"}},
	})
	if err != nil {
		t.Fatalf("创建属性组失败: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/product-attributes?project="+f.projectID, nil)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("页面应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"商品属性", "颜色", "color", "不参与变体", "红色",
		"attr-values-" + group.ID, "保存属性值", "+ 添加值",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q", want)
		}
	}

	for _, want := range []string{`class="data-table"`, `class="col-check"`, "data-check-all", `class="bulk-bar"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("属性组列表缺少标准表格骨架 %q", want)
		}
	}
}

func TestAttributeValuesFormWritesHTML(t *testing.T) {
	engine, f := newAttrPageEngine(t)
	if engine == nil {
		return
	}
	group, err := f.svc.CreateAttribute(t.Context(), &productdto.CreateAttributeReq{ProjectID: f.projectID, Name: "尺寸", Key: "size", Values: []productdto.AttributeValueReq{{Label: "S", Key: "s"}, {Label: "M", Key: "m"}}})
	if err != nil {
		t.Fatal(err)
	}
	// 属性值是结构化集合，端点声明的是 JSON 对象：按真实协议提交，不用表单拼数组。
	body := map[string]any{
		"id": group.ID, "projectId": f.projectID,
		"values": []map[string]any{
			{"id": group.Values[1].ID, "key": "m", "label": "中码", "sort": 2, "enabled": false},
			{"key": "l", "label": "L", "sort": 1, "enabled": true},
		},
	}
	rec := postJSON(t, engine, "/api/product/attribute/set-values", body)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), `role="status"`) {
		t.Fatalf("保存应返回 HTML 200: %d %s", rec.Code, rec.Body.String())
	}
	got, err := f.svc.GetAttribute(t.Context(), &productdto.GetAttributeReq{ID: group.ID, ProjectID: f.projectID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Values) != 2 || got.Values[0].Key != "l" || got.Values[1].ID != group.Values[1].ID || got.Values[1].Label != "中码" || got.Values[1].Enabled {
		t.Fatalf("增删/保留身份/禁用/排序错误: %+v", got.Values)
	}
	rec = postJSON(t, engine, "/api/product/attribute/set-values", map[string]any{
		"id": group.ID, "projectId": f.projectID, "values": []map[string]any{},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("清空失败: %s", rec.Body.String())
	}
	got, err = f.svc.GetAttribute(t.Context(), &productdto.GetAttributeReq{ID: group.ID, ProjectID: f.projectID})
	if err != nil || len(got.Values) != 0 {
		t.Fatalf("清空未落库: %+v %v", got, err)
	}
}

func TestAttributePageCreateAndSetValues(t *testing.T) {
	engine, f := newAttrPageEngine(t)
	if engine == nil {
		return
	}
	group, err := f.svc.CreateAttribute(t.Context(), &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "口味", Key: "flavor", IsVariation: boolPtr(true),
		Values: []productdto.AttributeValueReq{{Label: "原味"}, {Label: "香辣"}},
	})
	if err != nil {
		t.Fatalf("建组失败: %v", err)
	}
	list, err := f.svc.ListAttributes(t.Context(), &productdto.ListAttributeReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(list) != 1 || list[0].Key != "flavor" || len(list[0].Values) != 2 {
		t.Fatalf("建组结果不符：%+v", list)
	}

	if _, err = f.svc.SetAttributeValues(t.Context(), &productdto.SetAttributeValuesReq{
		ID: group.ID, Values: []productdto.AttributeValueReq{{Label: "原味"}},
	}); err != nil {
		t.Fatalf("保存属性值失败: %v", err)
	}
	got, err := f.svc.GetAttribute(t.Context(), &productdto.GetAttributeReq{ID: group.ID})
	if err != nil {
		t.Fatalf("读属性组失败: %v", err)
	}
	if got.ValueCount != 1 {
		t.Fatalf("全量替换后应只剩 1 个值，实际 %d", got.ValueCount)
	}
}

func postForm(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

var (
	_ = productservice.NewService
	_ = productmodel.NewModel
	_ = projectservice.NewService
	_ = projectmodel.NewProjectModel
	_ = schema.Apply
	_ = support.NewPGTestDB
	_ = projectdto.CreateReq{}
)
