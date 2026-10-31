package contenthttp

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

		msg := adminui.MsgText(en, articleBulkDeleteResult(tc.deleted, tc.skipped))
		if msg != tc.want {
			t.Errorf("%s：应按当前语言拼装，got %q want %q", tc.name, msg, tc.want)
		}
	}

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
