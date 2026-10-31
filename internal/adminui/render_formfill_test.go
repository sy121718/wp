package adminui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"
)

type createFormFixture struct {
	ProjectID     string   `form:"projectId"`
	Name          string   `form:"name"`
	TrackQuantity bool     `form:"trackQuantity"`
	Quantity      *int     `form:"quantity"`
	DefaultPrice  *float64 `form:"defaultPrice"`
	AttributeIDs  []string `form:"attributeIds"`
	URLSlug       string   `form:"slug"`
	Ignored       string   `form:"-"`
	internal      string
}

func TestFillFormFillsByFieldName(t *testing.T) {
	form := url.Values{
		"projectId":     {"p-1"},
		"name":          {"衬衫 A"},
		"trackQuantity": {"1"},
		"quantity":      {"5"},
		"defaultPrice":  {"12.5"},
		"attributeIds":  {"a-1", "a-2"},
		"slug":          {"shan-ye"},
		"ignored":       {"不该进"},
		"internal":      {"不该进"},
	}
	v := &createFormFixture{}
	FillForm(v, form)

	if v.ProjectID != "p-1" || v.Name != "衬衫 A" {
		t.Fatalf("文本字段未回填：%+v", v)
	}
	if !v.TrackQuantity {
		t.Fatalf("复选框字段应按 1/on/true/yes 判真：%+v", v)
	}
	if v.Quantity == nil || *v.Quantity != 5 {
		t.Fatalf("指针数值字段应回填：%+v", v.Quantity)
	}
	if v.URLSlug != "shan-ye" {
		t.Fatalf("form 标签应覆盖字段名（URLSlug ← slug）：%q", v.URLSlug)
	}
	if len(v.AttributeIDs) != 2 || v.AttributeIDs[0] != "a-1" || v.AttributeIDs[1] != "a-2" {
		t.Fatalf("多值字段应保序回填：%v", v.AttributeIDs)
	}
	if v.Ignored != "" || v.internal != "" {
		t.Fatalf("form:\"-\" 与未导出字段不该被填：%+v", v)
	}
}

func TestFillFormPointerDistinguishesAbsentFromEmpty(t *testing.T) {
	absent := &createFormFixture{}
	FillForm(absent, url.Values{"name": {"x"}})
	if absent.Quantity != nil {
		t.Fatalf("键不存在时指针字段必须留 nil（= 没填），实际 %v", *absent.Quantity)
	}
	if absent.DefaultPrice != nil {
		t.Fatalf("键不存在时指针字段必须留 nil，实际 %v", *absent.DefaultPrice)
	}

	empty := &createFormFixture{}
	FillForm(empty, url.Values{"quantity": {""}, "defaultPrice": {""}})
	if empty.Quantity == nil || *empty.Quantity != 0 {
		t.Fatalf("键存在但空 → 零值指针（= 明确 0），实际 %v", empty.Quantity)
	}
	if empty.DefaultPrice == nil || *empty.DefaultPrice != 0 {
		t.Fatalf("键存在但空 → 零值指针，实际 %v", empty.DefaultPrice)
	}
}

type ambiguousFormFixture struct {
	ProjectID string
}

func TestFillFormPanicsOnAmbiguousAcronymName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("含连续大写的字段未写 form 标签时应 panic")
		}
	}()
	FillForm(&ambiguousFormFixture{}, url.Values{"projectId": {"p-1"}})
}

func TestFillFormRejectsNonPointer(t *testing.T) {
	for _, bad := range []any{createFormFixture{}, nil} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("FillForm(%T) 应 panic（写对了永不触发）", bad)
				}
			}()
			FillForm(bad, url.Values{})
		}()
	}
}

type pageViewFixture struct {
	Title string
	Error Msg
}

func (v pageViewFixture) PageStatus() int   { return http.StatusBadRequest }
func (v pageViewFixture) PageNoStore() bool { return true }

type okViewFixture struct{ Title string }

type recordingRender struct {
	tpl  string
	data any
}

func (r *recordingRender) Instance(name string, data any) render.Render {
	return &recordingHTML{name: name, data: data, owner: r}
}

type recordingHTML struct {
	name  string
	data  any
	owner *recordingRender
}

func (h *recordingHTML) Render(w http.ResponseWriter) error {
	h.owner.tpl, h.owner.data = h.name, h.data

	_, err := w.Write([]byte("<stub/>"))
	return err
}

func (h *recordingHTML) WriteContentType(http.ResponseWriter) {}

func newRenderContext(t *testing.T, target string) (*gin.Context, *httptest.ResponseRecorder, *recordingRender) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()

	c, engine := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	renderer := &recordingRender{}
	engine.HTMLRender = renderer
	return c, rec, renderer
}

func TestRenderPageUsesViewStatusAndNoStore(t *testing.T) {
	c, rec, renderer := newRenderContext(t, "/admin/products/new")
	RenderPage(c, "admin/product/products_new.html", pageViewFixture{Title: "新建商品"})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("状态码应取视图的 PageStatus（400），实际 %d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("视图要求 no-store 时必须设置，实际 %q", rec.Header().Get("Cache-Control"))
	}
	if renderer.tpl != "admin/product/products_new.html" {
		t.Fatalf("渲染模板名应为传入值，实际 %q", renderer.tpl)
	}
	data, ok := renderer.data.(gin.H)
	if !ok {
		t.Fatalf("渲染 data 应为摊平后的 gin.H，实际 %T", renderer.data)
	}
	if data["Title"] != "新建商品" {
		t.Fatalf("视图字段应已摊平，实际 %#v", data["Title"])
	}
	if _, ok := data["t"].(func(string, string) string); !ok {
		t.Fatalf("整页渲染必须注入取词函数 t（漏了是静默空串）")
	}
	if _, ok := data["csrf_token"]; !ok {
		t.Fatalf("整页渲染必须带 csrf_token")
	}
}

func TestRenderPageDefaultsToOK(t *testing.T) {
	c, rec, _ := newRenderContext(t, "/admin/products")
	RenderPage(c, "admin/product/products.html", okViewFixture{Title: "商品列表"})

	if rec.Code != http.StatusOK {
		t.Fatalf("未实现 PageResult 的视图应 200，实际 %d", rec.Code)
	}
	if rec.Header().Get("Cache-Control") == "no-store" {
		t.Fatalf("未要求 no-store 的视图不该被设成 no-store")
	}
}

func TestRenderBareAlwaysOK(t *testing.T) {
	c, rec, renderer := newRenderContext(t, "/admin/products/new")
	RenderBare(c, "admin/product/product_create_form.html", pageViewFixture{Title: "失败表单"})

	if rec.Code != http.StatusOK {
		t.Fatalf("这条出口必须恒 200（htmx 不 swap 5xx），实际 %d", rec.Code)
	}
	data := renderer.data.(gin.H)
	if _, ok := data["tf"]; !ok {
		t.Fatalf("这条出口必须注入 tf（模板用 tf 填占位符，缺了是 500）")
	}
	if _, ok := data["csrf_token"]; ok {
		t.Fatalf("这条出口不该注入页壳键（csrf_token 属整页渲染）")
	}
}
