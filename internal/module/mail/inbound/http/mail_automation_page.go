// mail_automation_page.go — 后台自动化页（issue #38 P3，目标 ⑦）。
//
// **表单式编辑器，不是拖拽**：一行一个节点（标识 / 类型 / 参数 / 下一步 / 分支两臂），
// 提交时服务端组装成图定义再走同一套校验。
//
// 为什么先做表单而不是拖拽：
//
//	· 拖拽需要一整套前端状态管理（画布坐标、连线命中、撤销栈），是独立的前端工程；
//	· 表单能表达引擎的**全部**能力（引擎只认节点与连线，不关心它们怎么被画出来）；
//	· 引擎的正确性（环检测 / 可达性 / 执行语义）与编辑器形态无关，先让它可用、能验证。
//
// 表单里不写自定义 JS（项目约定：后台交互走原生表单 / HTMX）。
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

// maxAutomationNodes 表单最多支持多少个节点行。
//
// 有上限不是偷懒：表单式编辑器一旦超过十几行就不好用了 —— 那正是该上拖拽的信号。
const maxAutomationNodes = 12

// mailAutomationPageSize 流程列表每页条数（运行记录页用自己的 mailAutomationRunsPageSize）。
const mailAutomationPageSize = 50

// mailTr 取词函数签名（shell.TranslateFor(c) 的形态）。
type mailTr func(key, fallback string) string

// mailLabel 展示名取词：命中出译文、缺词条回落中文兜底；key 为空（未登记）直接给兜底。
func mailLabel(tr mailTr, pair mailenums.LabelPair) string {
	if strings.TrimSpace(pair.Key) == "" {
		return pair.Fallback
	}
	return tr(pair.Key, pair.Fallback)
}

// nodeTypeOption 类型下拉的一项（Label / Hint 已在组装时按当前语言取词）。
type nodeTypeOption struct {
	Value string
	Label string
	Hint  string
}

// nodeTypeOptions 类型下拉选项（参数提示直接写在界面上，省得去翻文档）。
//
// 取值与文案的真源在 mailenums.AutomationNodeTypes（key + 中文兜底），这里只做取词 ——
// 画布页（mail_automation_canvas.go）与表单页共用同一份，不各写一套。
func nodeTypeOptions(tr mailTr) []nodeTypeOption {
	opts := make([]nodeTypeOption, 0, len(mailenums.AutomationNodeTypes))
	for _, o := range mailenums.AutomationNodeTypes {
		opts = append(opts, nodeTypeOption{
			Value: o.Value,
			Label: mailLabel(tr, o.Label),
			Hint:  mailLabel(tr, o.Hint),
		})
	}
	return opts
}

// MailAutomationPage 流程列表（只回答「有哪些流程」）。
//
// 运行实例搬到 /admin/mail/automation/runs：流程是「配置」，实例是「排障」，
// 两者读的人不同、看的时机不同（配流程时不需要每次都扫一遍实例列表），
// 挤在一页时页面下半部常年滚动着几十条与当前操作无关的记录。
func (h *mailPageHandle) MailAutomationPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := shell.TranslateFor(c)
	page := int(shell.ParseUint(c.Query("page")))
	if page <= 0 {
		page = 1
	}
	automations, err := h.mail.ListAutomations(ctx, &maildto.AutomationListReq{Page: page, PageSize: mailAutomationPageSize})
	data := gin.H{
		"title": mailLabel(tr, mailenums.PageTitleAutomation),
		"Page":  page,
		// 读侧回执一律经 mail_err.go 的白名单出口：查询参数不是可信边界。
		"Err": mailPageErr(c),
		"Ok":  mailPageOk(c),
		// Done：批量动作的结论（全成功走 ?done=，有跳过走 ?err=）。
		"Done": mailPageDone(c),
	}
	if err != nil {
		// 取数失败：归口文案 + 空列表。
		// 空列表不可省 —— 模板随后就用 len(.Automations) / .AutoTotal 渲染列表，
		// 缺键会让 Jet 在那一行中断（HTTP 仍是 200、正文整块消失）。
		data["Err"] = mailErrPageText(c, err)
		data["Automations"] = []any{}
		data["AutoTotal"] = 0
		c.HTML(http.StatusOK, "admin/mail/mail_automation.html", shell.Prepare(c, data))
		return
	}
	// 列表行按模板需要投影：触发方式给中文标签（枚举不直接进界面），其余字段原样透出。
	autoRows := make([]gin.H, 0, len(automations.Items))
	for _, a := range automations.Items {
		autoRows = append(autoRows, gin.H{
			"ID": a.ID, "Name": a.Name, "Description": a.Description,
			"Status": a.Status, "Version": a.Version,
			"TriggerLabel": triggerLabelOf(tr, a.TriggerType),
		})
	}
	data["Automations"] = autoRows
	data["AutoTotal"] = automations.Total
	c.HTML(http.StatusOK, "admin/mail/mail_automation.html", shell.Prepare(c, data))
}

