package pagehttp

// 形状：**HTMX 片段 + 原生表单**（form-urlencoded + 隐藏
// csrf_token 域），写操作成功后重渲染同一片段。为什么不让 HTMX 直接打 JSON 接口：
// HTMX 的表单 POST 是 form-encoded，而 JSON 接口只收 JSON —— 要么引入 json-enc 扩展
// （新增前端依赖），要么在接口里做双形态绑定（两套解析路径，容易只测到一条）。

// 鉴权：写操作挂**后台页面组**并显式复用 API 的权限点路径（builtin.CasbinMiddlewareForPath）。
// 页面路径与权限点路径不一致，直接按页面路径 enforce 会因权限点表无此路径而拒绝所有用户
// （含超管）—— 与 /admin/page-schedules/* 同一手法。

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

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	"go_wp/internal/shell"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	"go_wp/pkg/sitetz"
	"go_wp/pkg/utils"
)

// SetSchedule 排定一次到点动作（JSON）。
func (h *Handle) SetSchedule(c *gin.Context) {
	var req pagedto.ScheduleSetReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	// 发起人由服务端填，不从请求体读（审计字段不能由请求方自报）。
	req.CreateBy = int64(builtin.CurrentUserID(c))
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

// scheduleFailureFact 单条失败原因的**事实**（写侧只写 key，读侧在模板里取词）。
//
// 未登记的 key 落到归口词条而不是裸 key：那一列的值由 service 写、词条表由运营改，
// 两边对不上时用户看到的该是「排定执行失败」而不是一串常量名。
func scheduleFailureFact(key string) pageMsg {
	fallback, ok := pageenums.ScheduleFailureFallbacks[key]
	if !ok {
		return pageMsg{
			Key:      pageenums.ErrScheduleApplyFailed,
			Fallback: pageenums.ScheduleFailureFallbacks[pageenums.ErrScheduleApplyFailed],
		}
	}
	return pageMsg{Key: key, Fallback: fallback}
}

// scheduleFailureText 单条失败原因的取词 —— **只给 JSON 出口用**。
//
// JSON 响应没有模板取词这一环（前端拿到的是字节，不是模板），成品文案必须在服务端拼；
// 页面出口用 scheduleFailureFact + 模板里的 tr，不要调本函数。
func scheduleFailureText(tr func(key, fallback string) string, key string) string {
	f := scheduleFailureFact(key)
	return tr(f.Key, f.Fallback)
}

// —— 后台面板 ——
//
// 面板挂在 /admin 页面组（Session + CSRF 已具备），写操作显式复用 API 的权限点路径
// （builtin.CasbinMiddlewareForPath），与页面列表页的行内写操作同法 ——
// 页面路径与权限点路径不一致，直接按页面路径 enforce 会因权限点表无此路径而全员 403。

// schedulePanelData 面板片段的模板数据（gin.H，键与模板逐一对应）。
//
// 键一律**总是注入**（空串 / 空切片而不是缺失）：后台片段模板用点号访问 map 键，
// 缺 key 会在运行期报错并让整段片段消失（inert 的失败，页面上只是少了东西）。
// errMsg / doneMsg 是**事实**（key + 中文兜底 + 参数），取词由片段模板做；
// 数据在返回前经 adminui.PrepareBare 注入 t / tf（片段不走 Prepare）。
func (h *pagesAdminHandle) schedulePanelData(c *gin.Context, pageID string, errMsg, doneMsg pageMsg) gin.H {
	// token 拿不到不阻断渲染：提交会被 CSRF 中间件拒（与其它后台页一致）。
	token, terr := builtin.GetCSRFToken(c)
	if terr != nil {
		token = ""
	}
	// 面板标题里的页面路径：取**数据库**的草稿路径（detail），而不是从 query 参数回显 ——
	// 查询参数不是可信边界（读侧回执协议已整批删除：结论走响应体，不进 URL），
	// 而这里的数据本来就有一个真源。
	path := ""
	if pageID != "" && h.pages != nil {
		if pid, perr := h.pages.ProjectOfPage(c.Request.Context(), pageID); perr == nil && pid != "" {
			if detail, derr := h.pages.Detail(c.Request.Context(), &pagedto.DetailReq{ProjectID: pid, ID: pageID}); derr == nil && detail != nil {
				path = detail.DraftPath
			}
		}
	}
	rows := []pagedto.ScheduleRow{}
	if pageID != "" && h.pages != nil {
		res, lerr := h.pages.ListPageSchedules(c.Request.Context(), &pagedto.ScheduleListReq{PageID: pageID})
		if lerr != nil {
			errMsg = pageErrMsg(c, lerr)
		} else if res != nil {
			for i := range res.Items {
				item := res.Items[i]
				failure := scheduleFailureFact(item.LastError)
				rows = append(rows, pagedto.ScheduleRow{
					ID: item.ID, Action: item.Action, Lang: item.Lang,
					ScheduledAt:  item.ScheduledAt,
					Status:       item.Status,
					Attempts:     item.Attempts,
					NoteKey:      failure.Key,
					NoteFallback: failure.Fallback,
					// 只取消 pending 与 running 中的前者：running 的行已经在执行了。
					CanCancel: item.Status == pagemodel.ScheduleStatusPending,
				})
			}
		}
	}
	return adminui.PrepareBare(c, gin.H{
		"CSRFToken": token,
		"PageID":    pageID,
		"Path":      path,
		"Items":     rows,
		"Err":       errMsg,
		"Done":      doneMsg,
		// 默认到点时刻 = 站点时区下的「一小时后」，省掉每次手填（运营多数排的是近期动作）。
		"DefaultAt": sitetz.FormatDateTime(time.Now().Add(time.Hour)),
	})
}

// schedulePendingAt 列表行「已排定」徽标要的到点时刻 —— 只交**事实**（时间对象），
// 格式化交给模板的 siteDateTime()（站点时区、到分钟）。
//
// 时区口径：库里的 scheduled_at 是绝对时刻，而「几点上线」是**面向人**的口径
// （pkg/sitetz 是它的唯一声明点）。注意排定**面板**的行（pagedto.ScheduleRow.ScheduledAt →
// 模板 dateTime()）仍按服务器本地时区显示 —— 两处口径不同是存量事实，本卡只把格式化挪到
// 模板，**不统一口径**（统一属行为变更，另卡 + 真库证据）。
//
// nil / 零值返回 nil：模板用 {{if isset(...)}} + 格式化后判空串两档，徽标不显示。
func schedulePendingAt(item *pagedto.ScheduleItem) *utils.JSONTime {
	if item == nil {
		return nil
	}
	at := item.ScheduledAt.Time()
	if at.IsZero() {
		return nil
	}
	return &item.ScheduledAt
}

// SchedulePanel 渲染某个页面的排定面板片段（HTMX：列表页行内「定时」按钮的落点）。
func (h *pagesAdminHandle) SchedulePanel(c *gin.Context) {
	pageID := strings.TrimSpace(c.Query("pageId"))
	c.HTML(http.StatusOK, "fragments/page_schedule_panel",
		h.schedulePanelData(c, pageID, pageMsg{}, pageMsg{}))
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
		CreateBy:     int64(builtin.CurrentUserID(c)),
	}
	var errMsg, doneMsg pageMsg
	if h.pages == nil {
		errMsg = pageInternalMsg()
	} else if _, err := h.pages.SetPageSchedule(c.Request.Context(), req); err != nil {
		errMsg = pageErrMsg(c, err)
	} else {
		doneMsg = pageMsg{Key: pageenums.MsgScheduleSet, Fallback: pageenums.MsgScheduleSet}
	}
	h.renderSchedulePanel(c, pageID, errMsg, doneMsg)
}

