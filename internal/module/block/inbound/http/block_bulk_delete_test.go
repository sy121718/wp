package blockhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	"go_wp/internal/templates"
	"go_wp/internal/text"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
)

type bulkDeleteStubService struct {
	fail  map[string]error
	calls []string
}

func (s *bulkDeleteStubService) Delete(_ context.Context, req *blockdto.DeleteReq) error {
	s.calls = append(s.calls, req.ID)
	if err, ok := s.fail[req.ID]; ok {
		return err
	}
	return nil
}

func (s *bulkDeleteStubService) List(context.Context, *blockdto.ListReq) ([]blockdto.BlockResp, error) {
	panic("BulkDelete 不应调用 List")
}

func (s *bulkDeleteStubService) Detail(context.Context, *blockdto.DetailReq) (*blockdto.BlockResp, error) {
	panic("BulkDelete 不应调用 Detail")
}

func (s *bulkDeleteStubService) Create(context.Context, *blockdto.CreateReq) (*blockdto.BlockResp, error) {
	panic("BulkDelete 不应调用 Create")
}

func (s *bulkDeleteStubService) Update(context.Context, *blockdto.UpdateReq) (*blockdto.BlockResp, error) {
	panic("BulkDelete 不应调用 Update")
}

func (s *bulkDeleteStubService) CloneAST(context.Context, *blockdto.CloneReq) (*blockdto.CloneResp, error) {
	panic("BulkDelete 不应调用 CloneAST")
}

func (s *bulkDeleteStubService) ListBlockSourceRefs(context.Context, string) ([]blockcontract.BlockUsage, error) {
	panic("BulkDelete 不应调用 ListBlockSourceRefs")
}

// bulkDeleteEngine 复刻生产接线：失败中间件、请求取词与 HTML 渲染都必须在场，
// 否则失败会落成 JSON 或空响应，测出来的不是真实行为。
func bulkDeleteEngine(svc blockcontract.BlockService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	r.Use(pageview.FailureMiddleware(adminui.RenderFailure))
	r.Use(pageview.UseSuccess(adminui.NewSuccessWriter()))
	r.Use(func(c *gin.Context) {
		text.AttachScope(c)
		c.Next()
	})
	r.POST("/api/block/bulk-delete", pageview.HTMLFragment, (&Handle{svc: svc}).BulkDelete)
	return r
}

func bulkDeleteResp(t *testing.T, svc blockcontract.BlockService, form url.Values) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/block/bulk-delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	bulkDeleteEngine(svc).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("界面响应必须是 HTML，实际 %q，正文：%s", rec.Header().Get("Content-Type"), body)
	}
	return rec.Code, body
}

func TestBulkDeleteBindFailIsControlledHTML(t *testing.T) {
	// 表单编码下只剩「完全没有 ids」这一种缺形状；空白项由 Normalize 过滤，不算绑定失败。
	svc := &bulkDeleteStubService{}
	status, body := bulkDeleteResp(t, svc, url.Values{})

	if status != http.StatusBadRequest {
		t.Fatalf("绑定失败应为 400，实际 %d", status)
	}
	if !strings.Contains(body, "data-ui-error") {
		t.Fatalf("失败应渲染错误片段，实际 %s", firstBlockN(body, 400))
	}
	for _, leak := range []string{"json:", "cannot unmarshal", "Go struct", "binding", "reflect", "dto.BulkDeleteForm"} {
		if strings.Contains(body, leak) {
			t.Fatalf("绑定错误原文泄漏到响应里（%q 出现在 %s）", leak, firstBlockN(body, 400))
		}
	}
	if len(svc.calls) != 0 {
		t.Fatalf("参数都没绑定成功就不该删任何东西，实际调用 %v", svc.calls)
	}
}

