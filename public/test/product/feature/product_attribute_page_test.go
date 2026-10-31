// Package feature product 模块 feature 测试 —— 后台属性管理页渲染链路（issue #7）。
//
// 覆盖验收 4「后台可管理属性组与属性值」的服务端部分：
//
//	· GET /admin/product-attributes 渲染完整页（属性组、值、参与变体标记都在 HTML 里）；
//	· POST /api/product/attribute/set-values 消费浏览器整形后的 values JSON 数组。
//	  增删、禁用、排序和清空通过同一请求保存。
//
// 用真实 Jet 模板渲染（与生产同一 template root），断言的是「页面里到底有没有
// 那几样东西」，而不是「我们自己写下的配置」。
package feature

import (
	"go_wp/internal/adminui"
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

	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"

	"go_wp/public/migrations"
	"go_wp/public/test/support"
)

// newAttrPageEngine 装配一个只挂属性管理页的测试引擎（真实 Jet 模板 + 真实 service）。
func newAttrPageEngine(t *testing.T) (*gin.Engine, *attrFixture) {
	t.Helper()
	f := newAttrFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 属性组行内操作与值编辑器抽屉按权限渲染（adminui.Prepare 读 PermSetKey）：
	// 这条链路不挂鉴权中间件，注入一份权限，让「页面里存在值编辑器」这类断言保持有效。
	engine.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, map[string]bool{
			"product:attribute_create": true, "product:attribute_update": true, "product:attribute_delete": true,
		})
		c.Set(adminctx.ButtonsKey, map[string]bool{
			"product.attribute_create": true, "product.attribute_update": true, "product.attribute_delete": true,
		})

	})
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	handle := producthttp.NewProductPageHandle(f.svc, f.svc)
	engine.GET("/admin/product-attributes", pageview.UseRenderer(adminui.RenderPage), handle.ProductAttributesPage)
	engine.POST("/api/product/attribute/set-values", producthttp.NewProductHandle(f.svc).SetAttributeValues)
	return engine, f
}

// attrTemplateRoot 模板根目录（测试进程工作目录在 public/test/product/feature）。
func attrTemplateRoot() string {
	return "../../../../internal/templates"
}

// TestProductAttributesPageRendersGroupsAndValues 页面渲染：
// 属性组名、标识、参与变体标记、属性值、值编辑器容器 id 都必须在 HTML 里。
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
	// 列表页标准骨架：勾选列 + 全选 + 批量删除栏 + 数据表格（属性组表已改成表格形态）。
	for _, want := range []string{`class="data-table"`, `class="col-check"`, "data-check-all", `class="bulk-bar"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("属性组列表缺少标准表格骨架 %q", want)
		}
	}
}

// TestAttributeValuesFormWritesJSON 用浏览器写基座的 values JSON 字段保存增删与禁用。
func TestAttributeValuesFormWritesJSON(t *testing.T) {
	engine, f := newAttrPageEngine(t)
	if engine == nil {
		return
	}
	group, err := f.svc.CreateAttribute(t.Context(), &productdto.CreateAttributeReq{ProjectID: f.projectID, Name: "尺寸", Key: "size", Values: []productdto.AttributeValueReq{{Label: "S", Key: "s"}, {Label: "M", Key: "m"}}})
	if err != nil {
		t.Fatal(err)
	}
	payload := `[{"id":"` + group.Values[1].ID + `","key":"m","label":"中码","sort":"2","enabled":"false"},{"key":"l","label":"L","sort":"1","enabled":"true"}]`
	rec := postForm(engine, "/api/product/attribute/set-values", url.Values{"id": {group.ID}, "projectId": {f.projectID}, "values": {payload}})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("保存应返回 JSON 200: %d %s", rec.Code, rec.Body.String())
	}
	got, err := f.svc.GetAttribute(t.Context(), &productdto.GetAttributeReq{ID: group.ID, ProjectID: f.projectID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Values) != 2 || got.Values[0].Key != "l" || got.Values[1].ID != group.Values[1].ID || got.Values[1].Label != "中码" || got.Values[1].Enabled {
		t.Fatalf("增删/保留身份/禁用/排序错误: %+v", got.Values)
	}
	rec = postForm(engine, "/api/product/attribute/set-values", url.Values{"id": {group.ID}, "projectId": {f.projectID}, "values": {"[]"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("清空失败: %s", rec.Body.String())
	}
	got, err = f.svc.GetAttribute(t.Context(), &productdto.GetAttributeReq{ID: group.ID, ProjectID: f.projectID})
	if err != nil || len(got.Values) != 0 {
		t.Fatalf("清空未落库: %+v %v", got, err)
	}
}

// TestAttributePageCreateAndSetValues 建组 + 整体保存值（服务端归一与排序）。
//
// 页面写链路已迁到 ui/write-base.js（模板直调 /api/product/attribute/{create,set-values}），
// 这里直接验用例：values 行数组由 product-attribute-values.test.cjs 与页面渲染测试分别钉住。
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

	// 整体保存值：只提交一行 → 库里应只剩一行。
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

// postForm 发送 application/x-www-form-urlencoded 表单。
func postForm(engine *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// 编译期确保构造用的 service 类型一致（防装配签名漂移）。
var (
	_ = productservice.NewService
	_ = productmodel.NewModel
	_ = projectservice.NewService
	_ = projectmodel.NewProjectModel
	_ = migrations.Run
	_ = support.NewPGTestDB
	_ = projectdto.CreateReq{}
)
