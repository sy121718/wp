package feature

import (
	pageview "go_wp/pkg/view"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/adminctx"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	"go_wp/internal/templates"

	"github.com/gin-gonic/gin"
)

var marketingPagePerms = map[string]bool{
	"mail:contact_import":    true,
	"mail:contact_status":    true,
	"mail:campaign_save":     true,
	"mail:campaign_delete":   true,
	"mail:template_save":     true,
	"mail:template_delete":   true,
	"mail:automation_delete": true,
}

func newMarketingPageRouter(t *testing.T) (*gin.Engine, *mailFixture) {
	t.Helper()
	f := newMailFeatureFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(adminctx.PermSetKey, marketingPagePerms)
		c.Set(adminctx.ButtonsKey, buttonsOf(marketingPagePerms))
	})
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	handle := mailhttp.NewMailPageHandle(f.svc)
	router.GET("/admin/mail/contacts",
		pageview.Slot(mailhttp.ContactsPageTemplate, adminui.RenderPage), handle.MailContactsPage)
	router.GET("/admin/mail/campaigns",
		pageview.Slot(mailhttp.CampaignsPageTemplate, adminui.RenderPage), handle.MailCampaignsPage)
	router.GET("/admin/mail/marketing", handle.MailMarketingRedirect)
	return router, f
}

func TestMailContactsPageRenders(t *testing.T) {
	router, f := newMarketingPageRouter(t)
	if router == nil {
		return
	}
	ctx := context.Background()

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
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/mail/contacts", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("页面状态码 %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		"<h1>联系人</h1>", "sub@example.com", "pend@example.com",
		`<span class="badge badge-success">已订阅</span>`,
		`<span class="badge badge-mute">待确认</span>`,
		`hx-post="/admin/mail/contact/import`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q；前 600 字：\n%s", want, firstN(body, 600))
		}
	}

	if strings.Contains(body, "九月活动") {
		t.Fatal("联系人页出现了群发活动名 —— 两块职能没搬干净")
	}
}

func TestMailCampaignsPageRenders(t *testing.T) {
	router, f := newMarketingPageRouter(t)
	if router == nil {
		return
	}
	ctx := context.Background()
	if err := f.seedCampaign(ctx, "九月活动"); err != nil {
		t.Fatal(err)
	}
	if err := f.seedContact(ctx, "sub@example.com", "订阅者", "subscribed", "vip"); err != nil {
		t.Fatal(err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/mail/campaigns", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("页面状态码 %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		"<h1>群发活动</h1>", "九月活动", "全部已订阅", "启动群发",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q；前 600 字：\n%s", want, firstN(body, 600))
		}
	}

	if strings.Contains(body, "sub@example.com") {
		t.Fatal("群发活动页出现了联系人邮箱 —— 两块职能没搬干净")
	}
}

func TestMailContactsPageRendersEmpty(t *testing.T) {
	router, _ := newMarketingPageRouter(t)
	if router == nil {
		return
	}
	for _, tc := range []struct {
		name, query, title, description string
		clearFilters                    bool
	}{
		{"无筛选", "", "还没有联系人", "新建一位联系人手工加一条，或用「导入联系人」批量导入 CSV —— 名单建立起来才能按标签圈人群群发。", false},
		{"关键词筛选", "?keyword=missing%40example.com", "没有匹配的联系人", "可调整筛选条件，或点右上角「导入联系人」批量导入。", true},
		{"状态筛选", "?status=subscribed", "没有匹配的联系人", "可调整筛选条件，或点右上角「导入联系人」批量导入。", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/mail/contacts"+tc.query, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("空状态页面状态码 %d：%s", recorder.Code, firstN(recorder.Body.String(), 600))
			}
			body := recorder.Body.String()
			for _, want := range []string{
				"<thead><tr>", "<th>邮箱</th>", `<td colspan="8" class="cell-wrap">`,
				`<p class="empty-title">` + tc.title + `</p>`,
				`<p class="empty-desc">` + tc.description + `</p>`,

				`hx-post="/admin/mail/contact/import`, "</table>", "</html>",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("空状态页面缺少 %q；前 600 字：\n%s", want, firstN(body, 600))
				}
			}
			otherTitle := "还没有联系人"
			if !tc.clearFilters {
				otherTitle = "没有匹配的联系人"
			}
			if strings.Contains(body, `<p class="empty-title">`+otherTitle+`</p>`) {
				t.Fatalf("空状态页面误入另一分档 %q", otherTitle)
			}

			clearAction := `<a class="btn" href="/admin/mail/contacts">清空筛选看全部</a>`
			if strings.Contains(body, clearAction) != tc.clearFilters {
				t.Fatalf("清空筛选出口：实际存在 %t，期望 %t", strings.Contains(body, clearAction), tc.clearFilters)
			}
		})
	}
}

func TestMailMarketingLegacyPathRedirects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/admin/mail/marketing", mailhttp.NewMailPageHandle(nil).MailMarketingRedirect)

	for _, tc := range []struct {
		name    string
		query   string
		want    map[string]string
		dropped []string
	}{
		{"无参数", "", nil, nil},
		{"透传三个筛选参数", "?keyword=vip%40example.com&status=subscribed&page=2",
			map[string]string{"keyword": "vip@example.com", "status": "subscribed", "page": "2"}, nil},
		{"丢弃活动类参数", "?keyword=vip&campaignId=9&runStatus=failed",
			map[string]string{"keyword": "vip"}, []string{"campaignId", "runStatus"}},
		{"空白参数按未给处理", "?keyword=%20&status=&page=", nil, []string{"keyword", "status", "page"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/mail/marketing"+tc.query, nil))

			if recorder.Code != http.StatusFound {
				t.Fatalf("应回 302，实际 %d（Location=%s）", recorder.Code, recorder.Header().Get("Location"))
			}
			location := recorder.Header().Get("Location")
			parsed, err := url.Parse(location)
			if err != nil {
				t.Fatalf("Location 无法解析：%v（%s）", err, location)
			}
			if parsed.Path != "/admin/mail/contacts" {
				t.Fatalf("应跳到 /admin/mail/contacts，实际 %q", location)
			}
			got := parsed.Query()
			if len(got) != len(tc.want) {
				t.Fatalf("透传参数数量不符：%q（期望 %d 个，实际 %d 个）", location, len(tc.want), len(got))
			}
			for key, value := range tc.want {
				if got.Get(key) != value {
					t.Errorf("参数 %s 应为 %q，实际 %q（Location=%s）", key, value, got.Get(key), location)
				}
			}
			for _, key := range tc.dropped {
				if got.Has(key) {
					t.Errorf("参数 %s 应被丢弃，实际透传了（Location=%s）", key, location)
				}
			}
		})
	}
}
