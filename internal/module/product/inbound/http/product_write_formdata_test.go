package producthttp

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/binding"
)

func TestProductWriteDTOsBindFormdata(t *testing.T) {
	cases := []struct {
		name string
		body string
		bind func(c *gin.Context) bool
	}{
		{"DeleteBrandReq", "id=b1&projectId=p1", func(c *gin.Context) bool { _, ok := binding.JSON[productdto.DeleteBrandReq](c); return ok }},
		{"DeleteCategoryReq", "id=c1&projectId=p1", func(c *gin.Context) bool { _, ok := binding.JSON[productdto.DeleteCategoryReq](c); return ok }},
		{"DeleteTagReq", "id=t1&projectId=p1", func(c *gin.Context) bool { _, ok := binding.JSON[productdto.DeleteTagReq](c); return ok }},
		{"DeleteAttributeReq", "id=a1&projectId=p1", func(c *gin.Context) bool { _, ok := binding.JSON[productdto.DeleteAttributeReq](c); return ok }},
		{"BulkDeleteReq", "projectId=p1&ids=x&ids=y", func(c *gin.Context) bool { _, ok := binding.JSON[productdto.BulkDeleteReq](c); return ok }},

		{"CreateAttributeReq（带 values）", "projectId=p1&name=n&key=k" +
			"&values=" + `[{"id":"v1","key":"k1","label":"甲"},{"id":"v2","key":"k2","label":"乙"}]`,
			func(c *gin.Context) bool {
				req, ok := binding.JSON[productdto.CreateAttributeReq](c)
				if !ok {
					return false
				}
				return len(req.Values) == 2
			}},
		{"SetAttributeValuesReq（带 values）", "projectId=p1&id=a1" +
			"&values=" + `[{"id":"v1","key":"k1","label":"甲","sort":"1"},{"id":"v2","key":"k2","label":"乙","sort":"2"}]`,
			func(c *gin.Context) bool {
				req, ok := binding.JSON[productdto.SetAttributeValuesReq](c)
				if !ok {
					return false
				}
				return len(req.Values) == 2
			}},
		{"SaveProductTranslationsReq", "productId=p1&projectId=p1", func(c *gin.Context) bool { _, ok := binding.JSON[productdto.SaveProductTranslationsReq](c); return ok }},
		{"RecalcTagsReq", "projectId=p1", func(c *gin.Context) bool { _, ok := binding.JSON[productdto.RecalcTagsReq](c); return ok }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			r := gin.New()
			var ok bool
			r.POST("/x", func(c *gin.Context) { ok = tc.bind(c) })
			w := httptest.NewRecorder()
			rq := httptest.NewRequest("POST", "/x", strings.NewReader(tc.body))
			rq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.ServeHTTP(w, rq)
			if !ok {
				t.Fatalf("%s 用 formdata 绑定失败（status=%d）—— 该端点的 htmx 表单会 400 或静默收零值", tc.name, w.Code)
			}
		})
	}
}

func TestSliceOfStructRejectsWholeArrayString(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var ok bool
	r.POST("/x", func(c *gin.Context) {
		_, ok = binding.JSON[struct {
			Values []productdto.AttributeValueReq `form:"values"`
		}](c)
	})
	w := httptest.NewRecorder()
	body := `projectId=p1&id=a1&values=[{"id":"v1","key":"k1"}]`
	rq := httptest.NewRequest("POST", "/x", strings.NewReader(body))
	rq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, rq)
	if ok {
		t.Fatal("gin 默认行为已接受整数组，请复核 DTO FormBinder 是否仍必需")
	}
}
