// mail_templates_page_handle.go — 邮件模板页（从「邮箱设置」页拆出的独立职能）。
//
// 为什么单独成页：模板只在「发信时被引用」，与发信账号没有任何共同的操作对象 ——
// 挤在一页时，日常换一次 SMTP 密码的人每次都要越过一整张模板表才能点保存。
// 拆开后按 admin-ui-logic §2 的判据各答一句话：本页只回答「有哪些模板」。
package mailhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/internal/web/shell"
)

// MailTemplatesPage 邮件模板列表页。
//
// 分页参数用 page（不是 template_page）：拆分前同页有两张表共用一个 ?page=，
// 才需要 account_page / template_page 区分；本页只有一张表，沿用域内惯用的 page。
func (h *mailPageHandle) MailTemplatesPage(c *gin.Context) {
	ctx := c.Request.Context()
	rows, total, page, err := h.mail.ListTemplatesPage(ctx, "", mailPageNumber(c.Query("page")), mailListPageSize)
	if err != nil {
		// 失败路径必须注入 title 与空集合：缺 title 会在 layout 中断、缺 Templates
		// 会在列表行中断，两者都表现为「HTTP 200 + 半页 HTML」，运营连归口文案都看不全。
		c.HTML(http.StatusOK, "admin/mail/mail_templates.html", shell.Prepare(c, mailTemplatesErrData(c, err)))
		return
	}
	data := shell.Prepare(c, gin.H{
		"title":         mailLabel(shell.TranslateFor(c), mailenums.PageTitleTemplates),
		"Templates":     rows,
		"TemplateTotal": total,
		// 读侧回执一律经 mail_err.go 的白名单出口：查询参数不是可信边界。
		"Err": mailPageErr(c),
		"Ok":  mailPageOk(c),
		// Done：批量动作的结论（全成功走 ?done=，有跳过走 ?err=）。
		"Done": mailPageDone(c),
	})
	// 分页数据摊平进 data 顶层：单页 / 空数据时 TemplateKeys 返回空 map，
	// 于是 PaginationLinks 这个键不存在 —— 模板用 {{key := .["X"]}} 取值（缺键为 nil，不中断）。
	for k, v := range mailListPagination(total, page, "page", "/admin/mail/templates", shell.TranslateFor(c)) {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/mail/mail_templates.html", data)
}

// mailTemplatesErrData 取数失败时的页面数据：归口文案 + 空列表 + 小写 title。
func mailTemplatesErrData(c *gin.Context, err error) gin.H {
	return gin.H{
		"title":         mailLabel(shell.TranslateFor(c), mailenums.PageTitleTemplates),
		"Err":           mailErrPageText(c, err),
		"Templates":     []any{},
		"TemplateTotal": int64(0),
	}
}
