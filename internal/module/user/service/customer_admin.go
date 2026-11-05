package userservice

import (
	"context"
	"gorm.io/gorm"
	"time"

	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	usermodel "go_wp/internal/module/user/model"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

const (
	customerDefaultPageSize = 20
	customerMaxPageSize     = 100
)

func (s *Service) ListCustomers(ctx context.Context, req *userdto.CustomerListReq) (res *userdto.CustomerListResp, err error) {
	if req == nil {
		return nil, userenums.Err(userenums.ErrInvalidParam)
	}
	if req.RegisteredFrom != nil && req.RegisteredTo != nil && req.RegisteredFrom.Time().After(req.RegisteredTo.Time()) {

		return nil, userenums.Err(userenums.ErrInvalidParam)
	}
	limit, offset := utils.NormalizeLimitOffset(req.Limit, req.Offset, customerDefaultPageSize, customerMaxPageSize)

	now := time.Now()
	list, total, lerr := s.m.List(ctx, usermodel.UserFilter{
		Keyword:        req.Keyword,
		Status:         req.Status,
		EmailVerified:  customerEmailVerifiedFilter(req.EmailVerified),
		RegisteredFrom: req.RegisteredFrom.TimePtr(),
		RegisteredTo:   req.RegisteredTo.TimePtr(),
		LockedOnly:     req.LockedOnly,

		UserIDs: req.UserIDs,
		Now:     now,
		Offset:  offset,
		Limit:   limit,
	})
	if lerr != nil {
		return nil, lerr
	}
	counters, cerr := s.m.CountCustomers(ctx, now)
	if cerr != nil {
		return nil, cerr
	}
	res = &userdto.CustomerListResp{
		List:  make([]*userdto.CustomerResp, 0, len(list)),
		Total: total,
		Counters: userdto.CustomerCounters{
			Total:      counters.Total,
			Active:     counters.Active,
			Disabled:   counters.Disabled,
			Pending:    counters.Pending,
			Locked:     counters.Locked,
			Verified:   counters.Verified,
			Unverified: counters.Unverified,
		},
	}
	for _, e := range list {
		res.List = append(res.List, toCustomerResp(e, now))
	}
	return res, nil
}

func (s *Service) GetCustomer(ctx context.Context, customerID uint64) (res *userdto.CustomerResp, err error) {
	if customerID == 0 {
		return nil, userenums.Err(userenums.ErrInvalidParam)
	}
	e, gerr := s.m.GetByID(ctx, customerID)
	if gerr != nil || e == nil {

		return nil, userenums.Err(userenums.ErrUserNotFound)
	}
	return toCustomerResp(e, time.Now()), nil
}

func (s *Service) SetCustomerStatus(ctx context.Context, req *userdto.CustomerStatusReq) (res *userdto.CustomerStatusResp, err error) {
	if req == nil || req.CustomerID == 0 {
		return nil, userenums.Err(userenums.ErrInvalidParam)
	}
	switch req.Status {
	case usermodel.UserStatusActive, usermodel.UserStatusDisabled:
	default:
		return nil, userenums.Err(userenums.ErrCustomerStatusInvalid)
	}
	e, gerr := s.m.GetByID(ctx, req.CustomerID)
	if gerr != nil || e == nil {
		return nil, userenums.Err(userenums.ErrUserNotFound)
	}
	if e.Status != req.Status {
		if err = s.m.SetStatus(ctx, req.CustomerID, req.Status); err != nil {
			return nil, err
		}

		if req.Status == usermodel.UserStatusDisabled {

			if rerr := s.RevokeAllSessions(ctx, req.CustomerID); rerr != nil {
				logger.Scene("user").With("user_id", req.CustomerID).Error(rerr,
					"停用账号后撤销会话失败（状态已更新，需人工清理残留会话）")
			}
		}
	}

	return &userdto.CustomerStatusResp{
		CustomerID: req.CustomerID,
		Status:     req.Status,
	}, nil
}

func (s *Service) UnlockCustomer(ctx context.Context, req *userdto.CustomerUnlockReq) (res *userdto.CustomerUnlockResp, err error) {
	if req == nil || req.CustomerID == 0 {
		return nil, userenums.Err(userenums.ErrInvalidParam)
	}
	e, gerr := s.m.GetByID(ctx, req.CustomerID)
	if gerr != nil || e == nil {
		return nil, userenums.Err(userenums.ErrUserNotFound)
	}
	now := time.Now()
	locked := e.LockedUntilTime != nil && e.LockedUntilTime.After(now)
	cleared := e.LoginFailureCount > 0
	res = &userdto.CustomerUnlockResp{CustomerID: req.CustomerID, Unlocked: locked, Cleared: cleared}
	if !locked && !cleared {

		return res, nil
	}
	if err = s.m.ResetLoginFailure(ctx, req.CustomerID); err != nil {
		return nil, err
	}
	return res, nil
}

func customerEmailVerifiedFilter(v int) int {
	switch v {
	case userdto.EmailVerifiedYes:
		return usermodel.EmailVerifiedOnly
	case userdto.EmailVerifiedNo:
		return usermodel.EmailVerifiedNone
	default:
		return usermodel.EmailVerifiedAny
	}
}

func toCustomerResp(e *usermodel.UserEntity, now time.Time) *userdto.CustomerResp {
	r := &userdto.CustomerResp{
		ID:                e.ID,
		Username:          e.Username,
		Email:             e.Email,
		Nickname:          deref(e.Nickname),
		DisplayName:       deref(e.DisplayName),
		Avatar:            deref(e.Avatar),
		Status:            e.Status,
		EmailVerified:     e.EmailVerifiedAt != nil,
		RegisteredAt:      utils.NewJSONTimePtr(e.RegisteredAt),
		RegisteredAtText:  formatTime(e.RegisteredAt),
		RegisterIP:        deref(e.RegisterIP),
		RegisterLocation:  deref(e.RegisterLocation),
		LastLoginTime:     utils.NewJSONTimePtr(e.LastLoginTime),
		LastLoginTimeText: formatTime(e.LastLoginTime),
		LastLoginIP:       deref(e.LastLoginIP),
		LastLoginLocation: deref(e.LastLoginLocation),
		LoginFailureCount: e.LoginFailureCount,
	}
	if e.LockedUntilTime != nil && e.LockedUntilTime.After(now) {
		r.Locked = true
		r.LockedUntilTime = utils.NewJSONTimePtr(e.LockedUntilTime)
		r.LockedUntilText = formatTime(e.LockedUntilTime)
	}
	return r
}

// BulkSetCustomerStatus 在一个事务里批量改账号状态：任一失败整批回滚。
//
// 会话撤销是事务外的副作用，只在事务提交成功后执行；回滚不会留下半批状态。
func (s *Service) BulkSetCustomerStatus(ctx context.Context, req *userdto.CustomerBulkStatusReq) (err error) {
	if req == nil || len(req.IDs) == 0 || req.Status == nil {
		return userenums.Err(userenums.ErrInvalidParam)
	}
	status := *req.Status
	switch status {
	case usermodel.UserStatusActive, usermodel.UserStatusDisabled:
	default:
		return userenums.Err(userenums.ErrCustomerStatusInvalid)
	}
	disabled := make([]uint64, 0)
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		for _, id := range req.IDs {
			var e usermodel.UserEntity
			if gerr := tx.WithContext(ctx).Where("id = ?", id).First(&e).Error; gerr != nil {
				return userenums.Err(userenums.ErrUserNotFound)
			}
			if e.Status == status {
				continue
			}
			if uerr := tx.WithContext(ctx).Model(&usermodel.UserEntity{}).
				Where("id = ?", id).Update("status", status).Error; uerr != nil {
				return uerr
			}
			if status == usermodel.UserStatusDisabled {
				disabled = append(disabled, id)
			}
		}
		return nil
	}); err != nil {
		return err
	}
	for _, id := range disabled {
		if rerr := s.RevokeAllSessions(ctx, id); rerr != nil {
			logger.Scene("user").With("user_id", id).Error(rerr,
				"停用账号后撤销会话失败（状态已更新，需人工清理残留会话）")
		}
	}
	return nil
}

// BulkUnlockCustomers 在一个事务里批量解除锁定并清零失败计数。
func (s *Service) BulkUnlockCustomers(ctx context.Context, req *userdto.CustomerBulkUnlockReq) error {
	if req == nil || len(req.IDs) == 0 {
		return userenums.Err(userenums.ErrInvalidParam)
	}
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		for _, id := range req.IDs {
			var e usermodel.UserEntity
			if gerr := tx.WithContext(ctx).Where("id = ?", id).First(&e).Error; gerr != nil {
				return userenums.Err(userenums.ErrUserNotFound)
			}
			if uerr := tx.WithContext(ctx).Model(&usermodel.UserEntity{}).Where("id = ?", id).
				Updates(map[string]any{
					"login_failure_count": 0,
					"locked_until_time":   nil,
					"last_failure_time":   nil,
				}).Error; uerr != nil {
				return uerr
			}
		}
		return nil
	})
}
