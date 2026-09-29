package pagehttp

// page_schedule_handle.go — 定时上下线（PIPE-7）的接口与后台面板。
//
// 两条通道，职责分开：
//
//   - **JSON 接口**（/api/page/schedule/{set,cancel,list}）：给工作台 / 脚本 / 后续的
//     批量排定用。错误经 pageErrorStatus / pageErrorMessage 分类，状态码与文案与其它
//     page 接口同源。
//   - **后台面板**（/admin/page-schedules/{panel,set,cancel}）：HTMX 片段，
//     表单是 form-urlencoded（原生表单 + 隐藏 csrf_token 域），成功后重新渲染面板片段。
//     为什么不是「HTMX 直接打 JSON 接口」：HTMX 的表单 POST 是 form-encoded，
//     而 JSON 接口只收 JSON —— 要么引入 json-enc 扩展（新增前端依赖），
//     要么在接口里做双形态绑定（两套解析路径，容易只测到一条）。挂页面组与
//     修订历史面板（workbenchPages.POST("/workbench/history")）同形，是本仓的既有做法。
//
// 面板与列表页的失败原因都**经词条取词**显示（pageenums.ScheduleFailureFallbacks）：
// page_schedules.last_error 存的是业务 key 而不是原文，原文只进日志 ——
// 直接把那一列渲染出来，英文界面上会显示 ErrRebuildRequired 这样的裸 key。

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	"go_wp/internal/web/shell"
	"go_wp/pkg/response"
	"go_wp/pkg/sitetz"
)

// SetSchedule 排定一次到点动作（JSON）。
func (h *Handle) SetSchedule(c *gin.Context) {
	var req pagedto.ScheduleSetReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	// 发起人由服务端填，不从请求体读（审计字段不能由请求方自报）。
	req.CreateBy = int64(shell.CurrentUserID(c))
	res, err := h.svc.SetPageSchedule(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	fillScheduleFailureText(c, []*pagedto.ScheduleItem{res})
	response.SuccessWithMessage(c, pageenums.MsgScheduleSet, res)
}

// CancelSchedule 取消一条尚未执行的排定（JSON）。
func (h *Handle) CancelSchedule(c *gin.Context) {
	var req pagedto.ScheduleCancelReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	if err := h.svc.CancelPageSchedule(c.Request.Context(), &req); err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgScheduleCanceled, nil)
}

// ListSchedules 列出某页面的排定（JSON）。
func (h *Handle) ListSchedules(c *gin.Context) {
	var req pagedto.ScheduleListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ListPageSchedules(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	if res != nil {
		items := make([]*pagedto.ScheduleItem, 0, len(res.Items))
		for i := range res.Items {
			items = append(items, &res.Items[i])
		}
		fillScheduleFailureText(c, items)
	}
	response.Success(c, res)
}

// fillScheduleFailureText 把 last_error（业务 key）翻成当前语言文本。
//
// 未登记的 key 落到归口文案而不是裸 key：那一列的值由 service 写、读侧才知道词条，
// 两边对不上时用户看到的该是「排定执行失败」而不是一串常量名。
func fillScheduleFailureText(c *gin.Context, items []*pagedto.ScheduleItem) {
	tr := shell.TranslateFor(c)
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.LastError) == "" {
			continue
		}
		item.LastErrorText = scheduleFailureText(tr, item.LastError)
	}
}

// scheduleFailureText 单条失败原因的取词（写侧只写 key，读侧统一走这里）。
func scheduleFailureText(tr func(key, fallback string) string, key string) string {
	fallback, ok := pageenums.ScheduleFailureFallbacks[key]
	if !ok {
		return tr(pageenums.ErrScheduleApplyFailed, pageenums.ScheduleFailureFallbacks[pageenums.ErrScheduleApplyFailed])
	}
	return tr(key, fallback)
}

// —— 后台面板 ——
//
// 面板挂在 /admin 页面组（Session + CSRF 已具备），写操作显式复用 API 的权限点路径
// （builtin.CasbinMiddlewareForPath），与页面列表页的行内写操作同法 ——
// 页面路径与权限点路径不一致，直接按页面路径 enforce 会因权限点表无此路径而全员 403。

