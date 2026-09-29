package producthttp

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBrandDrawerFailureEchoAndRedirect(t *testing.T) {
	h := &productPageHandle{}
	values := url.Values{"projectId": {"p1"}, "id": {"b1"}, "name": {"  Brand  "}, "slug": {""}, "sort": {"07"}, "logo": {" /logo "}, "description": {"<p>Draft</p>"}, "seoTitle": {""}, "seoDescription": {" SEO "}}
	for _, mode := range []string{"create", "update"} {
		t.Run(mode, func(t *testing.T) {
			c, rec, cap := newAttrCaptureContext(t, "true", values.Encode())
			h.brandFormFail(c, mode, "duplicate")
			c.Writer.WriteHeaderNow()
			if rec.Code != http.StatusOK || cap.name != "admin/product/product_brand_form.html" {
				t.Fatalf("failure: %d %q", rec.Code, cap.name)
			}
			data := capturedData(t, cap)
			if data["SubmitErr"] != "duplicate" {
				t.Errorf("error: %v", data["SubmitErr"])
			}
			echo := data["FormEcho"].(gin.H)
			for _, key := range []string{"projectId", "id", "name", "slug", "sort", "logo", "description", "seoTitle", "seoDescription"} {
				if echo[key] != values.Get(key) {
					t.Errorf("%s: %q != %q", key, echo[key], values.Get(key))
				}
			}
		})
	}
	c, rec, _ := newAttrCaptureContext(t, "", values.Encode())
	h.brandFormFail(c, "update", "duplicate")
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/admin/product-brands?project=p1&err=") {
		t.Fatalf("native failure: %d %v", rec.Code, rec.Header())
	}
	for _, hx := range []string{"true", ""} {
		c, rec, _ := newAttrCaptureContext(t, hx, values.Encode())
		brandFormSuccess(c, "p1")
		c.Writer.WriteHeaderNow()
		if hx == "true" && (rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "/admin/product-brands?project=p1") {
			t.Errorf("HX success: %d %v", rec.Code, rec.Header())
		}
		if hx == "" && (rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/product-brands?project=p1") {
			t.Errorf("native success: %d %v", rec.Code, rec.Header())
		}
	}
}
