package membershiphttp

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"

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

// membershipUint 把表单里的无符号整数字串转成数值；空串或非法值返回错误。
func membershipUint(text, field string) (uint64, error) {
	value, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	if err != nil {
		return 0, membershipenums.Err(membershipenums.ErrInvalidParam)
	}
	return value, nil
}

// membershipInt 把表单里的整数字串转成数值；空串或非法值返回错误。
func membershipInt(text string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	if err != nil {
		return 0, membershipenums.Err(membershipenums.ErrInvalidParam)
	}
	return value, nil
}

// membershipYuan 把「元」的字符串按十进制语法精确换算成分。
// 不走浮点：金额一旦经过 float64 就会在边界上漂移。
func membershipYuan(text string) (int64, error) {
	raw := strings.ReplaceAll(strings.TrimSpace(text), ",", "")
	if raw == "" {
		return 0, nil
	}
	neg := false
	if strings.HasPrefix(raw, "-") {
		neg, raw = true, raw[1:]
	}
	whole, frac, hasFrac := strings.Cut(raw, ".")
	if len(frac) > 2 || !hasFrac && strings.Contains(raw, ".") {
		return 0, membershipenums.Err(membershipenums.ErrInvalidParam)
	}
	for _, r := range whole + frac {
		if r < '0' || r > '9' {
			return 0, membershipenums.Err(membershipenums.ErrInvalidParam)
		}
	}
	if whole == "" {
		whole = "0"
	}
	for len(frac) < 2 {
		frac += "0"
	}
	cents, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil {
		return 0, membershipenums.Err(membershipenums.ErrInvalidParam)
	}
	if neg {
		cents = -cents
	}
	return cents, nil
}

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
	var form membershipdto.TierFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		membershipFormFail(c, membershipenums.Err(membershipenums.ErrInvalidParam))
		return
	}
	threshold, err := membershipYuan(form.ThresholdYuan)
	if err != nil {
		membershipFormFail(c, err)
		return
	}
	sortOrder := int64(0)
	if strings.TrimSpace(form.SortOrder) != "" {
		if sortOrder, err = membershipInt(form.SortOrder); err != nil {
			membershipFormFail(c, err)
			return
		}
	}
	req := &membershipdto.CreateTierReq{
		ProjectID:       form.ProjectID,
		Name:            form.Name,
		SortOrder:       int(sortOrder),
		ThresholdAmount: threshold,
		IsDefault:       strings.TrimSpace(form.IsDefault) != "",
		Remark:          form.Remark,
		Entitlements:    membershipEntitlements(form.FreeShipping, form.DiscountPercent),
	}
	if _, err := h.svc.CreateTier(c.Request.Context(), req); err != nil {
		membershipFail(c, err)
		return
	}
	membershipWriteOK(c, membershipenums.MsgTierCreated)
}

func (h *Handle) UpdateTier(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form membershipdto.TierFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		membershipFormFail(c, membershipenums.Err(membershipenums.ErrInvalidParam))
		return
	}
	tierID, err := membershipInt(form.TierID)
	if err != nil {
		membershipFormFail(c, err)
		return
	}
	req := &membershipdto.UpdateTierReq{ProjectID: form.ProjectID, TierID: tierID}
	if strings.TrimSpace(form.Name) != "" {
		name := form.Name
		req.Name = &name
	}
	if strings.TrimSpace(form.Remark) != "" {
		remark := form.Remark
		req.Remark = &remark
	}
	if strings.TrimSpace(form.SortOrder) != "" {
		sortOrder := int(0)
		value, err := membershipInt(form.SortOrder)
		if err != nil {
			membershipFormFail(c, err)
			return
		}
		sortOrder = int(value)
		req.SortOrder = &sortOrder
	}
	if strings.TrimSpace(form.ThresholdYuan) != "" {
		threshold, err := membershipYuan(form.ThresholdYuan)
		if err != nil {
			membershipFormFail(c, err)
			return
		}
		req.ThresholdAmount = &threshold
	}
	if strings.TrimSpace(form.IsDefault) != "" {
		isDefault := true
		req.IsDefault = &isDefault
	}
	if _, err := h.svc.UpdateTier(c.Request.Context(), req); err != nil {
		membershipFail(c, err)
		return
	}
	membershipWriteOK(c, membershipenums.MsgTierUpdated)
}

func (h *Handle) DeleteTier(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form membershipdto.DeleteTierFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		membershipFormFail(c, membershipenums.Err(membershipenums.ErrInvalidParam))
		return
	}
	tierID, err := membershipInt(form.TierID)
	if err != nil {
		membershipFormFail(c, err)
		return
	}
	if err := h.svc.DeleteTier(c.Request.Context(), &membershipdto.DeleteTierReq{
		ProjectID: form.ProjectID, TierID: tierID,
	}); err != nil {
		membershipFail(c, err)
		return
	}
	membershipWriteOK(c, membershipenums.MsgTierDeleted)
}

func (h *Handle) SaveEntitlements(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form membershipdto.EntitlementFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		membershipFormFail(c, membershipenums.Err(membershipenums.ErrInvalidParam))
		return
	}
	tierID, err := membershipInt(form.TierID)
	if err != nil {
		membershipFormFail(c, err)
		return
	}
	req := &membershipdto.SaveEntitlementsReq{
		ProjectID: form.ProjectID,
		TierID:    tierID,
		Items:     membershipEntitlements(form.FreeShipping, form.DiscountPercent),
	}
	if _, err := h.svc.SaveEntitlements(c.Request.Context(), req); err != nil {
		membershipFail(c, err)
		return
	}
	membershipWriteOK(c, membershipenums.MsgEntitlementSaved)
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
	var form membershipdto.AssignFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		membershipFormFail(c, membershipenums.Err(membershipenums.ErrInvalidParam))
		return
	}
	userID, err := membershipUint(form.UserID, "userId")
	if err != nil {
		membershipFormFail(c, err)
		return
	}
	tierID, err := membershipInt(form.TierID)
	if err != nil {
		membershipFormFail(c, err)
		return
	}
	req := &membershipdto.AssignManualReq{ProjectID: form.ProjectID, UserID: userID, TierID: tierID}
	if _, err := h.svc.AssignManual(c.Request.Context(), req); err != nil {
		membershipFail(c, err)
		return
	}
	membershipWriteOK(c, membershipenums.MsgAssignSet)
}

func (h *Handle) AssignUnlock(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var form membershipdto.UnlockFormReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		membershipFormFail(c, membershipenums.Err(membershipenums.ErrInvalidParam))
		return
	}
	userID, err := membershipUint(form.UserID, "userId")
	if err != nil {
		membershipFormFail(c, err)
		return
	}
	req := &membershipdto.UnlockManualReq{ProjectID: form.ProjectID, UserID: userID}
	if err := h.svc.UnlockManual(c.Request.Context(), req); err != nil {
		membershipFail(c, err)
		return
	}
	membershipWriteOK(c, membershipenums.MsgAssignUnlocked)
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
	var form membershipdto.RecalcProjectReq
	if err := c.ShouldBindWith(&form, ginbinding.FormPost); err != nil {
		membershipFormFail(c, membershipenums.Err(membershipenums.ErrInvalidParam))
		return
	}
	res, err := h.svc.RecalcProject(c.Request.Context(), &form)
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