// scheduleRowView 面板里的一行排定（模板字段，值都已按当前语言取词）。
type scheduleRowView struct {
	ID       int64
	Action   string
	Lang     string
	Time     string
	Status   string
	StatusID string
	Attempts int
	Note     string
	// CanCancel 只有 pending 能取消（running 已在执行、终态没有可取消的东西）。
	CanCancel bool
}

// schedulePanelData 面板片段的模板数据（gin.H，键与模板逐一对应）。
//
// 键一律**总是注入**（空串 / 空切片而不是缺失）：后台片段模板用点号访问 map 键，
// 缺 key 会在运行期报错并让整段片段消失（inert 的失败，页面上只是少了东西）。
func (h *pagesAdminHandle) schedulePanelData(c *gin.Context, pageID, errText, doneText string) gin.H {
	tr := shell.TranslateFor(c)
	// token 拿不到不阻断渲染：提交会被 CSRF 中间件拒（与其它后台页一致）。
	token, terr := builtin.GetCSRFToken(c)
	if terr != nil {
		token = ""
	}
	// 面板标题里的页面路径：取**数据库**的草稿路径（detail），而不是从 query 参数回显 ——
	// 查询参数不是可信边界（page_err.go 的读侧收口给出的正是这条判据），
	// 而这里的数据本来就有一个真源。
	path := ""
	if pageID != "" && h.pages != nil {
		if pid, perr := h.pages.ProjectOfPage(c.Request.Context(), pageID); perr == nil && pid != "" {
			if detail, derr := h.pages.Detail(c.Request.Context(), &pagedto.DetailReq{ProjectID: pid, ID: pageID}); derr == nil && detail != nil {
				path = detail.DraftPath
			}
		}
	}
	rows := []scheduleRowView{}
	if pageID != "" && h.pages != nil {
		res, lerr := h.pages.ListPageSchedules(c.Request.Context(), &pagedto.ScheduleListReq{PageID: pageID})
		if lerr != nil {
			errText = pageErrPageText(c, lerr)
		} else if res != nil {
			for i := range res.Items {
				item := res.Items[i]
				rows = append(rows, scheduleRowView{
					ID: item.ID, Action: scheduleActionText(tr, item.Action), Lang: item.Lang,
					Time:   item.ScheduledAt.Time().Local().Format("2006-01-02 15:04"),
					Status: scheduleStatusText(tr, item.Status), StatusID: item.Status,
					Attempts: item.Attempts,
					Note:     scheduleFailureText(tr, item.LastError),
					// 只取消 pending 与 running 中的前者：running 的行已经在执行了。
					CanCancel: item.Status == pagemodel.ScheduleStatusPending,
				})
			}
		}
	}
	return gin.H{
		"t":         tr,
		"CSRFToken": token,
		"PageID":    pageID,
		"Path":      path,
		"Items":     rows,
		"Error":     errText,
		"Done":      doneText,
		// 默认到点时刻 = 站点时区下的「一小时后」，省掉每次手填（运营多数排的是近期动作）。
		"DefaultAt": sitetz.FormatDateTime(time.Now().Add(time.Hour)),
	}
}

// scheduleRowTime 列表页徽标里的到点时刻（**站点时区**、到分钟）。
//
// 时区口径与面板一致（sitetz）：库里的 scheduled_at 是绝对时刻，
// 直接按 Local 格式化会在「服务器时区 ≠ 站点时区」的部署上显示成另一个时刻。
func scheduleRowTime(item *pagedto.ScheduleItem) string {
	if item == nil {
		return ""
	}
	at := item.ScheduledAt.Time()
	if at.IsZero() {
		return ""
	}
	return at.In(sitetz.Location()).Format("2006-01-02 15:04")
}

// scheduleActionText 动作文案。
func scheduleActionText(tr func(key, fallback string) string, action string) string {
	if action == pagemodel.ScheduleActionOffline {
		return tr("admin.page.schedule.action.offline", "下线")
	}
	return tr("admin.page.schedule.action.publish", "上线")
}

