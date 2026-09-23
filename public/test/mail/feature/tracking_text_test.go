package feature

// tracking_text_test.go — 邮件公开链接（点击追踪 / 一键退订）的**访客面文案**（迁移 410 的验收）。
//
// 这三处出口（/_t/c/{token} 与 /_t/u/{token} 的失败短句 + 退订成功页）服务的不是后台运营，
// 而是**收件人**：邮件客户端里点开链接，浏览器直接打开 —— 无登录态、无后台页面壳。
// 形态（失败一句纯文本短句、成功一张整页 HTML）本来就对，本批收的是**文案来源**：
//   · 此前硬编码中文 —— 英文收件人（Accept-Language: en）只能看到中文；
//   · 而且那几句连词条都没有，运营想改措辞只能改代码。
//
// 断言是**强**的（与 mail_error_leak_test.go 同一水准）：
//  1. 响应体是受控文案（词条的当前语言值 / 其中文兜底），且**不出现裸 key** ——
//     i18n 未初始化或词条缺失时兜底必须落到中文原文；落到 key 就等于把
//     "mail.err.trackLinkInvalid" 印在收件人眼前（后台页面还有人会来报，这张页面没有人会）；
//  2. service 侧的失败原文（「追踪 token 签名校验失败」「联系人不存在」这类内部判据）不外发；
//  3. 退订成功页的 lang 属性跟随请求语言 —— 语言协商链真的接进了这张页面（此前固定 zh-CN）。

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	maildto "go_wp/internal/module/mail/dto"
	mailmodel "go_wp/internal/module/mail/model"
)

// 访客面文案的中文兜底（= Go 里的常量，见 mail_tracking.go）。
//
// 词条存在且值为同一句话（迁移 410 的 zh-CN 行与这两句逐字一致），词条缺失时
// pkg/i18n 也落到调用方给的 fallback —— 两条路径给出同一句话，所以断言取这里。
const (
	wantTrackLinkInvalidText       = "链接无效或已过期"
	wantUnsubscribeLinkInvalidText = "退订链接无效或已过期"
)

// mailVisitorKeyTokens 裸 key 指纹：受控文案里一定不出现（出现即「没翻译就渲染了」）。
var mailVisitorKeyTokens = []string{"mail.err.", "mail.msg.", "mailenums"}

// mailVisitorInternalTokens service 侧原文的指纹：内部判据不进响应。
var mailVisitorInternalTokens = []string{"追踪 token", "联系人不存在", "退订链接缺少", "SQLSTATE", `relation "`}

// assertVisitorTextControlled 断言访客面响应的正文是一句受控文案。
func assertVisitorTextControlled(t *testing.T, where, body, want string) {
	t.Helper()
	if got := strings.TrimSpace(body); got != want {
		t.Errorf("%s 应为受控文案 %q，实际 %q", where, want, mailHead(got))
	}
	for _, tok := range mailVisitorKeyTokens {
		if strings.Contains(body, tok) {
			t.Errorf("%s 出现裸 key 片段 %q（兜底必须落到中文原文）：%s", where, tok, mailHead(body))
		}
	}
	for _, tok := range mailVisitorInternalTokens {
		if strings.Contains(body, tok) {
			t.Errorf("%s 外发了 service 原文 %q：%s", where, tok, mailHead(body))
		}
	}
}

// TestMailTrackingClickFailTextControlled 点击端点失败：400 + 一句受控文案（不是裸 key、不是验签原文）。
func TestMailTrackingClickFailTextControlled(t *testing.T) {
	router, _ := newTrackingRouter(t)
	if router == nil {
		return
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/_t/c/bogus.token", nil))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("无效 token 应 400，实际 %d", recorder.Code)
	}
	assertVisitorTextControlled(t, "点击端点失败响应", recorder.Body.String(), wantTrackLinkInvalidText)
}

// TestMailTrackingUnsubscribeFailTextControlled 退订端点失败的两档都只给一句受控文案：
// token 无效（验签失败）与「token 有效但联系人查不到」（service 的原话是「联系人不存在」）。
func TestMailTrackingUnsubscribeFailTextControlled(t *testing.T) {
	router, f := newTrackingRouter(t)
	if router == nil {
		return
	}

	cases := []struct {
		name  string
		token string
	}{
		{name: "验签失败", token: "bogus.token"},
		{
			// 签名合法但联系人不存在：走的是 service 的第二档错误（原文「联系人不存在」）。
			name:  "联系人查不到",
			token: mustSignToken(t, f, maildto.TrackPayload{LogID: 1, CampaignID: 2, ContactID: 99_999_999}),
		},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/_t/u/"+tc.token, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s 应 400，实际 %d", tc.name, recorder.Code)
		}
		assertVisitorTextControlled(t, "退订端点失败响应（"+tc.name+"）", recorder.Body.String(), wantUnsubscribeLinkInvalidText)
	}
}

