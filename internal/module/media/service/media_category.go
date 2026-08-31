package mediaservice

// media_category.go — 媒体库左树：无限级分类 CRUD 与附件元数据更新。
// 级联策略（对标 WP 分类删除语义）：有子级拒绝删除；有附件时附件移入未分类后删除。

import (
	"context"
	"encoding/json"
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
	if _, err := s.cm.GetCategory(ctx, req.ID); err != nil {
		return errors.New("分类不存在")
	}
	updates := map[string]any{"update_time": time.Now()}
	if req.CategoryName != nil && strings.TrimSpace(*req.CategoryName) != "" {
		updates["category_name"] = strings.TrimSpace(*req.CategoryName)
	}
	if req.SortOrder != nil {
		updates["sort_order"] = *req.SortOrder
	}
	if req.ParentID != nil {
		newParent := *req.ParentID
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
	e, err := s.am.GetByID(ctx, req.ID)
	if err != nil {
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
	// ExtraInfo JSON 合并（alt/title/description）。
	extra := map[string]any{}
	if e.ExtraInfo != nil && *e.ExtraInfo != "" {
		_ = json.Unmarshal([]byte(*e.ExtraInfo), &extra)
	}
	changed := false
	for _, pair := range []struct {
		key string
		val *string
	}{{"alt", req.Alt}, {"title", req.Title}, {"description", req.Description}} {
		if pair.val != nil {
			v := strings.TrimSpace(*pair.val)
			if v == "" {
				delete(extra, pair.key)
			} else {
				extra[pair.key] = v
			}
			changed = true
		}
	}
	if changed {
		raw, err := json.Marshal(extra)
		if err != nil {
			return err
		}
		updates["extra_info"] = string(raw)
	}
	return s.am.AttachmentUpdate(ctx, req.ID, updates)
}
