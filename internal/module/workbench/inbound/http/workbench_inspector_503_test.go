package workbenchhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/adminui"
)

func TestInspectorNavigationCreateUnassembledKeepsTranslatedMessage(t *testing.T) {

	handle := workbenchModeDeps{}.newHandle()
	router := newWorkbenchErrorProbeRouter(t, handle.InspectorNavigationCreate)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/probe?projectId=p-1", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("未装配能力应是 503，got %d", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, adminui.MsgInternalError) {
		t.Fatalf("message 不许是裸 key（前端会原样弹出来）：%s", body)
	}
	if !strings.Contains(body, "系统内部错误") {
		t.Fatalf("message 应是归口译文（缺词条时回落到与 058 zh-CN 词条逐字相同的中文），got %s", body)
	}
}
