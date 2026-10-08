package adminhttp

// admin_jump_test.go — 写动作提示页的**写侧文案与回跳上下文**单测。
//
// 取代原先的 admin_page_err_text_test.go / admin_err_texts_test.go：那两份钉的是**读侧**
// 受控出口（把 ?err= / ?done= 里读回来的任意串与白名单整体匹配，未命中落空串）。结论改由
// shell.RenderJump 渲染整页提示之后，查询参数不再喂给页面，那套读侧判定已整批删除 ——
// 它的覆盖目标（「手拼 URL 不能伪造系统提示」）改由 feature 层断言「?err= 不再渲染任何内容」，
// 本文件则钉住**写侧**剩下的两件事：
//
//  1. 结论文案与成功 / 失败档位（adminBulkOutcome）—— 改传输通道不该改「成功说的是哪句话」；
//  2. 回跳上下文的键白名单（adminListQuery）—— 表单 action 上带什么、BackPath 就读什么，
//     两处必须是同一份键表，且**只透传调用点列出的键**。
//
// 提示页本身的 HTTP 形态（200 + data-jump-state）由 feature 层断言（需要真 Jet 渲染器）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// adminJumpCtx 组一个带 query 的 POST 上下文（归口助手与取词都从它取）。
func adminJumpCtx(t *testing.T, rawQuery string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(http.MethodPost, "/admin/roles/bulk-delete?"+rawQuery, nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

// TestAdminBulkOutcomeBranches 批量删除的三种结论：全成功（成功档）/ 有跳过（失败档）/
// 一个可操作项都没有（失败档 + 参数错误文案）。
//
// 档位（ok）与文案一起断言：只断言文案会让「部分成功被报成成功」（绿勾）这种缺陷溜过，
// 而运营看到绿色对勾就不会再去看剩下那些没删掉的。
func TestAdminBulkOutcomeBranches(t *testing.T) {
	c := newPageErrContext(t)
	for _, noun := range adminBulkNouns {
		nounText := adminBulkTextOf(c, noun)

		ok, msg := adminBulkOutcome(c, noun, 3, 0)
		if !ok {
			t.Errorf("全成功应走成功档（noun=%q）", noun.key)
		}
		if !strings.Contains(msg, nounText) || !strings.Contains(msg, "3") {
			t.Errorf("成功文案应含名词与计数：got %q（noun=%q）", msg, nounText)
		}

		ok, msg = adminBulkOutcome(c, noun, 3, 2)
		if ok {
			t.Errorf("有跳过必须走失败档（noun=%q）—— 否则部分成功会被绿勾报成全部成功", noun.key)
		}
		if !strings.Contains(msg, nounText) || !strings.Contains(msg, "3") || !strings.Contains(msg, "2") {
			t.Errorf("部分成功文案应含名词与两个计数：got %q", msg)
		}

		ok, msg = adminBulkOutcome(c, noun, 0, 0)
		if ok {
			t.Errorf("一个可操作项都没有应走失败档（noun=%q）", noun.key)
		}
		if msg != response.TranslateMessage(c, adminenums.MsgBadRequest) {
			t.Errorf("无可操作项应给参数错误文案，got %q", msg)
		}
	}
}

// TestAdminBulkOutcomeFollowsLanguage 结论文案按当前语言取词（与改造前的 adminBulkResultURL 同源）。
//
// 用 pkg/i18n.InjectForTest 直接往内存缓存塞 en-US 词条：不建库、不起装配，任何一处没按
// 当前语言取词都会立刻暴露。测完用 t.Cleanup 注入空缓存还原。
func TestAdminBulkOutcomeFollowsLanguage(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		adminenums.BulkDoneKey:    {"zh-CN": "已删除 %s 个%s", "en-US": "EN-DONE %s %s"},
		adminenums.BulkPartialKey: {"zh-CN": "已删除 %s 个%s，%s 个未能删除（受保护或被引用）", "en-US": "EN-PARTIAL %s / %s / %s"},
		adminenums.BulkNounRole:   {"zh-CN": "角色", "en-US": "EN-ROLE"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	en := adminJumpCtx(t, "")
	en.Request.Header.Set("Accept-Language", "en-US")

	ok, msg := adminBulkOutcome(en, adminBulkNounRole, 3, 0)
	if !ok || msg != "EN-DONE 3 EN-ROLE" {
		t.Fatalf("成功文案应按当前语言取词，got ok=%v msg=%q", ok, msg)
	}
	ok, msg = adminBulkOutcome(en, adminBulkNounRole, 3, 2)
	if ok || msg != "EN-PARTIAL 3 / EN-ROLE / 2" {
		t.Fatalf("部分成功文案应按当前语言取词，got ok=%v msg=%q", ok, msg)
	}

	zh := adminJumpCtx(t, "")
	zh.Request.Header.Set("Accept-Language", "zh-CN")
	if _, msg := adminBulkOutcome(zh, adminBulkNounRole, 3, 0); msg != "已删除 3 个角色" {
		t.Fatalf("中文请求应取中文模板，got %q", msg)
	}
}

// TestAdminListQueryOnlyWhitelistedKeys 表单 action 上只带调用点列出的筛选键。
//
// 白名单不是可选项：它决定 POST 回来时 shell.BackPath 能读回什么，也决定一个
// 「让管理员点一个带几 KB query 的链接、回跳时再拼回去」的入口是否存在。
func TestAdminListQueryOnlyWhitelistedKeys(t *testing.T) {
	c := adminJumpCtx(t, "keyword=%E8%A7%92%E8%89%B2&page=2&evil=payload&limit=20")
	got := adminListQuery(c, adminRolesBackKeys...)
	for _, want := range []string{"keyword=", "page=2", "limit=20"} {
		if !strings.Contains(got, want) {
			t.Errorf("白名单键 %q 应被透传，got %q", want, got)
		}
	}
	if strings.Contains(got, "evil") {
		t.Errorf("白名单之外的键不许透传，got %q", got)
	}
	if got := adminListQuery(c); got != "" {
		t.Errorf("未给键时不应透传任何参数，got %q", got)
	}
}

// TestAdminI18nEntryIdentityShapes 词条身份串的形状：只放行 `<key> · <lang>`，其余落空串。
func TestAdminI18nEntryIdentityShapes(t *testing.T) {
	for _, tc := range [][2]string{{"site.name", "zh-CN"}, {"admin.i18n.title", "en-US"}, {"a_b-c.d", "en"}} {
		want := tc[0] + adminI18nIdentitySep + tc[1]
		if got := adminI18nEntryIdentity(tc[0], tc[1]); got != want {
			t.Errorf("合法身份串应原样拼出，got %q want %q", got, want)
		}
	}
	for _, tc := range [][2]string{{"邮件.自动", "zh-CN"}, {"site.name", "中文"}, {"", "zh-CN"}, {"site.name", ""}, {"site name", "zh-CN"}} {
		if got := adminI18nEntryIdentity(tc[0], tc[1]); got != "" {
			t.Errorf("不合形状的输入应落空串（提示页不显示身份），got %q（%q · %q）", got, tc[0], tc[1])
		}
	}
}

// TestAdminI18nBulkDeleteResultBranches 词条页批量删除的四个分支都按当前语言取词。
//
// 与 adminBulkOutcome 同一判据：传输通道换了（URL → 响应体），取词那一层不许分叉 ——
// 分叉的表现是英文站点上提示整句回落中文（可见但不该发生）。
func TestAdminI18nBulkDeleteResultBranches(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		adminenums.BulkI18nNoneSelected: {"zh-CN": "没有勾选任何词条，列表未改动。", "en-US": "EN-I18N-NONE"},
		adminenums.BulkI18nAllDeleted:   {"zh-CN": "已删除 %s 条词条（构建时回退到组件包内的中文兜底）。", "en-US": "EN-I18N-DELETED %s"},
		adminenums.BulkI18nAllSkipped:   {"zh-CN": "%s 条词条都未能删除，列表未改动。", "en-US": "EN-I18N-SKIPPED %s"},
		adminenums.BulkI18nPartial:      {"zh-CN": "已删除 %s 条，%s 条未能删除（可能已被删除）。", "en-US": "EN-I18N-PARTIAL %s / %s"},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	c := adminJumpCtx(t, "")
	c.Request.Header.Set("Accept-Language", "en-US")
	cases := []struct {
		deleted, skipped int
		want             string
	}{
		{0, 0, "EN-I18N-NONE"},
		{3, 0, "EN-I18N-DELETED 3"},
		{0, 3, "EN-I18N-SKIPPED 3"},
		{2, 3, "EN-I18N-PARTIAL 2 / 3"},
	}
	for _, tc := range cases {
		if got := adminI18nBulkDeleteResult(c, tc.deleted, tc.skipped); got != tc.want {
			t.Errorf("deleted=%d skipped=%d：got %q want %q", tc.deleted, tc.skipped, got, tc.want)
		}
	}
}

// TestAdminI18nSavedDeletedTextFallsBackToSuccess 身份串不合形状时退回全站成功文案 ——
// 提示页不能显示「已保存：」后面空着（那是半句话）。
func TestAdminI18nSavedDeletedTextFallsBackToSuccess(t *testing.T) {
	c := newPageErrContext(t)
	success := shell.TranslateFor(c)(adminenums.MsgSuccess, "操作成功")

	if got := adminI18nSavedText(c, "site.name", "zh-CN"); !strings.Contains(got, "site.name") {
		t.Errorf("合法身份应出现在保存回执里，got %q", got)
	}
	if got := adminI18nSavedText(c, "邮件.自动", "zh-CN"); got != success {
		t.Errorf("不合形状的身份应退回全站成功文案，got %q", got)
	}
	if got := adminI18nDeletedText(c, "site.name", "zh-CN"); !strings.Contains(got, "site.name") {
		t.Errorf("合法身份应出现在删除回执里，got %q", got)
	}
	if got := adminI18nDeletedText(c, "", ""); got != success {
		t.Errorf("空身份应退回全站成功文案，got %q", got)
	}
}
