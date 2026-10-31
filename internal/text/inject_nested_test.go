package text_test

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/text"
)

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
	text.Inject(c, data)

	if _, ok := inner["t"]; !ok {
		t.Fatal("嵌套 map 没拿到 t：include 里的取词会全是空串且页面 200")
	}
	if _, ok := typed["tf"]; !ok {
		t.Fatal("gin.H 形态的嵌套片段没拿到 tf")
	}
	if gt := keep["t"]; gt != "自定义取词函数" {
		t.Fatalf("已有键被覆盖了：%v", gt)
	}
	if lang, _ := inner["lang"].(string); lang == "" {
		t.Fatal("嵌套 map 应带当前语言")
	}

	if _, ok := data["Structs"].([]row)[0].EditForm["t"]; !ok {
		t.Fatal("结构体行里的 EditForm 没拿到 t")
	}
}
