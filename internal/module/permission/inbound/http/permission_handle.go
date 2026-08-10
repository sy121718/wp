package permissionhttp

import (
	permissiondto "go_wp/internal/module/permission/dto"
	permissionenums "go_wp/internal/module/permission/enums"
	permissioncontract "go_wp/internal/module/permission/contract"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// Handle 权限点 HTTP 控制器，只做参数绑定和响应输出。
type Handle struct {
	svc permissioncontract.PermissionService
}

func NewHandle(svc permissioncontract.PermissionService) *Handle {
	return &Handle{svc: svc}
}

func (h *Handle) List(c *gin.Context) {
	var req permissiondto.ListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, permissionenums.MsgBadRequest+": "+err.Error())
		return
	}

	res, err := h.svc.List(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

func (h *Handle) Detail(c *gin.Context) {
	var req permissiondto.DetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, permissionenums.MsgBadRequest+": "+err.Error())
		return
	}

	res, err := h.svc.Detail(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.Success(c, res)
}

func (h *Handle) Options(c *gin.Context) {
	var req permissiondto.OptionsReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, permissionenums.MsgBadRequest+": "+err.Error())
		return
	}

	res, err := h.svc.Options(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

func (h *Handle) Create(c *gin.Context) {
	var req permissiondto.CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, permissionenums.MsgBadRequest+": "+err.Error())
		return
	}

	res, err := h.svc.Create(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, permissionenums.MsgSuccess, res)
}

func (h *Handle) Update(c *gin.Context) {
	var req permissiondto.UpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, permissionenums.MsgBadRequest+": "+err.Error())
		return
	}

	res, err := h.svc.Update(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, permissionenums.MsgSuccess, res)
}

func (h *Handle) Delete(c *gin.Context) {
	var req permissiondto.DeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, permissionenums.MsgBadRequest+": "+err.Error())
		return
	}

	res, err := h.svc.Delete(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, permissionenums.MsgSuccess, res)
}