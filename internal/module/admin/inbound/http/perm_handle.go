package adminhttp

import (
	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// PermList 权限点分页列表接口。
func (h *Handle) PermList(c *gin.Context) {
	var req admindto.PermListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}

	res, err := h.perm.PermList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// PermDetail 权限点详情接口。
func (h *Handle) PermDetail(c *gin.Context) {
	var req admindto.PermDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}

	res, err := h.perm.PermDetail(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.Success(c, res)
}

// PermOptions 启用权限选项接口。
func (h *Handle) PermOptions(c *gin.Context) {
	var req admindto.PermOptionsReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}

	res, err := h.perm.PermOptions(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// PermCreate 新建权限点接口。
func (h *Handle) PermCreate(c *gin.Context) {
	var req admindto.PermCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}

	res, err := h.perm.PermCreate(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

// PermUpdate 更新权限点接口。
func (h *Handle) PermUpdate(c *gin.Context) {
	var req admindto.PermUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}

	res, err := h.perm.PermUpdate(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

// PermDelete 批量删除权限点接口。
func (h *Handle) PermDelete(c *gin.Context) {
	var req admindto.PermDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}

	res, err := h.perm.PermDelete(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}
