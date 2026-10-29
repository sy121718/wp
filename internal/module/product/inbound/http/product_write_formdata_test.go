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
		// **slice-of-struct 必须带内容**：审查指出只发 projectId/name/key 覆盖不到
		// Values 字段，于是「整体数组字符串绑不上」这个 P0 与全绿并存。
		// 统一写基座把各行序列化为一个 JSON 数组，控件基础类型保持字符串。
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

// TestSliceOfStructRejectsWholeArrayString 验证 DTO FormBinder 的必要性：gin 默认逐元素绑定不接受整数组。
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