// ScheduleCancel 后台表单取消排定。
func (h *pagesAdminHandle) ScheduleCancel(c *gin.Context) {
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	id, perr := strconv.ParseInt(strings.TrimSpace(c.PostForm("id")), 10, 64)
	var errMsg, doneMsg pageMsg
	switch {
	case h.pages == nil:
		errMsg = pageInternalMsg()
	case perr != nil || id <= 0:
		errMsg = pageErrMsg(c, pageservice.ErrInvalidParam)
	default:
		if err := h.pages.CancelPageSchedule(c.Request.Context(), &pagedto.ScheduleCancelReq{
			PageID: pageID, ID: id,
		}); err != nil {
			errMsg = pageErrMsg(c, err)
		} else {
			doneMsg = pageMsg{Key: pageenums.MsgScheduleCanceled, Fallback: pageenums.MsgScheduleCanceled}
		}
	}
	h.renderSchedulePanel(c, pageID, errMsg, doneMsg)
}

// renderSchedulePanel 排定面板写动作的出口分档：htmx 重渲片段、原生走整页提示。
//
// 为什么非 HTMX 不渲染片段：浏览器直接 POST（禁用了 JS、或从表单源码提交）时，
// 把片段当成整页返回会得到一个没有外壳的裸片段。原生档因此改走 shell.RenderJump
// （结论走响应体，取代原先「303 回列表页、什么也不说」）。
func (h *pagesAdminHandle) renderSchedulePanel(c *gin.Context, pageID string, errMsg, doneMsg pageMsg) {
	if !reqparams.IsHXRequest(c) {
		if errMsg.Key != "" || errMsg.Fallback != "" {
			pageListJump(c, false, errMsg)
			return
		}
		pageListJump(c, true, doneMsg)
		return
	}
	c.HTML(http.StatusOK, "fragments/page_schedule_panel",
		h.schedulePanelData(c, pageID, errMsg, doneMsg))
}
