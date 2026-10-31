package shell

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	bulkids "go_wp/pkg/bulkids"
)

func bulkCtx(t *testing.T, ids ...string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	form := url.Values{}
	for _, id := range ids {
		form.Add("ids", id)
	}
	req := httptest.NewRequest(http.MethodPost, "/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

func seq(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, "id-"+strconv.Itoa(i))
	}
	return out
}

func TestBulkIDs_ReadsInOrder(t *testing.T) {
	got, err := BulkIDs(bulkCtx(t, "a", "b", "c"))
	if err != nil {
		t.Fatalf("正常输入不应报错: %v", err)
	}
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("应保持首次出现顺序，got %v", got)
	}
}

func TestBulkIDs_TrimsAndDropsBlank(t *testing.T) {
	got, err := BulkIDs(bulkCtx(t, " a ", "", "   ", "b"))
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if strings.Join(got, ",") != "a,b" {
		t.Fatalf("应去空白并丢弃空串，got %v", got)
	}
}

func TestBulkIDs_DedupesKeepingFirstOrder(t *testing.T) {
	got, err := BulkIDs(bulkCtx(t, "a", "b", "a", "c", "b"))
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("应去重且保持首次出现顺序，got %v", got)
	}
}

func TestBulkIDs_EmptyIsNilNotError(t *testing.T) {
	got, err := BulkIDs(bulkCtx(t))
	if err != nil {
		t.Fatalf("空输入不应是错误（未选择由调用方自行处理）: %v", err)
	}
	if got != nil {
		t.Fatalf("空输入应返回 nil，got %v", got)
	}
}

func TestBulkIDs_ExactlyAtLimitPasses(t *testing.T) {
	got, err := BulkIDs(bulkCtx(t, seq(bulkids.MaxIDs)...))
	if err != nil {
		t.Fatalf("恰好 %d 项应放行: %v", bulkids.MaxIDs, err)
	}
	if len(got) != bulkids.MaxIDs {
		t.Fatalf("应返回 %d 项，got %d", bulkids.MaxIDs, len(got))
	}
}

func TestBulkIDs_OverLimitRejectsWholeBatch(t *testing.T) {
	got, err := BulkIDs(bulkCtx(t, seq(bulkids.MaxIDs+1)...))
	if err == nil {
		t.Fatal("超过上限必须报错（整批拒绝，不截断）")
	}
	if got != nil {
		t.Fatalf("拒绝时不应返回部分 id（那会诱导调用方部分执行），got %d 项", len(got))
	}

	if !strings.Contains(err.Error(), strconv.Itoa(bulkids.MaxIDs)) ||
		!strings.Contains(err.Error(), strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("错误文案应含上限与实际数量，got %q", err.Error())
	}

	if !errors.Is(err, bulkids.ErrTooMany) {
		t.Fatalf("超限错误应命中 sentinel bulkids.ErrTooMany，got %v", err)
	}
	var typed *bulkids.IDsError
	if !errors.As(err, &typed) {
		t.Fatalf("超限错误应是 *bulkids.IDsError，got %T", err)
	}
	if typed.Max != bulkids.MaxIDs || typed.Count != bulkids.MaxIDs+1 {
		t.Fatalf("类型应带 Max=%d / Count=%d，got Max=%d / Count=%d",
			bulkids.MaxIDs, bulkids.MaxIDs+1, typed.Max, typed.Count)
	}

	if !errors.Is(fmt.Errorf("批量删除失败: %w", err), bulkids.ErrTooMany) {
		t.Fatal("包装后的超限错误仍应命中 sentinel")
	}
}

func TestBulkIDs_LimitAppliesAfterDedup(t *testing.T) {
	ids := make([]string, 0, bulkids.MaxIDs*2)
	for i := 0; i < bulkids.MaxIDs*2; i++ {
		ids = append(ids, "id-"+strconv.Itoa(i%bulkids.MaxIDs))
	}
	got, err := BulkIDs(bulkCtx(t, ids...))
	if err != nil {
		t.Fatalf("去重后未超限，不应报错: %v", err)
	}
	if len(got) != bulkids.MaxIDs {
		t.Fatalf("去重后应剩 %d 项，got %d", bulkids.MaxIDs, len(got))
	}
}
