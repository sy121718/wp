// mail_marketing_handle.go — 后台营销页（issue #37）：联系人与群发活动。
//
// 与邮箱配置页分开：日常操作营销的人不需要看到 SMTP 配置。
// 页面语义上反复强调两件事：
//
//	· 只发给**已订阅**的人（pending 未确认同意的绝不发）—— 合规底线；
//	· 导入时「同意声明」不是 UI 便利，而是**留痕**：谁在什么时候声明过什么。
package dashboardhttp

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	maildto "go_wp/internal/module/mail/dto"
)

// mailMarketingPageSize 联系人 / 活动每页条数。
const mailMarketingPageSize = 50

// MailMarketingPage 联系人与群发活动页。
func (h *mailPageHandle) MailMarketingPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := int(parseUint64(c.Query("page")))
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
		c.HTML(http.StatusOK, "admin/mail_marketing.html", withCSRF(c, gin.H{"title": "邮件营销", "Err": err.Error()}))
		return
	}
	campaigns, err := h.mail.ListCampaigns(ctx, &maildto.CampaignListReq{Page: 1, PageSize: mailMarketingPageSize})
	if err != nil {
		c.HTML(http.StatusOK, "admin/mail_marketing.html", withCSRF(c, gin.H{"title": "邮件营销", "Err": err.Error()}))
		return
	}
	accounts, _ := h.mail.ListAccounts(ctx, "")
	templates, _ := h.mail.ListTemplates(ctx, "")
	c.HTML(http.StatusOK, "admin/mail_marketing.html", withCSRF(c, gin.H{
		"title":        "邮件营销",
		"Contacts":     contacts.Items,
		"ContactTotal": contacts.Total,
		"Campaigns":    campaigns.Items,
		"Accounts":     accounts,
		"Templates":    templates,
		"Page":         page,
		"Keyword":      c.Query("keyword"),
		"Status":       c.Query("status"),
		"Err":          c.Query("err"),
		"Ok":           c.Query("ok"),
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
		ID:     parseUint64(c.PostForm("id")),
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
		ID:         parseUint64(c.PostForm("id")),
		Name:       c.PostForm("name"),
		AccountID:  parseUint64(c.PostForm("account_id")),
		TemplateID: parseUint64(c.PostForm("template_id")),
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
		CampaignID: parseUint64(c.PostForm("id")),
	})
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(err.Error()))
		return
	}
	msg := "活动已开始发送，目标 " + strconv.FormatInt(res.Total, 10) + " 人；进度可在下方列表刷新查看。"
	c.Redirect(http.StatusFound, "/admin/mail/marketing?ok="+urlQueryEscape(msg))
}

// MailCampaignDelete 删除活动。
func (h *mailPageHandle) MailCampaignDelete(c *gin.Context) {
	if err := h.mail.DeleteCampaign(c.Request.Context(), parseUint64(c.PostForm("id"))); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/marketing?err="+urlQueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail/marketing?ok=1")
}
