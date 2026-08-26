package adminhttp

import (
	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// MenuTree 菜单树查询接口。
func (h *Handle) MenuTree(c *gin.Context) {
	var req admindto.MenuTreeReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}

	list, err := h.menu.MenuTree(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, list)
}

// MenuDetail 菜单详情接口。
func (h *Handle) MenuDetail(c *gin.Context) {
	var req admindto.MenuDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}

	res, err := h.menu.MenuDetail(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.Success(c, res)
}

// MenuCreate 新建菜单接口。
func (h *Handle) MenuCreate(c *gin.Context) {
	var req admindto.MenuCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}

	if err := h.menu.MenuCreate(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// MenuUpdate 更新菜单接口。
func (h *Handle) MenuUpdate(c *gin.Context) {
	var req admindto.MenuUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}

	if err := h.menu.MenuUpdate(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// MenuDelete 批量删除菜单接口。
func (h *Handle) MenuDelete(c *gin.Context) {
	var req admindto.MenuDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}

	if err := h.menu.MenuDelete(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}
