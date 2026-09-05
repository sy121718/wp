// Package blockservice 实现全局块业务用例。
package blockservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"go_wp/internal/builder"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	blockmodel "go_wp/internal/module/block/model"
	projectcontract "go_wp/internal/module/project/contract"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var _ blockcontract.BlockService = (*Service)(nil)

// Service 全局块业务服务：跨页面复用的结构片段（页眉/页脚/区块）。
type Service struct {
	model    *blockmodel.Model
	projects projectcontract.ProjectService
}

// NewService 创建全局块服务。
func NewService(model *blockmodel.Model, projects projectcontract.ProjectService) *Service {
	return &Service{model: model, projects: projects}
}

// List 列出工程全部块（kind 可选过滤）。
func (s *Service) List(ctx context.Context, req *blockdto.ListReq) (res []blockdto.BlockResp, err error) {
	// 参数缺失（nil/空 projectID）是调用方错误，与「工程下无块」/资源不存在区分开。
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New(blockenums.ErrBlockParamRequired)
	}
	entities, err := s.model.ListByProject(ctx, req.ProjectID, strings.TrimSpace(req.Kind))
	if err != nil {
		return nil, err
	}
	res = make([]blockdto.BlockResp, 0, len(entities))
	for i := range entities {
		res = append(res, blockResp(&entities[i]))
	}
	return res, nil
}

// Detail 按 ID 查询块。
func (s *Service) Detail(ctx context.Context, req *blockdto.DetailReq) (res *blockdto.BlockResp, err error) {
	// 参数缺失（nil/空 ID）是调用方错误，与「ID 对应块不存在」区分开。
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(blockenums.ErrBlockParamRequired)
	}
	entity, err := s.getExistingBlock(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return blockRespPtr(entity), nil
}

// Create 新建块（同工程名称唯一；文档走页面文档同构校验）。
func (s *Service) Create(ctx context.Context, req *blockdto.CreateReq) (res *blockdto.BlockResp, err error) {
	if req == nil || strings.TrimSpace(req.Name) == "" {
		return nil, errors.New(blockenums.ErrBlockNameRequired)
	}
	if err = s.requireProject(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	kind := normalizeKind(req.Kind)
	document, err := validateDocument(req.Document)
	if err != nil {
		return nil, err
	}
	existing, err := s.model.ListByProject(ctx, req.ProjectID, "")
	if err != nil {
		return nil, err
	}
	for _, e := range existing {
		if strings.EqualFold(e.Name, strings.TrimSpace(req.Name)) {
			return nil, errors.New(blockenums.ErrBlockDuplicate)
		}
	}
	now := time.Now().UTC()
	entity := &blockmodel.BlockEntity{
		ID: uuid.NewString(), ProjectID: req.ProjectID,
		Name: strings.TrimSpace(req.Name), Kind: kind, Document: document,
		CreatedAt: now, UpdatedAt: now,
	}
	if err = s.model.Create(ctx, entity); err != nil {
		return nil, err
	}
	return blockRespPtr(entity), nil
}

// Update 更新块（名称/类型/文档整树保存）。
func (s *Service) Update(ctx context.Context, req *blockdto.UpdateReq) (res *blockdto.BlockResp, err error) {
	// 参数缺失（nil/空 ID）是调用方错误，与「ID 对应块不存在」区分开（与 List/Detail 语义一致）。
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, errors.New(blockenums.ErrBlockParamRequired)
	}
	entity, err := s.getExistingBlock(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	name := entity.Name
	if strings.TrimSpace(req.Name) != "" {
		name = strings.TrimSpace(req.Name)
	}
	kind := entity.Kind
	if strings.TrimSpace(req.Kind) != "" {
		kind = normalizeKind(req.Kind)
	}
	document := entity.Document
	if len(req.Document) > 0 {
		if document, err = validateDocument(req.Document); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	if err = s.model.UpdateDocument(ctx, entity.ID, name, kind, document, now); err != nil {
		return nil, err
	}
	entity.Name, entity.Kind, entity.Document, entity.UpdatedAt = name, kind, document, now
	return blockRespPtr(entity), nil
}

// Delete 删除块。引用方（主题槽位/页面引用）由调用方编排标 stale。
func (s *Service) Delete(ctx context.Context, req *blockdto.DeleteReq) (err error) {
	// 参数缺失（nil/空 ID）是调用方错误，与「ID 对应块不存在」区分开（与 List/Detail 语义一致）。
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return errors.New(blockenums.ErrBlockParamRequired)
	}
	// 先确认存在：避免 model.Delete RowsAffected=0 静默成功，
	// 与 Detail/Update 的「不存在 → ErrBlockNotFound」语义保持一致。
	if _, err := s.getExistingBlock(ctx, req.ID); err != nil {
		return err
	}
	return s.model.Delete(ctx, req.ID)
}

func (s *Service) getExistingBlock(ctx context.Context, id string) (e *blockmodel.BlockEntity, err error) {
	e, err = s.model.GetByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New(blockenums.ErrBlockNotFound)
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (s *Service) requireProject(ctx context.Context, projectID string) error {
	exists, err := s.projects.Exists(ctx, projectID)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New(blockenums.ErrProjectNotFound)
	}
	return nil
}

// normalizeKind 归一化块类型（空默认 block）。
func normalizeKind(raw string) string {
	switch strings.TrimSpace(raw) {
	case blockmodel.KindHeader:
		return blockmodel.KindHeader
	case blockmodel.KindFooter:
		return blockmodel.KindFooter
	default:
		return blockmodel.KindBlock
	}
}

// validateDocument 校验块文档：与页面文档同构（root 组件树），复用页面校验器。
func validateDocument(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[]}`)
	}
	page, err := builder.ParsePage(raw)
	if err != nil {
		return nil, errors.New(blockenums.ErrBlockInvalidDoc)
	}
	if err = builder.ValidatePage(page); err != nil {
		return nil, errors.New(blockenums.ErrBlockInvalidDoc)
	}
	out, err := json.Marshal(page)
	if err != nil {
		return nil, errors.New(blockenums.ErrBlockInvalidDoc)
	}
	return out, nil
}

func blockResp(e *blockmodel.BlockEntity) blockdto.BlockResp {
	return blockdto.BlockResp{
		ID: e.ID, ProjectID: e.ProjectID, Name: e.Name, Kind: e.Kind,
		Document: e.Document, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

func blockRespPtr(e *blockmodel.BlockEntity) *blockdto.BlockResp {
	r := blockResp(e)
	return &r
}
