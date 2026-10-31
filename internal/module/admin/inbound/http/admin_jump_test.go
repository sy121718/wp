package adminhttp

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

func adminJumpCtx(t *testing.T, rawQuery string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(http.MethodPost, "/admin/roles/bulk-delete?"+rawQuery, nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

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
