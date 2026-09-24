// Package feature product 模块 feature 测试 —— 后台属性管理页渲染链路（issue #7）。
//
// 覆盖验收 4「后台可管理属性组与属性值」的服务端部分：
//
//	· GET /admin/product-attributes 渲染完整页（属性组、值、参与变体标记都在 HTML 里）；
//	· POST /admin/product-attributes/value-rows 的值编辑器片段（HTMX）按 action
//	  增删一行并回渲染有序行数据 —— 这是「编辑中的值」唯一的归一路径。
//
// 用真实 Jet 模板渲染（与生产同一 template root），断言的是「页面里到底有没有
// 那几样东西」，而不是「我们自己写下的配置」。
package feature

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	producthttp "go_wp/internal/module/product/inbound/http"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/internal/templates"
	"go_wp/internal/web/shell"

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
	// 属性组行内操作与值编辑器抽屉按权限渲染（shell.Prepare 读 PermSetKey）：
	// 这条链路不挂鉴权中间件，注入一份权限，让「页面里存在值编辑器」这类断言保持有效。
	engine.Use(func(c *gin.Context) {
		c.Set(shell.PermSetKey, map[string]bool{
			"product:attribute_create": true, "product:attribute_update": true, "product:attribute_delete": true,
		})
	})
	engine.HTMLRender = templates.NewJetHTMLRender(attrTemplateRoot(), true)
	handle := producthttp.NewProductPageHandle(f.svc, f.projects)
	engine.GET("/admin/product-attributes", handle.ProductAttributesPage)
	engine.POST("/admin/product-attributes/value-rows", handle.ProductAttributesValueRows)
	engine.POST("/admin/product-attributes/create", handle.ProductAttributesCreate)
	engine.POST("/admin/product-attributes/set-values", handle.ProductAttributesSetValues)
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

// TestAttributeValueRowsFragmentAddRemove 值编辑器片段：
// add 增一行、remove 减一行，且返回的是有序的 values[n] 表单字段。
func TestAttributeValueRowsFragmentAddRemove(t *testing.T) {
	engine, f := newAttrPageEngine(t)
	if engine == nil {
		return
	}
	group, err := f.svc.CreateAttribute(t.Context(), &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Name: "尺寸", Key: "size",
		Values: []productdto.AttributeValueReq{
			{Label: "S", Key: "s"},
			{Label: "M", Key: "m"},
		},
	})
	if err != nil {
		t.Fatalf("创建属性组失败: %v", err)
	}

	// add：两行 → 三行（新行为空占位）。
	form := url.Values{}
	form.Set("action", "add")
	form.Set("groupId", group.ID)
	form.Set("values[0].id", group.Values[0].ID)
	form.Set("values[0].key", "s")
	form.Set("values[0].label", "S")
	form.Set("values[1].id", group.Values[1].ID)
	form.Set("values[1].key", "m")
	form.Set("values[1].label", "M")
	rec := postForm(engine, "/admin/product-attributes/value-rows", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("片段应 200，实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "values[2].label") {
		t.Fatalf("add 后应出现第 3 行：%s", body)
	}

	// remove：删第 0 行（S）后保留原索引 1，避免失败回显把用户行值压紧错位。
	form.Set("action", "remove")
	form.Set("removeIndex", "0")
	rec = postForm(engine, "/admin/product-attributes/value-rows", form)
	body = rec.Body.String()
	if strings.Contains(body, "values[2].label") || strings.Contains(body, "values[0].label") {
		t.Fatalf("remove 后只应保留原索引 1 的一行：%s", body)
	}
	needle := "values[1].label\" value=\"M\""
	if !strings.Contains(body, needle) {
		t.Fatalf("remove 第 0 行后原索引 1 的显示名应为 M：%s", body)
	}
}

// TestAttributePageCreateAndSetValues 页面写链路：
// 表单 POST 建组 + 整体保存值（服务端归一与排序），随后页面能看到结果。
func TestAttributePageCreateAndSetValues(t *testing.T) {
	engine, f := newAttrPageEngine(t)
	if engine == nil {
		return
	}
	form := url.Values{}
	form.Set("projectId", f.projectID)
	form.Set("name", "口味")
	form.Set("key", "flavor")
	form.Set("isVariation", "1")
	form.Set("values[0].label", "原味")
	form.Set("values[1].label", "香辣")
	rec := postForm(engine, "/admin/product-attributes/create", form)
	if rec.Code != http.StatusFound {
		t.Fatalf("POST create 应 302 回列表，实际 %d", rec.Code)
	}

	list, err := f.svc.ListAttributes(t.Context(), &productdto.ListAttributeReq{ProjectID: f.projectID})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if len(list) != 1 || list[0].Key != "flavor" || len(list[0].Values) != 2 {
		t.Fatalf("表单建组结果不符：%+v", list)
	}

	// 整体保存值：只提交一行 → 库里应只剩一行。
	form2 := url.Values{}
	form2.Set("projectId", f.projectID)
	form2.Set("id", list[0].ID)
	form2.Set("values[0].label", "原味")
	rec = postForm(engine, "/admin/product-attributes/set-values", form2)
	if rec.Code != http.StatusFound {
		t.Fatalf("POST set-values 应 302 回列表，实际 %d", rec.Code)
	}
	got, err := f.svc.GetAttribute(t.Context(), &productdto.GetAttributeReq{ID: list[0].ID})
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
