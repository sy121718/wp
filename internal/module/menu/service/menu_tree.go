// Package menuservice 菜单模块业务逻辑实现。
package menuservice

import (
	"context"
	"strings"

	menudto "go_wp/internal/module/menu/dto"
	menumodel "go_wp/internal/module/menu/model"
)

// Tree 查询完整菜单树，支持 status/type/search 筛选。
func (s *Service) Tree(ctx context.Context, req *menudto.TreeReq) ([]menudto.TreeNode, error) {
	all, err := s.mm.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	// 内存过滤
	filtered := make([]menumodel.MenuEntity, 0, len(all))
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

	return buildTree(filtered), nil
}

// buildTree 将扁平列表构建为树结构。
func buildTree(list []menumodel.MenuEntity) []menudto.TreeNode {
	nodeMap := make(map[uint64]*menudto.TreeNode, len(list))
	var roots []menudto.TreeNode

	// 第一遍：创建所有节点
	for _, m := range list {
		node := entityToNode(m)
		nodeMap[m.ID] = &node
	}

	// 第二遍：组装父子关系
	for _, m := range list {
		node := nodeMap[m.ID]
		if m.ParentID == 0 {
			roots = append(roots, *node)
		} else {
			if parent, ok := nodeMap[m.ParentID]; ok {
				parent.Children = append(parent.Children, *node)
			} else {
				// 父节点不在列表中（被过滤），作为顶级
				roots = append(roots, *node)
			}
		}
	}

	return roots
}

// entityToNode MenuEntity 转树节点 TreeNode。
func entityToNode(m menumodel.MenuEntity) menudto.TreeNode {
	code := ""
	if m.PermissionCode != nil {
		code = *m.PermissionCode
	}
	remark := ""
	if m.Remark != nil {
		remark = *m.Remark
	}
	return menudto.TreeNode{
		ID:             m.ID,
		PermissionCode: code,
		Title:          m.Title,
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