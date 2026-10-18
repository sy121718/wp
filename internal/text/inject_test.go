package text

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestInjectProvidesNamedPlaceholderTranslate 守住 tn：具名占位符的填充必须在模板侧可用，
// 否则「{n} 个」这类词条会原样渲染成占位符（页面 200、内容错，测试不会红）。
func TestInjectProvidesNamedPlaceholderTranslate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/admin/products", nil)

	data := Inject(c, nil)
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