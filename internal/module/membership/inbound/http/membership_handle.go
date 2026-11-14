package membershiphttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	membershipcontract "go_wp/internal/module/membership/contract"
	membershipdto "go_wp/internal/module/membership/dto"
	membershipenums "go_wp/internal/module/membership/enums"
	"go_wp/pkg/binding"
	"go_wp/pkg/facing"
	"go_wp/pkg/response"
	pageview "go_wp/pkg/view"
)

// membershipFormFail 是表单绑定与字段解析失败的统一出口：只给受控文案，不上屏内部原文。
func membershipFormFail(c *gin.Context, err error) {
	pageview.FailStatus(c, http.StatusBadRequest, membershipErrScene, err)
}

// runMembershipWrite 是会员写端点的统一骨架：绑定 → 投影 → 调用服务 → 成功出口。
//
// 三个回调各自只做一件事；任一环节失败都已写出响应，控制器不再重复判断。
func runMembershipWrite[F any, R any](c *gin.Context,
	build func(*F) (R, error),
	call func(context.Context, R) (any, error),
	success func(*gin.Context)) {
	form := bindMembershipForm[F](c)
	if form == nil {
		return
	}
	req, err := build(form)
	if err != nil {
		membershipFormFail(c, err)
		return
	}
	if _, err := call(c.Request.Context(), req); err != nil {
		membershipFail(c, err)
		return
	}
	success(c)
}

// bindMembershipForm 绑定会员写表单；失败已由这里写出 400。
func bindMembershipForm[T any](c *gin.Context) *T {
	form, err := binding.ParseForm[T](c)
	if err != nil {
		membershipFormFail(c, membershipenums.Err(membershipenums.ErrInvalidParam))
		return nil
	}
	return form
}

// membershipUint 把表单里的无符号整数字串转成数值；空串或非法值返回错误。
func membershipUint(text, field string) (uint64, error) {
	value, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	if err != nil {
		return 0, membershipenums.Err(membershipenums.ErrInvalidParam)
	}
	return value, nil
}

// membershipInt 是 DTO 解析的本地别名（控制器只做绑定，解析归 DTO）。
func membershipInt(text string) (int64, error) { return membershipdto.ParseIntField(text) }

// membershipYuan 是 DTO 金额解析的本地别名。
func membershipYuan(text string) (int64, error) { return membershipdto.ParseYuan(text) }

// membershipEntitlements 把「免运费勾选 + 折扣数字」映射成权益清单。
// 空折扣表示该等级没有折扣权益；0 与非法值交给 service 的统一校验。
func membershipEntitlements(freeShipping, discount string) []membershipdto.EntitlementReq {
	items := make([]membershipdto.EntitlementReq, 0, 2)
	if strings.TrimSpace(freeShipping) != "" {
		items = append(items, membershipdto.EntitlementReq{
			Kind: membershipenums.KindFreeShipping, ValueInt: 1,
		})
	}
	if strings.TrimSpace(discount) != "" {
		value, err := strconv.ParseInt(strings.TrimSpace(discount), 10, 64)
		if err != nil {
			value = -1
		}
		items = append(items, membershipdto.EntitlementReq{
			Kind: membershipenums.KindDiscount, ValueInt: value,
		})
	}
	return items
}

// membershipMessageFallback 只在缺词时兜底，绝不让裸 key 上屏。
var membershipMessageFallback = map[string]string{
	membershipenums.MsgTierCreated:      "等级已创建",
	membershipenums.MsgTierUpdated:      "等级已更新",
	membershipenums.MsgTierDeleted:      "等级已删除",
	membershipenums.MsgEntitlementSaved: "权益已保存",
	membershipenums.MsgAssignSet:        "已指定会员等级（该账号不再参与自动升级）",
	membershipenums.MsgAssignUnlocked:   "已解除手工锁定的会员归属",
}

// membershipWriteOK 是写操作的统一成功提示：走共享提示组件，按词条取词。
func membershipWriteOK(c *gin.Context, key string) {
	pageview.SuccessMessage(c, key, membershipMessageFallback[key])
}

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
		pageview.FailStatus(c, http.StatusInternalServerError, membershipErrScene, err)
		return
	}
	pageview.FailStatus(c, membershipErrStatus(fact), membershipErrScene, err)
}

func membershipText(c *gin.Context, err error) string {
	fact, business := facing.FactOf(err)
	if !business {
		return response.MessageOf(c, membershipErrScene, err)
	}

	text := response.MessageOf(c, membershipErrScene, facing.New(fact.Key))
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
	c.Header("Cache-Control", "no-store")
	runMembershipWrite(c, (*membershipdto.TierFormReq).CreateReq,
		func(ctx context.Context, req *membershipdto.CreateTierReq) (any, error) {
			return h.svc.CreateTier(ctx, req)
		},
		func(c *gin.Context) { membershipWriteOK(c, membershipenums.MsgTierCreated) })
}

func (h *Handle) UpdateTier(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	runMembershipWrite(c, (*membershipdto.TierFormReq).UpdateReq,
		func(ctx context.Context, req *membershipdto.UpdateTierReq) (any, error) {
			return h.svc.UpdateTier(ctx, req)
		},
		func(c *gin.Context) { membershipWriteOK(c, membershipenums.MsgTierUpdated) })
}

func (h *Handle) DeleteTier(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	runMembershipWrite(c, (*membershipdto.DeleteTierFormReq).DeleteReq,
		func(ctx context.Context, req *membershipdto.DeleteTierReq) (any, error) {
			return nil, h.svc.DeleteTier(ctx, req)
		},
		func(c *gin.Context) { membershipWriteOK(c, membershipenums.MsgTierDeleted) })
}

func (h *Handle) SaveEntitlements(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	runMembershipWrite(c, (*membershipdto.EntitlementFormReq).SaveReq,
		func(ctx context.Context, req *membershipdto.SaveEntitlementsReq) (any, error) {
			return h.svc.SaveEntitlements(ctx, req)
		},
		func(c *gin.Context) { membershipWriteOK(c, membershipenums.MsgEntitlementSaved) })
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
	c.Header("Cache-Control", "no-store")
	runMembershipWrite(c, (*membershipdto.AssignFormReq).AssignReq,
		func(ctx context.Context, req *membershipdto.AssignManualReq) (any, error) {
			return h.svc.AssignManual(ctx, req)
		},
		func(c *gin.Context) { membershipWriteOK(c, membershipenums.MsgAssignSet) })
}

func (h *Handle) AssignUnlock(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	runMembershipWrite(c, (*membershipdto.UnlockFormReq).UnlockReq,
		func(ctx context.Context, req *membershipdto.UnlockManualReq) (any, error) {
			return nil, h.svc.UnlockManual(ctx, req)
		},
		func(c *gin.Context) { membershipWriteOK(c, membershipenums.MsgAssignUnlocked) })
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
	c.Header("Cache-Control", "no-store")
	form := bindMembershipForm[membershipdto.RecalcProjectReq](c)
	if form == nil {
		return
	}
	res, err := h.svc.RecalcProject(c.Request.Context(), form)
	if err != nil {
		membershipFail(c, err)
		return
	}
	c.HTML(http.StatusOK, "admin/membership/membership_recalc_result.html", pageview.Objects(
		pageview.Object("Scanned", res.Scanned),
		pageview.Object("Changed", res.Changed),
		pageview.Object("Skipped", res.Skipped),
	))
}
