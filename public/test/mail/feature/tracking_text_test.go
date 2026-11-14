package feature

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

const (
	wantTrackLinkInvalidText       = "链接无效或已过期"
	wantUnsubscribeLinkInvalidText = "退订链接无效或已过期"
)

var mailVisitorKeyTokens = []string{"mail.err.", "mail.msg.", "mailenums"}

var mailVisitorInternalTokens = []string{"追踪 token", "联系人不存在", "退订链接缺少", "SQLSTATE", `relation "`}

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

func TestMailTrackingUnsubscribeDonePageLocalizedAndEscaped(t *testing.T) {
	router, f := newTrackingRouter(t)
	if router == nil {
		return
	}

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
		t.Fatalf("成功提示 Content-Type 应为 text/html，实际 %q", ct)
	}
	// 提示类响应：共享成功出口的单根状态节点，文案由 Jet 按请求语言取词。
	page := recorder.Body.String()
	if !strings.Contains(page, `role="status"`) {
		t.Errorf("成功提示应是单根状态节点：%s", mailHead(page))
	}
	if !strings.Contains(page, html.EscapeString(rawEmail)) {
		t.Errorf("成功提示应含被 HTML 转义的邮箱 %q：%s", html.EscapeString(rawEmail), mailHead(page))
	}
	if strings.Contains(page, rawEmail) {
		t.Errorf("成功提示出现未转义的邮箱（注入面）：%s", mailHead(page))
	}
	if !strings.Contains(page, "不会再收到我们的营销邮件") {
		t.Errorf("成功提示缺少正文文案：%s", mailHead(page))
	}
	for _, tok := range mailVisitorKeyTokens {
		if strings.Contains(page, tok) {
			t.Errorf("成功提示出现裸 key 片段 %q：%s", tok, mailHead(page))
		}
	}
}

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
	// 提示类响应没有 <html lang>；语言体现在取词结果上：en-US 的正文必须出现英文。
	if !strings.Contains(page, "will no longer receive our marketing emails") {
		t.Errorf("Accept-Language: en-US 的请求应拿到英文取词（固定中文是改前的行为）：%s", mailHead(page))
	}
	if strings.Contains(page, "不会再收到") {
		t.Errorf("en-US 请求不应出现中文正文：%s", mailHead(page))
	}
	if !strings.Contains(page, "lang@x.test") {
		t.Errorf("成功提示应含收件人邮箱：%s", mailHead(page))
	}
}

func mustSignToken(t *testing.T, f *fixture, p maildto.TrackPayload) string {
	t.Helper()
	token, err := f.svc.SignTrackToken(p)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	return token
}
