package workbenchhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	presentationdto "go_wp/internal/module/presentation/dto"
)

type instancePreviewRecorder struct {
	request *presentationdto.PreviewInstanceReq
}

func (r *instancePreviewRecorder) PreviewInstance(_ context.Context, req *presentationdto.PreviewInstanceReq) (*presentationdto.PreviewInstanceResp, error) {
	copy := *req
	r.request = &copy
	return &presentationdto.PreviewInstanceResp{HTML: "<html><body>INSTANCE-PREVIEW</body></html>"}, nil
}

func TestInstancePreviewUsesInstanceIdentityWithoutTemplateParameter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := &instancePreviewRecorder{}
	h := New(nil, nil, nil, nil, nil, nil, recorder)
	router := gin.New()
	router.GET("/workbench/template/preview", h.TemplatePreview)
	router.POST("/workbench/template/preview", h.TemplatePreviewDraft)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/workbench/template/preview?instance=instance-1&projectId=project-1", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "INSTANCE-PREVIEW") || recorder.request == nil || recorder.request.InstanceID != "instance-1" || recorder.request.TemplateID != "" {
		t.Fatalf("实例初始预览不得要求template参数: status=%d request=%+v", w.Code, recorder.request)
	}
	form := url.Values{"instance": {"instance-1"}, "projectId": {"project-1"}, "draftDocument": {`{"settings":{},"root":[]}`}, "entityType": {"spoofed"}, "entityId": {"spoofed"}}
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/template/preview", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(w, req)
	if w.Code != 200 || recorder.request.InstanceID != "instance-1" || string(recorder.request.DraftDocument) != form.Get("draftDocument") || recorder.request.EntityID != "" {
		t.Fatalf("未保存实例草稿应保持实例身份并交唯一服务读取实体: status=%d request=%+v", w.Code, recorder.request)
	}
}
