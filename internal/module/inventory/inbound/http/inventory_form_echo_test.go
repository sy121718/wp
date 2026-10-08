package inventoryhttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginrender "github.com/gin-gonic/gin/render"

	inventoryenums "go_wp/internal/module/inventory/enums"
)

type inventoryCaptureRender struct {
	name string
	data any
}

func (r *inventoryCaptureRender) Instance(name string, data any) ginrender.Render {
	r.name, r.data = name, data
	return &hxStubInstance{name: name}
}

func inventoryFormContext(t *testing.T, values url.Values, hx bool) (*gin.Context, *httptest.ResponseRecorder, *inventoryCaptureRender) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(rec)
	view := &inventoryCaptureRender{}
	engine.HTMLRender = view
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/inventory/form", strings.NewReader(values.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if hx {
		c.Request.Header.Set("HX-Request", "true")
	}
	return c, rec, view
}

func TestInventoryFormFailurePreservesRawInput(t *testing.T) {
	tests := []struct {
		name, templateName string
		fields             []string
		values             url.Values
		fail               func(*gin.Context)
	}{
		{"warehouse create", "admin/inventory/inventory_warehouse_form.html", warehouseCreateFields,
			url.Values{"projectId": {"p1"}, "code": {"  WH  "}, "name": {"  仓库  "}, "sort": {""}, "type": {"third_party"}, "provider": {"  vendor  "}, "apiCredential": {"secret"}, "isDefault": {"1"}},
			func(c *gin.Context) {
				(&inventoryPageHandle{}).warehouseFormFail(c, false, errors.New(inventoryenums.ErrWarehouseCodeTaken))
			}},
		{"warehouse edit", "admin/inventory/inventory_warehouse_form.html", warehouseEditFields,
			url.Values{"projectId": {"p1"}, "id": {"w1"}, "name": {"  新名  "}, "code": {"  WH  "}, "sort": {"7"}, "type": {"virtual"}, "status": {"disabled"}, "allowsShipping": {"1"}},
			func(c *gin.Context) {
				(&inventoryPageHandle{}).warehouseFormFail(c, true, errors.New(inventoryenums.ErrWarehouseCodeTaken))
			}},
		{"reason create", "admin/inventory/inventory_reason_form.html", reasonCreateFields,
			url.Values{"projectId": {"p1"}, "code": {"  reason  "}, "name": {"  原因  "}, "direction": {"out"}, "sort": {""}},
			func(c *gin.Context) {
				(&inventoryPageHandle{}).reasonFormFail(c, errors.New(inventoryenums.ErrReasonCodeTaken))
			}},
		{"reason builtin edit", "admin/inventory/inventory_reason_form.html", reasonEditFields,
			url.Values{"projectId": {"p1"}, "id": {"7"}, "builtin": {"1"}, "sort": {" 12 "}},
			func(c *gin.Context) {
				(&inventoryPageHandle{}).reasonEditFormFail(c, errors.New(inventoryenums.ErrReasonStatusInvalid))
			}},
		{"reason custom edit", "admin/inventory/inventory_reason_form.html", reasonEditFields,
			url.Values{"projectId": {"p1"}, "id": {"8"}, "builtin": {"0"}, "name": {"  新原因  "}, "sort": {"3"}},
			func(c *gin.Context) {
				(&inventoryPageHandle{}).reasonEditFormFail(c, errors.New(inventoryenums.ErrReasonStatusInvalid))
			}},
		{"source create", "admin/inventory/inventory_source_form.html", sourceCreateFields,
			url.Values{"projectId": {"p1"}, "code": {"  SOURCE  "}, "name": {"  货源  "}, "type": {"internal"}, "relatedParty": {"false"}, "settlePrice": {""}, "sort": {""}, "config": {" {\"a\":1}  "}},
			func(c *gin.Context) {
				(&inventorySourcePageHandle{}).sourceFormFail(c, false, errors.New(inventoryenums.ErrSourceSettleInvalid))
			}},
		{"source edit", "admin/inventory/inventory_source_form.html", sourceEditFields,
			url.Values{"projectId": {"p1"}, "id": {"s1"}, "code": {"  SOURCE  "}, "name": {"  货源  "}, "type": {"external"}, "relatedParty": {""}, "status": {"disabled"}, "settlePrice": {""}, "sort": {"0"}, "config": {"  {bad}  "}},
			func(c *gin.Context) {
				(&inventorySourcePageHandle{}).sourceFormFail(c, true, errors.New(inventoryenums.ErrSourceSettleInvalid))
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, rec, view := inventoryFormContext(t, tt.values, true)
			tt.fail(c)
			c.Writer.WriteHeaderNow()
			if rec.Code != http.StatusOK || view.name != tt.templateName {
				t.Fatalf("status=%d template=%q", rec.Code, view.name)
			}
			if rec.Header().Get("HX-Redirect") != "" {
				t.Fatal("failure must remain in drawer")
			}
			data, ok := view.data.(gin.H)
			if !ok {
				t.Fatalf("render data: %T", view.data)
			}
			got, ok := data["FormEcho"].(gin.H)
			if !ok {
				t.Fatalf("echo: %T", data["FormEcho"])
			}
			for _, field := range tt.fields {
				if got[field] != tt.values.Get(field) {
					t.Errorf("%s = %q, want %q", field, got[field], tt.values.Get(field))
				}
			}
			if data["SubmitErr"] == "" {
				t.Error("missing visible error")
			}
		})
	}
}

