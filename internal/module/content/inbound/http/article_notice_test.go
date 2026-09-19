package contenthttp

// article_notice_test.go — 文章列表页 ?done= 的受控出口回归。

// 为什么必须有：批量删除的结论带计数（「已删除 3 篇文章。」），列表页此前是
// `data["Done"] = strings.TrimSpace(c.Query("done"))` 原样进模板 —— 手拼一个
// /admin/articles?done=任意文案 就能往页面上塞一条顶着「成功」样式的伪造消息。

// 收口之后最容易静默出错的不是「伪造能进来」，而是**写侧改了措辞（或换了 %d 个数）读侧就再也
// 认不出来**：那时页面上的成功提示会整体消失（未命中落空串），既没有报错也没有日志。
// 所以这里按写侧的四个分支穷举：写侧能产出多少种结论，读侧就要认多少种。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// articleBulkCtx 纯函数用例用的上下文：批量结论文案的模板要按当前语言取
// （articleBulkTextOf），取词必须带 c。本包单跑时 i18n 未初始化，取词回落到中文原文。
func articleBulkCtx(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/articles", nil)
	return c
}

// TestArticlePageDoneAcceptsEveryWriterShape 写侧四个分支的每一种结局都要被认出来。
func TestArticlePageDoneAcceptsEveryWriterShape(t *testing.T) {
	c := articleBulkCtx(t)
	for _, deleted := range []int{0, 1, 3} {
		for _, skipped := range []int{0, 1, 3} {
			msg := articleBulkDeleteResult(c, deleted, skipped)
			if strings.TrimSpace(msg) == "" {
				t.Fatalf("写侧文案为空：deleted=%d skipped=%d", deleted, skipped)
			}
			if got := articlePageDone(c, msg); got != msg {
				t.Errorf("写侧结论读侧认不出来（页面上会没有提示）：got %q want %q", got, msg)
			}
		}
	}
}

// TestArticlePageDoneRejectsForged 不是写侧产出的取值一律落空串。
//
// 落空串而不是归口文案：成功提示没有「必须说点什么」的语义，
// 在成功的位置上顶一条错误提示比什么都不显示更糟。
func TestArticlePageDoneRejectsForged(t *testing.T) {
	c := articleBulkCtx(t)
	msg := articleBulkDeleteResult(c, 3, 0)
	for _, raw := range []string{
		"",
		"   ",
		"文章已全部删除，干得漂亮",
		"已删除 3 个块。", // 别的模块的受控文案，本页不该认
		"已删除 3 篇。",  // 少了「文章」这个名词
		"<script>alert(1)</script>" + msg,
		msg + "<script>alert(1)</script>",
		strings.Repeat(msg, 100),
	} {
		if got := articlePageDone(c, raw); got != "" {
			t.Errorf("未命中应返回空串，实际 %q（raw=%q）", got, raw)
		}
	}
}

// TestArticleBulkResultTemplatesAreDistinct 四个分支的候选不能互相覆盖。
//
// 归一（数字 → 占位）之后如果两条模板变成同一串，说明它们只差一个计数 ——
// 那「全部失败」就会被当成「部分成功」显示（反之亦然），属于会误导运营的静默错误。
func TestArticleBulkResultTemplatesAreDistinct(t *testing.T) {
	if len(articleBulkResultTemplates) != 4 {
		t.Fatalf("四个分支应当各有一条模板，实际 %d 条", len(articleBulkResultTemplates))
	}
	c := articleBulkCtx(t)
	seen := make(map[string]int, len(articleBulkResultTemplates))
	for i, tpl := range articleBulkResultTemplates {
		// 比较**当前语言**的模板：词条写重了同样要红（中文相同、英文不同也算重）。
		key := strings.TrimSpace(articleBulkTextOf(c, tpl))
		if prev, ok := seen[key]; ok {
			t.Errorf("第 %d 与第 %d 条模板完全相同：%q", prev, i, key)
		}
		seen[key] = i
	}
}
