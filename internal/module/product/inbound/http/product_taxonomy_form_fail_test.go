package producthttp

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

"github.com/gin-gonic/gin"
)

func TestCategoryDrawerFailureEcho(t *testing.T) {
	h := &productPageHandle{}
	form := url.Values{"projectId": {"p1"}, "id": {"c1"}, "name": {"  Category  "}, "slug": {""}, "parentId": {"p2"}, "sort": {"07"}, "image": {" /img "}, "seoTitle": {"  SEO "}, "seoDescription": {""}, "description": {"<p>Draft</p>"}}
	for _, mode := range []string{"create", "update"} {
		t.Run(mode, func(t *testing.T) {
			c, rec, cap := newAttrCaptureContext(t, "true", form.Encode())
			h.categoryFormFail(c, mode, "duplicate")
			c.Writer.WriteHeaderNow()
			if rec.Code != http.StatusOK || cap.name != "admin/product/product_category_form.html" {
				t.Fatalf("failure response: status=%d template=%q", rec.Code, cap.name)
			}
			data := capturedData(t, cap)
			if data["SubmitErr"] != "duplicate" {
				t.Fatalf("error slot: %v", data["SubmitErr"])
			}
			echo := data["FormEcho"].(gin.H)
			for _, key := range []string{"projectId", "name", "slug", "parentId", "sort", "image", "seoTitle", "seoDescription", "description"} {
				if echo[key] != form.Get(key) {
					t.Errorf("%s: got %q want %q", key, echo[key], form.Get(key))
				}
			}
		})
	}
}

func TestCategoryDrawerNativeFailureAndSuccess(t *testing.T) {
	h := &productPageHandle{}
	c, rec, _ := newAttrCaptureContext(t, "", "projectId=p1&name=Draft")
	h.categoryFormFail(c, "create", "duplicate")
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/admin/product-categories?project=p1&err=") {
		t.Fatalf("native failure: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	for _, hx := range []string{"true", ""} {
		c, rec, _ := newAttrCaptureContext(t, hx, "projectId=p1")
		categoryFormSuccess(c, "p1")
		c.Writer.WriteHeaderNow()
		if hx == "true" && (rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "/admin/product-categories?project=p1") {
			t.Errorf("HX success: %d headers=%v", rec.Code, rec.Header())
		}
		if hx == "" && (rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/product-categories?project=p1") {
			t.Errorf("native success: %d headers=%v", rec.Code, rec.Header())
		}
	}
}
