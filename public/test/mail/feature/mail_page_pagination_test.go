package feature

import (
	pageview "go_wp/pkg/view"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/html"
	maildto "go_wp/internal/module/mail/dto"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/internal/templates"
)

func newMailPaginationRouter(t *testing.T, f *mailFixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	engine.Use(func(c *gin.Context) {
		c.Next()
		for _, ge := range c.Errors {
			t.Logf("gin 渲染错误: %v", ge.Err)
		}
	})
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	h := mailhttp.NewMailPageHandle(f.svc)
	engine.GET("/admin/mail",
		pageview.Slot(mailhttp.AccountsPageTemplate, pageview.RenderPage), h.MailPage)
	engine.GET("/admin/mail/templates",
		pageview.Slot(mailhttp.TemplatesPageTemplate, pageview.RenderPage), h.MailTemplatesPage)
	engine.GET("/admin/mail/contacts",
		pageview.Slot(mailhttp.ContactsPageTemplate, pageview.RenderPage), h.MailContactsPage)
	engine.GET("/admin/mail/campaign",
		pageview.Slot(mailhttp.CampaignPageTemplate, pageview.RenderPage), h.MailCampaignPage)
	return engine
}

func mailGet(t *testing.T, engine *gin.Engine, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s 应回 200，实际 %d（Location=%s）", path, rec.Code, rec.Header().Get("Location"))
	}
	return rec.Body.String()
}

func (f *mailFixture) seedAccounts(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := f.svc.CreateAccount(context.Background(), &maildto.SaveAccountReq{
			Name:      fmt.Sprintf("账号%02d", i),
			Purpose:   mailmodel.AccountPurposeTransactional,
			FromEmail: fmt.Sprintf("sender%02d@example.com", i),
			Host:      "smtp.example.com", Port: 587, Password: "p",
		}); err != nil {
			t.Fatalf("造第 %d 个账号失败：%v", i, err)
		}
	}
}

func TestMailPageAccountsPagination(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	engine := newMailPaginationRouter(t, f)
	f.seedAccounts(t, 21)

	first := mailGet(t, engine, "/admin/mail")
	if !strings.Contains(first, `class="pagination"`) {
		t.Fatal("21 个账号应出现分页条（每页 20 条）")
	}
	if !strings.Contains(first, "共 21 条") {
		t.Errorf("分页条应给出真实总数「共 21 条」：%s", mailHead(first))
	}
	if strings.Contains(first, "账号20") {
		t.Error("第 1 页不应包含第 21 个账号（排序按创建顺序，最后一条在第 2 页）")
	}
	if !strings.Contains(first, "account_page=2") {
		t.Error("第 1 页应给出账号翻到第 2 页的链接")
	}

	second := mailGet(t, engine, "/admin/mail?account_page=2")
	if !strings.Contains(second, "账号20") {
		t.Errorf("第 2 页应显示第 21 个账号（此前它永远点不到）：%s", mailHead(second))
	}
	if !strings.Contains(second, "共 21 条，第 21-21 条") {
		t.Errorf("第 2 页的分页信息应是「第 21-21 条」：%s", mailHead(second))
	}
}

