// mail_marketing_page_handle.go — 后台营销页（issue #37）：联系人与群发活动。
//
// 与邮箱配置页分开：日常操作营销的人不需要看到 SMTP 配置。
// 页面语义上反复强调两件事：
//
//	· 只发给**已订阅**的人（pending 未确认同意的绝不发）—— 合规底线；
//	· 导入时「同意声明」不是 UI 便利，而是**留痕**：谁在什么时候声明过什么。
package mailhttp

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	maildto "go_wp/internal/module/mail/dto"
	"go_wp/internal/web/shell"
	"go_wp/pkg/response"
)

// mailMarketingPageSize 联系人 / 活动每页条数。
const mailMarketingPageSize = 50

// MailMarketingPage 联系人与群发活动页。
func (h *mailPageHandle) MailMarketingPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := int(shell.ParseUint(c.Query("page")))
	if page <= 0 {
		page = 1
	}
	contacts, err := h.mail.ListContacts(ctx, &maildto.ContactFilterReq{
		Keyword:  c.Query("keyword"),
		Status:   c.Query("status"),
		Page:     page,
		PageSize: mailMarketingPageSize,
	})
	if err != nil {
		c.HTML(http.StatusOK, "admin/mail_marketing.html", shell.Prepare(c, gin.H{"title": "邮件营销", "Err": err.Error()}))
		return
	}
	campaigns, err := h.mail.ListCampaigns(ctx, &maildto.CampaignListReq{Page: 1, PageSize: mailMarketingPageSize})
	if err != nil {
		c.HTML(http.StatusOK, "admin/mail_marketing.html", shell.Prepare(c, gin.H{"title": "邮件营销", "Err": err.Error()}))
		return
	}
	accounts, _ := h.mail.ListAccounts(ctx, "")
	templates, _ := h.mail.ListTemplates(ctx, "")
	c.HTML(http.StatusOK, "admin/mail_marketing.html", shell.Prepare(c, gin.H{
		"title":        "邮件营销",
		"Contacts":     contacts.Items,
		"ContactTotal": contacts.Total,
		"Campaigns":    campaigns.Items,
		"Accounts":     accounts,
		"Templates":    templates,
		"Page":         page,
		"Keyword":      c.Query("keyword"),
		"Status":       c.Query("status"),
		"Err":          strings.TrimSpace(c.Query("err")),
		"Ok":           strings.TrimSpace(c.Query("ok")),
		// Done：批量动作的结论（全成功走 ?done=，有跳过走 ?err=）。模板用 isset 认这个可选键。
		"Done": strings.TrimSpace(c.Query("done")),
	}))
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
		c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(err.Error()))
		return
	}
	msg := "导入完成：新增 " + strconv.Itoa(res.Imported) + "，更新 " + strconv.Itoa(res.Updated) +
		"，跳过 " + strconv.Itoa(res.Skipped) + "，抑制名单命中 " + strconv.Itoa(res.Suppressed) +
		"，非法 " + strconv.Itoa(len(res.Errors))
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
		c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(err.Error()))
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
		c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(err.Error()))
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
		c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(err.Error()))
		return
	}
	msg := "活动已开始发送，目标 " + strconv.FormatInt(res.Total, 10) + " 人；进度可在下方列表刷新查看。"
	c.Redirect(http.StatusFound, "/admin/mail/marketing?ok="+urlQueryEscape(msg))
}

// MailCampaignPage 活动报表页（打开 / 点击 / 退订与收件人明细）。
//
// 页面上把「打开率是估算」写清楚：多数客户端默认不加载图片（漏报），Apple Mail 还会代理预取
// （虚高）。点击 / 退信 / 退订这三个数是准的，运营决策该靠它们。
func (h *mailPageHandle) MailCampaignPage(c *gin.Context) {
	ctx := c.Request.Context()
	id := shell.ParseUint(c.Query("id"))
	page := int(shell.ParseUint(c.Query("page")))
	if page <= 0 {
		page = 1
	}
	report, err := h.mail.CampaignReport(ctx, id, page, mailMarketingPageSize)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(err.Error()))
		return
	}
	c.HTML(http.StatusOK, "admin/mail_campaign.html", shell.Prepare(c, gin.H{
		"title": "活动报表",
		"R":     report,
		"Page":  page,
		"Err":   c.Query("err"),
	}))
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
		c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(err.Error()))
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
			mailMarketingBackParams, "", "目标状态不合法，本次没有处理任何联系人。"))
		return
	}
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail/marketing", mailMarketingBackParams, "", berr.Error()))
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
		c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail/marketing", mailMarketingBackParams, "", berr.Error()))
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
