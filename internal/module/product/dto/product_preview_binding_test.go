package productdto_test

import (
	"github.com/gin-gonic/gin"
	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/binding"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVariantPreviewBindsJSONAndFormSelections(t *testing.T) {
	for _, tc := range []struct{ kind, body string }{
		{"application/json", `{"productId":"p1","projectId":"pr1","selections":[{"attributeId":"a1","valueIds":["v1"]}]}`},
		{"application/x-www-form-urlencoded", "productId=p1&projectId=pr1&attr:a1=v1"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/api/product/variant/preview", strings.NewReader(tc.body))
			c.Request.Header.Set("Content-Type", tc.kind)
			r, ok := binding.JSON[productdto.VariantPreviewFormReq](c)
			if !ok || r.ProductID != "p1" || len(r.Selections) != 1 || r.Selections[0].AttributeID != "a1" || len(r.Selections[0].ValueIDs) != 1 || r.Selections[0].ValueIDs[0] != "v1" {
				t.Fatalf("绑定丢失选择：%+v", r)
			}
		})
	}
}
