package mailhttp

// mail_jump.go — mail 后台页写动作的**出口**：整页提示（shell.RenderJump，
// 对应 ThinkPHP 的 success() / error()）。
//
// 取代原先的 302 + `?err=` / `?ok=` / `?done=` 回列表页：那条通道要求读侧再判一次
// 「这条提示是不是本仓给的」（mailPageErr / mailPageOk / mailPageDone / mailNoticeTexts
// 就是那套），而查询参数不是可信边界。文案改走响应体之后，读侧判定整批删除
//（见 mail_err.go 的说明）。
//
// 出口收的是**事实**（adminui.Msg 的三段：i18n key / 中文兜底 / 位置参数），不是拼好的整句 ——
// 取词发生在**模板渲染期**（adminui/prepare.go 注入的 tr 与 tf：jump.html 的正文走
// tf(MsgKey, Msg, MsgArgs)、回跳链接走 tr(BackTextKey, BackText)）。语言是**请求级**的：
// 在 Go 侧拼句子时漏一处、或漏带当前请求，症状是「中文界面全对、英文界面整句中文」，
// 不报错也不记日志 —— 所以本模块的出口只交事实，不取词。
//
// 三条边界（同 shell.RenderJump 的注释）：
//   · 文案必须是**已归口的事实或受控成品**（mailErrMsg / mailFormErrText /
//     mailBulkIDsMsg 的产物）—— 原文只进日志，换个页面呈现不等于可以把
//     err.Error() 铺上去；
//   · 回跳地址由 backref.BackPath 从**表单 action 的 query** 按白名单读回（服务端自己拼，
//     不读隐藏域里的整串 URL）；
//   · 结论不进 URL —— 成功 / 失败只体现在提示页的响应体里。

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/backref"
	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/internal/shell"
	"go_wp/pkg/backpath"
	"go_wp/pkg/reqparams"
)

// 后台页面路径（回跳目标）。mailContactsPath 定义在 mail_page.go（页面地址与拼接只用一处）。
const (
	mailAccountsPath       = "/admin/mail"
	mailTemplatesPath      = "/admin/mail/templates"
	mailCampaignsPath      = "/admin/mail/campaigns"
	mailAutomationPath     = "/admin/mail/automation"
	mailAutomationEditPath = "/admin/mail/automation/edit"
	mailAutomationRunsPath = "/admin/mail/automation/runs"
)

// 各页回跳筛选键。
//
// 同一份键表服务两条路径：**渲染时**拼进表单 action 的 query、**POST 回来时**由
// backref.BackPath 读回。两处分叉的表现是「写完跳回去筛选静默丢了」—— 页面不报错、
// 日志也干净，所以键表必须是同一份（不要在两处各写一遍字面量）。
//
// 联系人页的键与筛选表单同名（keyword / status / tags / page）；批量改状态表单里的
// `status` 是**目标状态**，与列表筛选同名不冲突 —— 前者走 body（c.PostForm），
// 后者走 query（c.Request.URL.Query）。
var (
	mailContactsBackKeys       = []string{"keyword", "status", "tags", "page"}
	mailCampaignsBackKeys      = []string{"page"}
	mailAutomationBackKeys     = []string{"page"}
	mailAutomationRunsBackKeys = []string{"automationId", "runStatus", "page"}
)

