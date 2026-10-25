package mailhttp

// **画布是流程的唯一编辑器**：带 ?id=N 是编辑，不带 id 是新建。
//
// 此前有两条入口 —— 步骤式表单页（/admin/mail/automation/edit）与画布。两条入口的代价
// 是同一份图有**两套还原器**（表单要「图 → 步骤行 → 图」，画布直接改图），而它们必然
// 滞后于引擎：节点类型一扩，表单的还原逻辑就有一部分表达不了，只能退化成只读兜底页，
// 用户看到的是「这个流程改不了」却说不出原因。表单页已删除，且**不留重定向** ——
// 留一条跳转等于两个入口以另一种形式复活。
//
// 画布承担三件事：把图渲染成可拖的节点、画出连线、把图存回去。连线走画布上的出口圆钮
// 与边的插入菜单（触屏 / 键盘都能用），不再靠侧栏下拉 —— 那种做法只能改「已存在的边」，
// 新建节点的第一步仍然要用户自己去填 key。
//
// 前端模块见 static/js/automation/（state / layout / render / menu / index 五模块 +
// 几何工具 graph.js）；业务判断一律在服务端（nodeArity / ValidateDefinition），
// 画布不自己算权重和、不自己判合法性。

// 为什么单独成页：流程是「配置」（一年改几次），实例是「排障」（出事时盯着看）。
// 挤在一页时，配流程的人要往下滚过几十条与本次操作无关的运行记录；
// 而排障的人要越过整张流程表才能看到实例。拆开后本页只回答「哪些实例需要处理」。

// 为什么从「邮件营销」页拆出来：联系人是**名单**、活动是**发送任务**，
// 两者读的人不同（管名单的人不必每次都扫一遍活动进度表），批量动作也不同。
// 拆开后本页只回答「有哪些活动」，每条活动的报表在 /admin/mail/campaign?id=N。

// 页面语义上反复强调两件事：
//
//	· 只发给**已订阅**的人（pending 未确认同意的绝不发）—— 合规底线；
//	· 导入时「同意声明」不是 UI 便利，而是**留痕**：谁在什么时候声明过什么。

// 与货源管理页同一模式：GET 渲染完整页，POST 写动作的结论由 shell.RenderJump 渲染成
// 整页提示（原生表单 + csrf_token 隐藏域，见 mail_jump.go），不再经 302 + ?err= 回带。
//
// 六页分工（评审规则 admin-ui-logic §2「一页一职能」）：
//
//	· /admin/mail                 —— 发信账号
//	· /admin/mail/templates       —— 邮件模板
//	· /admin/mail/contacts        —— 联系人（含导入）
//	· /admin/mail/campaigns       —— 群发活动
//	· /admin/mail/automation      —— 自动化流程
//	· /admin/mail/automation/runs —— 自动化运行记录
//
// 为什么从「两页」拆成「六页」：账号与模板挤在一页、联系人与活动挤在另一页时，
// 每页承载两个不相干的职能（模板只在发信时被引用、联系人与活动各有自己的批量动作），
// 完成最常见任务要在同一页里上下找。装不下就拆，不折叠（§2 的判据）。
//
// 页面壳层（CSRF / 多语言 / 侧栏 / 权限上下文 / 分页）统一走 internal/shell；
// 页面路由与 API 在同一处装配（mail_router.go → setupMailPageRoutes）。

// 为什么单独成页：模板只在「发信时被引用」，与发信账号没有任何共同的操作对象 ——
// 挤在一页时，日常换一次 SMTP 密码的人每次都要越过一整张模板表才能点保存。
// 拆开后按 admin-ui-logic §2 的判据各答一句话：本页只回答「有哪些模板」。

// 三个端点在**访问面**：无鉴权、无登录态、只做受控的事。
//
//	GET /_t/o/{token}.gif  打开追踪：返回 1×1 透明 GIF，事件异步入队
//	GET /_t/c/{token}      点击追踪：验签 → 记录 → 302 跳转到**签名里的那个 URL**
//	GET /_t/u/{token}      一键退订：写抑制名单 + 改状态，返回一个确认页
//
// 点击跳转的目标**只从 token 里解**，绝不接受请求参数 —— 否则这个端点就是
// 「可信域名 + 任意跳转」的开放重定向，会被用来伪装钓鱼链接。

// 用户概念只有「触发方式 + 按顺序的步骤」（issue #38 P3 重做）：标识 / 下一步 / yes / no
// 四个输入框从界面消失 —— 用户原话是「新建自动化不知道是个什么东西完全没法用」。
// 换算规则固定在这里，别处不许再有一份：
//
//   - **顺序即执行顺序**：第 i 步的 next 就是第 i+1 步的 key；
//   - **末步接结束节点**：末步不是「结束」时自动补一个（key 优先 end），分支臂选
//     「结束」也指向它；
//   - **入口由服务端补**：入口（trigger）节点的 key 来自隐藏域 entry（新建 n1、编辑带回
//     原值），next 指向第一步。用户只需在基本信息里选触发方式，不需要知道「入口节点」是什么；
//   - **只有条件分支要用户指定跳转**：界面上的「第 N 步 / 结束」在提交时换算回节点 key。
//
// 反向（既有流程 → 步骤行）只在**表单能表达的子集**内成立：主链必须从 trigger 入口出发、
// 每个节点都落在主链上、分支只向后跳、条件只有一条。超出子集（环 / 非 trigger 入口 /
// 有节点走不到 / 多条件分支）**不猜也不丢节点** —— 返回 ok=false，由 handler 走只读兜底，
// 用户看到的是原图 + 一个去画布的入口。

// 为什么在出口做：写侧（service）落库的是「key + 参数」编码（见 mail/enums/mail_run_text.go），
// 只有到出口这一层才知道请求语言。页面出口用 mailTranslate(c)（= templates.TranslateFunc
// 按 response.RequestLanguage 取词），JSON API 出口用 i18n.TranslateFunc(...)。
//
// 为什么两个出口都要做：**不做的那一边会退步** —— 页面翻了、API 原样返回编码串，
// 消费方看到的是 `mail.run.explain.waiting\x1f2026-01-02 15:04\x1fn1`。
//
// 旧数据不需要特殊分支：FormatRunText 对中文原文原样返回（判定不通过），
// 所以历史行在排障页上显示的还是当年那句话。

import (
	"fmt"
	json "go_wp/pkg/jsonx"
	"html"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/builder/core"
	mailcontract "go_wp/internal/module/mail/contract"
	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	mailmodel "go_wp/internal/module/mail/model"
	"go_wp/internal/templates"
	bulkids "go_wp/pkg/bulkids"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	"go_wp/pkg/utils"
)

