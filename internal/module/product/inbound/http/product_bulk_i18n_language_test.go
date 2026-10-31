package producthttp

import (
	"github.com/gin-gonic/gin"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProductVariantSaveNoticeFollowsRequestLanguage(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{productenums.ProductsActionDone: {"zh-CN": "操作已完成", "en-US": "Done"}}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	for lang, want := range map[string]string{"zh-CN": "操作已完成", "en-US": "Done"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/api/product/variant/save", nil)
		c.Request.Header.Set("Accept-Language", lang)
		if got := response.MsgText(c, productActionDoneMsg); got != want {
			t.Fatalf("%s 成功文案=%q，期望 %q", lang, got, want)
		}
	}
}
