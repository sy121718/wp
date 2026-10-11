package shell

// bulk_test.go — 批量 id 读取的边界测试（纯逻辑，不碰数据库）。
//
// 这里守的是三个容易静默出错的点：去空后仍要判上限（空串不该占额度）、
// 上限判断发生在**去重之后**（重复提交不该把用户挡在门外）、恰好等于上限时放行。
//
// 另加两组「受控文案」的守卫（本批新增）：超限错误是**带 sentinel 的类型**
//（errors.Is / errors.As 可取到 Count/Max），以及 BulkIDsFacingText 只认类型、
// 不认文本 —— 包装进来的内部上下文不会随文案出去。

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
	// 错误文案要能直接展示给用户：必须说清上限与实际数量。
	if !strings.Contains(err.Error(), strconv.Itoa(bulkids.MaxIDs)) ||
		!strings.Contains(err.Error(), strconv.Itoa(bulkids.MaxIDs+1)) {
		t.Fatalf("错误文案应含上限与实际数量，got %q", err.Error())
	}
	// 「受控」必须是**类型事实**，不是文案形状：errors.Is 命中 sentinel，Count/Max 可取。
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
	// 包装之后判定依然成立（调用方加内部上下文是常见写法，不该把判定打断）。
	if !errors.Is(fmt.Errorf("批量删除失败: %w", err), bulkids.ErrTooMany) {
		t.Fatal("包装后的超限错误仍应命中 sentinel")
	}
}

// 上限按去重后的数量算：同一批里大量重复不该把用户挡在门外。
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

// —— 同值映射守卫已随别名一起删除（task-55） ——
//
// 这里曾用 TestBulkIDsTransitionAliasesShareIdentity 钉住「过渡别名与 pkg/bulkids 的 sentinel /
// 错误类型是同一个东西」：把别名改成新类型、或把 sentinel 换成第二个 errors.New，旧调用方的
// errors.As / errors.Is 会**静默**判不中（超限提示退化成「系统内部错误」，且没有编译错误）。
// 四个别名删除后不再有第二份真值，该断言失去对象；归一化与上限的测试在 pkg/bulkids。
