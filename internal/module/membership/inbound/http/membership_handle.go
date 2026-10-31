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

const membershipErrScene = "membership"

var membershipConflictKeys = map[string]struct{}{
	membershipenums.ErrTierNameTaken:       {},
	membershipenums.ErrThresholdTaken:      {},
	membershipenums.ErrDefaultTierExists:   {},
	membershipenums.ErrDefaultTierRequired: {},
	membershipenums.ErrTierInUse:           {},
	membershipenums.ErrManualNotLocked:     {},
}

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

func membershipFail(c *gin.Context, err error) {
	fact, business := facing.FactOf(err)
	if !business {
		response.ErrorInternal(c, membershipErrScene, err)
		return
	}
	response.ErrorWithMessage(c, membershipErrStatus(fact), membershipText(c, err))
}

func membershipText(c *gin.Context, err error) string {
	fact, business := facing.FactOf(err)
	if !business {
		return response.MessageOf(c, membershipErrScene, err)
	}

	text := response.MessageOf(c, membershipErrScene, facing.New(fact.Key, fact.Fallback))
	if fact.Tail != "" {
		text += membershipDetailSep(response.RequestLanguage(c)) + fact.Tail
	}
	return text
}

func membershipDetailSep(lang string) string {
	if strings.HasPrefix(strings.ToLower(lang), "zh") {
		return "："
	}
	return ": "
}

type Handle struct {
	svc membershipcontract.MembershipService
}

func NewHandle(svc membershipcontract.MembershipService) *Handle {
	return &Handle{svc: svc}
}

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
