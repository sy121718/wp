package adminhttp

import (
	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// RoleList 角色分页列表接口。
func (h *Handle) RoleList(c *gin.Context) {
	var req admindto.RoleListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.role.RoleList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// RoleDetail 角色详情接口。
func (h *Handle) RoleDetail(c *gin.Context) {
	var req admindto.RoleDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.role.RoleDetail(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.Success(c, res)
}

// RoleCreate 新建角色接口。
func (h *Handle) RoleCreate(c *gin.Context) {
	var req admindto.RoleCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.role.RoleCreate(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// RoleUpdate 更新角色接口。
func (h *Handle) RoleUpdate(c *gin.Context) {
	var req admindto.RoleUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.role.RoleUpdate(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// RoleDelete 删除角色接口。
func (h *Handle) RoleDelete(c *gin.Context) {
	var req admindto.RoleDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.role.RoleDelete(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// RoleMenuList 角色拥有的菜单 ID 列表接口。
func (h *Handle) RoleMenuList(c *gin.Context) {
	var req admindto.RoleMenuListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.role.RoleMenuList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.Success(c, res)
}

// RoleMenuSave 保存角色菜单授权接口。
func (h *Handle) RoleMenuSave(c *gin.Context) {
	var req admindto.RoleMenuSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.role.RoleMenuSave(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}

// RoleUserList 角色下的用户列表接口。
func (h *Handle) RoleUserList(c *gin.Context) {
	var req admindto.RoleUserListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.role.RoleUserList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.Success(c, res)
}

// RoleUserSave 保存角色用户绑定接口。
func (h *Handle) RoleUserSave(c *gin.Context) {
	var req admindto.RoleUserSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	// 注入当前操作者（超管保护判定依据，禁止前端伪造）。
	userID, exists := c.Get("user_id")
	uid, ok := userID.(int64)
	if !exists || !ok || uid <= 0 {
		r.ErrorWithMessage(c, 401, adminenums.MsgUnauthorized)
		return
	}
	req.OperatorID = uint64(uid)
	res, err := h.role.RoleUserSave(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, res)
}