// automationFormValues 编辑页表单当前值。
//
// 单独一份而不是直接读 *maildto.AutomationItem：步骤动作（增 / 删 / 移）只回显不保存，
// 那种请求里没有 item（新建流程时更是一个都没有），但**用户刚填的名称 / 触发方式 / 说明
// 必须原样留在页面上** —— 点一下「添加一步」就清空表单是最让人恼火的一种「功能」。
type automationFormValues struct {
	ID          uint64
	IsNew       bool
	Name        string
	Description string
	Trigger     string
	Entry       string
}

// formValuesFromItem 编辑既有流程时的表单初值。
func formValuesFromItem(item *maildto.AutomationItem) automationFormValues {
	entry := strings.TrimSpace(item.Entry)
	if entry == "" {
		entry = automationEntryKeyDefault
	}
	return automationFormValues{
		ID:          item.ID,
		Name:        item.Name,
		Description: item.Description,
		Trigger:     item.TriggerType,
		Entry:       entry,
	}
}

// formValuesFromPost 动作回显时取自本次提交（不落库）。
func formValuesFromPost(c *gin.Context) automationFormValues {
	id := shell.ParseUint(c.PostForm("id"))
	trigger := strings.TrimSpace(c.PostForm("trigger_type"))
	if trigger == "" {
		trigger = "manual"
	}
	entry := strings.TrimSpace(c.PostForm("entry"))
	if entry == "" {
		entry = automationEntryKeyDefault
	}
	return automationFormValues{
		ID:          id,
		IsNew:       id == 0,
		Name:        strings.TrimSpace(c.PostForm("name")),
		Description: strings.TrimSpace(c.PostForm("description")),
		Trigger:     trigger,
		Entry:       entry,
	}
}

