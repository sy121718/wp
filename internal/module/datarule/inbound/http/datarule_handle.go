// Package datarulehttp datarule 模块 HTTP 路由注册与处理器装配。
package datarulehttp

import (
	datarulecontract "go_wp/internal/module/datarule/contract"
	dataruledto "go_wp/internal/module/datarule/dto"
	dataruleenums "go_wp/internal/module/datarule/enums"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// Handle 数据规则 HTTP 请求处理器。
type Handle struct {
	svc datarulecontract.DataRuleService
}

// NewHandle 创建 Handle 实例。
func NewHandle(svc datarulecontract.DataRuleService) *Handle {
	return &Handle{svc: svc}
}

// List 规则分页列表。
func (h *Handle) List(c *gin.Context) {
	var req dataruledto.ListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, dataruleenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.svc.List(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// Detail 规则详情。
func (h *Handle) Detail(c *gin.Context) {
	var req dataruledto.DetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, dataruleenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.svc.Detail(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.Success(c, res)
}

// Create 新建规则。
func (h *Handle) Create(c *gin.Context) {
	var req dataruledto.CreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, dataruleenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.svc.Create(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, dataruleenums.MsgSuccess, nil)
}

// Update 更新规则。
func (h *Handle) Update(c *gin.Context) {
	var req dataruledto.UpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, dataruleenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.svc.Update(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, dataruleenums.MsgSuccess, nil)
}

// Delete 批量删除规则。
func (h *Handle) Delete(c *gin.Context) {
	var req dataruledto.DeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, dataruleenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.svc.Delete(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, dataruleenums.MsgSuccess, nil)
}

// SchemaList 返回所有已注册 domain。
func (h *Handle) SchemaList(c *gin.Context) {
	res, err := h.svc.SchemaList(c.Request.Context())
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// SchemaDetail 返回 domain 的字段白名单详情。
func (h *Handle) SchemaDetail(c *gin.Context) {
	var req dataruledto.SchemaDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, dataruleenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.svc.SchemaDetail(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	if res == nil {
		r.ErrorWithMessage(c, 400, dataruleenums.ErrInvalidDomain)
		return
	}
	r.Success(c, res)
}

// AssignmentList 查询规则分配列表。
func (h *Handle) AssignmentList(c *gin.Context) {
	var req dataruledto.AssignmentListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, dataruleenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.svc.AssignmentList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// AssignmentSave 批量保存规则分配。
func (h *Handle) AssignmentSave(c *gin.Context) {
	var req dataruledto.AssignmentSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, dataruleenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.svc.AssignmentSave(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, dataruleenums.MsgSuccess, nil)
}