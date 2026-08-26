package adminhttp

import (
	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	r "go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// RuleList 数据规则分页列表。
func (h *Handle) RuleList(c *gin.Context) {
	var req admindto.RuleListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.rule.RuleList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// RuleDetail 数据规则详情。
func (h *Handle) RuleDetail(c *gin.Context) {
	var req admindto.RuleDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.rule.RuleDetail(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.Success(c, res)
}

// RuleCreate 新建数据规则。
func (h *Handle) RuleCreate(c *gin.Context) {
	var req admindto.RuleCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.rule.RuleCreate(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// RuleUpdate 更新数据规则。
func (h *Handle) RuleUpdate(c *gin.Context) {
	var req admindto.RuleUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.rule.RuleUpdate(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// RuleDelete 批量删除数据规则。
func (h *Handle) RuleDelete(c *gin.Context) {
	var req admindto.RuleDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.rule.RuleDelete(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}

// RuleSchemaList 返回所有已注册 domain。
func (h *Handle) RuleSchemaList(c *gin.Context) {
	res, err := h.rule.RuleSchemaList(c.Request.Context())
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// RuleSchemaDetail 返回 domain 的字段白名单详情。
func (h *Handle) RuleSchemaDetail(c *gin.Context) {
	var req admindto.RuleSchemaDetailReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.rule.RuleSchemaDetail(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	if res == nil {
		r.ErrorWithMessage(c, 400, adminenums.ErrInvalidDomain)
		return
	}
	r.Success(c, res)
}

// RuleAssignmentList 查询规则分配列表。
func (h *Handle) RuleAssignmentList(c *gin.Context) {
	var req admindto.RuleAssignmentListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	res, err := h.rule.RuleAssignmentList(c.Request.Context(), &req)
	if err != nil {
		r.ErrorWithMessage(c, 500, err.Error())
		return
	}
	r.Success(c, res)
}

// RuleAssignmentSave 批量保存规则分配。
func (h *Handle) RuleAssignmentSave(c *gin.Context) {
	var req admindto.RuleAssignmentSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		r.ErrorWithMessage(c, 400, adminenums.MsgBadRequest+": "+err.Error())
		return
	}
	if err := h.rule.RuleAssignmentSave(c.Request.Context(), &req); err != nil {
		r.ErrorWithMessage(c, 400, err.Error())
		return
	}
	r.SuccessWithMessage(c, adminenums.MsgSuccess, nil)
}