func TestInventoryFormSuccessJumpAndNativeFailure(t *testing.T) {
	back := inventoryWarehousesPath + "?project=p1"
	// 成功：htmx 档走 HX-Redirect，原生档渲染提示页（结论走响应体，不再是 302 + ?ok=）。
	for _, hx := range []bool{true, false} {
		c, rec, view := inventoryFormContext(t, url.Values{"projectId": {"p1"}}, hx)
		inventoryJump(c, true, "操作已完成", back, "仓库管理")
		c.Writer.WriteHeaderNow()
		if hx {
			if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != back {
				t.Fatalf("HX 成功跳转: %d %q", rec.Code, rec.Header().Get("HX-Redirect"))
			}
			continue
		}
		if rec.Code != http.StatusOK || view.name != "admin/jump.html" {
			t.Fatalf("原生成功提示页: %d %q", rec.Code, view.name)
		}
	}
	// 原生失败：渲染失败提示页（不再是 302 + ?err=）。
	c, rec, view := inventoryFormContext(t, url.Values{"projectId": {"p1"}, "code": {"bad"}}, false)
	(&inventoryPageHandle{}).warehouseFormFail(c, false, errors.New(inventoryenums.ErrWarehouseCodeTaken))
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusOK || view.name != "admin/jump.html" {
		t.Fatalf("原生失败提示页: %d %q", rec.Code, view.name)
	}
}
func TestInventoryFormEchoFieldsMatchFragments(t *testing.T) {
	for _, tt := range []struct {
		name   string
		fields []string
	}{
		// 仓库模板一个文件服务新建+编辑两态，isDefault 只在新建分支渲染；
		// 静态扫描按两态字段并集核对，运行时各自分支不会读到对方字段。
		{"warehouse", append(append([]string{}, warehouseCreateFields...), warehouseEditFields...)},
		// 原因片段同样服务新建与编辑，两份清单的并集必须覆盖每个 echo 键。
		{"reason", append(append([]string{}, reasonCreateFields...), reasonEditFields...)},
		{"source", sourceEditFields},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join("../../../../templates/admin/inventory", "inventory_"+tt.name+"_form.html")
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			re := regexp.MustCompile(`echo\.([a-zA-Z][a-zA-Z0-9]*)`)
			actual := map[string]bool{}
			for _, match := range re.FindAllStringSubmatch(string(body), -1) {
				actual[match[1]] = true
			}
			want := map[string]bool{}
			for _, field := range tt.fields {
				want[field] = true
			}
			for key := range actual {
				if !want[key] {
					t.Errorf("模板引用未列入回填清单: %s", key)
				}
			}
			for key := range want {
				if key == "projectId" || key == "id" && tt.name != "reason" {
					continue
				}
				if !actual[key] {
					t.Errorf("回填清单字段在模板中未引用: %s", key)
				}
			}
		})
	}
}
