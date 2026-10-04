// mail_contacts_page_handle.go — 联系人页（含导入），从「邮件营销」页拆出的独立职能。
//
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
	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/internal/web/shell"
)

// mailContactsPageSize 联系人每页条数。
const mailContactsPageSize = 50

// mailContactsClampPage 把页码收敛到有效范围。
//
// 为什么需要它：mail 服务端分页**不收敛** —— 页码越界时返回的是「空列表 + 真实 total」
// （见 service/mail_contact.go 的 offset 计算）。不收敛就会把「有 51 个联系人、
// 只是页码落到第 9 页」渲染成「没有匹配的联系人」的空态，而那句话是错的。
// 收敛后页码、数据与分页条三者自洽。
func mailContactsClampPage(page int, total int64) int {
	if page < 1 {
		return 1
	}
	if total <= 0 {
		// 没有数据时页码没有含义（分页条也不会渲染），留着 ?page=7 只是把无效状态写进 URL。
		return 1
	}
	maxPage := int((total + mailContactsPageSize - 1) / mailContactsPageSize)
	if maxPage < 1 {
		maxPage = 1
	}
	if page > maxPage {
		return maxPage
	}
	return page
}

// MailContactsPage 联系人列表页（筛选 + 表 + 批量动作 + 分页；导入/新建/编辑走抽屉）。
func (h *mailPageHandle) MailContactsPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := shell.TranslateFor(c)
	page := mailPageNumber(c.Query("page"))
	keyword, status := c.Query("keyword"), c.Query("status")
	tags := c.Query("tags")
	tagList := splitTagInput(tags)

	filter := func() (*maildto.ContactListResp, error) {
		return h.mail.ListContacts(ctx, &maildto.ContactFilterReq{
			Keyword:  keyword,
			Status:   status,
			Tags:     tagList,
			Page:     page,
			PageSize: mailContactsPageSize,
		})
	}
	contacts, err := filter()
	if err != nil {
		c.HTML(http.StatusOK, "admin/mail/mail_contacts.html", shell.Prepare(c, mailContactsErrData(c, err)))
		return
	}
	// 页号收敛（见 mailContactsClampPage）：越界时用收敛后的页码重取一次。
	// 只在越界这一种情况下多一次查询，正常翻页仍是原来的一次。
	if fixed := mailContactsClampPage(page, contacts.Total); fixed != page {
		page = fixed
		if contacts, err = filter(); err != nil {
			c.HTML(http.StatusOK, "admin/mail/mail_contacts.html", shell.Prepare(c, mailContactsErrData(c, err)))
			return
		}
	}

	// 筛选区的标签候选：只读、失败不影响页面 —— 候选只是输入提示，
	// 缺了它 datalist 为空，输入框仍然可用（所以这里不把错误升级成整页错误分支）。
	tagOptions, tagErr := h.mail.ListContactTags(ctx)
	if tagErr != nil {
		tagOptions = nil
	}

	data := shell.Prepare(c, gin.H{
		"title":        mailLabel(tr, mailenums.PageTitleContacts),
		"Contacts":     contacts.Items,
		"ContactTotal": contacts.Total,
		"Page":         page,
		"Keyword":      keyword,
		"Status":       status,
		"Tags":         tags,
		"TagOptions":   tagOptions,
		// 读侧回执一律经 mail_err.go 的白名单出口：查询参数不是可信边界。
		"Err": mailPageErr(c),
		"Ok":  mailPageOk(c),
		// Done：批量动作的结论（全成功走 ?done=，有跳过走 ?err=）。
		"Done": mailPageDone(c),
	})
	// 基地址带上筛选，翻页才保留筛选条件（否则翻到第 2 页就回到全量）。
	base := shell.FilterBaseURL(mailContactsPath, map[string]string{"keyword": keyword, "status": status, "tags": tags})
	for k, v := range shell.BuildPagination(contacts.Total, page, mailContactsPageSize, base, tr).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/mail/mail_contacts.html", data)
}

// mailContactsErrData 取数失败时的页面数据。
//
// 空值不是可选的：admin/mail/mail_contacts.html 在提示条之后就用 .Keyword / .Status /
// .Page / .ContactTotal / len(.Contacts) 渲染筛选与列表，缺键会让 Jet **在那一行中断**
// （HTTP 仍是 200、正文整块消失）。只注入 Err 的分支因此渲染不完。
// mailContactsPath 联系人页地址（回跳与拼接回执只用这一个字面量）。
const mailContactsPath = "/admin/mail/contacts"

