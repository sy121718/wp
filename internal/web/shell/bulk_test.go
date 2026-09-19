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

	"go_wp/pkg/i18n"
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
	// 「受控」必须是**类型事实**，不是文案形状：errors.Is 命中 sentinel，Count/Max 可取。
	if !errors.Is(err, ErrBulkIDsTooMany) {
		t.Fatalf("超限错误应命中 sentinel ErrBulkIDsTooMany，got %v", err)
	}
	var typed *BulkIDsError
	if !errors.As(err, &typed) {
		t.Fatalf("超限错误应是 *BulkIDsError，got %T", err)
	}
	if typed.Max != MaxBulkIDs || typed.Count != MaxBulkIDs+1 {
		t.Fatalf("类型应带 Max=%d / Count=%d，got Max=%d / Count=%d",
			MaxBulkIDs, MaxBulkIDs+1, typed.Max, typed.Count)
	}
	// 包装之后判定依然成立（调用方加内部上下文是常见写法，不该把判定打断）。
	if !errors.Is(fmt.Errorf("批量删除失败: %w", err), ErrBulkIDsTooMany) {
		t.Fatal("包装后的超限错误仍应命中 sentinel")
	}
}

// TestBulkIDsFacingTextUsesTypedCounts 命中 sentinel 时给出的文案含**两个数字**（上限与本次条数），
// 且不含任何内部细节指纹（sentinel 的英文诊断文本、类型名都不许出现）。
func TestBulkIDsFacingTextUsesTypedCounts(t *testing.T) {
	_, err := BulkIDs(bulkCtx(t, seq(MaxBulkIDs+1)...))
	if err == nil {
		t.Fatal("准备条件失败：应触发上限")
	}
	got := BulkIDsFacingText(bulkCtx(t), err)
	if !strings.Contains(got, "一次最多操作") {
		t.Fatalf("应给出受控提示，got %q", got)
	}
	if !strings.Contains(got, strconv.Itoa(MaxBulkIDs)) ||
		!strings.Contains(got, strconv.Itoa(MaxBulkIDs+1)) {
		t.Fatalf("受控文案应含上限与本次条数（当前 N 项由类型给回），got %q", got)
	}
	for _, tok := range []string{"shell:", "bulk ids exceeded", "BulkIDsError"} {
		if strings.Contains(got, tok) {
			t.Fatalf("受控文案里出现了错误原文/类型名 %q：%q", tok, got)
		}
	}
}

// TestBulkIDsFacingTextIgnoresWrappedContext 出口只认**类型**，不认文本：
// 调用方把内部原文（表名 / SQLSTATE）包进同一个错误链时，一个字都不许跟着出去。
func TestBulkIDsFacingTextIgnoresWrappedContext(t *testing.T) {
	wrapped := fmt.Errorf("加载货源失败: SQLSTATE 42P01 relation `inventory_sources`: %w",
		&BulkIDsError{Count: MaxBulkIDs + 1, Max: MaxBulkIDs})
	got := BulkIDsFacingText(bulkCtx(t), wrapped)
	if !strings.Contains(got, strconv.Itoa(MaxBulkIDs+1)) {
		t.Fatalf("受控文案应取类型里的 Count，got %q", got)
	}
	for _, tok := range []string{"SQLSTATE", "42P01", "inventory_sources", "加载货源失败"} {
		if strings.Contains(got, tok) {
			t.Fatalf("包装进来的内部上下文 %q 不许出现在文案里：%q", tok, got)
		}
	}
}

// TestBulkIDsFacingTextFallsBackForUnknownError 未命中 sentinel 一律回落归口文案，
// 且原文只进日志 —— 这是「入口对未知错误也接得住」的那一半契约。
func TestBulkIDsFacingTextFallsBackForUnknownError(t *testing.T) {
	got := BulkIDsFacingText(bulkCtx(t), errors.New("ERROR: relation `orders` does not exist (SQLSTATE 42P01)"))
	for _, tok := range []string{"SQLSTATE", "42P01", "relation", "orders"} {
		if strings.Contains(got, tok) {
			t.Fatalf("未命中的错误原文不许出现在文案里（%q）：%q", tok, got)
		}
	}
	// 归口文案可能是 key 本身（i18n 未加载）或译文，两者都算命中。
	if got != MsgInternalError && !strings.Contains(got, "内部错误") {
		t.Fatalf("未命中应回落 MsgInternalError（或其译文），got %q", got)
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

// TestBulkIDsFacingTextFollowsRequestLanguage 命中 sentinel 时按**当前语言**给文案。
//
// 只说「受控」不够：受控文案还必须是可翻译的 —— 否则中文原文会原样出现在英文界面上。
// 用 i18n.InjectForTest 注入词条（生产由迁移 277 seed 进 sys_i18n），因此不依赖数据库；
// 语言走 query `lang`（response.requestLanguage 取值链里的一环）。
func TestBulkIDsFacingTextFollowsRequestLanguage(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		MsgBulkIDsTooMany: {
			"zh-CN": bulkIDsTooManyTemplate,
			"en-US": "At most %s items can be operated at once; %s selected, please split into batches.",
		},
	}, nil)

	form := url.Values{}
	for i := 0; i <= MaxBulkIDs; i++ {
		form.Add("ids", "id-"+strconv.Itoa(i))
	}
	langCtx := func(lang string) *gin.Context {
		req := httptest.NewRequest(http.MethodPost, "/bulk?lang="+lang, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = req
		return c
	}
	_, err := BulkIDs(langCtx("zh-CN"))
	if err == nil {
		t.Fatal("准备条件失败：应触发上限")
	}

	zh := BulkIDsFacingText(langCtx("zh-CN"), err)
	if !strings.Contains(zh, "一次最多操作") {
		t.Fatalf("中文请求应给中文文案，got %q", zh)
	}
	en := BulkIDsFacingText(langCtx("en-US"), err)
	if !strings.Contains(en, "At most") {
		t.Fatalf("英文请求应给英文文案（词条缺失时会退回中文兜底），got %q", en)
	}
	for _, want := range []string{strconv.Itoa(MaxBulkIDs), strconv.Itoa(MaxBulkIDs + 1)} {
		if !strings.Contains(en, want) {
			t.Fatalf("英文文案也应带上限与本次条数 %s，got %q", want, en)
		}
	}
}
