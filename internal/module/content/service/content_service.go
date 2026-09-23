// Package contentservice content 模块业务实现（0-A2）。
package contentservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	contentcontract "go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	contentenums "go_wp/internal/module/content/enums"
	contentmodel "go_wp/internal/module/content/model"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/upload"

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
	entityType, err := normalizeListFilter(req)
	if err != nil {
		return nil, err
	}
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 20
	}
	rows, err := s.m.List(ctx, entityType, req.Limit, req.Offset)
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

// Count 按类型统计总数（后台文章列表的分页总数与总页数）。
//
// **与 List 共用同一个 normalizeListFilter**：过滤条件（这里是实体类型）各写一遍时，
// 抄漏的那一侧不报错 —— 只表现为总数与列表条数静默对不上，翻到最后一页才发现少了几条。
// 形状与 product 域的 CountBrands / CountProducts 一致：收同一个 ListReq，返回 (int64, error)。
//
// 与 CountForCollection 不是一回事：那是集合渲染路径的计数（带 data 字段等值过滤，
// 供组件集合翻页），本方法服务的是后台列表页的分页条。
func (s *Service) Count(ctx context.Context, req *contentdto.ListReq) (n int64, err error) {
	entityType, err := normalizeListFilter(req)
	if err != nil {
		return 0, err
	}
	return s.m.Count(ctx, entityType)
}

// normalizeListFilter 归一内容列表的过滤条件（List / Count 共用）。
//
// 空类型不校验（List 的历史语义是「不限类型」）；非空类型必须在白名单里 ——
// 与 List 原来的两行校验逐字一致，只是搬到了共用助手，避免两处各写一遍后分叉。
func normalizeListFilter(req *contentdto.ListReq) (entityType string, err error) {
	if req == nil {
		return "", nil
	}
	if req.EntityType != "" && !contentcontract.IsValidType(req.EntityType) {
		return "", errors.New(contentenums.ErrInvalidType)
	}
	return req.EntityType, nil
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

// normalizableMediaFields 各内容类型里「取自媒体库」的字段（值为单个 URL）。
//
// 与字段白名单一样单处定义：写入口与读出口必须共用同一份清单，各写一份迟早分叉 ——
// 分叉的表现是「保存后是完整链接、读回来又变回相对路径」。
var normalizableMediaFields = map[string]map[string]bool{
	"article": {"featuredImage": true},
}

// normalizeMediaFields 就地把媒体字段归一到**对外可用的完整链接**。
//
// 媒体库给出的是 upload.base_url 前缀下的地址（未配置时是 "/storage/<id>.<ext>"），
// 而这里要的是「谁能直接拿去用」的那一份。写入口归一一次（新数据天然是完整链接），
// 读出口再归一一次（存量相对值也能显示对）—— 两侧都过同一个 upload.StorageURL，
// 换域名时全站跟着配置走，不需要回填历史数据。
//
// 非字符串与空值原样保留：空串表示「没配图」，改写成 "/storage/" 这种半截地址
// 会让调用方的「有没有配图」判断失真。
func normalizeMediaFields(entityType string, data map[string]any) map[string]any {
	fields := normalizableMediaFields[entityType]
	if len(fields) == 0 || data == nil {
		return data
	}
	for key := range fields {
		v, ok := data[key].(string)
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		data[key] = upload.StorageURL(v)
	}
	return data
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
	return normalizeMediaFields(entityType, out), nil
}

// toResp 实体 → 响应。
//
// 读出口同样过一遍归一：存量行（相对路径入库）与手工写进库的值都能显示成完整链接。
func toResp(e *contentmodel.Entity, data map[string]any) *contentdto.ContentResp {
	return &contentdto.ContentResp{
		ID: e.ID, EntityType: e.EntityType, Slug: e.Slug,
		Revision: e.Revision, Data: normalizeMediaFields(e.EntityType, data),
		UpdatedAt: e.UpdatedAt.Format("2006-01-02 15:04"),
	}
}
