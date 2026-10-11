package pagehttp

// page_bulk_i18n_language_test.go — 批量结论文案的**当前语言一致性**回归。
//
// 为什么必须有这一条：结论文案由写侧直接渲染进整页提示（shell.RenderJump），
// 取词发生在渲染期 —— 只要有人把结论写回中文常量（不再带 key），中文环境下一切正常，
// 英文页面上却整句变中文，既没有报错也没有日志。
//
// task-152 起结论是**事实**（pageMsg = key + 兜底 + `%s` 参数），所以判据有两条：
//   · 事实必须带 key（下面每条 case 都先断言 Key）—— 没有 key 就永远取不到译文；
//   · pageMsgText（Go 侧取词，片段槽位/JSON 出口用）与模板里注入的 tf（提示页用）
//     走的是同一个 adminui.TranslateArgs，所以这里断言前者即可代表后者。
//
// 用 pkg/i18n.InjectForTest 直接塞内存词条，不建库、不起装配。

import (
	"net/http"
	"net/http/httptest"
	"testing"

	pageenums "go_wp/internal/module/page/enums"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// pageBulkInjectedEntries 注入的词条（中英都注：只注英文会被 cache 的
// 「当前语言没有就遍历所有可用语言」兜底顶掉，中文断言就失去意义）。
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

// pageBulkLangCtx 构造一个绑定了 Accept-Language 的上下文。
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

// TestPageBulkNoticeFollowsRequestLanguage 页面列表与重定向两个批量结论都随当前语言。
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

	// 缺 id 的参数级提示（单条删除路径）走整页提示，同样随语言。
	if msg := pageMsgText(en, pagesLocalNoticeMissingID); msg != "EN-MISSING-ID" {
		t.Errorf("缺 id 提示应按当前语言取词，实际 %q", msg)
	}

	// 重定向批量删除（计数回带，文案由服务端重拼）。
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

	// 中文请求走中文词条。
	zh := pageBulkLangCtx(t, "zh-CN")
	if msg := pageMsgText(zh, outcomeMsg(pageenums.PagesBulkDeleteOutcome(3, 0))); msg != "已删除 3 个页面。" {
		t.Errorf("中文请求应取中文词条，实际 %q", msg)
	}
}
