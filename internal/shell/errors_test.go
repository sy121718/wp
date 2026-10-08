package shell

// errors_test.go — AdminWriteFailedText 的入口契约：「文案由模块负责，壳层只兜空」。
//
// 为什么要单独钉这条入口：AdminWriteFailed（不区分错误类型，一律通用提示）与
// AdminWriteFailedText（文案由调用方过完模块白名单再传进来）是两条语义不同的出口。
// 前者用在纯基础设施错误的兜底，后者用在「可能是业务错误」的页面路径 —— 混用其中任何一条，
// 都会重新引入「业务文案被吞成系统错误」（用前者）或「模块把未过白名单的原文递进来而不自知」
//（用后者时把空串 / 原文直接传）。所以这里只钉壳层自己负责的那部分：空文案必须回落，
// 非空文案必须原样（trim 后）写出 400。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// newWriteFailedContext 组一个 POST 测试上下文（入口只读 path，不碰会话）。
func newWriteFailedContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/admin/roles/create", nil)
	return c, recorder
}

// TestAdminWriteFailedTextBlankFallsBackToInternal 空 / 纯空白文案回落 MsgInternalError：
// 页面不能显示空串（提示条会变成一条空白，看起来像渲染坏了）。
func TestAdminWriteFailedTextBlankFallsBackToInternal(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n\t "} {
		c, recorder := newWriteFailedContext()
		AdminWriteFailedText(c, raw)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("写失败入口应是 400，got %d（raw=%q）", recorder.Code, raw)
		}
		if !strings.Contains(recorder.Body.String(), MsgInternalError) {
			t.Fatalf("空文案应回落到 %s，got body=%s", MsgInternalError, recorder.Body.String())
		}
	}
}

// TestAdminWriteFailedTextKeepsModuleText 非空文案原样写出（trim 首尾空白后）：
// 壳层不做任何「是不是业务文案」的判断 —— 那是拥有白名单的模块的事。
func TestAdminWriteFailedTextKeepsModuleText(t *testing.T) {
	c, recorder := newWriteFailedContext()
	AdminWriteFailedText(c, "  角色编码已存在\n")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("写失败入口应是 400，got %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "角色编码已存在") {
		t.Fatalf("模块传进来的文案必须原样写出，got body=%s", body)
	}
	if strings.Contains(body, "\n") {
		t.Fatalf("文案应被 trim 后再写出，got body=%s", body)
	}
}
