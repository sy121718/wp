// mail_campaigns_page_handle.go — 群发活动页与活动报表页。
//
// 为什么从「邮件营销」页拆出来：联系人是**名单**、活动是**发送任务**，
// 两者读的人不同（管名单的人不必每次都扫一遍活动进度表），批量动作也不同。
// 拆开后本页只回答「有哪些活动」，每条活动的报表在 /admin/mail/campaign?id=N。
package mailhttp

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/response"
)

// mailCampaignsPageSize 活动每页条数。
const mailCampaignsPageSize = 50

// mailCampaignsClampPage 把页码收敛到有效范围（理由同 mailContactsClampPage：
// 服务端分页不收敛，越界会渲染出「还没有活动」的空态，而那句话是错的）。
func mailCampaignsClampPage(page int, total int64) int {
	if page < 1 {
		return 1
	}
	if total <= 0 {
		return 1
	}
	maxPage := int((total + mailCampaignsPageSize - 1) / mailCampaignsPageSize)
	if maxPage < 1 {
		maxPage = 1
	}
	if page > maxPage {
		return maxPage
	}
	return page
}

// MailCampaignsPage 群发活动列表页（新建活动抽屉 + 表 + 批量删除 + 分页）。
func (h *mailPageHandle) MailCampaignsPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := shell.TranslateFor(c)
	page := mailPageNumber(c.Query("page"))

	campaigns, err := h.mail.ListCampaigns(ctx, &maildto.CampaignListReq{Page: page, PageSize: mailCampaignsPageSize})
	if err != nil {
		c.HTML(http.StatusOK, "admin/mail/mail_campaigns.html", shell.Prepare(c, mailCampaignsErrData(c, err)))
		return
	}
	if fixed := mailCampaignsClampPage(page, campaigns.Total); fixed != page {
		page = fixed
		if campaigns, err = h.mail.ListCampaigns(ctx, &maildto.CampaignListReq{Page: page, PageSize: mailCampaignsPageSize}); err != nil {
			c.HTML(http.StatusOK, "admin/mail/mail_campaigns.html", shell.Prepare(c, mailCampaignsErrData(c, err)))
			return
		}
	}
	// 新建活动抽屉要在页内选发信账号与模板：两份选项列表随页提供（失败不阻塞列表，
	// 账号 / 模板列表取不到时抽屉里的下拉就是空的，列表本身仍然可用）。
	accounts, _ := h.mail.ListAccounts(ctx, "")
	templates, _ := h.mail.ListTemplates(ctx, "")

	data := shell.Prepare(c, gin.H{
		"title":         mailLabel(tr, mailenums.PageTitleCampaigns),
		"Campaigns":     campaigns.Items,
		"CampaignTotal": campaigns.Total,
		"Accounts":      accounts,
		"Templates":     templates,
		"Page":          page,
		// 读侧回执一律经 mail_err.go 的白名单出口：查询参数不是可信边界。
		"Err": mailPageErr(c),
		"Ok":  mailPageOk(c),
		// Done：批量动作的结论（全成功走 ?done=，有跳过走 ?err=）。
		"Done": mailPageDone(c),
	})
	for k, v := range shell.BuildPagination(campaigns.Total, page, mailCampaignsPageSize,
		"/admin/mail/campaigns", tr).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/mail/mail_campaigns.html", data)
}

// mailCampaignsErrData 取数失败时的页面数据。
//
// 空值不是可选的：admin/mail/mail_campaigns.html 在提示条之后就用 .Page / .CampaignTotal /
// len(.Campaigns) 渲染列表，缺键会让 Jet **在那一行中断**（HTTP 仍是 200、正文整块消失）。
func mailCampaignsErrData(c *gin.Context, err error) gin.H {
	return gin.H{
		"title":         mailLabel(shell.TranslateFor(c), mailenums.PageTitleCampaigns),
		"Err":           mailErrPageText(c, err),
		"Campaigns":     []any{},
		"CampaignTotal": int64(0),
		"Accounts":      []any{},
		"Templates":     []any{},
		"Page":          1,
	}
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
		c.Redirect(http.StatusFound, "/admin/mail/campaigns?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail/campaigns?ok=1")
}

// MailCampaignStart 启动群发（HTTP 秒回；展开在后台分批进行）。
func (h *mailPageHandle) MailCampaignStart(c *gin.Context) {
	res, err := h.mail.StartCampaign(c.Request.Context(), &maildto.StartCampaignReq{
		CampaignID: shell.ParseUint(c.PostForm("id")),
	})
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/campaigns?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	// 与 mail_err.go 的 mailCountedNoticeTemplates[3] 同形（数字归一后可判定）。
	msg := fmt.Sprintf("活动已开始发送，目标 %d 人；进度可在下方列表刷新查看。", res.Total)
	c.Redirect(http.StatusFound, "/admin/mail/campaigns?ok="+urlQueryEscape(msg))
}

