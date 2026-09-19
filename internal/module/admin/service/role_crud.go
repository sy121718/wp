package adminservice

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/casbin"

	"gorm.io/gorm"
)

var pureNumericPattern = regexp.MustCompile(`^[0-9]+$`)

// RoleList 角色分页列表。
func (s *Service) RoleList(ctx context.Context, req *admindto.RoleListReq) (res *admindto.RoleListResp, err error) {
	total, entities, err := s.rm.ListAll(ctx, req.GetPage(), req.GetLimit(), req.Keyword)
	if err != nil {
		return nil, err
	}

	list := make([]admindto.RoleItem, 0, len(entities))
	for _, e := range entities {
		list = append(list, roleEntityToItem(e))
	}
	return &admindto.RoleListResp{Total: total, List: list}, nil
}

// RoleDetail 角色详情。
func (s *Service) RoleDetail(ctx context.Context, req *admindto.RoleDetailReq) (res *admindto.RoleDetailResp, err error) {
	entity, err := s.rm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New(adminenums.ErrRoleNotFound)
	}
	return roleEntityToDetailResp(entity), nil
}

// RoleCreate 新建角色。
//
// 事务：sys_role 行与 sys_casbin_rule 的 g2 启用标记是两处持久化写入，必须同事务。
// 旧实现先提交角色行、再写 g2，g2 写失败就留下「角色已建但没启用标记」——
// 角色看起来启用（status=1）却对所有人不生效，且没有任何回滚。
//
// 唯一性：事务外的重复检查只为给出可读错误，真正防并发重复的是 sys_role.role_code 上的
// 唯一索引（并发插入失败会原样上抛，不静默合并/改码）。
func (s *Service) RoleCreate(ctx context.Context, req *admindto.RoleCreateReq) error {
	if pureNumericPattern.MatchString(req.RoleCode) {
		return errors.New(adminenums.ErrRoleCodeNumeric)
	}

	existing, err := s.rm.GetByCode(ctx, req.RoleCode)
	if err != nil {
		return err
	}
	if existing != nil {
		return errors.New(adminenums.ErrRoleCodeExists)
	}

	entity := &adminmodel.RoleEntity{
		RoleCode:  req.RoleCode,
		RoleName:  req.RoleName,
		SortOrder: req.SortOrder,
	}
	// Status 未显式传（nil）时默认启用；显式传 0（禁用）尊重之，不再强制改写。
	if req.Status != nil {
		entity.Status = *req.Status
	} else {
		entity.Status = adminmodel.RoleStatusEnabled
	}
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	reloadPolicy := false
	err = s.rm.Transaction(ctx, func(tx *gorm.DB) error {
		if err := s.rm.CreateTx(ctx, tx, entity); err != nil {
			return err
		}
		// 新建角色默认启用：写入 g2（与角色行同事务）
		if entity.Status != adminmodel.RoleStatusEnabled {
			return nil
		}
		changed, err := casbin.ActivateRoleTx(ctx, tx, entity.RoleCode)
		if err != nil {
			return fmt.Errorf("角色已建但启用标记写入失败，本次创建已整体回滚: %w", err)
		}
		reloadPolicy = changed
		return nil
	})
	if err != nil {
		return err
	}
	if reloadPolicy {
		if err = casbin.ReloadPolicy(); err != nil {
			return fmt.Errorf("角色已创建，但权限策略重载失败（内存副本仍是旧策略，请重试或重启服务）: %w", err)
		}
	}
	return nil
}

