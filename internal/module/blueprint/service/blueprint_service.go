// Package blueprintservice blueprint 模块业务实现（0-B）。
package blueprintservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	blueprintcontract "go_wp/internal/module/blueprint/contract"
	blueprintdto "go_wp/internal/module/blueprint/dto"
	blueprintenums "go_wp/internal/module/blueprint/enums"
	blueprintmodel "go_wp/internal/module/blueprint/model"
)

// Service blueprint 模块业务实现。
type Service struct {
	m *blueprintmodel.Model
}

// NewService 构造（model 注入，不持有 *gorm.DB）。
func NewService(m *blueprintmodel.Model) *Service { return &Service{m: m} }

// 编译期契约断言。
var _ blueprintcontract.BlueprintService = (*Service)(nil)

// Create 创建 Blueprint（校验 Kind 白名单 + 文档合法 → draft_version=1 + 版本 1）。
func (s *Service) Create(ctx context.Context, req *blueprintdto.CreateReq) (res *blueprintdto.BlueprintResp, err error) {
	if req == nil || req.Name == "" {
		return nil, errors.New(blueprintenums.ErrInvalidParam)
	}
	if !blueprintmodel.IsValidKind(req.Kind) {
		return nil, errors.New(blueprintenums.ErrInvalidKind)
	}
	// 校验 DraftDocument 是合法 Page Document 并规范化为存储字节。
	doc, err := validateDocument(req.DraftDocument)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	e := &blueprintmodel.BlueprintEntity{
		ID: uuid.NewString(), Name: req.Name, Kind: req.Kind,
		DraftDocument: doc, DraftVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	// 草稿行 + 首个不可变版本原子写入（分步写失败会留下没有 LatestVersion 的 Blueprint）。
	if err = s.m.CreateWithVersion(ctx, e, &blueprintmodel.VersionEntity{
		ID: uuid.NewString(), BlueprintID: e.ID, Version: 1, Document: doc, CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// Update 修改草稿（draft_version 递增并写新不可变版本）。
func (s *Service) Update(ctx context.Context, req *blueprintdto.UpdateReq) (res *blueprintdto.BlueprintResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(blueprintenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(blueprintenums.ErrNotFound)
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
	// 草稿更新 + 新版本行原子写入（分步写会在版本行失败时留下「草稿已改、版本缺失」）。
	if err = s.m.SaveWithVersion(ctx, e, &blueprintmodel.VersionEntity{
		ID: uuid.NewString(), BlueprintID: e.ID, Version: e.DraftVersion, Document: doc, CreatedAt: e.UpdatedAt,
	}); err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// Publish 发布：基于当前 draft_document 生成不可变版本（版本号 = draft_version）。
// MVP 简化：Create/Update 已同步写入同号版本，此处幂等复用该快照，避免重复
// 写入违反 (blueprint_id, version) 唯一约束；仅在异常缺失同号版本时补写。
func (s *Service) Publish(ctx context.Context, req *blueprintdto.PublishReq) (res *blueprintdto.BlueprintResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(blueprintenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(blueprintenums.ErrNotFound)
		}
		return nil, err
	}
	// 幂等：当前 draft 已有版本号=draft_version 的快照则直接复用。
	if _, err = s.m.GetVersion(ctx, e.ID, e.DraftVersion); err == nil {
		return toResp(e), nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	// 理论上不出现：基于当前草稿直接 CreateVersion(版本号 = draft_version)。
	now := time.Now().UTC()
	if err = s.m.CreateVersion(ctx, &blueprintmodel.VersionEntity{
		ID: uuid.NewString(), BlueprintID: e.ID, Version: e.DraftVersion, Document: e.DraftDocument, CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	return toResp(e), nil
}

// Get 按 ID 查询。
func (s *Service) Get(ctx context.Context, req *blueprintdto.GetReq) (res *blueprintdto.BlueprintResp, err error) {
	if req == nil || req.ID == "" {
		return nil, errors.New(blueprintenums.ErrInvalidParam)
	}
	e, err := s.m.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(blueprintenums.ErrNotFound)
		}
		return nil, err
	}
	return toResp(e), nil
}

// List 按 kind 列表。
func (s *Service) List(ctx context.Context, req *blueprintdto.ListReq) (list []*blueprintdto.BlueprintResp, err error) {
	if req == nil {
		req = &blueprintdto.ListReq{}
	}
	if req.Kind != "" && !blueprintmodel.IsValidKind(req.Kind) {
		return nil, errors.New(blueprintenums.ErrInvalidKind)
	}
	rows, err := s.m.List(ctx, req.Kind)
	if err != nil {
		return nil, err
	}
	out := make([]*blueprintdto.BlueprintResp, 0, len(rows))
	for _, r := range rows {
		out = append(out, toResp(r))
	}
	return out, nil
}

// Delete 删除 Blueprint。
func (s *Service) Delete(ctx context.Context, req *blueprintdto.DeleteReq) (err error) {
	if req == nil || req.ID == "" {
		return errors.New(blueprintenums.ErrInvalidParam)
	}
	if _, err = s.m.Get(ctx, req.ID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New(blueprintenums.ErrNotFound)
		}
		return err
	}
	return s.m.Delete(ctx, req.ID)
}

// InitPageDocument 取最新版本 document，递归复制 AST 并为每个节点生成新 UUID，
// 返回不再依赖 Blueprint 的完整 Page Document（page 模块 CreatePage 的输入）。
func (s *Service) InitPageDocument(ctx context.Context, blueprintID string) (doc json.RawMessage, err error) {
	if strings.TrimSpace(blueprintID) == "" {
		return nil, errors.New(blueprintenums.ErrInvalidParam)
	}
	ver, err := s.m.LatestVersion(ctx, blueprintID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(blueprintenums.ErrNotFound)
		}
		return nil, err
	}
	page, err := builder.ParsePage(ver.Document)
	if err != nil {
		return nil, errors.New(blueprintenums.ErrDataInvalid)
	}
	// 复制完整 AST：settings 原样保留，root 每个节点递归重写 ID（公共机制，与 block 片段模板共用）。
	cloned := builder.ClonePageWithNewIDs(page)
	out, err := json.Marshal(cloned)
	if err != nil {
		return nil, errors.New(blueprintenums.ErrDataInvalid)
	}
	return out, nil
}

// validateDocument 解析并校验 Page Document，返回规范化存储字节。
func validateDocument(raw json.RawMessage) (json.RawMessage, error) {
	page, err := builder.ParsePage(raw)
	if err != nil {
		return nil, errors.New(blueprintenums.ErrDataInvalid)
	}
	if err = builder.ValidatePage(page); err != nil {
		return nil, errors.New(blueprintenums.ErrDataInvalid)
	}
	// 重新编码保证存储 JSON 的规范格式，不接受散乱字节。
	doc, err := json.Marshal(page)
	if err != nil {
		return nil, errors.New(blueprintenums.ErrDataInvalid)
	}
	return doc, nil
}

// toResp 实体 → 响应。
func toResp(e *blueprintmodel.BlueprintEntity) *blueprintdto.BlueprintResp {
	return &blueprintdto.BlueprintResp{
		ID:            e.ID,
		Name:          e.Name,
		Kind:          e.Kind,
		DraftVersion:  e.DraftVersion,
		DraftDocument: e.DraftDocument,
		UpdatedAt:     e.UpdatedAt.Format("2006-01-02 15:04"),
	}
}