// MailCampaignDelete 删除活动。
func (h *mailPageHandle) MailCampaignDelete(c *gin.Context) {
	if err := h.mail.DeleteCampaign(c.Request.Context(), shell.ParseUint(c.PostForm("id"))); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/campaigns?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail/campaigns?ok=1")
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
// 缺 id 的处理：回活动列表（那里有「报表」入口）并带一句**指名去哪选**的引导文案，
// 而不是静默 302（静默弹回才会让菜单看起来是坏的）。
// 为什么不就地渲染一张引导页：本页模板 mail_campaign.html 以完整报表数据为前提
// （`{{r := .R}}` → `{{c := r.Campaign}}`），无数据即整页中断（HTTP 仍是 200、正文整块消失）——
// 在缺少数据时渲染它等于给运营一张白页。
func (h *mailPageHandle) MailCampaignPage(c *gin.Context) {
	ctx := c.Request.Context()
	id, hasID := mailQueryID(c)
	if !hasID {
		c.Redirect(http.StatusFound, "/admin/mail/campaigns?err="+urlQueryEscape(mailCampaignIDRequiredText))
		return
	}
	page := mailPageNumber(c.Query("page"))
	report, err := h.mail.CampaignReport(ctx, id, page, mailCampaignsPageSize)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/campaigns?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	// 页号收敛：报表服务端不收敛（页码越界时收件人明细为空、Total 仍是真值），
	// 不处理会把「这条活动有 300 个收件人、只是页码落到第 9 页」渲染成
	// 「还没有投递记录」的空态，同时分页条还显示第 9 页 —— 两者自相矛盾。
	if report.Total > 0 {
		if maxPage := int((report.Total + mailCampaignsPageSize - 1) / mailCampaignsPageSize); page > maxPage {
			page = maxPage
			if report, err = h.mail.CampaignReport(ctx, id, page, mailCampaignsPageSize); err != nil {
				c.Redirect(http.StatusFound, "/admin/mail/campaigns?err="+urlQueryEscape(mailErrPageText(c, err)))
				return
			}
		}
	}
	data := shell.Prepare(c, gin.H{
		"title": mailLabel(shell.TranslateFor(c), mailenums.PageTitleCampaignReport),
		"R":     report,
		"Page":  page,
		"Err":   mailPageErr(c),
	})
	// 收件人明细的分页条：baseURL 带上 id，翻页时不会丢掉「在看哪条活动」。
	// 键名与 partials/pagination.html 读的键一致（该片段在这里用无参 include 渲染）。
	for k, v := range shell.BuildPagination(report.Total, page, mailCampaignsPageSize,
		fmt.Sprintf("/admin/mail/campaign?id=%d", id), shell.TranslateFor(c)).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/mail/mail_campaign.html", data)
}

// MailCampaignReportJSON 报表数据接口（图表 / 外部核对用同一份口径）。
//
// 目前没有路由指向它（保留给外部核对脚本）—— 拆分本轮只是随活动域搬家，未注册路由。
func (h *mailPageHandle) MailCampaignReportJSON(c *gin.Context) {
	page := int(shell.ParseUint(c.Query("page")))
	if page <= 0 {
		page = 1
	}
	report, err := h.mail.CampaignReport(c.Request.Context(), shell.ParseUint(c.Query("id")), page, mailCampaignsPageSize)
	if err != nil {
		shell.PageErrorBadRequest(c, "mail_campaigns", err)
		return
	}
	response.Success(c, report)
}

// mailCampaignsBackParams 批量动作回跳时带回的列表状态（表单字段名 → URL 参数名）。
// 活动列表没有筛选，只带回页码 —— 批量删完被弹回第 1 页会让人重新翻回去。
var mailCampaignsBackParams = [][2]string{{"returnPage", "page"}}

// MailCampaignsBulkDelete 批量删除群发活动（POST /admin/mail/campaigns/bulk-delete）。
//
// 单条路径 = DeleteCampaign，权限点复用 /api/mail/campaign/delete。
// 发送中的活动由服务端拒绝（ErrCampaignSending）→ 只跳过它、其余照常删除：
// 删掉发送中的活动，后台分批展开的收件人任务会在中途找不到活动，留下一批半截记录。
func (h *mailPageHandle) MailCampaignsBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail/campaigns", mailCampaignsBackParams, "", mailBulkIDsText(c, berr)))
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
	c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail/campaigns", mailCampaignsBackParams, done, warn))
}
