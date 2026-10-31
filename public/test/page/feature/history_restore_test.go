package feature

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pagehttp "go_wp/internal/module/page/inbound/http"

	"github.com/gin-gonic/gin"
)

func TestHistoryRestoreOverwritesDraft(t *testing.T) {
	_, svc, projectID := newPageService(t)
	ctx := context.Background()
	firstDoc := `{"settings":{"layout":{"mode":"full"}},"root":[]}`
	secondDoc := `{"settings":{"layout":{"mode":"full"},"seo":{"title":"第二版"}},"root":[]}`

	created, err := svc.Create(ctx, &pagedto.CreateReq{
		ProjectID: projectID, Kind: "home", ContentTargetType: "none",
		DraftPath: "/about", DraftDocument: json.RawMessage(firstDoc),
	})
	if err != nil {
		t.Fatalf("创建页面失败: %v", err)
	}
	if _, err = svc.SaveDraft(ctx, &pagedto.SaveDraftReq{
		ID: created.ID, ExpectedVersion: created.DraftVersion,
		DraftPath: "/about", DraftDocument: json.RawMessage(secondDoc),
	}); err != nil {
		t.Fatalf("保存草稿失败: %v", err)
	}

	revs, err := svc.ListRevisions(ctx, &pagecontract.RevisionReq{PageID: created.ID})
	if err != nil || len(revs) < 2 {
		t.Fatalf("修订列表异常: %v（%d 条）", err, len(revs))
	}
	firstVersion := revs[len(revs)-1].Version

	gin.SetMode(gin.TestMode)
	handle := pagehttp.NewPagesAdminHandle(svc, nil, nil, nil)
	router := gin.New()
	router.POST("/workbench/history/restore", handle.HistoryRestore)
	form := url.Values{"pageId": {created.ID}, "version": {fmt.Sprint(firstVersion)}}
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/workbench/history/restore", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("恢复失败，状态码 %d：%s", recorder.Code, recorder.Body.String())
	}

	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: created.ID})
	if err != nil {
		t.Fatalf("查询页面失败: %v", err)
	}
	if strings.Contains(string(detail.DraftDocument), "第二版") {
		t.Errorf("恢复后草稿仍是第二版：%s", detail.DraftDocument)
	}
	if detail.DraftVersion <= created.DraftVersion+1 {
		t.Errorf("恢复应产生新修订（覆盖保存），实际版本 %d", detail.DraftVersion)
	}
}
