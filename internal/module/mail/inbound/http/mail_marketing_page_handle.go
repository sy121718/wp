// mail_marketing_page_handle.go — 后台营销页（issue #37）：联系人与群发活动。
//
// 与邮箱配置页分开：日常操作营销的人不需要看到 SMTP 配置。
// 页面语义上反复强调两件事：
//
//	· 只发给**已订阅**的人（pending 未确认同意的绝不发）—— 合规底线；
//	· 导入时「同意声明」不是 UI 便利，而是**留痕**：谁在什么时候声明过什么。
package mailhttp

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	maildto "go_wp/internal/module/mail/dto"
	"go_wp/internal/web/shell"
	"go_wp/pkg/response"
)

// mailMarketingPageSize 联系人 / 活动每页条数。
const mailMarketingPageSize = 50

// mailMarketingClampPage 把页码收敛到两张表都有效的范围（取两者中更小的最大页号）。
//
// 为什么需要它：本页两张表共用 ?page=（分页组件的链接参数名写死 page/limit，同一页里
// 无法给两张表各带一个页码），而 mail 服务端分页**不收敛** —— 页码越界时返回的是
// 「空列表 + 真实 total」（见 service/mail_contact.go 与 mail_campaign.go 的 offset 计算）。
// 不收敛就会把「有 3 个活动、只是页码落到第 5 页」渲染成「还没有活动」的空态，
// 而那句话是错的。收敛后两张表的页码、数据与分页条三者自洽。
//
// 两张表都没有数据时收敛到第 1 页：此时页码没有任何含义（分页条也不会渲染），
// 留着 ?page=7 只是把一个无效状态写进 URL。
func mailMarketingClampPage(page int, totals ...int64) int {
	if page < 1 {
		page = 1
	}
	maxPage := 0
	for _, total := range totals {
		if total <= 0 {
			continue
		}
		pages := int((total + mailMarketingPageSize - 1) / mailMarketingPageSize)
		if pages < 1 {
			pages = 1
		}
		if maxPage == 0 || pages < maxPage {
			maxPage = pages
		}
	}
	if maxPage == 0 {
		return 1
	}
	if page > maxPage {
		return maxPage
	}
	return page
}

// MailMarketingPage 联系人与群发活动页。
func (h *mailPageHandle) MailMarketingPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := mailPageNumber(c.Query("page"))
	keyword, status := c.Query("keyword"), c.Query("status")

	contacts, err := h.mail.ListContacts(ctx, &maildto.ContactFilterReq{
		Keyword:  keyword,
		Status:   status,
		Page:     page,
		PageSize: mailMarketingPageSize,
	})
	if err != nil {
		c.HTML(http.StatusOK, "admin/mail/mail_marketing.html", shell.Prepare(c, mailMarketingErrData(c, err)))
		return
	}
	campaigns, err := h.mail.ListCampaigns(ctx, &maildto.CampaignListReq{Page: page, PageSize: mailMarketingPageSize})
	if err != nil {
		c.HTML(http.StatusOK, "admin/mail/mail_marketing.html", shell.Prepare(c, mailMarketingErrData(c, err)))
		return
	}
	// 页号收敛（见 mailMarketingClampPage）：越界时用收敛后的页码重取一次。
	// 只在越界这一种情况下多两次查询，正常翻页仍然是原来的两次。
	if fixed := mailMarketingClampPage(page, contacts.Total, campaigns.Total); fixed != page {
		page = fixed
		if contacts, err = h.mail.ListContacts(ctx, &maildto.ContactFilterReq{
			Keyword: keyword, Status: status, Page: page, PageSize: mailMarketingPageSize,
		}); err != nil {
			c.HTML(http.StatusOK, "admin/mail/mail_marketing.html", shell.Prepare(c, mailMarketingErrData(c, err)))
			return
		}
		if campaigns, err = h.mail.ListCampaigns(ctx, &maildto.CampaignListReq{
			Page: page, PageSize: mailMarketingPageSize,
		}); err != nil {
			c.HTML(http.StatusOK, "admin/mail/mail_marketing.html", shell.Prepare(c, mailMarketingErrData(c, err)))
			return
		}
	}
	accounts, _ := h.mail.ListAccounts(ctx, "")
	templates, _ := h.mail.ListTemplates(ctx, "")

	data := shell.Prepare(c, gin.H{
		"title":         "邮件营销",
		"Contacts":      contacts.Items,
		"ContactTotal":  contacts.Total,
		"Campaigns":     campaigns.Items,
		"CampaignTotal": campaigns.Total,
		"Accounts":      accounts,
		"Templates":     templates,
		"Page":          page,
		"Keyword":       keyword,
		"Status":        status,
		// 读侧回执一律经 mail_err.go 的白名单出口：查询参数不是可信边界。
		"Err": mailPageErr(c),
		"Ok":  mailPageOk(c),
		// Done：批量动作的结论（全成功走 ?done=，有跳过走 ?err=）。模板用 isset 认这个可选键。
		"Done": mailPageDone(c),
	})
	// 两张表的分页数据分开命名（Contacts* / Campaigns*）：模板两次 include 分页片段时
	// 各传一份 context。基地址带上筛选，翻页才能保留筛选条件（否则翻到第 2 页就回到全量）。
	base := shell.FilterBaseURL("/admin/mail/marketing", map[string]string{"keyword": keyword, "status": status})
	for k, v := range shell.BuildPagination(contacts.Total, page, mailMarketingPageSize,
		base, shell.TranslateFor(c)).TemplateKeys() {
		data["Contacts"+k] = v
	}
	for k, v := range shell.BuildPagination(campaigns.Total, page, mailMarketingPageSize,
		base, shell.TranslateFor(c)).TemplateKeys() {
		data["Campaigns"+k] = v
	}
	c.HTML(http.StatusOK, "admin/mail/mail_marketing.html", data)
}