// TestMailTrackingUnsubscribeDonePageLocalizedAndEscaped 退订成功页：
// lang 跟随请求语言、三句文案都来自词条（当前为中文兜底）、邮箱经 HTML 转义。
func TestMailTrackingUnsubscribeDonePageLocalizedAndEscaped(t *testing.T) {
	router, f := newTrackingRouter(t)
	if router == nil {
		return
	}

	// 邮箱里塞一个引号：转义一旦丢失，它就变成了能改写页面的字符（词条与邮箱都进 HTML）。
	const rawEmail = `escape"quote@x.test`
	contact := &mailmodel.MailContactEntity{
		Email: rawEmail, Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
	}
	if err := f.m.CreateContact(context.Background(), contact); err != nil {
		t.Fatalf("建联系人失败: %v", err)
	}
	token := mustSignToken(t, f, maildto.TrackPayload{LogID: 1, CampaignID: 2, ContactID: contact.ID})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/_t/u/"+token, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("退订应成功，实际 %d", recorder.Code)
	}
	if ct := recorder.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("成功页 Content-Type 应为 text/html，实际 %q", ct)
	}
	page := recorder.Body.String()

	if !strings.Contains(page, `lang="zh-CN"`) {
		t.Errorf("默认语言下 lang 应为 zh-CN：%s", mailHead(page))
	}
	if !strings.Contains(page, html.EscapeString(rawEmail)) {
		t.Errorf("成功页应含被 HTML 转义的邮箱 %q：%s", html.EscapeString(rawEmail), mailHead(page))
	}
	if strings.Contains(page, rawEmail) {
		t.Errorf("成功页出现未转义的邮箱（注入面）：%s", mailHead(page))
	}
	// 三句文案（词条缺失时落中文兜底）—— 只断言「说清了三件事」，不钉死标点。
	for _, want := range []string{"已退订", "不会再收到我们的营销邮件", "事务类邮件"} {
		if !strings.Contains(page, want) {
			t.Errorf("成功页缺少文案片段 %q：%s", want, mailHead(page))
		}
	}
	for _, tok := range mailVisitorKeyTokens {
		if strings.Contains(page, tok) {
			t.Errorf("成功页出现裸 key 片段 %q：%s", tok, mailHead(page))
		}
	}
}

// TestMailTrackingUnsubscribeDonePageFollowsRequestLanguage 语言协商真的接进了这张页面：
// Accept-Language: en-US 的请求拿到的 lang 必须是 en-US（此前固定 zh-CN）。
//
// 只断言 lang 属性、不断言英文文案：测试进程不初始化 i18n 组件，词条取不到时
// 中文兜底是**预期行为**（文案本身由 TestMailTrackingUnsubscribeDonePageLocalizedAndEscaped 钉住）。
func TestMailTrackingUnsubscribeDonePageFollowsRequestLanguage(t *testing.T) {
	router, f := newTrackingRouter(t)
	if router == nil {
		return
	}
	contact := &mailmodel.MailContactEntity{
		Email: "lang@x.test", Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
	}
	if err := f.m.CreateContact(context.Background(), contact); err != nil {
		t.Fatalf("建联系人失败: %v", err)
	}
	token := mustSignToken(t, f, maildto.TrackPayload{LogID: 1, CampaignID: 2, ContactID: contact.ID})

	req := httptest.NewRequest(http.MethodGet, "/_t/u/"+token, nil)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("退订应成功，实际 %d", recorder.Code)
	}
	page := recorder.Body.String()
	if !strings.Contains(page, `lang="en-US"`) {
		t.Errorf("Accept-Language: en-US 的请求应拿到 lang=\"en-US\"（固定 zh-CN 是改前的行为）：%s", mailHead(page))
	}
	if !strings.Contains(page, "lang@x.test") {
		t.Errorf("成功页应含收件人邮箱：%s", mailHead(page))
	}
}

// mustSignToken 签发追踪 token（失败即结束用例）。
func mustSignToken(t *testing.T, f *fixture, p maildto.TrackPayload) string {
	t.Helper()
	token, err := f.svc.SignTrackToken(p)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	return token
}
