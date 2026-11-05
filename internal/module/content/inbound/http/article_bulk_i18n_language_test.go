package contenthttp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	contentenums "go_wp/internal/module/content/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
)

var articleBulkInjectedEntries = map[string]map[string]string{
	contentenums.BulkArticleNoneSelected: {"zh-CN": "没有选中任何文章，列表未改动。", "en-US": "EN-NONE"},
	contentenums.BulkArticleAllDeleted:   {"zh-CN": "已删除 %s 篇文章。", "en-US": "EN-ALL %s"},
	contentenums.BulkArticleAllSkipped:   {"zh-CN": "%s 篇文章都未能删除，列表未改动。", "en-US": "EN-SKIP %s"},
	contentenums.BulkArticlePartial:      {"zh-CN": "已删除 %s 篇，%s 篇未能删除（可能已被删除）。", "en-US": "EN-PART %s %s"},
}

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

		msg := response.MsgText(en, articleBulkDeleteResult(tc.deleted, tc.skipped))
		if msg != tc.want {
			t.Errorf("%s：应按当前语言拼装，got %q want %q", tc.name, msg, tc.want)
		}
	}

	e := articleBulkTestEngine(&articleBulkDeleteStub{})
	form := url.Values{"ids": {"a", "b", "c"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/articles/bulk-delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept-Language", "en-US")
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("成功提示状态 %d：%s", rec.Code, rec.Body.String())
	}
	// 成功走统一提示组件：批量结果只播报默认文案，逐条结果属业务数据。
	if body := rec.Body.String(); !strings.Contains(body, "role=\"status\"") {
		t.Errorf("成功应走统一提示组件：%s", body[:min(len(body), 240)])
	}

	zh := articleBulkLangCtx(t, "zh-CN")
	if msg := response.MsgText(zh, articleBulkDeleteResult(3, 0)); msg != "已删除 3 篇文章。" {
		t.Errorf("中文请求应取中文词条，实际 %q", msg)
	}
}
