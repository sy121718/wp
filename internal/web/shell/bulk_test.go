package shell

// bulk_test.go — 批量 id 读取的边界测试（纯逻辑，不碰数据库）。
//
// 这里守的是三个容易静默出错的点：去空后仍要判上限（空串不该占额度）、
// 上限判断发生在**去重之后**（重复提交不该把用户挡在门外）、恰好等于上限时放行。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// bulkCtx 造一个带表单域 ids 的 gin 上下文。
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

// seq 造 n 个互不相同的 id。
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

// 恰好等于上限必须放行（边界值，off-by-one 最容易在这里出错）。
func TestBulkIDs_ExactlyAtLimitPasses(t *testing.T) {
	got, err := BulkIDs(bulkCtx(t, seq(MaxBulkIDs)...))
	if err != nil {
		t.Fatalf("恰好 %d 项应放行: %v", MaxBulkIDs, err)
	}
	if len(got) != MaxBulkIDs {
		t.Fatalf("应返回 %d 项，got %d", MaxBulkIDs, len(got))
	}
}

func TestBulkIDs_OverLimitRejectsWholeBatch(t *testing.T) {
	got, err := BulkIDs(bulkCtx(t, seq(MaxBulkIDs+1)...))
	if err == nil {
		t.Fatal("超过上限必须报错（整批拒绝，不截断）")
	}
	if got != nil {
		t.Fatalf("拒绝时不应返回部分 id（那会诱导调用方部分执行），got %d 项", len(got))
	}
	// 错误文案要能直接展示给用户：必须说清上限与实际数量。
	if !strings.Contains(err.Error(), strconv.Itoa(MaxBulkIDs)) ||
		!strings.Contains(err.Error(), strconv.Itoa(MaxBulkIDs+1)) {
		t.Fatalf("错误文案应含上限与实际数量，got %q", err.Error())
	}
}

// 上限按去重后的数量算：同一批里大量重复不该把用户挡在门外。
func TestBulkIDs_LimitAppliesAfterDedup(t *testing.T) {
	ids := make([]string, 0, MaxBulkIDs*2)
	for i := 0; i < MaxBulkIDs*2; i++ {
		ids = append(ids, "id-"+strconv.Itoa(i%MaxBulkIDs))
	}
	got, err := BulkIDs(bulkCtx(t, ids...))
	if err != nil {
		t.Fatalf("去重后未超限，不应报错: %v", err)
	}
	if len(got) != MaxBulkIDs {
		t.Fatalf("去重后应剩 %d 项，got %d", MaxBulkIDs, len(got))
	}
}