// MailAutomationCanvas 流程画布页。
//
// **这是流程的唯一编辑器**（列表页只有一个「编辑」按钮指向这里）：带 ?id=N 是编辑，
// 不带 id 是新建 —— 新建也走画布，不再有第二个表单页（两条入口的代价是同一份图有
// 两套还原逻辑，而它们必然滞步）。缺 id 不再当异常：给一份只含入口节点的空图。
func (h *mailPageHandle) MailAutomationCanvas(c *gin.Context) {
	item, ok := h.automationForCanvas(c)
	if !ok {
		return
	}
	nodesJSON, merr := json.Marshal(item.Nodes)
	if merr != nil {
		nodesJSON = []byte("[]")
	}
	metaJSON, _ := json.Marshal(h.automationCanvasMeta(c, item))
	// 发信节点要选模板：把模板列表给页面（省得用户手敲 key）。
	templates, _ := h.mail.ListTemplates(c.Request.Context(), "")
	c.HTML(http.StatusOK, "admin/mail/mail_automation_canvas.html", adminui.Prepare(c, gin.H{
		"title":     mailenums.PageTitleAutomationCanvas.Key,
		"A":         item,
		"NodesJSON": utils.JsonSafe(string(nodesJSON)),
		"MetaJSON":  utils.JsonSafe(string(metaJSON)),
		"Templates": templates,
		"jsVer":     automationJsVer(),
		// 写动作的结论不再回显在本页（走 shell.RenderJump 提示页，见 mail_jump.go）；
		// 本页也没有取数失败分支（失败已在上面渲染提示页），所以不再注入 Err。
		// 按需内联动效关键帧（本次只用到入场一个）。不声明就是零字节 ——
		// 后台不常驻加载 63 条营销动效，谁用谁声明（见 core.KeyframeCSS）。
		"KeyframesCSS": core.KeyframeCSS([]string{"sky-fade-up"}),
	}))
}

// automationForCanvas 取画布要编辑的流程：带 id 读库，不带 id 给空图。
//
// 第二个返回 false 表示已经渲染了提示页（调用方直接 return）。
// 新建分支的图只含入口节点：它与迁移后的 v2 形状逐字一致（schema + out），
// 画布不需要知道「这份图是刚造的」—— 它只看见一份合法的图定义。
func (h *mailPageHandle) automationForCanvas(c *gin.Context) (*maildto.AutomationItem, bool) {
	id := reqparams.ParseUint(c.Query("id"))
	if id == 0 {
		return newAutomationDraft(), true
	}
	item, err := h.mail.GetAutomation(c.Request.Context(), id)
	if err != nil {
		mailAutomationJump(c, false, mailErrMsg(c, err))
		return nil, false
	}
	return item, true
}

// newAutomationDraft 新建流程时的空图（入口 + 一条未连的出边）。
//
// 只造图、不落库：用户在画布上画完、点保存才建行。否则「点开新建 → 关掉」会在库里
// 留下一堆没有节点的草稿。
func newAutomationDraft() *maildto.AutomationItem {
	return &maildto.AutomationItem{
		Name:        "",
		TriggerType: mailmodel.TriggerManual,
		Status:      mailmodel.AutomationStatusDraft,
		Version:     0,
		Entry:       automationEntryKeyDefault,
		Nodes: []maildto.AutomationNodeItem{
			{Key: automationEntryKeyDefault, Type: "trigger"},
		},
	}
}

// automationCanvasMeta 画布页的元数据（前端只读这一份，不自己推导）。
//
// 节点类型表也在这里：**palette、出口菜单、结构树的白名单都读它**，不在前端硬编码
// 第二份（两处名字一对不上，用户就在 palette 里选得到但保存被拒的节点）。
func (h *mailPageHandle) automationCanvasMeta(c *gin.Context, item *maildto.AutomationItem) map[string]any {
	tr := mailTranslate(c)
	nodeTypes := make([]gin.H, 0, len(mailenums.AutomationNodeTypes))
	for _, o := range mailenums.AutomationNodeTypes {
		nodeTypes = append(nodeTypes, gin.H{
			"value": o.Value,
			"label": mailLabel(tr, o.Label),
			"hint":  mailLabel(tr, o.Hint),
		})
	}
	return map[string]any{
		"automationId": item.ID,
		"name":         item.Name,
		"description":  item.Description,
		"triggerType":  item.TriggerType,
		"entry":        item.Entry,
		"status":       item.Status,
		"version":      item.Version,
		"isNew":        item.ID == 0,
		// 只有草稿 / 已暂停才允许调结构（启用中的流程改图会让在跑的实例走岔）。
		// 新建的流程恒可编辑（它还没落库，state 是 draft）。
		"structureEditable": item.Status != mailmodel.AutomationStatusActive,
		"nodeTypes":         nodeTypes,
	}
}

// automationJsVer 自动化编辑器脚本的缓存版本（模块目录下 .js 的最新 mtime）。
//
// 与 workbenchJsVer 同一手法：开发期改 JS 不必手动升版本号，
// 任一模块改动都会让入口 URL 的 ?v= 变化，浏览器不会再执行旧模块。
func automationJsVer() string {
	root := filepath.Join("internal", "templates", "static", "js", "automation")
	var latest int64
	_ = filepath.Walk(root, func(_ string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // 目录缺失不阻断渲染，版本退化为 0
		}
		if fi.IsDir() || !strings.HasSuffix(fi.Name(), ".js") {
			return nil
		}
		if m := fi.ModTime().Unix(); m > latest {
			latest = m
		}
		return nil
	})
	if latest == 0 {
		return "0"
	}
	return strconv.FormatInt(latest, 10)
}

// maxAutomationNodes 表单最多支持多少个节点行。
//
// 有上限不是偷懒：表单式编辑器一旦超过十几行就不好用了 —— 那正是该上拖拽的信号。
const maxAutomationNodes = 12

// mailAutomationPageSize 流程列表每页条数（运行记录页用自己的 mailAutomationRunsPageSize）。
const mailAutomationPageSize = 50

// automationEntryKeyDefault 新建流程时入口节点的 key。
//
// 固定为 n1 而不是随机串：它进图的 entry 字段、也进运行实例的 current_node，一个可读的固定值
// 在排障页上比 uuid 有用，且新建的图只有这一个节点，不存在冲突。
const automationEntryKeyDefault = "n1"

// mailTr 取词函数签名（= 后台模板注入的 .["t"]：templates.TranslateFunc 按
// response.RequestLanguage 取词，与 shell.TranslateFor 是同一实现）。
type mailTr func(key, fallback string) string

// mailTranslate 本模块的取词函数（先例见 user 模块的 userTr）。
//
// 直引 templates.TranslateFunc(response.RequestLanguage(c))：shell.TranslateFor 逐字就是
// 这两行，而 shell 那一层是过渡转发，终局归属是 templates / pkg/i18n。
//
// 为什么本模块仍有 Go 侧取词：这几处**没有模板可代办** —— 访客面出口走 c.String、
// JSON 逐条原因要成品文本、下拉选项与运行文案还原是数据字段。凡是「给模板放一句话」的
// 槽位一律改成事实（mailMsg，见 mail_jump.go），取词交给渲染期。
func mailTranslate(c *gin.Context) mailTr {
	return templates.TranslateFunc(response.RequestLanguage(c))
}

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

// nodeTypeOptions 节点类型选项（取词后的形状）。
//
// 取值与文案的真源在 mailenums.AutomationNodeTypes（key + 中文兜底），这里只做取词。
// 画布的 palette 与结构树读的是 meta.nodeTypes（同一份 enums），本函数给 Go 侧
// 需要直接渲染的场景用。
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

// nodeTypeLabelOf 节点类型 → 当前语言标签；未知类型原样返回（兜底表要能显示真实取值）。
func nodeTypeLabelOf(tr mailTr, value string) string {
	for _, o := range mailenums.AutomationNodeTypes {
		if o.Value == value {
			return mailLabel(tr, o.Label)
		}
	}
	return value
}

// triggerLabelOf 枚举 → 当前语言的标签；未知枚举原样返回（不吞掉不认识的取值）。
func triggerLabelOf(tr mailTr, value string) string {
	for _, o := range mailenums.AutomationTriggers {
		if o.Value == value {
			return mailLabel(tr, o.Label)
		}
	}
	return value
}

