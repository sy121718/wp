package feature

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	pagedto "go_wp/internal/module/page/dto"

	"github.com/gin-gonic/gin"
)

// TestWorkbenchPreviewDraft 验证临时 AST 预览只编译响应，不修改已保存草稿。
func TestWorkbenchPreviewDraft(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, svc, projectID := newPageService(t)
	ctx := context.Background()
	initialDocument := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"heading-initial","type":"core.heading","props":{"text":"初始标题"}}]}`
	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID:         projectID,
		Kind:              "home",
		ContentTargetType: "none",
		DraftPath:         "/workbench-preview",
		DraftDocument:     json.RawMessage(initialDocument),
	})
	if err != nil {
		t.Fatalf("创建测试页面失败: %v", err)
	}
	before, err := svc.Detail(ctx, &pagedto.DetailReq{ID: created.ID})
	if err != nil {
		t.Fatalf("查询初始草稿失败: %v", err)
	}

	handle := dashboardhttp.NewHandle(svc, nil)
	router := gin.New()
	router.GET("/workbench/preview", handle.Preview)
	router.POST("/workbench/preview", handle.PreviewDraft)

	getRecorder := httptest.NewRecorder()
	router.ServeHTTP(getRecorder, httptest.NewRequest(http.MethodGet, "/workbench/preview?id="+created.ID+"&editor=1", nil))
	if getRecorder.Code != http.StatusOK || !strings.Contains(getRecorder.Body.String(), "初始标题") || !strings.Contains(getRecorder.Body.String(), "data-wp-id") {
		t.Fatalf("已保存草稿预览失败: status=%d body=%s", getRecorder.Code, getRecorder.Body.String())
	}

	temporaryDocument := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"heading-1","type":"core.heading","props":{"text":"未保存即时预览"}}]}`
	body := url.Values{
		"id":              {created.ID},
		"expectedVersion": {"1"},
		"draftDocument":   {temporaryDocument},
	}.Encode()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/workbench/preview", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("临时预览失败: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "未保存即时预览") || !strings.Contains(recorder.Body.String(), "data-wp-id") {
		t.Fatalf("临时预览缺少编译内容或编辑器桥接: %s", recorder.Body.String())
	}

	page, err := svc.Detail(ctx, &pagedto.DetailReq{ID: created.ID})
	if err != nil {
		t.Fatalf("查询测试页面失败: %v", err)
	}
	if string(page.DraftDocument) != string(before.DraftDocument) || page.DraftVersion != before.DraftVersion || page.UpdatedAt != before.UpdatedAt {
		t.Fatalf("临时预览不得修改草稿: %+v", page)
	}
}

// TestWorkbenchPreviewDraftRejectsStaleVersion 验证版本冲突不会编译临时草稿。
func TestWorkbenchPreviewDraftRejectsStaleVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, svc, projectID := newPageService(t)
	ctx := context.Background()
	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID:         projectID,
		Kind:              "home",
		ContentTargetType: "none",
		DraftPath:         "/workbench-preview-stale",
		DraftDocument:     json.RawMessage(pageDocument),
	})
	if err != nil {
		t.Fatalf("创建测试页面失败: %v", err)
	}

	handle := dashboardhttp.NewHandle(svc, nil)
	router := gin.New()
	router.POST("/workbench/preview", handle.PreviewDraft)
	body := url.Values{
		"id":              {created.ID},
		"expectedVersion": {"2"},
		"draftDocument":   {pageDocument},
	}.Encode()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/workbench/preview", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("旧版本临时预览应拒绝: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
