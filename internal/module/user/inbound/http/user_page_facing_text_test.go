package userhttp

// user_page_facing_text_test.go — 访客页面「命中白名单后显示的是译文、不是裸 key」的回归。
//
// 根因（2026-09，与后台客户页 / order / return 各页同源）：userenums 的值是 i18n
// **item_key**（user.err.usernameTaken / user.err.internal …），而访客页面
// （user/register、user/account、user/message）里的 {{.error}} / {{.message}}
// 是**直接渲染**的文本、不经过 pkg/response 的 translate —— 原先的 userMessage 只放行 key
// 就把 key 原样渲染了出去：注册失败时访客看到的是「user.err.usernameTaken」。
//
// 修法照 return_page_query.go / customer_query.go 的样板：**判定只有一份**
// （userFacingText），页面出口多一层取词（userPageMessage / userKeyText）。
//
// 用 pkg/i18n.InjectForTest 直接塞内存词条（英文值带 EN- 前缀，与中文毫无相似度）：
// 不建库、不起装配，任何一处没按当前语言取词都会立刻暴露。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	userenums "go_wp/internal/module/user/enums"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// userFacingInjectedEntries 注入的词条（中英都注：只注英文会被 cache 的
// 「当前语言没有就遍历所有可用语言」兜底顶掉，中文断言就失去意义）。
var userFacingInjectedEntries = map[string]map[string]string{
	userenums.ErrUsernameTaken:   {"zh-CN": "用户名已被占用", "en-US": "EN-USERNAME-TAKEN"},
	userenums.ErrSessionNotFound: {"zh-CN": "会话不存在", "en-US": "EN-SESSION-NOT-FOUND"},
	userenums.MsgResetMailSent:   {"zh-CN": "重置邮件已发送", "en-US": "EN-RESET-MAIL-SENT"},
	userenums.ErrInternal:        {"zh-CN": "操作失败，请稍后重试", "en-US": "EN-INTERNAL"},
}

// userFacingLangCtx 构造一个绑定了 Accept-Language 的上下文。
func userFacingLangCtx(t *testing.T, lang string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/user/register", nil)
	if lang != "" {
		c.Request.Header.Set("Accept-Language", lang)
	}
	return c
}

// TestUserPageMessageTranslatesWhitelistHit 命中白名单的那一支按当前语言取词。
func TestUserPageMessageTranslatesWhitelistHit(t *testing.T) {
	i18n.InjectForTest(userFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	// 判定出口保持「原样返回 key」：白名单里存的就是 item_key。
	if got := userFacingText(userenums.ErrUsernameTaken); got != userenums.ErrUsernameTaken {
		t.Fatalf("判定出口应原样返回 key，实际 %q", got)
	}

	// 页面出口：同一份判定 + 按当前语言取词。
	zh := userPageMessage(userFacingLangCtx(t, "zh-CN"), errors.New(userenums.ErrUsernameTaken))
	if zh != "用户名已被占用" {
		t.Errorf("zh-CN 页面出口应给译文，实际 %q（裸 key 说明少了取词那一层）", zh)
	}
	en := userPageMessage(userFacingLangCtx(t, "en-US"), errors.New(userenums.ErrUsernameTaken))
	if en != "EN-USERNAME-TAKEN" {
		t.Errorf("en-US 页面出口应给英文译文，实际 %q", en)
	}

	// 参数级出口（没有 error 对象、文案就是常量）：同样不能裸出 key。
	if got := userKeyText(userFacingLangCtx(t, "en-US"), userenums.ErrSessionNotFound); got != "EN-SESSION-NOT-FOUND" {
		t.Errorf("userKeyText 应给译文，实际 %q", got)
	}
	if got := userKeyText(userFacingLangCtx(t, "zh-CN"), userenums.MsgResetMailSent); got != "重置邮件已发送" {
		t.Errorf("成功文案也应取词，实际 %q", got)
	}
}

// TestUserPageMessageFallsBackToInternalText 未命中 → 归口文案；nil → 空串。
func TestUserPageMessageFallsBackToInternalText(t *testing.T) {
	i18n.InjectForTest(userFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	c := userFacingLangCtx(t, "en-US")

	// 未命中白名单：落归口文案，且**原文不进页面**（数据库原文只进日志）。
	raw := `pq: relation "users" does not exist`
	if got := userPageMessage(c, errors.New(raw)); got != "EN-INTERNAL" {
		t.Errorf("未命中应落归口译文，实际 %q", got)
	}

	// nil error 给空串（调用点据此不显示提示条）。
	if got := userPageMessage(c, nil); got != "" {
		t.Errorf("nil error 应给空串，实际 %q", got)
	}

	// ErrInternal 自身不在白名单里 —— 它是归口文案，不是可透出的业务文案。
	if got := userFacingText(userenums.ErrInternal); got != "" {
		t.Errorf("ErrInternal 不应命中白名单，实际 %q", got)
	}
	// 但它经参数级出口时仍要取词（登出失败那一支就是这么用的）。
	if got := userKeyText(c, userenums.ErrInternal); got != "EN-INTERNAL" {
		t.Errorf("归口文案在参数级出口同样要取词，实际 %q", got)
	}
}
