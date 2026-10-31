package orderservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/database"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

func (s *Service) CreateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (res *orderdto.CouponResp, err error) {
	if req == nil {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
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
		return nil, orderenums.Err(orderenums.ErrCouponCodeTaken)
	}
	if cerr := s.coupons.Create(ctx, e); cerr != nil {

		if database.IsUniqueViolation(cerr) {
			return nil, orderenums.Err(orderenums.ErrCouponCodeTaken)
		}
		return nil, cerr
	}
	return toCouponResp(e, now), nil
}

func (s *Service) UpdateCoupon(ctx context.Context, req *orderdto.CouponSaveReq) (res *orderdto.CouponResp, err error) {
	if req == nil || req.ID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}

	projectID, perr := s.locateCouponProject(ctx, req.ID)
	if perr != nil {
		return nil, perr
	}
	existing, err := s.coupons.GetByID(ctx, projectID, req.ID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, orderenums.Err(orderenums.ErrCouponNotFound)
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
		return nil, orderenums.Err(orderenums.ErrCouponNotFound)
	}
	return toCouponResp(updated, now), nil
}

func (s *Service) GetCoupon(ctx context.Context, couponID uint64) (res *orderdto.CouponResp, err error) {
	if couponID == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
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
		return nil, orderenums.Err(orderenums.ErrCouponNotFound)
	}
	return toCouponResp(e, time.Now()), nil
}

func (s *Service) ListCoupons(ctx context.Context, req *orderdto.CouponListReq) (res *orderdto.CouponListResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
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
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
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

func (s *Service) DeleteCoupon(ctx context.Context, couponID uint64) (err error) {
	if couponID == 0 {
		return orderenums.Err(orderenums.ErrInvalidParam)
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
		return orderenums.Err(orderenums.ErrCouponNotFound)
	}
	used, cerr := s.coupons.CountRedemptions(ctx, e.ProjectID, couponID, nil)
	if cerr != nil {
		return cerr
	}
	if used > 0 {
		return orderenums.Err(orderenums.ErrCouponInUse)
	}

	return s.coupons.Delete(ctx, e.ProjectID, couponID)
}

func (s *Service) ListCouponRedemptions(ctx context.Context, req *orderdto.CouponRedemptionListReq) (res *orderdto.CouponRedemptionListResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
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

func (s *Service) ValidateCoupon(ctx context.Context, req *orderdto.CouponValidateReq) (res *orderdto.CouponValidateResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, orderenums.Err(orderenums.ErrProjectRequired)
	}
	code := normalizeCouponCode(req.Code)
	if code == "" {
		return nil, orderenums.Err(orderenums.ErrCouponCodeRequired)
	}
	e, err := s.coupons.GetByCode(ctx, req.ProjectID, code)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, orderenums.Err(orderenums.ErrCouponNotFound)
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

func toCouponResp(e *ordermodel.CouponEntity, now time.Time) *orderdto.CouponResp {
	if e == nil {
		return nil
	}

	state := couponState(e, now)
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
		State:            state,
		StateTone:        orderenums.CouponStateTone(state),
		StatusLabel:      couponStatusLabel(e, now),
		Remark:           e.Remark,
		CreateTime:       utils.NewJSONTime(e.CreateTime),
		UpdateTime:       utils.NewJSONTime(e.UpdateTime),
	}
}

const (
	maxCouponCodeLen = 64

	maxCouponNameLen = 120

	maxCouponRemarkLen = 255
)

func normalizeCouponCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

var couponTimeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

var couponWindowLocation = time.UTC

func parseCouponTime(raw string) (*time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	for _, layout := range couponTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, couponWindowLocation); err == nil {

			u := t.UTC()
			return &u, nil
		}
	}
	return nil, orderenums.Err(orderenums.ErrCouponWindowInvalid)
}

