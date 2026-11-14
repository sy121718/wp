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

// 预览端点声明为表单写：选择项由 attr:<id> 重复键提交，经 DTO 的 BindForm 钩子解析。
func TestVariantPreviewBindsFormSelections(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/product/variant/preview",
		strings.NewReader("productId=p1&projectId=pr1&attr:a1=v1"))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r, err := binding.ParseForm[productdto.VariantPreviewFormReq](c)
	if err != nil || r.ProductID != "p1" || len(r.Selections) != 1 ||
		r.Selections[0].AttributeID != "a1" || len(r.Selections[0].ValueIDs) != 1 || r.Selections[0].ValueIDs[0] != "v1" {
		t.Fatalf("绑定丢失选择：%+v err=%v", r, err)
	}
}
