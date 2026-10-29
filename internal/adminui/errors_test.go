package adminui

// errors_test.go — 页面出口与受控文本 helper 的入口契约（task-92 批 2b-1 自 internal/shell 迁入）。
//
// 钉三件事：
//
//	① AdminWriteFailedText：**文案由模块负责，本层只兜空**。模块把空串（提示条会变成一条空白）
//	   或未过白名单的原文递进来时，都会重新引入缺陷，所以本层只钉自己负责的那部分：空文案必须
//	   回落 MsgInternalError，非空文案必须 trim 后原样写出 400。
//	② PageErrorBadRequest / PageErrorInternal：400 / 500 + 通用文案，**原始错误只进日志**
//	   （响应体里不得出现 err 原文）；PageErrorInternal 迁家（shell.PageError → 本包）不改协议，
//	   响应体与 pkg/response.ErrorWithMessage(500, MsgInternalError) 逐字节相同。
//	③ PageInternalText / MsgInternalError：随请求语言取词、缺词条回落中文兜底；常量值就是 key
//	   字面量（模板与词条表按字面量取值）。

import (
	"errors"
	"go_wp/internal/text"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
)

// newPageExitContext 组一个页面写请求上下文（出口只读 path / 查询串，不碰会话）。
func newPageExitContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	return c, recorder
}

// TestAdminWriteFailedTextBlankFallsBackToInternal 空 / 纯空白文案回落 MsgInternalError：
// 页面不能显示空串（提示条会变成一条空白，看起来像渲染坏了）。
func TestAdminWriteFailedTextBlankFallsBackToInternal(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n\t "} {
		c, recorder := newPageExitContext("/admin/roles/create")
		AdminWriteFailedText(c, raw)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("写失败入口应是 400，got %d（raw=%q）", recorder.Code, raw)
		}
		// 断言**归口文案**而不是裸 key：出口会把 key 取成当前语言的译文，
		// 裸 key 只应出现在日志里（translate 未命中时原样返回，正是"把
		// MsgInternalError 直接发给客户端"那条缺陷）。
		if !strings.Contains(recorder.Body.String(), text.InternalFallback) {
			t.Fatalf("空文案应回落到归口文案（%s），got body=%s", text.InternalFallback, recorder.Body.String())
		}
	}
}

