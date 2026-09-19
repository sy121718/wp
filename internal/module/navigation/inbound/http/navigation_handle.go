package navigationhttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
	"go_wp/pkg/response"
)

// Handle navigation 模块 HTTP 处理器。
type Handle struct {
	svc navigationcontract.NavigationService
}

// NewHandle 构造。
func NewHandle(svc navigationcontract.NavigationService) *Handle {
	return &Handle{svc: svc}
}

// Create 新建导航项。
func (h *Handle) Create(c *gin.Context) {
	req := &navigationdto.CreateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Create(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgCreateSuccess, res)
}

// Update 更新导航项。
func (h *Handle) Update(c *gin.Context) {
	req := &navigationdto.UpdateReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgUpdateSuccess, res)
}

// Get 导航项详情。
func (h *Handle) Get(c *gin.Context) {
	req := &navigationdto.GetReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Get(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgDetailSuccess, res)
}

// List 导航项列表。
func (h *Handle) List(c *gin.Context) {
	req := &navigationdto.ListReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ParamError(c)
		return
	}
	list, err := h.svc.List(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgListSuccess, list)
}

// Delete 删除导航项。
func (h *Handle) Delete(c *gin.Context) {
	req := &navigationdto.DeleteReq{}
	if err := c.ShouldBindJSON(req); err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, navigationErrorStatus(err), navigationErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, navigationenums.MsgDeleteSuccess, nil)
}

// navigationErrorStatus 把业务错误映射为 HTTP 状态码。
func navigationErrorStatus(err error) int {
	msg := err.Error()
	switch {
	case strings.Contains(msg, navigationenums.ErrNotFound):
		return http.StatusNotFound
	// 乐观锁冲突用 409：与「你填错了」（400）和「系统坏了」（500）都不同 ——
	// 这一次请求本身没问题，是数据在这期间被别人改过，客户端该刷新后重做。
	case strings.Contains(msg, navigationenums.ErrStaleVersion):
		return http.StatusConflict
	case strings.Contains(msg, navigationenums.ErrInvalidParam),
		strings.Contains(msg, navigationenums.ErrInvalidKind),
		strings.Contains(msg, navigationenums.ErrPathTaken):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
