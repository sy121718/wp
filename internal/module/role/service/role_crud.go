// Package roleservice 角色模块业务逻辑实现。
package roleservice

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	roledto "go_wp/internal/module/role/dto"
	roleenums "go_wp/internal/module/role/enums"
	rolemodel "go_wp/internal/module/role/model"
	"go_wp/pkg/casbin"
)

var pureNumericPattern = regexp.MustCompile(`^[0-9]+$`)

// List 角色分页列表。
func (s *Service) List(ctx context.Context, req *roledto.ListReq) (res *roledto.ListResp, err error) {
	total, entities, err := s.rm.ListAll(ctx, req.GetPage(), req.GetLimit(), req.Keyword)
	if err != nil {
		return nil, err
	}

	list := make([]roledto.RoleItem, 0, len(entities))
	for _, e := range entities {
		list = append(list, entityToItem(e))
	}
	return &roledto.ListResp{Total: total, List: list}, nil
}

// Detail 角色详情。
func (s *Service) Detail(ctx context.Context, req *roledto.DetailReq) (res *roledto.DetailResp, err error) {
	entity, err := s.rm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New(roleenums.ErrRoleNotFound)
	}
	return entityToDetailResp(entity), nil
}

// Create 新建角色。
func (s *Service) Create(ctx context.Context, req *roledto.CreateReq) error {
	if pureNumericPattern.MatchString(req.RoleCode) {
		return errors.New(roleenums.ErrRoleCodeNumeric)
	}

	existing, err := s.rm.GetByCode(ctx, req.RoleCode)
	if err != nil {
		return err
	}
	if existing != nil {
		return errors.New(roleenums.ErrRoleCodeExists)
	}

	entity := &rolemodel.RoleEntity{
		RoleCode:  req.RoleCode,
		RoleName:  req.RoleName,
		Status:    req.Status,
		SortOrder: req.SortOrder,
	}
	if entity.Status == 0 {
		entity.Status = rolemodel.RoleStatusEnabled
	}
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	if err = s.rm.Create(ctx, entity); err != nil {
		return err
	}

	// 新建角色默认启用：写入 g2
	if entity.Status == rolemodel.RoleStatusEnabled {
		if err = casbin.ActivateRole(entity.RoleCode); err != nil {
			return fmt.Errorf("激活角色失败: %w", err)
		}
	}
	return nil
}

// Update 更新角色元信息。
func (s *Service) Update(ctx context.Context, req *roledto.UpdateReq) error {
	entity, err := s.rm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return errors.New(roleenums.ErrRoleNotFound)
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
		if entity.Status == rolemodel.RoleStatusEnabled {
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

// Delete 删除角色。
// 系统内置角色不可删除。
// 普通角色删除时：删除 sys_role 行 + Casbin 层面清理 p/g/g2。
func (s *Service) Delete(ctx context.Context, req *roledto.DeleteReq) error {
	entity, err := s.rm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return errors.New(roleenums.ErrRoleNotFound)
	}
	if entity.IsSystem == 1 {
		return errors.New(roleenums.ErrRoleIsSystem)
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
		if role.Status == rolemodel.RoleStatusEnabled {
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

// entityToItem RoleEntity 转列表项 RoleItem。
func entityToItem(e rolemodel.RoleEntity) roledto.RoleItem {
	remark := ""
	if e.Remark != nil {
		remark = *e.Remark
	}
	return roledto.RoleItem{
		ID:        e.ID,
		RoleCode:  e.RoleCode,
		RoleName:  e.RoleName,
		Status:    e.Status,
		IsSystem:  e.IsSystem,
		SortOrder: e.SortOrder,
		Remark:    remark,
	}
}

// entityToDetailResp RoleEntity 转详情响应 DetailResp。
func entityToDetailResp(e *rolemodel.RoleEntity) *roledto.DetailResp {
	remark := ""
	if e.Remark != nil {
		remark = *e.Remark
	}
	return &roledto.DetailResp{
		ID:        e.ID,
		RoleCode:  e.RoleCode,
		RoleName:  e.RoleName,
		Status:    e.Status,
		IsSystem:  e.IsSystem,
		SortOrder: e.SortOrder,
		Remark:    remark,
	}
}
