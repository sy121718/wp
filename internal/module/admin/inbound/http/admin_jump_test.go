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
// 结论文案现在是**事实**（adminui.Msg：key + 中文兜底 + %s 参数），所以断言前先在测试里
// 渲染一次（adminui.MsgText）—— 这正好等价于提示页模板的 tf 在渲染期做的事。
//
// 提示页本身的 HTTP 形态（200 + data-jump-state）由 feature 层断言（需要真 Jet 渲染器）。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/i18n"

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
		nounText := adminui.MsgText(c, noun)

		ok, msg := adminBulkOutcome(c, noun, 3, 0)
		if !ok {
			t.Errorf("全成功应走成功档（noun=%q）", noun.Key)
		}
		if text := adminui.MsgText(c, msg); !strings.Contains(text, nounText) || !strings.Contains(text, "3") {
			t.Errorf("成功文案应含名词与计数：got %q（noun=%q）", text, nounText)
		}

		ok, msg = adminBulkOutcome(c, noun, 3, 2)
		if ok {
			t.Errorf("有跳过必须走失败档（noun=%q）—— 否则部分成功会被绿勾报成全部成功", noun.Key)
		}
		if text := adminui.MsgText(c, msg); !strings.Contains(text, nounText) || !strings.Contains(text, "3") || !strings.Contains(text, "2") {
			t.Errorf("部分成功文案应含名词与两个计数：got %q", text)
		}

		ok, msg = adminBulkOutcome(c, noun, 0, 0)
		if ok {
			t.Errorf("一个可操作项都没有应走失败档（noun=%q）", noun.Key)
		}
		if msg.Key != adminBadRequestMsg.Key {
			t.Errorf("无可操作项应给参数错误文案，got %q", msg.Key)
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
	if text := adminui.MsgText(en, msg); !ok || text != "EN-DONE 3 EN-ROLE" {
		t.Fatalf("成功文案应按当前语言取词，got ok=%v text=%q", ok, text)
	}
	ok, msg = adminBulkOutcome(en, adminBulkNounRole, 3, 2)
	if text := adminui.MsgText(en, msg); ok || text != "EN-PARTIAL 3 / EN-ROLE / 2" {
		t.Fatalf("部分成功文案应按当前语言取词，got ok=%v text=%q", ok, text)
	}

	zh := adminJumpCtx(t, "")
	zh.Request.Header.Set("Accept-Language", "zh-CN")
	if _, msg := adminBulkOutcome(zh, adminBulkNounRole, 3, 0); adminui.MsgText(zh, msg) != "已删除 3 个角色" {
		t.Fatalf("中文请求应取中文模板，got %q", adminui.MsgText(zh, msg))
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