// mailListQuery 把页面筛选拼成**带前导 `?` 的**查询串，供模板直接拼进写动作表单的 action。
//
// 空值时返回空串（不产出裸 `?`，避免 `action="/x?"` 这种形态）。与 membershipListQuery
// 同形：空值丢弃、编码走 url.Values（键有序，产物稳定）。
// 服务端自己拼：页面不把上下文塞进隐藏域，回跳时由 backref.BackPath 按同一份键表读回来。
func mailListQuery(params map[string]string) string {
	q := url.Values{}
	for k, v := range params {
		if strings.TrimSpace(v) != "" {
			q.Set(k, v)
		}
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// mailPageQuery 列表页只带回页码的筛选串：第 1 页是默认值，不写进 URL（避免 `?page=1`）。
func mailPageQuery(page int) string {
	if page <= 1 {
		return ""
	}
	return mailListQuery(map[string]string{"page": strconv.Itoa(page)})
}

// mailAutomationRunsListQuery 运行记录页补投表单 action 的查询串（流程 / 状态 / 页码）。
//
// 与回跳键表 mailAutomationRunsBackKeys 同一份键名；第 1 页不写进 URL。
func mailAutomationRunsListQuery(automationID, runStatus string, page int) string {
	params := map[string]string{"automationId": automationID, "runStatus": runStatus}
	if page > 1 {
		params["page"] = strconv.Itoa(page)
	}
	return mailListQuery(params)
}

// —— 回跳地址（每页一个，键表与上面同一份）——

func mailTemplatesBack(c *gin.Context) string {
	return mailTemplatesPath
}

func mailContactsBack(c *gin.Context) string {
	return backref.BackPath(c, mailContactsPath, mailContactsBackKeys...)
}

func mailCampaignsBack(c *gin.Context) string {
	return backref.BackPath(c, mailCampaignsPath, mailCampaignsBackKeys...)
}

func mailAutomationBack(c *gin.Context) string {
	return backref.BackPath(c, mailAutomationPath, mailAutomationBackKeys...)
}

func mailAutomationRunsBack(c *gin.Context) string {
	return backref.BackPath(c, mailAutomationRunsPath, mailAutomationRunsBackKeys...)
}

// mailAutomationEditBack 流程编辑页的回跳地址：id 来自本次保存的流程（不在表单 action 的
// query 里），所以用 backpath.WithParams 拼 —— 与 BackPath 同一份编码口径。
func mailAutomationEditBack(id uint64) string {
	if id == 0 {
		return mailAutomationEditPath
	}
	return backpath.WithParams(mailAutomationEditPath, map[string]string{"id": strconv.FormatUint(id, 10)})
}

// —— 提示页文案的形状与静态事实 ——

// mailMsg 提示页的一句结论 = 共享形状 adminui.Msg（i18n key + 中文兜底 + 位置参数）。
//
// 用别名而不是自造 struct：键名与参数编码必须与其它模块逐字段一致（模板侧的占位符是
// 百分号 s，传 int 会渲染成 %!s(int=3)），各造一份迟早分叉。
type mailMsg = adminui.Msg

// mailLabelMsg 展示名标签 → 提示页事实（与 mailLabel 同一份 LabelPair，只是不取词）。
func mailLabelMsg(pair mailenums.LabelPair) mailMsg {
	return mailMsg{Key: pair.Key, Fallback: pair.Fallback}
}

// 回跳链接文字：复用各页标题词条，不新增全站词条。
//
// 它们是**静态事实**（语言在渲染期由模板的 tr 决定），所以是包级变量、不接 *gin.Context ——
// 顺手把「每个出口都得记得取词」这条隐式约定消掉。
var (
	mailAccountsBackMsg       = mailLabelMsg(mailenums.PageTitleAccounts)
	mailTemplatesBackMsg      = mailLabelMsg(mailenums.PageTitleTemplates)
	mailContactsBackMsg       = mailLabelMsg(mailenums.PageTitleContacts)
	mailCampaignsBackMsg      = mailLabelMsg(mailenums.PageTitleCampaigns)
	mailAutomationBackMsg     = mailLabelMsg(mailenums.PageTitleAutomation)
	mailAutomationEditBackMsg = mailLabelMsg(mailenums.PageTitleAutomationEdit)
	mailAutomationRunsBackMsg = mailLabelMsg(mailenums.PageTitleAutomationRuns)
)

// —— 出口 ——

// mailJump 渲染整页提示：成功 1 秒后自动回跳，失败不自动跳（运营要看清楚原因）。
func mailJump(c *gin.Context, ok bool, msg mailMsg, back string, backText mailMsg) {
	seconds := 0
	if ok {
		seconds = 1
	}
	shell.RenderJump(c, shell.Jump{
		OK:          ok,
		MsgKey:      msg.Key,
		Msg:         msg.Fallback,
		MsgArgs:     msg.Args,
		Back:        back,
		BackTextKey: backText.Key,
		BackText:    backText.Fallback,
		Seconds:     seconds,
	})
}

// 各页的提示页出口（back 与 backText 由上面两组助手给）。
func mailAccountsJump(c *gin.Context, ok bool, msg mailMsg) {
	mailJump(c, ok, msg, mailAccountsPath, mailAccountsBackMsg)
}

func mailTemplatesJump(c *gin.Context, ok bool, msg mailMsg) {
	mailJump(c, ok, msg, mailTemplatesBack(c), mailTemplatesBackMsg)
}

func mailContactsJump(c *gin.Context, ok bool, msg mailMsg) {
	mailJump(c, ok, msg, mailContactsBack(c), mailContactsBackMsg)
}

func mailCampaignsJump(c *gin.Context, ok bool, msg mailMsg) {
	mailJump(c, ok, msg, mailCampaignsBack(c), mailCampaignsBackMsg)
}

func mailAutomationJump(c *gin.Context, ok bool, msg mailMsg) {
	mailJump(c, ok, msg, mailAutomationBack(c), mailAutomationBackMsg)
}

func mailAutomationEditJump(c *gin.Context, ok bool, msg mailMsg, id uint64) {
	mailJump(c, ok, msg, mailAutomationEditBack(id), mailAutomationEditBackMsg)
}

func mailAutomationRunsJump(c *gin.Context, ok bool, msg mailMsg) {
	mailJump(c, ok, msg, mailAutomationRunsBack(c), mailAutomationRunsBackMsg)
}

// mailRedirect 批量动作「一个 id 都没选」时的出口：直接回列表，不渲染提示页。
//
// 与 inventory 的 redirectWhere 同一取舍：那种请求不是用户路径（批量条在无勾选时不会提交），
// 而提示页不能显示空串（RenderJump 会把空串落成归口文案）—— 静默回列表比一句假成功诚实。
func mailRedirect(c *gin.Context, target string) {
	if reqparams.IsHXRequest(c) {
		c.Header("HX-Redirect", target)
		c.Status(http.StatusOK)
		return
	}
	c.Redirect(http.StatusFound, target)
}

// mailBulkJump 批量动作的统一出口：有跳过走失败提示（部分成功必须说出来），
// 全成功走成功提示，一个 id 都没选则静默回列表。
//
// 语义与改造前的「?done= / ?err=」一致（mailBulkOutcome 不变），只换传输通道。
func mailBulkJump(c *gin.Context, done, warn mailMsg, back string, backText mailMsg) {
	switch {
	case !warn.Empty():
		mailJump(c, false, warn, back, backText)
	case !done.Empty():
		mailJump(c, true, done, back, backText)
	default:
		mailRedirect(c, back)
	}
}

// mailDoneMsg 单条写动作的通用成功回执（词条缺失回落中文兜底）。
//
// 与读侧时代的 ?ok=1 同一句话（mailPageOk 把 token 收敛成 MsgSaveSuccess）：
// 改造只换传输通道，不换「成功说的是哪句话」。
var mailDoneMsg = mailMsg{Key: mailenums.MsgSaveSuccess, Fallback: "保存成功"}
