package producthttp

// product_filter_keys_test.go — 各页**筛选上下文键表**的覆盖面。
//
// 键表是页面表单 action 与读回侧共用的唯一一份：删掉一个键不会编译报错，
// 只会让那一页的筛选在翻页 / 提交后静默丢失（页面不报错、日志也干净）。
// 读回实现本身在 internal/adminui（ListQuery），由它自己的测试守。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
)

func newFilterKeyContext(t *testing.T, rawQuery string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/x?"+rawQuery, nil)
	return c
}

// TestProductFilterKeysKeepContext 各页键表保留自己需要的上下文，且不透传未列出的键。
func TestProductFilterKeysKeepContext(t *testing.T) {
	c := newFilterKeyContext(t, "project=p1&keyword=abc&product=prod1&lang=en-US&source=BundleSourceProduct")

	if got := adminui.ListQuery(c, productPricingBackKeys...); !strings.Contains(got, "project=p1") {
		t.Errorf("定价键表 = %q（应保留 project）", got)
	}
	for name, keys := range map[string][]string{
		"标签": productTagsBackKeys,
		"分类": productCategoriesBackKeys,
		"品牌": productBrandsBackKeys,
	} {
		if got := adminui.ListQuery(c, keys...); !strings.Contains(got, "project=p1") {
			t.Errorf("%s键表 = %q（应保留 project）", name, got)
		}
	}
}