// mailMarketingErrData 取数失败时的页面数据：归口文案 + 让模板能整页渲染完的空值。
//
// 为什么空值不是可选的：admin/mail_marketing.html 在提示条之后就用 .Keyword / .Status /
// .Page / .ContactTotal / len(.Contacts) / len(.Campaigns) 渲染列表，缺键会让 Jet
// **在那一行中断**（HTTP 仍是 200、正文整块消失）。只注入 Err 的分支因此渲染不完，
// 运营连那句归口文案都只能看到半页。
func mailMarketingErrData(c *gin.Context, err error) gin.H {
	return gin.H{
		"title":        "邮件营销",
		"Err":          mailErrPageText(c, err),
		"Contacts":     []any{},
		"ContactTotal": int64(0),
		"Campaigns":    []any{},
		"Accounts":     []any{},
		"Templates":    []any{},
		"Page":         1,
		"Keyword":      "",
		"Status":       "",
	}
}

// MailContactImport 导入联系人。
//
// 页面上「同意声明」与「同意来源」是两个必填判断：勾了才置 subscribed 并留痕，
// 没勾则一律 pending（不可发营销）。这不是 UI 便利，是合规留痕。
func (h *mailPageHandle) MailContactImport(c *gin.Context) {
	req := &maildto.ImportContactsReq{
		Content:         []byte(c.PostForm("content")),
		ConsentDeclared: c.PostForm("consent_declared") == "on",
		ConsentSource:   c.PostForm("consent_source"),
		UpdateExisting:  c.PostForm("update_existing") == "on",
	}
	if tags := strings.TrimSpace(c.PostForm("tags")); tags != "" {
		for _, t := range strings.Split(tags, ",") {
			if v := strings.TrimSpace(t); v != "" {
				req.DefaultTags = append(req.DefaultTags, v)
			}
		}
	}
	res, err := h.mail.ImportContacts(c.Request.Context(), req)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	// 与 mail_err.go 的 mailCountedNoticeTemplates[2] 同形（数字归一后可判定）。
	msg := fmt.Sprintf("导入完成：新增 %d，更新 %d，跳过 %d，抑制名单命中 %d，非法 %d",
		res.Imported, res.Updated, res.Skipped, res.Suppressed, len(res.Errors))
	c.Redirect(http.StatusFound, "/admin/mail/marketing?ok="+urlQueryEscape(msg))
}

