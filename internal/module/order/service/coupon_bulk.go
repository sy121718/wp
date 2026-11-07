package orderservice

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
)

// couponBlockedError 是一次优惠码批量写被整体拒绝时的事实：
// 主消息只取一句公共文案（单条请求取该条的具体业务原因），
// 逐条 id / 名称 / 原因交给共享失败出口按 BlockedItems 输出。
type couponBlockedError struct {
	key   string
	items []facing.BlockedItem
}

func (e *couponBlockedError) Error() string { return e.key }

func (e *couponBlockedError) Facing() (string, string) { return e.key, "" }

func (e *couponBlockedError) HTTPStatus() int { return couponBlockedStatus(e.items) }

func (e *couponBlockedError) BlockedItems() []facing.BlockedItem { return e.items }

// couponReject 构造整批拒绝事实：只有一个目标且只有它失败时给该条具体原因，
// 其余情况给公共整批文案；状态按原因区分冲突 409 / 缺失 404 / 参数 400。
func couponReject(requested int, items []facing.BlockedItem) error {
	if len(items) == 0 {
		return nil
	}
	key := orderenums.ErrBulkRejected
	if requested == 1 && len(items) == 1 {
		key = items[0].ReasonKey
	}
	return &couponBlockedError{key: key, items: items}
}

// couponBlockedStatus 从拒绝原因推导 HTTP 状态：冲突优先，其次缺失，其余按参数错误。
func couponBlockedStatus(items []facing.BlockedItem) int {
	status := http.StatusBadRequest
	for _, item := range items {
		switch item.ReasonKey {
		case orderenums.ErrCouponInUse:
			return http.StatusConflict
		case orderenums.ErrCouponNotFound:
			status = http.StatusNotFound
		}
	}
	return status
}

func couponBlockedItem(id uint64, name, reasonKey string) facing.BlockedItem {
	return facing.BlockedItem{
		ID: strconv.FormatUint(id, 10), Name: strings.TrimSpace(name), ReasonKey: reasonKey,
	}
}

