package mediaservice

// media_category.go — 媒体库左树：无限级分类 CRUD 与附件元数据更新。
// 级联策略（对标 WP 分类删除语义）：有子级拒绝删除；有附件时附件移入未分类后删除。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	mediadto "go_wp/internal/module/media/dto"
	mediamodel "go_wp/internal/module/media/model"

	"gorm.io/gorm"
)

// --- 分类 CRUD ---

// CreateCategory 新建分类（同父级下重名拒绝；code 自动生成保证唯一索引）。
func (s *Service) CreateCategory(ctx context.Context, req *mediadto.CategoryCreateReq) (*mediadto.CategoryTreeNode, error) {
	name := strings.TrimSpace(req.CategoryName)
	if name == "" {
		return nil, errors.New("分类名称不能为空")
	}
	if req.ParentID > 0 {
		if _, err := s.cm.GetCategory(ctx, req.ParentID); err != nil {
			return nil, errors.New("父级分类不存在")
		}
	}
	siblings, err := s.cm.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range siblings {
		if c.ParentID == req.ParentID && strings.EqualFold(c.CategoryName, name) {
			return nil, errors.New("同级分类下已存在同名分类")
		}
	}
	now := time.Now()
	entity := &mediamodel.FileCategoryEntity{
		CategoryName: name,
		CategoryCode: fmt.Sprintf("cat_%d", now.UnixNano()),
		ParentID:     req.ParentID,
		SortOrder:    req.SortOrder,
		Status:       1,
		CreateTime:   &now,
		UpdateTime:   &now,
	}
	if err := s.cm.CreateCategory(ctx, entity); err != nil {
		return nil, err
	}
	return &mediadto.CategoryTreeNode{
		ID:           entity.ID,
		CategoryName: entity.CategoryName,
		CategoryCode: entity.CategoryCode,
		ParentID:     entity.ParentID,
		SortOrder:    entity.SortOrder,
		Children:     []mediadto.CategoryTreeNode{},
	}, nil
}

// UpdateCategory 更新分类（改名 / 移动父级 / 排序；移动防环：新父级不能是自己或自己的后代）。
func (s *Service) UpdateCategory(ctx context.Context, req *mediadto.CategoryUpdateReq) error {
	current, err := s.cm.GetCategory(ctx, req.ID)
	if err != nil {
		return errors.New("分类不存在")
	}
	// 查重基准必须是**移动后**的父级：一次请求同时改名 + 移动时按旧父级查重会漏检
	// 新父级下的同名分类（无 DB 唯一约束兜底），破坏「同父级唯一名」约束。
	newParent := current.ParentID
	if req.ParentID != nil {
		newParent = *req.ParentID
	}
	updates := map[string]any{"update_time": time.Now()}
	if req.CategoryName != nil && strings.TrimSpace(*req.CategoryName) != "" {
		name := strings.TrimSpace(*req.CategoryName)
		// 改名同级查重（与 CreateCategory 约束一致，排除自身）。
		siblings, err := s.cm.ListAll(ctx)
		if err != nil {
			return err
		}
		for _, c := range siblings {
			if c.ID != req.ID && c.ParentID == newParent && strings.EqualFold(c.CategoryName, name) {
				return errors.New("同级分类下已存在同名分类")
			}
		}
		updates["category_name"] = name
	}
	if req.SortOrder != nil {
		updates["sort_order"] = *req.SortOrder
	}
	if req.ParentID != nil {
		if newParent == req.ID {
			return errors.New("父级不能是自己")
		}
		if newParent > 0 {
			if _, err := s.cm.GetCategory(ctx, newParent); err != nil {
				return errors.New("目标父级分类不存在")
			}
			// 防环：newParent 不得位于以自己为根的子树内。
			all, err := s.cm.ListAll(ctx)
			if err != nil {
				return err
			}
			if isDescendant(all, req.ID, newParent) {
				return errors.New("不能移动到自己的子分类下")
			}
		}
		updates["parent_id"] = newParent
	}
	return s.cm.UpdateCategory(ctx, req.ID, updates)
}

// isDescendant 判断 target 是否位于 root 的子树内（all 为全量启用分类）。
func isDescendant(all []mediamodel.FileCategoryEntity, root, target uint64) bool {
	childrenOf := map[uint64][]uint64{}
	for _, c := range all {
		childrenOf[c.ParentID] = append(childrenOf[c.ParentID], c.ID)
	}
	stack := []uint64{root}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, child := range childrenOf[cur] {
			if child == target {
				return true
			}
			stack = append(stack, child)
		}
	}
	return false
}

// DeleteCategory 删除分类：有子级拒绝；有附件时附件移入未分类（category_id=NULL）。
func (s *Service) DeleteCategory(ctx context.Context, req *mediadto.CategoryDeleteReq) error {
	if _, err := s.cm.GetCategory(ctx, req.ID); err != nil {
		return errors.New("分类不存在")
	}
	hasChildren, err := s.cm.HasChildren(ctx, req.ID)
	if err != nil {
		return err
	}
	if hasChildren {
		return errors.New("请先删除或移动该分类下的子分类")
	}
	if err := s.am.DetachAttachments(ctx, req.ID); err != nil {
		return err
	}
	return s.cm.DeleteCategory(ctx, req.ID)
}

// --- 附件元数据更新 ---

// UpdateAttachment 更新附件（文件名 / 分类 / alt / 标题 / 描述；alt 等存 ExtraInfo JSON）。
func (s *Service) UpdateAttachment(ctx context.Context, req *mediadto.AttachmentUpdateReq) error {
	// 存在性校验（GetByID 带 status=1 过滤）。ExtraInfo 合并在 model 内以 SQL 原子完成，
	// 不再依赖这里读到的快照 —— 快照会与构建期 refs 写入竞态并互相覆盖。
	if _, err := s.am.GetByID(ctx, req.ID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("附件不存在")
		}
		return err
	}
	updates := map[string]any{"update_time": time.Now()}
	if req.FileName != nil && strings.TrimSpace(*req.FileName) != "" {
		updates["file_name"] = strings.TrimSpace(*req.FileName)
	}
	if req.CategoryID != nil {
		if *req.CategoryID > 0 {
			if _, err := s.cm.GetCategory(ctx, *req.CategoryID); err != nil {
				return errors.New("目标分类不存在")
			}
			updates["category_id"] = *req.CategoryID
		} else {
			updates["category_id"] = nil // 移入未分类
		}
	}
	// ExtraInfo 合并（alt/title/description）：只动这三个键，**不整列覆盖**。
	// 同列的 refs 是构建期写入的引用缓存，整列读-改-写会把它回退成旧值，
	// 导致删除保护失效（在用中的媒体被误删）。合并走 model 的 SQL 级 jsonb 操作。
	setExtra := map[string]any{}
	var delExtra []string
	for _, pair := range []struct {
		key string
		val *string
	}{{"alt", req.Alt}, {"title", req.Title}, {"description", req.Description}} {
		if pair.val == nil {
			continue
		}
		v := strings.TrimSpace(*pair.val)
		if v == "" {
			delExtra = append(delExtra, pair.key)
		} else {
			setExtra[pair.key] = v
		}
	}
	if len(setExtra) > 0 || len(delExtra) > 0 {
		if merr := s.am.MergeAttachmentExtra(ctx, req.ID, setExtra, delExtra); merr != nil {
			return merr
		}
	}
	return s.am.AttachmentUpdate(ctx, req.ID, updates)
}
