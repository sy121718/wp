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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	maildto "go_wp/internal/module/mail/dto"
	"go_wp/internal/web/shell"
)

// maxAutomationNodes 表单最多支持多少个节点行。
//
// 有上限不是偷懒：表单式编辑器一旦超过十几行就不好用了 —— 那正是该上拖拽的信号。
const maxAutomationNodes = 12

// nodeTypeOption 类型下拉的一项。
type nodeTypeOption struct {
	Value string
	Label string
	Hint  string
}

// nodeTypeOptions 类型下拉选项（参数提示直接写在界面上，省得去翻文档）。
var nodeTypeOptions = []nodeTypeOption{
	{"trigger", "入口", "流程从这里开始（不需要参数）"},
	{"delay", "等待", "参数填分钟数，例如 1440 表示一天"},
	{"email", "发邮件", "参数填邮件模板的模板 key"},
	{"branch", "条件分支", "参数填条件（逗号分隔）：opened / clicked / subscribed / has_tag:标签；再填 yes 与 no 两条出边"},
	{"tag", "打标签", "参数填要加的标签（逗号分隔）"},
	{"end", "结束", "流程到此结束（不需要参数）"},
}

// MailAutomationPage 流程列表 + 实例列表。
func (h *mailPageHandle) MailAutomationPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := int(shell.ParseUint(c.Query("page")))
	if page <= 0 {
		page = 1
	}
	automations, err := h.mail.ListAutomations(ctx, &maildto.AutomationListReq{Page: page, PageSize: mailMarketingPageSize})
	data := gin.H{
		"title":     "自动化流程",
		"Page":      page,
		"FilterID":  c.Query("automationId"),
		"FilterRun": c.Query("runStatus"),
		"Err":       c.Query("err"),
		"Ok":        c.Query("ok"),
	}
	if err != nil {
		data["Err"] = err.Error()
		c.HTML(http.StatusOK, "admin/mail_automation.html", shell.Prepare(c, data))
		return
	}
	// 列表行按模板需要投影：触发方式给中文标签（枚举不直接进界面），其余字段原样透出。
	autoRows := make([]gin.H, 0, len(automations.Items))
	for _, a := range automations.Items {
		autoRows = append(autoRows, gin.H{
			"ID": a.ID, "Name": a.Name, "Description": a.Description,
			"Status": a.Status, "Version": a.Version,
			"TriggerLabel": triggerLabelOf(a.TriggerType),
		})
	}
	data["Automations"] = autoRows
	data["AutoTotal"] = automations.Total

	// 实例列表（可按流程 / 状态筛）：它是排障入口。
	runs, rerr := h.mail.ListAutomationRuns(ctx, &maildto.AutomationRunListReq{
		AutomationID: shell.ParseUint(c.Query("automationId")),
		Status:       c.Query("runStatus"),
		Page:         1,
		PageSize:     mailMarketingPageSize,
	})
	if rerr == nil {
		data["Runs"] = runs.Items
		data["RunTotal"] = runs.Total
		data["Counts"] = runs.Counts
		data["CountRunning"] = runs.Counts["running"]
		data["CountWaiting"] = runs.Counts["waiting"]
		data["CountCompleted"] = runs.Counts["completed"]
		data["CountFailed"] = runs.Counts["failed"]
		data["CountStopped"] = runs.Counts["stopped"]
	}
	c.HTML(http.StatusOK, "admin/mail_automation.html", shell.Prepare(c, data))
}

// MailAutomationEdit 流程编辑页（?id=N 编辑，缺省为新建）。
func (h *mailPageHandle) MailAutomationEdit(c *gin.Context) {
	ctx := c.Request.Context()
	id := shell.ParseUint(c.Query("id"))
	data := gin.H{
		"title":    "编辑自动化流程",
		"IsNew":    id == 0,
		"MaxRow":   maxAutomationNodes,
		"Types":    nodeTypeOptions,
		"Triggers": triggerOptions(""),
		"Rows":     []gin.H{},
		"Err":      c.Query("err"),
		"Ok":       c.Query("ok"),
	}
	if id > 0 {
		item, err := h.mail.GetAutomation(ctx, id)
		if err != nil {
			c.Redirect(http.StatusFound, "/admin/mail/automation?err="+urlQueryEscape(err.Error()))
			return
		}
		data["A"] = item
		rows := automationFormRows(item)
		// 补足空行：Jet 没有 C 风格 for，用一个固定长度的切片当行模板。
		// 空行在提交时被跳过，所以多给几行是无害的。
		for len(rows) < maxAutomationNodes {
			rows = append(rows, emptyAutomationRow(len(rows)+1))
		}
		data["Rows"] = rows
		data["Entry"] = item.Entry
		data["Triggers"] = triggerOptions(item.TriggerType)
	}
	if all, ok := data["Rows"].([]gin.H); ok && len(all) < maxAutomationNodes {
		for len(all) < maxAutomationNodes {
			all = append(all, emptyAutomationRow(len(all)+1))
		}
		data["Rows"] = all
	}
	// 发信节点要选模板：把模板列表给页面（省得用户手敲 key）。
	templates, _ := h.mail.ListTemplates(ctx, "")
	data["Templates"] = templates
	c.HTML(http.StatusOK, "admin/mail_automation_edit.html", shell.Prepare(c, data))
}

