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

	// 原生失败：整页提示（200 + err 态）回品牌页，取代原先的 302 + ?err=。
	c, rec := newProductJumpContext(t, "", "project=p1", values.Encode())
	h.brandFormFail(c, "update", "duplicate")
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-jump-state="err"`) {
		t.Fatalf("native failure: %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/admin/product-brands?") || !strings.Contains(rec.Body.String(), "project=p1") {
		t.Fatalf("native failure 回跳应回品牌页并带上工程，body=%s", rec.Body.String())
	}

	// 成功出口：htmx 走 HX-Redirect（XHR 会跟随 302，读不到 Location），原生渲染 ok 态提示页。
	for _, hx := range []string{"true", ""} {
		if hx == "true" {
			c, rec, _ := newAttrCaptureContext(t, hx, values.Encode())
			brandFormSuccess(c, "p1")
			c.Writer.WriteHeaderNow()
			if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("HX-Redirect"), "/admin/product-brands") {
				t.Errorf("HX success: %d %v", rec.Code, rec.Header())
			}
			continue
		}
		c, rec := newProductJumpContext(t, "", "project=p1", values.Encode())
		brandFormSuccess(c, "p1")
		c.Writer.WriteHeaderNow()
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-jump-state="ok"`) {
			t.Errorf("native success: %d %q", rec.Code, rec.Body.String())
		}
	}
}
