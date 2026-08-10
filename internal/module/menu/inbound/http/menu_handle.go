// Package menuhttp 菜单模块 HTTP 层。
package menuhttp

import (
	menudto "go_wp/internal/module/menu/dto"
	menuenums "go_wp/internal/module/menu/enums"
	menucontract "go_wp/internal/module/menu/contract"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// Handle 菜单 HTTP 请求处理器。
type Handle struct {
	svc menucontract.MenuService
}

// NewHandle 创建菜单 HTTP 请求处理器。
func NewHandle(svc menucontract.MenuService) *Handle {
	return &Handle{svc: svc}
}

// Tree 菜单树查询接口。
func (h *Handle) Tree(c *gin.Context) {
	var req menudto.TreeReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, menuenums.MsgBadRequest+": "+err.Error())
		return
	}

	list, err := h.svc.Tree(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, list)
}

// Detail 菜单详情接口。
func (h *Handle) Detail(c *gin.Context) {
	var req menudto.DetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, menuenums.MsgBadRequest+": "+err.Error())
		return
	}

	res, err := h.svc.Detail(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.Success(c, res)
}

// Create 新建菜单接口。
func (h *Handle) Create(c *gin.Context) {
	var req menudto.CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, menuenums.MsgBadRequest+": "+err.Error())
		return
	}

	if err := h.svc.Create(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, menuenums.MsgSuccess, nil)
}

// Update 更新菜单接口。
func (h *Handle) Update(c *gin.Context) {
	var req menudto.UpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, menuenums.MsgBadRequest+": "+err.Error())
		return
	}

	if err := h.svc.Update(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, menuenums.MsgSuccess, nil)
}

// Delete 批量删除菜单接口。
func (h *Handle) Delete(c *gin.Context) {
	var req menudto.DeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, menuenums.MsgBadRequest+": "+err.Error())
		return
	}

	if err := h.svc.Delete(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, menuenums.MsgSuccess, nil)
}