func validateCouponRule(e *ordermodel.CouponEntity) error {
	if strings.TrimSpace(e.ProjectID) == "" {
		return orderenums.Err(orderenums.ErrProjectRequired)
	}
	if e.Code == "" {
		return orderenums.Err(orderenums.ErrCouponCodeRequired)
	}
	if len(e.Code) > maxCouponCodeLen || len(e.Name) > maxCouponNameLen || len(e.Remark) > maxCouponRemarkLen {
		return orderenums.Err(orderenums.ErrInvalidParam)
	}
	switch e.DiscountType {
	case ordermodel.CouponTypePercent:
		if e.DiscountValue <= 0 || e.DiscountValue > 100 {
			return orderenums.Err(orderenums.ErrCouponValueInvalid)
		}
	case ordermodel.CouponTypeFixed:
		if e.DiscountValue <= 0 {
			return orderenums.Err(orderenums.ErrCouponValueInvalid)
		}
	default:
		return orderenums.Err(orderenums.ErrCouponTypeInvalid)
	}
	if e.MinSubtotal < 0 {
		return orderenums.Err(orderenums.ErrCouponValueInvalid)
	}
	if e.MaxUses < 0 || e.PerUserLimit < 0 {
		return orderenums.Err(orderenums.ErrCouponValueInvalid)
	}
	if e.StartsAt != nil && e.EndsAt != nil && !e.EndsAt.After(*e.StartsAt) {

		return orderenums.Err(orderenums.ErrCouponWindowInvalid)
	}
	if e.Status != ordermodel.CouponStatusEnabled && e.Status != ordermodel.CouponStatusDisabled {
		return orderenums.Err(orderenums.ErrInvalidParam)
	}
	return nil
}

func couponDiscount(e *ordermodel.CouponEntity, subtotal int64) int64 {
	if e == nil || subtotal <= 0 {
		return 0
	}
	var d int64
	switch e.DiscountType {
	case ordermodel.CouponTypePercent:
		d = subtotal * e.DiscountValue / 100
	case ordermodel.CouponTypeFixed:
		d = e.DiscountValue
	default:
		return 0
	}
	if d > subtotal {

		d = subtotal
	}
	if d < 0 {
		d = 0
	}
	return d
}

func couponRuleCheck(e *ordermodel.CouponEntity, subtotal int64, now time.Time) string {
	if e == nil {
		return orderenums.ErrCouponNotFound
	}
	switch {
	case e.Status != ordermodel.CouponStatusEnabled:
		return orderenums.ErrCouponDisabled
	case e.StartsAt != nil && now.Before(*e.StartsAt):
		return orderenums.ErrCouponNotStarted
	case e.EndsAt != nil && now.After(*e.EndsAt):
		return orderenums.ErrCouponExpired
	case e.MaxUses > 0 && e.UsedCount >= e.MaxUses:
		return orderenums.ErrCouponExhausted
	case subtotal < e.MinSubtotal:
		return orderenums.ErrCouponMinSubtotal
	}
	return ""
}

func couponDiscountLabel(e *ordermodel.CouponEntity) string {
	if e == nil {
		return ""
	}
	if e.DiscountType == ordermodel.CouponTypePercent {
		return fmt.Sprintf("减 %d%%", e.DiscountValue)
	}
	return fmt.Sprintf("减 %s 元", centsToYuanLabel(e.DiscountValue))
}

func couponState(e *ordermodel.CouponEntity, now time.Time) string {
	if e == nil {
		return ""
	}
	switch {
	case e.Status != ordermodel.CouponStatusEnabled:
		return orderenums.CouponStateDisabled
	case e.EndsAt != nil && now.After(*e.EndsAt):
		return orderenums.CouponStateExpired
	case e.StartsAt != nil && now.Before(*e.StartsAt):
		return orderenums.CouponStateNotStarted
	case e.MaxUses > 0 && e.UsedCount >= e.MaxUses:
		return orderenums.CouponStateExhausted
	}
	return orderenums.CouponStateEnabled
}