// couponRequestIDs 把表单里的 id 串归一化成整数：
// 按数值身份去重（"1" 与 "01" 是同一张券），再按数值升序稳定排序；
// 空项与非纯数字项不静默跳过，整批按参数错误拒绝。
func couponRequestIDs(raw []string) ([]uint64, error) {
	if len(raw) == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	seen := make(map[uint64]bool, len(raw))
	invalid := make([]facing.BlockedItem, 0)
	ids := make([]uint64, 0, len(raw))
	for _, item := range raw {
		text := strings.TrimSpace(item)
		if !isDigits(text) {
			invalid = append(invalid, facing.BlockedItem{ID: text, ReasonKey: orderenums.ErrInvalidParam})
			continue
		}
		id, err := strconv.ParseUint(text, 10, 64)
		if err != nil || id == 0 {
			invalid = append(invalid, facing.BlockedItem{ID: text, ReasonKey: orderenums.ErrInvalidParam})
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(invalid) > 0 {
		return nil, couponReject(len(raw), invalid)
	}
	if len(ids) == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	if len(ids) > bulkids.MaxIDs {
		return nil, &bulkids.IDsError{Count: len(ids), Max: bulkids.MaxIDs}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

// isDigits 只接受非空的全数字串：拒绝 "+1" / "-1" / "1e3" 这类解析歧义写法。
func isDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// couponTarget 是待写券的最小事实：工程用于 RLS 作用域与锁定范围。
type couponTarget struct {
	projectID string
	id        uint64
}

// resolveCouponTargets 定位每个 id 所属工程，并把定位阶段就不存在的 id 交回调用方。
// 显式给工程时整批按该工程匹配（越界项在事务内锁不到，按缺失拒绝）；
// 未给工程时逐工程探测归属，不退化成一个不限工程的作用域。
// 缺失不在这里提前返回：已存在的目标仍要在同一事务里完成冲突检查，
// 让「缺失 + 已核销」这类混合拒绝按冲突优先给出真实状态。
func (s *Service) resolveCouponTargets(ctx context.Context, ids []uint64, explicit string) ([]couponTarget, []facing.BlockedItem, error) {
	if pid := strings.TrimSpace(explicit); pid != "" {
		// 显式工程必须整批匹配；非法工程 id 是参数错误，不能退化成不限工程。
		if _, err := uuid.Parse(pid); err != nil {
			return nil, nil, orderenums.Err(orderenums.ErrInvalidParam)
		}
		targets := make([]couponTarget, 0, len(ids))
		for _, id := range ids {
			targets = append(targets, couponTarget{projectID: pid, id: id})
		}
		return targets, nil, nil
	}
	projects, err := s.projectIDs(ctx)
	if err != nil {
		return nil, nil, err
	}
	located := make(map[uint64]string, len(ids))
	for _, pid := range projects {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		for _, id := range ids {
			if _, ok := located[id]; ok {
				continue
			}
			e, gerr := s.coupons.GetByID(ctx, pid, id)
			if gerr != nil {
				return nil, nil, gerr
			}
			if e != nil {
				located[id] = e.ProjectID
			}
		}
	}
	missing := make([]facing.BlockedItem, 0)
	targets := make([]couponTarget, 0, len(ids))
	for _, id := range ids {
		pid, ok := located[id]
		if !ok {
			missing = append(missing, couponBlockedItem(id, "", orderenums.ErrCouponNotFound))
			continue
		}
		targets = append(targets, couponTarget{projectID: pid, id: id})
	}
	return targets, missing, nil
}

// groupCouponTargets 按工程分组并返回稳定排序的工程序列；
// 工程内 id 的锁序由 LockByIDsTx 的 id 升序统一保证。
func groupCouponTargets(targets []couponTarget) (map[string][]uint64, []string) {
	groups := make(map[string][]uint64, len(targets))
	order := make([]string, 0, len(targets))
	for _, t := range targets {
		if _, ok := groups[t.projectID]; !ok {
			order = append(order, t.projectID)
		}
		groups[t.projectID] = append(groups[t.projectID], t.id)
	}
	sort.Strings(order)
	return groups, order
}

// DeleteCoupons 是批量删除优惠码的明确入口：整批同一事务，
// 与核销共用券行锁，删前在事务内复核存在性与核销计数，任一失败整批回滚。
func (s *Service) DeleteCoupons(ctx context.Context, req *orderdto.CouponBulkDeleteReq) (deleted int64, err error) {
	if req == nil {
		return 0, orderenums.Err(orderenums.ErrInvalidParam)
	}
	ids, ierr := couponRequestIDs(req.IDs)
	if ierr != nil {
		return 0, ierr
	}
	targets, missing, terr := s.resolveCouponTargets(ctx, ids, req.ProjectID)
	if terr != nil {
		return 0, terr
	}
	return s.deleteCouponTargetsTx(ctx, targets, missing, len(ids))
}

// deleteCouponTargetsTx 把所有目标放进同一个事务：
// 先按工程稳定排序逐段锁行并复核存在性，再在同一事务内查核销并删除；
// 任一失败整批回滚，调用方拿到 0。
func (s *Service) deleteCouponTargetsTx(ctx context.Context, targets []couponTarget,
	missing []facing.BlockedItem, requested int) (deleted int64, err error) {
	if len(targets) == 0 {
		if len(missing) > 0 {
			return 0, couponReject(requested, missing)
		}
		return 0, orderenums.Err(orderenums.ErrInvalidParam)
	}
	groups, order := groupCouponTargets(targets)
	var wrote int64
	err = s.coupons.Transaction(ctx, func(tx *gorm.DB) error {
		lockedByProject, lockMissing, lerr := s.lockCouponTargetsTx(ctx, tx, groups, order)
		if lerr != nil {
			return lerr
		}
		// 计数与删除同事务：券行已锁定，核销写入口同样先锁这张券，
		// 因此「删前计数 + 删除」不会与并发核销交错出孤儿记录。
		// 缺失与已核销一起收集后再拒绝，冲突优先于缺失。
		blocked := append([]facing.BlockedItem{}, missing...)
		blocked = append(blocked, lockMissing...)
		for _, pid := range order {
			for _, id := range groups[pid] {
				locked, ok := lockedByProject[pid][id]
				if !ok {
					continue
				}
				used, cerr := s.coupons.CountRedemptionsTx(ctx, tx, pid, id, nil)
				if cerr != nil {
					return cerr
				}
				if used > 0 {
					blocked = append(blocked, couponBlockedItem(id, locked.Name, orderenums.ErrCouponInUse))
				}
			}
		}
		if len(blocked) > 0 {
			return couponReject(requested, blocked)
		}
		for _, pid := range order {
			for _, id := range groups[pid] {
				affected, derr := s.coupons.DeleteTx(ctx, tx, pid, id)
				if derr != nil {
					return derr
				}
				if affected != 1 {
					return couponReject(requested,
						[]facing.BlockedItem{couponBlockedItem(id, "", orderenums.ErrCouponNotFound)})
				}
				wrote++
			}
		}
		return nil
	})
	if err != nil {
		// 回滚后不得把已执行的删除数交出去。
		return 0, err
	}
	return wrote, nil
}

// lockCouponTargetsTx 按工程稳定排序、工程内 id 升序锁行，并交回锁到时已消失的 id。
func (s *Service) lockCouponTargetsTx(ctx context.Context, tx *gorm.DB,
	groups map[string][]uint64, order []string) (map[string]map[uint64]*ordermodel.CouponEntity, []facing.BlockedItem, error) {
	lockedByProject := make(map[string]map[uint64]*ordermodel.CouponEntity, len(order))
	missing := make([]facing.BlockedItem, 0)
	for _, pid := range order {
		locked, lerr := s.coupons.LockByIDsTx(ctx, tx, pid, groups[pid])
		if lerr != nil {
			return nil, nil, lerr
		}
		found := make(map[uint64]*ordermodel.CouponEntity, len(locked))
		for _, e := range locked {
			found[e.ID] = e
		}
		for _, id := range groups[pid] {
			if _, ok := found[id]; !ok {
				missing = append(missing, couponBlockedItem(id, "", orderenums.ErrCouponNotFound))
			}
		}
		lockedByProject[pid] = found
	}
	return lockedByProject, missing, nil
}

// SetCouponsStatus 是批量启停的明确入口：只写 status（与审计列），
// 不整行覆盖名称、门槛、时间窗等其它字段；整批同一事务。
func (s *Service) SetCouponsStatus(ctx context.Context, req *orderdto.CouponBulkStatusReq) (updated int64, err error) {
	if req == nil || (req.Status != ordermodel.CouponStatusEnabled && req.Status != ordermodel.CouponStatusDisabled) {
		return 0, orderenums.Err(orderenums.ErrInvalidParam)
	}
	ids, ierr := couponRequestIDs(req.IDs)
	if ierr != nil {
		return 0, ierr
	}
	targets, missing, terr := s.resolveCouponTargets(ctx, ids, req.ProjectID)
	if terr != nil {
		return 0, terr
	}
	return s.setCouponTargetsStatusTx(ctx, targets, missing, len(ids), req.Status, req.OperatorID)
}

func (s *Service) setCouponTargetsStatusTx(ctx context.Context, targets []couponTarget,
	missing []facing.BlockedItem, requested, status int, operatorID uint64) (updated int64, err error) {
	if len(targets) == 0 {
		if len(missing) > 0 {
			return 0, couponReject(requested, missing)
		}
		return 0, orderenums.Err(orderenums.ErrInvalidParam)
	}
	groups, order := groupCouponTargets(targets)
	now := time.Now()
	var wrote int64
	err = s.coupons.Transaction(ctx, func(tx *gorm.DB) error {
		_, lockMissing, lerr := s.lockCouponTargetsTx(ctx, tx, groups, order)
		if lerr != nil {
			return lerr
		}
		if len(lockMissing) > 0 || len(missing) > 0 {
			blocked := append(append([]facing.BlockedItem{}, missing...), lockMissing...)
			return couponReject(requested, blocked)
		}
		for _, pid := range order {
			ids := groups[pid]
			affected, uerr := s.coupons.UpdateStatusTx(ctx, tx, pid, ids, status, operatorID, now)
			if uerr != nil {
				return uerr
			}
			if affected != int64(len(ids)) {
				return couponReject(requested,
					[]facing.BlockedItem{couponBlockedItem(ids[0], "", orderenums.ErrCouponNotFound)})
			}
			wrote += affected
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return wrote, nil
}

// NormalizeBatchIDs 是批量写入口的共享 id 归一：去重、去空白并校验上限。
//
// 归一属于业务规则（身份去重与上限），归 service；控制器只把表单原始值交进来。
// 返回空集时按参数错误拒绝——空选择不是「改了 0 条」的成功。
func NormalizeBatchIDs(raw []string) ([]string, error) {
	normalized, err := bulkids.Normalize(raw)
	if err != nil {
		return nil, err
	}
	if len(normalized) == 0 {
		return nil, orderenums.Err(orderenums.ErrInvalidParam)
	}
	return normalized, nil
}
