package userhttp

// user_page_facing_text_test.go — 访客页面「页面出口给的是译文、不是裸 key」的回归。
//
// 判据在本批改成了**形态**（pkg/response.IsBusinessError）：userenums 的值本身就是
// i18n item_key，也是 `模块.类别.语义` 形态 —— 所以模块侧那份白名单
// （userenums.UserFacingMessages / userFacingText）整批删除。
//
// 两层出口：
//   · userPageMessage —— 有 error 对象的那一类（注册 / 登录 / 资料保存失败）；
//   · userKeyText     —— 没有 error、文案就是 enums 常量的那一类（缺设备标识、登出失败归口）。
//
// 两者都必须交**成品文本**：访客模板里的 {{ .["error"] }} / {{ .["message"] }} 是直接渲染的，
// 拿不到 .["t"]（那是 adminui.Prepare 注入后台页面的）。只放行 key 的话，
// 注册失败时访客看到的是「user.err.usernameTaken」。
//
// 用 pkg/i18n.InjectForTest 直接塞内存词条（英文值带 EN- 前缀，与中文毫无相似度）：
// 不建库、不起装配，任何一处没按当前语言取词都会立刻暴露。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go_wp/internal/adminui"
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
	adminui.MsgInternalError:     {"zh-CN": "系统内部错误，请稍后重试", "en-US": "EN-INTERNAL-GATE"},
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

// TestUserPageMessageTranslatesBusinessError 业务错误那一支按当前语言取词。
func TestUserPageMessageTranslatesBusinessError(t *testing.T) {
	i18n.InjectForTest(userFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

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

// TestUserPageMessageFallsBackToInternalText 未命中形态 → 归口文案；nil → 空串。
func TestUserPageMessageFallsBackToInternalText(t *testing.T) {
	i18n.InjectForTest(userFacingInjectedEntries, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	c := userFacingLangCtx(t, "en-US")

	// 未命中（数据库原文）：落归口文案，且**原文不进页面**（只进日志）。
	raw := `pq: relation "users" does not exist`
	if got := userPageMessage(c, errors.New(raw)); got != "EN-INTERNAL-GATE" {
		t.Errorf("未命中应落归口译文，实际 %q", got)
	}

	// nil error 给空串（调用点据此不显示提示条）。
	if got := userPageMessage(c, nil); got != "" {
		t.Errorf("nil error 应给空串，实际 %q", got)
	}

	// 形态不成立（数据库原文）的参数级出口 → 同样落归口，不把原文摆到页面上。
	if got := userKeyText(c, raw); got != "EN-INTERNAL-GATE" {
		t.Errorf("形态不成立的 key 应落归口译文，实际 %q", got)
	}
	// ErrInternal 本身是**形态成立的 key**（userenums 的值就是 item_key），
	// 所以它在参数级出口给的是它自己的译文 —— 登出失败那一支就是这么用的。
	if got := userKeyText(c, userenums.ErrInternal); got != "EN-INTERNAL" {
		t.Errorf("user.err.internal 在参数级出口应取它自己的译文，实际 %q", got)
	}
}
