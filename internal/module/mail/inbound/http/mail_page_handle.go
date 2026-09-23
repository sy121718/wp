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
	"fmt"
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

// mailListPageSize 邮箱设置页两张表（发信账号 / 邮件模板）每页条数。
//
// 与 shell.PageParams 的默认值一致：本页也接受 ?limit=（上限 100 由 PageParams 兜住），
// 缺省就是 20 条/页 —— 十几条账号加几十条模板时，页高不再随数据量无上限增长。
const mailListPageSize = 20

// mailPageNumber 解析 ?page=（缺省 1；非法值按 1 处理，不报错）。
func mailPageNumber(raw string) int {
	page := int(shell.ParseUint(raw))
	if page <= 0 {
		return 1
	}
	return page
}

// mailListPageSlice 切出「第 page 页」，并返回**收敛后**的页码。
//
// 为什么在 handler 侧切：mail 契约的 ListAccounts / ListTemplates 只返回 []Item ——
// 既没有 total，也没有 limit/offset（见 mail/contract 的签名），而分页条要的
// 「共 N 条，第 X-Y 条」必须由 total 算出（shell.BuildPagination 的第一个入参就是它）。
// 真源分页（service 暴露 limit/offset + count）会越出本批的独占文件范围，
// 所以这里先把三件事做对：页高有上限、计数准确、页码可用 —— 与商品域
// product_taxonomy_page.go 的 listPageSlice 同一手法与同一取舍。
//
// current 必须交给 BuildPagination：page=999 时若不先在这里收敛，
// 会出现「表格为空、分页条却显示第 999 页」这种自相矛盾的组合。
func mailListPageSlice[T any](all []T, page, size int) (rows []T, current int) {
	if size < 1 {
		size = mailListPageSize
	}
	if page < 1 {
		page = 1
	}
	pages := (len(all) + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	from := (page - 1) * size
	if from > len(all) {
		from = len(all)
	}
	to := from + size
	if to > len(all) {
		to = len(all)
	}
	return all[from:to], page
}

// NewMailPageHandle 构造邮箱后台页处理器（与 NewHandle 同风格；
// 装配走它，页面测试也走它 —— 测试不该为了拿到 handle 而装配整棵后台路由树）。
func NewMailPageHandle(mail mailcontract.MailService) *mailPageHandle {
	return &mailPageHandle{mail: mail}
}

// MailPage 发信账号 + 邮件模板页。
func (h *mailPageHandle) MailPage(c *gin.Context) {
	ctx := c.Request.Context()
	// 取数失败只回一条归口文案（service 的 i18n key 翻成中文；基础设施错误只进日志）。
	//
	// **必须带小写 title**：layout.html 用 {{.title}} 取值，缺这个键会让渲染在 layout 里
	// 中断（HTTP 仍是 200、正文整块为空）—— 也就是说「取数失败」会变成「白页」，
	// 运营连那句归口文案都看不到。这两条分支此前正是这样。
	accounts, err := h.mail.ListAccounts(ctx, "")
	if err != nil {
		c.HTML(http.StatusOK, "admin/mail/mail.html", shell.Prepare(c, mailPageErrData(c, err)))
		return
	}
	templates, err := h.mail.ListTemplates(ctx, "")
	if err != nil {
		c.HTML(http.StatusOK, "admin/mail/mail.html", shell.Prepare(c, mailPageErrData(c, err)))
		return
	}
	// 注意用小写 title：layout.html 的字段访问是 {{.title}}，缺 key 会渲染报错。
	//
	// 两张表共用 ?page=（分页组件的链接参数名写死 page/limit，同页两张表无法各带一个页码），
	// 但各自按自己的数据切片、各自算分页条 —— 页数少的那张表会被收敛到自己的最后一页，
	// 不会出现「表里没数据、分页条却翻着」的自相矛盾。
	page := mailPageNumber(c.Query("page"))
	accountRows, accountPage := mailListPageSlice(accounts, page, mailListPageSize)
	templateRows, templatePage := mailListPageSlice(templates, page, mailListPageSize)

	data := shell.Prepare(c, gin.H{
		"title":     "邮箱设置",
		"Accounts":  accountRows,
		"Templates": templateRows,
		// 读侧回执一律经 mail_err.go 的白名单出口：查询参数不是可信边界。
		"Err": mailPageErr(c),
		"Ok":  mailPageOk(c),
		// Done：批量动作的结论（全成功走 ?done=，有跳过走 ?err=）。模板用 isset 认这个可选键。
		"Done": mailPageDone(c),
	})
	// 两张表的分页数据分开命名（Accounts* / Templates*）：模板两次 include 分页片段时
	// 各自传一份 context（Jet 的 include 传了 context 后，被包含模板的 . 就是它本身）。
	// TemplateKeys 在「无分页」时返回空 map，因此这两组键在单页时可能不存在 ——
	// 模板侧用 {{key := .["X"]}} 取值（缺键为 nil，不中断），片段里的 if 自然跳过。
	for k, v := range shell.BuildPagination(int64(len(accounts)), accountPage, mailListPageSize,
		"/admin/mail", shell.TranslateFor(c)).TemplateKeys() {
		data["Accounts"+k] = v
	}
	for k, v := range shell.BuildPagination(int64(len(templates)), templatePage, mailListPageSize,
		"/admin/mail", shell.TranslateFor(c)).TemplateKeys() {
		data["Templates"+k] = v
	}
	c.HTML(http.StatusOK, "admin/mail/mail.html", data)
}

// mailPageErrData 取数失败时的页面数据：归口文案 + 让模板能整页渲染完的空列表。
//
// 为什么空列表不是可选的：admin/mail.html 在提示条之后就用 len(.Accounts) / range 取列表，
// 缺键会让 Jet **在那一行中断**（HTTP 仍是 200、正文整块消失，本项目出过多次）。
// 只注入 Err 的分支因此永远渲染不完 —— 运营既看不到列表，也只有半页 HTML。
//
// **title 必须是小写 key**：layout.html 用 {{.title}} 取值，缺它同样会中断。
func mailPageErrData(c *gin.Context, err error) gin.H {
	return gin.H{
		"title":     "邮箱设置",
		"Err":       mailErrPageText(c, err),
		"Accounts":  []any{},
		"Templates": []any{},
	}
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
		c.Redirect(http.StatusFound, "/admin/mail?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail?ok=1")
}

// MailAccountDelete 删除发信账号。
func (h *mailPageHandle) MailAccountDelete(c *gin.Context) {
	if err := h.mail.DeleteAccount(c.Request.Context(), shell.ParseUint(c.PostForm("id"))); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail?ok=1")
}

// MailAccountDefault 设为该用途的默认账号。
func (h *mailPageHandle) MailAccountDefault(c *gin.Context) {
	if err := h.mail.SetDefaultAccount(c.Request.Context(), shell.ParseUint(c.PostForm("id"))); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail?err="+urlQueryEscape(mailErrPageText(c, err)))
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
		c.Redirect(http.StatusFound, "/admin/mail?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	if !res.OK {
		// SMTP 的响应码与主机名只进日志（mailTestSendFailedText 里记）：它们既不是给运营看的，
		// 也不该经 302 的 Location 留在浏览器历史里。对外只给「可重试 / 永久拒绝 / 配置问题」。
		c.Redirect(http.StatusFound, "/admin/mail?err="+
			urlQueryEscape(mailTestSendFailedText(c, res.ErrorKind, res.Error)))
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
		c.Redirect(http.StatusFound, "/admin/mail?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail?ok=1")
}

// MailTemplateDelete 删除邮件模板。
func (h *mailPageHandle) MailTemplateDelete(c *gin.Context) {
	if err := h.mail.DeleteTemplate(c.Request.Context(), c.PostForm("template_key"), c.PostForm("locale")); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail?ok=1")
}

// urlQueryEscape 查询参数转义（重定向回显错误文案 / 导入统计）。
// 该包内只有后台页面会拼 ?err= / ?ok= 回显，所以留在页面文件里。
func urlQueryEscape(value string) string {
	return url.QueryEscape(value)
}

// —— 批量动作（评审规则 admin-ui-logic §7：列表首列勾选 + 批量条）——
//
// 账号 / 模板的批量端点在配置页，联系人 / 活动的在营销页（mail_marketing_page_handle.go），
// 但形状是同一个，与产品、订单、优惠码域一致：**逐条走同一条单条路径**，
// 失败只计跳过、不中断整批 —— 批量操作不能因为一条被服务端拒绝就整批回滚，
// 那会让人以为「一条都没做」然后反复重试；也不能静默部分成功，所以结论按
// 「成功 N / 跳过 M」如实回带列表页。
//
// 权限点一律复用对应单条动作的路径（见 mail_page_router.go 的 CasbinMiddlewareForPath），
// 不新增权限点、不写迁移。

// MailAccountsBulkDelete 批量删除发信账号（POST /admin/mail/accounts/bulk-delete）。
//
// 单条路径 = DeleteAccount，权限点复用 /api/mail/account/delete。
// 注意：账号行没有软删除、也没有「被活动引用」的数据库级校验（mail_campaigns.account_id
// 无外键），所以服务端拒绝只来自「账号不存在 / 数据库报错」；将来引用校验加上来，
// 这里自然按跳过处理，不需要改一处。
func (h *mailPageHandle) MailAccountsBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail", nil, "", mailBulkIDsText(c, berr)))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := shell.ParseUint(raw)
		if id == 0 {
			skipped++
			continue
		}
		if err := h.mail.DeleteAccount(c.Request.Context(), id); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	done, warn := mailBulkOutcome("删除", "发信账号", deleted, skipped)
	c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail", nil, done, warn))
}

// MailTemplatesBulkDelete 批量删除邮件模板（POST /admin/mail/templates/bulk-delete）。
//
// 模板的唯一键是 key + locale（单条动作 DeleteTemplate 就按这两者删，没有按 id 删的口子），
// 而勾选框提交的是行的主键 id，所以这里先把 id 映射回 key / locale，再逐条调用**同一条**
// DeleteTemplate —— 删除语义仍然只有 service / model 那一份，不在这里另写一套匹配条件
// （抄一份 key+locale 的 where 就是两份真相，抄错时不会报错、只会删错行）。
func (h *mailPageHandle) MailTemplatesBulkDelete(c *gin.Context) {
	ctx := c.Request.Context()
	list, err := h.mail.ListTemplates(ctx, "")
	if err != nil {
		c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail", nil, "",
			mailTemplateListFailedText))
		return
	}
	type templateRef struct{ key, locale string }
	byID := make(map[uint64]templateRef, len(list))
	for _, t := range list {
		if t != nil {
			byID[t.ID] = templateRef{key: t.TemplateKey, locale: t.Locale}
		}
	}
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail", nil, "", mailBulkIDsText(c, berr)))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		ref, ok := byID[shell.ParseUint(raw)]
		if !ok {
			skipped++
			continue
		}
		if err := h.mail.DeleteTemplate(ctx, ref.key, ref.locale); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	done, warn := mailBulkOutcome("删除", "邮件模板", deleted, skipped)
	c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail", nil, done, warn))
}

