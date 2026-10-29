package feature

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	pagedto "go_wp/internal/module/page/dto"
	workbenchhttp "go_wp/internal/module/workbench/inbound/http"
	json "go_wp/pkg/jsonx"
)

// TestWorkbenchIncompleteDraftPreview 保存和预览真实的未完成文档，
// 验证嵌套空文本不再阻断兄弟内容，也不会被预览偷偷补成持久化占位。
func TestWorkbenchIncompleteDraftPreview(t *testing.T) {
	gin.SetMode(gin.TestMode)
	_, svc, projectID := newPageService(t)
	ctx := context.Background()
	document := `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"body","type":"core.container","props":{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column"}}},"children":[{"id":"rich","type":"core.text","props":{"mode":"richtext","text":""}},{"id":"sibling","type":"core.heading","props":{"text":"兄弟标题正常预览"}}]}]}`
	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/incomplete-preview", DraftDocument: json.RawMessage(document),
	})
	if err != nil {
		t.Fatalf("未完成控件应能保存为草稿: %v", err)
	}
	before, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: created.ID})
	if err != nil {
		t.Fatal(err)
	}
	handle := workbenchhttp.New(svc, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.POST("/workbench/preview", handle.PreviewDraft)
	body := url.Values{"id": {created.ID}, "expectedVersion": {"1"}, "draftDocument": {document}, "editor": {"1"}}.Encode()
	request := httptest.NewRequest(http.MethodPost, "/workbench/preview?editor=1", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("未完成草稿预览应成功: %d %s", recorder.Code, recorder.Body.String())
	}
	for _, want := range []string{`sky-c-rich sky-incomplete`, `data-sky-id="rich"`, "兄弟标题正常预览"} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Errorf("真实预览缺少 %q", want)
		}
	}
	after, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: created.ID})
	if err != nil {
		t.Fatal(err)
	}
	if string(before.DraftDocument) != string(after.DraftDocument) || before.DraftVersion != after.DraftVersion {
		t.Fatal("预览不得修改保存的草稿")
	}
	invalid := strings.Replace(document, `"text":""`, `"text":"","binding":{"field":"post.bad;field"}`, 1)
	_, err = svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/invalid-preview", DraftDocument: json.RawMessage(invalid),
	})
	if err == nil {
		t.Fatal("空文本含非法绑定时不得按未完成状态保存")
	}
}