func couponStatusLabel(e *ordermodel.CouponEntity, now time.Time) string {
	_, fallback := orderenums.CouponStateLabel(couponState(e, now))
	return fallback
}

func usageLabel(used, max int) string {
	if max <= 0 {
		return fmt.Sprintf("%d / 不限", used)
	}
	return fmt.Sprintf("%d / %d", used, max)
}

func (s *Service) redeemCouponTx(ctx context.Context, tx *gorm.DB, e *ordermodel.CouponEntity, orderID uint64, orderNo string, discount int64, userID *uint64, now time.Time) (err error) {
	if e == nil {
		return nil
	}
	locked, lerr := s.coupons.LockByIDTx(ctx, tx, e.ProjectID, e.ID)
	if lerr != nil {
		return lerr
	}
	if locked == nil {
		return orderenums.Err(orderenums.ErrCouponNotFound)
	}
	e = locked
	if e.PerUserLimit > 0 && userID != nil {
		used, cerr := s.coupons.CountRedemptionsTx(ctx, tx, e.ProjectID, e.ID, userID)
		if cerr != nil {
			return cerr
		}
		if used >= int64(e.PerUserLimit) {
			return orderenums.Err(orderenums.ErrCouponUserLimit)
		}
	}
	inserted, ierr := s.coupons.InsertRedemptionTx(ctx, tx, &ordermodel.CouponRedemptionEntity{
		CouponID:       e.ID,
		ProjectID:      e.ProjectID,
		Code:           e.Code,
		OrderID:        orderID,
		OrderNo:        orderNo,
		DiscountAmount: discount,
		UserID:         userID,
		CreateTime:     now,
	})
	if ierr != nil {
		return ierr
	}
	if !inserted {

		return nil
	}
	ok, uerr := s.coupons.IncrementUsedTx(ctx, tx, e.ID)
	if uerr != nil {
		return uerr
	}
	if !ok {

		return orderenums.Err(orderenums.ErrCouponExhausted)
	}
	return nil
}

func (s *Service) auditProjectIDs(ctx context.Context, explicit string) ([]string, error) {
	if pid := strings.TrimSpace(explicit); pid != "" {
		return []string{pid}, nil
	}
	return s.projectIDs(ctx)
}

func (s *Service) AuditCouponCounts(ctx context.Context, req *orderdto.CouponCountAuditReq) (res *orderdto.CouponCountAuditResp, err error) {
	if s == nil || s.coupons == nil {
		return &orderdto.CouponCountAuditResp{Items: []orderdto.CouponCountMismatch{}}, nil
	}
	projectID := ""
	limit := 100
	if req != nil {
		projectID = strings.TrimSpace(req.ProjectID)
		if req.Limit > 0 {
			limit = req.Limit
		}
	}

	projects, perr := s.auditProjectIDs(ctx, projectID)
	if perr != nil {
		return nil, perr
	}
	res = &orderdto.CouponCountAuditResp{Items: make([]orderdto.CouponCountMismatch, 0)}
	for _, pid := range projects {
		if ctx.Err() != nil {
			break
		}
		checked, cerr := s.coupons.CountCoupons(ctx, pid)
		if cerr != nil {
			return nil, cerr
		}

		rows, lerr := s.coupons.ListCountMismatches(ctx, pid, limit)
		if lerr != nil {
			return nil, lerr
		}
		res.Checked += int(checked)
		res.Mismatched += len(rows)
		for _, row := range rows {
			res.Items = append(res.Items, orderdto.CouponCountMismatch{
				CouponID: row.CouponID, ProjectID: row.ProjectID, Code: row.Code,
				UsedCount: row.UsedCount, ActualCount: row.ActualCount,
				Diff: row.UsedCount - row.ActualCount,
			})
		}
	}
	if len(res.Items) > 0 {

		logger.Scene("order").With("mismatched", len(res.Items)).With("checked", res.Checked).
			Warn("券计数与核销明细不一致（只报告，不自动修正）")
	}
	return res, nil
}
