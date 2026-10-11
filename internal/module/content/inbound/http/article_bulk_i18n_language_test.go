package contenthttp

// article_bulk_i18n_language_test.go — 批量结论文案的**当前语言一致性**回归。
//
// 为什么必须有这一条：写侧（articleBulkDeleteResult）只产出**事实**（key + 中文兜底 +
// `%s` 参数），取词发生在渲染期（提示页用注入的 tf）—— 只要有人把兜底写死成中文常量
// 并当成成品串直接输出（或忘了带 c），中文环境下一切正常，英文页面上运营看到的却是中文。
// 所以断言分两半：事实经 adminui.MsgText(当前语言) 取词正确，以及提示页正文照此渲染。
// 用 pkg/i18n.InjectForTest 直接塞内存词条，不建库、不起装配。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	contentenums "go_wp/internal/module/content/enums"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
)

// articleBulkInjectedEntries 注入的词条（中英都注：只注英文会被 cache 的
// 「当前语言没有就遍历所有可用语言」兜底顶掉，中文断言就失去意义）。
var articleBulkInjectedEntries = map[string]map[string]string{
	contentenums.BulkArticleNoneSelected: {"zh-CN": "没有选中任何文章，列表未改动。", "en-US": "EN-NONE"},
	contentenums.BulkArticleAllDeleted:   {"zh-CN": "已删除 %s 篇文章。", "en-US": "EN-ALL %s"},
	contentenums.BulkArticleAllSkipped:   {"zh-CN": "%s 篇文章都未能删除，列表未改动。", "en-US": "EN-SKIP %s"},
	contentenums.BulkArticlePartial:      {"zh-CN": "已删除 %s 篇，%s 篇未能删除（可能已被删除）。", "en-US": "EN-PART %s %s"},
}

// articleBulkLangCtx 构造一个绑定了 Accept-Language 的上下文。
func articleBulkLangCtx(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/articles", nil)
	if lang != "" {
		c.Request.Header.Set("Accept-Language", lang)
	}
	return c
}

// TestArticleBulkNoticeFollowsRequestLanguage 四个分支都随当前语言，且提示页照此渲染。
func TestArticleBulkNoticeFollowsRequestLanguage(t *testing.T) {
	i18n.InjectForTest(articleBulkInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	en := articleBulkLangCtx(t, "en-US")
	cases := []struct {
		name    string
		deleted int
		skipped int
		want    string
	}{
		{"未选中", 0, 0, "EN-NONE"},
		{"全成功", 3, 0, "EN-ALL 3"},
		{"全部失败", 0, 3, "EN-SKIP 3"},
		{"部分成功", 2, 3, "EN-PART 2 3"},
	}
	for _, tc := range cases {
		// 断言的是「事实 + 渲染期取词」的合成结果：articleBulkDeleteResult 给 key 与参数，
		// adminui.MsgText 按当前语言翻（提示页的 tmpl 走同一实现）。
		msg := adminui.MsgText(en, articleBulkDeleteResult(tc.deleted, tc.skipped))
		if msg != tc.want {
			t.Errorf("%s：应按当前语言拼装，got %q want %q", tc.name, msg, tc.want)
		}
	}

	// 提示页正文也随当前语言（不是只有纯函数取词对）：直接打 handler。
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.HTMLRender = templates.NewJetHTMLRender(filepath.Join("..", "..", "..", "..", "templates"), true)
	e.POST("/admin/articles/bulk-delete", (&articlePageHandle{contents: &articleBulkDeleteStub{}}).ArticlesBulkDelete)
	form := url.Values{"ids": {"a", "b", "c"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/articles/bulk-delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept-Language", "en-US")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("提示页状态 %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "EN-ALL 3") {
		t.Errorf("提示页正文未按当前语言渲染：%s", body[:min(len(body), 240)])
	}

	zh := articleBulkLangCtx(t, "zh-CN")
	if msg := adminui.MsgText(zh, articleBulkDeleteResult(3, 0)); msg != "已删除 3 篇文章。" {
		t.Errorf("中文请求应取中文词条，实际 %q", msg)
	}
}
