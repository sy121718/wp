package blockservice

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	blockmodel "go_wp/internal/module/block/model"
	"go_wp/pkg/database"
	"go_wp/pkg/facing"
	"go_wp/pkg/utils"
)

func (s *Service) List(ctx context.Context, req *blockdto.ListReq) (res []blockdto.BlockResp, err error) {

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

func (s *Service) Detail(ctx context.Context, req *blockdto.DetailReq) (res *blockdto.BlockResp, err error) {

	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrParamRequired
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		entity, lerr := s.getExistingBlock(ctx, req.ID)
		if lerr != nil {
			return nil, lerr
		}
		return blockRespPtr(entity), nil
	}
	if err = s.requireProject(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	entity, err := s.model.GetByID(ctx, strings.TrimSpace(req.ID), req.ProjectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return blockRespPtr(entity), nil
}

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
	document, err := s.resolveCreateDocument(ctx, req)
	if err != nil {
		return nil, err
	}

	exists, err := s.model.ExistsByName(ctx, req.ProjectID, strings.TrimSpace(req.Name))
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, ErrDuplicate
	}
	now := time.Now().UTC()

	entity := &blockmodel.BlockEntity{
		ID:        uuid.NewString(),
		ProjectID: req.ProjectID,
		Name:      strings.TrimSpace(req.Name), Kind: kind, Category: category, ReuseMode: reuseMode, Document: document,
		CreateTime: now, UpdatedAt: now,
	}
	if req.PresetKey != "" {
		key, version := req.PresetKey, req.PresetVersion
		entity.PresetKey, entity.PresetVersion = &key, &version
	}
	if err = s.model.Create(ctx, entity); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || database.IsUniqueViolation(err) {
			return nil, ErrDuplicate
		}
		return nil, err
	}
	return blockRespPtr(entity), nil
}

func (s *Service) Update(ctx context.Context, req *blockdto.UpdateReq) (res *blockdto.BlockResp, err error) {

	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrParamRequired
	}
	if req.ExpectedVersion == 0 || req.ExpectedVersion > uint64(1<<63-1) {
		return nil, facing.WithStatus(ErrParamRequired, http.StatusBadRequest)
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

		if entity.ReuseMode == blockmodel.ReuseGlobal && reuseMode == blockmodel.ReuseTemplate {
			if usages, inUse := s.blockRefState(ctx, entity.ID); inUse {
				return nil, blockcontract.NewBlockInUseError(usages)
			}
		}
	}
	document := entity.Document
	if len(req.Document) > 0 {
		if document, err = validateDocument(req.Document); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()

	affected, err := s.model.UpdateDocumentTx(ctx, entity.ProjectID, entity.ID, req.ExpectedVersion,
		name, kind, category, reuseMode, document, now)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, s.versionConflict(ctx, entity.ProjectID, entity.ID)
	}
	entity.Version = int64(req.ExpectedVersion) + 1
	entity.Name, entity.Kind, entity.Category, entity.ReuseMode, entity.Document, entity.UpdatedAt = name, kind, category, reuseMode, document, now
	s.propagateStale(ctx, entity.ID, entity.ReuseMode)
	return blockRespPtr(entity), nil
}

func (s *Service) Delete(ctx context.Context, req *blockdto.DeleteReq) (err error) {

	if req == nil || strings.TrimSpace(req.ID) == "" {
		return ErrParamRequired
	}

	entity, err := s.getExistingBlock(ctx, req.ID)
	if err != nil {
		return err
	}
	if entity.ReuseMode == blockmodel.ReuseGlobal {
		if usages, inUse := s.blockRefState(ctx, entity.ID); inUse {

			return blockcontract.NewBlockInUseError(usages)
		}
	}
	if err = s.model.Delete(ctx, entity.ProjectID, entity.ID); err != nil {
		return err
	}
	s.propagateStale(ctx, req.ID, entity.ReuseMode)
	return nil
}

func (s *Service) getExistingBlock(ctx context.Context, id string) (e *blockmodel.BlockEntity, err error) {

	trimmed := strings.TrimSpace(id)
	if _, perr := uuid.Parse(trimmed); perr != nil {
		return nil, ErrNotFound
	}
	return s.locateBlockEntity(ctx, trimmed)
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

func blockResp(e *blockmodel.BlockEntity) blockdto.BlockResp {
	return blockdto.BlockResp{
		ID: e.ID, ProjectID: e.ProjectID, Name: e.Name, Kind: e.Kind, Category: e.Category,
		ReuseMode: e.ReuseMode, Document: e.Document, CreatedAt: utils.NewJSONTime(e.CreateTime), UpdatedAt: utils.NewJSONTime(e.UpdatedAt),
		Version: e.Version, PresetKey: e.PresetKey, PresetVersion: e.PresetVersion,
	}
}

func (s *Service) versionConflict(ctx context.Context, projectID, id string) error {
	cur, cerr := s.model.GetByID(ctx, id, projectID)
	if cerr != nil || cur == nil {
		return facing.WithStatus(blockenums.Err(blockenums.ErrBlockVersionConflict), http.StatusConflict)
	}

	return facing.WithStatus(&versionConflictError{version: cur.Version}, http.StatusConflict)
}

type versionConflictError struct{ version int64 }

func (e *versionConflictError) Error() string { return blockenums.ErrBlockVersionConflict }

func (e *versionConflictError) Is(target error) bool {
	return target != nil && target.Error() == blockenums.ErrBlockVersionConflict
}

// Facing 交「key|参数」：版本号是位置参数，由失败片段取词填进完整句。
func (e *versionConflictError) Facing() (string, string) {
	return blockenums.ErrBlockVersionConflict + "|" + strconv.FormatInt(e.version, 10), ""
}

func (e *versionConflictError) Version() string { return strconv.FormatInt(e.version, 10) }

func blockRespPtr(e *blockmodel.BlockEntity) *blockdto.BlockResp {
	r := blockResp(e)
	return &r
}

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

var categoryPattern = regexp.MustCompile(`^[a-z0-9_-]{1,50}$`)

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

var kindWhitelist = map[string]bool{
	blockmodel.KindBlock: true, blockmodel.KindHeader: true, blockmodel.KindFooter: true,
	blockmodel.KindAnnouncement: true, blockmodel.KindSidebar: true, blockmodel.KindBreadcrumb: true,
	blockmodel.KindDrawer: true, blockmodel.KindSearch: true,
	blockmodel.KindCTA: true, blockmodel.KindTrust: true, blockmodel.KindBrands: true,
	blockmodel.KindContact: true, blockmodel.KindAbout: true,
	blockmodel.KindBanner: true, blockmodel.KindGrid: true,
	blockmodel.KindSnippet: true,
}

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
