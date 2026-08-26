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
		Status:    req.Status,
		SortOrder: req.SortOrder,
	}
	if entity.Status == 0 {
		entity.Status = adminmodel.RoleStatusEnabled
	}
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	if err = s.rm.Create(ctx, entity); err != nil {
		return err
	}

	// 新建角色默认启用：写入 g2
	if entity.Status == adminmodel.RoleStatusEnabled {
		if err = casbin.ActivateRole(entity.RoleCode); err != nil {
			return fmt.Errorf("激活角色失败: %w", err)
		}
	}
	return nil
}

// RoleUpdate 更新角色元信息。
func (s *Service) RoleUpdate(ctx context.Context, req *admindto.RoleUpdateReq) error {
	entity, err := s.rm.GetByID(ctx, req.ID)
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

	if err = s.rm.Update(ctx, entity); err != nil {
		return err
	}

	// 状态变更同步到 Casbin g2
	if oldStatus != entity.Status {
		if entity.Status == adminmodel.RoleStatusEnabled {
			if err = casbin.ActivateRole(entity.RoleCode); err != nil {
				return err
			}
		} else {
			if err = casbin.DeactivateRole(entity.RoleCode); err != nil {
				return err
			}
		}
	}
	return nil
}

// RoleDelete 删除角色。
// 系统内置角色不可删除。
// 普通角色删除时：删除 sys_role 行 + Casbin 层面清理 p/g/g2。
func (s *Service) RoleDelete(ctx context.Context, req *admindto.RoleDeleteReq) error {
	entity, err := s.rm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return errors.New(adminenums.ErrRoleNotFound)
	}
	if entity.IsSystem == 1 {
		return errors.New(adminenums.ErrRoleIsSystem)
	}

	// Casbin 清理
	if err = casbin.DeleteRoleAllPolicies(entity.RoleCode); err != nil {
		return err
	}
	// 删除 sys_role
	if err = s.rm.Delete(ctx, req.ID); err != nil {
		return err
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
