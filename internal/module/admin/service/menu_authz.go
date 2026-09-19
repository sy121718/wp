package adminservice

import (
	"context"
	"fmt"
	"sort"

	admindto "go_wp/internal/module/admin/dto"
	adminmodel "go_wp/internal/module/admin/model"
	"go_wp/pkg/i18n"
)

// GetPermissionCodesByIDs 根据 menu_id 列表收集 permission_code 并去重。
//
// 判据是「这个节点有没有权限码」，**不按菜单类型白名单过滤**：
//   - 目录（type=1）本来就没有权限码（迁移 224 重建的 7 个目录全为空），会被自然跳过；
//   - iframe / 外链（type=4/5）如果配了码，同样应当生效。此前写死 type=2/3 会让它们
//     「勾了保存后静默消失」—— 反查路径 ListByPermissionCodes 并不过滤类型，于是它们
//     出现在已勾选列表里、看起来已授权，一保存又被丢掉，表现为「配置随机丢失」。
//
// 调用方传进来的 id 里混着目录、不存在的 id 都是常态（前端提交的是整棵勾选树）：
// ListByIDs 只回存在的行，其余自然被忽略，不需要在这里做额外校验。
func (s *Service) GetPermissionCodesByIDs(ctx context.Context, menuIDs []uint64) ([]string, error) {
	menus, err := s.mm.ListByIDs(ctx, menuIDs)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	var codes []string
	for _, m := range menus {
		if m.PermissionCode != nil && *m.PermissionCode != "" {
			if _, ok := seen[*m.PermissionCode]; !ok {
				seen[*m.PermissionCode] = struct{}{}
				codes = append(codes, *m.PermissionCode)
			}
		}
	}
	return codes, nil
}

// withAncestorMenuIDs 向上补齐祖先 id（目录与父菜单），返回去重且升序的集合。
//
// 为什么必须补（「只勾了按钮、没勾它所属的菜单」这个陷阱的根因）：
//   - 反查路径（GetIDsByPermissionCodes）只按 permission_code 命中节点，而**目录没有码**，
//     所以目录永远不会出现在已勾选列表里 —— 重新打开分配页时目录一律显示未勾选，
//     看起来像「上次勾的目录没保存」；
//   - 授权一个按钮（type=3）时，它的父菜单（type=2）不会被带上，该角色拿到的是
//     「API 能调、侧栏没有入口」的分裂状态：按钮的权限点在 Casbin 里是通的，
//     而 BuildAuthorizedTree 按 permission_code 过滤菜单，父菜单的码不在 codes 里，
//     于是侧栏根本不生成这个入口。
//
// 让「子节点被授权 ⇒ 祖先全部在集合里」成为不变式，读取与保存两端共用同一个实现。
//
// 语义后果（刻意，写下来避免后来者当成 bug 修）：补齐会**连带授予祖先菜单的权限码**。
// 这是「勾了按钮就必须能看到入口」的必然代价。反方向的联动（勾菜单自动勾全部子按钮）
// 才是提权 —— 一次「页面管理」会顺手给出「删除页面 / 发布页面」，本项目不做。
//
// 不存在的 id 会被丢弃（byID 查不到即停），这同时充当保存入口的白名单过滤。
func withAncestorMenuIDs(all []adminmodel.MenuEntity, ids []uint64) []uint64 {
	if len(ids) == 0 {
		return nil
	}
	byID := make(map[uint64]adminmodel.MenuEntity, len(all))
	for _, m := range all {
		byID[m.ID] = m
	}

	seen := make(map[uint64]struct{}, len(ids))
	out := make([]uint64, 0, len(ids))
	for _, id := range ids {
		cursor := id
		// seen 兼作防环：脏数据（parent_id 成环）时上溯会立刻停下，不死循环。
		for cursor != 0 {
			if _, ok := seen[cursor]; ok {
				break
			}
			m, ok := byID[cursor]
			if !ok {
				break
			}
			seen[cursor] = struct{}{}
			out = append(out, cursor)
			cursor = m.ParentID
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// buildPermissionTree 构建角色权限分配树的节点集合：
// 启用节点 ∪ 已勾选节点 ∪ 它们的全部祖先。
//
// 为什么不是「只给启用节点」：角色可能先勾选、后被禁用的菜单。若树里没有它，
// 前端就渲染不出这个勾选，管理员一保存就把这条授权静默删掉（数据丢失、且没有任何提示）。
// 因此把禁用项与祖先一起保留在树里，由页面标灰呈现，保存时原样带回。
func buildPermissionTree(all []adminmodel.MenuEntity, checkedIDs []uint64) []admindto.MenuTreeNode {
	byID := make(map[uint64]adminmodel.MenuEntity, len(all))
	for _, m := range all {
		byID[m.ID] = m
	}

	keep := make(map[uint64]struct{}, len(all))
	for _, m := range all {
		if m.Status == adminmodel.MenuStatusEnabled {
			keep[m.ID] = struct{}{}
		}
	}
	for _, id := range checkedIDs {
		keep[id] = struct{}{}
	}

	// 补祖先用显式队列，不在 range map 的同时增删（Go 允许，但迭代结果不确定）。
	queue := make([]uint64, 0, len(keep))
	for id := range keep {
		queue = append(queue, id)
	}
	for i := 0; i < len(queue); i++ {
		m, ok := byID[queue[i]]
		if !ok || m.ParentID == 0 {
			continue
		}
		if _, exists := keep[m.ParentID]; exists {
			continue
		}
		keep[m.ParentID] = struct{}{}
		queue = append(queue, m.ParentID)
	}

	// all 已按 sort_order, id 排序，过滤后顺序保持，因此建出来的树是确定的。
	filtered := make([]adminmodel.MenuEntity, 0, len(keep))
	for _, m := range all {
		if _, ok := keep[m.ID]; ok {
			filtered = append(filtered, m)
		}
	}
	return buildMenuTree(filtered)
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
	all, err := s.listEnabledMenusCached(ctx)
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
	all, err := s.listEnabledMenusCached(ctx)
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
