package mailhttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/internal/web/shell"
	"go_wp/pkg/mailer"
)

// mail_notice_consistency_test.go — 回执「写侧 → 读侧」的形态对齐。
//
// 为什么值得一条测试：本模块的成功/失败回执是**写侧拼句子进 302 的 query、读侧按白名单
// 整句比对**（mail_err.go 的 mailNoticeTexts + shell.FacingNotice）。两侧各写一份
// 拼接代码时，改了其中一侧的表现是「点完按钮，页面上什么都没有」或一句「系统内部错误」——
// 服务端不报错、日志里也看不出来。
//
// 判据刻意用**实际拼出来的句子**去过读侧白名单，而不是比较两份字面量：
// 常量表对得上、拼装写错（少个句号、词条里换了占位符名）照样会失配。
func TestMailNoticeWriteSideMatchesReadSide(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newCtx := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/admin/mail/automation", nil)
		return c
	}

	// 状态回执：三条已知状态 + 未知档（提交方塞任意值也走 unknown 那条）。
	for _, status := range []string{"active", "paused", "draft", "", "bogus"} {
		c := newCtx()
		notice := mailAutomationStatusNotice(c, status)
		if strings.TrimSpace(notice) == "" {
			t.Fatalf("状态 %q 拼出的回执是空串", status)
		}
		if got := shell.FacingNotice(notice, mailNoticeTexts(c)); got == "" {
			t.Errorf("状态 %q 的回执在读侧白名单里认不出来（写侧写了、读侧不认）: %q", status, notice)
		}
	}

	// 测试发送失败回执：三个 mailer.Kind + 未分类档。
	for _, kind := range []string{
		string(mailer.KindTemporary), string(mailer.KindPermanent),
		string(mailer.KindConfiguration), "",
	} {
		c := newCtx()
		notice := mailTestSendFailedText(c, kind, "")
		if got := shell.FacingNotice(notice, mailNoticeTexts(c)); got == "" {
			t.Errorf("发送失败分类 %q 的回执在读侧白名单里认不出来: %q", kind, notice)
		}
	}
}