// TestAdminWriteFailedTextKeepsModuleText 非空文案原样写出（trim 首尾空白后）：
// 本层不做任何「是不是业务文案」的判断 —— 那是拥有白名单的模块的事。
func TestAdminWriteFailedTextKeepsModuleText(t *testing.T) {
	c, recorder := newPageExitContext("/admin/roles/create")
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

// TestPageErrorBadRequestHidesRawError 参数错误出口必须 400 + 通用文案，且原始错误不得进响应体。
//
// 这条用一个「像内部错误」的原文当探针：出口的价值就在于它只进日志。
func TestPageErrorBadRequestHidesRawError(t *testing.T) {
	c, recorder := newPageExitContext("/admin/roles/create")
	secret := `pq: relation "sys_roles" does not exist (42P01)`
	PageErrorBadRequest(c, "roles", errors.New(secret))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("参数错误出口应是 400，got %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, text.InternalFallback) {
		t.Fatalf("响应体应带归口文案（%s），got body=%s", text.InternalFallback, body)
	}
	if strings.Contains(body, secret) || strings.Contains(body, "42P01") || strings.Contains(body, "sys_roles") {
		t.Fatalf("原始错误泄漏进了响应体：%s", body)
	}
}

// TestPageErrorInternalIsInternalExit 局部端点的内部错误出口：500 + 通用文案，原文只进日志。
//
// 两个反向断言都很重要：原始错误不得进响应体（这是出口存在的理由）；**传 nil err 也必须写出
// 500** —— 调用方常常是「拿不到细节」的失败分支，一个不存在的错误不该让出口退化成「什么都没写」。
func TestPageErrorInternalIsInternalExit(t *testing.T) {
	c, recorder := newPageExitContext("/admin/coupons/edit")
	secret := `pq: relation "order_coupons" does not exist (42P01)`
	PageErrorInternal(c, "coupon-edit-form", errors.New(secret))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("局部内部错误出口应是 500，got %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, text.InternalFallback) {
		t.Fatalf("响应体应带归口文案（%s），got body=%s", text.InternalFallback, body)
	}
	if strings.Contains(body, secret) || strings.Contains(body, "42P01") || strings.Contains(body, "order_coupons") {
		t.Fatalf("原始错误泄漏进了响应体：%s", body)
	}

	c2, recorder2 := newPageExitContext("/admin/coupons/edit")
	PageErrorInternal(c2, "coupon-edit-form", nil)
	if recorder2.Code != http.StatusInternalServerError {
		t.Fatalf("nil err 也应写出 500，got %d", recorder2.Code)
	}
}

// TestPageErrorInternalKeepsShellProtocol 迁家**不改协议**：状态码 / Content-Type / body 与
// pkg/response.ErrorWithMessage(500, MsgInternalError) 逐字节相同。
//
// 消费者是 coupon 局部端点（浏览器拿不到页壳、前端按这个形状解 JSON），所以这里不放宽成
// 「contains key」—— 一旦有人给它加个字段或换文案，这条会红。
func TestPageErrorInternalKeepsShellProtocol(t *testing.T) {
	wantCtx, wantRecorder := newPageExitContext("/admin/coupons/edit")
	response.ErrorWithMessage(wantCtx, http.StatusInternalServerError, MsgInternalError)

	gotCtx, gotRecorder := newPageExitContext("/admin/coupons/edit")
	PageErrorInternal(gotCtx, "coupon-edit-form", errors.New("内部细节：表 order_coupons"))

	if gotRecorder.Code != wantRecorder.Code {
		t.Fatalf("状态码与 pkg/response 出口不一致：got %d want %d", gotRecorder.Code, wantRecorder.Code)
	}
	if got, want := gotRecorder.Header().Get("Content-Type"), wantRecorder.Header().Get("Content-Type"); got != want {
		t.Fatalf("Content-Type 不一致：got %q want %q", got, want)
	}
	if got, want := gotRecorder.Body.String(), wantRecorder.Body.String(); got != want {
		t.Fatalf("响应体必须逐字节相同：\ngot  %q\nwant %q", got, want)
	}
}

// TestPageInternalTextFollowsRequestLanguage 受控文本随请求语言取词；缺词条回落中文兜底。
//
// 词条是注入的合成值（不依赖库）：命中时返回注入值，未注入时返回与 058 zh-CN 词条逐字相同的
// 中文兜底 —— 后者是「库里没有词条也不会显示裸 key」的保证。
func TestPageInternalTextFollowsRequestLanguage(t *testing.T) {
	i18n.InjectForTest(map[string]map[string]string{
		"MsgInternalError": {"zh-CN": "系统内部错误，请稍后重试", "en-US": "Internal error, please retry later"},
	}, nil)
	defer i18n.InjectForTest(nil, nil)

	if c, _ := newPageExitContext("/admin/roles?lang=en-US"); PageInternalText(c) != "Internal error, please retry later" {
		t.Fatalf("en-US 应取注入的英文词条，got %q", PageInternalText(c))
	}
	if c, _ := newPageExitContext("/admin/roles?lang=zh-CN"); PageInternalText(c) != "系统内部错误，请稍后重试" {
		t.Fatalf("zh-CN 应取注入的中文词条，got %q", PageInternalText(c))
	}

	// 清空词条：回落到与 058 zh-CN 词条逐字一致的中文兜底，绝不回落裸 key。
	i18n.InjectForTest(nil, nil)
	for _, path := range []string{"/admin/roles?lang=zh-CN", "/admin/roles?lang=en-US"} {
		c, _ := newPageExitContext(path)
		got := PageInternalText(c)
		if got != "系统内部错误，请稍后重试" {
			t.Fatalf("缺词条时应回落中文兜底，got %q（path=%s）", got, path)
		}
		if got == MsgInternalError {
			t.Fatalf("缺词条时回落成了裸 key：%q", got)
		}
	}
}

// TestMsgInternalErrorIsTheKeyLiteral 常量值就是 key 字面量：
// 词条表、模板与模块 enums 都按这个字符串取值，改这里等于换 key。
func TestMsgInternalErrorIsTheKeyLiteral(t *testing.T) {
	if MsgInternalError != "MsgInternalError" {
		t.Fatalf("MsgInternalError 必须是 key 字面量 %q，got %q", "MsgInternalError", MsgInternalError)
	}
}
