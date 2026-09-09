package feature

// history_restore_test.go — 修订历史「恢复」的服务端编排（HTMX 化，docs/09 §3）。
//
// 恢复 = 查修订 → 以当前 draftVersion 覆盖保存草稿（乐观锁）；
// 客户端随后整页刷新重新注入文档。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"

	"github.com/gin-gonic/gin"
)

// TestHistoryRestoreOverwritesDraft 恢复到旧版本后草稿内容回到该版本。
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
	firstVersion := revs[len(revs)-1].Version // 列表按版本升序，最后一条为最早版本

	gin.SetMode(gin.TestMode)
	handle := dashboardhttp.NewHandle(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
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

	detail, err := svc.Detail(ctx, &pagedto.DetailReq{ID: created.ID})
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
