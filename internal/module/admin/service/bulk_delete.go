package adminservice

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"gorm.io/gorm"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/casbin"
	"go_wp/pkg/facing"
	"go_wp/pkg/reqparams"
)

// 批量删除：全成或全败。
//
// 口径来自 AGENTS.md「写操作」：一次用户可感知的写操作要么全成要么全败，
// 不推算 skipped、不返回逐条回执；拒绝时把 id / 名称 / 原因放进 data.items。
// 所以「校验」与「删除」必须在同一个事务里：
//
//   - 校验读的是本事务内的行锁结果，不会出现「校验时还在、删除时已被别人删掉」；
//   - 任一行被拒绝就整体回滚，前面已校验通过的行不会留下半成品。
//
// 锁顺序：按 id 升序加锁（见 uniqueIDs 里的 sortIDs）。批量内多个行锁谁先谁后
// 由 id 决定，与提交顺序无关，避免两个并发批量以相反顺序拿锁形成死锁。
//
// 保护语义保持原样，不引入级联：部门停用成员照旧算「有成员」，
// 有子节点按既有拒绝 key 打回，不做同批拓扑删除。

// blockedItemsError 把「哪些行被拒绝、为什么」带给 JSON 出口。
//
// 它实现 facing.BlockedError：pkg/response 的 blockedDataOf 会把 items
// 逐条取词放进 data.items，主错误文案仍走 message。
type blockedItemsError struct {
	items []facing.BlockedItem
}

func (e *blockedItemsError) Error() string { return "批量删除被拒绝" }

func (e *blockedItemsError) BlockedItems() []facing.BlockedItem { return e.items }

// newBlockedError 组装「主错误 key + 明细」。
//
// 主错误决定状态码与 message（被拒绝那一类的业务 key）；
// 明细是逐行的 id / 名称 / 原因 key，供前端列出来。
func newBlockedError(mainKey string, mainStatus int, items []facing.BlockedItem) error {
	base := facing.WithStatus(facing.New(mainKey), mainStatus)
	if len(items) == 0 {
		return base
	}
	return errors.Join(base, &blockedItemsError{items: items})
}

const (
	statusConflict = http.StatusConflict
	statusBadInput = http.StatusBadRequest
)

