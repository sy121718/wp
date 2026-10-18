// membership_handle.go — membership 模块的 JSON 接口（BIZ-3）。
//
// 后台写表单直调本接口，采用 pkg/response 的 JSON 信封与请求语言文案。
package membershiphttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
)

// membershipErrScene 日志场景名（与模块内其它 logger.Scene("membership") 一致）。
const membershipErrScene = "membership"

// membershipConflictKeys 冲突类业务错误（409）：同工程内的唯一性约束，以及「默认等级必须留一个」。
var membershipConflictKeys = map[string]struct{}{
	membershipenums.ErrTierNameTaken:       {},
	membershipenums.ErrThresholdTaken:      {},
	membershipenums.ErrDefaultTierExists:   {},
	membershipenums.ErrDefaultTierRequired: {},
	membershipenums.ErrTierInUse:           {},
	membershipenums.ErrManualNotLocked:     {},
}

// membershipErrStatus 业务错误 → HTTP 状态码。
//
// 判据只有 pkg/facing.FactOf 给出的 Key（不再有第二份白名单）；未命中判据的内部错误
// 由 response.ErrorInternal 统一给 500，所以这里只覆盖「确实是业务错误」的那一档。
func membershipErrStatus(fact facing.Fact) int {
	switch fact.Key {
	case membershipenums.ErrNotFound:
		return http.StatusNotFound
	case membershipenums.ErrRecalcUnavailable:
		return http.StatusServiceUnavailable
	}
	if _, ok := membershipConflictKeys[fact.Key]; ok {
		return http.StatusConflict
	}
	return http.StatusBadRequest
}

// membershipFail 本模块 JSON 接口的错误出口。
//
// 文案交给 response.MessageOf（与页面出口同一个判据、同一份兜底），
// 带定位信息时把 Tail 拼回去 —— 「该工程没有默认等级」后面那句 project_id 是给操作者定位用的数据。
func membershipFail(c *gin.Context, err error) {
	fact, business := facing.FactOf(err)
	if !business {
		response.ErrorInternal(c, membershipErrScene, err)
		return
	}
	response.ErrorWithMessage(c, membershipErrStatus(fact), membershipText(c, err))
}

// membershipText 一条错误的对外文案（按本次请求的语言取词），带定位信息时把 Tail 拼回去。
//
// 页面出口（整页失败槽、片段提示条）与 JSON 出口共用它：各写一份的症状是同一条错误
// 在接口上给了原因、在页面上只说「服务器内部错误」，而两边都不报错。
func membershipText(c *gin.Context, err error) string {
	fact, business := facing.FactOf(err)
	text := response.MessageOf(c, membershipErrScene, err)
	if business && fact.Tail != "" {
		text += membershipDetailSep(response.RequestLanguage(c)) + fact.Tail
	}
	return text
}

// membershipDetailSep 定位信息的分隔符：中文用全角冒号，其余语言用「: 」。
func membershipDetailSep(lang string) string {
	if strings.HasPrefix(strings.ToLower(lang), "zh") {
		return "："
	}
	return ": "
}

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
	req, ok := binding.Form[membershipdto.ListTiersReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListTiers(c.Request.Context(), req)
	if err != nil {
		membershipFail(c, err)
		return
	}
	response.Success(c, list)
}

// GetTier 等级详情（含权益）。
func (h *Handle) GetTier(c *gin.Context) {
	req, ok := binding.Form[membershipdto.GetTierReq](c)
	if !ok {
		return
	}
	res, err := h.svc.GetTier(c.Request.Context(), req)
	if err != nil {
		membershipFail(c, err)
		return
	}
	response.Success(c, res)
}

// CreateTier 新建等级（可一次带上权益）。
func (h *Handle) CreateTier(c *gin.Context) {
	req, ok := binding.Form[membershipdto.CreateTierReq](c)
	if !ok {
		return
	}
	res, err := h.svc.CreateTier(c.Request.Context(), req)
	if err != nil {
		membershipFail(c, err)
		return
	}
	response.SuccessWithMessage(c, membershipenums.MsgTierCreated, res)
}