// RoleUpdate 更新角色元信息。
//
// 事务：sys_role.status 与 sys_casbin_rule 的 g2 启用标记必须同事务。旧实现先提交角色行、
// 再动 g2：**禁用角色时 g2 删除失败，库里已禁用而 g2 还在 —— 该角色下所有人仍被放行**
// （禁用没生效，且错误被吞在响应里，运维看不出）。现在任一步失败整体回滚，禁用要么完整生效、
// 要么角色保持启用，不会停在「一半」。
//
// 读-改-写加行锁（LockByIDTx）：要先读旧 status 才能判断该不该动 g2，不加锁并发启停会互相覆盖。
func (s *Service) RoleUpdate(ctx context.Context, req *admindto.RoleUpdateReq) error {
	reloadPolicy := false
	err := s.rm.Transaction(ctx, func(tx *gorm.DB) error {
		entity, err := s.rm.LockByIDTx(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if entity == nil {
			return errors.New(adminenums.ErrRoleNotFound)
		}

		oldStatus := entity.Status
		entity.RoleName = req.RoleName
		entity.Status = req.Status
		entity.SortOrder = req.SortOrder
		if req.Remark != "" {
			entity.Remark = &req.Remark
		} else {
			entity.Remark = nil
		}

		if err = s.rm.UpdateTx(ctx, tx, entity); err != nil {
			return err
		}

		// 状态变更同步到 Casbin g2（与角色行同事务）
		if oldStatus == entity.Status {
			return nil
		}
		changed := false
		if entity.Status == adminmodel.RoleStatusEnabled {
			changed, err = casbin.ActivateRoleTx(ctx, tx, entity.RoleCode)
		} else {
			changed, err = casbin.DeactivateRoleTx(ctx, tx, entity.RoleCode)
		}
		if err != nil {
			return fmt.Errorf("角色状态已改但启用标记同步失败，本次修改已整体回滚: %w", err)
		}
		reloadPolicy = changed
		return nil
	})
	if err != nil {
		return err
	}
	if reloadPolicy {
		if err = casbin.ReloadPolicy(); err != nil {
			return fmt.Errorf("角色已保存，但权限策略重载失败（内存副本仍是旧策略，请重试或重启服务）: %w", err)
		}
	}
	return nil
}

// RoleDelete 删除角色。
// 系统内置角色不可删除。
// 普通角色删除时：删除 sys_role 行 + Casbin 层面清理 p/g/g2。
//
// 事务：sys_role 行与 sys_casbin_rule 里该角色的全部策略行（p 拥有权限 / g 用户绑定 /
// g2 启用标记）是两处持久化写，必须同事务。旧实现先删策略、后删实体：策略删完而实体
// 删除失败，就留下「角色还在、权限一个不剩」—— 该角色下所有人瞬间失权，而报错只回给
// 这一次请求，列表页上完全看不出异常（与 PermUpdate / RoleUpdate 同一形态）。
// 现在任一步失败整体回滚：要么角色与策略一起消失，要么都保持原样。
//
// 读-改-写加行锁（LockByIDTx）：先读 is_system 才能判断能不能删，不加锁时并发改
// 「普通角色 → 系统角色」会让保护失效。
func (s *Service) RoleDelete(ctx context.Context, req *admindto.RoleDeleteReq) error {
	reloadPolicy := false
	err := s.rm.Transaction(ctx, func(tx *gorm.DB) error {
		entity, err := s.rm.LockByIDTx(ctx, tx, req.ID)
		if err != nil {
			return err
		}
		if entity == nil {
			return errors.New(adminenums.ErrRoleNotFound)
		}
		if entity.IsSystem == 1 {
			return errors.New(adminenums.ErrRoleIsSystem)
		}

		// Casbin 清理（p / g / g2）与该角色的 sys_role 行同事务
		changed, err := casbin.DeleteRoleAllPoliciesTx(ctx, tx, entity.RoleCode)
		if err != nil {
			return fmt.Errorf("角色策略清理失败，本次删除已整体回滚: %w", err)
		}
		if err = s.rm.DeleteTx(ctx, tx, req.ID); err != nil {
			return err
		}
		reloadPolicy = changed
		return nil
	})
	if err != nil {
		return err
	}
	if reloadPolicy {
		if err = casbin.ReloadPolicy(); err != nil {
			return fmt.Errorf("角色已删除，但权限策略重载失败（内存副本仍是旧策略，请重试或重启服务）: %w", err)
		}
	}
	return nil
}

// GetRoleCodesByUserID 对外契约：查询用户绑定且已启用的角色编码列表。
func (s *Service) GetRoleCodesByUserID(ctx context.Context, userID uint64) ([]string, error) {
	codes, err := casbin.GetRoleCodesByUserID(strconv.FormatUint(userID, 10))
	if err != nil || len(codes) == 0 {
		return codes, err
	}

	roles, err := s.rm.ListByCodes(ctx, codes)
	if err != nil {
		return nil, err
	}
	enabled := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if role.Status == adminmodel.RoleStatusEnabled {
			enabled[role.RoleCode] = struct{}{}
		}
	}

	activeCodes := make([]string, 0, len(enabled))
	for _, code := range codes {
		if _, ok := enabled[code]; ok {
			activeCodes = append(activeCodes, code)
		}
	}
	return activeCodes, nil
}

// GetEnabledRoleIDsByCodes 对外契约：将角色编码解析为已启用角色 ID。
func (s *Service) GetEnabledRoleIDsByCodes(ctx context.Context, codes []string) (ids []uint64, err error) {
	return s.rm.GetEnabledIDsByCodes(ctx, codes)
}

// roleEntityToItem RoleEntity 转列表项 RoleItem。
func roleEntityToItem(e adminmodel.RoleEntity) admindto.RoleItem {
	return admindto.RoleItem{
		ID:        e.ID,
		RoleCode:  e.RoleCode,
		RoleName:  e.RoleName,
		Status:    e.Status,
		IsSystem:  e.IsSystem,
		SortOrder: e.SortOrder,
		Remark:    roleRemark(e.Remark),
	}
}

// roleEntityToDetailResp RoleEntity 转详情响应 RoleDetailResp。
func roleEntityToDetailResp(e *adminmodel.RoleEntity) *admindto.RoleDetailResp {
	return &admindto.RoleDetailResp{
		ID:        e.ID,
		RoleCode:  e.RoleCode,
		RoleName:  e.RoleName,
		Status:    e.Status,
		IsSystem:  e.IsSystem,
		SortOrder: e.SortOrder,
		Remark:    roleRemark(e.Remark),
	}
}

// roleRemark 解引用可选备注字段。
func roleRemark(remark *string) string {
	if remark != nil {
		return *remark
	}
	return ""
}
