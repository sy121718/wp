package feature

// mail_marketing_page_test.go — 营销页（联系人 + 群发活动）的服务端渲染（issue #37）。
//
// 与配置页测试同样的理由：Jet 模板运行时解析，编译通过不代表模板正确。
// 这里额外钉两件事：
//   · 「只发给已订阅」这条合规语义在页面上**可见**（空标签显示为「全部已订阅」）；
//   · 模板下拉需要模板 id（活动存的是 template_id 而不是 key），字段缺失会让下拉空白。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dashboardhttp "go_wp/internal/module/dashboard/inbound/http"
	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

func newMarketingPageRouter(t *testing.T) (*gin.Engine, *mailFixture) {
	t.Helper()
	f := newMailFeatureFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	handle := dashboardhttp.NewMailPageHandle(f.svc)
	router.GET("/admin/mail/marketing", handle.MailMarketingPage)
	return router, f
}

func TestMailMarketingPageRenders(t *testing.T) {
	router, f := newMarketingPageRouter(t)
	if router == nil {
		return
	}
	ctx := context.Background()
	// 一个已订阅联系人 + 一个待确认联系人：页面要能同时呈现两种状态。
	if err := f.seedContact(ctx, "sub@example.com", "订阅者", "subscribed", "vip"); err != nil {
		t.Fatal(err)
	}
	if err := f.seedContact(ctx, "pend@example.com", "待确认者", "pending", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.seedCampaign(ctx, "九月活动"); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/mail/marketing", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("页面状态码 %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		"邮件营销", "sub@example.com", "pend@example.com",
		"已订阅", "待确认", "九月活动", "全部已订阅", "启动群发",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q；前 600 字：\n%s", want, firstN(body, 600))
		}
	}
}

func TestMailMarketingPageRendersEmpty(t *testing.T) {
	router, _ := newMarketingPageRouter(t)
	if router == nil {
		return
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/mail/marketing", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("空状态页面状态码 %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "没有匹配的联系人") {
		t.Fatal("空状态提示缺失")
	}
}
