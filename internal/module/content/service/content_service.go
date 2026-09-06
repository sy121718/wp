// Package contentservice content 模块业务实现（0-A2）。
package contentservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
	contentmodel "go_wp/internal/module/content/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Service content 模块业务实现。
type Service struct {
	m *contentmodel.Model
}

// NewService 构造（model 注入，不持有 *gorm.DB）。
func NewService(m *contentmodel.Model) *Service { return &Service{m: m} }

// 编译期契约断言。
var _ contentcontract.ContentService = (*Service)(nil)

// Create 新建内容实体（revision=1）。
func (s *Service) Create(ctx context.Context, req *contentdto.CreateReq) (res *contentdto.ContentResp, err error) {
	if req == nil || !contentcontract.IsValidType(req.EntityType) || req.Slug == "" {
		return nil, errors.New(contentenums.ErrInvalidParam)
	}
	// slug 唯一性（同类型内）。
	if _, gerr := s.m.GetBySlug(ctx, req.EntityType, req.Slug); gerr == nil {
		return nil, errors.New(contentenums.ErrSlugTaken)
	} else if !errors.Is(gerr, gorm.ErrRecordNotFound) {
		return nil, gerr
	}
	// data 字段白名单校验（不变量 4：只接受白名单字段）。
	data, err := validateData(req.EntityType, req.Data)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(data)
	now := time.Now().UTC()
	e := &contentmodel.Entity{
		ID: uuid.NewString(), EntityType: req.EntityType, Slug: req.Slug,
		Revision: 1, Data: raw, CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.Create(ctx, e); err != nil {
		return nil, err
	}
	return toResp(e, data), nil
}

// Update 更新内容（revision 递增）。
func (s *Service) Update(ctx context.Context, req *contentdto.UpdateReq) (res *contentdto.ContentResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(contentenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contentenums.ErrNotFound)
		}
		return nil, err
	}
	data, err := validateData(e.EntityType, req.Data)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(data)
	e.Data = raw
	e.Revision++ // 单调递增（内容变更触发依赖追踪）
	e.UpdatedAt = time.Now().UTC()
	if err = s.m.Save(ctx, e); err != nil {
		return nil, err
	}
	return toResp(e, data), nil
}

// Get 按 ID 查询。
func (s *Service) Get(ctx context.Context, req *contentdto.GetReq) (res *contentdto.ContentResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(contentenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contentenums.ErrNotFound)
		}
		return nil, err
	}
	data := map[string]any{}
	_ = json.Unmarshal(e.Data, &data)
	return toResp(e, data), nil
}

// List 按类型分页列表。
func (s *Service) List(ctx context.Context, req *contentdto.ListReq) (list []*contentdto.ContentResp, err error) {
	if req == nil {
		req = &contentdto.ListReq{}
	}
	if req.EntityType != "" && !contentcontract.IsValidType(req.EntityType) {
		return nil, errors.New(contentenums.ErrInvalidType)
	}
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 20
	}
	rows, err := s.m.List(ctx, req.EntityType, req.Limit, req.Offset)
	if err != nil {
		return nil, err
	}
	out := make([]*contentdto.ContentResp, 0, len(rows))
	for _, r := range rows {
		data := map[string]any{}
		_ = json.Unmarshal(r.Data, &data)
		out = append(out, toResp(r, data))
	}
	return out, nil
}

// Delete 删除实体。
func (s *Service) Delete(ctx context.Context, req *contentdto.DeleteReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(contentenums.ErrInvalidParam)
	}
	if _, err = s.m.Get(ctx, req.ID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(contentenums.ErrNotFound)
		}
		return err
	}
	return s.m.Delete(ctx, req.ID)
}

// validateData 字段白名单校验（不变量 4：拒绝白名单外字段，防夹带）。
func validateData(entityType string, data map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(data))
	for key, val := range data {
		if !contentcontract.IsValidField(entityType, key) {
			return nil, fmt.Errorf("%s: %q", contentenums.ErrInvalidField, key)
		}
		out[key] = val
	}
	return out, nil
}

// toResp 实体 → 响应。
func toResp(e *contentmodel.Entity, data map[string]any) *contentdto.ContentResp {
	return &contentdto.ContentResp{
		ID: e.ID, EntityType: e.EntityType, Slug: e.Slug,
		Revision: e.Revision, Data: data,
		UpdatedAt: e.UpdatedAt.Format("2006-01-02 15:04"),
	}
}
