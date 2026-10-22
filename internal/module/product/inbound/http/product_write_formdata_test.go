package producthttp

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	productdto "go_wp/internal/module/product/dto"
	"go_wp/pkg/binding"
)

// product_write_formdata_test.go — 商品写端点必须能绑定 **formdata**。
//
// 为什么需要它：htmx 的写表单发的是 application/x-www-form-urlencoded，而其余测试
// （handler 级、feature 级）几乎都用 JSON body。于是「DTO 缺 form tag」这类缺陷
// 在测试里完全不可见 —— 实测过：无 form tag 时 camelCase 键名一个都绑不上，
// 有 required 的直接 400，没 required 的静默收零值（后者更危险：保存成功但什么都没改）。
//
// 判据用**模板实际会发的键名**（与 json tag 一致的小驼峰），不是 Go 字段名 ——
// 后者才是 gin 在没有 form tag 时的匹配依据，用它测等于把缺陷测没了。
//
// 新增商品写端点时，把它的 DTO 与最小 body 加进 cases。
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
		{"AddRatingReq", "productId=p1&score=4.5", func(c *gin.Context) bool { _, ok := binding.JSON[productdto.AddRatingReq](c); return ok }},
		{"CreateAttributeReq", "projectId=p1&name=n&key=k", func(c *gin.Context) bool { _, ok := binding.JSON[productdto.CreateAttributeReq](c); return ok }},
		{"SaveProductTranslationsReq", "productId=p1&projectId=p1", func(c *gin.Context) bool { _, ok := binding.JSON[productdto.SaveProductTranslationsReq](c); return ok }},
		{"SetBundleConfigReq", "productId=p1&projectId=p1", func(c *gin.Context) bool { _, ok := binding.JSON[productdto.SetBundleConfigReq](c); return ok }},
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