// triggerOptions 触发方式下拉的选项（带选中态）。
func triggerOptions(tr mailTr, selected string) []gin.H {
	if selected == "" {
		selected = mailmodel.TriggerManual
	}
	opts := make([]gin.H, 0, len(mailenums.AutomationTriggers))
	for _, o := range mailenums.AutomationTriggers {
		opts = append(opts, gin.H{"Value": o.Value, "Label": mailLabel(tr, o.Label), "Selected": o.Value == selected})
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
	tr := mailTranslate(c)
	page := int(reqparams.ParseUint(c.Query("page")))
	if page <= 0 {
		page = 1
	}
	automations, err := h.mail.ListAutomations(ctx, &maildto.AutomationListReq{Page: page, PageSize: mailAutomationPageSize})
	data := gin.H{
		"title": mailLabel(tr, mailenums.PageTitleAutomation),
		"Page":  page,
		// 批量删除表单 action 的 query：回跳时由 shell.BackPath 读回，批量删完不会被弹回第 1 页。
		"ListQuery": mailPageQuery(page),
		// 写动作的结论不再回显在本页（走 shell.RenderJump 提示页，见 mail_jump.go）；
		// Err 只由下面的取数失败分支注入。
	}
	if err != nil {
		// 取数失败：归口文案 + 空列表。
		// 空列表不可省 —— 模板随后就用 len(.Automations) / .AutoTotal 渲染列表，
		// 缺键会让 Jet 在那一行中断（HTTP 仍是 200、正文整块消失）。
		data["Err"] = mailErrPageText(c, err)
		data["Automations"] = []any{}
		data["AutoTotal"] = 0
		c.HTML(http.StatusOK, "admin/mail/mail_automation.html", adminui.Prepare(c, data))
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
	c.HTML(http.StatusOK, "admin/mail/mail_automation.html", adminui.Prepare(c, data))
}

// MailAutomationStatus 启用 / 暂停 / 退回草稿。
func (h *mailPageHandle) MailAutomationStatus(c *gin.Context) {
	id := reqparams.ParseUint(c.PostForm("id"))
	status := strings.TrimSpace(c.PostForm("status"))
	if err := h.mail.SetAutomationStatus(c.Request.Context(), &maildto.SetAutomationStatusReq{ID: id, Status: status}); err != nil {
		mailAutomationJump(c, false, mailErrMsg(c, err))
		return
	}
	mailAutomationJump(c, true, mailMsg{Fallback: mailAutomationStatusNotice(c, status)})
}

// MailAutomationDelete 删除流程。
func (h *mailPageHandle) MailAutomationDelete(c *gin.Context) {
	if err := h.mail.DeleteAutomation(c.Request.Context(), reqparams.ParseUint(c.PostForm("id"))); err != nil {
		mailAutomationJump(c, false, mailErrMsg(c, err))
		return
	}
	mailAutomationJump(c, true, mailAutomationDeletedMsg)
}

// mailAutomationsBulkDelete 批量删除自动化流程（POST /admin/mail/automations/bulk-delete）。
//
// 单条路径 = DeleteAutomation，权限点复用 /api/mail/automation/delete（不新增权限点、不写迁移）：
// 逐条走同一条单条路径，失败只计跳过、不中断整批 —— 批量操作不能因为一条被服务端拒绝
// 就整批回滚（那会让人以为「一条都没做」然后反复重试）。
func (h *mailPageHandle) MailAutomationsBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（pkg/bulkids：去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := bulkids.Normalize(c.PostFormArray("ids"))
	if berr != nil {
		mailAutomationJump(c, false, mailBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := reqparams.ParseUint(raw)
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
	mailBulkJump(c, done, warn, mailAutomationBack(c), mailAutomationBackMsg)
}

// MailAutomationRunDetail 实例排障详情页（「这个人卡在哪一步、为什么」）。
//
// 缺 id 前置判定见 MailCampaignPage 的说明（审计 P0）：改前「没带 id」与「id 查不到」
// 共用 service 的「参数不合法」，用户看不出该去实例列表选一条。
func (h *mailPageHandle) MailAutomationRunDetail(c *gin.Context) {
	id, hasID := mailQueryID(c)
	if !hasID {
		// 缺 id 是**引导**（不是异常输入）：渲染提示页并给出回实例列表的入口。
		mailAutomationRunsJump(c, false, mailRunIDRequiredMsg)
		return
	}
	detail, err := h.mail.AutomationRunDetail(c.Request.Context(), id)
	if err != nil {
		mailAutomationRunsJump(c, false, mailErrMsg(c, err))
		return
	}
	// Explain / error_message / 时间线 detail 都是写侧落下的运行文案编码，出口按语言还原。
	mailRunDetailTexts(mailTranslate(c), detail)
	c.HTML(http.StatusOK, "admin/mail/mail_automation_run.html", adminui.Prepare(c, gin.H{
		"title": mailenums.PageTitleAutomationRun.Key,
		"D":     detail,
		// 写动作的结论不再回显在本页（走 shell.RenderJump 提示页，见 mail_jump.go）。
	}))
}

// MailAutomationTick 手工补投一轮延时实例（排障：主路径失效时立刻补）。
//
// 按钮在运行记录页上（那是看实例的地方），补投结果也回那一页。
func (h *mailPageHandle) MailAutomationTick(c *gin.Context) {
	n, err := h.mail.EnqueueDueRuns(c.Request.Context(), 500)
	if err != nil {
		mailAutomationRunsJump(c, false, mailErrMsg(c, err))
		return
	}
	// 与 mail_err.go 的 mailAutomationTickMsg 同一句话（补投条数是位置参数）。
	mailAutomationRunsJump(c, true, mailAutomationTickMsg.WithArgs(strconv.Itoa(n)))
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

// mailAutomationRunsPageSize 运行实例每页条数（与流程页共用同一条每页约定）。
const mailAutomationRunsPageSize = 50

// MailAutomationRunsPage 运行记录页：实例计数 + 筛选（流程 / 状态）+ 实例表 + 分页。
//
// 计数（运行中 / 等待 / 已完成 / 失败 / 已停止）来自 service 的 Counts，
// 是排障时的第一判断：有多少在等、有多少已经失败。
func (h *mailPageHandle) MailAutomationRunsPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := mailTranslate(c)
	page := mailPageNumber(c.Query("page"))
	automationID, runStatus := c.Query("automationId"), c.Query("runStatus")

	runs, err := h.mail.ListAutomationRuns(ctx, &maildto.AutomationRunListReq{
		AutomationID: reqparams.ParseUint(automationID),
		Status:       runStatus,
		Page:         page,
		PageSize:     mailAutomationRunsPageSize,
	})
	data := gin.H{
		"title":     mailLabel(tr, mailenums.PageTitleAutomationRuns),
		"Page":      page,
		"FilterID":  automationID,
		"FilterRun": runStatus,
		// 补投表单 action 的 query：回跳时由 shell.BackPath 读回，翻到第 2 页补投后
		// 不会被弹回未筛选的第 1 页。写动作的结论不在本页回显（走 shell.RenderJump）。
		"RunsQuery": mailAutomationRunsListQuery(automationID, runStatus, page),
	}
	if err != nil {
		// 取数失败：归口文案 + 空列表 + 五个计数键。
		// 模板随后就用 len(.Runs) / .RunTotal 与计数渲染，缺任一项都会让 Jet
		// 在那一行中断（HTTP 仍是 200、正文整块消失）。
		data["Err"] = mailErrPageText(c, err)
		data["Runs"] = []any{}
		data["RunTotal"] = int64(0)
		data["CountRunning"], data["CountWaiting"], data["CountCompleted"] = 0, 0, 0
		data["CountFailed"], data["CountStopped"] = 0, 0
		c.HTML(http.StatusOK, "admin/mail/mail_automation_runs.html", adminui.Prepare(c, data))
		return
	}
	// 运行文案（error_message）落库时是「key + 参数」编码，到出口才按语言还原。
	mailRunTexts(tr, runs.Items)
	data["Runs"] = runs.Items
	data["RunTotal"] = runs.Total
	data["Counts"] = runs.Counts
	data["CountRunning"] = runs.Counts["running"]
	data["CountWaiting"] = runs.Counts["waiting"]
	data["CountCompleted"] = runs.Counts["completed"]
	data["CountFailed"] = runs.Counts["failed"]
	data["CountStopped"] = runs.Counts["stopped"]

	// 分页条的基地址带上筛选：翻页时不能丢掉「在看哪个流程 / 哪个状态」，
	// 否则翻到第 2 页就变成全量实例（而人以为自己还在筛选里）。
	base := reqparams.FilterBaseURL("/admin/mail/automation/runs", map[string]string{
		"automationId": automationID,
		"runStatus":    runStatus,
	})
	// 分页只给事实：总数 / 页码 / 每页 / 基址由本行注入，信息行、翻页按钮与链接
	// 全部由 admin/partials/pagination.html 按请求语言渲染。单页 / 空数据不注入键
	// （与迁移前的空 map 同义），片段里 isset 守卫自然跳过。
	adminui.AttachPagination(data, "", nil, pagination.Page{
		Total: runs.Total, Page: page, Limit: mailAutomationRunsPageSize, Base: base,
	})
	c.HTML(http.StatusOK, "admin/mail/mail_automation_runs.html", adminui.Prepare(c, data))
}

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
	tr := mailTranslate(c)
	page := mailPageNumber(c.Query("page"))

	campaigns, err := h.mail.ListCampaigns(ctx, &maildto.CampaignListReq{Page: page, PageSize: mailCampaignsPageSize})
	if err != nil {
		c.HTML(http.StatusOK, "admin/mail/mail_campaigns.html", adminui.Prepare(c, mailCampaignsErrData(c, err)))
		return
	}
	if fixed := mailCampaignsClampPage(page, campaigns.Total); fixed != page {
		page = fixed
		if campaigns, err = h.mail.ListCampaigns(ctx, &maildto.CampaignListReq{Page: page, PageSize: mailCampaignsPageSize}); err != nil {
			c.HTML(http.StatusOK, "admin/mail/mail_campaigns.html", adminui.Prepare(c, mailCampaignsErrData(c, err)))
			return
		}
	}
	// 新建活动抽屉要在页内选发信账号与模板：两份选项列表随页提供（失败不阻塞列表，
	// 账号 / 模板列表取不到时抽屉里的下拉就是空的，列表本身仍然可用）。
	accounts, _ := h.mail.ListAccounts(ctx, "")
	templates, _ := h.mail.ListTemplates(ctx, "")

	data := adminui.Prepare(c, gin.H{
		"title":         mailLabel(tr, mailenums.PageTitleCampaigns),
		"Campaigns":     campaigns.Items,
		"CampaignTotal": campaigns.Total,
		"Accounts":      accounts,
		"Templates":     templates,
		"Page":          page,
		// 批量删除表单 action 的 query：回跳时由 shell.BackPath 读回，批量删完不会
		// 被弹回第 1 页。写动作的结论不在本页回显（走 shell.RenderJump 提示页）。
		"ListQuery": mailPageQuery(page),
	})
	adminui.AttachPagination(data, "", nil, pagination.Page{
		Total: campaigns.Total, Page: page, Limit: mailCampaignsPageSize, Base: "/admin/mail/campaigns",
	})
	c.HTML(http.StatusOK, "admin/mail/mail_campaigns.html", data)
}

// mailCampaignsErrData 取数失败时的页面数据。
//
// 空值不是可选的：admin/mail/mail_campaigns.html 在提示条之后就用 .Page / .CampaignTotal /
// len(.Campaigns) 渲染列表，缺键会让 Jet **在那一行中断**（HTTP 仍是 200、正文整块消失）。
func mailCampaignsErrData(c *gin.Context, err error) gin.H {
	return gin.H{
		"title":         mailenums.PageTitleCampaigns.Key,
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
		ID:         reqparams.ParseUint(c.PostForm("id")),
		Name:       c.PostForm("name"),
		AccountID:  reqparams.ParseUint(c.PostForm("account_id")),
		TemplateID: reqparams.ParseUint(c.PostForm("template_id")),
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
		mailCampaignsJump(c, false, mailErrMsg(c, err))
		return
	}
	mailCampaignsJump(c, true, mailDoneMsg)
}

// MailCampaignStart 启动群发（HTTP 秒回；展开在后台分批进行）。
func (h *mailPageHandle) MailCampaignStart(c *gin.Context) {
	res, err := h.mail.StartCampaign(c.Request.Context(), &maildto.StartCampaignReq{
		CampaignID: reqparams.ParseUint(c.PostForm("id")),
	})
	if err != nil {
		mailCampaignsJump(c, false, mailErrMsg(c, err))
		return
	}
	// 与 mail_err.go 的 mailCampaignStartedMsg 同一句话（目标人数是位置参数）。
	mailCampaignsJump(c, true, mailCampaignStartedMsg.WithArgs(strconv.FormatInt(res.Total, 10)))
}

// MailCampaignDelete 删除活动。
func (h *mailPageHandle) MailCampaignDelete(c *gin.Context) {
	if err := h.mail.DeleteCampaign(c.Request.Context(), reqparams.ParseUint(c.PostForm("id"))); err != nil {
		mailCampaignsJump(c, false, mailErrMsg(c, err))
		return
	}
	mailCampaignsJump(c, true, mailDoneMsg)
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
		// 缺 id 是**引导**（本页是菜单项，点菜单进来必然没带 id）：渲染提示页并给出
		// 回活动列表的入口，而不是静默 302（静默弹回才会让菜单看起来是坏的）。
		mailJump(c, false, mailCampaignIDRequiredMsg, mailCampaignsPath, mailCampaignsBackMsg)
		return
	}
	page := mailPageNumber(c.Query("page"))
	report, err := h.mail.CampaignReport(ctx, id, page, mailCampaignsPageSize)
	if err != nil {
		// 回跳用固定路径（不带本页的 ?page=：那是收件人明细的页码，不是活动列表的页码）。
		mailJump(c, false, mailErrMsg(c, err), mailCampaignsPath, mailCampaignsBackMsg)
		return
	}
	// 页号收敛：报表服务端不收敛（页码越界时收件人明细为空、Total 仍是真值），
	// 不处理会把「这条活动有 300 个收件人、只是页码落到第 9 页」渲染成
	// 「还没有投递记录」的空态，同时分页条还显示第 9 页 —— 两者自相矛盾。
	if report.Total > 0 {
		if maxPage := int((report.Total + mailCampaignsPageSize - 1) / mailCampaignsPageSize); page > maxPage {
			page = maxPage
			if report, err = h.mail.CampaignReport(ctx, id, page, mailCampaignsPageSize); err != nil {
				mailJump(c, false, mailErrMsg(c, err), mailCampaignsPath, mailCampaignsBackMsg)
				return
			}
		}
	}
	data := adminui.Prepare(c, gin.H{
		"title": mailenums.PageTitleCampaignReport.Key,
		"R":     report,
		"Page":  page,
		// 写动作的结论不在本页回显（走 shell.RenderJump 提示页，见 mail_jump.go）；
		// 本页也没有取数失败分支（失败已在上面渲染提示页）。
	})
	// 收件人明细的分页条：baseURL 带上 id，翻页时不会丢掉「在看哪条活动」。
	// 键名与 partials/pagination.html 读的键一致（该片段在这里用无参 include 渲染）。
	adminui.AttachPagination(data, "", nil, pagination.Page{
		Total: report.Total, Page: page, Limit: mailCampaignsPageSize,
		Base: fmt.Sprintf("/admin/mail/campaign?id=%d", id),
	})
	c.HTML(http.StatusOK, "admin/mail/mail_campaign.html", data)
}

// MailCampaignReportJSON 报表数据接口（图表 / 外部核对用同一份口径）。
//
// 目前没有路由指向它（保留给外部核对脚本）—— 拆分本轮只是随活动域搬家，未注册路由。
func (h *mailPageHandle) MailCampaignReportJSON(c *gin.Context) {
	page := int(reqparams.ParseUint(c.Query("page")))
	if page <= 0 {
		page = 1
	}
	report, err := h.mail.CampaignReport(c.Request.Context(), reqparams.ParseUint(c.Query("id")), page, mailCampaignsPageSize)
	if err != nil {
		adminui.PageErrorBadRequest(c, "mail_campaigns", err)
		return
	}
	response.Success(c, report)
}

// MailCampaignsBulkDelete 批量删除群发活动（POST /admin/mail/campaigns/bulk-delete）。
//
// 单条路径 = DeleteCampaign，权限点复用 /api/mail/campaign/delete。
// 发送中的活动由服务端拒绝（ErrCampaignSending）→ 只跳过它、其余照常删除：
// 删掉发送中的活动，后台分批展开的收件人任务会在中途找不到活动，留下一批半截记录。
func (h *mailPageHandle) MailCampaignsBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（pkg/bulkids：去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := bulkids.Normalize(c.PostFormArray("ids"))
	if berr != nil {
		mailCampaignsJump(c, false, mailBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := reqparams.ParseUint(raw)
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
	mailBulkJump(c, done, warn, mailCampaignsBack(c), mailCampaignsBackMsg)
}

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
	tr := mailTranslate(c)
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
		c.HTML(http.StatusOK, "admin/mail/mail_contacts.html", adminui.Prepare(c, mailContactsErrData(c, err)))
		return
	}
	// 页号收敛（见 mailContactsClampPage）：越界时用收敛后的页码重取一次。
	// 只在越界这一种情况下多一次查询，正常翻页仍是原来的一次。
	if fixed := mailContactsClampPage(page, contacts.Total); fixed != page {
		page = fixed
		if contacts, err = filter(); err != nil {
			c.HTML(http.StatusOK, "admin/mail/mail_contacts.html", adminui.Prepare(c, mailContactsErrData(c, err)))
			return
		}
	}

	// 筛选区的标签候选：只读、失败不影响页面 —— 候选只是输入提示，
	// 缺了它 datalist 为空，输入框仍然可用（所以这里不把错误升级成整页错误分支）。
	tagOptions, tagErr := h.mail.ListContactTags(ctx)
	if tagErr != nil {
		tagOptions = nil
	}

	data := adminui.Prepare(c, gin.H{
		"title":        mailLabel(tr, mailenums.PageTitleContacts),
		"Contacts":     contacts.Items,
		"ContactTotal": contacts.Total,
		"Page":         page,
		"Keyword":      keyword,
		"Status":       status,
		"Tags":         tags,
		"TagOptions":   tagOptions,
		// 写动作表单 action 的 query（筛选 + 页码）：回跳时由 shell.BackPath 读回，
		// 批量改完不会被弹回未筛选的第 1 页。写动作的结论不在本页回显（走 shell.RenderJump）。
		"ListQuery": mailContactsListQuery(keyword, status, tags, page),
	})
	// 基地址带上筛选，翻页才保留筛选条件（否则翻到第 2 页就回到全量）。
	adminui.AttachPagination(data, mailContactsPath,
		map[string]string{"keyword": keyword, "status": status, "tags": tags},
		pagination.Page{Total: contacts.Total, Page: page, Limit: mailContactsPageSize})
	c.HTML(http.StatusOK, "admin/mail/mail_contacts.html", data)
}

