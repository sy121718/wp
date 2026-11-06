package blockservice

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"

	"go_wp/internal/module/block/contract"
	"go_wp/internal/module/block/model"
	"go_wp/internal/module/project/contract"
	"go_wp/pkg/logger"
)

var _ blockcontract.BlockService = (*Service)(nil)

type Service struct {
	presetResolver blockcontract.PresetResolver
	model          *blockmodel.Model
	projects       projectcontract.ProjectService

	propagate func(ctx context.Context, blockID string) error

	referenced func(ctx context.Context, blockID string) (bool, error)

	usages func(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error)
}

func NewService(model *blockmodel.Model, projects projectcontract.ProjectService) *Service {
	return &Service{model: model, projects: projects}
}

func (s *Service) RequireWiring() {
	if s.propagate == nil || (s.referenced == nil && s.usages == nil) {
		panic("block.Service: stale 传播器与引用检查器必须注入（装配层在 page 装配后调用 RequireWiring）")
	}
}

func (s *Service) projectIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.model == nil {
		return nil, ErrProjectRequired
	}

	if s.projects == nil {
		return nil, ErrProjectRequired
	}
	list, err := s.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for i := range list {
		if id := strings.TrimSpace(list[i].ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {

		return nil, ErrProjectRequired
	}
	return ids, nil
}

func (s *Service) locateBlockEntity(ctx context.Context, id string) (*blockmodel.BlockEntity, error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, projectID := range ids {
		if ctx.Err() != nil {
			break
		}
		e, gerr := s.model.GetByID(ctx, id, projectID)
		if gerr == nil {
			return e, nil
		}
		if !errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, gerr
		}
	}
	return nil, ErrNotFound
}

var (
	ErrParamRequired = blockcontract.ErrParamRequired

	ErrNotFound = blockcontract.ErrNotFound

	ErrProjectNotFound = blockcontract.ErrProjectNotFound

	ErrProjectRequired = blockcontract.ErrProjectRequired

	ErrNameRequired = blockcontract.ErrNameRequired

	ErrInvalidDoc = blockcontract.ErrInvalidDoc

	ErrInvalidKind = blockcontract.ErrInvalidKind

	ErrInvalidCategory = blockcontract.ErrInvalidCategory

	ErrDuplicate = blockcontract.ErrDuplicate

	ErrInvalidReuseMode = blockcontract.ErrInvalidReuseMode

	ErrBlockInUse = blockcontract.ErrBlockInUse
)

func (s *Service) SetStalePropagator(p func(ctx context.Context, blockID string) error) {
	s.propagate = p
}

func (s *Service) propagateStale(ctx context.Context, blockID, reuseMode string) {
	if reuseMode == blockmodel.ReuseTemplate {
		return
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

func (s *Service) SetReferenceChecker(r func(ctx context.Context, blockID string) (bool, error)) {
	s.referenced = r
}

func (s *Service) SetReferenceUsageChecker(r func(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error)) {
	s.usages = r
}

// blockRefStateTx 是删除事务内的引用复核：同工程块文档引用读事务快照，
// 跨模块引用（页面 / 模板 / 主题）由注入端口回答。
//
// 端口失败一律按「被引用」处理，删除整批拒绝；不能因为查不出来就放行。
func (s *Service) blockRefStateTx(ctx context.Context, tx *gorm.DB, projectID, blockID string) ([]blockcontract.BlockUsage, bool) {
	usages, referenced := s.blockRefState(ctx, blockID)
	if referenced {
		return usages, true
	}
	rows, err := s.model.ListBlockDocumentRefsTx(ctx, tx, projectID, blockID)
	if err != nil {
		logger.Scene("block").With("block_id", blockID).Error(err, "块文档引用检查失败，按被引用处理")
		return nil, true
	}
	for i := range rows {
		usages = append(usages, blockcontract.BlockUsage{
			Kind: blockcontract.UsageKindBlockDocument, ProjectID: projectID,
			EntityID: rows[i].ID, Label: rows[i].Name,
		})
	}
	return usages, len(usages) > 0
}

func (s *Service) blockRefState(ctx context.Context, blockID string) (usages []blockcontract.BlockUsage, referenced bool) {
	if s.usages != nil {
		list, err := s.usages(ctx, blockID)
		if err != nil {
			logger.Scene("block").With("block_id", blockID).Error(err, "块引用明细检查失败，按被引用处理")
			return nil, true
		}
		return list, len(list) > 0
	}
	if s.referenced != nil {
		ok, err := s.referenced(ctx, blockID)
		if err != nil {
			logger.Scene("block").With("block_id", blockID).Error(err, "块引用检查失败，按被引用处理")
			return nil, true
		}
		return nil, ok
	}
	return nil, true
}

func (s *Service) ListBlockSourceRefs(ctx context.Context, blockID string) (out []blockcontract.BlockUsage, err error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, projectID := range ids {
		if ctx.Err() != nil {
			break
		}
		rows, rerr := s.model.ListBlockDocumentRefs(ctx, projectID, blockID)
		if rerr != nil {
			return nil, rerr
		}
		for i := range rows {
			out = append(out, blockcontract.BlockUsage{
				Kind: blockcontract.UsageKindBlockDocument, ProjectID: projectID,
				EntityID: rows[i].ID, Label: rows[i].Name,
			})
		}
	}
	return out, nil
}
