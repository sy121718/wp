package adminhttp

// admin_bulk_i18n_language_test.go — 批量结论文案的**当前语言一致性**回归。
//
// 为什么必须有这一条：写侧与读侧现在共用 adminBulkTextOf 这一个取法，但「共用」是结构事实、
// 不是可观测事实 —— 只要有人在读侧把候选重新写成中文常量（或忘了带 c），中文环境下一切正常，
// 英文页面上真实的回执却被 shell.FacingNotice 判成伪造而**静默消失**
//（?done= 落空串、?err= 落归口文案），既没有报错也没有日志。
//
// 本用例用 pkg/i18n.InjectForTest 直接往内存缓存里塞 en-US 词条（值带 EN- 前缀，
// 与中文毫无相似度）：不建库、不起装配，任何一处没按当前语言取词都会立刻暴露。
// 测完用 t.Cleanup 注入空缓存还原 —— 注入是全局副作用，不能留给后面的用例。

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// adminBulkInjectedEntries 注入的词条（只注本批的 key，其余 key 的取词行为不变）。
//
// 中英都注：cache.Get 有一条「当前语言没有就遍历所有可用语言返回第一个」的兜底
// （pkg/i18n/cache.go），只注英文会让中文请求也拿到英文模板 —— 那是缓存的既有行为、
// 不是本批的取法问题，但会让「中文仍走中文」这条断言失去意义。生产 seed 中英成行，无此问题。
var adminBulkInjectedEntries = map[string]map[string]string{
	adminenums.BulkDoneKey: {"zh-CN": "已删除 %s 个%s", "en-US": "EN-DONE %s %s"},
	adminenums.BulkPartialKey: {
		"zh-CN": "已删除 %s 个%s，%s 个未能删除（受保护或被引用）",
		"en-US": "EN-PARTIAL %s / %s / %s",
	},
	adminenums.BulkNounRole:         {"zh-CN": "角色", "en-US": "EN-ROLE"},
	adminenums.BulkI18nNoneSelected: {"zh-CN": "没有勾选任何词条，列表未改动。", "en-US": "EN-I18N-NONE"},
	adminenums.BulkI18nAllDeleted: {
		"zh-CN": "已删除 %s 条词条（构建时回退到组件包内的中文兜底）。",
		"en-US": "EN-I18N-DELETED %s",
	},
	adminenums.BulkI18nAllSkipped: {"zh-CN": "%s 条词条都未能删除，列表未改动。", "en-US": "EN-I18N-SKIPPED %s"},
	adminenums.BulkI18nPartial:    {"zh-CN": "已删除 %s 条，%s 条未能删除（可能已被删除）。", "en-US": "EN-I18N-PARTIAL %s / %s"},
}

// adminBulkLangCtx 构造一个绑定了 Accept-Language 的上下文。
func adminBulkLangCtx(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/roles", nil)
	if lang != "" {
		c.Request.Header.Set("Accept-Language", lang)
	}
	return c
}

// TestAdminBulkNoticeFollowsRequestLanguage 写侧产出英文、读侧必须照样认得。
func TestAdminBulkNoticeFollowsRequestLanguage(t *testing.T) {
	i18n.InjectForTest(adminBulkInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	en := adminBulkLangCtx(t, "en-US")

	// 1) ?done=：全成功的批量删除。
	loc := adminBulkResultURL(en, "/admin/roles", adminBulkNounRole, 3, 0)
	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("写侧回跳地址无法解析：%v", err)
	}
	done := parsed.Query().Get("done")
	if done != "EN-DONE 3 EN-ROLE" {
		t.Fatalf("?done= 应按当前语言产出英文，实际 %q", done)
	}
	if got := adminPageDone(en, done); got != done {
		t.Errorf("英文页面上这条回执被读侧判成伪造（会静默消失）：got %q want %q", got, done)
	}

	// 2) ?err=：部分成功的批量删除（模板 + 名词都要按当前语言取）。
	loc = adminBulkResultURL(en, "/admin/roles", adminBulkNounRole, 3, 2)
	parsed, err = url.Parse(loc)
	if err != nil {
		t.Fatalf("写侧回跳地址无法解析：%v", err)
	}
	partial := parsed.Query().Get("err")
	if partial != "EN-PARTIAL 3 / EN-ROLE / 2" {
		t.Fatalf("?err= 应按当前语言产出英文，实际 %q", partial)
	}
	if got := adminPageErrText(en, partial); got != partial {
		t.Errorf("英文部分成功回执被读侧判成伪造：got %q want %q", got, partial)
	}

	// 3) 词条页批量删除的四个分支：走 ?done= 的两条 + 走 ?err= 的两条。
	doneBranches := []struct{ got, want string }{
		{adminI18nBulkDeleteResult(en, 0, 0), "EN-I18N-NONE"},
		{adminI18nBulkDeleteResult(en, 3, 0), "EN-I18N-DELETED 3"},
	}
	for _, tc := range doneBranches {
		if tc.got != tc.want {
			t.Fatalf("词条页 ?done= 分支应按当前语言产出，实际 %q want %q", tc.got, tc.want)
		}
		if got := adminPageDone(en, tc.got); got != tc.got {
			t.Errorf("词条页英文回执被读侧判成伪造：got %q want %q", got, tc.got)
		}
	}
	errBranches := []struct{ got, want string }{
		{adminI18nBulkDeleteResult(en, 0, 3), "EN-I18N-SKIPPED 3"},
		{adminI18nBulkDeleteResult(en, 2, 3), "EN-I18N-PARTIAL 2 / 3"},
	}
	for _, tc := range errBranches {
		if tc.got != tc.want {
			t.Fatalf("词条页 ?err= 分支应按当前语言产出，实际 %q want %q", tc.got, tc.want)
		}
		if got := adminPageErrText(en, tc.got); got != tc.got {
			t.Errorf("词条页英文回执被读侧判成伪造：got %q want %q", got, tc.got)
		}
	}

	// 4) 中文请求走中文词条（不能被英文模板顶掉）。
	zh := adminBulkLangCtx(t, "zh-CN")
	zhLoc := adminBulkResultURL(zh, "/admin/roles", adminBulkNounRole, 3, 0)
	zhParsed, zhErr := url.Parse(zhLoc)
	if zhErr != nil {
		t.Fatalf("中文回跳地址无法解析：%v", zhErr)
	}
	if msg := zhParsed.Query().Get("done"); msg != "已删除 3 个角色" {
		t.Errorf("中文请求应取中文模板，实际 %q", msg)
	} else if got := adminPageDone(zh, msg); got != msg {
		t.Errorf("中文回执应被认出，实际 %q", got)
	}
}
