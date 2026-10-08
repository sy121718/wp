package mailhttp

// mail_jump.go — mail 后台页写动作的**出口**：整页提示（shell.RenderJump，
// 对应 ThinkPHP 的 success() / error()）。
//
// 取代原先的 302 + `?err=` / `?ok=` / `?done=` 回列表页：那条通道要求读侧再判一次
// 「这条提示是不是本仓给的」（mailPageErr / mailPageOk / mailPageDone / mailNoticeTexts
// 就是那套），而查询参数不是可信边界。文案改走响应体之后，读侧判定整批删除
//（见 mail_err.go 的说明）。
//
// 三条边界（同 shell.RenderJump 的注释）：
//   · 文案必须**已过本模块白名单 / 已归口**（mailErrPageText / mailFormErrText /
//     shell.BulkIDsFacingText 的产物）—— 原文只进日志，换个页面呈现不等于可以把
//     err.Error() 铺上去；
//   · 回跳地址由 shell.BackPath 从**表单 action 的 query** 按白名单读回（服务端自己拼，
//     不读隐藏域里的整串 URL）；
//   · 结论不进 URL —— 成功 / 失败只体现在提示页的响应体里。

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/internal/shell"
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
// shell.BackPath 读回。两处分叉的表现是「写完跳回去筛选静默丢了」—— 页面不报错、
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
// 服务端自己拼：页面不把上下文塞进隐藏域，回跳时由 shell.BackPath 按同一份键表读回来。
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
	return shell.BackPath(c, mailContactsPath, mailContactsBackKeys...)
}

func mailCampaignsBack(c *gin.Context) string {
	return shell.BackPath(c, mailCampaignsPath, mailCampaignsBackKeys...)
}

func mailAutomationBack(c *gin.Context) string {
	return shell.BackPath(c, mailAutomationPath, mailAutomationBackKeys...)
}

func mailAutomationRunsBack(c *gin.Context) string {
	return shell.BackPath(c, mailAutomationRunsPath, mailAutomationRunsBackKeys...)
}

// mailAutomationEditBack 流程编辑页的回跳地址：id 来自本次保存的流程（不在表单 action 的
// query 里），所以用 shell.WithParams 拼 —— 与 BackPath 同一份编码口径。
func mailAutomationEditBack(id uint64) string {
	if id == 0 {
		return mailAutomationEditPath
	}
	return shell.WithParams(mailAutomationEditPath, map[string]string{"id": strconv.FormatUint(id, 10)})
}

// —— 回跳链接文字（复用各页标题词条，不新增全站词条）——

func mailAccountsBackText(c *gin.Context) string {
	return mailLabel(shell.TranslateFor(c), mailenums.PageTitleAccounts)
}

func mailTemplatesBackText(c *gin.Context) string {
	return mailLabel(shell.TranslateFor(c), mailenums.PageTitleTemplates)
}

func mailContactsBackText(c *gin.Context) string {
	return mailLabel(shell.TranslateFor(c), mailenums.PageTitleContacts)
}

func mailCampaignsBackText(c *gin.Context) string {
	return mailLabel(shell.TranslateFor(c), mailenums.PageTitleCampaigns)
}

func mailAutomationBackText(c *gin.Context) string {
	return mailLabel(shell.TranslateFor(c), mailenums.PageTitleAutomation)
}

func mailAutomationEditBackText(c *gin.Context) string {
	return mailLabel(shell.TranslateFor(c), mailenums.PageTitleAutomationEdit)
}

func mailAutomationRunsBackText(c *gin.Context) string {
	return mailLabel(shell.TranslateFor(c), mailenums.PageTitleAutomationRuns)
}

// —— 出口 ——

// mailJump 渲染整页提示：成功 1 秒后自动回跳，失败不自动跳（运营要看清楚原因）。
func mailJump(c *gin.Context, ok bool, msg, back, backText string) {
	if ok {
		shell.RenderJump(c, shell.Jump{OK: true, Msg: msg, Back: back, BackText: backText, Seconds: 1})
		return
	}
	shell.RenderJump(c, shell.Jump{Msg: msg, Back: back, BackText: backText})
}

// 各页的提示页出口（back 与 backText 由上面两组助手给）。
func mailAccountsJump(c *gin.Context, ok bool, msg string) {
	mailJump(c, ok, msg, mailAccountsPath, mailAccountsBackText(c))
}

func mailTemplatesJump(c *gin.Context, ok bool, msg string) {
	mailJump(c, ok, msg, mailTemplatesBack(c), mailTemplatesBackText(c))
}

func mailContactsJump(c *gin.Context, ok bool, msg string) {
	mailJump(c, ok, msg, mailContactsBack(c), mailContactsBackText(c))
}

func mailCampaignsJump(c *gin.Context, ok bool, msg string) {
	mailJump(c, ok, msg, mailCampaignsBack(c), mailCampaignsBackText(c))
}

func mailAutomationJump(c *gin.Context, ok bool, msg string) {
	mailJump(c, ok, msg, mailAutomationBack(c), mailAutomationBackText(c))
}

func mailAutomationEditJump(c *gin.Context, ok bool, msg string, id uint64) {
	mailJump(c, ok, msg, mailAutomationEditBack(id), mailAutomationEditBackText(c))
}

func mailAutomationRunsJump(c *gin.Context, ok bool, msg string) {
	mailJump(c, ok, msg, mailAutomationRunsBack(c), mailAutomationRunsBackText(c))
}

// mailRedirect 批量动作「一个 id 都没选」时的出口：直接回列表，不渲染提示页。
//
// 与 inventory 的 redirectWhere 同一取舍：那种请求不是用户路径（批量条在无勾选时不会提交），
// 而提示页不能显示空串（RenderJump 会把空串落成归口文案）—— 静默回列表比一句假成功诚实。
func mailRedirect(c *gin.Context, target string) {
	if shell.IsHXRequest(c) {
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
func mailBulkJump(c *gin.Context, done, warn, back, backText string) {
	switch {
	case warn != "":
		mailJump(c, false, warn, back, backText)
	case done != "":
		mailJump(c, true, done, back, backText)
	default:
		mailRedirect(c, back)
	}
}

// mailDoneText 单条写动作的通用成功回执（取当前语言，词条缺失回落中文）。
//
// 与读侧时代的 ?ok=1 同一句话（mailPageOk 把 token 收敛成 MsgSaveSuccess）：
// 改造只换传输通道，不换「成功说的是哪句话」。
func mailDoneText(c *gin.Context) string {
	return shell.TranslateFor(c)(mailenums.MsgSaveSuccess, "保存成功")
}
