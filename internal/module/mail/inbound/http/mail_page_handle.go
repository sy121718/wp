// mail_page_handle.go — 后台邮箱页（issue #37）。
//
// 与货源管理页同一模式：GET 渲染完整页，POST 处理完 302 回本页
// （原生表单 + csrf_token 隐藏域），错误经 ?err= 回显。
//
// 两页分工：
//
//	· /admin/mail            —— 发信账号与邮件模板（配置类）
//	· /admin/mail/marketing  —— 联系人与群发活动（营销类）
//
// 配置与营销放在两页：日常操作营销的人不需要看到 SMTP 密码相关的配置项。
//
// 页面壳层（CSRF / 多语言 / 侧栏 / 权限上下文 / 分页）统一走 internal/web/shell；
// 页面路由与 API 在同一处装配（mail_router.go → setupMailPageRoutes）。
package mailhttp

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	"go_wp/internal/web/shell"
)

// mailPageHandle 邮箱后台页处理器。
type mailPageHandle struct {
	mail mailcontract.MailService
}

// NewMailPageHandle 构造邮箱后台页处理器（与 NewHandle 同风格；
// 装配走它，页面测试也走它 —— 测试不该为了拿到 handle 而装配整棵后台路由树）。
func NewMailPageHandle(mail mailcontract.MailService) *mailPageHandle {
	return &mailPageHandle{mail: mail}
}

// MailPage 发信账号 + 邮件模板页。
func (h *mailPageHandle) MailPage(c *gin.Context) {
	ctx := c.Request.Context()
	accounts, err := h.mail.ListAccounts(ctx, "")
	if err != nil {
		c.HTML(http.StatusOK, "admin/mail.html", shell.Prepare(c, gin.H{"Err": err.Error()}))
		return
	}
	templates, err := h.mail.ListTemplates(ctx, "")
	if err != nil {
		c.HTML(http.StatusOK, "admin/mail.html", shell.Prepare(c, gin.H{"Err": err.Error()}))
		return
	}
	// 注意用小写 title：layout.html 的字段访问是 {{.title}}，缺 key 会渲染报错。
	c.HTML(http.StatusOK, "admin/mail.html", shell.Prepare(c, gin.H{
		"title":     "邮箱设置",
		"Accounts":  accounts,
		"Templates": templates,
		"Err":       c.Query("err"),
		"Ok":        c.Query("ok"),
	}))
}

// MailAccountSave 保存发信账号（id 为 0 即新建）。
func (h *mailPageHandle) MailAccountSave(c *gin.Context) {
	req := &maildto.SaveAccountReq{
		ID:          shell.ParseUint(c.PostForm("id")),
		Name:        c.PostForm("name"),
		Purpose:     c.PostForm("purpose"),
		FromName:    c.PostForm("from_name"),
		FromEmail:   c.PostForm("from_email"),
		ReplyTo:     c.PostForm("reply_to"),
		Provider:    c.PostForm("provider"),
		Host:        c.PostForm("host"),
		Port:        int(shell.ParseUint(c.PostForm("port"))),
		Username:    c.PostForm("username"),
		Password:    c.PostForm("password"),
		Encryption:  c.PostForm("encryption"),
		RatePerHour: int(shell.ParseUint(c.PostForm("rate_per_hour"))),
	}
	var err error
	if req.ID > 0 {
		_, err = h.mail.UpdateAccount(c.Request.Context(), req)
	} else {
		_, err = h.mail.CreateAccount(c.Request.Context(), req)
	}
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/mail?err="+urlQueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail?ok=1")
}

// MailAccountDelete 删除发信账号。
func (h *mailPageHandle) MailAccountDelete(c *gin.Context) {
	if err := h.mail.DeleteAccount(c.Request.Context(), shell.ParseUint(c.PostForm("id"))); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail?err="+urlQueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail?ok=1")
}

// MailAccountDefault 设为该用途的默认账号。
func (h *mailPageHandle) MailAccountDefault(c *gin.Context) {
	if err := h.mail.SetDefaultAccount(c.Request.Context(), shell.ParseUint(c.PostForm("id"))); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail?err="+urlQueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail?ok=1")
}

// MailAccountTest 测试发送：结果**不回显为成功页**，而是把 SMTP 的真实反馈带回来。
func (h *mailPageHandle) MailAccountTest(c *gin.Context) {
	res, err := h.mail.TestSend(c.Request.Context(), &maildto.TestSendReq{
		AccountID: shell.ParseUint(c.PostForm("id")),
		ToEmail:   c.PostForm("to_email"),
	})
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/mail?err="+urlQueryEscape(err.Error()))
		return
	}
	if !res.OK {
		msg := "发送失败（" + res.ErrorKind + "）：" + res.Error
		c.Redirect(http.StatusFound, "/admin/mail?err="+urlQueryEscape(msg))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail?ok=1")
}

// MailTemplateSave 保存邮件模板。
func (h *mailPageHandle) MailTemplateSave(c *gin.Context) {
	req := &maildto.SaveTemplateReq{
		TemplateKey: c.PostForm("template_key"),
		Locale:      c.PostForm("locale"),
		Name:        c.PostForm("name"),
		Subject:     c.PostForm("subject"),
		BodyHTML:    c.PostForm("body_html"),
		BodyText:    c.PostForm("body_text"),
	}
	if vars := strings.TrimSpace(c.PostForm("variables")); vars != "" {
		for _, v := range strings.Split(vars, ",") {
			if s := strings.TrimSpace(v); s != "" {
				req.Variables = append(req.Variables, s)
			}
		}
	}
	if _, err := h.mail.UpsertTemplate(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail?err="+urlQueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail?ok=1")
}

// MailTemplateDelete 删除邮件模板。
func (h *mailPageHandle) MailTemplateDelete(c *gin.Context) {
	if err := h.mail.DeleteTemplate(c.Request.Context(), c.PostForm("template_key"), c.PostForm("locale")); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail?err="+urlQueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail?ok=1")
}

// urlQueryEscape 查询参数转义（重定向回显错误文案 / 导入统计）。
// 该包内只有后台页面会拼 ?err= / ?ok= 回显，所以留在页面文件里。
func urlQueryEscape(value string) string {
	return url.QueryEscape(value)
}
