package producthttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/binding"
)

// 商品后台写入口已收窄为显式表单（FormPost）：表单编码被接受，JSON 编码被拒。
// 这条判据锁定「协议唯一」的事实 —— 退回兼容入口会让契约重新报 unknown。
func TestProductWriteDTOsAcceptFormdataThroughExplicitFormEntry(t *testing.T) {
	cases := []struct {
		name string
		body string
		bind func(c *gin.Context) bool
	}{
		{"DeleteBrandReq", "id=b1&projectId=p1", func(c *gin.Context) bool {
			var req productdto.DeleteBrandReq
			return c.ShouldBindWith(&req, ginbinding.FormPost) == nil
		}},
		{"DeleteCategoryReq", "id=c1&projectId=p1", func(c *gin.Context) bool {
			var req productdto.DeleteCategoryReq
			return c.ShouldBindWith(&req, ginbinding.FormPost) == nil
		}},
		{"DeleteTagReq", "id=t1&projectId=p1", func(c *gin.Context) bool {
			var req productdto.DeleteTagReq
			return c.ShouldBindWith(&req, ginbinding.FormPost) == nil
		}},
		{"DeleteAttributeReq", "id=a1&projectId=p1", func(c *gin.Context) bool {
			var req productdto.DeleteAttributeReq
			return c.ShouldBindWith(&req, ginbinding.FormPost) == nil
		}},
		{"BulkDeleteReq", "projectId=p1&ids=x&ids=y", func(c *gin.Context) bool {
			var req productdto.BulkDeleteReq
			return c.ShouldBindWith(&req, ginbinding.FormPost) == nil
		}},
		{"SetAttributeValuesReq", "projectId=p1&id=a1&values=" +
			`[{"id":"v1","key":"k1","label":"甲"}]`, func(c *gin.Context) bool {
			_, err := binding.ParseForm[productdto.SetAttributeValuesReq](c)
			return err == nil
		}},
		{"SaveProductTranslationsReq", "productId=p1&projectId=p1", func(c *gin.Context) bool {
			var req productdto.SaveProductTranslationsReq
			return c.ShouldBindWith(&req, ginbinding.FormPost) == nil
		}},
		{"RecalcTagsReq", "projectId=p1", func(c *gin.Context) bool {
			var req productdto.RecalcTagsReq
			return c.ShouldBindWith(&req, ginbinding.FormPost) == nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			r := gin.New()
			var bound bool
			r.POST("/x", func(c *gin.Context) { bound = tc.bind(c) })
			w := httptest.NewRecorder()
			rq := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(tc.body))
			rq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.ServeHTTP(w, rq)
			if !bound {
				t.Fatalf("%s 走显式表单入口时应接受表单编码", tc.name)
			}
		})
	}
}

// 收窄后 JSON 编码必须被拒：这是有意行为，不是回归。
func TestProductWriteFormEndpointsRejectJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var bound bool
	r.POST("/x", func(c *gin.Context) {
		var req productdto.DeleteBrandReq
		bound = c.ShouldBindWith(&req, ginbinding.FormPost) == nil
	})
	rq := httptest.NewRequest(http.MethodPost, "/x",
		strings.NewReader(`{"id":"b1","projectId":"p1"}`))
	rq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(httptest.NewRecorder(), rq)
	if bound {
		t.Fatal("表单端点不应接受 JSON 编码（协议必须唯一）")
	}
}

// DTO 的表单钩子仍可由显式表单入口调用（这些钩子本身不是双轨，入口才是）。
func TestProductWriteDTOsStillBindThroughExplicitFormEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var req *productdto.SetAttributeValuesReq
	r.POST("/x", func(c *gin.Context) {
		req, _ = binding.ParseForm[productdto.SetAttributeValuesReq](c)
	})
	body := "projectId=p1&id=a1&values=" + `[{"id":"v1","key":"k1","label":"甲"},{"id":"v2","key":"k2","label":"乙"}]`
	rq := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	rq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(httptest.NewRecorder(), rq)
	if req == nil || len(req.Values) != 2 {
		t.Fatalf("ParseForm 应走 DTO 的 BindForm 钩子：%+v", req)
	}
}

// 切片字段不接受把整个数组塞进一个字符串 —— 保持对 gin 默认行为的显式判据。
func TestSliceOfStructRejectsWholeArrayString(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var ok bool
	r.POST("/x", func(c *gin.Context) {
		_, err := binding.ParseForm[struct {
			Values []productdto.AttributeValueReq `form:"values"`
		}](c)
		ok = err == nil
	})
	w := httptest.NewRecorder()
	body := `projectId=p1&id=a1&values=[{"id":"v1","key":"k1"}]`
	rq := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	rq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, rq)
	if ok {
		t.Fatal("gin 默认行为已接受整数组，请复核 DTO FormBinder 是否仍必需")
	}
}
