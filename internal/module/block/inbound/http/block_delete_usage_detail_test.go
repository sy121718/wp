package blockhttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	blockcontract "go_wp/internal/module/block/contract"
	blockenums "go_wp/internal/module/block/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
)

// deleteEngine 复刻生产接线：失败中间件、请求取词与 HTML 渲染都必须在场。
func deleteEngine(svc blockcontract.BlockService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	r.Use(pageview.FailureMiddleware(pageview.RenderFailure))
	r.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	r.Use(func(c *gin.Context) {
		text.AttachScope(c)
		c.Next()
	})
	r.POST("/api/block/delete", pageview.HTMLFragment, (&Handle{svc: svc}).Delete)
	return r
}

func deleteResp(t *testing.T, svc blockcontract.BlockService, form url.Values) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/block/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deleteEngine(svc).ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("界面响应必须是 HTML，实际 %q，正文：%s", rec.Header().Get("Content-Type"), body)
	}
	return rec.Code, body
}

func TestDeleteRejectionShowsMessageAndLocatableUsage(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		blockenums.ErrBlockInUse:             {"zh-CN": "该区块仍被页面引用，不能删除。"},
		blockenums.MsgBlockUsagePageDocument: {"zh-CN": "页面文档"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	svc := &bulkDeleteStubService{
		fail: map[string]error{
			"b1": blockcontract.NewBlockInUseError([]blockcontract.BlockUsage{
				{Kind: blockcontract.UsageKindPageDocument, EntityID: "page-42", Label: "/pricing", Detail: "main"},
			}),
		},
	}
	status, body := deleteResp(t, svc, url.Values{"id": {"b1"}})
	if status != http.StatusConflict {
		t.Fatalf("被引用应回 409，实际 %d（%s）", status, firstBlockN(body, 400))
	}
	if !strings.Contains(body, "该区块仍被页面引用") {
		t.Errorf("失败提示应只给一句「为什么不行」，实际 %s", firstBlockN(body, 600))
	}
	// 主消息之外，逐条引用必须可定位：HTML 响应没有另一份 JSON data，
	// 只给一句「被引用」等于让用户无从知道被谁引用。
	if !strings.Contains(body, "/pricing") || !strings.Contains(body, "页面文档") {
		t.Errorf("引用明细应可定位（位置 + 用途），实际 %s", firstBlockN(body, 600))
	}
	if strings.Contains(body, "page_document") {
		t.Errorf("提示里出现了内部枚举原文（page_document）：%s", firstBlockN(body, 600))
	}
	if strings.Contains(body, blockenums.ErrBlockInUse) {
		t.Errorf("提示里出现了裸 key，前端会原样显示：%s", firstBlockN(body, 600))
	}
}

func TestDeleteBusinessErrorWithoutUsageKeepsPlainMessage(t *testing.T) {
	svc := &bulkDeleteStubService{fail: map[string]error{"b1": blockcontract.ErrNotFound}}
	status, body := deleteResp(t, svc, url.Values{"id": {"b1"}})
	if status != http.StatusNotFound {
		t.Fatalf("不存在应回 404，实际 %d（%s）", status, firstBlockN(body, 400))
	}
	if !strings.Contains(body, "data-ui-error") {
		t.Fatalf("失败应渲染错误片段，实际 %s", firstBlockN(body, 400))
	}
	if strings.Contains(body, "：") {
		t.Fatalf("无引用明细的提示里出现了定位后缀：%s", firstBlockN(body, 400))
	}
}

func TestDeleteInternalErrorDoesNotLeakDriverText(t *testing.T) {
	svc := &bulkDeleteStubService{
		fail: map[string]error{"b1": errors.New(`pq: relation "blocks" does not exist`)},
	}
	status, body := deleteResp(t, svc, url.Values{"id": {"b1"}})
	if status != http.StatusInternalServerError {
		t.Fatalf("未知错误应回 500，实际 %d（%s）", status, firstBlockN(body, 400))
	}
	if strings.Contains(body, "pq:") || strings.Contains(body, "relation") {
		t.Fatalf("500 响应泄露了驱动原文：%s", firstBlockN(body, 400))
	}
}
