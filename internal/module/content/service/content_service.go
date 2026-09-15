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
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Service content 模块业务实现。
type Service struct {
	m *contentmodel.Model
	// invalidator 依赖失效扇出端口（PIPE-3，编排层注入，可空）。
	// 为空时内容写入行为与本轮之前完全一致（不触发任何失效）。
	invalidator contentcontract.DependencyInvalidator
	// contentStore 内容译文存储（审计 I18N-006，装配期注入，可空）：
	// 构建期按 lang 批量取译文；为空即不做翻译（产物与接入前逐字一致）。
	contentStore i18n.ContentStore
}

// SetDependencyInvalidator 注入依赖失效扇出端口（编排层装配；可空）。
func (s *Service) SetDependencyInvalidator(inv contentcontract.DependencyInvalidator) {
	s.invalidator = inv
}

// notifyContentChanged 内容实体变更后推导依赖源键并交给扇出端口。
//
// 两条键（docs/03-pipeline.md §8.2 典型 fan-out）：
//   - direct_content:{type}:{id}       —— 直接引用该实体的产物；
//   - content_collection:collection:content:{type}
//     —— 渲染该类型集合的产物（新增/删除成员时旧产物里还没有该实体，
//     只能靠集合键失效，这是「新增实体也要让列表页更新」的关键）。
//
// 失败一律降级：内容已经写入成功，失效标记失败不能反向让写入报错。
func (s *Service) notifyContentChanged(ctx context.Context, e *contentmodel.Entity) {
	if s == nil || s.invalidator == nil || e == nil {
		return
	}
	for _, k := range []pipeline.DepKey{
		pipeline.DirectContentKey(e.EntityType, e.ID),
		pipeline.ContentCollectionKey(e.EntityType),
	} {
		s.invalidator.Invalidate(ctx, k.Kind, k.Key)
	}
}

// NewService 构造（model 注入，不持有 *gorm.DB）。
func NewService(m *contentmodel.Model) *Service { return &Service{m: m} }

// 编译期契约断言。
var _ contentcontract.ContentService = (*Service)(nil)

// 构建期数据源（issue #35）：组件取内容数据只走这个受限接口，写方法不在它上面。
var _ contentcontract.ContentDataSource = (*Service)(nil)

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
	// 新增实体同样要失效：集合键让「列表页出现新条目」，实体键覆盖「先建实例后建内容」的极端顺序。
	s.notifyContentChanged(ctx, e)
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
	s.notifyContentChanged(ctx, e)
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
	if err = json.Unmarshal(e.Data, &data); err != nil {
		return nil, fmt.Errorf("%s: %w", contentenums.ErrDataInvalid, err)
	}
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
		if uerr := json.Unmarshal(r.Data, &data); uerr != nil {
			// 单行数据损坏：跳过该行并记 Warn，不阻塞整列表。
			logger.Scene("content").With("id", r.ID).With("err", uerr).Warn("内容数据解析失败，跳过该行")
			continue
		}
		out = append(out, toResp(r, data))
	}
	return out, nil
}

// Delete 删除实体。
func (s *Service) Delete(ctx context.Context, req *contentdto.DeleteReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(contentenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(contentenums.ErrNotFound)
		}
		return err
	}
	if err = s.m.Delete(ctx, req.ID); err != nil {
		return err
	}
	// 删除后仍要失效：产物里还留着这个实体的字面量，且集合少了一个成员。
	s.notifyContentChanged(ctx, e)
	return nil
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
