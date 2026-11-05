package workbenchhttp

import (
	"github.com/gin-gonic/gin"
	"go_wp/pkg/builder/source"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPreviewStateBindingIsTemporaryAndClosed(t *testing.T) {
	for _, tc := range []struct {
		query  string
		want   source.PreviewState
		status int
	}{{"", source.PreviewDefault, 200}, {"?previewState=success", source.PreviewSuccess, 200}, {"?previewState=forbidden", source.PreviewForbidden, 200}, {"?previewState=variant-required", source.PreviewVariantRequired, 200}, {"?previewState=freeform-json", "", 400}} {
		t.Run(tc.query, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			var got source.PreviewState
			router.GET("/preview", previewContext, func(c *gin.Context) { got = source.PreviewStateFrom(c.Request.Context()); c.Status(http.StatusOK) })
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/preview"+tc.query, nil))
			if rec.Code != tc.status || got != tc.want {
				t.Fatalf("参数必须在绑定期收敛: status=%d state=%s", rec.Code, got)
			}
		})
	}
}