func mailContactsErrData(c *gin.Context, err error) gin.H {
	return gin.H{
		"title":        mailLabel(shell.TranslateFor(c), mailenums.PageTitleContacts),
		"Err":          mailErrPageText(c, err),
		"Contacts":     []any{},
		"ContactTotal": int64(0),
		"Page":         1,
		"Keyword":      "",
		"Status":       "",
		"Tags":         "",
		"TagOptions":   []string{},
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
		c.Redirect(http.StatusFound, "/admin/mail/contacts?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	// 与 mail_err.go 的 mailCountedNoticeTemplates[2] 同形（数字归一后可判定）。
	msg := fmt.Sprintf("导入完成：新增 %d，更新 %d，跳过 %d，抑制名单命中 %d，非法 %d",
		res.Imported, res.Updated, res.Skipped, res.Suppressed, len(res.Errors))
	c.Redirect(http.StatusFound, "/admin/mail/contacts?ok="+urlQueryEscape(msg))
}

// MailContactStatus 改联系人同意状态。
func (h *mailPageHandle) MailContactStatus(c *gin.Context) {
	req := &maildto.UpdateContactStatusReq{
		ID:     shell.ParseUint(c.PostForm("id")),
		Status: c.PostForm("status"),
		Note:   c.PostForm("note"),
	}
	if err := h.mail.UpdateContactStatus(c.Request.Context(), req); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/contacts?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail/contacts?ok=1")
}

// —— 批量动作（评审规则 admin-ui-logic §7：列表首列勾选 + 批量条）——
//
// 与账号 / 模板的批量删除同一形状（见 mail_page_handle.go 的 mailBulkOutcome / mailBulkLocation）：
// 逐条走**同一条单条路径**，失败只计跳过、不中断整批，结论按「成功 N / 跳过 M」回带列表页。
// 权限点一律复用对应单条动作的路径，不新增权限点、不写迁移。

// mailContactsBackParams 批量动作回跳时带回的列表状态（表单字段名 → URL 参数名）。
//
// 两者刻意不同名：表单里的 status 是**目标状态**（批量改状态用），而列表筛选参数也叫
// status —— 同名直接回带会把筛选条件写成目标状态，于是「筛了已退订 → 批量改回订阅」
// 之后列表会莫名其妙变成筛选「订阅」。
var mailContactsBackParams = [][2]string{
	{"returnKeyword", "keyword"},
	{"returnStatus", "status"},
	{"returnTags", "tags"},
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
		c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail/contacts",
			mailContactsBackParams, "", mailContactStatusBadText))
		return
	}
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail/contacts", mailContactsBackParams, "", mailBulkIDsText(c, berr)))
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
	c.Redirect(http.StatusFound, mailBulkLocation(c, mailContactsPath, mailContactsBackParams, done, warn))
}

// —— 新建 / 编辑 / 删除 / 批量打标签（补齐「联系人 CRUD + 标签」）——
//
// 四条写路由的权限点各自独立（save / delete / tag），批量端点复用对应单条的 API 路径：
// 权限路径与 API 完全同源，单列一条策略就等于把「能删一个」的人挡在批量外。

// mailContactsSavedLocation 单条写操作成功后的回跳地址（保留筛选 + ?ok=1）。
//
// 不用 mailBulkLocation 的 done= 通道：单条动作的回执是「保存成功」这类成功消息
// （?ok=1 由 mailPageOk 取），done= 是批量结论的通道；两者混用会让页面上出现
// 「已删除 1 个联系人」这种批量口吻的单条回执。mailBulkLocation 在只带回筛选参数时
// 返回 "path?筛选" 或 "path"，两种形态都要能接上 ok=1。
func mailContactsSavedLocation(c *gin.Context) string {
	loc := mailBulkLocation(c, mailContactsPath, mailContactsBackParams, "", "")
	if strings.Contains(loc, "?") {
		return loc + "&ok=1"
	}
	return loc + "?ok=1"
}

