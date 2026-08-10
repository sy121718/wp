// Package menuservice 菜单模块业务逻辑实现。
package menuservice

import (
	"context"
	"errors"
	"regexp"
	"strings"

	menudto "go_wp/internal/module/menu/dto"
	menuenums "go_wp/internal/module/menu/enums"
	menumodel "go_wp/internal/module/menu/model"
)

// Detail 查询单个菜单详情。
func (s *Service) Detail(ctx context.Context, req *menudto.DetailReq) (res *menudto.DetailResp, err error) {
	entity, err := s.mm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New(menuenums.ErrMenuNotFound)
	}

	return entityToDetailResp(entity), nil
}

// Create 新建菜单。
func (s *Service) Create(ctx context.Context, req *menudto.CreateReq) error {
	req.Component = strings.TrimSpace(req.Component)
	if err := validateComponentBinding(req.Type, req.Component); err != nil {
		return err
	}
	if err := s.validatePermissionBinding(req.Type, req.PermissionCode, ctx); err != nil {
		return err
	}

	entity := &menumodel.MenuEntity{
		Title:       req.Title,
		ParentID:    req.ParentID,
		Type:        req.Type,
		Path:        req.Path,
		Component:   req.Component,
		ExternalURL: req.ExternalURL,
		Icon:        req.Icon,
		Status:      defaultStatus(req.Status),
		IsHidden:    req.IsHidden,
		IsPublic:    req.IsPublic,
		SortOrder:   req.SortOrder,
	}
	if req.PermissionCode != "" {
		entity.PermissionCode = &req.PermissionCode
	}
	if req.Remark != "" {
		entity.Remark = &req.Remark
	}

	return s.mm.Create(ctx, entity)
}

// Update 更新菜单。
func (s *Service) Update(ctx context.Context, req *menudto.UpdateReq) error {
	entity, err := s.mm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return errors.New(menuenums.ErrMenuNotFound)
	}

	// 系统内置菜单不允许修改类型
	if entity.IsSystem == 1 && entity.Type != req.Type {
		return errors.New(menuenums.ErrMenuIsSystem)
	}

	// 防止父子环
	if req.ParentID == req.ID {
		return errors.New(menuenums.ErrMenuCircle)
	}
	if req.ParentID != entity.ParentID && req.ParentID != 0 {
		if err := s.checkCircle(ctx, req.ID, req.ParentID); err != nil {
			return err
		}
	}

	req.Component = strings.TrimSpace(req.Component)
	if err := validateComponentBinding(req.Type, req.Component); err != nil {
		return err
	}
	if err := s.validatePermissionBinding(req.Type, req.PermissionCode, ctx); err != nil {
		return err
	}

	entity.Title = req.Title
	entity.ParentID = req.ParentID
	entity.Type = req.Type
	entity.Path = req.Path
	entity.Component = req.Component
	entity.ExternalURL = req.ExternalURL
	entity.Icon = req.Icon
	entity.Status = req.Status
	entity.IsHidden = req.IsHidden
	entity.IsPublic = req.IsPublic
	entity.SortOrder = req.SortOrder
	if req.PermissionCode != "" {
		entity.PermissionCode = &req.PermissionCode
	} else {
		entity.PermissionCode = nil
	}
	if req.Remark != "" {
		entity.Remark = &req.Remark
	} else {
		entity.Remark = nil
	}

	return s.mm.Update(ctx, entity)
}

// Delete 批量删除菜单（软删除）。
func (s *Service) Delete(ctx context.Context, req *menudto.DeleteReq) error {
	// 检查系统菜单
	for _, id := range req.IDs {
		entity, err := s.mm.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if entity == nil {
			continue
		}
		if entity.IsSystem == 1 {
			return errors.New(menuenums.ErrMenuIsSystem)
		}
		// 检查是否有子菜单
		childCount, err := s.mm.CountByParentID(ctx, id)
		if err != nil {
			return err
		}
		if childCount > 0 {
			return errors.New(menuenums.ErrMenuHasChildren)
		}
	}

	_, err := s.mm.SoftDelete(ctx, req.IDs)
	return err
}

var componentPathPattern = regexp.MustCompile(`^/src/views/(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9_-]+\.vue$`)

// validateComponentBinding 校验菜单类型与前端组件路径的绑定关系。
func validateComponentBinding(menuType int, component string) error {
	if menuType != menumodel.MenuTypeMenu {
		if component != "" {
			return errors.New(menuenums.ErrComponentNotAllowed)
		}
		return nil
	}
	if component == "" {
		return errors.New(menuenums.ErrComponentRequired)
	}
	if !componentPathPattern.MatchString(component) {
		return errors.New(menuenums.ErrComponentInvalid)
	}
	return nil
}

// validatePermissionBinding 校验 type 与 permission_code 的绑定约束。
func (s *Service) validatePermissionBinding(menuType int, code string, ctx context.Context) error {
	// 目录、iframe、外链不得绑定权限
	if menuType == menumodel.MenuTypeDirectory ||
		menuType == menumodel.MenuTypeIframe ||
		menuType == menumodel.MenuTypeExternal {
		if code != "" {
			return errors.New(menuenums.ErrCodeNotBindable)
		}
		return nil
	}

	// 菜单和按钮必须绑定权限
	if menuType == menumodel.MenuTypeMenu || menuType == menumodel.MenuTypeButton {
		if code == "" {
			return errors.New(menuenums.ErrCodeRequired)
		}
		// 校验 code 存在且启用
		if s.permSvc != nil {
			ok, err := s.permSvc.ExistsEnabledCode(ctx, code)
			if err != nil {
				return err
			}
			if !ok {
				return errors.New(menuenums.ErrCodeNotEnabled)
			}
		}
	}
	return nil
}

// checkCircle 检查将 targetID 的 parent 设为 newParentID 是否形成环。
func (s *Service) checkCircle(ctx context.Context, targetID, newParentID uint64) error {
	// 向上追溯 newParentID 的祖先链
	cursor := newParentID
	for cursor != 0 {
		if cursor == targetID {
			return errors.New(menuenums.ErrMenuCircle)
		}
		parent, err := s.mm.GetByID(ctx, cursor)
		if err != nil {
			return err
		}
		if parent == nil {
			break
		}
		cursor = parent.ParentID
	}
	return nil
}

// defaultStatus 返回默认启用状态（1），当传入 status 为 0 时使用。
func defaultStatus(status int) int {
	if status == 0 {
		return menumodel.MenuStatusEnabled
	}
	return status
}

// entityToDetailResp MenuEntity 转 DetailResp。
func entityToDetailResp(e *menumodel.MenuEntity) *menudto.DetailResp {
	code := ""
	if e.PermissionCode != nil {
		code = *e.PermissionCode
	}
	remark := ""
	if e.Remark != nil {
		remark = *e.Remark
	}
	return &menudto.DetailResp{
		ID:             e.ID,
		PermissionCode: code,
		Title:          e.Title,
		ParentID:       e.ParentID,
		Type:           e.Type,
		Path:           e.Path,
		Component:      e.Component,
		ExternalURL:    e.ExternalURL,
		Icon:           e.Icon,
		Status:         e.Status,
		IsHidden:       e.IsHidden,
		IsPublic:       e.IsPublic,
		IsSystem:       e.IsSystem,
		SortOrder:      e.SortOrder,
		Remark:         remark,
	}
}
