package jet_test

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/view/jet"
)

func TestInjectProvidesNamedPlaceholderTranslate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/admin/products", nil)

	data := jet.Inject(c, nil)
	tn, ok := data["tn"].(func(string, string, map[string]any) string)
	if !ok {
		t.Fatalf("tn 未注入或签名不符，实际 %T", data["tn"])
	}
	if got := tn("admin.products.list.tagCount", "{n} 个", map[string]any{"n": 4}); got != "4 个" {
		t.Fatalf("具名占位符未被填充：%q", got)
	}
	if _, ok := data["t"].(func(string, string) string); !ok {
		t.Fatalf("t 未注入，实际 %T", data["t"])
	}
}

func TestInjectSeedsNestedIncludeData(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/admin/product-tags?project=p1", nil)

	inner := map[string]any{"ID": "t1"}
	type row struct {
		Name     string
		EditForm map[string]any
	}
	typed := gin.H{"Name": "行内片段"}
	keep := map[string]any{"t": "自定义取词函数"}
	data := map[string]any{
		"Rows":    []map[string]any{{"EditForm": inner, "Items": []any{typed}}},
		"Structs": []row{{Name: "r1", EditForm: map[string]any{"ID": "a1"}}},
		"Keep":    keep,
		"Total":   1,
	}
	jet.Inject(c, data)

	if _, ok := inner["t"]; !ok {
		t.Fatal("嵌套 map 没拿到 t：include 里的取词会全是空串且页面 200")
	}
	if _, ok := typed["tf"]; !ok {
		t.Fatal("gin.H 形态的嵌套片段没拿到 tf")
	}
	if gt := keep["t"]; gt != "自定义取词函数" {
		t.Fatalf("已有键被覆盖了：%v", gt)
	}
}
