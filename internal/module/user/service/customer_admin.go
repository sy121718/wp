package userservice

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	usermodel "go_wp/internal/module/user/model"
	"go_wp/pkg/facing"
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
	ids := customerUniqueIDs(req.IDs)
	disabled := make([]uint64, 0)
	if err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		locked, missing, lerr := s.lockCustomersTx(ctx, tx, ids)
		if lerr != nil {
			return lerr
		}
		if len(missing) > 0 {
			return newCustomerBatchError(ids, missing)
		}
		for i := range locked {
			if locked[i].Status == status {
				continue
			}
			if uerr := tx.WithContext(ctx).Model(&usermodel.UserEntity{}).
				Where("id = ?", locked[i].ID).Update("status", status).Error; uerr != nil {
				return uerr
			}
			if status == usermodel.UserStatusDisabled {
				disabled = append(disabled, locked[i].ID)
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
	ids := customerUniqueIDs(req.IDs)
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		locked, missing, lerr := s.lockCustomersTx(ctx, tx, ids)
		if lerr != nil {
			return lerr
		}
		if len(missing) > 0 {
			return newCustomerBatchError(ids, missing)
		}
		for i := range locked {
			if uerr := tx.WithContext(ctx).Model(&usermodel.UserEntity{}).Where("id = ?", locked[i].ID).
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

// customerUniqueIDs 去重并保持提交顺序；批量错误明细按这个顺序列出。
func customerUniqueIDs(raw []uint64) []uint64 {
	out := make([]uint64, 0, len(raw))
	seen := make(map[uint64]bool, len(raw))
	for _, id := range raw {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// lockCustomersTx 在调用方事务内按 id 升序锁定这批账号，返回锁定行与缺失 id。
//
// 锁序固定，避免两个批量以相反顺序取行锁形成死锁；缺失不在这里直接报错，
// 由调用方汇总成整批拒绝事实（逐条 id 进 data.items）。
func (s *Service) lockCustomersTx(ctx context.Context, tx *gorm.DB, ids []uint64) ([]usermodel.UserEntity, []uint64, error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}
	ordered := append([]uint64(nil), ids...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	var rows []usermodel.UserEntity
	if err := tx.WithContext(ctx).Model(&usermodel.UserEntity{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id IN ?", ordered).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	found := make(map[uint64]bool, len(rows))
	for i := range rows {
		found[rows[i].ID] = true
	}
	missing := make([]uint64, 0)
	for _, id := range ordered {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	return rows, missing, nil
}

// customerBatchError 是一次客户批量写被整体拒绝的事实：
// 主消息只给一句，逐条 id 与原因走共享失败出口的 data.items。
type customerBatchError struct {
	items []facing.BlockedItem
}

func (e *customerBatchError) Error() string { return userenums.ErrUserNotFound }

func (e *customerBatchError) Facing() (string, string) { return userenums.ErrUserNotFound, "" }

func (e *customerBatchError) HTTPStatus() int { return http.StatusNotFound }

func (e *customerBatchError) BlockedItems() []facing.BlockedItem { return e.items }

// newCustomerBatchError 把缺失 id 收成结构化拒绝明细。
func newCustomerBatchError(requested []uint64, missing []uint64) error {
	items := make([]facing.BlockedItem, 0, len(missing))
	for _, id := range missing {
		items = append(items, facing.BlockedItem{
			ID: strconv.FormatUint(id, 10), ReasonKey: userenums.ErrUserNotFound,
		})
	}
	if len(requested) == 1 && len(items) == 1 {
		return userenums.Err(userenums.ErrUserNotFound)
	}
	return &customerBatchError{items: items}
}
