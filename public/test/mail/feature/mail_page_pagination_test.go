package feature

// mail_page_pagination_test.go — 邮箱后台列表分页的端到端守卫。
//
// 邮箱设置页两表分别使用数据库分页与独立页码；营销页和活动页维持既有分页契约。
//
// 断言都是「用户可观察的行为」：第 51 条数据能不能翻到、翻页后筛选还在不在、
// 页码越界时表格会不会被渲染成「还没有数据」的空态（后一条是这次改动最危险的回归：
// 服务端分页不收敛，越界返回空列表 + 真实 total）。

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	maildto "go_wp/internal/module/mail/dto"
	mailhttp "go_wp/internal/module/mail/inbound/http"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/internal/templates"
)

// newMailPaginationRouter 挂本批涉及的三条页面路由（不经三层鉴权链：断言的是渲染结果）。
func newMailPaginationRouter(t *testing.T, f *mailFixture) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// Jet 是流式渲染：出错时响应码仍是 200、正文可能整块截断 —— 打进日志才看得见。
	engine.Use(func(c *gin.Context) {
		c.Next()
		for _, ge := range c.Errors {
			t.Logf("gin 渲染错误: %v", ge.Err)
		}
	})
	engine.HTMLRender = templates.NewJetHTMLRender("../../../../internal/templates", true)
	h := mailhttp.NewMailPageHandle(f.svc)
	engine.GET("/admin/mail", h.MailPage)
	engine.GET("/admin/mail/templates", h.MailTemplatesPage)
	engine.GET("/admin/mail/contacts", h.MailContactsPage)
	engine.GET("/admin/mail/campaign", h.MailCampaignPage)
	return engine
}

// mailGet 发一个页面 GET 并回正文（页面路由只可能 200 / 302）。
func mailGet(t *testing.T, engine *gin.Engine, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s 应回 200，实际 %d（Location=%s）", path, rec.Code, rec.Header().Get("Location"))
	}
	return rec.Body.String()
}

// seedAccounts 造 n 个发信账号（名称与发件人唯一，避免撞唯一键）。
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

// TestMailPageAccountsPagination 发信账号表：超过一页时出现分页条，第 2 页能看到最后一条。
func TestMailPageAccountsPagination(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	engine := newMailPaginationRouter(t, f)
	f.seedAccounts(t, 21) // 每页 20 条 → 第 2 页只有 1 条

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

// TestMailPageIndependentPagination 拆页后账号页（account_page）与模板页（page）各自计数、互不带页码。
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
	// 拆页判据：两个列表各自翻页，任何一页的链接都不能带上另一个列表的页码参数，
	// 否则用户在账号页翻页会把模板列表也一起翻走（这正是拆页前必须切断的耦合）。
	assertNoQueryParam(t, accountBody, "template_page")
	assertNoQueryParam(t, tplBody, "account_page")
	// 越界页各自收敛到末页：服务端分页不收敛会渲染成空态 + 真实 total，用户以为数据没了。
	if got := mailGet(t, engine, "/admin/mail?account_page=999"); !strings.Contains(got, "账号20") {
		t.Fatal("账号页越界应收敛到末页")
	}
	if got := mailGet(t, engine, "/admin/mail/templates?page=999"); !strings.Contains(got, "list_tpl_40") {
		t.Fatal("模板页越界应收敛到末页")
	}
}

// assertNoQueryParam 断言页面里任何 href 都不带指定 query 参数。
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

// TestMailListDatabasePageFilters 计数与行查询共享筛选，空结果和越界页不回退为全量。
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

// TestMailMarketingContactsPaginationKeepsFilter 联系人表：翻页保留筛选，且页码越界不空表。
func TestMailMarketingContactsPaginationKeepsFilter(t *testing.T) {
	f := newMailFeatureFixture(t)
	if f == nil {
		return
	}
	engine := newMailPaginationRouter(t, f)
	ctx := context.Background()
	for i := 0; i < 51; i++ { // 每页 50 条 → 第 2 页只有 1 条
		if err := f.seedContact(ctx, fmt.Sprintf("bulk%02d@example.com", i), fmt.Sprintf("批量%d", i),
			mailmodel.ContactStatusSubscribed, "bulk"); err != nil {
			t.Fatalf("造第 %d 个联系人失败：%v", i, err)
		}
	}
	// 一个带筛选的账号：确认翻页链接会把它带上（关键词筛选命中 51 条中的一条）。
	if err := f.seedContact(ctx, "vip@example.com", "VIP", mailmodel.ContactStatusSubscribed, "vip"); err != nil {
		t.Fatalf("造 VIP 联系人失败：%v", err)
	}

	page1 := mailGet(t, engine, "/admin/mail/contacts?keyword=bulk")
	if !strings.Contains(page1, `class="pagination"`) {
		t.Fatal("51 个匹配联系人应出现分页条")
	}
	// Jet 会把 href 里的 & 转义成 &amp;，断言前先反转义（否则会把「转义」误判成「丢参数」）。
	plain1 := htmlUnescape(page1)
	if !strings.Contains(plain1, "keyword=bulk&") && !strings.Contains(plain1, "keyword=bulk&amp;") {
		t.Errorf("翻页链接应保留关键词筛选：%s", plain1)
	}
	if strings.Contains(page1, "vip@example.com") {
		t.Error("关键词筛选应把不匹配的 VIP 联系人挡住（它是客户端唯一的筛后结果校验）")
	}

	page2 := mailGet(t, engine, "/admin/mail/contacts?keyword=bulk&page=2")
	// 联系人按 id 倒序（最新在前）：第 1 页是 bulk50…bulk01，第 2 页只剩最早的那条 bulk00。
	if !strings.Contains(page2, "bulk00@example.com") {
		t.Errorf("第 2 页应显示第 51 个联系人（此前翻页控件不存在）：%s", mailHead(page2))
	}
	if !strings.Contains(htmlUnescape(page2), "keyword=bulk") {
		t.Error("第 2 页的分页链接应继续保留关键词")
	}

	// 页码越界：联系人页数少的那一侧被收敛，**不能**渲染成「没有匹配的联系人」的空态。
	overshoot := mailGet(t, engine, "/admin/mail/contacts?keyword=bulk&page=9")
	if strings.Contains(overshoot, "没有匹配的联系人") {
		t.Errorf("页码越界时不应渲染空态（数据其实有 51 条）：%s", mailHead(overshoot))
	}
	if !strings.Contains(overshoot, "bulk00@example.com") {
		t.Errorf("页码越界应收敛到最后一页并显示真实数据：%s", mailHead(overshoot))
	}
}

// TestMailCampaignRecipientsPagination 收件人明细：假分页（只有「第 N 页」文本）→ 真分页条。
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

	// 页码越界同样不能渲染成空态（收件人明细为空 = 「还没有投递记录」，那句话是错的）。
	overshoot := mailGet(t, engine, page1Path+"&page=9")
	if strings.Contains(overshoot, "还没有投递记录") {
		t.Errorf("页码越界时不应渲染空态（51 条投递记录确实存在）：%s", mailHead(overshoot))
	}
}

// seedCampaignWithLogs 造一条活动并写入 n 条投递日志（收件人明细的行来源是 mail_logs）。
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

// htmlUnescape 反转义 HTML 实体（Jet 会把 href 里的 & 写成 &amp;）。
func htmlUnescape(s string) string {
	return strings.NewReplacer("&amp;", "&", "&#34;", `"`, "&quot;", `"`).Replace(s)
}