// AdminBulkDelete 在单个事务里批量删除管理员。
//
// 拒绝语义沿用单条删除：不能删自己、不能删超管、不存在即打回。
// 会话吊销在提交之后做，因为它跨库且不可回滚。
func (s *Service) AdminBulkDelete(ctx context.Context, req *admindto.AdminDeleteReq) (*admindto.AdminDeleteResp, error) {
	ids := uniqueIDs(req.IDList())
	if len(ids) == 0 {
		return nil, facing.WithStatus(facing.New(adminenums.ErrBulkDeleteEmpty), statusBadInput)
	}

	var blocked []facing.BlockedItem
	var deleted int64
	var reloadTaskID int64
	var revokeTaskIDs []int64

	err := s.am.Transaction(ctx, func(tx *gorm.DB) error {
		locked, err := s.am.LockByIDsTx(ctx, tx, ids)
		if err != nil {
			return err
		}
		byID := make(map[uint64]adminmodel.AdminEntity, len(locked))
		for _, entity := range locked {
			byID[entity.ID] = entity
		}

		for _, id := range ids {
			entity, ok := byID[id]
			if !ok {
				blocked = append(blocked, facing.BlockedItem{
					ID:        fmt.Sprint(id),
					ReasonKey: adminenums.ErrAdminNotFound,
					Fallback:  adminenums.ErrAdminNotFound,
				})
				continue
			}
			switch {
			case id == req.OperatorID:
				blocked = append(blocked, facing.BlockedItem{
					ID:        fmt.Sprint(id),
					Name:      entity.Username,
					ReasonKey: adminenums.ErrDeleteSelf,
					Fallback:  adminenums.ErrDeleteSelf,
				})
			case entity.IsSuperAdmin():
				blocked = append(blocked, facing.BlockedItem{
					ID:        fmt.Sprint(id),
					Name:      entity.Username,
					ReasonKey: adminenums.ErrDeleteSuperAdmin,
					Fallback:  adminenums.ErrDeleteSuperAdmin,
				})
			}
		}
		if len(blocked) > 0 {
			key, status := blockSummary(blocked)
			return newBlockedError(key, status, blocked)
		}

		deleted, err = s.am.DeleteByIDsTx(ctx, tx, ids)
		if err != nil {
			return err
		}
		if deleted != int64(len(ids)) {
			// 行锁之后仍然少删：说明数据在事务外被改动，按冲突打回。
			return facing.WithStatus(facing.New(adminenums.ErrAdminNotFound), statusConflict)
		}

		for _, id := range ids {
			if _, err := casbin.DeleteUserAllPoliciesTx(ctx, tx, fmt.Sprint(id)); err != nil {
				return fmt.Errorf("管理员行已删但策略清理失败，本次删除已整体回滚: %w", err)
			}
		}

		// 策略重载与会话吊销跨库/跨进程，PG 事务回滚不了它们。
		// 在这里写任务：业务回滚，任务一起消失；提交后立刻尝试一次，失败交给 worker 重放。
		reloadTaskID, err = s.appendPolicyReloadTx(ctx, tx)
		if err != nil {
			return err
		}
		revokeTaskIDs = revokeTaskIDs[:0]
		for _, id := range ids {
			taskID, err := s.appendRevokeSessionTx(ctx, tx, id)
			if err != nil {
				return err
			}
			revokeTaskIDs = append(revokeTaskIDs, taskID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 提交了就是删成了：副作用在别处失败不能把这句话改成「整批回滚」。
	s.FlushSideEffect(ctx, reloadTaskID)
	for _, taskID := range revokeTaskIDs {
		s.FlushSideEffect(ctx, taskID)
	}
	return &admindto.AdminDeleteResp{DeletedCount: deleted}, nil
}

// RoleBulkDelete 在单个事务里批量删除角色。
//
// 系统内置角色一律打回（沿用 ErrRoleIsSystem），不做「跳过系统角色删其余」。
func (s *Service) RoleBulkDelete(ctx context.Context, req *admindto.RoleDeleteReq) error {
	ids := uniqueIDs(req.IDList())
	if len(ids) == 0 {
		return facing.WithStatus(facing.New(adminenums.ErrBulkDeleteEmpty), statusBadInput)
	}

	var blocked []facing.BlockedItem
	reloadPolicy := false
	var reloadTaskID int64

	err := s.rm.Transaction(ctx, func(tx *gorm.DB) error {
		locked := make([]adminmodel.RoleEntity, 0, len(ids))
		for _, id := range ids {
			entity, err := s.rm.LockByIDTx(ctx, tx, id)
			if err != nil {
				return err
			}
			if entity == nil {
				blocked = append(blocked, facing.BlockedItem{
					ID: fmt.Sprint(id), ReasonKey: adminenums.ErrRoleNotFound, Fallback: adminenums.ErrRoleNotFound,
				})
				continue
			}
			if entity.IsSystem == 1 {
				blocked = append(blocked, facing.BlockedItem{
					ID: fmt.Sprint(id), Name: entity.RoleName,
					ReasonKey: adminenums.ErrRoleIsSystem, Fallback: adminenums.ErrRoleIsSystem,
				})
				continue
			}
			locked = append(locked, *entity)
		}
		if len(blocked) > 0 {
			key, status := blockSummary(blocked)
			return newBlockedError(key, status, blocked)
		}

		for _, entity := range locked {
			changed, err := casbin.DeleteRoleAllPoliciesTx(ctx, tx, entity.RoleCode)
			if err != nil {
				return fmt.Errorf("角色策略清理失败，本次删除已整体回滚: %w", err)
			}
			reloadPolicy = reloadPolicy || changed
		}
		deleted, err := s.rm.DeleteByIDsTx(ctx, tx, ids)
		if err != nil {
			return err
		}
		if deleted != int64(len(ids)) {
			return facing.WithStatus(facing.New(adminenums.ErrRoleNotFound), statusConflict)
		}
		if reloadPolicy {
			// 策略重载改不动已提交的数据：落成任务，提交后立刻试一次，失败留给 worker。
			reloadTaskID, err = s.appendPolicyReloadTx(ctx, tx)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	s.FlushSideEffect(ctx, reloadTaskID)
	s.reloadDataRuleSnapshotAfterWrite("角色批量删除")
	return nil
}

// PermBulkDelete 在单个事务里批量删除权限点。
//
// 原有两条保护：已被角色引用、已被菜单引用。两者都在本事务内判定，
// 任一不满足即整体打回并列出明细。
func (s *Service) PermBulkDelete(ctx context.Context, req *admindto.PermDeleteReq) (*admindto.PermDeleteResp, error) {
	ids := uniqueIDs(req.IDList())
	if len(ids) == 0 {
		return nil, facing.WithStatus(facing.New(adminenums.ErrBulkDeleteEmpty), statusBadInput)
	}

	var blocked []facing.BlockedItem
	var deleted int64

	err := s.pm.Transaction(ctx, func(tx *gorm.DB) error {
		// 按 id 升序锁住这批权限点行。锁序必须与授予端一致：授予端的
		// LockByCodesTx 同样是 ORDER BY id —— 两端在同一批行上按同一顺序排队，
		// 「A 读到没人引用 → B 授予提交 → A 删掉」才挡得住。
		entities, err := s.pm.LockByIDsTx(ctx, tx, ids)
		if err != nil {
			return err
		}
		byID := make(map[uint64]adminmodel.PermissionEntity, len(entities))
		for _, entity := range entities {
			byID[entity.ID] = entity
		}

		codes := make([]string, 0, len(ids))
		for _, id := range ids {
			entity, ok := byID[id]
			if !ok {
				blocked = append(blocked, facing.BlockedItem{
					ID: fmt.Sprint(id), ReasonKey: adminenums.ErrPermissionNotFound, Fallback: adminenums.ErrPermissionNotFound,
				})
				continue
			}
			codes = append(codes, entity.PermissionCode)

			// 角色绑定读事务内的政策表：内存 enforcer 是副本，看不到本事务刚写入的绑定。
			assigned, err := casbin.HasPermissionPoliciesTx(ctx, tx, entity.PermissionCode)
			if err != nil {
				return err
			}
			if assigned {
				blocked = append(blocked, facing.BlockedItem{
					ID: fmt.Sprint(id), Name: entity.PermissionName,
					ReasonKey: adminenums.ErrPermissionAssigned, Fallback: adminenums.ErrPermissionAssigned,
				})
			}
		}
		if len(blocked) > 0 {
			key, status := blockSummary(blocked)
			return newBlockedError(key, status, blocked)
		}

		// 菜单引用同样读本事务，不用缓存。
		menuIDs, err := s.mm.ListMenuIDsByPermissionCodesTx(ctx, tx, codes)
		if err != nil {
			return err
		}
		if len(menuIDs) > 0 {
			for _, id := range ids {
				entity := byID[id]
				blocked = append(blocked, facing.BlockedItem{
					ID: fmt.Sprint(id), Name: entity.PermissionName,
					ReasonKey: adminenums.ErrMenuReferenced, Fallback: adminenums.ErrMenuReferenced,
				})
			}
			key, status := blockSummary(blocked)
			return newBlockedError(key, status, blocked)
		}

		deleted, err = s.pm.DeleteByIDsTx(ctx, tx, ids)
		if err != nil {
			return err
		}
		if deleted != int64(len(ids)) {
			return facing.WithStatus(facing.New(adminenums.ErrPermissionNotFound), statusConflict)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &admindto.PermDeleteResp{DeletedCount: deleted}, nil
}

// MenuBulkDelete 在单个事务里批量软删菜单。
//
// 保护语义沿用单条删除：系统菜单打回、有子节点打回。
// 子节点判定读本事务内的行锁结果，不扩成级联删除。
func (s *Service) MenuBulkDelete(ctx context.Context, req *admindto.MenuDeleteReq) error {
	ids := uniqueIDs(req.IDList())
	if len(ids) == 0 {
		return facing.WithStatus(facing.New(adminenums.ErrBulkDeleteEmpty), statusBadInput)
	}

	var blocked []facing.BlockedItem

	err := s.mm.Transaction(ctx, func(tx *gorm.DB) error {
		for _, id := range ids {
			entity, err := s.mm.GetByIDTx(ctx, tx, id)
			if err != nil {
				return err
			}
			if entity == nil {
				blocked = append(blocked, facing.BlockedItem{
					ID: fmt.Sprint(id), ReasonKey: adminenums.ErrMenuNotFound, Fallback: adminenums.ErrMenuNotFound,
				})
				continue
			}
			if entity.IsSystem == 1 {
				blocked = append(blocked, facing.BlockedItem{
					ID: fmt.Sprint(id), Name: entity.Title,
					ReasonKey: adminenums.ErrMenuIsSystem, Fallback: adminenums.ErrMenuIsSystem,
				})
				continue
			}
			childCount, err := s.mm.CountByParentIDTx(ctx, tx, id)
			if err != nil {
				return err
			}
			if childCount > 0 {
				blocked = append(blocked, facing.BlockedItem{
					ID: fmt.Sprint(id), Name: entity.Title,
					ReasonKey: adminenums.ErrMenuHasChildren, Fallback: adminenums.ErrMenuHasChildren,
				})
			}
		}
		if len(blocked) > 0 {
			key, status := blockSummary(blocked)
			return newBlockedError(key, status, blocked)
		}

		deleted, err := s.mm.SoftDeleteWithPermissionCodesTx(ctx, tx, ids)
		if err != nil {
			return err
		}
		if deleted != int64(len(ids)) {
			return facing.WithStatus(facing.New(adminenums.ErrMenuNotFound), statusConflict)
		}
		return nil
	})
	if err != nil {
		return err
	}
	invalidateMenuCache()
	return nil
}

// DeptBulkDelete 在单个事务里批量删除部门。
//
// 保护语义沿用单条删除：有子部门打回、有成员打回（停用成员也算，按 dept_id 数）。
func (s *Service) DeptBulkDelete(ctx context.Context, req *admindto.DeptDeleteReq) error {
	ids := uniqueIDs(req.IDList())
	if len(ids) == 0 {
		return facing.WithStatus(facing.New(adminenums.ErrBulkDeleteEmpty), statusBadInput)
	}

	var blocked []facing.BlockedItem

	err := s.dm.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.dm.LockTreeTx(ctx, tx); err != nil {
			return err
		}
		for _, id := range ids {
			entity, err := s.dm.LockByIDTx(ctx, tx, id)
			if err != nil {
				return err
			}
			if entity == nil {
				blocked = append(blocked, facing.BlockedItem{
					ID: fmt.Sprint(id), ReasonKey: adminenums.ErrDeptNotFound, Fallback: adminenums.ErrDeptNotFound,
				})
				continue
			}
			childCount, err := s.dm.CountByParentIDTx(ctx, tx, id)
			if err != nil {
				return err
			}
			if childCount > 0 {
				blocked = append(blocked, facing.BlockedItem{
					ID: fmt.Sprint(id), Name: entity.DeptName,
					ReasonKey: adminenums.ErrDeptHasChildren, Fallback: adminenums.ErrDeptHasChildren,
				})
				continue
			}
			userCount, err := s.am.CountByDeptIDTx(ctx, tx, id)
			if err != nil {
				return err
			}
			if userCount > 0 {
				blocked = append(blocked, facing.BlockedItem{
					ID: fmt.Sprint(id), Name: entity.DeptName,
					ReasonKey: adminenums.ErrDeptHasUsers, Fallback: adminenums.ErrDeptHasUsers,
				})
			}
		}
		if len(blocked) > 0 {
			key, status := blockSummary(blocked)
			return newBlockedError(key, status, blocked)
		}

		for _, id := range ids {
			if err := s.dm.DeleteTx(ctx, tx, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.reloadDataRuleSnapshotAfterWrite("部门批量删除")
	return nil
}

// RuleBulkDelete 在单个事务里批量删除数据规则与其生效指派。
func (s *Service) RuleBulkDelete(ctx context.Context, req *admindto.RuleDeleteReq) error {
	ids := uniqueIDs(req.IDList())
	if len(ids) == 0 {
		return facing.WithStatus(facing.New(adminenums.ErrBulkDeleteEmpty), statusBadInput)
	}

	var blocked []facing.BlockedItem

	err := s.drm.Transaction(ctx, func(tx *gorm.DB) error {
		locked, err := s.drm.LockByIDsTx(ctx, tx, ids)
		if err != nil {
			return err
		}
		byID := make(map[uint64]struct{}, len(locked))
		for _, entity := range locked {
			byID[entity.ID] = struct{}{}
		}
		for _, id := range ids {
			if _, ok := byID[id]; !ok {
				blocked = append(blocked, facing.BlockedItem{
					ID: fmt.Sprint(id), ReasonKey: adminenums.ErrRuleNotFound, Fallback: adminenums.ErrRuleNotFound,
				})
			}
		}
		if len(blocked) > 0 {
			key, status := blockSummary(blocked)
			return newBlockedError(key, status, blocked)
		}

		for _, id := range ids {
			if err := s.dram.DeleteByRuleIDTx(ctx, tx, id); err != nil {
				return err
			}
		}
		deleted, err := s.drm.DeleteByIDsTx(ctx, tx, ids)
		if err != nil {
			return err
		}
		if deleted != int64(len(ids)) {
			return facing.WithStatus(facing.New(adminenums.ErrRuleNotFound), statusConflict)
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.reloadDataRuleSnapshotAfterWrite("数据规则批量删除")
	return nil
}

// uniqueIDs 去重并去掉 0：0 不是有效主键，也不是「用户选中的行」。
func uniqueIDs(ids []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(ids))
	out := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	// 稳定排序：批量内行锁按 id 升序获取，避免并发批量以相反顺序拿锁。
	sortIDs(out)
	return out
}

// uniqueStrings 去空去重并按字典序排好。
//
// 角色编码不是主键，锁序不能靠 id —— 但两个写端（按角色收成员 / 按用户收角色）
// 传进来的编码集合顺序不同，必须在这里统一成同一个顺序再去加锁。
func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// sortIDs 就地升序，不引入 sort 的泛型依赖面。
func sortIDs(ids []uint64) {
	for i := 1; i < len(ids); i++ {
		v := ids[i]
		j := i - 1
		for j >= 0 && ids[j] > v {
			ids[j+1] = ids[j]
			j--
		}
		ids[j+1] = v
	}
}

// InvalidIDsError 表示请求里带了不能当主键解析的原始值。
//
// 它同时是业务错误声明（facing key + 400）和明细载体（BlockedError）：
// 出口据此回 data.items，逐条点名「哪个原始值不合法」，不做静默丢弃。
type InvalidIDsError struct {
	invalid []string
}

func (e *InvalidIDsError) Error() string { return "批量请求含非法主键" }

func (e *InvalidIDsError) BlockedItems() []facing.BlockedItem {
	items := make([]facing.BlockedItem, 0, len(e.invalid))
	for _, raw := range e.invalid {
		items = append(items, facing.BlockedItem{
			ID:        raw,
			ReasonKey: adminenums.ErrBulkDeleteInvalidID,
			Fallback:  adminenums.ErrBulkDeleteInvalidID,
		})
	}
	return items
}

// NewInvalidIDsError 把原始非法值包成「声明 400 + 明细逐条点名」的业务错误。
func NewInvalidIDsError(invalid []string) error {
	base := facing.WithStatus(facing.New(adminenums.ErrBulkDeleteInvalidID), statusBadInput)
	return errors.Join(base, &InvalidIDsError{invalid: invalid})
}

// blockSummary 从逐行明细里挑出「主错误」。
//
// 主错误决定响应的状态码与 message，所以不能一律拿某一个保护 key：
// 同一批里既有不存在的行（404）又有被保护的行（409）时，
// 用 409 的 key 会把「查无此对象」说成「这个对象受保护」。
//
// 状态码不在这里猜：每个 key 声明自己的状态（adminenums.StatusOf），
// 这一层只按声明挑「最该先说」的那条 —— 先说不存在（用户去改选），
// 再说不受保护之外的原因，最后按冲突处理。
func blockSummary(items []facing.BlockedItem) (string, int) {
	notFound, other := -1, -1
	for i, item := range items {
		switch adminenums.StatusOf(item.ReasonKey) {
		case http.StatusNotFound:
			if notFound < 0 {
				notFound = i
			}
		default:
			if other < 0 {
				other = i
			}
		}
	}
	switch {
	case notFound >= 0:
		return items[notFound].ReasonKey, http.StatusNotFound
	case other >= 0:
		return items[other].ReasonKey, statusConflict
	default:
		return adminenums.ErrBulkDeleteEmpty, statusBadInput
	}
}

// NormalizeBulkIDs 是后台批量写入口的共享 id 归一：去重、校验上限、
// 逐条解析为数字 id，并把非法项作为可定位明细交回。
//
// 归一属于业务规则，归 service；控制器只把表单原始值交进来、把结果交给出口。
func NormalizeBulkIDs(raw []string) (ids []uint64, err error) {
	normalized, berr := bulkids.Normalize(raw)
	if berr != nil {
		return nil, berr
	}
	ids = make([]uint64, 0, len(normalized))
	invalid := make([]string, 0)
	for _, item := range normalized {
		id := reqparams.ParseUint(item)
		if id == 0 {
			invalid = append(invalid, item)
			continue
		}
		ids = append(ids, id)
	}
	if len(invalid) > 0 {
		return nil, NewInvalidIDsError(invalid)
	}
	if len(ids) == 0 {
		return nil, adminenums.Err(adminenums.ErrAdminSelectionEmpty)
	}
	return ids, nil
}
