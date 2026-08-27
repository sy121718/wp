package feature

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	pagehttp "go_wp/internal/module/page/inbound/http"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

func TestPageHTTPCreateAndSaveDraft(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, svc, projectID := newPageService(t)
	handle := pagehttp.NewHandle(svc)
	router := gin.New()
	router.POST("/page/create", handle.Create)
	router.POST("/page/draft/save", handle.SaveDraft)

	createBody := []byte(`{"projectId":"` + projectID + `","kind":"home","contentTargetType":"none","draftPath":"/http-page","draftDocument":` + pageDocument + `}`)
	createRecorder := httptest.NewRecorder()
	router.ServeHTTP(createRecorder, httptest.NewRequest(http.MethodPost, "/page/create", bytes.NewReader(createBody)))
	if createRecorder.Code != http.StatusOK {
		t.Fatalf("创建 HTTP 请求失败: status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	var created response.Response
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("解析创建响应失败: %v", err)
	}
	data, ok := created.Data.(map[string]any)
	if !ok || data["id"] == "" || created.Code != http.StatusOK {
		t.Fatalf("创建响应内容错误: %+v", created)
	}

	saveBody := []byte(`{"id":"` + data["id"].(string) + `","expectedVersion":1,"draftPath":"/http-page-v2","draftDocument":` + pageDocument + `}`)
	saveRecorder := httptest.NewRecorder()
	router.ServeHTTP(saveRecorder, httptest.NewRequest(http.MethodPost, "/page/draft/save", bytes.NewReader(saveBody)))
	if saveRecorder.Code != http.StatusOK {
		t.Fatalf("保存草稿 HTTP 请求失败: status=%d body=%s", saveRecorder.Code, saveRecorder.Body.String())
	}
}
