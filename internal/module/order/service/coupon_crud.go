package orderservice

// coupon_crud.go — 优惠码后台管理与试算（BIZ-1）。
//
// 写入口只有两个（建 / 改），删除只允许在**没有核销记录**时进行：
// 有核销记录的券一旦删掉，那些记录就指向一张查不到的券，对账时分不清是数据坏了还是券被删了。
// 要停用请用 status=0（停用不删）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/database"
	"go_wp/pkg/utils"
)

// CreateCoupon 新建优惠码。
func (s *Service) CreateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (res *orderdto.CouponResp, err error) {
	if req == nil {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	startsAt, serr := parseCouponTime(req.StartsAt)
	if serr != nil {
		return nil, serr
	}
	endsAt, eerr := parseCouponTime(req.EndsAt)
	if eerr != nil {
		return nil, eerr
	}
	now := time.Now()
	e := &ordermodel.CouponEntity{
		ProjectID:     strings.TrimSpace(req.ProjectID),
		Code:          normalizeCouponCode(req.Code),
		Name:          strings.TrimSpace(req.Name),
		DiscountType:  strings.TrimSpace(req.DiscountType),
		DiscountValue: req.DiscountValue,
		MinSubtotal:   req.MinSubtotal,
		MaxUses:       req.MaxUses,
		PerUserLimit:  req.PerUserLimit,
		StartsAt:      startsAt,
		EndsAt:        endsAt,
		Status:        req.Status,
		Remark:        strings.TrimSpace(req.Remark),
		CreateBy:      req.OperatorID,
		UpdateBy:      req.OperatorID,
		CreateTime:    now,
		UpdateTime:    now,
	}
	if err = validateCouponRule(e); err != nil {
		return nil, err
	}
	existing, gerr := s.coupons.GetByCode(ctx, e.ProjectID, e.Code)
	if gerr != nil {
		return nil, gerr
	}
	if existing != nil {
		return nil, errors.New(orderenums.ErrCouponCodeTaken)
	}
	if cerr := s.coupons.Create(ctx, e); cerr != nil {
		// 并发下两个请求同时通过上面的存在性检查时，唯一索引会挡住第二个 ——
		// 对外文案与显式检查一致，不把数据库错误原文抛给运营。
		if database.IsUniqueViolation(cerr) {
			return nil, errors.New(orderenums.ErrCouponCodeTaken)
		}
		return nil, cerr
	}
	return toCouponResp(e, now), nil
}

// UpdateCoupon 修改优惠码。
//
// 券码**不可改**：改码等于换一张券，历史核销记录里的码会变成一个查不到的值。
// 需要新码就新建一张券、把旧的停用。
func (s *Service) UpdateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (res *orderdto.CouponResp, err error) {
	if req == nil || req.ID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	// 定位跳（DB-009 第四批）：请求只给 id；coupons 带 FORCE 策略，不带作用域的读取
	// 在非超级角色下返回 nil（表现为「优惠码不存在」）。归属由逐工程探测确定。
	projectID, perr := s.locateCouponProject(ctx, req.ID)
	if perr != nil {
		return nil, perr
	}
	existing, err := s.coupons.GetByID(ctx, projectID, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, errors.New(orderenums.ErrCouponNotFound)
	}
	startsAt, serr := parseCouponTime(req.StartsAt)
	if serr != nil {
		return nil, serr
	}
	endsAt, eerr := parseCouponTime(req.EndsAt)
	if eerr != nil {
		return nil, eerr
	}
	now := time.Now()
	// 在既有行上改字段后整体校验，保证「改完的券」本身自洽（而不是逐字段各校验一次）。
	next := *existing
	next.Name = strings.TrimSpace(req.Name)
	next.DiscountType = strings.TrimSpace(req.DiscountType)
	next.DiscountValue = req.DiscountValue
	next.MinSubtotal = req.MinSubtotal
	next.MaxUses = req.MaxUses
	next.PerUserLimit = req.PerUserLimit
	next.StartsAt = startsAt
	next.EndsAt = endsAt
	next.Status = req.Status
	next.Remark = strings.TrimSpace(req.Remark)
	if verr := validateCouponRule(&next); verr != nil {
		return nil, verr
	}
	fields := map[string]any{
		"name":           next.Name,
		"discount_type":  next.DiscountType,
		"discount_value": next.DiscountValue,
		"min_subtotal":   next.MinSubtotal,
		"max_uses":       next.MaxUses,
		"per_user_limit": next.PerUserLimit,
		"starts_at":      next.StartsAt,
		"ends_at":        next.EndsAt,
		"status":         next.Status,
		"remark":         next.Remark,
		"update_by":      req.OperatorID,
		"update_time":    now,
	}
	if uerr := s.coupons.UpdateFields(ctx, next.ProjectID, next.ID, fields); uerr != nil {
		return nil, uerr
	}
	updated, err := s.coupons.GetByID(ctx, next.ProjectID, next.ID)
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, errors.New(orderenums.ErrCouponNotFound)
	}
	return toCouponResp(updated, now), nil
}