// MailAutomationEdit 流程编辑页（?id=N 编辑，缺省为新建）。
//
// 页面只讲「触发方式 + 按顺序的步骤」：标识 / 下一步 / yes / no 四个输入框已从界面消失
// （用户原话「新建自动化不知道是个什么东西完全没法用」），换算在 mail_automation_form.go。
func (h *mailPageHandle) MailAutomationEdit(c *gin.Context) {
	id := shell.ParseUint(c.Query("id"))
	if id == 0 {
		h.renderAutomationForm(c, automationFormValues{IsNew: true, Trigger: "manual", Entry: automationEntryKeyDefault}, []automationStep{{}}, "")
		return
	}
	item, err := h.mail.GetAutomation(c.Request.Context(), id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/automation?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	if steps, ok := automationStepsFromGraph(item); ok {
		h.renderAutomationForm(c, formValuesFromItem(item), steps, "")
		return
	}
	// 表单表达不了的图（环 / 非 trigger 入口 / 有节点走不到 / 多条件分支）：只读兜底 ——
	// 原定义原样留着，用户改不了它，但也不会在下一次保存时被静默改坏。
	h.renderAutomationFallback(c, item)
}

// renderAutomationForm 渲染步骤表单（新建 / 编辑 / 动作回显 / 校验失败回显共用）。
//
// errText 非空时优先于 ?err= —— 表单校验失败走「200 回显」而不是 302：
// 用户填的内容（尤其是他刚改过的步骤类型）必须留在页面上。否则「改类型 → 保存 →
// 报错 → 页面翻回旧类型 → 再报错」就是死循环，而这条链路上用户没有任何出路。
func (h *mailPageHandle) renderAutomationForm(c *gin.Context, form automationFormValues, steps []automationStep, errText string) {
	tr := shell.TranslateFor(c)
	steps = trimAutomationSteps(steps)
	err := strings.TrimSpace(errText)
	if err == "" {
		err = mailPageErr(c)
	}
	data := gin.H{
		"title":        mailLabel(tr, mailenums.PageTitleAutomationEdit),
		"Form":         form,
		"IsNew":        form.IsNew,
		"Rows":         automationStepRows(tr, steps),
		"Fallback":     false,
		"FallbackRows": []gin.H{},
		"Types":        nodeTypeOptions(tr),
		"Triggers":     triggerOptions(tr, form.Trigger),
		"CanAddStep":   len(steps) < maxAutomationNodes,
		// 同列表页：回执文案过白名单（表单校验文案也在候选里，见 mail_err.go）。
		"Err": err,
		"Ok":  mailPageOk(c),
	}
	// 发信步骤要选模板：把模板列表给页面（省得用户手敲 key）。
	templates, _ := h.mail.ListTemplates(c.Request.Context(), "")
	data["Templates"] = templates
	c.HTML(http.StatusOK, "admin/mail/mail_automation_edit.html", shell.Prepare(c, data))
}

// renderAutomationFallback 表单表达不了的流程：只读展示原图 + 去画布的入口。
//
// 这里**不给可提交的步骤表单**：那份表单渲染出来是空的（还原失败），用户一保存就把
// 还原不出来的节点整段删掉 —— 旧实现正是如此。宁可让他去画布改。
func (h *mailPageHandle) renderAutomationFallback(c *gin.Context, item *maildto.AutomationItem) {
	tr := shell.TranslateFor(c)
	rows := make([]gin.H, 0, len(item.Nodes))
	for _, n := range item.Nodes {
		rows = append(rows, gin.H{
			"Key": n.Key, "Type": nodeTypeLabelOf(tr, n.Type),
			"Next": n.Next, "Yes": n.Yes, "No": n.No,
		})
	}
	form := formValuesFromItem(item)
	data := gin.H{
		"title":        mailLabel(tr, mailenums.PageTitleAutomationEdit),
		"Form":         form,
		"IsNew":        false,
		"Rows":         []gin.H{},
		"Fallback":     true,
		"FallbackRows": rows,
		"Types":        nodeTypeOptions(tr),
		"Triggers":     triggerOptions(tr, form.Trigger),
		"CanAddStep":   false,
		"Err":          mailPageErr(c),
		"Ok":           mailPageOk(c),
	}
	templates, _ := h.mail.ListTemplates(c.Request.Context(), "")
	data["Templates"] = templates
	c.HTML(http.StatusOK, "admin/mail/mail_automation_edit.html", shell.Prepare(c, data))
}

// nodeTypeLabelOf 节点类型 → 当前语言标签；未知类型原样返回（兜底表要能显示真实取值）。
func nodeTypeLabelOf(tr mailTr, value string) string {
	for _, o := range mailenums.AutomationNodeTypes {
		if o.Value == value {
			return mailLabel(tr, o.Label)
		}
	}
	return value
}

// automationTriggerLabels 触发方式的取值与文案（key + 中文兜底的真源在 mailenums）。
//
// 下拉选项与列表展示**共用这一份**：此前列表直接把枚举值（manual / contact_created…）
// 打进表格，同一个值在下拉里叫「新联系人产生」、在列表里叫 contact_created ——
// 界面自相矛盾，而且把内部标识露给了用户。

// triggerLabelOf 枚举 → 当前语言的标签；未知枚举原样返回（不吞掉不认识的取值）。
func triggerLabelOf(tr mailTr, value string) string {
	for _, o := range mailenums.AutomationTriggers {
		if o.Value == value {
			return mailLabel(tr, o.Label)
		}
	}
	return value
}

// —— 状态回执 ——
//
// 原先这里有一个 automationStatusLabel(status) 返回中文标签（「已启用 / 已暂停 / 草稿」）。
// 现在取值与文案都在 mailenums.AutomationStatusLabel（key + 中文兜底），拼装只在
// mail_err.go 的 mailAutomationStatusNotice —— 那一份同时是读侧白名单的候选来源。
// 判据没变：只有三个已知状态能进文案，其余（含空串、任意提交值）回落「未知状态」。

// triggerOptions 触发方式下拉的选项（带选中态）。
func triggerOptions(tr mailTr, selected string) []gin.H {
	if selected == "" {
		selected = "manual"
	}
	opts := make([]gin.H, 0, len(mailenums.AutomationTriggers))
	for _, o := range mailenums.AutomationTriggers {
		opts = append(opts, gin.H{"Value": o.Value, "Label": mailLabel(tr, o.Label), "Selected": o.Value == selected})
	}
	return opts
}

// MailAutomationSave 保存流程（原生表单 POST → 302 回编辑页）。
//
// 同一个地址承载两种请求：步骤动作（增 / 删 / 上移 / 下移）与真正保存。动作优先判，
// 它不落库、只回显 —— 这样没有 JS 也能排步骤。
func (h *mailPageHandle) MailAutomationSave(c *gin.Context) {
	tr := shell.TranslateFor(c)
	id := shell.ParseUint(c.PostForm("id"))
	steps := readAutomationSteps(c)

	if act, ok := stepActionOf(c); ok {
		h.renderAutomationForm(c, formValuesFromPost(c), applyStepAction(steps, act), "")
		return
	}

	form := formValuesFromPost(c)
	if form.Name == "" {
		h.renderAutomationForm(c, form, steps, mailLabel(tr, mailenums.AutomationFormErrNameRequired))
		return
	}
	raw, err := buildAutomationDefinition(tr, c.PostForm("entry"), steps, h.automationNodePositions(c, id))
	if err != nil {
		// 表单校验文案由本页组装（「第 3 步：…」），是运营照着改的依据：
		// 走 mailFormErrText 原样回带（判据见 mail_err.go 里对该函数的说明），不进白名单。
		h.renderAutomationForm(c, form, steps, mailFormErrText(err))
		return
	}
	item, err := h.mail.SaveAutomation(c.Request.Context(), &maildto.SaveAutomationReq{
		ID:          id,
		Name:        form.Name,
		Description: form.Description,
		TriggerType: form.Trigger,
		Definition:  raw,
	})
	if err != nil {
		// 图校验失败（有环 / 悬空边 / 不可达 / 形状不对）走到这里，错误里带定位信息；
		// 但也可能是数据库错误 —— 所以必须过白名单：命中 → 翻成中文回带，未命中 → 归口文案 + 日志。
		h.redirectAutomationEdit(c, id, mailErrPageText(c, err))
		return
	}
	// 与 mail_err.go 的 mailCountedNoticeTemplates[0] 同形（数字归一后可判定）。
	ok := fmt.Sprintf(mailCountedNoticeTemplates[0], item.Version)
	c.Redirect(http.StatusFound, fmt.Sprintf("/admin/mail/automation/edit?id=%d&ok=%s", item.ID, urlQueryEscape(ok)))
}

// automationNodePositions 取原图里各节点的画布坐标（key → X / Y）。
//
// 保存只重排连边，不该把用户摆好的画布抹平：丢掉坐标之后，画布页上所有节点会落回原点。
// 拿不到原图（新建流程 / 读失败）时返回空表 —— 位置是装饰，不能因为它挡住保存。
func (h *mailPageHandle) automationNodePositions(c *gin.Context, id uint64) map[string][2]float64 {
	pos := map[string][2]float64{}
	if id == 0 {
		return pos
	}
	item, err := h.mail.GetAutomation(c.Request.Context(), id)
	if err != nil {
		return pos
	}
	for _, n := range item.Nodes {
		if n.Key != "" {
			pos[n.Key] = [2]float64{n.X, n.Y}
		}
	}
	return pos
}

func (h *mailPageHandle) redirectAutomationEdit(c *gin.Context, id uint64, msg string) {
	if id > 0 {
		c.Redirect(http.StatusFound, fmt.Sprintf("/admin/mail/automation/edit?id=%d&err=%s", id, urlQueryEscape(msg)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail/automation/edit?err="+urlQueryEscape(msg))
}

// MailAutomationStatus 启用 / 暂停 / 退回草稿。
func (h *mailPageHandle) MailAutomationStatus(c *gin.Context) {
	id := shell.ParseUint(c.PostForm("id"))
	status := strings.TrimSpace(c.PostForm("status"))
	if err := h.mail.SetAutomationStatus(c.Request.Context(), &maildto.SetAutomationStatusReq{ID: id, Status: status}); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/automation?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail/automation?ok="+urlQueryEscape(mailAutomationStatusNotice(c, status)))
}

// MailAutomationDelete 删除流程。
func (h *mailPageHandle) MailAutomationDelete(c *gin.Context) {
	if err := h.mail.DeleteAutomation(c.Request.Context(), shell.ParseUint(c.PostForm("id"))); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/automation?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail/automation?ok="+urlQueryEscape(mailAutomationDeletedText))
}

// mailAutomationBackParams 批量动作回跳时带回的列表状态（表单字段名 → URL 参数名）。
// 流程列表没有筛选，只带回页码 —— 批量删完被弹回第 1 页会让人重新翻回去。
var mailAutomationBackParams = [][2]string{{"returnPage", "page"}}

// MailAutomationsBulkDelete 批量删除自动化流程（POST /admin/mail/automations/bulk-delete）。
//
// 单条路径 = DeleteAutomation，权限点复用 /api/mail/automation/delete（不新增权限点、不写迁移）：
// 逐条走同一条单条路径，失败只计跳过、不中断整批 —— 批量操作不能因为一条被服务端拒绝
// 就整批回滚（那会让人以为「一条都没做」然后反复重试）。
func (h *mailPageHandle) MailAutomationsBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail/automation", mailAutomationBackParams, "", mailBulkIDsText(c, berr)))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := shell.ParseUint(raw)
		if id == 0 {
			skipped++
			continue
		}
		if err := h.mail.DeleteAutomation(c.Request.Context(), id); err != nil {
			skipped++
			continue
		}
		deleted++
	}
	done, warn := mailBulkOutcome("删除", "自动化流程", deleted, skipped)
	c.Redirect(http.StatusFound, mailBulkLocation(c, "/admin/mail/automation", mailAutomationBackParams, done, warn))
}

// MailAutomationRunDetail 实例排障详情页（「这个人卡在哪一步、为什么」）。
//
// 缺 id 前置判定见 MailCampaignPage 的说明（审计 P0）：改前「没带 id」与「id 查不到」
// 共用 service 的「参数不合法」，用户看不出该去实例列表选一条。
func (h *mailPageHandle) MailAutomationRunDetail(c *gin.Context) {
	id, hasID := mailQueryID(c)
	if !hasID {
		c.Redirect(http.StatusFound, "/admin/mail/automation/runs?err="+urlQueryEscape(mailRunIDRequiredText))
		return
	}
	detail, err := h.mail.AutomationRunDetail(c.Request.Context(), id)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/automation/runs?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	// Explain / error_message / 时间线 detail 都是写侧落下的运行文案编码，出口按语言还原。
	mailRunDetailTexts(shell.TranslateFor(c), detail)
	c.HTML(http.StatusOK, "admin/mail/mail_automation_run.html", shell.Prepare(c, gin.H{
		"title": mailLabel(shell.TranslateFor(c), mailenums.PageTitleAutomationRun),
		"D":     detail,
		"Err":   mailPageErr(c),
	}))
}

// MailAutomationTick 手工补投一轮延时实例（排障：主路径失效时立刻补）。
//
// 按钮在运行记录页上（那是看实例的地方），补投结果也回那一页。
func (h *mailPageHandle) MailAutomationTick(c *gin.Context) {
	n, err := h.mail.EnqueueDueRuns(c.Request.Context(), 500)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/automation/runs?err="+urlQueryEscape(mailErrPageText(c, err)))
		return
	}
	// 与 mail_err.go 的 mailCountedNoticeTemplates[1] 同形（数字归一后可判定）。
	ok := fmt.Sprintf(mailCountedNoticeTemplates[1], n)
	c.Redirect(http.StatusFound, "/admin/mail/automation/runs?ok="+urlQueryEscape(ok))
}

// splitFormList 逗号 / 中文逗号 / 顿号分隔 → 去空白去空项。
func splitFormList(raw string) []string {
	raw = strings.ReplaceAll(raw, "，", ",")
	raw = strings.ReplaceAll(raw, "、", ",")
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func strSlice(v any) []string {
	switch arr := v.(type) {
	case []string:
		return arr
	case []any:
		out := make([]string, 0, len(arr))
		for _, it := range arr {
			if s, ok := it.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
