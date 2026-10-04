// mail_automation_runs_page_handle.go — 自动化运行记录页（从流程页拆出的独立职能）。
//
// 为什么单独成页：流程是「配置」（一年改几次），实例是「排障」（出事时盯着看）。
// 挤在一页时，配流程的人要往下滚过几十条与本次操作无关的运行记录；
// 而排障的人要越过整张流程表才能看到实例。拆开后本页只回答「哪些实例需要处理」。
package mailhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/internal/web/shell"
)

// mailAutomationRunsPageSize 运行实例每页条数（与流程页共用同一条每页约定）。
const mailAutomationRunsPageSize = 50

// MailAutomationRunsPage 运行记录页：实例计数 + 筛选（流程 / 状态）+ 实例表 + 分页。
//
// 计数（运行中 / 等待 / 已完成 / 失败 / 已停止）来自 service 的 Counts，
// 是排障时的第一判断：有多少在等、有多少已经失败。
func (h *mailPageHandle) MailAutomationRunsPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := shell.TranslateFor(c)
	page := mailPageNumber(c.Query("page"))
	automationID, runStatus := c.Query("automationId"), c.Query("runStatus")

	runs, err := h.mail.ListAutomationRuns(ctx, &maildto.AutomationRunListReq{
		AutomationID: shell.ParseUint(automationID),
		Status:       runStatus,
		Page:         page,
		PageSize:     mailAutomationRunsPageSize,
	})
	data := gin.H{
		"title":     mailLabel(tr, mailenums.PageTitleAutomationRuns),
		"Page":      page,
		"FilterID":  automationID,
		"FilterRun": runStatus,
		// 读侧回执一律经 mail_err.go 的白名单出口：查询参数不是可信边界。
		"Err": mailPageErr(c),
		"Ok":  mailPageOk(c),
		// Done：批量动作的结论。模板用 isset 认这个可选键。
		"Done": mailPageDone(c),
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
		c.HTML(http.StatusOK, "admin/mail/mail_automation_runs.html", shell.Prepare(c, data))
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
	base := shell.FilterBaseURL("/admin/mail/automation/runs", map[string]string{
		"automationId": automationID,
		"runStatus":    runStatus,
	})
	for k, v := range shell.BuildPagination(runs.Total, page, mailAutomationRunsPageSize, base, tr).TemplateKeys() {
		data[k] = v
	}
	c.HTML(http.StatusOK, "admin/mail/mail_automation_runs.html", shell.Prepare(c, data))
}
