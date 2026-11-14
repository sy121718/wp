package workbenchhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 非表单编码的写请求必须在绑定阶段被拒，不能落到「空表单 + 默认值」路径。
func TestWorkbenchFormEndpointsRejectJSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handle := &Handle{}
	router := gin.New()
	router.POST("/workbench/outline", handle.OutlineTree)
	router.POST("/workbench/preview", handle.PreviewDraft)
	router.POST("/workbench/template/preview", handle.TemplatePreviewDraft)

	for _, path := range []string{"/workbench/outline", "/workbench/preview", "/workbench/template/preview"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"id":"p1","draftDocument":"{}"}`))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("状态码 = %d，期望 400；正文 %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}
