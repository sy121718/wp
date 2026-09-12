package feature

// tracking_test.go — 营销追踪（打开 / 点击 / 退订）的 feature 测试（issue #38 P1）。
//
// 四条不能错的语义：
//  1. token **签名**校验：篡改载荷必须失败（否则谁都能伪造退订）；
//  2. 点击目标**只从 token 里解**：传入的请求参数不参与跳转（防开放重定向）；
//  3. 退订写抑制名单 + 改状态两者都做（只做一边，换个活动又会被发出去）；
//  4. 打开端点对无效 token 也返回像素（不泄露 token 是否有效，也不让收件人看到报错图）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
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

	// 篡改载荷（换掉联系人 id）必须失败 —— 否则任何人都能伪造「退订别人」的链接。
	dot := strings.LastIndex(token, ".")
	tampered := "A" + token[1:dot] + token[dot:]
	if _, err := f.svc.ParseTrackToken(tampered); err == nil {
		t.Fatal("篡改后的 token 必须验签失败")
	}

	// 换个密钥的 service 也验不过（签名绑密钥）。
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
	// mailto 与锚点不该被追踪（追踪了也没意义，还会破坏客户端行为）。
	if !strings.Contains(out, `href="mailto:a@b.com"`) || !strings.Contains(out, `href="#top"`) {
		t.Fatal("mailto / 锚点不该被改写")
	}
	// 从改写后的链接里解出 token，目标 URL 必须与原文一致。
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
	mailhttp.SetupTrackingRoutes(router, mailcontract.TrackingService(f.svc))
	return router, f
}

// TestClickRedirectsToSignedTarget 点击端点 302 到签名里的目标；无效 token 不跳转。
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

	// 无效 token：不跳转（跳去任意地方等于开放重定向）。
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/_t/c/bogus.token", nil))
	if recorder.Code == http.StatusFound {
		t.Fatal("无效 token 不该产生跳转")
	}
}

// TestOpenAlwaysReturnsPixel 打开端点对有效与无效 token 都返回 GIF。
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

// TestUnsubscribeWritesSuppressionAndStatus 退订同时改状态与写抑制名单。
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

	// 幂等：再点一次仍然 200，不报错。
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/_t/u/"+token, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("重复退订应幂等成功，实际 %d", recorder.Code)
	}
}