// mailContactsErrData 取数失败时的页面数据。
//
// 空值不是可选的：admin/mail/mail_contacts.html 在提示条之后就用 .Keyword / .Status /
// .Page / .ContactTotal / len(.Contacts) 渲染筛选与列表，缺键会让 Jet **在那一行中断**
// （HTTP 仍是 200、正文整块消失）。只注入 Err 的分支因此渲染不完。
// mailContactsPath 联系人页地址（回跳与拼接回执只用这一个字面量）。
const mailContactsPath = "/admin/mail/contacts"

// mailContactsListQuery 联系人页写动作表单 action 的查询串（筛选 + 页码）。
//
// 与回跳键表 mailContactsBackKeys 同一份键名：页面拼进 action、服务端用 shell.BackPath 读回，
// 两处分叉的表现是「写完跳回去筛选静默丢了」—— 页面不报错、日志也干净。
func mailContactsListQuery(keyword, status, tags string, page int) string {
	params := map[string]string{"keyword": keyword, "status": status, "tags": tags}
	if page > 1 {
		params["page"] = strconv.Itoa(page)
	}
	return mailListQuery(params)
}

func mailContactsErrData(c *gin.Context, err error) gin.H {
	return gin.H{
		"title":        mailenums.PageTitleContacts.Key,
		"Err":          mailErrPageText(c, err),
		"Contacts":     []any{},
		"ContactTotal": int64(0),
		"Page":         1,
		"Keyword":      "",
		"Status":       "",
		"Tags":         "",
		"TagOptions":   []string{},
		// 取数失败时没有筛选上下文：表单 action 的 query 为空（回跳落到未筛选的列表）。
		"ListQuery": "",
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
		mailContactsJump(c, false, mailErrMsg(c, err))
		return
	}
	// 与 mail_err.go 的 mailContactImportMsg 同一句话（五个计数是位置参数）。
	mailContactsJump(c, true, mailContactImportMsg.WithArgs(
		strconv.Itoa(res.Imported), strconv.Itoa(res.Updated), strconv.Itoa(res.Skipped),
		strconv.Itoa(res.Suppressed), strconv.Itoa(len(res.Errors))))
}

