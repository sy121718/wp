package adminservice

import (
	"context"
	"errors"
	"regexp"
	"strings"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	adminmodel "go_wp/internal/module/admin/model"
)

// MenuDetail 查询单个菜单详情。
func (s *Service) MenuDetail(ctx context.Context, req *admindto.MenuDetailReq) (res *admindto.MenuDetailResp, err error) {
	entity, err := s.mm.GetByID(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, errors.New(adminenums.ErrMenuNotFound)
	}

	return menuEntityToDetailResp(entity), nil
}

// MenuCreate 新建菜单。
func (s *Service) MenuCreate(ctx context.Context, req *admindto.MenuCreateReq) error {
	req.Component = strings.TrimSpace(req.Component)
	if err := validateComponentBinding(req.Type, req.Component); err != nil {
		return err
	}
	if err := s.validatePermissionBinding(req.Type, req.PermissionCode, ctx); err != nil {
		return err
	}

	entity := &adminmodel.MenuEntity{
		Title:       req.Title,
		TitleKey:    req.TitleKey,
		ParentID:    req.ParentID,
		Type:        req.Type,
		Path:        req.Path,
		Component:   req.Component,
		ExternalURL: req.ExternalURL,
		Icon:        req.Icon,
		Status:      menuDefaultStatus(req.Status),
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

// MenuUpdate 更新菜单。
func (s *Service) MenuUpdate(ctx context.Context, req *admindto.MenuUpdateReq) error {
	entity, err := s.mm.GetByID(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity == nil {
		return errors.New(adminenums.ErrMenuNotFound)
	}

	// 系统内置菜单不允许修改类型
	if entity.IsSystem == 1 && entity.Type != req.Type {
		return errors.New(adminenums.ErrMenuIsSystem)
	}

	// 防止父子环
	if req.ParentID == req.ID {
		return errors.New(adminenums.ErrMenuCircle)
	}
	if req.ParentID != entity.ParentID && req.ParentID != 0 {
		if err := s.menuCheckCircle(ctx, req.ID, req.ParentID); err != nil {
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
	entity.TitleKey = req.TitleKey
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

// MenuDelete 批量删除菜单（软删除）。
func (s *Service) MenuDelete(ctx context.Context, req *admindto.MenuDeleteReq) error {
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
			return errors.New(adminenums.ErrMenuIsSystem)
		}
		// 检查是否有子菜单
		childCount, err := s.mm.CountByParentID(ctx, id)
		if err != nil {
			return err
		}
		if childCount > 0 {
			return errors.New(adminenums.ErrMenuHasChildren)
		}
	}

	_, err := s.mm.SoftDelete(ctx, req.IDs)
	return err
}

// component 兼容三种格式：
//   - soybean 目录：layout.base
//   - soybean 叶子页面：view.xxx
//   - 旧 vue-pure-admin 路径：/src/views/xxx/index.vue
var componentPathPattern = regexp.MustCompile(`^(?:layout\.(?:base|blank)|view\.[A-Za-z0-9_-]+|/src/views/(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9_-]+\.vue)$`)

// validateComponentBinding 校验菜单类型与前端组件路径的绑定关系。
func validateComponentBinding(menuType int, component string) error {
	if menuType != adminmodel.MenuTypeMenu {
		if component != "" {
			return errors.New(adminenums.ErrComponentNotAllowed)
		}
		return nil
	}
	if component == "" {
		return errors.New(adminenums.ErrComponentRequired)
	}
	if !componentPathPattern.MatchString(component) {
		return errors.New(adminenums.ErrComponentInvalid)
	}
	return nil
}

// validatePermissionBinding 校验 type 与 permission_code 的绑定约束。
func (s *Service) validatePermissionBinding(menuType int, code string, ctx context.Context) error {
	// 目录、iframe、外链不得绑定权限
	if menuType == adminmodel.MenuTypeDirectory ||
		menuType == adminmodel.MenuTypeIframe ||
		menuType == adminmodel.MenuTypeExternal {
		if code != "" {
			return errors.New(adminenums.ErrCodeNotBindable)
		}
		return nil
	}

	// 菜单和按钮必须绑定权限
	if menuType == adminmodel.MenuTypeMenu || menuType == adminmodel.MenuTypeButton {
		if code == "" {
			return errors.New(adminenums.ErrCodeRequired)
		}
		// 校验 code 存在且启用（同包直调）
		ok, err := s.ExistsEnabledCode(ctx, code)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New(adminenums.ErrCodeNotEnabled)
		}
	}
	return nil
}

// menuCheckCircle 检查将 targetID 的 parent 设为 newParentID 是否形成环。
func (s *Service) menuCheckCircle(ctx context.Context, targetID, newParentID uint64) error {
	// 向上追溯 newParentID 的祖先链
	cursor := newParentID
	for cursor != 0 {
		if cursor == targetID {
			return errors.New(adminenums.ErrMenuCircle)
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

// menuDefaultStatus 返回默认启用状态（1），当传入 status 为 0 时使用。
func menuDefaultStatus(status int) int {
	if status == 0 {
		return adminmodel.MenuStatusEnabled
	}
	return status
}

// menuEntityToDetailResp MenuEntity 转 DetailResp。
func menuEntityToDetailResp(e *adminmodel.MenuEntity) *admindto.MenuDetailResp {
	code := ""
	if e.PermissionCode != nil {
		code = *e.PermissionCode
	}
	remark := ""
	if e.Remark != nil {
		remark = *e.Remark
	}
	return &admindto.MenuDetailResp{
		ID:             e.ID,
		PermissionCode: code,
		Title:          e.Title,
		TitleKey:       e.TitleKey,
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

// MenuTree 查询完整菜单树，支持 status/type/search 筛选。
func (s *Service) MenuTree(ctx context.Context, req *admindto.MenuTreeReq) ([]admindto.MenuTreeNode, error) {
	all, err := s.mm.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	// 内存过滤
	filtered := make([]adminmodel.MenuEntity, 0, len(all))
	for _, m := range all {
		if req.Status != nil && m.Status != *req.Status {
			continue
		}
		if req.Type != nil && m.Type != *req.Type {
			continue
		}
		if req.Search != "" {
			if !strings.Contains(m.Title, req.Search) {
				continue
			}
		}
		filtered = append(filtered, m)
	}

	return buildMenuTree(filtered), nil
}

// buildMenuTree 将扁平列表构建为树结构。
//
// 递归构建：先完整构建子树，再组装父节点——Children 是值切片
// （[]MenuTreeNode），若用「roots 值拷贝 + nodeMap 指针挂载」的两遍组装，
// 顶级节点副本的 Children 恒为空（子节点挂在指针上，拷贝时未完成），
// 整棵菜单树会丢失全部子树。
func buildMenuTree(list []adminmodel.MenuEntity) []admindto.MenuTreeNode {
	childrenOf := make(map[uint64][]adminmodel.MenuEntity, len(list))
	ids := make(map[uint64]bool, len(list))
	for _, m := range list {
		childrenOf[m.ParentID] = append(childrenOf[m.ParentID], m)
		ids[m.ID] = true
	}
	// 孤儿节点（ParentID 非 0 但父不在列表中）归入顶级分组，与旧行为一致。
	var orphans []adminmodel.MenuEntity
	for _, m := range list {
		if m.ParentID != 0 && !ids[m.ParentID] {
			orphans = append(orphans, m)
		}
	}
	childrenOf[0] = append(childrenOf[0], orphans...)

	var build func(parentID uint64) []admindto.MenuTreeNode
	build = func(parentID uint64) []admindto.MenuTreeNode {
		var nodes []admindto.MenuTreeNode
		for _, m := range childrenOf[parentID] {
			n := menuEntityToNode(m)
			n.Children = build(m.ID)
			nodes = append(nodes, n)
		}
		return nodes
	}
	return build(0)
}

// menuEntityToNode MenuEntity 转树节点 MenuTreeNode。
func menuEntityToNode(m adminmodel.MenuEntity) admindto.MenuTreeNode {
	code := ""
	if m.PermissionCode != nil {
		code = *m.PermissionCode
	}
	remark := ""
	if m.Remark != nil {
		remark = *m.Remark
	}
	titleKey := ""
	if m.TitleKey != nil {
		titleKey = *m.TitleKey
	}
	return admindto.MenuTreeNode{
		ID:             m.ID,
		PermissionCode: code,
		Title:          m.Title,
		TitleKey:       titleKey,
		ParentID:       m.ParentID,
		Type:           m.Type,
		Path:           m.Path,
		Component:      m.Component,
		ExternalURL:    m.ExternalURL,
		Icon:           m.Icon,
		Status:         m.Status,
		IsHidden:       m.IsHidden,
		IsPublic:       m.IsPublic,
		IsSystem:       m.IsSystem,
		SortOrder:      m.SortOrder,
		Remark:         remark,
	}
}
