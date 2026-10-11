package adminui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"
)

// createFormFixture 表单回显的判据样本：文本 / 复选框 / 指针数值 / 多值 / 标签改名 / 未导出。
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

// TestFillFormFillsByFieldName 回填按字段名（小驼峰）与 form 标签取值。
//
// 判据：字段清单就是**结构体本身** —— 原先靠手写的 `[]string{13 个 name}`，漏一个字段是渲染期
// 500（htmx 不 swap，用户点了保存什么都看不到）。这条测试同时钉住「标签覆盖」与「未导出字段不填」。
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

// TestFillFormPointerDistinguishesAbsentFromEmpty 「没填」与「填了空/0」是两回事。
//
// 判据来自 product 迁移 261 的口径：不填数量 = 不跟踪（nil），开关勾了而数量留空 = 明确 0。
// 若把指针字段当值字段处理（缺键也写零值），这两种情况会被合并成同一个结果，
// 而页面上看不出任何异常 —— 库存会被静默改成「明确没货」。
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

// ambiguousFormFixture 含连续大写却没写 form 标签的字段（表单名有歧义）。
type ambiguousFormFixture struct {
	ProjectID string
}

// TestFillFormPanicsOnAmbiguousAcronymName 含连续大写的字段必须写 form 标签，否则当场炸。
//
// 判据：`ProjectID` 的表单名是 `projectId` 还是 `projectID` 猜不出，而猜错的后果是**静默**的
// （回填读不到值、用户填的内容丢光，不报错）。宁可在第一次跑到时炸，也不要一个沉默的丢数据。
func TestFillFormPanicsOnAmbiguousAcronymName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("含连续大写的字段未写 form 标签时应 panic")
		}
	}()
	FillForm(&ambiguousFormFixture{}, url.Values{"projectId": {"p-1"}})
}

// TestFillFormRejectsNonPointer 传错形态是编程错误，必须当场炸。
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

// pageViewFixture 渲染出口的判据样本：实现 PageResult（400 + no-store）。
type pageViewFixture struct {
	Title string
	Error Msg
}

func (v pageViewFixture) PageStatus() int   { return http.StatusBadRequest }
func (v pageViewFixture) PageNoStore() bool { return true }

// okViewFixture 不实现 PageResult 的视图（存量迁移期）：按 200 + 可缓存处理。
type okViewFixture struct{ Title string }

// recordingRender 记录 c.HTML 实际渲染的模板名与 data（gin 无默认渲染器，测试里装一个桩）。
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
	// 必须真的写字节：gin 的 WriteHeader 是惰性的（首次 Write 才落地），
	// 桩渲染器只记录不写时 httptest 的 Code 会恒为 200 —— 那是测试桩的问题，不是被验代码的问题。
	_, err := w.Write([]byte("<stub/>"))
	return err
}

func (h *recordingHTML) WriteContentType(http.ResponseWriter) {}

func newRenderContext(t *testing.T, target string) (*gin.Context, *httptest.ResponseRecorder, *recordingRender) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	// CreateTestContext 的第二个返回值就是它自建的引擎 —— c.HTML 走的是 c.engine.HTMLRender。
	c, engine := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	renderer := &recordingRender{}
	engine.HTMLRender = renderer
	return c, rec, renderer
}

// TestRenderPageUsesViewStatusAndNoStore 分档与缓存策略由**视图**给出，控制器不再手写。
//
// 判据：失败页的 400/500 与 no-store 若留在 handler 里，每个 handler 都要写一遍，
// 漏一处就是「失败页被缓存」（重试看到的是缓存的失败页）或「参数不合法报 500」。
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

// TestRenderPageDefaultsToOK 不实现 PageResult 的视图按 200 + 可缓存处理。
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

// TestRenderBareAlwaysOK 这条出口一律 200，且只注入取词函数。
//
// 判据：htmx 2.0.4 把 5xx 判成 swap:false —— 失败回填返回 500 等于「用户点了没反应」，
// 所以失败必须表达在视图的失败槽里，状态码恒 200。这条出口也不该带页壳键（csrf/PermSet/NavGroups）。
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
