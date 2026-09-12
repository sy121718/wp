// mail_automation_handle.go — 自动化流程与实例的 API 承接层（issue #38 P3）。
package mailhttp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	maildto "go_wp/internal/module/mail/dto"
	mailenums "go_wp/internal/module/mail/enums"
	"go_wp/pkg/response"
)

// operatorID 取当前操作人 id（会话中间件写入，只用于留痕，不参与鉴权判断）。
func operatorID(c *gin.Context) uint64 {
	if v, exists := c.Get("user_id"); exists {
		switch n := v.(type) {
		case uint64:
			return n
		case int64:
			if n > 0 {
				return uint64(n)
			}
		case int:
			if n > 0 {
				return uint64(n)
			}
		}
	}
	return 0
}

// AutomationSave 新建 / 更新流程（保存前校验图：无环 + 可达 + 形状）。
func (h *Handle) AutomationSave(c *gin.Context) {
	var req maildto.SaveAutomationReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	req.OperatorID = operatorID(c)
	item, err := h.svc.SaveAutomation(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, item)
}

// AutomationList 流程列表。
func (h *Handle) AutomationList(c *gin.Context) {
	res, err := h.svc.ListAutomations(c.Request.Context(), &maildto.AutomationListReq{
		Status:   c.Query("status"),
		Page:     parseInt(c.Query("page"), 1),
		PageSize: parseInt(c.Query("pageSize"), 20),
	})
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, res)
}

// AutomationGet 流程详情。
func (h *Handle) AutomationGet(c *gin.Context) {
	item, err := h.svc.GetAutomation(c.Request.Context(), parseID(c.Query("id")))
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, item)
}

// AutomationStatus 启用 / 暂停 / 退回草稿（启用时会再校验一遍图）。
func (h *Handle) AutomationStatus(c *gin.Context) {
	var req maildto.SetAutomationStatusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	req.OperatorID = operatorID(c)
	if err := h.svc.SetAutomationStatus(c.Request.Context(), &req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgSaveSuccess, nil)
}

// AutomationDelete 删除流程（进行中的实例会先被停止）。
func (h *Handle) AutomationDelete(c *gin.Context) {
	if err := h.svc.DeleteAutomation(c.Request.Context(), parseID(c.Query("id"))); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgDeleteSuccess, nil)
}

// AutomationStartRun 手工把联系人加进流程（manual 触发）。
//
// 已在流程中时返回明确错误而不是静默成功 —— 静默成功会让运营以为「加进去了」，
// 实际上那个人正走在上一轮流程里。
func (h *Handle) AutomationStartRun(c *gin.Context) {
	var req maildto.AutomationRunReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrInvalidParam)
		return
	}
	req.OperatorID = operatorID(c)
	started, err := h.svc.StartRun(c.Request.Context(), req.AutomationID, req.ContactID, "manual")
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	if !started {
		response.ErrorWithMessage(c, http.StatusBadRequest, mailenums.ErrAutomationRunExists)
		return
	}
	response.SuccessWithMessage(c, mailenums.MsgAutomationStarted, nil)
}

// AutomationRunList 实例列表（排障入口）。
func (h *Handle) AutomationRunList(c *gin.Context) {
	res, err := h.svc.ListAutomationRuns(c.Request.Context(), &maildto.AutomationRunListReq{
		AutomationID: parseID(c.Query("automationId")),
		Status:       c.Query("status"),
		Page:         parseInt(c.Query("page"), 1),
		PageSize:     parseInt(c.Query("pageSize"), 20),
	})
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, res)
}

// AutomationRunDetail 实例排障详情：一句人话解释「卡在哪、为什么」+ 节点时间线。
func (h *Handle) AutomationRunDetail(c *gin.Context) {
	res, err := h.svc.AutomationRunDetail(c.Request.Context(), parseID(c.Query("id")))
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, res)
}

// AutomationTick 手工扫一轮延时兜底（排障 / 运维用：主路径失效时立刻补投）。
func (h *Handle) AutomationTick(c *gin.Context) {
	n, err := h.svc.EnqueueDueRuns(c.Request.Context(), parseInt(c.Query("limit"), 200))
	if err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Success(c, gin.H{"queued": n})
}
