package feature

// mail_marketing_page_test.go — 拆页后的联系人页 / 群发活动页渲染 + 旧地址跳转（issue #37）。
//
// 拆页前「邮件营销」把两张表塞在同一页；拆页后每页只回答一个问题。
// 本文件钉三件事：
//   · 两个列表各自渲染，且**互不串台** —— 活动名不该出现在联系人页、邮箱不该出现在活动页。
//     这是「一页一职能」在服务端渲染层唯一可观测的判据（Jet 模板运行时解析，编译通过不代表模板正确）；
//   · 联系人页的空态分档、colspan 与清空筛选出口都指向新地址；
//   · 旧地址 /admin/mail/marketing 仍 302 到联系人页，且**只**透传 keyword / status / page。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	mailhttp "go_wp/internal/module/mail/inbound/http"
	"go_wp/internal/templates"
	"go_wp/internal/web/shell"

	"github.com/gin-gonic/gin"
)

// marketingPagePerms 页面里「导入联系人」「新建活动」等入口按权限码显隐，
// 生产由鉴权中间件写进 context（shell.PermSetKey）；这里直接给全，
// 断言的是渲染结果而不是权限链本身。
var marketingPagePerms = map[string]bool{
	"mail:contact_import":    true,
	"mail:contact_status":    true,
	"mail:campaign_save":     true,
	"mail:campaign_delete":   true,
	"mail:template_save":     true,
	"mail:template_delete":   true,
	"mail:automation_delete": true,
}

// newMarketingPageRouter 挂拆页后的三条路由（不经三层鉴权链：断言的是渲染结果与 Location）。
func newMarketingPageRouter(t *testing.T) (*gin.Engine, *mailFixture) {
	t.Helper()
	f := newMailFeatureFixture(t)
	if f == nil {
		return nil, nil
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(shell.PermSetKey, marketingPagePerms) })
	router.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	handle := mailhttp.NewMailPageHandle(f.svc)
	router.GET("/admin/mail/contacts", handle.MailContactsPage)
	router.GET("/admin/mail/campaigns", handle.MailCampaignsPage)
	router.GET("/admin/mail/marketing", handle.MailMarketingRedirect)
	return router, f
}

func TestMailContactsPageRenders(t *testing.T) {
	router, f := newMarketingPageRouter(t)
	if router == nil {
		return
	}
	ctx := context.Background()
	// 一个已订阅 + 一个待确认：页面要同时呈现两种同意状态（「只发给已订阅」的合规语义可见）。
	if err := f.seedContact(ctx, "sub@example.com", "订阅者", "subscribed", "vip"); err != nil {
		t.Fatal(err)
	}
	if err := f.seedContact(ctx, "pend@example.com", "待确认者", "pending", ""); err != nil {
		t.Fatal(err)
	}
	// 同时存在一个活动名：它属于另一页，不得串到本页。
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
		`action="/admin/mail/contact/import"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q；前 600 字：\n%s", want, firstN(body, 600))
		}
	}
	// 反证：群发活动属于另一页。这条断言若失败，说明拆页只搬了路由没搬区块。
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
	// 反证：联系人属于另一页。
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
				`action="/admin/mail/contact/import"`, "</table>", "</html>",
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
			// 清空筛选出口必须指向**本页**地址：拆页前它指向 /admin/mail/marketing，
			// 那样点一次会多绕一跳（现在那是个 302），且旧地址将来撤掉就断链。
			clearAction := `<a class="btn" href="/admin/mail/contacts">清空筛选看全部</a>`
			if strings.Contains(body, clearAction) != tc.clearFilters {
				t.Fatalf("清空筛选出口：实际存在 %t，期望 %t", strings.Contains(body, clearAction), tc.clearFilters)
			}
		})
	}
}

// TestMailMarketingLegacyPathRedirects 旧地址的兼容跳转：只透传目标页认识的参数。
//
// 不需要 fixture：跳转不碰数据（handler 只用 query）。
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
			// 302 而非 301：301 会被浏览器长期缓存，将来再调整落点老客户端回不来。
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
