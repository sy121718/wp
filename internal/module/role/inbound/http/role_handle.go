// Package rolehttp 角色模块 HTTP 层。
package rolehttp

import (
	rolecontract "go_wp/internal/module/role/contract"
	roledto "go_wp/internal/module/role/dto"
	roleenums "go_wp/internal/module/role/enums"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// Handle 角色 HTTP 请求处理器。
type Handle struct {
	svc rolecontract.RoleService
}

// NewHandle 创建角色 HTTP 请求处理器。
func NewHandle(svc rolecontract.RoleService) *Handle {
	return &Handle{svc: svc}
}

// List 角色分页列表接口。
func (h *Handle) List(c *gin.Context) {
	var req roledto.ListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, roleenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.svc.List(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// Detail 角色详情接口。
func (h *Handle) Detail(c *gin.Context) {
	var req roledto.DetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, roleenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.svc.Detail(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.Success(c, res)
}

// Create 新建角色接口。
func (h *Handle) Create(c *gin.Context) {
	var req roledto.CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, roleenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.svc.Create(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, roleenums.MsgSuccess, nil)
}

// Update 更新角色接口。
func (h *Handle) Update(c *gin.Context) {
	var req roledto.UpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, roleenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.svc.Update(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, roleenums.MsgSuccess, nil)
}

// Delete 删除角色接口。
func (h *Handle) Delete(c *gin.Context) {
	var req roledto.DeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, roleenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.svc.Delete(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, roleenums.MsgSuccess, nil)
}

// MenuList 角色拥有的菜单 ID 列表接口。
func (h *Handle) MenuList(c *gin.Context) {
	var req roledto.MenuListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, roleenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.svc.MenuList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.Success(c, res)
}

// MenuSave 保存角色菜单授权接口。
func (h *Handle) MenuSave(c *gin.Context) {
	var req roledto.MenuSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, roleenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.svc.MenuSave(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, roleenums.MsgSuccess, res)
}

// UserList 角色下的用户列表接口。
func (h *Handle) UserList(c *gin.Context) {
	var req roledto.UserListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, roleenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.svc.UserList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.Success(c, res)
}

// UserSave 保存角色用户绑定接口。
func (h *Handle) UserSave(c *gin.Context) {
	var req roledto.UserSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, roleenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.svc.UserSave(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, roleenums.MsgSuccess, res)
}