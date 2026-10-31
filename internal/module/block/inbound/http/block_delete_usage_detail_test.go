package blockhttp

import (
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	blockenums "go_wp/internal/module/block/enums"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/i18n"
)

func deleteResp(t *testing.T, svc blockcontract.BlockService, body string) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/block/delete", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c.Request = req

	(&Handle{svc: svc}).Delete(c)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(w.Body.String()), &parsed); err != nil {
		t.Fatalf("响应不是合法 JSON：status=%d body=%q err=%v", w.Code, w.Body.String(), err)
	}
	return w.Code, parsed
}

func TestDeleteKeepsUsageDetailInJSON(t *testing.T) {

	i18n.InjectForTest(map[string]map[string]string{
		blockenums.ErrBlockInUse:             {"zh-CN": "该区块仍被页面引用，不能删除。"},
		blockenums.MsgBlockUsagePageDocument: {"zh-CN": "页面文档"},
		blockenums.UsageMore:                 {"zh-CN": " 等 {count} 处"},
	}, nil)

	svc := &bulkDeleteStubService{
		fail: map[string]error{
			"b1": blockcontract.NewBlockInUseError([]blockcontract.BlockUsage{
				{Kind: blockcontract.UsageKindPageDocument, EntityID: "page-42", Label: "/pricing", Detail: "main"},
			}),
		},
	}
	status, parsed := deleteResp(t, svc, `{"id":"b1"}`)
	if status != http.StatusConflict {
		t.Fatalf("被引用应回 409，实际 %d（body=%v）", status, parsed)
	}
	msg, _ := parsed["message"].(string)
	if !strings.Contains(msg, "/pricing") {
		t.Errorf("删除被拒的提示必须能定位到具体引用（/pricing），实际 %q", msg)
	}
	if !strings.Contains(msg, "页面文档") {
		t.Errorf("引用类别应是当前语言的文案（页面文档），实际 %q", msg)
	}
	if strings.Contains(msg, "page_document") {
		t.Errorf("提示里出现了内部枚举原文（page_document）—— 说明走的是 err.Error() 而不是本地化明细：%q", msg)
	}
	if strings.Contains(msg, "block.err.inUse") {
		t.Errorf("提示里出现了裸 key，前端会原样显示：%q", msg)
	}
}

func TestDeleteBusinessErrorWithoutUsageKeepsPlainMessage(t *testing.T) {
	svc := &bulkDeleteStubService{fail: map[string]error{"b1": blockcontract.ErrNotFound}}
	status, parsed := deleteResp(t, svc, `{"id":"b1"}`)
	if status != http.StatusNotFound {
		t.Fatalf("不存在应回 404，实际 %d（body=%v）", status, parsed)
	}
	msg, _ := parsed["message"].(string)
	if msg != blockenums.ErrBlockNotFound {
		t.Fatalf("无引用明细时不应拼接别的后缀（无词条时的回退就是 key 本身），实际 %q", msg)
	}
	if strings.Contains(msg, "：") || strings.Contains(msg, " /") {
		t.Fatalf("无明细的提示里出现了定位后缀：%q", msg)
	}
}

func TestDeleteInternalErrorDoesNotLeakDriverText(t *testing.T) {
	svc := &bulkDeleteStubService{
		fail: map[string]error{"b1": errors.New(`pq: relation "blocks" does not exist`)},
	}
	status, parsed := deleteResp(t, svc, `{"id":"b1"}`)
	if status != http.StatusInternalServerError {
		t.Fatalf("未知错误应回 500，实际 %d", status)
	}
	if msg, _ := parsed["message"].(string); strings.Contains(msg, "pq:") || strings.Contains(msg, "relation") {
		t.Fatalf("500 响应泄露了驱动原文：%q", msg)
	}
}
