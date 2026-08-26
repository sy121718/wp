package adminservice

import (
	"context"
	"fmt"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/i18n"
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
		if m.Type != adminmodel.MenuTypeMenu && m.Type != adminmodel.MenuTypeButton {
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
func (s *Service) BuildAuthorizedTree(ctx context.Context, codes []string) ([]admindto.MenuTreeNode, error) {
	all, err := s.mm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	return buildAuthorizedTree(all, codes), nil
}

func buildAuthorizedTree(all []adminmodel.MenuEntity, codes []string) []admindto.MenuTreeNode {
	codeSet := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		codeSet[c] = struct{}{}
	}

	// 收集启用且公开或已授权的 type=2 菜单
	visibleIDs := make(map[uint64]struct{})
	for _, m := range all {
		if m.Status != adminmodel.MenuStatusEnabled || m.Type != adminmodel.MenuTypeMenu {
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
	byID := make(map[uint64]adminmodel.MenuEntity, len(all))
	for _, m := range all {
		if m.Status == adminmodel.MenuStatusEnabled {
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
	filtered := make([]adminmodel.MenuEntity, 0, len(visibleIDs))
	for _, m := range all {
		if m.Type != adminmodel.MenuTypeDirectory && m.Type != adminmodel.MenuTypeMenu {
			continue
		}
		if _, ok := visibleIDs[m.ID]; ok {
			filtered = append(filtered, m)
		}
	}

	return buildMenuTree(filtered)
}

// BuildAuthorizedRoutes 根据有效 permission_code 列表构建前端动态路由树。
// lang 为请求语言，菜单 title 按 title_key 翻译（未配置或未命中时回退 title 原文）。
func (s *Service) BuildAuthorizedRoutes(ctx context.Context, codes []string, lang string) (res []admindto.RouteNode, err error) {
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
		if item.Status != adminmodel.MenuStatusEnabled || item.Type != adminmodel.MenuTypeButton || item.PermissionCode == nil {
			continue
		}
		if _, authorized := codeSet[*item.PermissionCode]; authorized {
			buttonAuths[item.ParentID] = append(buttonAuths[item.ParentID], *item.PermissionCode)
		}
	}

	tree := buildAuthorizedTree(all, codes)
	return buildRouteNodes(tree, buttonAuths, "", lang), nil
}

// buildRouteNodes 递归构建动态路由树。
// parentName 用于拼接子路由 name（如 Menu200_Menu201），保持与 soybean 多级路由命名一致，
// 使子路由 component（view.xxx）能作为目录（layout.base）的 children 正常挂载。
func buildRouteNodes(nodes []admindto.MenuTreeNode, buttonAuths map[uint64][]string, parentName, lang string) []admindto.RouteNode {
	routes := make([]admindto.RouteNode, 0, len(nodes))
	for _, node := range nodes {
		children := buildRouteNodes(node.Children, buttonAuths, fmt.Sprintf("Menu%d", node.ID), lang)
		auths := make([]string, 0, 1+len(buttonAuths[node.ID]))
		if node.PermissionCode != "" {
			auths = append(auths, node.PermissionCode)
		}
		auths = append(auths, buttonAuths[node.ID]...)
		routeName := fmt.Sprintf("Menu%d", node.ID)
		if parentName != "" {
			routeName = parentName + "_" + routeName
		}
		route := admindto.RouteNode{
			Path:      node.Path,
			Name:      routeName,
			Component: node.Component,
			Meta: admindto.RouteMeta{
				Title:    translateMenuTitle(node.TitleKey, node.Title, lang),
				TitleKey: node.TitleKey,
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

// translateMenuTitle 菜单标题翻译：title_key 命中 i18n 资源则用翻译，否则回退 title 原文。
func translateMenuTitle(titleKey, title, lang string) string {
	if titleKey == "" {
		return title
	}
	if translated := i18n.GetText(titleKey, lang); translated != titleKey {
		return translated
	}
	return title
}
