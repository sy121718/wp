// membership_handle.go — membership 模块的 JSON 接口（BIZ-3）。
//
// 与后台页面（membership_page_handle.go / membership_assign_page_handle.go）的分工：
// 页面的写动作经「表单 POST → 302 回列表」，接口走 pkg/response 的 JSON 信封。
// 两者共用同一份 service 与同一份错误归口（membership_err.go），
// 所以「同一个错误在页面与接口上说法一致」不是靠两边各写一遍。
package membershiphttp

import (
	"github.com/gin-gonic/gin"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	"go_wp/pkg/response"
)

// Handle membership 模块的 HTTP 处理器。
type Handle struct {
	svc membershipcontract.MembershipService
}

// NewHandle 构造。
func NewHandle(svc membershipcontract.MembershipService) *Handle {
	return &Handle{svc: svc}
}

// ListTiers 列出某工程的等级（含各自权益）。
func (h *Handle) ListTiers(c *gin.Context) {
	req := &membershipdto.ListTiersReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ParamError(c)
		return
	}
	list, err := h.svc.ListTiers(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, membershipErrStatus(err), membershipErrText(c, err))
		return
	}
	response.Success(c, list)
}

// GetTier 等级详情（含权益）。
func (h *Handle) GetTier(c *gin.Context) {
	req := &membershipdto.GetTierReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.GetTier(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, membershipErrStatus(err), membershipErrText(c, err))
		return
	}
	response.Success(c, res)
}

// CreateTier 新建等级（可一次带上权益）。
func (h *Handle) CreateTier(c *gin.Context) {
	req := &membershipdto.CreateTierReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.CreateTier(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, membershipErrStatus(err), membershipErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, membershipenums.MsgTierCreated, res)
}

// UpdateTier 更新等级（指针字段 = 只在非 nil 时改）。
func (h *Handle) UpdateTier(c *gin.Context) {
	req := &membershipdto.UpdateTierReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.UpdateTier(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, membershipErrStatus(err), membershipErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, membershipenums.MsgTierUpdated, res)
}

// DeleteTier 删除等级（软删；仍挂着归属时被拒）。
func (h *Handle) DeleteTier(c *gin.Context) {
	req := &membershipdto.DeleteTierReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.DeleteTier(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, membershipErrStatus(err), membershipErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, membershipenums.MsgTierDeleted, gin.H{"tierId": req.TierID})
}

// SaveEntitlements 全量保存某等级的权益。
func (h *Handle) SaveEntitlements(c *gin.Context) {
	req := &membershipdto.SaveEntitlementsReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.SaveEntitlements(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, membershipErrStatus(err), membershipErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, membershipenums.MsgEntitlementSaved, res)
}

// ListAssignments 列出某工程的会员归属（分页 + 筛选，与计数同一组条件）。
func (h *Handle) ListAssignments(c *gin.Context) {
	req := &membershipdto.ListAssignmentsReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ParamError(c)
		return
	}
	list, err := h.svc.ListAssignments(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, membershipErrStatus(err), membershipErrText(c, err))
		return
	}
	total, cerr := h.svc.CountAssignments(c.Request.Context(), &membershipdto.CountAssignmentsReq{
		ProjectID: req.ProjectID, TierID: req.TierID, Source: req.Source, UserID: req.UserID,
	})
	if cerr != nil {
		response.ErrorWithMessage(c, membershipErrStatus(cerr), membershipErrText(c, cerr))
		return
	}
	response.Success(c, gin.H{"list": list, "total": total})
}

// AssignSet 手工指定某访客的等级。
func (h *Handle) AssignSet(c *gin.Context) {
	req := &membershipdto.AssignManualReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.AssignManual(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, membershipErrStatus(err), membershipErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, membershipenums.MsgAssignSet, res)
}

// AssignUnlock 取消手工锁定。
func (h *Handle) AssignUnlock(c *gin.Context) {
	req := &membershipdto.UnlockManualReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ParamError(c)
		return
	}
	if err := h.svc.UnlockManual(c.Request.Context(), req); err != nil {
		response.ErrorWithMessage(c, membershipErrStatus(err), membershipErrText(c, err))
		return
	}
	response.SuccessWithMessage(c, membershipenums.MsgAssignUnlocked, gin.H{"userId": req.UserID})
}

// Resolve 解析某访客在某工程的会员身份（消费侧读接口）。
func (h *Handle) Resolve(c *gin.Context) {
	req := &membershipdto.ResolveReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.Resolve(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, membershipErrStatus(err), membershipErrText(c, err))
		return
	}
	response.Success(c, res)
}

// Recalc 立即重算某工程的会员归属。
//
// 这个接口存在的原因是排障：消费额端口未接入时它返回 503 + 归口文案
// （「归属重算不可用」），而不是一个 Scanned = 0 的「成功」结果 ——
// 后者会让人以为「这个工程没人消费过」，从而往完全错误的方向排查。
func (h *Handle) Recalc(c *gin.Context) {
	req := &membershipdto.RecalcProjectReq{}
	if err := c.ShouldBind(req); err != nil {
		response.ParamError(c)
		return
	}
	res, err := h.svc.RecalcProject(c.Request.Context(), req)
	if err != nil {
		response.ErrorWithMessage(c, membershipErrStatus(err), membershipErrText(c, err))
		return
	}
	response.Success(c, res)
}
