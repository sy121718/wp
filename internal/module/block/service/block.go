package blockservice

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	blockmodel "go_wp/internal/module/block/model"
	"go_wp/pkg/builder"
	"go_wp/pkg/database"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
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

	}
	document := entity.Document
	if len(req.Document) > 0 {
		if document, err = validateDocument(req.Document); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()

	// 全局块转模板会放开「可被删除」的约束：引用复核必须与写入同事务、共享工程内容锁，
	// 否则检查通过到提交之间仍可能新增引用。
	var affected int64
	err = s.model.Transaction(ctx, entity.ProjectID, func(tx *gorm.DB) error {
		if lerr := i18n.LockProjectContentWrite(ctx, tx, entity.ProjectID); lerr != nil {
			return lerr
		}
		if entity.ReuseMode == blockmodel.ReuseGlobal && reuseMode == blockmodel.ReuseTemplate {
			if usages, inUse := s.blockRefStateTx(ctx, tx, entity.ProjectID, entity.ID); inUse {
				return blockcontract.NewBlockInUseError(usages)
			}
		}
		n, uerr := s.model.UpdateDocumentTx(ctx, entity.ProjectID, entity.ID, req.ExpectedVersion,
			name, kind, category, reuseMode, document, now)
		affected = n
		return uerr
	})
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

// Delete 是单条删除，与 BulkDelete 共用同一条事务内删除规则：
// 锁行、复核存在性、检查全局块引用、按行数确认删除，任一不成立都不写库。
func (s *Service) Delete(ctx context.Context, req *blockdto.DeleteReq) (err error) {

	if req == nil || strings.TrimSpace(req.ID) == "" {
		return ErrParamRequired
	}

	located, err := s.getExistingBlock(ctx, req.ID)
	if err != nil {
		return err
	}
	targets := []blockTarget{{id: located.ID, projectID: located.ProjectID, reuseMode: located.ReuseMode}}
	deleted, err := s.deleteTargetsTx(ctx, targets)
	if err != nil {
		return err
	}
	for _, t := range deleted {
		s.propagateStale(ctx, t.id, t.reuseMode)
	}
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

// BulkDelete 在一个事务里删除多个块：任一失败整批回滚，并把失败原因交给调用方。
func (s *Service) BulkDelete(ctx context.Context, req *blockdto.BulkDeleteReq) error {
	if req == nil || len(req.IDs) == 0 {
		return ErrParamRequired
	}
	targets, err := s.locateTargets(ctx, req.IDs)
	if err != nil {
		return err
	}
	deleted, err := s.deleteTargetsTx(ctx, targets)
	if err != nil {
		return err
	}
	for _, t := range deleted {
		s.propagateStale(ctx, t.id, t.reuseMode)
	}
	return nil
}

// blockTarget 是待删块的最小事实：工程用于 RLS 作用域，复用方式决定是否要查引用与传播 stale。
type blockTarget struct {
	id        string
	projectID string
	reuseMode string
}

// locateTargets 逐 id 定位所属工程并去重；任一 id 非法或不存在都整批拒绝，
// 逐条原因进 data.items，不静默跳过。
func (s *Service) locateTargets(ctx context.Context, rawIDs []string) ([]blockTarget, error) {
	targets := make([]blockTarget, 0, len(rawIDs))
	seen := make(map[string]struct{}, len(rawIDs))
	blocked := make([]facing.BlockedItem, 0)

	for _, raw := range rawIDs {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			blocked = append(blocked, facing.BlockedItem{ID: raw, ReasonKey: blockenums.ErrBlockNotFound})
			continue
		}
		if _, dup := seen[trimmed]; dup {
			continue
		}
		entity, err := s.getExistingBlock(ctx, trimmed)
		if err != nil {
			key := blockenums.ErrBlockNotFound
			if !errors.Is(err, ErrNotFound) {
				key = blockenums.MsgInternalError
			}
			blocked = append(blocked, facing.BlockedItem{ID: trimmed, ReasonKey: key})
			continue
		}
		seen[trimmed] = struct{}{}
		targets = append(targets, blockTarget{
			id: entity.ID, projectID: entity.ProjectID, reuseMode: entity.ReuseMode,
		})
	}
	if len(blocked) > 0 {
		return nil, &bulkBlockedError{items: blocked}
	}
	if len(targets) == 0 {
		return nil, ErrParamRequired
	}
	return targets, nil
}

// deleteTargetsTx 把所有目标放进同一个事务：按工程稳定排序逐段切换 RLS 作用域，
// 段内按 id 升序锁定、复核存在性与引用、逐条核对受影响行数；任一失败整批回滚。
func (s *Service) deleteTargetsTx(ctx context.Context, targets []blockTarget) ([]blockTarget, error) {
	groups := map[string][]blockTarget{}
	order := make([]string, 0, len(targets))
	for _, t := range targets {
		if _, ok := groups[t.projectID]; !ok {
			order = append(order, t.projectID)
		}
		groups[t.projectID] = append(groups[t.projectID], t)
	}
	sort.Strings(order)

	blocked := make([]facing.BlockedItem, 0)
	var blockedUsages []blockcontract.BlockUsage
	err := s.model.TransactionAny(ctx, func(tx *gorm.DB) error {
		// 工程内容写锁先于任何行操作：块引用的写入方（页面 / 模板 / 主题）持同一把锁，
		// 否则「锁内检查通过」与「提交后又有新引用」之间的窗口仍然存在。
		for _, projectID := range order {
			if serr := s.model.ScopeTx(tx, projectID); serr != nil {
				return serr
			}
			if lerr := i18n.LockProjectContentWrite(ctx, tx, projectID); lerr != nil {
				return lerr
			}
		}
		for _, projectID := range order {
			rows := groups[projectID]
			if serr := s.model.ScopeTx(tx, projectID); serr != nil {
				return serr
			}
			ids := make([]string, 0, len(rows))
			for _, t := range rows {
				ids = append(ids, t.id)
			}
			locked, lerr := s.model.LockByIDsTx(ctx, tx, projectID, ids)
			if lerr != nil {
				return lerr
			}
			found := make(map[string]*blockmodel.BlockEntity, len(locked))
			for i := range locked {
				found[locked[i].ID] = &locked[i]
			}
			for _, t := range rows {
				entity, ok := found[t.id]
				if !ok {
					blocked = append(blocked, facing.BlockedItem{ID: t.id, ReasonKey: blockenums.ErrBlockNotFound})
					continue
				}
				// 全局块的引用检查必须在锁内复核：定位阶段的检查到删除之间可能有新引用。
				// 同工程块文档引用用事务内读取；跨模块引用由注入端口回答（端口自身不在事务内）。
				if entity.ReuseMode == blockmodel.ReuseGlobal {
					if usages, inUse := s.blockRefStateTx(ctx, tx, projectID, t.id); inUse {
						blocked = append(blocked, facing.BlockedItem{
							ID: t.id, Name: entity.Name, ReasonKey: blockenums.ErrBlockInUse,
						})
						blockedUsages = append(blockedUsages, usages...)
					}
				}
			}
			if len(blocked) > 0 {
				return blockedErrorFor(targets, blocked, blockedUsages)
			}
			for _, t := range rows {
				affected, derr := s.model.DeleteTx(ctx, tx, projectID, t.id)
				if derr != nil {
					return derr
				}
				if affected != 1 {
					return &bulkBlockedError{items: []facing.BlockedItem{
						{ID: t.id, ReasonKey: blockenums.ErrBlockNotFound},
					}}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return targets, nil
}

// blockedErrorFor 保留单条删除对引用明细的既有契约：唯一目标因引用被拒时，
// 仍返回携带逐条引用位置的 *BlockInUseError；批量则收敛为一条公共拒绝事实。
func blockedErrorFor(targets []blockTarget, items []facing.BlockedItem, usages []blockcontract.BlockUsage) error {
	if len(targets) == 1 && len(items) == 1 && items[0].ReasonKey == blockenums.ErrBlockInUse {
		return blockcontract.NewBlockInUseError(usages)
	}
	return &bulkBlockedError{items: items}
}

// bulkBlockedError 携带整批被拒的逐条原因；由共享失败出口放进 data.items。
type bulkBlockedError struct {
	items []facing.BlockedItem
}

func (e *bulkBlockedError) Error() string { return blockenums.ErrBlockInUse }

func (e *bulkBlockedError) Facing() (string, string) {
	return blockenums.ErrBlockInUse, ""
}

func (e *bulkBlockedError) BlockedItems() []facing.BlockedItem { return e.items }
