package adminui

import (
	"go_wp/pkg/facing"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type viewFixture struct {
	Title        string
	Projects     []string
	LoadFailed   bool
	LoadError    facing.Msg
	renamed      string
	RenamedField string `view:"CustomName"`
}

func TestViewMapFlattensExportedFields(t *testing.T) {
	v := &viewFixture{
		Title:        "新建商品",
		Projects:     []string{"p1", "p2"},
		LoadFailed:   true,
		LoadError:    facing.Msg{Key: "admin.products.preview.failed", Fallback: "预览生成失败"},
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

	if msg, ok := data["LoadError"].(facing.Msg); !ok || msg.Key == "" {
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

func TestViewMapPanicsOnNonStruct(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("非结构体传入应 panic（写对了永不触发）")
		}
	}()
	ViewMap([]string{"a"})
}

func TestPrepareViewInjectsI18nAndFlattens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/products/new", nil)

	data := prepareView(c, &viewFixture{Title: "新建商品", Projects: []string{"p1"}})
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
