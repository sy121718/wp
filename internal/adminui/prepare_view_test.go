package adminui

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// viewFixture 视图摊平的判据样本：导出字段、标签改名、嵌套结构体、未导出字段。
type viewFixture struct {
	Title        string
	Projects     []string
	LoadFailed   bool
	LoadError    Msg
	renamed      string
	RenamedField string `view:"CustomName"`
}

// TestViewMapFlattensExportedFields 视图结构体摊平的键名规则。
//
// 判据：模板读的是**一层键**（`{{.Projects}}`），而视图是结构体（控制器不该手工拼 gin.H）。
// 键名 = 字段名（`view:"名字"` 可改名）；嵌套结构体**不递归**（模板写 `{{.LoadError.Key}}`）；
// 未导出字段不进；`renamed` 这种未导出字段漏进去会让模板拿到不该暴露的内部值。
func TestViewMapFlattensExportedFields(t *testing.T) {
	v := &viewFixture{
		Title:        "新建商品",
		Projects:     []string{"p1", "p2"},
		LoadFailed:   true,
		LoadError:    Msg{Key: "admin.products.preview.failed", Fallback: "预览生成失败"},
		renamed:      "内部值",
		RenamedField: "带标签",
	}
	data := ViewMap(v)

	if data["Title"] != "新建商品" {
		t.Fatalf("Title 应摊平到一层键，实际 %#v", data["Title"])
	}
	if got, ok := data["Projects"].([]string); !ok || len(got) != 2 {
		t.Fatalf("Projects 应原样保留切片，实际 %#v", data["Projects"])
	}
	if data["LoadFailed"] != true {
		t.Fatalf("LoadFailed 应为 bool true（模板用它判失败分支），实际 %#v", data["LoadFailed"])
	}
	// 嵌套结构体不摊平：模板写 {{.LoadError.Key}}，摊平会把 Key 提升到根上（撞名风险）。
	if msg, ok := data["LoadError"].(Msg); !ok || msg.Key == "" {
		t.Fatalf("嵌套视图应原样保留结构体，实际 %#v", data["LoadError"])
	}
	if _, ok := data["Key"]; ok {
		t.Fatalf("嵌套结构体的字段不该被提升到根键")
	}
	if _, ok := data["renamed"]; ok {
		t.Fatalf("未导出字段不该进视图 map")
	}
	if data["CustomName"] != "带标签" {
		t.Fatalf("view 标签应覆盖键名，实际 %#v", data["CustomName"])
	}
	if _, ok := data["RenamedField"]; ok {
		t.Fatalf("带 view 标签的字段不应再以字段名出现")
	}
}

// TestViewMapNilAndMapPassthrough nil 视图与 map 直通。
//
// nil（含 nil 指针）必须是**空 map 而不是 panic**：错误分支拿不到视图时页面仍要能渲染
// （失败页只多一个错误槽）；存量 handler 仍在传 gin.H / map[string]any，必须原样透传。
func TestViewMapNilAndMapPassthrough(t *testing.T) {
	if got := ViewMap(nil); len(got) != 0 {
		t.Fatalf("nil 视图应为空 map，实际 %#v", got)
	}
	var p *viewFixture
	if got := ViewMap(p); len(got) != 0 {
		t.Fatalf("nil 指针视图应为空 map，实际 %#v", got)
	}
	raw := gin.H{"Projects": []string{"p1"}}
	if got := ViewMap(raw); got["Projects"] == nil {
		t.Fatalf("gin.H 应原样透传，实际 %#v", got)
	}
	if got := ViewMap(map[string]any{"X": 1}); got["X"] != 1 {
		t.Fatalf("map[string]any 应原样透传，实际 %#v", got)
	}
}

// TestViewMapPanicsOnNonStruct 非结构体传入是编程错误，必须当场炸。
//
// 判据的理由同 order 的 selectExpr：把视图装配写成 `ViewMap([]string{...})` 这类错，
// 静默返回空 map 会让页面渲染成「空白但 200」，而正确写法永不触发 panic。
func TestViewMapPanicsOnNonStruct(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("非结构体传入应 panic（写对了永不触发）")
		}
	}()
	ViewMap([]string{"a"})
}

// TestPrepareViewInjectsI18nAndFlattens PrepareView = ViewMap + Prepare。
//
// 判据：页面渲染入口必须同时拿到取词函数（t/tf）与摊平后的视图键 —— 少一样都是**静默**的：
// 缺 t 是整页取词变空串（200、结构完整），缺视图键是模板读不到数据。
func TestPrepareViewInjectsI18nAndFlattens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/products/new", nil)

	data := PrepareView(c, &viewFixture{Title: "新建商品", Projects: []string{"p1"}})
	if data["Projects"] == nil {
		t.Fatalf("视图键应已摊平，实际 %#v", data)
	}
	if _, ok := data["t"].(func(string, string) string); !ok {
		t.Fatalf("应注入取词函数 t，实际 %#v", data["t"])
	}
	if _, ok := data["tf"]; !ok {
		t.Fatalf("应注入带占位符的取词函数 tf")
	}
	if _, ok := data["csrf_token"]; !ok {
		t.Fatalf("整页渲染入口必须带 csrf_token（表单提交靠它）")
	}
}