// MailContactStatus 改联系人同意状态。
func (h *mailPageHandle) MailContactStatus(c *gin.Context) {
	req := &maildto.UpdateContactStatusReq{
		ID:     reqparams.ParseUint(c.PostForm("id")),
		Status: c.PostForm("status"),
		Note:   c.PostForm("note"),
	}
	if err := h.mail.UpdateContactStatus(c.Request.Context(), req); err != nil {
		mailContactsJump(c, false, mailErrMsg(c, err))
		return
	}
	mailContactsJump(c, true, mailDoneMsg)
}

// —— 批量动作（评审规则 admin-ui-logic §7：列表首列勾选 + 批量条）——
//
// 与账号 / 模板的批量删除同一形状（见 mail_page.go 的 mailBulkOutcome 与 mail_jump.go 的
// mailBulkJump）：逐条走**同一条单条路径**，失败只计跳过、不中断整批，结论按「成功 N / 跳过 M」
// 渲染成提示页。权限点一律复用对应单条动作的路径，不新增权限点、不写迁移。
//
// 回跳的筛选上下文由 shell.BackPath 从**表单 action 的 query** 读回（键表 mailContactsBackKeys）：
// 页面不再用 return* 隐藏域塞上下文，服务端也不必再按表单字段名转写一遍。

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
		mailContactsJump(c, false, mailContactStatusBadMsg)
		return
	}
	// 批量 id 统一入口（pkg/bulkids：去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := bulkids.Normalize(c.PostFormArray("ids"))
	if berr != nil {
		mailContactsJump(c, false, mailBulkIDsMsg(c, berr))
		return
	}
	changed, skipped := 0, 0
	for _, raw := range ids {
		id := reqparams.ParseUint(raw)
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
	mailBulkJump(c, done, warn, mailContactsBack(c), mailContactsBackMsg)
}

