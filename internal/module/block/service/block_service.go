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

	"gorm.io/gorm"
	"strconv"
)

var _ blockcontract.BlockService = (*Service)(nil)

// Service 全局块业务服务：跨页面复用的结构片段（页眉/页脚/区块）。
type Service struct {
	model    *blockmodel.Model
	projects projectcontract.ProjectService
	// propagate 全局块内容变更/删除后的 stale 传播器（编排层注入，可空）。
	// block 不直接依赖 page 模块，传播由注入的回调完成，避免 block↔page 装配循环。
	propagate func(ctx context.Context, blockID string) error
	// referenced 引用检查器（编排层注入，可空）：global 块删除 / global→template 切换前
	// 判断是否仍被页面（globalref/structure）或主题槽位引用。未注入时不拦截（兼容直连装配）。
	referenced func(ctx context.Context, blockID string) (bool, error)
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

// SetReferenceChecker 注入引用检查器（dashboard 在 page/project 装配后绑定，
// 与 SetStalePropagator 同模式）。生产装配必须调用 RequireWiring。
func (s *Service) SetReferenceChecker(r func(ctx context.Context, blockID string) (bool, error)) {
	s.referenced = r
}

// RequireWiring 编排完成后调用：传播器或引用检查器未注入则 fail-fast。
func (s *Service) RequireWiring() {
	if s.propagate == nil || s.referenced == nil {
		panic("block.Service: stale 传播器与引用检查器必须注入（dashboard 装配后调用 RequireWiring）")
	}
}

// blockReferenced 判断块是否仍被引用（检查器未注入视为仍被引用，宁拒勿删）。
func (s *Service) blockReferenced(ctx context.Context, blockID string) bool {
	if s.referenced == nil {
		return true
	}
	ok, err := s.referenced(ctx, blockID)
	if err != nil {
		// 检查失败按「被引用」处理（宁拒勿删，删除是不可逆操作）。
		logger.Scene("block").With("block_id", blockID).Error(err, "块引用检查失败，按被引用处理")
		return true
	}
	return ok
}

// propagateStale 块内容变更/删除后触发引用方 stale 传播。
// reuse_mode=template 的块不传播（docs/02-D §9）：插入时已复制 AST，页面持有独立副本，
// 源块修改不影响任何页面。传播器未注入或传播失败只记日志，不阻断保存/删除主流程。
func (s *Service) propagateStale(ctx context.Context, blockID, reuseMode string) {
	if reuseMode == blockmodel.ReuseTemplate {
		return // 一次性复制的片段不传播 stale
	}
	if s.propagate == nil {
		logger.Scene("block").With("block_id", blockID).Error(
			errors.New("stale 传播器未注入"),
			"全局块变更未传播 stale，请检查 dashboard 装配是否调用 RequireWiring",
		)
		return
	}
	if err := s.propagate(ctx, blockID); err != nil {
		logger.Scene("block").With("block_id", blockID).Error(err, "全局块 stale 传播失败")
	}
}

// List 列出工程全部块（kind/category/reuseMode 可选过滤）。
func (s *Service) List(ctx context.Context, req *blockdto.ListReq) (res []blockdto.BlockResp, err error) {
	// 参数缺失（nil/空 projectID）是调用方错误，与「工程下无块」/资源不存在区分开。
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, ErrParamRequired
	}
	category := strings.TrimSpace(req.Category)
	if category != "" && !categoryPattern.MatchString(category) {
		return nil, ErrInvalidCategory
	}
	reuseMode, err := normalizeReuseMode(req.ReuseMode)
	if err != nil {
		return nil, err
	}
	entities, err := s.model.ListByProject(ctx, req.ProjectID, strings.TrimSpace(req.Kind), category, reuseMode)
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
	// 参数缺失（nil/空 ID/空 projectID）是调用方错误，与「ID 对应块不存在」区分开。
	if req == nil || strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.ProjectID) == "" {
		return nil, ErrParamRequired
	}
	if err = s.requireProject(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	bid, perr := strconv.ParseInt(strings.TrimSpace(req.ID), 10, 64)
	if perr != nil {
		return nil, ErrNotFound
	}
	entity, err := s.model.GetByID(ctx, bid, req.ProjectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
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
	reuseMode, err := normalizeReuseMode(req.ReuseMode)
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
		ProjectID: req.ProjectID,
		Name:      strings.TrimSpace(req.Name), Kind: kind, Category: category, ReuseMode: reuseMode, Document: document,
		CreateTime: now, UpdatedAt: now,
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
	reuseMode := entity.ReuseMode
	if strings.TrimSpace(req.ReuseMode) != "" {
		if reuseMode, err = normalizeReuseMode(req.ReuseMode); err != nil {
			return nil, err
		}
		// global→template 切换防御：仍被引用的 global 块一旦切成 template，
		// 引用页面将悬空（stale 不再传播），必须先解除引用。
		if entity.ReuseMode == blockmodel.ReuseGlobal && reuseMode == blockmodel.ReuseTemplate &&
			s.blockReferenced(ctx, strconv.FormatInt(entity.ID, 10)) {
			return nil, ErrBlockInUse
		}
	}
	document := entity.Document
	if len(req.Document) > 0 {
		if document, err = validateDocument(req.Document); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	if err = s.model.UpdateDocument(ctx, entity.ID, name, kind, category, reuseMode, document, now); err != nil {
		return nil, err
	}
	entity.Name, entity.Kind, entity.Category, entity.ReuseMode, entity.Document, entity.UpdatedAt = name, kind, category, reuseMode, document, now
	s.propagateStale(ctx, strconv.FormatInt(entity.ID, 10), entity.ReuseMode)
	return blockRespPtr(entity), nil
}

// Delete 删除块。docs/02-D §9：
//   - reuse_mode=template：页面已持有独立副本，删除无副作用，不传播；
//   - reuse_mode=global：仍被页面（globalref/structure）或主题槽位引用时默认拒绝（ErrBlockInUse），
//     Force=true 强制删除并传播（引用页面退化为无该块，下次构建 globalref 降级占位）。
func (s *Service) Delete(ctx context.Context, req *blockdto.DeleteReq) (err error) {
	// 参数缺失（nil/空 ID）是调用方错误，与「ID 对应块不存在」区分开（与 List/Detail 语义一致）。
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return ErrParamRequired
	}
	// 先确认存在：避免 model.Delete RowsAffected=0 静默成功，
	// 与 Detail/Update 的「不存在 → ErrBlockNotFound」语义保持一致。
	entity, err := s.getExistingBlock(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity.ReuseMode == blockmodel.ReuseGlobal && !req.Force && s.blockReferenced(ctx, strconv.FormatInt(entity.ID, 10)) {
		return ErrBlockInUse
	}
	if err = s.model.Delete(ctx, entity.ID); err != nil {
		return err
	}
	s.propagateStale(ctx, req.ID, entity.ReuseMode) // req.ID 为 dto 字符串形式，与引用检查口径一致
	return nil
}

// CloneAST 复制块文档为独立 AST（docs/02-D §5.2「插入-复制」动作）：
// 解析 → 公共克隆（重生成全部 Node ID）→ 返回。副本与源块脱钩，此后互不影响。
// 与 blueprint 的整页初始化复用同一 ClonePageWithNewIDs 机制（片段层级 vs 完整文档层级）。
func (s *Service) CloneAST(ctx context.Context, req *blockdto.CloneReq) (res *blockdto.CloneResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrParamRequired
	}
	entity, err := s.getExistingBlock(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	page, err := builder.ParsePage(entity.Document)
	if err != nil {
		return nil, ErrInvalidDoc
	}
	out, err := json.Marshal(builder.ClonePageWithNewIDs(page))
	if err != nil {
		return nil, ErrInvalidDoc
	}
	return &blockdto.CloneResp{Document: out}, nil
}

func (s *Service) getExistingBlock(ctx context.Context, id string) (e *blockmodel.BlockEntity, err error) {
	// 不是合法十进制整数的 id 直接判「不存在」，别让查询落到 PG 上：blocks.id 是 bigint 列
	// （DB-020 统一 BIGSERIAL），dto 层仍以字符串承载以维持 API 兼容。
	//
	// 判「不存在」而不是「参数错误」：这四个调用方（详情 / 更新 / 删除 / 克隆）
	// 对这两种情况的处理本来就一样，多一个 400 分支只会逼每条调用路径判断两种错。
	bid, perr := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	if perr != nil {
		return nil, ErrNotFound
	}
	e, err = s.model.GetByID(ctx, bid, "")
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

// kindWhitelist 块类型白名单（docs/02-D §4/§5）：站点骨架 + 复用内容段 + 布局骨架 + 片段模板。
var kindWhitelist = map[string]bool{
	blockmodel.KindBlock: true, blockmodel.KindHeader: true, blockmodel.KindFooter: true,
	blockmodel.KindAnnouncement: true, blockmodel.KindSidebar: true, blockmodel.KindBreadcrumb: true,
	blockmodel.KindDrawer: true, blockmodel.KindSearch: true,
	blockmodel.KindCTA: true, blockmodel.KindTrust: true, blockmodel.KindBrands: true,
	blockmodel.KindContact: true, blockmodel.KindAbout: true,
	blockmodel.KindBanner: true, blockmodel.KindGrid: true,
	blockmodel.KindSnippet: true,
}

// normalizeKind 归一化块类型：空默认 block，仅接受白名单内的类型，
// 其余返回 ErrInvalidKind（拒绝而非静默改写为 block）。
func normalizeKind(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return blockmodel.KindBlock, nil
	}
	if !kindWhitelist[s] {
		return "", ErrInvalidKind
	}
	return s, nil
}

// normalizeReuseMode 归一化复用方式（docs/02-D §5）：空默认 global（存量语义），
// 仅接受 global/template，其余返回 ErrInvalidReuseMode。
func normalizeReuseMode(raw string) (string, error) {
	switch strings.TrimSpace(raw) {
	case "":
		return blockmodel.ReuseGlobal, nil
	case blockmodel.ReuseGlobal:
		return blockmodel.ReuseGlobal, nil
	case blockmodel.ReuseTemplate:
		return blockmodel.ReuseTemplate, nil
	default:
		return "", ErrInvalidReuseMode
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
		ID: strconv.FormatInt(e.ID, 10), ProjectID: e.ProjectID, Name: e.Name, Kind: e.Kind, Category: e.Category,
		ReuseMode: e.ReuseMode, Document: e.Document, CreatedAt: e.CreateTime, UpdatedAt: e.UpdatedAt,
	}
}

func blockRespPtr(e *blockmodel.BlockEntity) *blockdto.BlockResp {
	r := blockResp(e)
	return &r
}
