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
	// 原生失败：整页提示（200 + err 态）回分类页，取代原先的 302 + ?err=。
	c, rec := newProductJumpContext(t, "", "project=p1", "projectId=p1&name=Draft")
	h.categoryFormFail(c, "create", "duplicate")
	c.Writer.WriteHeaderNow()
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-jump-state="err"`) {
		t.Fatalf("native failure: %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "/admin/product-categories?") || !strings.Contains(rec.Body.String(), "project=p1") {
		t.Fatalf("native failure 回跳应回分类页并带上工程，body=%s", rec.Body.String())
	}

	// 成功出口：htmx 走 HX-Redirect，原生渲染 ok 态提示页。
	for _, hx := range []string{"true", ""} {
		if hx == "true" {
			c, rec, _ := newAttrCaptureContext(t, hx, "projectId=p1")
			categoryFormSuccess(c, "p1")
			c.Writer.WriteHeaderNow()
			if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("HX-Redirect"), "/admin/product-categories") {
				t.Errorf("HX success: %d headers=%v", rec.Code, rec.Header())
			}
			continue
		}
		c, rec := newProductJumpContext(t, "", "project=p1", "projectId=p1")
		categoryFormSuccess(c, "p1")
		c.Writer.WriteHeaderNow()
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-jump-state="ok"`) {
			t.Errorf("native success: %d headers=%v body=%s", rec.Code, rec.Header(), rec.Body.String())
		}
	}
}