func TestBulkDeleteOverLimitRejectsWholeBatch(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		"shell.err.bulkIdsTooMany": {
			"zh-CN": "一次最多操作 %s 项，当前 %s 项，请分批进行",
			"en-US": "At most %s items can be operated at once; %s selected, please split into batches.",
		},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	const over = 201
	ids := make([]string, 0, over)
	for i := 0; i < over; i++ {
		ids = append(ids, "b-"+strconv.Itoa(i))
	}
	svc := &bulkDeleteStubService{}
	status, body := bulkDeleteResp(t, svc, url.Values{"ids": ids})

	if status != http.StatusBadRequest {
		t.Fatalf("超限应为 400（整批拒绝），实际 %d", status)
	}
	if !strings.Contains(body, "200") || !strings.Contains(body, "201") {
		t.Fatalf("超限文案应带上限与本次条数（200 / 201），实际 %s", firstBlockN(body, 400))
	}
	if strings.Contains(body, "shell.err.bulkIdsTooMany") {
		t.Fatalf("词条没取到，前端会看到裸 key：%s", firstBlockN(body, 400))
	}
	if len(svc.calls) != 0 {
		t.Fatalf("超限必须整批拒绝（不静默截断执行），实际删了 %d 条", len(svc.calls))
	}
}

func TestBulkDeleteDedupesBeforeDelete(t *testing.T) {
	svc := &bulkDeleteStubService{}
	status, body := bulkDeleteResp(t, svc, url.Values{"ids": {" b1 ", "b1", "  ", "b2", "b1"}})

	if status != http.StatusOK {
		t.Fatalf("部分重复不是错误，应 200，实际 %d", status)
	}
	if len(svc.calls) != 2 || svc.calls[0] != "b1" || svc.calls[1] != "b2" {
		t.Fatalf("应去重保序后逐条删除，实际调用 %v", svc.calls)
	}
	if !strings.Contains(body, "已删除 2 个块") && !strings.Contains(body, "Deleted 2 blocks") {
		t.Fatalf("计数应如实报告 2 个，实际 %s", firstBlockN(body, 400))
	}
}

func TestBulkDeletePartialFailureKeepsGoing(t *testing.T) {
	inUse := blockcontract.NewBlockInUseError([]blockcontract.BlockUsage{
		{Kind: blockcontract.UsageKindPageDocument, EntityID: "page-42", Label: "/pricing", Detail: "main"},
	})
	svc := &bulkDeleteStubService{fail: map[string]error{"b2": inUse}}
	status, body := bulkDeleteResp(t, svc, url.Values{"ids": {"b1", "b2", "b3"}})

	if status != http.StatusOK {
		t.Fatalf("部分成功应 200（不伪装成整批失败），实际 %d", status)
	}
	if len(svc.calls) != 3 {
		t.Fatalf("单条失败不得中断整批：三条都应尝试，实际 %v", svc.calls)
	}
	if !strings.Contains(body, "b2") || !strings.Contains(body, "/pricing") {
		t.Fatalf("失败行必须带 id 与引用明细（运营据此去解除引用），实际 %s", firstBlockN(body, 600))
	}
	if !strings.Contains(body, "另有 1 个被跳过") && !strings.Contains(body, "1 more were skipped") {
		t.Fatalf("计数应如实报告跳过 1 个，实际 %s", firstBlockN(body, 600))
	}
}

func TestBulkDeleteAllBlankSucceedsEmpty(t *testing.T) {
	svc := &bulkDeleteStubService{}
	status, body := bulkDeleteResp(t, svc, url.Values{"ids": {"   ", " \t "}})

	if status != http.StatusOK {
		t.Fatalf("全空白不是错误，应 200（前端据此说「没有选中任何块」），实际 %d", status)
	}
	if len(svc.calls) != 0 {
		t.Fatalf("没有有效 id 不该调用 service，实际 %v", svc.calls)
	}
	if !strings.Contains(body, "没有选中任何块") {
		t.Fatalf("空勾选要说清列表未改动，实际 %s", firstBlockN(body, 400))
	}
}

func TestBulkDeleteUnknownErrorStillControlledHTML(t *testing.T) {
	svc := &bulkDeleteStubService{fail: map[string]error{"b1": errors.New(`pq: relation "blocks" does not exist`)}}
	status, body := bulkDeleteResp(t, svc, url.Values{"ids": {"b1"}})

	if status != http.StatusOK {
		t.Fatalf("单条未知错误仍应 200 + 逐条结果，实际 %d", status)
	}
	if strings.Contains(body, "pq:") || strings.Contains(body, `relation "blocks"`) {
		t.Fatalf("驱动原文泄漏到逐条原因里：%s", firstBlockN(body, 400))
	}
}

func firstBlockN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