// UpdateTier 更新等级（指针字段 = 只在非 nil 时改）。
func (h *Handle) UpdateTier(c *gin.Context) {
	req, ok := binding.Form[membershipdto.UpdateTierReq](c)
	if !ok {
		return
	}
	res, err := h.svc.UpdateTier(c.Request.Context(), req)
	if err != nil {
		membershipFail(c, err)
		return
	}
	response.SuccessWithMessage(c, membershipenums.MsgTierUpdated, res)
}

// DeleteTier 删除等级（软删；仍挂着归属时被拒）。
func (h *Handle) DeleteTier(c *gin.Context) {
	req, ok := binding.Form[membershipdto.DeleteTierReq](c)
	if !ok {
		return
	}
	if err := h.svc.DeleteTier(c.Request.Context(), req); err != nil {
		membershipFail(c, err)
		return
	}
	response.SuccessWithMessage(c, membershipenums.MsgTierDeleted, gin.H{"tierId": req.TierID})
}

// SaveEntitlements 全量保存某等级的权益。
func (h *Handle) SaveEntitlements(c *gin.Context) {
	req, ok := binding.Form[membershipdto.SaveEntitlementsReq](c)
	if !ok {
		return
	}
	res, err := h.svc.SaveEntitlements(c.Request.Context(), req)
	if err != nil {
		membershipFail(c, err)
		return
	}
	response.SuccessWithMessage(c, membershipenums.MsgEntitlementSaved, res)
}

// ListAssignments 列出某工程的会员归属（分页 + 筛选，与计数同一组条件）。
func (h *Handle) ListAssignments(c *gin.Context) {
	req, ok := binding.Form[membershipdto.ListAssignmentsReq](c)
	if !ok {
		return
	}
	list, err := h.svc.ListAssignments(c.Request.Context(), req)
	if err != nil {
		membershipFail(c, err)
		return
	}
	total, cerr := h.svc.CountAssignments(c.Request.Context(), &membershipdto.CountAssignmentsReq{
		ProjectID: req.ProjectID, TierID: req.TierID, Source: req.Source, UserID: req.UserID,
	})
	if cerr != nil {
		membershipFail(c, cerr)
		return
	}
	response.Success(c, gin.H{"list": list, "total": total})
}

// AssignSet 手工指定某访客的等级。
func (h *Handle) AssignSet(c *gin.Context) {
	req, ok := binding.Form[membershipdto.AssignManualReq](c)
	if !ok {
		return
	}
	res, err := h.svc.AssignManual(c.Request.Context(), req)
	if err != nil {
		membershipFail(c, err)
		return
	}
	response.SuccessWithMessage(c, membershipenums.MsgAssignSet, res)
}

// AssignUnlock 取消手工锁定。
func (h *Handle) AssignUnlock(c *gin.Context) {
	req, ok := binding.Form[membershipdto.UnlockManualReq](c)
	if !ok {
		return
	}
	if err := h.svc.UnlockManual(c.Request.Context(), req); err != nil {
		membershipFail(c, err)
		return
	}
	response.SuccessWithMessage(c, membershipenums.MsgAssignUnlocked, gin.H{"userId": req.UserID})
}

// Resolve 解析某访客在某工程的会员身份（消费侧读接口）。
func (h *Handle) Resolve(c *gin.Context) {
	req, ok := binding.Form[membershipdto.ResolveReq](c)
	if !ok {
		return
	}
	res, err := h.svc.Resolve(c.Request.Context(), req)
	if err != nil {
		membershipFail(c, err)
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
	req, ok := binding.Form[membershipdto.RecalcProjectReq](c)
	if !ok {
		return
	}
	res, err := h.svc.RecalcProject(c.Request.Context(), req)
	if err != nil {
		membershipFail(c, err)
		return
	}
	response.Success(c, res)
}
