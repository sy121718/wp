package adminhttp

import (
	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// DeptTree 获取完整部门树。
func (h *Handle) DeptTree(c *gin.Context) {
	list, err := h.dept.DeptTree(c.Request.Context())
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, list)
}

// DeptDetail 查询部门详情（GET 参数 id）。
func (h *Handle) DeptDetail(c *gin.Context) {
	var req admindto.DeptDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.dept.DeptDetail(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.Success(c, res)
}

// DeptCreate 新建部门（JSON body）。
func (h *Handle) DeptCreate(c *gin.Context) {
	var req admindto.DeptCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.dept.DeptCreate(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// DeptUpdate 更新部门信息，支持移动父节点（JSON body）。
func (h *Handle) DeptUpdate(c *gin.Context) {
	var req admindto.DeptUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.dept.DeptUpdate(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// DeptDelete 删除部门（JSON body id）。
func (h *Handle) DeptDelete(c *gin.Context) {
	var req admindto.DeptDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.dept.DeptDelete(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// DeptUserList 查询部门下的用户列表（GET 参数）。
func (h *Handle) DeptUserList(c *gin.Context) {
	var req admindto.DeptUserListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.dept.DeptUserList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// DeptUserSave 批量分配用户到部门（JSON body）。
func (h *Handle) DeptUserSave(c *gin.Context) {
	var req admindto.DeptUserSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.dept.DeptUserSave(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}
