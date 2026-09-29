package userservice

// customer_admin.go — 后台客户管理（列表 / 详情 / 停用启用 / 解除锁定）。
//
// 三条原则：
//
//  1. **只返回展示要用的字段**：password / activation_key / 激活有效期留在 users 表里，
//     连 dto 都不进。少写一个字段，比事后审计「哪个响应带上了凭据」可靠得多。
//
//  2. **时间口径集中在这里**：dto 同时给 *time.Time 与展示文本，模板不必处理空指针，
//     也不会各处各写一种格式（同 userdto.SessionItem 的做法）。
//
//  3. **停用与锁定是两条轴**：status 表达管理状态（正常 / 停用），
//     locked_until_time + login_failure_count 表达「连续登录失败」。
//     解除锁定只清后者，改状态只动前者 —— 混在一起就会出现
//     「停用再启用之后，第一次输错密码就被锁」这种没人能解释的行为。

import (
	"context"
	"errors"
	"time"

	userdto "go_wp/internal/module/user/dto"
	userenums "go_wp/internal/module/user/enums"
	usermodel "go_wp/internal/module/user/model"
	"go_wp/pkg/utils"
)

// 客户列表的每页条数：默认与上限。
//
// 上限存在的原因不是性能，而是「一次拉 10000 个客户」这种事只可能来自脚本：
// 夹取上限让页面永远是页面，而脚本要去读数据库。
const (
	customerDefaultPageSize = 20
	customerMaxPageSize     = 100
)

// ListCustomers 客户列表：分页 + 关键词 / 状态 / 邮箱验证 / 注册时间范围筛选。
func (s *Service) ListCustomers(ctx context.Context, req *userdto.CustomerListReq) (res *userdto.CustomerListResp, err error) {
	if req == nil {
		return nil, errors.New(userenums.ErrInvalidParam)
	}
	if req.RegisteredFrom != nil && req.RegisteredTo != nil && req.RegisteredFrom.Time().After(req.RegisteredTo.Time()) {
		// 起止颠倒不静默交换：那会让「我明明是这么筛的」变成一个说不清的问题，
		// 而且交换后的结果与运营预期的往往相反（他以为筛的是 9 月，实际给了 10 月）。
		return nil, errors.New(userenums.ErrInvalidParam)
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
		Now:            now,
		Offset:         offset,
		Limit:          limit,
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

// GetCustomer 单个客户的资料与登录事实。
//
// 不含订单：订单是另一个模块的数据，页面经订单模块的只读契约单独取 ——
// 在这里 join 一次，就等于把「订单状态怎么算消费」的解释权搬进了用户模块。
func (s *Service) GetCustomer(ctx context.Context, customerID uint64) (res *userdto.CustomerResp, err error) {
	if customerID == 0 {
		return nil, errors.New(userenums.ErrInvalidParam)
	}
	e, gerr := s.m.GetByID(ctx, customerID)
	if gerr != nil || e == nil {
		// 已注销的账号与不存在的账号回同一句话：注销是客户自己的选择，
		// 后台不需要（也不应该）从这句话里区分出「他注销过」。
		return nil, errors.New(userenums.ErrUserNotFound)
	}
	return toCustomerResp(e, time.Now()), nil
}

// SetCustomerStatus 启用 / 停用账号。
//
// 只接受「正常」与「已停用」两个目标值（见 dto 注释）；重复设置同一个状态不写库 ——
// 那只会让 update_time 无意义地变化，而 update_time 是排查问题时要看的东西。
func (s *Service) SetCustomerStatus(ctx context.Context, req *userdto.CustomerStatusReq) (res *userdto.CustomerStatusResp, err error) {
	if req == nil || req.CustomerID == 0 {
		return nil, errors.New(userenums.ErrInvalidParam)
	}
	switch req.Status {
	case usermodel.UserStatusActive, usermodel.UserStatusDisabled:
	default:
		return nil, errors.New(userenums.ErrCustomerStatusInvalid)
	}
	e, gerr := s.m.GetByID(ctx, req.CustomerID)
	if gerr != nil || e == nil {
		return nil, errors.New(userenums.ErrUserNotFound)
	}
	if e.Status != req.Status {
		if err = s.m.SetStatus(ctx, req.CustomerID, req.Status); err != nil {
			return nil, err
		}
	}
	// StatusLabel 是**留给出口填的**展示文案（真源在 userenums.StatusLabel，key + 中文兜底）。
	//
	// 不在这一层填：service 拿不到请求语言，在这里拼中文的表现是英文站点的接口响应恒中文，
	// 而不会有任何报错。取词在出口 —— /api/customer/* 走 userhttp 的 localizeCustomerLabels，
	// 后台页按 Status 值给模板 (key, 兜底) 再取当前语言。
	return &userdto.CustomerStatusResp{
		CustomerID: req.CustomerID,
		Status:     req.Status,
	}, nil
}

// UnlockCustomer 解除登录锁定。
//
// 「锁定」按**当前时间**判定（locked_until_time 在未来）：那个时间点过去之后列里还留着值，
// 拿「非空」当锁定会让所有曾被锁过的账号永远解锁不掉 —— 点了按钮什么也不会发生。
//
// 顺带清掉残留的失败计数：刚错 3 次还没到锁定窗口的账号，那个计数会一直留着，
// 客户下次再错 2 次就被锁，而他完全不知道前面那 3 次来自哪里。
func (s *Service) UnlockCustomer(ctx context.Context, req *userdto.CustomerUnlockReq) (res *userdto.CustomerUnlockResp, err error) {
	if req == nil || req.CustomerID == 0 {
		return nil, errors.New(userenums.ErrInvalidParam)
	}
	e, gerr := s.m.GetByID(ctx, req.CustomerID)
	if gerr != nil || e == nil {
		return nil, errors.New(userenums.ErrUserNotFound)
	}
	now := time.Now()
	locked := e.LockedUntilTime != nil && e.LockedUntilTime.After(now)
	cleared := e.LoginFailureCount > 0
	res = &userdto.CustomerUnlockResp{CustomerID: req.CustomerID, Unlocked: locked, Cleared: cleared}
	if !locked && !cleared {
		// 无事可做就不写库：一次「什么都没改」的 UPDATE 会污染 update_time，
		// 让后来排查的人以为这个账号刚被谁动过。
		return res, nil
	}
	if err = s.m.ResetLoginFailure(ctx, req.CustomerID); err != nil {
		return nil, err
	}
	return res, nil
}

// customerEmailVerifiedFilter 展示层三态 → model 层三态。
//
// 两边取值不同是刻意的：model 的零值必须是「不过滤」（有别的调用方会漏设这个字段），
// 而 dto 的零值同样必须是「不过滤」—— 直接透传会让「只看未验证」与「都看」撞在一起。
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

// toCustomerResp 实体 → 视图（**只挑展示字段，凭据一律不搬运**）。
//
// StatusLabel 是**留给出口填的**展示文案（真源在 userenums.StatusLabel，key + 中文兜底）：
// 后台页按 Status 值自行取 (key, 兜底) 交给模板取词，/api/customer/* 由 handler 就地取词 ——
// 这一层只给状态取值，不拼文案（service 拿不到请求语言）。
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