// MailContactStatus 改联系人同意状态。
func (h *mailPageHandle) MailContactStatus(c *gin.Context) {
	req := &maildto.UpdateContactStatusReq{
		ID:     shell.ParseUint(c.PostForm("id")),
		Status: c.PostForm("status"),
		Note:   c.PostForm("note"),
	}
	if err := h.mail.UpdateContactStatus(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail/marketing?ok=1")
}

// MailCampaignSave 保存群发活动。
func (h *mailPageHandle) MailCampaignSave(c *gin.Context) {
	req := &maildto.SaveCampaignReq{
		ID:         shell.ParseUint(c.PostForm("id")),
		Name:       c.PostForm("name"),
		AccountID:  shell.ParseUint(c.PostForm("account_id")),
		TemplateID: shell.ParseUint(c.PostForm("template_id")),
		Subject:    c.PostForm("subject"),
	}
	if tags := strings.TrimSpace(c.PostForm("target_tags")); tags != "" {
		for _, t := range strings.Split(tags, ",") {
			if v := strings.TrimSpace(t); v != "" {
				req.TargetTags = append(req.TargetTags, v)
			}
		}
	}
	if _, err := h.mail.SaveCampaign(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail/marketing?ok=1")
}

// MailCampaignStart 启动群发（HTTP 秒回；展开在后台分批进行）。
func (h *mailPageHandle) MailCampaignStart(c *gin.Context) {
	res, err := h.mail.StartCampaign(c.Request.Context(), &maildto.StartCampaignReq{
		CampaignID: shell.ParseUint(c.PostForm("id")),
	})
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	// 与 mail_err.go 的 mailCountedNoticeTemplates[3] 同形（数字归一后可判定）。
	msg := fmt.Sprintf("活动已开始发送，目标 %d 人；进度可在下方列表刷新查看。", res.Total)
	c.Redirect(http.StatusFound, "/admin/mail/marketing?ok="+urlQueryEscape(msg))
}

// MailCampaignPage 活动报表页（打开 / 点击 / 退订与收件人明细）。
//
// 页面上把「打开率是估算」写清楚：多数客户端默认不加载图片（漏报），Apple Mail 还会代理预取
// （虚高）。点击 / 退信 / 退订这三个数是准的，运营决策该靠它们。
//
// **缺 id 前置判定（审计 P0）**：本页同时是 sys_menus 里的正式菜单项
// （id=137「邮件活动」→ /admin/mail/campaign，path 不带参数），所以「没带 id」不是异常输入，
// 而是**点菜单的常规路径**。改前它直接调 service，靠查询失败兜底 —— 运营点菜单必看到
// 「参数不合法」，而本页没有任何参数可改（用户无出路）。
//
// 缺 id 的处理：回营销页（活动列表就在那里）并带一句**指名去哪选**的引导文案，
// 而不是静默 302（静默弹回才会让菜单看起来是坏的）。
// 为什么不就地渲染一张引导页：本页模板 mail_campaign.html 以完整报表数据为前提
// （`{{r := .R}}` → `{{c := r.Campaign}}`），无数据即整页中断（HTTP 仍是 200、正文整块消失）——
// 在缺少数据时渲染它等于给运营一张白页。
func (h *mailPageHandle) MailCampaignPage(c *gin.Context) {
	ctx := c.Request.Context()
	id, hasID := mailQueryID(c)
	if !hasID {
		c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(mailCampaignIDRequiredText))
		return
	}
	page := mailPageNumber(c.Query("page"))
	report, err := h.mail.CampaignReport(ctx, id, page, mailMarketingPageSize)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	// 页号收敛：报表服务端不收敛（页码越界时收件人明细为空、Total 仍是真值），
	// 不处理会把「这条活动有 300 个收件人、只是页码落到第 9 页」渲染成
	// 「还没有投递记录」的空态，同时分页条还显示第 9 页 —— 两者自相矛盾。
	if report.Total > 0 {
		if maxPage := int((report.Total + mailMarketingPageSize - 1) / mailMarketingPageSize); page > maxPage {
			page = maxPage
			if report, err = h.mail.CampaignReport(ctx, id, page, mailMarketingPageSize); err != nil {
				c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(mailErrPageText(c, err)))
				return
			}
		}
	}
	data := shell.Prepare(c, gin.H{
		"title": "活动报表",
		"R":     report,
		"Page":  page,
		"Err":   mailPageErr(c),
	})
	// 收件人明细的分页条：baseURL 带上 id，翻页时不会丢掉「在看哪条活动」。
	// 键名与 partials/pagination.html 读的键一致（该片段在这里用无参 include 渲染）。
	for k, v := range shell.BuildPagination(report.Total, page, mailMarketingPageSize,
		fmt.Sprintf("/admin/mail/campaign?id=%d", id), shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/mail/mail_campaign.html", data)
}

// MailCampaignReportJSON 报表数据接口（图表 / 外部核对用同一份口径）。
func (h *mailPageHandle) MailCampaignReportJSON(c *gin.Context) {
	page := int(shell.ParseUint(c.Query("page")))
	if page <= 0 {
		page = 1
	}
	report, err := h.mail.CampaignReport(c.Request.Context(), shell.ParseUint(c.Query("id")), page, mailMarketingPageSize)
	if err != nil {
		shell.PageErrorBadRequest(c, "mail_marketing", err)
		return

	}
	response.Success(c, report)
}

// MailCampaignDelete 删除活动。
func (h *mailPageHandle) MailCampaignDelete(c *gin.Context) {
	if err := h.mail.DeleteCampaign(c.Request.Context(), shell.ParseUint(c.PostForm("id"))); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail/marketing?ok=1")
}

// —— 批量动作（评审规则 admin-ui-logic §7：列表首列勾选 + 批量条）——
//
// 与配置页的批量删除同一形状（见 mail_page_handle.go 的 mailBulkOutcome / mailBulkLocation）：
// 逐条走**同一条单条路径**，失败只计跳过、不中断整批，结论按「成功 N / 跳过 M」回带列表页。
// 权限点一律复用对应单条动作的路径，不新增权限点、不写迁移。

// mailMarketingBackParams 批量动作回跳时带回的列表状态（表单字段名 → URL 参数名）。
//
// 两者刻意不同名：表单里的 status 是**目标状态**（批量改状态用），而列表筛选参数也叫
// status —— 同名直接回带会把筛选条件写成目标状态，于是「筛了已退订 → 批量改回订阅」
// 之后列表会莫名其妙变成筛选「订阅」。
var mailMarketingBackParams = [][2]string{
	{"returnKeyword", "keyword"},
	{"returnStatus", "status"},
	{"returnPage", "page"},
}

// MailContactsBulkStatus 批量改联系人订阅状态（POST /admin/mail/contacts/bulk-status）。
//
// 单条路径 = UpdateContactStatus，权限点复用 /api/mail/contact/status。
//
// 目标状态只收单条抽屉里的那三个（订阅 / 待确认 / 退订）：bounced / complained 是投递反馈的
// **事实记录**，不该由后台手工往那个方向改 —— 手工把联系人标成「硬退信」既没有投递证据，
// 又会顺带写进抑制名单。
// 批量不带「来源备注」：单条抽屉里那句备注是给一次人工操作留痕的，批量套用同一句来源，
// consent_source 里留下的会是与事实不符的记录。
func (h *mailPageHandle) MailContactsBulkStatus(c *gin.Context) {
	target := strings.TrimSpace(c.PostForm("status"))
	switch target {
	case "subscribed", "pending", "unsubscribed":
	default:
		c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail/marketing",
			mailMarketingBackParams, "", mailContactStatusBadText))
		return
	}
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail/marketing", mailMarketingBackParams, "", mailBulkIDsText(c, berr)))
		return
	}
	changed, skipped := 0, 0
	for _, raw := range ids {
		id := shell.ParseUint(raw)
		if id == 0 {
			skipped++
			continue
		}
		if err := h.mail.UpdateContactStatus(c.Request.Context(), &maildto.UpdateContactStatusReq{
			ID: id, Status: target,
		}); err != nil {
			skipped++
			continue
		}
		changed++
	}
	done, warn := mailBulkOutcome("更新", "联系人", changed, skipped)
	c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail/marketing", mailMarketingBackParams, done, warn))
}

// MailCampaignsBulkDelete 批量删除群发活动（POST /admin/mail/campaigns/bulk-delete）。
//
// 单条路径 = DeleteCampaign，权限点复用 /api/mail/campaign/delete。
// 发送中的活动由服务端拒绝（ErrCampaignSending）→ 只跳过它、其余照常删除：
// 删掉发送中的活动，后台分批展开的收件人任务会在中途找不到活动，留下一批半截记录。
func (h *mailPageHandle) MailCampaignsBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail/marketing", mailMarketingBackParams, "", mailBulkIDsText(c, berr)))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := shell.ParseUint(raw)
		if id == 0 {
			skipped++
			continue
		}
		if err := h.mail.DeleteCampaign(c.Request.Context(), id); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	done, warn := mailBulkOutcome("删除", "群发活动", deleted, skipped)
	c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail/marketing", mailMarketingBackParams, done, warn))
}
