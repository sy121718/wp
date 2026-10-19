package text_test

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/text"
)

// TestInjectSeedsNestedIncludeData 渲染层给每一张嵌套 map 补取词键。
//
// include 会换掉 . 的指向，抽屉 / 行内片段里的 .["t"] 只能由渲染层给；形状由 service 出，
// 取词函数由渲染层出 —— 少了这层，控制器就得为了一个取词函数去搬每一份片段数据。
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
	// 结构体行里的片段数据根同样要补：抽屉的 EditForm 就在这一层，漏了它 include 里
	// 的 .["t"] 是零值，取词调用在渲染期 panic。
	if _, ok := data["Structs"].([]row)[0].EditForm["t"]; !ok {
		t.Fatal("结构体行里的 EditForm 没拿到 t")
	}
}