func TestMailPageIndependentPagination(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	engine := newMailPaginationRouter(t, f)
	f.seedAccounts(t, 21)
	for i := 0; i < 41; i++ {
		_, err := f.svc.UpsertTemplate(context.Background(), &maildto.SaveTemplateReq{
			TemplateKey: fmt.Sprintf("list_tpl_%02d", i), Name: "分页模板", Subject: "主题", BodyHTML: "<p>x</p>",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	accountBody := mailGet(t, engine, "/admin/mail?account_page=2")
	if !strings.Contains(accountBody, "账号20") {
		t.Fatalf("账号页第 2 页应显示第 21 个账号：%s", mailHead(accountBody))
	}
	if strings.Contains(accountBody, "list_tpl_40") {
		t.Fatal("账号页不应再渲染模板表（模板已拆到 /admin/mail/templates）")
	}
	tplBody := mailGet(t, engine, "/admin/mail/templates?page=3")
	if !strings.Contains(tplBody, "list_tpl_40") {
		t.Fatalf("模板页第 3 页应显示最后一个模板：%s", mailHead(tplBody))
	}

	assertNoQueryParam(t, accountBody, "template_page")
	assertNoQueryParam(t, tplBody, "account_page")

	if got := mailGet(t, engine, "/admin/mail?account_page=999"); !strings.Contains(got, "账号20") {
		t.Fatal("账号页越界应收敛到末页")
	}
	if got := mailGet(t, engine, "/admin/mail/templates?page=999"); !strings.Contains(got, "list_tpl_40") {
		t.Fatal("模板页越界应收敛到末页")
	}
}

func assertNoQueryParam(t *testing.T, body, param string) {
	t.Helper()
	for _, fragment := range strings.Split(body, `href="`)[1:] {
		href := strings.SplitN(fragment, `"`, 2)[0]
		u, err := url.Parse(htmlUnescape(href))
		if err == nil && u.Query().Has(param) {
			t.Errorf("链接不应带 %s 参数（拆页后两个列表各自翻页）：%s", param, href)
		}
	}
}

func TestMailListDatabasePageFilters(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.seedAccounts(t, 21)
	m := mailmodel.NewMailModel(f.db)
	accRows, accTotal, accPage, err := m.ListAccountsPage(ctx, mailmodel.AccountPurposeMarketing, false, 3, 20)
	if err != nil || accTotal != 0 || accPage != 1 || len(accRows) != 0 {
		t.Fatalf("空筛选应保持空态：total=%d page=%d rows=%d err=%v", accTotal, accPage, len(accRows), err)
	}
	accRows, accTotal, accPage, err = m.ListAccountsPage(ctx, mailmodel.AccountPurposeTransactional, false, 99, 20)
	if err != nil || accTotal != 21 || accPage != 2 || len(accRows) != 1 {
		t.Fatalf("账号筛选末页应与计数一致：total=%d page=%d rows=%d err=%v", accTotal, accPage, len(accRows), err)
	}
	for i := 0; i < 21; i++ {
		key := "other"
		if i < 2 {
			key = "matched"
		}
		if _, err := f.svc.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
			TemplateKey: key, Locale: fmt.Sprintf("l%02d", i), Subject: "主题", BodyHTML: "<p>x</p>",
		}); err != nil {
			t.Fatal(err)
		}
	}
	tplRows, tplTotal, tplPage, err := m.ListTemplatesPage(ctx, "matched", 99, 1)
	if err != nil || tplTotal != 2 || tplPage != 2 || len(tplRows) != 1 || tplRows[0].TemplateKey != "matched" {
		t.Fatalf("模板筛选末页应与计数一致：total=%d page=%d rows=%v err=%v", tplTotal, tplPage, tplRows, err)
	}
	tplRows, tplTotal, tplPage, err = m.ListTemplatesPage(ctx, "missing", 3, 1)
	if err != nil || tplTotal != 0 || tplPage != 1 || len(tplRows) != 0 {
		t.Fatalf("模板空筛选应保持空态：total=%d page=%d rows=%v err=%v", tplTotal, tplPage, tplRows, err)
	}
}

func TestMailMarketingContactsPaginationKeepsFilter(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	engine := newMailPaginationRouter(t, f)
	ctx := context.Background()
	for i := 0; i < 51; i++ {
		if err := f.seedContact(ctx, fmt.Sprintf("bulk%02d@example.com", i), fmt.Sprintf("批量%d", i),
			mailmodel.ContactStatusSubscribed, "bulk"); err != nil {
			t.Fatalf("造第 %d 个联系人失败：%v", i, err)
		}
	}

	if err := f.seedContact(ctx, "vip@example.com", "VIP", mailmodel.ContactStatusSubscribed, "vip"); err != nil {
		t.Fatalf("造 VIP 联系人失败：%v", err)
	}

	page1 := mailGet(t, engine, "/admin/mail/contacts?keyword=bulk")
	if !strings.Contains(page1, `class="pagination"`) {
		t.Fatal("51 个匹配联系人应出现分页条")
	}

	plain1 := htmlUnescape(page1)
	if !strings.Contains(plain1, "keyword=bulk&") && !strings.Contains(plain1, "keyword=bulk&amp;") {
		t.Errorf("翻页链接应保留关键词筛选：%s", plain1)
	}
	if strings.Contains(page1, "vip@example.com") {
		t.Error("关键词筛选应把不匹配的 VIP 联系人挡住（它是客户端唯一的筛后结果校验）")
	}

	page2 := mailGet(t, engine, "/admin/mail/contacts?keyword=bulk&page=2")

	if !strings.Contains(page2, "bulk00@example.com") {
		t.Errorf("第 2 页应显示第 51 个联系人（此前翻页控件不存在）：%s", mailHead(page2))
	}
	if !strings.Contains(htmlUnescape(page2), "keyword=bulk") {
		t.Error("第 2 页的分页链接应继续保留关键词")
	}

	overshoot := mailGet(t, engine, "/admin/mail/contacts?keyword=bulk&page=9")
	if strings.Contains(overshoot, "没有匹配的联系人") {
		t.Errorf("页码越界时不应渲染空态（数据其实有 51 条）：%s", mailHead(overshoot))
	}
	if !strings.Contains(overshoot, "bulk00@example.com") {
		t.Errorf("页码越界应收敛到最后一页并显示真实数据：%s", mailHead(overshoot))
	}
}

func TestMailCampaignRecipientsPagination(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	engine := newMailPaginationRouter(t, f)
	campaignID := f.seedCampaignWithLogs(t, 51)
	page1Path := fmt.Sprintf("/admin/mail/campaign?id=%d", campaignID)

	page1 := mailGet(t, engine, page1Path)
	if !strings.Contains(page1, `class="pagination"`) {
		t.Fatalf("51 条收件人应出现分页条（此前只有一句「第 N 页」文本）：%s", mailHead(page1))
	}
	if !strings.Contains(page1, "共 51 条") {
		t.Errorf("分页条应给出真实总数：%s", mailHead(page1))
	}
	if !strings.Contains(htmlUnescape(page1), fmt.Sprintf("campaign?id=%d&page=2", campaignID)) {
		t.Error("翻页链接必须带活动 id，否则翻页会丢掉「在看哪条活动」")
	}

	page2 := mailGet(t, engine, page1Path+"&page=2")
	if !strings.Contains(page2, "共 51 条，第 51-51 条") {
		t.Errorf("第 2 页只应有最后一条：%s", mailHead(page2))
	}

	overshoot := mailGet(t, engine, page1Path+"&page=9")
	if strings.Contains(overshoot, "还没有投递记录") {
		t.Errorf("页码越界时不应渲染空态（51 条投递记录确实存在）：%s", mailHead(overshoot))
	}
}

func (f *mailFixture) seedCampaignWithLogs(t *testing.T, n int) uint64 {
	t.Helper()
	ctx := context.Background()
	acc, err := f.svc.CreateAccount(ctx, &maildto.SaveAccountReq{
		Name: "报表账号", Purpose: mailmodel.AccountPurposeMarketing,
		FromEmail: "report@example.com", Host: "smtp.example.com", Port: 587, Password: "p",
	})
	if err != nil {
		t.Fatalf("造发信账号失败：%v", err)
	}
	tpl, err := f.svc.UpsertTemplate(ctx, &maildto.SaveTemplateReq{
		TemplateKey: "report_tpl", Name: "报表模板", Subject: "主题", BodyHTML: "<p>x</p>",
	})
	if err != nil {
		t.Fatalf("造模板失败：%v", err)
	}
	campaign, err := f.svc.SaveCampaign(ctx, &maildto.SaveCampaignReq{
		Name: "九月群发", AccountID: acc.ID, TemplateID: tpl.ID, Subject: "九月上新",
	})
	if err != nil {
		t.Fatalf("建活动失败：%v", err)
	}
	now := time.Now()
	for i := 0; i < n; i++ {
		row := &mailmodel.MailLogEntity{
			ToEmail:    fmt.Sprintf("recipient%02d@example.com", i),
			Status:     "sent",
			CampaignID: &campaign.ID,
			SentAt:     &now,
		}
		if err := f.db.Create(row).Error; err != nil {
			t.Fatalf("写第 %d 条投递日志失败：%v", i, err)
		}
	}
	return campaign.ID
}

func htmlUnescape(s string) string {
	return strings.NewReplacer("&amp;", "&", "&#34;", `"`, "&quot;", `"`).Replace(s)
}

func mailPaginationNav(t *testing.T, body string) []struct{ Text, Href string } {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("解析页面 HTML 失败: %v", err)
	}
	var nav *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if nav != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "nav" {
			for _, a := range n.Attr {
				if a.Key == "class" && strings.Contains(a.Val, "pagination") {
					nav = n
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if nav == nil {
		t.Fatal("页面里没有分页条（<nav class=\"pagination\">）")
	}
	var out []struct{ Text, Href string }
	var collect func(*html.Node)
	collect = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "a" || n.Data == "span") {
			entry := struct{ Text, Href string }{}
			for _, a := range n.Attr {
				if a.Key == "href" {
					entry.Href = a.Val
				}
			}
			var text strings.Builder
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.TextNode {
					text.WriteString(c.Data)
				}
			}
			entry.Text = strings.TrimSpace(text.String())
			if entry.Text != "" {
				out = append(out, entry)
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collect(c)
		}
	}
	collect(nav)
	if len(out) == 0 {
		t.Fatal("分页条里没有可断言的分页项")
	}
	return out
}

func TestMailTemplatesPagePaginationLinksCarryPageParam(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	engine := newMailPaginationRouter(t, f)
	for i := 0; i < 41; i++ {
		if _, err := f.svc.UpsertTemplate(context.Background(), &maildto.SaveTemplateReq{
			TemplateKey: fmt.Sprintf("pg_tpl_%02d", i), Name: "分页模板", Subject: "主题", BodyHTML: "<p>x</p>",
		}); err != nil {
			t.Fatal(err)
		}
	}

	body := mailGet(t, engine, "/admin/mail/templates?page=2")
	nav := mailPaginationNav(t, body)

	var active *struct{ Text, Href string }
	for i := range nav {
		if nav[i].Text == "2" {
			active = &nav[i]
		}
	}
	if active == nil {
		t.Fatalf("第 2 页应有一个「2」分页项，实际 %+v", nav)
	}
	if active.Href != "" {
		t.Errorf("当前页不应是链接（会把用户送回同一页）：%q", active.Href)
	}

	wants := map[string]string{"上一页": "page=1", "下一页": "page=3"}
	for _, e := range nav {
		want, ok := wants[e.Text]
		if !ok {
			continue
		}
		if e.Href == "" {
			t.Errorf("%q 应是可点链接（修复前它指向不带页码的基址）", e.Text)
			continue
		}
		u, err := url.Parse(htmlUnescape(e.Href))
		if err != nil {
			t.Fatalf("解析分页链接失败 %q: %v", e.Href, err)
		}
		if got := u.Query().Get("page"); got != want[5:] {
			t.Errorf("%q 应指向 %s，实际 %q", e.Text, want, e.Href)
		}

		if u.Query().Has("limit") {
			t.Errorf("%q 链接不应带 limit：%q", e.Text, e.Href)
		}
		if u.Query().Has("account_page") {
			t.Errorf("%q 链接不应带另一张表的页码参数：%q", e.Text, e.Href)
		}
	}
}

func TestMailPaginationLinksKeepPageFilters(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	engine := newMailPaginationRouter(t, f)

	for i := 0; i < 120; i++ {
		if err := f.seedContact(context.Background(),
			fmt.Sprintf("vip%03d@example.com", i), "常客", mailmodel.ContactStatusSubscribed, ""); err != nil {
			t.Fatalf("造第 %d 个联系人失败：%v", i, err)
		}
	}

	body := mailGet(t, engine, "/admin/mail/contacts?keyword=vip")
	nav := mailPaginationNav(t, body)

	links := 0
	for _, e := range nav {
		if e.Href == "" {
			continue
		}
		links++
		u, err := url.Parse(htmlUnescape(e.Href))
		if err != nil {
			t.Fatalf("解析分页链接失败 %q: %v", e.Href, err)
		}
		if got := u.Query().Get("keyword"); got != "vip" {
			t.Errorf("%q 的分页链接丢了筛选 keyword（实际 %q）：%s", e.Text, got, e.Href)
		}
	}
	if links < 2 {
		t.Fatalf("应有多个可点分页链接（上一页/下一页/其它页码），实际 %d 条：%+v", links, nav)
	}

	assertNoQueryParam(t, body, "account_page")
}