// —— 联系人写动作：**已全部迁到 JSON 端点** ——
//
// 新建 / 编辑 / 单条删除 / 批量删除 / 批量打标签都由浏览器直调真实端点
// （POST /api/mail/contact/{save,delete,tag}，见 mail_contact_write.go）：
// 页面表单/按钮带 data-write-api（端点声明在**按钮**上，见 mail_contacts.html 的批量条注释），
// 页面 POST 不再做转发，所以这里既没有单条 save/delete，也没有批量入口。
//
// 留在这里的只有**没有 JSON 端点**的批量改状态（MailContactsBulkStatus，上面那条）：
// 服务端改状态要同步写抑制名单，语义与单条抽屉一致，仍走页面 POST。
//
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

// parseFormIDs 把批量表单提交的字符串 id 转成 uint64，并统计非法项。
// id 已过 pkg/bulkids 的归一（去空白 / 去重 / 上限），这里只管类型转换与非法项计数。
//
// 非法项不静默丢弃：回执里的「跳过 M」要如实包含它们，否则运营点了 4 个、
// 回执写「已更新 3 个」，会以为系统漏了一条。
func parseFormIDs(raw []string) (ids []uint64, invalid int) {
	ids = make([]uint64, 0, len(raw))
	for _, s := range raw {
		if v := reqparams.ParseUint(s); v > 0 {
			ids = append(ids, v)
			continue
		}
		invalid++
	}
	return ids, invalid
}

// mailPageHandle 邮箱后台页处理器。
type mailPageHandle struct {
	mail mailcontract.MailService
}

// 两张列表的页码互不影响；原有无分页调用仍使用全量查询。
const mailListPageSize = 20

func mailPageNumber(raw string) int {
	page := int(reqparams.ParseUint(raw))
	if page < 1 {
		return 1
	}
	return page
}

// mailListPaginationFacts 造列表页分页事实（列表页共用：账号页 / 模板页）。
//
// 为什么还要传 param：拆页前同一页有两张表、共用一个 ?page=，必须靠 account_page /
// template_page 区分；拆页后账号表仍沿用 account_page —— 它是既有书签与测试的契约。
// limit 每页固定，KeepLimit=false 让链接不带它（避免出现「翻页改条数」这种页面并不支持的操作）。
//
// 历史：本函数此前逐条改写 BuildPagination 产出的链接（url.Parse → 改名 → 删 limit），
// 那条路径在 param == "page"（模板页）时会把刚设好的页码一并删掉，链接退化成不带 ?page=
// 的基址（翻页失效）。改为事实层后链接由 partial 用 Base + PageParam 生成，这一缺陷随
// 旧路径一起消失 —— 属**有意的行为修复**，见 task-105 卡面与渲染测试的同名注释。
func mailListPaginationFacts(total int64, page int, param, base string) pagination.Page {
	pg := pagination.Page{Total: total, Page: page, Limit: mailListPageSize}
	pg.Base = base
	pg.PageParam = param
	pg.DropLimit = true
	return pg
}

// NewMailPageHandle 构造邮箱后台页处理器（与 NewHandle 同风格；
// 装配走它，页面测试也走它 —— 测试不该为了拿到 handle 而装配整棵后台路由树）。
func NewMailPageHandle(mail mailcontract.MailService) *mailPageHandle {
	return &mailPageHandle{mail: mail}
}

// MailPage 发信账号页（只回答「有哪些发信账号」，模板搬到 /admin/mail/templates）。
func (h *mailPageHandle) MailPage(c *gin.Context) {
	ctx := c.Request.Context()
	// 取数失败只回一条归口文案（service 的 i18n key 翻成中文；基础设施错误只进日志）。
	//
	// **必须带小写 title**：layout.html 用 {{.title}} 取值，缺这个键会让渲染在 layout 里
	// 中断（HTTP 仍是 200、正文整块为空）—— 也就是说「取数失败」会变成「白页」，
	// 运营连那句归口文案都看不到。这两条分支此前正是这样。
	accountRows, accountTotal, accountPage, err := h.mail.ListAccountsPage(ctx, "", mailPageNumber(c.Query("account_page")), mailListPageSize)
	if err != nil {
		c.HTML(http.StatusOK, "admin/mail/mail.html", adminui.Prepare(c, mailPageErrData(c, err)))
		return
	}

	data := adminui.Prepare(c, gin.H{
		// 标题取词在组装侧做（mailLabel）：只交 key 的话词条缺失 / i18n 未初始化时
		// 页面上是裸 key（admin.mail.accounts.heading），而 adminui.Prepare 的 title 兜底
		// 正是 key 本身 —— 与 mail_ui_labels.go 文件头写明的那条相反。同模块其余四页
		// 都是 mailLabel(...) 的形态，这里是唯一的例外。
		"title":    mailLabel(mailTranslate(c), mailenums.PageTitleAccounts),
		"Accounts": accountRows,
		// 写动作的结论不再回显在本页（走 shell.RenderJump 提示页，见 mail_jump.go）；
		// Err 只由上面的取数失败分支注入。
	})
	// 分页只注入事实；单页 / 空数据不注入键（与迁移前键缺失同义），
	// 模板 include 里 isset 守卫自然跳过。
	adminui.AttachPagination(data, "", nil,
		mailListPaginationFacts(accountTotal, accountPage, "account_page", "/admin/mail"))
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
		"title":    mailLabel(mailTranslate(c), mailenums.PageTitleAccounts),
		"Err":      mailErrPageText(c, err),
		"Accounts": []any{},
	}
}

// MailAccountSave 保存发信账号（id 为 0 即新建）。
func (h *mailPageHandle) MailAccountSave(c *gin.Context) {
	req := &maildto.SaveAccountReq{
		ID:          reqparams.ParseUint(c.PostForm("id")),
		Name:        c.PostForm("name"),
		Purpose:     c.PostForm("purpose"),
		FromName:    c.PostForm("from_name"),
		FromEmail:   c.PostForm("from_email"),
		ReplyTo:     c.PostForm("reply_to"),
		Provider:    c.PostForm("provider"),
		Host:        c.PostForm("host"),
		Port:        int(reqparams.ParseUint(c.PostForm("port"))),
		Username:    c.PostForm("username"),
		Password:    c.PostForm("password"),
		Encryption:  c.PostForm("encryption"),
		RatePerHour: int(reqparams.ParseUint(c.PostForm("rate_per_hour"))),
	}
	var err error
	if req.ID > 0 {
		_, err = h.mail.UpdateAccount(c.Request.Context(), req)
	} else {
		_, err = h.mail.CreateAccount(c.Request.Context(), req)
	}
	if err != nil {
		mailAccountsJump(c, false, mailErrMsg(c, err))
		return
	}
	mailAccountsJump(c, true, mailDoneMsg)
}