// mailBulkOutcome 把「成功 N / 跳过 M」折成两条回带文案。
//
// 有跳过时走警告（?err=）而不是成功（?done=）：部分成功必须说出来，
// 否则只看到「已删除 2 个」的人会以为选中的都删了。
func mailBulkOutcome(verb, noun string, done, skipped int) (doneText, warnText string) {
	// 模板取自 mail_err.go 的 mailBulkResultTemplates —— 那里同时是读侧候选文案的来源
	//（按 mailBulkVerbs × mailBulkNouns 展开）：写侧改措辞时读侧跟着变，不会静默失配。
	switch {
	case done == 0 && skipped == 0:
		return "", ""
	case skipped == 0:
		return fmt.Sprintf(mailBulkResultTemplates[0], verb, done, noun), ""
	case done == 0:
		return "", fmt.Sprintf(mailBulkResultTemplates[1], noun, verb, skipped)
	default:
		return "", fmt.Sprintf(mailBulkResultTemplates[2], verb, done, noun, skipped)
	}
}

// mailBulkLocation 拼批量动作的回跳 URL：结论走 ?done= / ?err=（值经 urlQueryEscape），
// backParams 里的表单字段原样带回 —— 批量改完被弹回未筛选的第一页，等于让人重新筛一遍。
//
// backParams 是「表单字段名 → URL 参数名」的配对而不是一份字段名清单：营销页表单里的
// status 是**目标状态**，列表筛选参数也叫 status，同名直接回带会把筛选条件写成目标状态。
func mailBulkLocation(c *gin.Context, path string, backParams [][2]string, doneText, warnText string) string {
	parts := make([]string, 0, len(backParams)+1)
	for _, p := range backParams {
		formKey, queryKey := p[0], p[1]
		if v := strings.TrimSpace(c.PostForm(formKey)); v != "" {
			parts = append(parts, queryKey+"="+urlQueryEscape(v))
		}
	}
	switch {
	case warnText != "":
		parts = append(parts, "err="+urlQueryEscape(warnText))
	case doneText != "":
		parts = append(parts, "done="+urlQueryEscape(doneText))
	}
	if len(parts) == 0 {
		return path
	}
	return path + "?" + strings.Join(parts, "&")
}
