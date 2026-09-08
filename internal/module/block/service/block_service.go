// Package blockservice 实现全局块业务用例。
package blockservice

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"go_wp/internal/builder"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var _ blockcontract.BlockService = (*Service)(nil)

// Service 全局块业务服务：跨页面复用的结构片段（页眉/页脚/区块）。
type Service struct {
	model    *blockmodel.Model
	projects projectcontract.ProjectService
	// propagate 全局块内容变更/删除后的 stale 传播器（编排层注入，可空）。
	// block 不直接依赖 page 模块，传播由注入的回调完成，避免 block↔page 装配循环。
	propagate func(ctx context.Context, blockID string) error
}

// NewService 创建全局块服务。
func NewService(model *blockmodel.Model, projects projectcontract.ProjectService) *Service {
	return &Service{model: model, projects: projects}
}

// SetStalePropagator 注入全局块 stale 传播器（编排层在 page 装配后绑定，
// 打破「block 先于 page 装配」的顺序约束）。未注入时内容变更不传播。
func (s *Service) SetStalePropagator(p func(ctx context.Context, blockID string) error) {
	s.propagate = p
}

// propagateStale 块内容变更/删除后触发引用方 stale 传播。
// 传播器未注入（直连 REST 且未接线）或传播失败只记日志，不阻断保存/删除主流程。
func (s *Service) propagateStale(ctx context.Context, blockID string) {
	if s.propagate == nil {
		logger.Scene("block").With("block_id", blockID).Warn("stale 传播器未注入，引用页面不会标待重建")
		return
	}
	if err := s.propagate(ctx, blockID); err != nil {
		logger.Scene("block").With("block_id", blockID).Error(err, "全局块 stale 传播失败")
	}
}

// List 列出工程全部块（kind/category 可选过滤）。
func (s *Service) List(ctx context.Context, req *blockdto.ListReq) (res []blockdto.BlockResp, err error) {
	// 参数缺失（nil/空 projectID）是调用方错误，与「工程下无块」/资源不存在区分开。
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, ErrParamRequired
	}
	category := strings.TrimSpace(req.Category)
	if category != "" && !categoryPattern.MatchString(category) {
		return nil, ErrInvalidCategory
	}
	entities, err := s.model.ListByProject(ctx, req.ProjectID, strings.TrimSpace(req.Kind), category)
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
		return nil, ErrParamRequired
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
		return nil, ErrNameRequired
	}
	if err = s.requireProject(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	kind, err := normalizeKind(req.Kind)
	if err != nil {
		return nil, err
	}
	category, err := normalizeCategory(req.Category)
	if err != nil {
		return nil, err
	}
	document, err := validateDocument(req.Document)
	if err != nil {
		return nil, err
	}
	// 判重：参数化单条查询（LOWER(name)），避免拉全量 Document(jsonb) 后内存 EqualFold。
	// 并发下同名仍可能穿透（需 DB 唯一索引兜底，见 model 层 ExistsByName 说明）。
	exists, err := s.model.ExistsByName(ctx, req.ProjectID, strings.TrimSpace(req.Name))
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrDuplicate
	}
	now := time.Now().UTC()
	entity := &blockmodel.BlockEntity{
		ID: uuid.NewString(), ProjectID: req.ProjectID,
		Name: strings.TrimSpace(req.Name), Kind: kind, Category: category, Document: document,
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
		return nil, ErrParamRequired
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
		if kind, err = normalizeKind(req.Kind); err != nil {
			return nil, err
		}
	}
	category := entity.Category
	if strings.TrimSpace(req.Category) != "" {
		if category, err = normalizeCategory(req.Category); err != nil {
			return nil, err
		}
	}
	document := entity.Document
	if len(req.Document) > 0 {
		if document, err = validateDocument(req.Document); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	if err = s.model.UpdateDocument(ctx, entity.ID, name, kind, category, document, now); err != nil {
		return nil, err
	}
	entity.Name, entity.Kind, entity.Category, entity.Document, entity.UpdatedAt = name, kind, category, document, now
	s.propagateStale(ctx, entity.ID)
	return blockRespPtr(entity), nil
}

// Delete 删除块，并触发引用方 stale 传播（传播器注入时）。
func (s *Service) Delete(ctx context.Context, req *blockdto.DeleteReq) (err error) {
	// 参数缺失（nil/空 ID）是调用方错误，与「ID 对应块不存在」区分开（与 List/Detail 语义一致）。
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return ErrParamRequired
	}
	// 先确认存在：避免 model.Delete RowsAffected=0 静默成功，
	// 与 Detail/Update 的「不存在 → ErrBlockNotFound」语义保持一致。
	if _, err := s.getExistingBlock(ctx, req.ID); err != nil {
		return err
	}
	if err = s.model.Delete(ctx, req.ID); err != nil {
		return err
	}
	s.propagateStale(ctx, req.ID)
	return nil
}

func (s *Service) getExistingBlock(ctx context.Context, id string) (e *blockmodel.BlockEntity, err error) {
	e, err = s.model.GetByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
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
		return ErrProjectNotFound
	}
	return nil
}

// categoryPattern category 值白名单：小写字母、数字、下划线、连字符，1~50 字符。
// 收紧为分组键友好的稳定格式（防注入，保证管理端/工作台分组与 URL 传参稳定）。
var categoryPattern = regexp.MustCompile(`^[a-z0-9_-]{1,50}$`)

// normalizeCategory 归一化块分类：空默认 general，非空校验白名单。
func normalizeCategory(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return blockmodel.DefaultCategory, nil
	}
	if !categoryPattern.MatchString(s) {
		return "", ErrInvalidCategory
	}
	return s, nil
}

// normalizeKind 归一化块类型：空默认 block，仅接受 block/header/footer，
// 其余返回 ErrInvalidKind（拒绝而非静默改写为 block）。
func normalizeKind(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "":
		return blockmodel.KindBlock, nil
	case blockmodel.KindBlock:
		return blockmodel.KindBlock, nil
	case blockmodel.KindHeader:
		return blockmodel.KindHeader, nil
	case blockmodel.KindFooter:
		return blockmodel.KindFooter, nil
	default:
		return "", ErrInvalidKind
	}
}

// validateDocument 校验块文档：与页面文档同构（root 组件树），复用页面校验器。
func validateDocument(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[]}`)
	}
	page, err := builder.ParsePage(raw)
	if err != nil {
		return nil, ErrInvalidDoc
	}
	if err = builder.ValidatePage(page); err != nil {
		return nil, ErrInvalidDoc
	}
	out, err := json.Marshal(page)
	if err != nil {
		return nil, ErrInvalidDoc
	}
	return out, nil
}

func blockResp(e *blockmodel.BlockEntity) blockdto.BlockResp {
	return blockdto.BlockResp{
		ID: e.ID, ProjectID: e.ProjectID, Name: e.Name, Kind: e.Kind, Category: e.Category,
		Document: e.Document, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

func blockRespPtr(e *blockmodel.BlockEntity) *blockdto.BlockResp {
	r := blockResp(e)
	return &r
}