// scheduleStatusText 排定状态文案。
func scheduleStatusText(tr func(key, fallback string) string, status string) string {
	switch status {
	case pagemodel.ScheduleStatusRunning:
		return tr("admin.page.schedule.status.running", "执行中")
	case pagemodel.ScheduleStatusDone:
		return tr("admin.page.schedule.status.done", "已完成")
	case pagemodel.ScheduleStatusFailed:
		return tr("admin.page.schedule.status.failed", "已失败")
	case pagemodel.ScheduleStatusCanceled:
		return tr("admin.page.schedule.status.canceled", "已取消")
	default:
		return tr("admin.page.schedule.status.pending", "待执行")
	}
}

// SchedulePanel 渲染某个页面的排定面板片段（HTMX：列表页行内「定时」按钮的落点）。
func (h *pagesAdminHandle) SchedulePanel(c *gin.Context) {
	pageID := strings.TrimSpace(c.Query("pageId"))
	c.HTML(http.StatusOK, "fragments/page_schedule_panel",
		h.schedulePanelData(c, pageID, "", ""))
}

// ScheduleSet 后台表单排定（form-urlencoded），成功后重新渲染面板片段。
func (h *pagesAdminHandle) ScheduleSet(c *gin.Context) {
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	req := &pagedto.ScheduleSetReq{
		PageID:       pageID,
		Lang:         strings.TrimSpace(c.PostForm("lang")),
		Action:       strings.TrimSpace(c.PostForm("action")),
		ScheduledAt:  strings.TrimSpace(c.PostForm("scheduledAt")),
		RedirectPath: strings.TrimSpace(c.PostForm("redirectPath")),
		CreateBy:     int64(shell.CurrentUserID(c)),
	}
	doneText, errText := "", ""
	if h.pages == nil {
		errText = shell.PageInternalText(c)
	} else if _, err := h.pages.SetPageSchedule(c.Request.Context(), req); err != nil {
		errText = pageErrPageText(c, err)
	} else {
		doneText = shell.TranslateFor(c)(pageenums.MsgScheduleSet, pageenums.MsgScheduleSet)
	}
	h.renderSchedulePanel(c, pageID, errText, doneText)
}

// ScheduleCancel 后台表单取消排定。
func (h *pagesAdminHandle) ScheduleCancel(c *gin.Context) {
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	id, perr := strconv.ParseInt(strings.TrimSpace(c.PostForm("id")), 10, 64)
	doneText, errText := "", ""
	switch {
	case h.pages == nil:
		errText = shell.PageInternalText(c)
	case perr != nil || id <= 0:
		errText = pageErrPageText(c, pageservice.ErrInvalidParam)
	default:
		if err := h.pages.CancelPageSchedule(c.Request.Context(), &pagedto.ScheduleCancelReq{
			PageID: pageID, ID: id,
		}); err != nil {
			errText = pageErrPageText(c, err)
		} else {
			doneText = shell.TranslateFor(c)(pageenums.MsgScheduleCanceled, pageenums.MsgScheduleCanceled)
		}
	}
	h.renderSchedulePanel(c, pageID, errText, doneText)
}

// renderSchedulePanel 渲染面板片段；非 HTMX 请求改走 PRG（303 回页面列表）。
//
// 为什么非 HTMX 不渲染片段：浏览器直接 POST（禁用了 JS、或从表单源码提交）时，
// 把片段当成整页返回会得到一个没有外壳的裸片段。列表页不需要回带结论，
// 因此这里退回列表页即可 —— 不引入新的 ?done= 文案，也就不必扩张读侧候选白名单。
func (h *pagesAdminHandle) renderSchedulePanel(c *gin.Context, pageID, errText, doneText string) {
	if strings.TrimSpace(c.GetHeader("HX-Request")) == "" {
		c.Redirect(http.StatusSeeOther, "/admin/pages")
		return
	}
	c.HTML(http.StatusOK, "fragments/page_schedule_panel",
		h.schedulePanelData(c, pageID, errText, doneText))
}
