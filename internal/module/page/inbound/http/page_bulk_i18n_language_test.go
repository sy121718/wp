package pagehttp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	pageenums "go_wp/internal/module/page/enums"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

var pageBulkInjectedEntries = map[string]map[string]string{
	pageenums.BulkPageNoneSelected:     {"zh-CN": "没有勾选任何页面，列表未改动。", "en-US": "EN-NONE"},
	pageenums.BulkPageAllDeleted:       {"zh-CN": "已删除 %s 个页面。", "en-US": "EN-ALL %s"},
	pageenums.BulkPageAllSkipped:       {"zh-CN": "%s 个页面都未能删除，列表未改动。", "en-US": "EN-SKIP %s"},
	pageenums.BulkPagePartial:          {"zh-CN": "已删除 %s 个，%s 个未能删除（可能已被删除或路径清理失败）。", "en-US": "EN-PART %s %s"},
	pageenums.BulkPageMissingID:        {"zh-CN": "缺少页面 id，未执行删除。", "en-US": "EN-MISSING-ID"},
	pageenums.BulkRedirectNoneSelected: {"zh-CN": "没有勾选任何重定向，列表未改动。", "en-US": "EN-R-NONE"},
	pageenums.BulkRedirectAllDeleted:   {"zh-CN": "已删除 %s 条重定向。", "en-US": "EN-R-ALL %s"},
	pageenums.BulkRedirectAllSkipped:   {"zh-CN": "%s 条重定向都未能删除，列表未改动。", "en-US": "EN-R-SKIP %s"},
	pageenums.BulkRedirectPartial:      {"zh-CN": "已删除 %s 条，%s 条未能删除（可能已不存在或访问面不可用）。", "en-US": "EN-R-PART %s %s"},
}

func pageBulkLangCtx(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/pages", nil)
	if lang != "" {
		c.Request.Header.Set("Accept-Language", lang)
	}
	return c
}

func TestPageBulkNoticeFollowsRequestLanguage(t *testing.T) {
	i18n.InjectForTest(pageBulkInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	en := pageBulkLangCtx(t, "en-US")
	cases := []struct {
		name    string
		deleted int
		skipped int
		want    string
	}{
		{"未勾选", 0, 0, "EN-NONE"},
		{"全成功", 3, 0, "EN-ALL 3"},
		{"全部失败", 0, 3, "EN-SKIP 3"},
		{"部分成功", 2, 3, "EN-PART 2 3"},
	}
	for _, tc := range cases {
		msg := pageMsgText(en, outcomeMsg(pageenums.PagesBulkDeleteOutcome(tc.deleted, tc.skipped)))
		if msg != tc.want {
			t.Errorf("页面列表「%s」应按当前语言拼装，got %q want %q", tc.name, msg, tc.want)
		}
	}

	if msg := pageMsgText(en, pagesLocalNoticeMissingID); msg != "EN-MISSING-ID" {
		t.Errorf("缺 id 提示应按当前语言取词，实际 %q", msg)
	}

	redirectCases := []struct {
		name    string
		deleted int
		skipped int
		want    string
	}{
		{"未勾选", 0, 0, "EN-R-NONE"},
		{"全成功", 3, 0, "EN-R-ALL 3"},
		{"全部失败", 0, 3, "EN-R-SKIP 3"},
		{"部分成功", 2, 3, "EN-R-PART 2 3"},
	}
	for _, tc := range redirectCases {
		if got := pageMsgText(en, outcomeMsg(pageenums.RedirectBulkDeleteOutcome(tc.deleted, tc.skipped))); got != tc.want {
			t.Errorf("重定向「%s」应按当前语言拼装，got %q want %q", tc.name, got, tc.want)
		}
	}

	zh := pageBulkLangCtx(t, "zh-CN")
	if msg := pageMsgText(zh, outcomeMsg(pageenums.PagesBulkDeleteOutcome(3, 0))); msg != "已删除 3 个页面。" {
		t.Errorf("中文请求应取中文词条，实际 %q", msg)
	}
}
