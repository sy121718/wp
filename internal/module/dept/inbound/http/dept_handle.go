// Package depthttp 部门模块 HTTP 层。
// 提供部门管理的 REST 接口：树形查询、详情、增删改、部门用户列表与分配。
package depthttp

import (
	deptcontract "go_wp/internal/module/dept/contract"
	deptdto "go_wp/internal/module/dept/dto"
	deptenums "go_wp/internal/module/dept/enums"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// Handle 部门模块 HTTP 处理器，封装请求绑定、service 调用和响应输出。
type Handle struct {
	svc deptcontract.DeptService
}

// NewHandle 创建部门模块 HTTP 处理器。
func NewHandle(svc deptcontract.DeptService) *Handle {
	return &Handle{svc: svc}
}

// Tree 获取完整部门树。
func (h *Handle) Tree(c *gin.Context) {
	list, err := h.svc.Tree(c.Request.Context())
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, list)
}

// Detail 查询部门详情（GET 参数 id）。
func (h *Handle) Detail(c *gin.Context) {
	var req deptdto.DetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, deptenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.svc.Detail(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.Success(c, res)
}

// Create 新建部门（JSON body）。
func (h *Handle) Create(c *gin.Context) {
	var req deptdto.CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, deptenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.svc.Create(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, deptenums.MsgSuccess, nil)
}

// Update 更新部门信息，支持移动父节点（JSON body）。
func (h *Handle) Update(c *gin.Context) {
	var req deptdto.UpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, deptenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.svc.Update(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, deptenums.MsgSuccess, nil)
}

// Delete 删除部门（JSON body id）。
func (h *Handle) Delete(c *gin.Context) {
	var req deptdto.DeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, deptenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.svc.Delete(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, deptenums.MsgSuccess, nil)
}

// UserList 查询部门下的用户列表（GET 参数）。
func (h *Handle) UserList(c *gin.Context) {
	var req deptdto.UserListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, deptenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.svc.UserList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// UserSave 批量分配用户到部门（JSON body）。
func (h *Handle) UserSave(c *gin.Context) {
	var req deptdto.UserSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, deptenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.svc.UserSave(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, deptenums.MsgSuccess, nil)
}