// GetCoupon 按 id 取券。
func (s *Service) GetCoupon(ctx context.Context, couponID uint64) (res *orderdto.CouponResp, err error) {
	if couponID == 0 {
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	projectID, perr := s.locateCouponProject(ctx, couponID)
	if perr != nil {
		return nil, perr
	}
	e, err := s.coupons.GetByID(ctx, projectID, couponID)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errors.New(orderenums.ErrCouponNotFound)
	}
	return toCouponResp(e, time.Now()), nil
}

// ListCoupons 优惠码列表。
//
// status 这个查询参数是**展示口径**（enabled / disabled / expired / exhausted），
// 翻译成 model 认得的时间与次数条件后再下推 —— 让 model 认识「过期」这个词，
// 等于把业务语义摊进查询层。
func (s *Service) ListCoupons(ctx context.Context, req *orderdto.CouponListReq) (res *orderdto.CouponListResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	f := ordermodel.CouponFilter{
		ProjectID: req.ProjectID,
		Keyword:   req.Keyword,
		Offset:    req.Offset,
		Limit:     req.Limit,
	}
	enabled, disabled, expired, exhausted := ordermodel.CouponStatusEnabled, ordermodel.CouponStatusDisabled, true, true
	switch strings.ToLower(strings.TrimSpace(req.Status)) {
	case "":
	case "enabled":
		f.Status, f.Expired = &enabled, &expired
	case "disabled":
		f.Status = &disabled
	case "expired":
		f.Expired = &expired
	case "exhausted":
		f.Exhausted = &exhausted
	default:
		return nil, errors.New(orderenums.ErrInvalidParam)
	}
	list, total, lerr := s.coupons.List(ctx, f)
	if lerr != nil {
		return nil, lerr
	}
	now := time.Now()
	res = &orderdto.CouponListResp{List: make([]*orderdto.CouponResp, 0, len(list)), Total: total}
	for _, e := range list {
		res.List = append(res.List, toCouponResp(e, now))
	}
	return res, nil
}

// DeleteCoupon 删除优惠码（有核销记录的一律拒绝）。
func (s *Service) DeleteCoupon(ctx context.Context, couponID uint64) (err error) {
	if couponID == 0 {
		return errors.New(orderenums.ErrInvalidParam)
	}
	projectID, perr := s.locateCouponProject(ctx, couponID)
	if perr != nil {
		return perr
	}
	e, err := s.coupons.GetByID(ctx, projectID, couponID)
	if err != nil {
		return err
	}
	if e == nil {
		return errors.New(orderenums.ErrCouponNotFound)
	}
	used, cerr := s.coupons.CountRedemptions(ctx, e.ProjectID, couponID, nil)
	if cerr != nil {
		return cerr
	}
	if used > 0 {
		return errors.New(orderenums.ErrCouponInUse)
	}
	// 删除带工程作用域：越界删在换角色后会被策略拒绝，而不是删掉别的工程的券。
	return s.coupons.Delete(ctx, e.ProjectID, couponID)
}

