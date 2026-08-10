// Package menuservice 菜单模块业务逻辑实现。
package menuservice

import (
	"context"
	"fmt"

	menucontract "go_wp/internal/module/menu/contract"
	menudto "go_wp/internal/module/menu/dto"
	menumodel "go_wp/internal/module/menu/model"
)

// GetPermissionCodesByIDs 根据 menu_id 列表收集 type=2/3 的 permission_code 并去重。
func (s *Service) GetPermissionCodesByIDs(ctx context.Context, menuIDs []uint64) ([]string, error) {
	menus, err := s.mm.ListByIDs(ctx, menuIDs)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	var codes []string
	for _, m := range menus {
		if m.Type != menumodel.MenuTypeMenu && m.Type != menumodel.MenuTypeButton {
			continue
		}
		if m.PermissionCode != nil && *m.PermissionCode != "" {
			if _, ok := seen[*m.PermissionCode]; !ok {
				seen[*m.PermissionCode] = struct{}{}
				codes = append(codes, *m.PermissionCode)
			}
		}
	}
	return codes, nil
}

// GetIDsByPermissionCodes 根据 permission_code 列表反查 menu_id。
func (s *Service) GetIDsByPermissionCodes(ctx context.Context, codes []string) ([]uint64, error) {
	menus, err := s.mm.ListByPermissionCodes(ctx, codes)
	if err != nil {
		return nil, err
	}

	var ids []uint64
	for _, m := range menus {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// CountByPermissionCodes 统计引用指定权限编码的未删除菜单数。
func (s *Service) CountByPermissionCodes(ctx context.Context, codes []string) (count int64, err error) {
	return s.mm.CountByPermissionCodes(ctx, codes)
}

// BuildAuthorizedTree 根据有效 permission_code 列表构建用户可见菜单树。
// 自动补齐祖先目录，只包含 type=1（目录）和 type=2（菜单）。
func (s *Service) BuildAuthorizedTree(ctx context.Context, codes []string) ([]menudto.TreeNode, error) {
	all, err := s.mm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	return buildAuthorizedTree(all, codes), nil
}

func buildAuthorizedTree(all []menumodel.MenuEntity, codes []string) []menudto.TreeNode {
	codeSet := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		codeSet[c] = struct{}{}
	}

	// 收集启用且公开或已授权的 type=2 菜单
	visibleIDs := make(map[uint64]struct{})
	for _, m := range all {
		if m.Status != menumodel.MenuStatusEnabled || m.Type != menumodel.MenuTypeMenu {
			continue
		}
		if m.IsPublic == 1 {
			visibleIDs[m.ID] = struct{}{}
			continue
		}
		if m.PermissionCode != nil {
			if _, ok := codeSet[*m.PermissionCode]; ok {
				visibleIDs[m.ID] = struct{}{}
			}
		}
	}

	// 补齐启用的祖先目录
	byID := make(map[uint64]menumodel.MenuEntity, len(all))
	for _, m := range all {
		if m.Status == menumodel.MenuStatusEnabled {
			byID[m.ID] = m
		}
	}

	for id := range visibleIDs {
		cursor := byID[id].ParentID
		for cursor != 0 {
			parent, ok := byID[cursor]
			if !ok {
				break
			}
			if _, exists := visibleIDs[cursor]; exists {
				break
			}
			visibleIDs[cursor] = struct{}{}
			cursor = parent.ParentID
		}
	}

	// 过滤出可见节点（只含目录和菜单，排除按钮/iframe/外链）
	filtered := make([]menumodel.MenuEntity, 0, len(visibleIDs))
	for _, m := range all {
		if m.Type != menumodel.MenuTypeDirectory && m.Type != menumodel.MenuTypeMenu {
			continue
		}
		if _, ok := visibleIDs[m.ID]; ok {
			filtered = append(filtered, m)
		}
	}

	return buildTree(filtered)
}

// BuildAuthorizedRoutes 根据有效 permission_code 列表构建前端动态路由树。
func (s *Service) BuildAuthorizedRoutes(ctx context.Context, codes []string) (res []menucontract.RouteNode, err error) {
	all, err := s.mm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	codeSet := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		codeSet[code] = struct{}{}
	}
	buttonAuths := make(map[uint64][]string)
	for _, item := range all {
		if item.Status != menumodel.MenuStatusEnabled || item.Type != menumodel.MenuTypeButton || item.PermissionCode == nil {
			continue
		}
		if _, authorized := codeSet[*item.PermissionCode]; authorized {
			buttonAuths[item.ParentID] = append(buttonAuths[item.ParentID], *item.PermissionCode)
		}
	}

	tree := buildAuthorizedTree(all, codes)
	return buildRouteNodes(tree, buttonAuths), nil
}

func buildRouteNodes(nodes []menudto.TreeNode, buttonAuths map[uint64][]string) []menucontract.RouteNode {
	routes := make([]menucontract.RouteNode, 0, len(nodes))
	for _, node := range nodes {
		children := buildRouteNodes(node.Children, buttonAuths)
		auths := make([]string, 0, 1+len(buttonAuths[node.ID]))
		if node.PermissionCode != "" {
			auths = append(auths, node.PermissionCode)
		}
		auths = append(auths, buttonAuths[node.ID]...)
		route := menucontract.RouteNode{
			Path:      node.Path,
			Name:      fmt.Sprintf("Menu%d", node.ID),
			Component: node.Component,
			Meta: menucontract.RouteMeta{
				Title:    node.Title,
				Icon:     node.Icon,
				ShowLink: node.IsHidden == 0,
				Rank:     node.SortOrder,
				Auths:    auths,
			},
			Children: children,
		}
		if len(children) > 0 {
			route.Redirect = children[0].Path
		}
		routes = append(routes, route)
	}
	return routes
}