// MailContactSave 新建 / 编辑联系人（POST /admin/mail/contact/save）。
//
// ID=0 新建、否则编辑：handler 只按 ID 分派，两条路径的差别全在 service 里，
// 表单字段因此同源 —— 分开两个端点必然出现「新建加了字段、编辑忘了加」。
//
// 失败时不原地留住输入（与账号 / 模板抽屉同一取舍）：错误文案回列表页顶部，
// 抽屉需要重新打开。模板侧没有 htmx 承载，硬做回灌会把抽屉模板变成两套数据源；
// 字段多到需要保住输入时，再按 internal/templates/CLAUDE.md 的写表单分档来做。
func (h *mailPageHandle) MailContactSave(c *gin.Context) {
	req := &maildto.SaveContactReq{
		ID:            shell.ParseUint(c.PostForm("id")),
		Email:         c.PostForm("email"),
		Name:          c.PostForm("name"),
		Tags:          splitTagInput(c.PostForm("tags")),
		Source:        c.PostForm("source"),
		ConsentSource: c.PostForm("consent_source"),
		Status:        c.PostForm("status"),
		OperatorID:    shell.CurrentUserID(c),
	}
	var err error
	if req.ID > 0 {
		err = h.mail.UpdateContact(c.Request.Context(), req)
	} else {
		_, err = h.mail.CreateContact(c.Request.Context(), req)
	}
	if err != nil {
		c.Redirect(http.StatusFound, mailBulkLocation(c, mailContactsPath, mailContactsBackParams, "", mailErrPageText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, mailContactsSavedLocation(c))
}

// MailContactDelete 删除单个联系人（POST /admin/mail/contact/delete）。
//
// 单条与批量走同一个 service 方法，抑制名单的边界（只删联系人、不动抑制记录）
// 因此只有一处实现，见 service.DeleteContacts 的注释。
func (h *mailPageHandle) MailContactDelete(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	if id == 0 {
		c.Redirect(http.StatusFound, mailBulkLocation(c, mailContactsPath, mailContactsBackParams, "", mailContactDeleteBadText))
		return
	}
	if _, err := h.mail.DeleteContacts(c.Request.Context(), &maildto.DeleteContactsReq{
		IDs: []uint64{id}, OperatorID: shell.CurrentUserID(c),
	}); err != nil {
		c.Redirect(http.StatusFound, mailBulkLocation(c, mailContactsPath, mailContactsBackParams, "", mailErrPageText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, mailContactsSavedLocation(c))
}

// MailContactsBulkDelete 批量删除联系人（POST /admin/mail/contacts/bulk-delete）。
//
// 结论按「已删除 N / 跳过 M」回带（mailBulkOutcome）：M 是点选里已经不存在的那几个，
// 必须说出来 —— 静默的部分成功会让人以为「一条都没删」然后反复重试。
func (h *mailPageHandle) MailContactsBulkDelete(c *gin.Context) {
	raw, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusFound, mailBulkLocation(c, mailContactsPath, mailContactsBackParams, "", mailBulkIDsText(c, berr)))
		return
	}
	idList, _ := parseFormIDs(raw)
	deleted, err := h.mail.DeleteContacts(c.Request.Context(), &maildto.DeleteContactsReq{
		IDs: idList, OperatorID: shell.CurrentUserID(c),
	})
	if err != nil {
		c.Redirect(http.StatusFound, mailBulkLocation(c, mailContactsPath, mailContactsBackParams, "", mailErrPageText(c, err)))
		return
	}
	// 用点选条数（raw）作分母：非法 id 也在这批里，它们同样没有被删除。
	skipped := len(raw) - int(deleted)
	if skipped < 0 {
		skipped = 0
	}
	done, warn := mailBulkOutcome("删除", "联系人", int(deleted), skipped)
	c.Redirect(http.StatusFound, mailBulkLocation(c, mailContactsPath, mailContactsBackParams, done, warn))
}

// MailContactsBulkTag 批量打标签（POST /admin/mail/contacts/bulk-tag）。
//
// 加与减在同一条请求里提交：分成两次提交必然出现「加成功、减失败」的半截状态，
// 而运营看到的是一个错误、以为整批没生效。
func (h *mailPageHandle) MailContactsBulkTag(c *gin.Context) {
	raw, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusFound, mailBulkLocation(c, mailContactsPath, mailContactsBackParams, "", mailBulkIDsText(c, berr)))
		return
	}
	idList, invalid := parseFormIDs(raw)
	changed, skipped, err := h.mail.TagContacts(c.Request.Context(), &maildto.TagContactsReq{
		IDs:        idList,
		Add:        splitTagInput(c.PostForm("add")),
		Remove:     splitTagInput(c.PostForm("remove")),
		OperatorID: shell.CurrentUserID(c),
	})
	if err != nil {
		c.Redirect(http.StatusFound, mailBulkLocation(c, mailContactsPath, mailContactsBackParams, "", mailErrPageText(c, err)))
		return
	}
	// service 看不到被丢掉的非法 id（它们根本没进请求），跳过数在这里补齐。
	done, warn := mailBulkOutcome("更新", "联系人", changed, skipped+invalid)
	c.Redirect(http.StatusFound, mailBulkLocation(c, mailContactsPath, mailContactsBackParams, done, warn))
}

// splitTagInput 把表单里的标签串切成标签列表（; | , 三种分隔符）。
//
// 与 service/mail_contact_import.go 的 splitTags **同口径**（导入抽屉的多标签也是
// 同一串形态）。两处要一起改：口径不一致会让「导入时分开的标签」与「编辑时分开的标签」
// 在库里变成不同的值，而按标签筛人群是精确匹配（tags @> ...），差一个字符就筛不到人。
func splitTagInput(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == '|' || r == ',' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// parseFormIDs 把 shell.BulkIDs 的字符串 id 转成 uint64，并统计非法项。
//
// 非法项不静默丢弃：回执里的「跳过 M」要如实包含它们，否则运营点了 4 个、
// 回执写「已更新 3 个」，会以为系统漏了一条。
func parseFormIDs(raw []string) (ids []uint64, invalid int) {
	ids = make([]uint64, 0, len(raw))
	for _, s := range raw {
		if v := shell.ParseUint(s); v > 0 {
			ids = append(ids, v)
			continue
		}
		invalid++
	}
	return ids, invalid
}
