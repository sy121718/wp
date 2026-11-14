package feature

import (
	"context"
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	pageview "go_wp/pkg/view"
	text "go_wp/pkg/view/jet"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	mailmodel "go_wp/internal/module/mail/model"
)

func TestTrackTokenSignAndVerify(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	payload := maildto.TrackPayload{LogID: 7, CampaignID: 3, ContactID: 11, URL: "https://shop.example.com/p/1"}
	token, err := f.svc.SignTrackToken(payload)
	if err != nil {
		t.Fatal(err)
	}
	back, err := f.svc.ParseTrackToken(token)
	if err != nil {
		t.Fatalf("验签失败: %v", err)
	}
	if back.LogID != 7 || back.CampaignID != 3 || back.ContactID != 11 || back.URL != payload.URL {
		t.Fatalf("载荷还原不一致: %+v", back)
	}

	dot := strings.LastIndex(token, ".")
	tampered := "A" + token[1:dot] + token[dot:]
	if _, err := f.svc.ParseTrackToken(tampered); err == nil {
		t.Fatal("篡改后的 token 必须验签失败")
	}

	other := mailmodel.NewMailModel(f.db)
	_ = other
}

func TestInjectTrackingRewritesLinksAndAddsPixel(t *testing.T) {
	f := newAccountFixture(t)
	if f == nil {
		return
	}
	html := `<p>你好</p><a href="https://shop.example.com/promo">去商城</a>` +
		`<a href="mailto:a@b.com">写信</a><a href="#top">回到顶部</a>`
	out, err := f.svc.InjectTracking(html, maildto.TrackPayload{LogID: 1, CampaignID: 2, ContactID: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "/_t/o/") {
		t.Fatal("没有注入打开像素")
	}
	if !strings.Contains(out, "/_t/c/") {
		t.Fatal("没有改写点击链接")
	}
	if strings.Contains(out, `href="https://shop.example.com/promo"`) {
		t.Fatal("原始外链应已被改写")
	}

	if !strings.Contains(out, `href="mailto:a@b.com"`) || !strings.Contains(out, `href="#top"`) {
		t.Fatal("mailto / 锚点不该被改写")
	}

	idx := strings.Index(out, "/_t/c/")
	start := idx + len("/_t/c/")
	end := strings.IndexAny(out[start:], `"`)
	token := out[start : start+end]
	p, err := f.svc.ParseTrackToken(token)
	if err != nil {
		t.Fatalf("改写后的 token 验不过: %v", err)
	}
	if p.URL != "https://shop.example.com/promo" {
		t.Fatalf("点击目标还原错误: %q", p.URL)
	}
}

func newTrackingRouter(t *testing.T) (*gin.Engine, *fixture) {
	t.Helper()
	f := newAccountFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	// 退订成功页由 Jet 渲染：与生产一样装配模板引擎与请求级取词作用域。
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	// 退订成功是提示类响应：与生产一样装共享成功出口与请求级取词作用域。
	router.Use(pageview.UseSuccess(pageview.NewSuccessWriter()))
	router.Use(func(c *gin.Context) { text.AttachScope(c); c.Next() })
	// 生产装配会从库里加载词库；测试夹具走内存注入，否则取词只落兜底、语言切换无从验证。
	i18n.InjectForTest(map[string]map[string]string{
		"mailUnsubDone": {
			"zh-CN": "%s 不会再收到我们的营销邮件。",
			"en-US": "%s will no longer receive our marketing emails.",
		},
		mailenums.ErrUnsubscribeLinkInvalid: {
			"zh-CN": "退订链接无效或已过期",
			"en-US": "This unsubscribe link is invalid or has expired",
		},
	}, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })
	mailhttp.SetupTrackingRoutes(router, mailcontract.TrackingService(f.svc))
	return router, f
}

func TestClickRedirectsToSignedTarget(t *testing.T) {
	router, f := newTrackingRouter(t)
	if router == nil {
		return
	}
	token, err := f.svc.SignTrackToken(maildto.TrackPayload{
		LogID: 1, CampaignID: 2, ContactID: 3, URL: "https://shop.example.com/x",
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/_t/c/"+token, nil))
	if recorder.Code != http.StatusFound {
		t.Fatalf("应 302，实际 %d", recorder.Code)
	}
	if loc := recorder.Header().Get("Location"); loc != "https://shop.example.com/x" {
		t.Fatalf("跳转目标错误: %q", loc)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/_t/c/bogus.token", nil))
	if recorder.Code == http.StatusFound {
		t.Fatal("无效 token 不该产生跳转")
	}
}

func TestOpenAlwaysReturnsPixel(t *testing.T) {
	router, f := newTrackingRouter(t)
	if router == nil {
		return
	}
	token, _ := f.svc.SignTrackToken(maildto.TrackPayload{LogID: 1, CampaignID: 2, ContactID: 3})
	for _, path := range []string{"/_t/o/" + token + ".gif", "/_t/o/bogus.gif"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s 应返回 200，实际 %d", path, recorder.Code)
		}
		if ct := recorder.Header().Get("Content-Type"); !strings.Contains(ct, "image/gif") {
			t.Fatalf("%s Content-Type 应为 image/gif，实际 %q", path, ct)
		}
	}
}

func TestUnsubscribeWritesSuppressionAndStatus(t *testing.T) {
	router, f := newTrackingRouter(t)
	if router == nil {
		return
	}
	ctx := context.Background()
	contact := &mailmodel.MailContactEntity{
		Email: "unsub@example.com", Source: mailmodel.ContactSourceImport, Status: mailmodel.ContactStatusSubscribed,
	}
	if err := f.m.CreateContact(ctx, contact); err != nil {
		t.Fatal(err)
	}
	token, err := f.svc.SignTrackToken(maildto.TrackPayload{LogID: 1, CampaignID: 2, ContactID: contact.ID})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/_t/u/"+token, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("退订应成功，实际 %d", recorder.Code)
	}

	row, err := f.m.GetContact(ctx, contact.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != mailmodel.ContactStatusUnsubscribed {
		t.Fatalf("状态应为 unsubscribed，实际 %s", row.Status)
	}
	suppressed, err := f.m.IsSuppressed(ctx, "unsub@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !suppressed {
		t.Fatal("退订必须同时写入抑制名单（只改状态的话，换个活动又会被发出去）")
	}

	// 提示类响应走共享成功出口：单根状态节点 + Jet 取词，不是 Go 内拼的整页。
	body := recorder.Body.String()
	if !strings.Contains(body, `role="status"`) || !strings.Contains(body, "</p>") {
		t.Fatalf("退订成功应交给共享成功出口渲染单根状态节点：\n%s", body)
	}
	if !strings.Contains(body, "不会再收到我们的营销邮件") {
		t.Fatalf("成功提示应按请求语言取词并带上收件地址：\n%s", body)
	}
	if strings.Contains(body, "<!DOCTYPE html>") {
		t.Fatalf("提示类响应不应另建整页文档：\n%s", body)
	}

	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/_t/u/"+token, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("重复退订应幂等成功，实际 %d", recorder.Code)
	}
}

// 无效 token 必须走失败出口且不暴露内部原因。
func TestUnsubscribeInvalidTokenRejected(t *testing.T) {
	router, _ := newTrackingRouter(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/_t/u/bogus.token", nil))
	if recorder.Code == http.StatusOK {
		t.Fatalf("无效 token 不该 200，实际 %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, leak := range []string{"SQLSTATE", "sys_", "mail_contacts"} {
		if strings.Contains(body, leak) {
			t.Fatalf("失败响应泄漏内部原文 %q：\n%s", leak, body)
		}
	}
}
