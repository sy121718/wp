// Package contenttemplateservice contenttemplate 模块业务实现（0-A2）。
package contenttemplateservice

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	contentcontract "go_wp/internal/module/content/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
)

// Service contenttemplate 模块业务实现。
type Service struct {
	m *contenttemplatemodel.Model
}

// NewService 构造（model 注入，不持有 *gorm.DB）。
func NewService(m *contenttemplatemodel.Model) *Service { return &Service{m: m} }

// 编译期契约断言。
var _ contenttemplatecontract.ContentTemplateService = (*Service)(nil)

// Create 创建模板（初始 draft_version=1 并写入 version=1 快照）。
func (s *Service) Create(ctx context.Context, req *contenttemplatedto.CreateReq) (res *contenttemplatedto.TemplateResp, err error) {
	if req == nil || !contentcontract.IsValidType(req.EntityType) || req.Name == "" {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	// 校验 DraftDocument 是合法 Page Document 并规范化为存储字节。
	doc, err := validateDocument(req.DraftDocument)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	e := &contenttemplatemodel.TemplateEntity{
		ID: uuid.NewString(), Name: req.Name, EntityType: req.EntityType,
		DraftDocument: doc, DraftVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err = s.m.Create(ctx, e); err != nil {
		return nil, err
	}
	// 立即写不可变版本 1（保证 ResolveTemplate 总有 LatestVersion）。
	if err = s.m.CreateVersion(ctx, &contenttemplatemodel.VersionEntity{
		ID: uuid.NewString(), TemplateID: e.ID, Version: 1, Document: doc, CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// Update 更新模板（draft_version 递增并写新不可变版本）。
func (s *Service) Update(ctx context.Context, req *contenttemplatedto.UpdateReq) (res *contenttemplatedto.TemplateResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	doc, err := validateDocument(req.DraftDocument)
	if err != nil {
		return nil, err
	}
	e.DraftDocument = doc
	e.DraftVersion++ // 单调递增（版本号即不可变快照序号）
	e.UpdatedAt = time.Now().UTC()
	if err = s.m.Save(ctx, e); err != nil {
		return nil, err
	}
	if err = s.m.CreateVersion(ctx, &contenttemplatemodel.VersionEntity{
		ID: uuid.NewString(), TemplateID: e.ID, Version: e.DraftVersion, Document: doc, CreatedAt: e.UpdatedAt,
	}); err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// Get 按 ID 查询。
func (s *Service) Get(ctx context.Context, req *contenttemplatedto.GetReq) (res *contenttemplatedto.TemplateResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(contenttemplateenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return toResp(e), nil
}

// List 按类型列表。
func (s *Service) List(ctx context.Context, req *contenttemplatedto.ListReq) (list []*contenttemplatedto.TemplateResp, err error) {
	if req == nil {
		req = &contenttemplatedto.ListReq{}
	}
	if req.EntityType != "" && !contentcontract.IsValidType(req.EntityType) {
		return nil, errors.New(contenttemplateenums.ErrInvalidType)
	}
	rows, err := s.m.List(ctx, req.EntityType)
	if err != nil {
		return nil, err
	}
	out := make([]*contenttemplatedto.TemplateResp, 0, len(rows))
	for _, r := range rows {
		out = append(out, toResp(r))
	}
	return out, nil
}

// ResolveTemplate 取 entityType 的当前激活模板版本（presentation 派生
// DocumentSnapshot 的唯一入口）。逻辑：
//  1. 取该类型最新的模板（updated_at 倒序首条），无则 ErrNotFound；
//  2. 取该模板最新版本（LatestVersion）的 document；
//  3. 组装 ResolvedTemplate{VersionID, Version, EntityType, Document}。
func (s *Service) ResolveTemplate(ctx context.Context, entityType string) (res *contenttemplatecontract.ResolvedTemplate, err error) {
	if entityType == "" || !contentcontract.IsValidType(entityType) {
		return nil, errors.New(contenttemplateenums.ErrInvalidType)
	}
	rows, err := s.m.List(ctx, entityType)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New(contenttemplateenums.ErrNotFound)
	}
	tpl := rows[0]
	ver, err := s.m.LatestVersion(ctx, tpl.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(contenttemplateenums.ErrNotFound)
		}
		return nil, err
	}
	return &contenttemplatecontract.ResolvedTemplate{
		VersionID:  ver.ID,
		Version:    ver.Version,
		EntityType: tpl.EntityType,
		Document:   ver.Document,
	}, nil
}

// validateDocument 解析并校验 Page Document，返回规范化存储字节。
func validateDocument(raw json.RawMessage) (json.RawMessage, error) {
	page, err := builder.ParsePage(raw)
	if err != nil {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	if err = builder.ValidatePage(page); err != nil {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	// 重新编码保证存储 JSON 的规范格式，不接受散乱字节。
	doc, err := json.Marshal(page)
	if err != nil {
		return nil, errors.New(contenttemplateenums.ErrDataInvalid)
	}
	return doc, nil
}

// toResp 实体 → 响应。
func toResp(e *contenttemplatemodel.TemplateEntity) *contenttemplatedto.TemplateResp {
	return &contenttemplatedto.TemplateResp{
		ID:            e.ID,
		Name:          e.Name,
		EntityType:    e.EntityType,
		DraftVersion:  e.DraftVersion,
		DraftDocument: e.DraftDocument,
		UpdatedAt:     e.UpdatedAt.Format("2006-01-02 15:04"),
	}
}