// MailAccountDelete 删除发信账号。
func (h *mailPageHandle) MailAccountDelete(c *gin.Context) {
	if err := h.mail.DeleteAccount(c.Request.Context(), reqparams.ParseUint(c.PostForm("id"))); err != nil {
		mailAccountsJump(c, false, mailErrMsg(c, err))
		return
	}
	mailAccountsJump(c, true, mailDoneMsg)
}

// MailAccountDefault 设为该用途的默认账号。
func (h *mailPageHandle) MailAccountDefault(c *gin.Context) {
	if err := h.mail.SetDefaultAccount(c.Request.Context(), reqparams.ParseUint(c.PostForm("id"))); err != nil {
		mailAccountsJump(c, false, mailErrMsg(c, err))
		return
	}
	mailAccountsJump(c, true, mailDoneMsg)
}

// MailAccountTest 测试发送：结果**不回显为成功页**，而是把 SMTP 的真实反馈带回来。
func (h *mailPageHandle) MailAccountTest(c *gin.Context) {
	res, err := h.mail.TestSend(c.Request.Context(), &maildto.TestSendReq{
		AccountID: reqparams.ParseUint(c.PostForm("id")),
		ToEmail:   c.PostForm("to_email"),
		Lang:      response.RequestLanguage(c),
	})
	if err != nil {
		mailAccountsJump(c, false, mailErrMsg(c, err))
		return
	}
	if !res.OK {
		// SMTP 的响应码与主机名只进日志（mailTestSendFailedMsg 里记）：它们既不是给运营看的，
		// 也不该出现在 URL / 浏览器历史里。对外只给「可重试 / 永久拒绝 / 配置问题」。
		mailAccountsJump(c, false, mailTestSendFailedMsg(c, res.ErrorKind, res.Error))
		return
	}
	mailAccountsJump(c, true, mailDoneMsg)
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
		mailTemplatesJump(c, false, mailErrMsg(c, err))
		return
	}
	mailTemplatesJump(c, true, mailDoneMsg)
}

// MailTemplateDelete 删除邮件模板。
func (h *mailPageHandle) MailTemplateDelete(c *gin.Context) {
	if err := h.mail.DeleteTemplate(c.Request.Context(), c.PostForm("template_key"), c.PostForm("locale")); err != nil {
		mailTemplatesJump(c, false, mailErrMsg(c, err))
		return
	}
	mailTemplatesJump(c, true, mailDoneMsg)
}

// MailMarketingRedirect 把旧的「邮件营销」地址引导到拆页后的联系人页。
//
// 保留 302 而不是让路径 404：它此前同时是 sys_menus 的菜单项和用户书签，
// 直接消失会让人以为功能被删了。选 302 不是 301 —— 301 会被浏览器长期缓存，
// 将来若再调整落点，老客户端永远回不来。
//
// 只透传目标页认识的三个参数（keyword / status / page）：带着不认识的参数跳到新页，
// 用户会以为自己筛过什么，而页面显示的是全量 —— 这比直接丢掉筛选更难排查。
// 活动类筛选参数（campaign_*）属于另一张表，按裁定丢弃。
func (h *mailPageHandle) MailMarketingRedirect(c *gin.Context) {
	query := url.Values{}
	for _, key := range []string{"keyword", "status", "page"} {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			query.Set(key, value)
		}
	}
	target := "/admin/mail/contacts"
	if encoded := query.Encode(); encoded != "" {
		target += "?" + encoded
	}
	c.Redirect(http.StatusFound, target)
}