// ListCouponRedemptions 核销记录列表。
func (s *Service) ListCouponRedemptions(ctx context.Context, req *orderdto.CouponRedemptionListReq) (res *orderdto.CouponRedemptionListResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	list, total, lerr := s.coupons.ListRedemptions(ctx, ordermodel.CouponRedemptionFilter{
		ProjectID: req.ProjectID,
		CouponID:  req.CouponID,
		Code:      normalizeCouponCode(req.Code),
		OrderID:   req.OrderID,
		Offset:    req.Offset,
		Limit:     req.Limit,
	})
	if lerr != nil {
		return nil, lerr
	}
	res = &orderdto.CouponRedemptionListResp{List: make([]*orderdto.CouponRedemptionResp, 0, len(list)), Total: total}
	for _, e := range list {
		res.List = append(res.List, &orderdto.CouponRedemptionResp{
			ID: e.ID, CouponID: e.CouponID, Code: e.Code,
			OrderID: e.OrderID, OrderNo: e.OrderNo,
			DiscountAmount: e.DiscountAmount,
			DiscountLabel:  fmt.Sprintf("%s 元", centsToYuanLabel(e.DiscountAmount)),
			UserID:         e.UserID,
			CreateTime:     utils.NewJSONTime(e.CreateTime),
		})
	}
	return res, nil
}

// ValidateCoupon 试算：纯读、不占次数。
//
// 不可用时**不返回 error**，而是在 res 里给 Usable=false + Message：
// 「这张券用不了」是正常业务结论（客服要能照着这句话回答客户），
// 只有参数本身不合法（缺工程、缺码）才是调用错误。
func (s *Service) ValidateCoupon(ctx context.Context, req *orderdto.CouponValidateReq) (res *orderdto.CouponValidateResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(orderenums.ErrProjectRequired)
	}
	code := normalizeCouponCode(req.Code)
	if code == "" {
		return nil, errors.New(orderenums.ErrCouponCodeRequired)
	}
	e, err := s.coupons.GetByCode(ctx, req.ProjectID, code)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, errors.New(orderenums.ErrCouponNotFound)
	}
	subtotal := req.Subtotal
	if subtotal < 0 {
		subtotal = 0
	}
	now := time.Now()
	res = &orderdto.CouponValidateResp{
		CouponID: e.ID, Code: e.Code, Name: e.Name,
		DiscountType: e.DiscountType, DiscountValue: e.DiscountValue,
		Subtotal: subtotal, Total: subtotal, TotalLabel: centsToYuanLabel(subtotal),
	}
	if reason := couponRuleCheck(e, subtotal, now); reason != "" {
		res.Message = reason
		return res, nil
	}
	if e.PerUserLimit > 0 && req.UserID != 0 {
		used, cerr := s.coupons.CountRedemptions(ctx, e.ProjectID, e.ID, &req.UserID)
		if cerr != nil {
			return nil, cerr
		}
		if used >= int64(e.PerUserLimit) {
			res.Message = orderenums.ErrCouponUserLimit
			return res, nil
		}
	}
	d := couponDiscount(e, subtotal)
	res.Usable = true
	res.DiscountAmount = d
	res.DiscountLabel = couponDiscountLabel(e)
	res.Total = subtotal - d
	res.TotalLabel = centsToYuanLabel(res.Total)
	res.Message = fmt.Sprintf("优惠码可用，已抵扣 %s 元", centsToYuanLabel(d))
	return res, nil
}

// toCouponResp 实体 → 视图（展示口径的字段在这里一次性算好）。
func toCouponResp(e *ordermodel.CouponEntity, now time.Time) *orderdto.CouponResp {
	if e == nil {
		return nil
	}
	// 时间窗的解释口径随券一起返回（审计 TX-011）：界面上必须能看出「填的是哪个时区」。
	return &orderdto.CouponResp{
		TimeZone: couponWindowLocation.String(),
		ID:       e.ID, ProjectID: e.ProjectID,
		Code: e.Code, Name: e.Name,
		DiscountType: e.DiscountType, DiscountValue: e.DiscountValue,
		DiscountLabel:    couponDiscountLabel(e),
		MinSubtotal:      e.MinSubtotal,
		MinSubtotalLabel: fmt.Sprintf("%s 元", centsToYuanLabel(e.MinSubtotal)),
		MaxUses:          e.MaxUses,
		UsedCount:        e.UsedCount,
		PerUserLimit:     e.PerUserLimit,
		StartsAt:         utils.NewJSONTimePtr(e.StartsAt),
		EndsAt:           utils.NewJSONTimePtr(e.EndsAt),
		Status:           e.Status,
		State:            couponState(e, now),
		StatusLabel:      couponStatusLabel(e, now),
		Remark:           e.Remark,
		CreateTime:       utils.NewJSONTime(e.CreateTime),
		UpdateTime:       utils.NewJSONTime(e.UpdateTime),
	}
}