// automationTriggerLabels 触发方式的枚举与中文标签。
//
// 下拉选项与列表展示**共用这一份**：此前列表直接把枚举值（manual / contact_created…）
// 打进表格，同一个值在下拉里叫「新联系人产生」、在列表里叫 contact_created ——
// 界面自相矛盾，而且把内部标识露给了用户。
var automationTriggerLabels = []struct{ Value, Label string }{
	{"manual", "手工添加（后台选人加入）"},
	{"contact_created", "新联系人产生"},
	{"contact_subscribed", "变为已订阅"},
	{"email_opened", "打开过营销邮件"},
	{"email_clicked", "点击过营销链接"},
	{"tag_added", "被打上某个标签"},
}

// triggerLabelOf 枚举 → 中文标签；未知枚举原样返回（不吞掉不认识的取值）。
func triggerLabelOf(value string) string {
	for _, o := range automationTriggerLabels {
		if o.Value == value {
			return o.Label
		}
	}
	return value
}

// triggerOptions 触发方式下拉的选项（带选中态）。
func triggerOptions(selected string) []gin.H {
	if selected == "" {
		selected = "manual"
	}
	opts := make([]gin.H, 0, len(automationTriggerLabels))
	for _, o := range automationTriggerLabels {
		opts = append(opts, gin.H{"Value": o.Value, "Label": o.Label, "Selected": o.Value == selected})
	}
	return opts
}

// automationRowOptions 为某一行生成类型下拉的选项（带选中态）。
//
// 为什么在 Go 里生成而不是模板里嵌套 range：Jet 的内层 range 拿不到外层变量（行本身），
// 没法判断「这一行选的是哪个类型」。与其在模板里绕，不如把选项摊平交给模板。
func automationRowOptions(selected string) []gin.H {
	opts := make([]gin.H, 0, len(nodeTypeOptions)+1)
	opts = append(opts, gin.H{"Value": "", "Label": "（不用这行）", "Selected": selected == ""})
	for _, o := range nodeTypeOptions {
		opts = append(opts, gin.H{"Value": o.Value, "Label": o.Label, "Selected": o.Value == selected})
	}
	return opts
}

// emptyAutomationRow 一行空白表单行。
func emptyAutomationRow(index int) gin.H {
	return gin.H{
		"Index": index, "Key": "", "Type": "", "Param": "",
		"Next": "", "Yes": "", "No": "", "Options": automationRowOptions(""),
	}
}

// automationFormRows 把图定义摊成表单行（原样回填，参数按类型还原成文本）。
func automationFormRows(item *maildto.AutomationItem) []gin.H {
	rows := make([]gin.H, 0, len(item.Nodes))
	for i, n := range item.Nodes {
		row := gin.H{
			"Index":   i + 1,
			"Key":     n.Key,
			"Type":    n.Type,
			"Options": automationRowOptions(n.Type),
			"Next":    n.Next,
			"Yes":     n.Yes,
			"No":      n.No,
			"Param":   "",
		}
		switch n.Type {
		case "delay":
			if v, ok := n.Params["minutes"]; ok {
				row["Param"] = fmt.Sprintf("%v", v)
			}
		case "email":
			row["Param"] = strOf(n.Params["template_key"])
		case "tag":
			row["Param"] = strings.Join(strSlice(n.Params["add"]), ", ")
		case "branch":
			row["Param"] = strings.Join(strSlice(n.Params["conditions"]), ", ")
		}
		rows = append(rows, row)
	}
	return rows
}

// MailAutomationSave 保存流程（原生表单 POST → 302 回编辑页）。
func (h *mailPageHandle) MailAutomationSave(c *gin.Context) {
	req, err := parseAutomationForm(c)
	if err != nil {
		h.redirectAutomationEdit(c, shell.ParseUint(c.PostForm("id")), err.Error())
		return
	}
	item, err := h.mail.SaveAutomation(c.Request.Context(), req)
	if err != nil {
		// 图校验失败（有环 / 悬空边 / 不可达 / 形状不对）走到这里，错误里带定位信息。
		h.redirectAutomationEdit(c, shell.ParseUint(c.PostForm("id")), err.Error())
		return
	}
	ok := "已保存（版本 " + strconv.Itoa(item.Version) + "）"
	c.Redirect(http.StatusFound, fmt.Sprintf("/admin/mail/automation/edit?id=%d&ok=%s", item.ID, urlQueryEscape(ok)))
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
		c.Redirect(http.StatusFound, "/admin/mail/automation?err="+urlQueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail/automation?ok="+urlQueryEscape("状态已更新为 "+status))
}

// MailAutomationDelete 删除流程。
func (h *mailPageHandle) MailAutomationDelete(c *gin.Context) {
	if err := h.mail.DeleteAutomation(c.Request.Context(), shell.ParseUint(c.PostForm("id"))); err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/automation?err="+urlQueryEscape(err.Error()))
		return
	}
	c.Redirect(http.StatusFound, "/admin/mail/automation?ok="+urlQueryEscape("已删除"))
}