// —— 批量动作（评审规则 admin-ui-logic §7：列表首列勾选 + 批量条）——
//
// 账号 / 模板 / 联系人 / 活动的批量端点形状是同一个，与产品、订单、优惠码域一致：
// **逐条走同一条单条路径**，失败只计跳过、不中断整批 —— 批量操作不能因为一条被服务端拒绝
// 就整批回滚，那会让人以为「一条都没做」然后反复重试；也不能静默部分成功，所以结论按
// 「成功 N / 跳过 M」渲染成提示页（见 mail_jump.go 的 mailBulkJump）。
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
	// 批量 id 统一入口（pkg/bulkids：去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := bulkids.Normalize(c.PostFormArray("ids"))
	if berr != nil {
		mailAccountsJump(c, false, mailBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		id := reqparams.ParseUint(raw)
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
	mailBulkJump(c, done, warn, mailAccountsPath, mailAccountsBackMsg)
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
		mailTemplatesJump(c, false, mailTemplateListFailedMsg)
		return
	}
	type templateRef struct{ key, locale string }
	byID := make(map[uint64]templateRef, len(list))
	for _, t := range list {
		if t != nil {
			byID[t.ID] = templateRef{key: t.TemplateKey, locale: t.Locale}
		}
	}
	// 批量 id 统一入口（pkg/bulkids：去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := bulkids.Normalize(c.PostFormArray("ids"))
	if berr != nil {
		mailTemplatesJump(c, false, mailBulkIDsMsg(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range ids {
		ref, ok := byID[reqparams.ParseUint(raw)]
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
	mailBulkJump(c, done, warn, mailTemplatesPath, mailTemplatesBackMsg)
}

// mailBulkOutcome 把「成功 N / 跳过 M」折成两条提示**事实**（模板常量见 mail_err.go）。
//
// 有跳过时走失败提示（部分成功必须说出来）：只看到「已删除 2 个」的人会以为选中的都删了。
// 计数与词（动作 / 名词）都是位置参数：句子是词条，语言在渲染期由模板决定 ——
// 这也是本函数不再返回拼好的中文串的原因。
func mailBulkOutcome(verb, noun string, done, skipped int) (doneMsg, warnMsg mailMsg) {
	switch {
	case done == 0 && skipped == 0:
		return mailMsg{}, mailMsg{}
	case skipped == 0:
		return mailBulkAllDoneMsg.WithArgs(verb, strconv.Itoa(done), noun), mailMsg{}
	case done == 0:
		return mailMsg{}, mailBulkAllSkippedMsg.WithArgs(noun, verb, strconv.Itoa(skipped))
	default:
		return mailMsg{}, mailBulkPartialMsg.WithArgs(verb, strconv.Itoa(done), noun, strconv.Itoa(skipped))
	}
}

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
		c.HTML(http.StatusOK, "admin/mail/mail_templates.html", adminui.Prepare(c, mailTemplatesErrData(c, err)))
		return
	}
	data := adminui.Prepare(c, gin.H{
		// 同上：标题在组装侧取词，缺词条回落中文兜底，不留裸 key。
		"title":         mailLabel(mailTranslate(c), mailenums.PageTitleTemplates),
		"Templates":     rows,
		"TemplateTotal": total,
		// 写动作的结论不再回显在本页（走 shell.RenderJump 提示页，见 mail_jump.go）；
		// Err 只由上面的取数失败分支注入。
	})
	// 分页只注入事实；单页 / 空数据不注入键，模板 include 的 isset 守卫自然跳过。
	adminui.AttachPagination(data, "", nil,
		mailListPaginationFacts(total, page, "page", "/admin/mail/templates"))
	c.HTML(http.StatusOK, "admin/mail/mail_templates.html", data)
}

// mailTemplatesErrData 取数失败时的页面数据：归口文案 + 空列表 + 小写 title。
func mailTemplatesErrData(c *gin.Context, err error) gin.H {
	return gin.H{
		"title":         mailLabel(mailTranslate(c), mailenums.PageTitleTemplates),
		"Err":           mailErrPageText(c, err),
		"Templates":     []any{},
		"TemplateTotal": int64(0),
	}
}

// 访客面文案的**中文兜底**（词条缺失 / i18n 未初始化时 pkg/i18n 落到这里）。
//
// 它们与迁移 410 的词条是同一句话的两份：词条是真相来源（后台可改），这里是兜底 ——
// 与 mail_err.go 的 mailInternalFallback（归口文案自带中文兜底）同一取舍。
// 为什么不能兜底成 key：后台页面上出现裸 key 有人会来报，而这张页面只有收件人看见。
const (
	mailTrackLinkInvalidText       = "链接无效或已过期"
	mailUnsubscribeLinkInvalidText = "退订链接无效或已过期"
	mailUnsubscribeDoneTitleText   = "已退订"
	mailUnsubscribeDoneBodyText    = "%s 不会再收到我们的营销邮件。"
	mailUnsubscribeDoneNoteText    = "事务类邮件（如密码重置、订单通知）不受影响。"
)

// trackingHandle 追踪端点处理器。
type trackingHandle struct {
	svc mailcontract.TrackingService
}

// NewTrackingHandle 构造。
func NewTrackingHandle(svc mailcontract.TrackingService) *trackingHandle {
	return &trackingHandle{svc: svc}
}

// 1×1 透明 GIF（43 字节，标准最小透明像素）。
var transparentGIF = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00, 0x00,
	0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x21, 0xf9, 0x04, 0x01, 0x00, 0x00, 0x00,
	0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x02,
	0x44, 0x01, 0x00, 0x3b,
}

// Open 打开追踪：无论 token 是否有效都返回像素。
//
// 无效 token 也返回 200 + 像素，不返回错误页 —— 收件人打开邮件时不该看到一个报错图，
// 而且返回差异会向外部泄露「这个 token 是否有效」。
func (h *trackingHandle) Open(c *gin.Context) {
	token := strings.TrimSuffix(c.Param("token"), ".gif")
	if p, err := h.svc.ParseTrackToken(token); err == nil {
		h.svc.RecordTrackEvent(c.Request.Context(), p, mailmodel.EventTypeOpen, c.ClientIP(), c.Request.UserAgent())
	}
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, private")
	c.Data(http.StatusOK, "image/gif", transparentGIF)
}

// Click 点击追踪：验签 → 记录 → 302 跳转。
func (h *trackingHandle) Click(c *gin.Context) {
	p, err := h.svc.ParseTrackToken(c.Param("token"))
	if err != nil || strings.TrimSpace(p.URL) == "" {
		// 无效 token 不跳转（跳去任意地方等于开放重定向），回一个中性提示。
		//
		// 形态不变（400 + 一句短句）：这是**访客**端点，没有后台壳也没有可回归的列表页，
		// 303 + ?err= 或 JSON 都无处可去。收口的是文案来源 —— 按请求语言取词条，
		// 英文收件人不再拿到一句中文。
		mailVisitorLog(c, err, "邮件点击链路处理失败")
		c.String(http.StatusBadRequest,
			mailVisitorText(c, mailenums.ErrTrackLinkInvalid, mailTrackLinkInvalidText))
		return
	}
	h.svc.RecordTrackEvent(c.Request.Context(), p, mailmodel.EventTypeClick, c.ClientIP(), c.Request.UserAgent())
	c.Redirect(http.StatusFound, p.URL)
}

// Unsubscribe 一键退订（无需登录）。
//
// 反垃圾邮件法要求退订足够简单，所以这里不校验登录态与 csrf；
// 安全性由「签名 token 只能由我们签发」保证，且退订是幂等的。
func (h *trackingHandle) Unsubscribe(c *gin.Context) {
	email, err := h.svc.UnsubscribeByToken(c.Request.Context(), c.Param("token"), c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		// 失败原因（token 无效 / 联系人查不到 / 抑制名单写失败）一律不外发：
		// 对收件人说「联系人不存在」既没有用处，也把系统内部结构讲给了外部 ——
		// service 那边的原文正是中文业务句与驱动原文两种都有。
		// 但原文必须进日志（这一条按 Error 记）：退订写库失败是真需要有人看的故障。
		mailVisitorLogError(c, err, "邮件退订链路处理失败")
		c.String(http.StatusBadRequest,
			mailVisitorText(c, mailenums.ErrUnsubscribeLinkInvalid, mailUnsubscribeLinkInvalidText))
		return
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, unsubscribeDonePage(c, email))
}

// unsubscribeDonePage 退订成功页：一张自带样式的整页 HTML（不依赖后台壳、不查库、不带脚本）。
//
// 三句文案与 lang 属性都按当前请求语言取 —— 这是收件人这次点击唯一的反馈，
// 固定写 zh-CN 会让浏览器按中文断行 / 选字体 / 朗读（英文收件人看到的是全中文页面）。
//
// 为什么用 strings.ReplaceAll 而不是 fmt.Sprintf 填邮箱：这里只是「把邮箱放进一句话」，
// 不需要 Go 的格式协议 —— 词条若被写进 %d 之类协议外占位符，Sprintf 会把
// "%!d(MISSING)" 摆到收件人面前（与 adminBulkTextOf 的 HasStringPlaceholdersOnly 同一顾虑，
// 这里的取法更省：连协议都不需要，直接换字面）。
//
// 三处文案都经 html.EscapeString：**词条是后台可编辑的数据**（sys_i18n），
// 直接拼进 HTML 就是一条存储型注入面；邮箱来自数据库，同理。
func unsubscribeDonePage(c *gin.Context, email string) string {
	lang := response.RequestLanguage(c)
	title := html.EscapeString(mailVisitorText(c, mailenums.MsgUnsubscribeDoneTitle, mailUnsubscribeDoneTitleText))
	body := strings.ReplaceAll(
		html.EscapeString(mailVisitorText(c, mailenums.MsgUnsubscribeDoneBody, mailUnsubscribeDoneBodyText)),
		"%s", html.EscapeString(email))
	note := html.EscapeString(mailVisitorText(c, mailenums.MsgUnsubscribeDoneNote, mailUnsubscribeDoneNoteText))

	return "<!doctype html><html lang=\"" + html.EscapeString(lang) + "\"><head><meta charset=\"utf-8\">" +
		"<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">" +
		"<title>" + title + "</title></head><body style=\"font-family:system-ui,sans-serif;max-width:520px;margin:80px auto;padding:0 20px;line-height:1.7\">" +
		"<h2>" + title + "</h2><p>" + body + "</p>" +
		"<p>" + note + "</p>" +
		"</body></html>"
}

// mailRunTexts 实例列表里的运行文案（原地改写）。
func mailRunTexts(tr mailTr, items []maildto.AutomationRunItem) {
	for i := range items {
		items[i].ErrorMessage = mailenums.FormatRunText(tr, items[i].ErrorMessage)
	}
}

// mailRunDetailTexts 排障详情：Explain / error_message / 时间线 detail（原地改写）。
func mailRunDetailTexts(tr mailTr, d *maildto.AutomationRunDetailResp) {
	if d == nil {
		return
	}
	d.Explain = mailenums.FormatRunText(tr, d.Explain)
	d.Run.ErrorMessage = mailenums.FormatRunText(tr, d.Run.ErrorMessage)
	for i := range d.Timeline {
		d.Timeline[i].Detail = mailenums.FormatRunText(tr, d.Timeline[i].Detail)
	}
}