// MailAutomationRunDetail 实例排障详情页（「这个人卡在哪一步、为什么」）。
func (h *mailPageHandle) MailAutomationRunDetail(c *gin.Context) {
	detail, err := h.mail.AutomationRunDetail(c.Request.Context(), shell.ParseUint(c.Query("id")))
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/automation?err="+urlQueryEscape(err.Error()))
		return
	}
	c.HTML(http.StatusOK, "admin/mail_automation_run.html", shell.Prepare(c, gin.H{
		"title": "实例排障",
		"D":     detail,
		"Err":   c.Query("err"),
	}))
}

// MailAutomationTick 手工补投一轮延时实例（排障：主路径失效时立刻补）。
func (h *mailPageHandle) MailAutomationTick(c *gin.Context) {
	n, err := h.mail.EnqueueDueRuns(c.Request.Context(), 500)
	if err != nil {
		c.Redirect(http.StatusFound, "/admin/mail/automation?err="+urlQueryEscape(err.Error()))
		return
	}
	ok := fmt.Sprintf("已补投 %d 个到点实例", n)
	c.Redirect(http.StatusFound, "/admin/mail/automation?ok="+urlQueryEscape(ok))
}

// parseAutomationForm 把表单行组装成图定义。
//
// 校验做在**组装这一层**：哪一行、哪个字段错了要直接说出来。
// 等到 SaveAutomation 再报错时，用户已经丢失了「第 3 行」这个上下文。
func parseAutomationForm(c *gin.Context) (*maildto.SaveAutomationReq, error) {
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		return nil, errors.New("流程名称不能为空")
	}
	trigger := strings.TrimSpace(c.PostForm("trigger_type"))
	if trigger == "" {
		trigger = "manual"
	}
	entry := strings.TrimSpace(c.PostForm("entry"))

	nodes := make([]any, 0, maxAutomationNodes)
	for i := 1; i <= maxAutomationNodes; i++ {
		idx := strconv.Itoa(i)
		key := strings.TrimSpace(c.PostForm("node_key_" + idx))
		typ := strings.TrimSpace(c.PostForm("node_type_" + idx))
		if key == "" && typ == "" {
			continue // 空行跳过：表单给足行数，用户只填需要的
		}
		if key == "" || typ == "" {
			return nil, fmt.Errorf("第 %d 行：节点标识与类型都要填", i)
		}
		n := map[string]any{"key": key, "type": typ}
		param := strings.TrimSpace(c.PostForm("param_" + idx))
		params := map[string]any{}
		switch typ {
		case "delay":
			mins, cerr := strconv.Atoi(param)
			if cerr != nil || mins <= 0 {
				return nil, fmt.Errorf("第 %d 行：等待分钟数要填正整数", i)
			}
			params["minutes"] = mins
		case "email":
			if param == "" {
				return nil, fmt.Errorf("第 %d 行：发信节点要选邮件模板", i)
			}
			params["template_key"] = param
		case "branch":
			conds := splitFormList(param)
			if len(conds) == 0 {
				return nil, fmt.Errorf("第 %d 行：条件分支至少填一个条件", i)
			}
			params["conditions"] = conds
		case "tag":
			tags := splitFormList(param)
			if len(tags) == 0 {
				return nil, fmt.Errorf("第 %d 行：标签节点要填要加的标签", i)
			}
			params["add"] = tags
		}
		if len(params) > 0 {
			n["params"] = params
		}
		if v := strings.TrimSpace(c.PostForm("next_" + idx)); v != "" {
			n["next"] = v
		}
		if v := strings.TrimSpace(c.PostForm("yes_" + idx)); v != "" {
			n["yes"] = v
		}
		if v := strings.TrimSpace(c.PostForm("no_" + idx)); v != "" {
			n["no"] = v
		}
		nodes = append(nodes, n)
	}
	if len(nodes) == 0 {
		return nil, errors.New("至少要填一个节点")
	}
	raw, err := json.Marshal(map[string]any{"entry": entry, "nodes": nodes})
	if err != nil {
		return nil, err
	}
	return &maildto.SaveAutomationReq{
		ID:          shell.ParseUint(c.PostForm("id")),
		Name:        name,
		Description: strings.TrimSpace(c.PostForm("description")),
		TriggerType: trigger,
		Definition:  raw,
	}, nil
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
