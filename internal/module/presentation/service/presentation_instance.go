package presentationservice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	"go_wp/pkg/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
	"go_wp/pkg/rls"
)

func (s *Service) CreateInstance(ctx context.Context, req *presentationdto.CreateInstanceReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.EntityType == "" || req.EntityID == "" || req.URLPath == "" {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}

	role := strings.TrimSpace(req.InstanceRole)
	if role == "" {
		role = presentationmodel.InstanceRoleDetail
	}
	if !presentationmodel.IsValidInstanceRole(role) {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}

	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}

	var (
		inst        *presentationmodel.InstanceEntity
		existing    *presentationmodel.InstanceEntity
		logicalPath string
	)
	lock := s.lockInstance(req.EntityType, req.EntityID)
	if err = func() error {
		lock.Lock()
		defer lock.Unlock()

		if row, gerr := s.m.GetInstanceByEntityRole(ctx, projectID, req.EntityType, req.EntityID, role); gerr == nil {
			existing = row
			return nil
		} else if !errors.Is(gerr, gorm.ErrRecordNotFound) {
			return gerr
		}

		tpl, rerr := s.resolveTemplate(ctx, projectID, req.EntityType, req.TemplateID)
		if rerr != nil {
			return rerr
		}

		path, perr := s.normalizeLogicalPath(ctx, projectID, req.URLPath)
		if perr != nil {
			return presentationenums.ErrDetail(presentationenums.ErrInvalidPath, perr.Error())
		}
		logicalPath = path

		if ferr := s.ensureLogicalPathFree(ctx, projectID, logicalPath, ""); ferr != nil {
			return ferr
		}

		now := time.Now().UTC()
		inst = &presentationmodel.InstanceEntity{
			ID: uuid.NewString(), ProjectID: projectID, EntityType: req.EntityType,
			EntityID: req.EntityID, InstanceRole: role, URLPath: logicalPath, TemplateID: tpl.TemplateID,
			Stale: true, CreatedAt: now, UpdatedAt: now,
		}
		return s.m.CreateInstance(ctx, inst)
	}(); err != nil {
		return nil, err
	}
	if existing != nil {
		return s.toResp(ctx, existing)
	}

	if _, err = s.publishAllLangs(ctx, inst, publishIntent{
		resolveDoc: func(ctx context.Context, fresh *presentationmodel.InstanceEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
			return s.resolveBoundTemplate(ctx, fresh, req.TemplateID)
		},
		logicalPath: logicalPath,
		trigger:     presentationmodel.PublishTriggerCreate,
	}); err != nil {
		return nil, publishFailedErr(err)
	}
	return s.toResp(ctx, inst)
}

func (s *Service) GetByEntity(ctx context.Context, req *presentationdto.GetByEntityReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.EntityType == "" || req.EntityID == "" {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}

	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstanceByEntity(ctx, projectID, req.EntityType, req.EntityID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, presentationenums.Err(presentationenums.ErrNotFound)
		}
		return nil, err
	}
	return s.toResp(ctx, inst)
}

func (s *Service) Get(ctx context.Context, req *presentationdto.GetReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || req.ID == "" {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, presentationenums.Err(presentationenums.ErrNotFound)
		}
		return nil, err
	}
	return s.toResp(ctx, inst)
}

func (s *Service) List(ctx context.Context, req *presentationdto.ListReq) (list []*presentationdto.InstanceResp, err error) {
	if req == nil {
		req = &presentationdto.ListReq{}
	}
	lang := strings.TrimSpace(req.Lang)
	if req.ActionTargetsOnly && lang == "" {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListInstances(ctx, projectID, req.EntityType)
	if err != nil {
		return nil, err
	}
	out := make([]*presentationdto.InstanceResp, 0, len(rows))
	for _, r := range rows {
		resp, rerr := s.toResp(ctx, r)
		if rerr != nil {
			return nil, rerr
		}
		if req.ActionTargetsOnly {
			active := false
			for _, pub := range resp.Publications {
				if pub.Lang != lang || pub.ArtifactID == nil || *pub.ArtifactID == "" {
					continue
				}
				path, pathErr := pipeline.ActivePublishedPath(s.publication, s.store, pub.ActivePath, pub.ArtifactHash)
				active = pathErr == nil && path != ""
				break
			}
			if !active {
				continue
			}
		}
		out = append(out, resp)
	}
	return out, nil
}

func (s *Service) Delete(ctx context.Context, req *presentationdto.DeleteReq) (err error) {
	if req == nil || req.ID == "" {
		return presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.ID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return presentationenums.Err(presentationenums.ErrNotFound)
		}
		return err
	}
	if err = s.ensureActionTargetUnused(ctx, inst.ProjectID, inst.ID); err != nil {
		return err
	}

	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	lock.Lock()
	defer lock.Unlock()

	paths := s.instanceActivePaths(ctx, inst)

	intent, ierr := s.registerDeactivationIntent(ctx, inst, paths)
	if ierr != nil {
		return ierr
	}

	var txErr error
	var commitUncertain bool
	if _, commitUncertain, txErr = s.m.InTransaction(ctx, func(tx *gorm.DB) error {

		if serr := rls.ScopeTx(tx, inst.ProjectID); serr != nil {
			return serr
		}

		if lerr := s.lockPresentationEntityOrAdvisory(ctx, tx, inst.ProjectID, inst.ID); lerr != nil {
			return lerr
		}

		if cerr := s.ensureDeactivationPathsUnchanged(ctx, inst, intent.Paths); cerr != nil {
			return cerr
		}

		if merr := s.routes.MarkDeactivationCommittedTx(ctx, tx,
			pubcontract.ReceiptOwner{SourceType: pubcontract.ReceiptSourcePresentation, SourceID: inst.ID},
			inst.ProjectID, intent.ReceiptID, intent.PathArtifacts); merr != nil {
			return fmt.Errorf("登记下线已提交事实失败: %w", merr)
		}
		if s.routes != nil {
			if rerr := s.routes.DeleteRoutesByPresentationTx(ctx, tx, &pubcontract.DeleteRoutesByPresentationReq{
				ProjectID: inst.ProjectID, PresentationID: inst.ID,
			}); rerr != nil {
				return fmt.Errorf("删除实例前释放路由占用失败: %w", rerr)
			}
		}
		return s.m.DeleteInstanceTx(tx, projectID, req.ID)
	}); txErr != nil {
		if commitUncertain {

			s.NotifyPendingReceipt()
			return txErr
		}

		s.abortPublishReceipt(ctx, intent.ReceiptID, "删除事务回滚")
		return txErr
	}

	return s.completeDeactivationIntent(ctx, inst, intent)
}

func (s *Service) instanceActivePaths(ctx context.Context, inst *presentationmodel.InstanceEntity) []string {
	var paths []string
	if pubs, perr := s.m.ListActivePathsForInstance(ctx, inst.ID); perr == nil && len(pubs) > 0 {
		paths = append(paths, pubs...)
	} else if inst.URLPath != "" {
		paths = append(paths, inst.URLPath)
	}
	if s.routes == nil {
		return dedupePaths(paths)
	}
	extra, err := s.routes.ListActivePathsByPresentation(ctx, &pubcontract.ListActivePathsByPresentationReq{
		ProjectID: inst.ProjectID, PresentationID: inst.ID,
	})
	if err != nil {
		logger.Scene("build").With("instanceId", inst.ID).
			Warn("读取实例已激活路径失败，仅清理已登记路径: " + err.Error())
		return dedupePaths(paths)
	}
	return dedupePaths(append(paths, extra...))
}

func dedupePaths(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, p := range in {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func (s *Service) findByEntityID(ctx context.Context, projectID, entityID string) (*presentationmodel.InstanceEntity, error) {
	rows, err := s.m.ListInstances(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.EntityID == entityID {
			return r, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (s *Service) toResp(ctx context.Context, e *presentationmodel.InstanceEntity) (resp *presentationdto.InstanceResp, err error) {
	resp = &presentationdto.InstanceResp{
		InstanceRole: e.InstanceRole,
		ID:           e.ID, ProjectID: e.ProjectID, EntityType: e.EntityType, EntityID: e.EntityID,
		URLPath: e.URLPath, TemplateID: e.TemplateID, Stale: e.Stale,
		UpdatedAt: e.UpdatedAt.Format("2006-01-02 15:04"),
	}
	publications, err := s.m.ListPublications(ctx, e.ID)
	if err != nil {
		return nil, err
	}
	for _, publication := range publications {
		resp.Publications = append(resp.Publications, presentationdto.InstancePublicationResp{Lang: publication.Lang, ActivePath: publication.ActivePath, ArtifactID: publication.ArtifactID, ArtifactHash: publication.ArtifactHash})
	}
	if e.ActiveArtifactID != nil {
		resp.Status = presentationenums.StatusActive
		resp.ArtifactID = *e.ActiveArtifactID

		if h, herr := s.m.GetArtifactHash(ctx, *e.ActiveArtifactID); herr == nil {
			resp.ArtifactHash = h
		}
	} else {
		resp.Status = presentationenums.StatusDraft
	}
	if e.CurrentSnapshotID != nil {
		resp.SnapshotID = *e.CurrentSnapshotID
	}

	resp.RenderMode = presentationmodel.NormalizeRenderMode(e.RenderMode)
	if presentationmodel.IsDocumentMode(e.RenderMode) && len(e.OverrideDocument) > 0 {
		resp.Document = e.OverrideDocument
	} else if e.CurrentSnapshotID != nil {
		if snap, serr := s.m.GetSnapshot(ctx, *e.CurrentSnapshotID); serr == nil {
			resp.Document = snap.Document

			resp.SourceTemplateVersionID = snap.SourceTemplateVersionID
		}
	}
	return resp, nil
}

func ArchivePathFor(entityType, slug string) string {
	return "/" + strings.Trim(strings.TrimSpace(entityType), "/") + "/" +
		strings.Trim(strings.TrimSpace(slug), "/")
}

func (s *Service) EnsureArchiveInstance(ctx context.Context, req *presentationdto.EnsureArchiveReq) (res *presentationdto.EnsureArchiveResp, err error) {
	if req == nil || strings.TrimSpace(req.EntityType) == "" || strings.TrimSpace(req.EntityID) == "" {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	slug := strings.TrimSpace(req.Slug)
	if slug == "" {

		return &presentationdto.EnsureArchiveResp{Skipped: "实体没有 slug，无法生成归档路径"}, nil
	}
	if s.templates == nil {
		return &presentationdto.EnsureArchiveResp{Skipped: "模板契约未装配"}, nil
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	tpl, terr := s.templates.ResolveTemplateByRoleScoped(ctx, projectID, req.EntityType, contenttemplatecontract.TemplateRoleArchive)
	if terr != nil {

		if strings.Contains(terr.Error(), "not found") || strings.Contains(terr.Error(), "ErrNotFound") {
			return &presentationdto.EnsureArchiveResp{Skipped: "该实体类型未配置归档模板"}, nil
		}
		return nil, terr
	}

	wantPath := ArchivePathFor(req.EntityType, slug)
	inst, cerr := s.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: projectID, EntityType: req.EntityType, EntityID: req.EntityID,
		URLPath: wantPath, TemplateID: tpl.TemplateID,
		InstanceRole: "archive",
	})
	if cerr != nil {
		return nil, cerr
	}

	if inst.URLPath != wantPath {
		updated, uerr := s.UpdateURL(ctx, &presentationdto.UpdateURLReq{
			ID: inst.ID, ProjectID: inst.ProjectID, NewPath: wantPath, WithRedirect: true,
		})
		if uerr != nil {
			return nil, uerr
		}
		inst = updated
	}
	return &presentationdto.EnsureArchiveResp{InstanceID: inst.ID, Created: true}, nil
}

func (s *Service) SaveOverrideDocument(ctx context.Context, req *presentationdto.SaveOverrideReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" || len(req.Document) == 0 {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	if !json.Valid(req.Document) {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.InstanceID)
	if err != nil {
		return nil, presentationenums.Err(presentationenums.ErrNotFound)
	}

	tpl, err := s.resolveBoundTemplate(ctx, inst, "")
	if err != nil {
		return nil, err
	}

	effective := instanceDocumentFor(inst, tpl)

	if documentsEqual(json.RawMessage(req.Document), effective.Document) {
		return s.toResp(ctx, inst)
	}

	if !presentationmodel.IsDocumentMode(inst.RenderMode) && !req.ConfirmDetach {
		return nil, presentationenums.Err(presentationenums.ErrDetachConfirmRequired)
	}

	mode := &instanceModePending{
		renderMode: presentationmodel.RenderModeDocument,
		document:   json.RawMessage(req.Document),
	}

	if _, err = s.publishAllLangs(ctx, inst, publishIntent{
		resolveDoc: func(ctx context.Context, fresh *presentationmodel.InstanceEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
			bound, rerr := s.resolveBoundTemplate(ctx, fresh, "")
			if rerr != nil {
				return nil, rerr
			}
			pending := *fresh
			pending.OverrideDocument = json.RawMessage(req.Document)
			pending.RenderMode = presentationmodel.RenderModeDocument
			return instanceDocumentFor(&pending, bound), nil
		},
		mode:    mode,
		trigger: presentationmodel.PublishTriggerOverride,
	}); err != nil {
		return nil, publishFailedErr(err)
	}
	return s.toResp(ctx, inst)
}

func (s *Service) ReapplyPreset(ctx context.Context, req *presentationdto.ReapplyPresetReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.InstanceID)
	if err != nil {
		return nil, presentationenums.Err(presentationenums.ErrNotFound)
	}

	switchTo := strings.TrimSpace(req.TemplateID)
	mode := &instanceModePending{renderMode: presentationmodel.RenderModeTemplate}

	trigger := presentationmodel.PublishTriggerSwitch
	if switchTo == "" {
		trigger = presentationmodel.PublishTriggerManual
	}
	if _, err = s.publishAllLangs(ctx, inst, publishIntent{
		resolveDoc: func(ctx context.Context, fresh *presentationmodel.InstanceEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
			return s.resolveBoundTemplate(ctx, fresh, switchTo)
		},
		mode:    mode,
		trigger: trigger,
	}); err != nil {
		return nil, publishFailedErr(err)
	}
	return s.toResp(ctx, inst)
}

func (s *Service) ClearOverride(ctx context.Context, req *presentationdto.ClearOverrideReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	return s.ReapplyPreset(ctx, &presentationdto.ReapplyPresetReq{
		InstanceID: req.InstanceID, ProjectID: req.ProjectID, TemplateID: req.TemplateID,
	})
}

func (s *Service) UpdateURL(ctx context.Context, req *presentationdto.UpdateURLReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}

	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.locateInstance(ctx, projectID, req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err == nil {
			s.invalidateActionTarget(ctx, inst.ID)
		}
	}()

	newLogical, err := s.normalizeLogicalPath(ctx, inst.ProjectID, req.NewPath)
	if err != nil {
		return nil, presentationenums.ErrDetail(presentationenums.ErrInvalidPath, err.Error())
	}

	var (
		oldPubs    []presentationmodel.PublicationEntity
		oldLogical string
	)
	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	if err = func() error {
		lock.Lock()
		defer lock.Unlock()

		fresh, gerr := s.m.GetInstance(ctx, inst.ProjectID, inst.ID)
		if gerr != nil {
			return presentationenums.Err(presentationenums.ErrNotFound)
		}
		inst = fresh
		oldLogical = s.instanceLogicalPath(ctx, inst)
		if newLogical == oldLogical {
			return presentationenums.Err(presentationenums.ErrSamePath)
		}
		oldPubs, _ = s.m.ListPublications(ctx, inst.ID)

		return s.ensureLogicalPathFree(ctx, inst.ProjectID, newLogical, inst.ID)
	}(); err != nil {
		return nil, err
	}

	plan := make([]oldPathIntent, 0, len(oldPubs))
	for _, pub := range oldPubs {
		if pub.ActivePath == "" || pub.ActivePath == newLogical {
			continue
		}
		from := ""
		if pub.ArtifactID != nil {
			from = *pub.ArtifactID
		}
		plan = append(plan, oldPathIntent{
			lang: pub.Lang, oldPath: pub.ActivePath, newPath: newLogical,
			fromArtifactID: from, redirect: req.WithRedirect,
		})
	}

	_, err = s.publishAllLangs(ctx, inst, publishIntent{

		resolveDoc: func(ctx context.Context, fresh *presentationmodel.InstanceEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
			return s.resolveBoundTemplate(ctx, fresh, "")
		},
		logicalPath: newLogical,

		inputPath: newLogical,
		oldPaths:  plan,
		trigger:   presentationmodel.PublishTriggerURL,
	})
	if err != nil {
		return nil, publishFailedErr(err)
	}
	logger.Scene("build").With("instanceId", inst.ID).With("oldPath", oldLogical).With("newPath", newLogical).
		Info("详情页 URL 修改完成")
	return s.toResp(ctx, inst)
}

func (s *Service) locateInstance(ctx context.Context, projectID string, req *presentationdto.UpdateURLReq) (*presentationmodel.InstanceEntity, error) {
	if id := strings.TrimSpace(req.ID); id != "" {
		inst, err := s.m.GetInstance(ctx, projectID, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, presentationenums.Err(presentationenums.ErrNotFound)
			}
			return nil, err
		}
		return inst, nil
	}
	entityType, entityID := strings.TrimSpace(req.EntityType), strings.TrimSpace(req.EntityID)
	if entityType == "" || entityID == "" {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	inst, err := s.m.GetInstanceByEntity(ctx, projectID, entityType, entityID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, presentationenums.Err(presentationenums.ErrNotFound)
		}
		return nil, err
	}
	return inst, nil
}

func (s *Service) ensurePathFree(ctx context.Context, projectID, path, excludeInstanceID string) error {
	if s.routes != nil {
		occupied, err := s.routes.IsPathOccupied(ctx, &pubcontract.IsOccupiedReq{
			ProjectID: projectID, Path: path, ExcludePresentationID: excludeInstanceID,
		})
		if err != nil {
			return err
		}
		if occupied {
			logger.Scene("build").With("url", path).Warn("详情页路径预检被拒绝：已被其他实体占用")
			return presentationenums.Err(presentationenums.ErrPathOccupied)
		}
	}
	if _, err := s.m.FindInstanceByPath(ctx, projectID, path, excludeInstanceID); err == nil {
		logger.Scene("build").With("url", path).Warn("详情页路径预检被拒绝：已被其他展示实例占用")
		return presentationenums.Err(presentationenums.ErrPathOccupied)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if _, err := s.m.FindInstanceByActivePath(ctx, projectID, path, excludeInstanceID); err == nil {
		logger.Scene("build").With("url", path).Warn("详情页路径预检被拒绝：已被其他展示实例的多语言路径占用")
		return presentationenums.Err(presentationenums.ErrPathOccupied)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

type blockResolverAdapter struct {
	blocks blockcontract.BlockService
	ctx    context.Context

	projectID string
	cache     map[string]*builder.Page
	errs      map[string]error
}

func newBlockResolverAdapter(blocks blockcontract.BlockService, ctx context.Context, projectID string) *blockResolverAdapter {
	return &blockResolverAdapter{
		blocks: blocks, ctx: ctx, projectID: projectID,
		cache: map[string]*builder.Page{}, errs: map[string]error{},
	}
}

func (a *blockResolverAdapter) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	page, err := a.blockPage(blockID)
	if err != nil {
		return nil, err
	}
	return page.Root, nil
}

func (a *blockResolverAdapter) blockPage(blockID string) (*builder.Page, error) {
	if page, ok := a.cache[blockID]; ok {
		return page, nil
	}
	if err, ok := a.errs[blockID]; ok {
		return nil, err
	}
	fail := func(err error) (*builder.Page, error) {
		a.errs[blockID] = err
		return nil, err
	}
	if a.blocks == nil {
		return fail(fmt.Errorf("全局块 %s 不可用（block 契约未装配）", blockID))
	}
	block, err := a.blocks.Detail(a.ctx, &blockcontract.DetailReq{ProjectID: a.projectID, ID: blockID})
	if err != nil || block == nil {
		return fail(fmt.Errorf("全局块 %s 不可用", blockID))
	}
	if block.ReuseMode == "template" {
		return fail(fmt.Errorf("全局块 %s 为一次性复制片段，不能被引用展开", blockID))
	}
	page, err := builder.ParsePage(block.Document)
	if err != nil {
		return fail(err)
	}
	a.cache[blockID] = page
	return page, nil
}

var _ core.BlockResolver = (*blockResolverAdapter)(nil)

func (s *Service) ResolveStructureDocument(ctx context.Context, projectID, templateID string) ([]byte, error) {
	if s == nil || s.templates == nil {
		return nil, fmt.Errorf("结构模板 %s 不可用（contenttemplate 契约未装配）", templateID)
	}
	tpl, err := s.templates.ResolveTemplateByIDScoped(ctx, projectID, templateID)
	if err != nil {
		return nil, err
	}
	if tpl == nil || len(tpl.Document) == 0 {
		return nil, fmt.Errorf("结构模板 %s 无可用文档", templateID)
	}
	return tpl.Document, nil
}

var _ pipeline.StructureTemplatePort = (*Service)(nil)

func (s *Service) ListBlockSourceRefs(ctx context.Context, blockID string) (out []blockcontract.BlockUsage, err error) {
	if s == nil || s.project == nil {

		return nil, errors.New("project 契约未装配，无法逐工程反查自动发布实例的块引用")
	}
	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range projects {
		projectID := strings.TrimSpace(projects[i].ID)
		if projectID == "" {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		rows, rerr := s.m.ListBlockDocumentRefs(ctx, projectID, blockID)
		if rerr != nil {
			return nil, rerr
		}
		for j := range rows {
			label := strings.TrimSpace(rows[j].URLPath)
			if label == "" {
				label = strings.TrimSpace(rows[j].EntityType) + ":" + strings.TrimSpace(rows[j].EntityID)
			}
			detail := "snapshot"
			if !rows[j].FromSnapshot {
				detail = "override_document"
			}
			out = append(out, blockcontract.BlockUsage{
				Kind: blockcontract.UsageKindPresentationInstance, ProjectID: projectID,
				EntityID: rows[j].InstanceID, Label: label, Detail: detail,
			})
		}
	}
	return out, nil
}

type instanceModePending struct {
	renderMode string
	document   json.RawMessage
}

func instanceDocumentFor(inst *presentationmodel.InstanceEntity,
	tpl *contenttemplatecontract.ResolvedTemplate) *contenttemplatecontract.ResolvedTemplate {
	if inst != nil && presentationmodel.IsDocumentMode(inst.RenderMode) {
		return withInstanceDocument(inst, tpl)
	}
	return tpl
}

func documentsEqual(a, b json.RawMessage) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	an, aerr := json.Marshal(av)
	bn, berr := json.Marshal(bv)
	if aerr != nil || berr != nil {
		return false
	}
	return bytes.Equal(an, bn)
}

func (s *Service) RollbackArtifact(ctx context.Context, req *presentationdto.RollbackArtifactReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" || strings.TrimSpace(req.TargetHash) == "" {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.InstanceID)
	if err != nil {
		return nil, presentationenums.Err(presentationenums.ErrNotFound)
	}

	lock := s.lockInstance(inst.EntityType, inst.EntityID)
	lock.Lock()
	defer lock.Unlock()

	art, aerr := s.m.GetArtifactByHash(ctx, inst.ID, req.TargetHash)
	if aerr != nil {
		return nil, presentationenums.Err(presentationenums.ErrRollbackTargetMiss)
	}

	loc := pipeline.ArtifactLocator(art.ArtifactHash)
	if verr := s.store.VerifyArtifact(loc, art.ArtifactHash); verr != nil {
		logger.Scene("build").With("presentation_id", inst.ID).With("hash", art.ArtifactHash).
			Warn("产物回滚目标文件不在位，拒绝回滚")
		return nil, presentationenums.Err(presentationenums.ErrRollbackTargetMiss)
	}

	now := time.Now().UTC()
	artID := art.ID

	fromLedger, ferr := s.ledgerIdentities(ctx, inst.ID)
	if ferr != nil {
		return nil, presentationenums.ErrDetail(presentationenums.ErrRollbackFailed, ferr.Error())
	}
	var lockedLedger []ledgerIdentity
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if serr := rls.ScopeTx(tx, inst.ProjectID); serr != nil {
			return serr
		}

		if lerr := s.lockPresentationEntityOrAdvisory(ctx, tx, inst.ProjectID, inst.ID); lerr != nil {
			return lerr
		}
		locked, lerr := s.m.LockInstanceForActivationTx(ctx, tx, inst.ProjectID, inst.ID)
		if lerr != nil {
			return lerr
		}

		lockedPaths := s.instanceActivePaths(ctx, locked)
		if !containsPath(lockedPaths, inst.URLPath) {
			return fmt.Errorf("%w: 回滚目标路径已不属于本实例", errOldPathFSTargetForeign)
		}

		lockedLedger, lerr = s.ledgerIdentitiesTx(ctx, tx, inst.ID)
		if lerr != nil {
			return lerr
		}
		if cerr := compareRollbackLedger(fromLedger, lockedLedger); cerr != nil {
			return cerr
		}
		inst.ActiveArtifactID = &artID
		inst.StagedArtifactID = &artID
		inst.Stale = false
		inst.PublishedAt = &now
		inst.UpdatedAt = now
		if uerr := s.m.UpdateInstancePointersTx(tx, inst.ProjectID, inst); uerr != nil {
			return uerr
		}

		return s.withPath(inst.URLPath, func() error {
			return s.publication.Activate(inst.URLPath, loc)
		})
	})
	if err != nil {
		logger.Scene("build").With("presentation_id", inst.ID).With("path", inst.URLPath).
			Error(err, "产物回滚激活失败")
		return nil, presentationenums.ErrDetail(presentationenums.ErrRollbackFailed, err.Error())
	}
	return s.toResp(ctx, inst)
}

type LedgerIdentity = ledgerIdentity

func CompareRollbackLedger(from, own []LedgerIdentity) error { return compareRollbackLedger(from, own) }

type ledgerIdentity struct {
	Lang       string
	ReceiptID  string
	ArtifactID string
}

func (s *Service) ledgerIdentities(ctx context.Context, instanceID string) ([]ledgerIdentity, error) {
	rows, err := s.m.ListPublicationsForInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	return ledgerIdentitiesOf(rows), nil
}

func (s *Service) ledgerIdentitiesTx(ctx context.Context, tx *gorm.DB, instanceID string) ([]ledgerIdentity, error) {
	rows, err := s.m.ListPublicationsTx(tx, instanceID)
	if err != nil {
		return nil, err
	}
	return ledgerIdentitiesOf(rows), nil
}

func receiptIDText(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func ledgerIdentitiesOf(rows []presentationmodel.PublicationEntity) []ledgerIdentity {
	out := make([]ledgerIdentity, 0, len(rows))
	for _, row := range rows {
		art := ""
		if row.ArtifactID != nil {
			art = strings.TrimSpace(*row.ArtifactID)
		}
		out = append(out, ledgerIdentity{
			Lang:       row.Lang,
			ReceiptID:  receiptIDText(row.ReceiptID),
			ArtifactID: art,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Lang < out[j].Lang })
	return out
}

func compareRollbackLedger(from, own []ledgerIdentity) error {
	if len(from) != len(own) {
		return fmt.Errorf("%w: 回滚期间账本语言集合发生变化", errPublishStateMoved)
	}
	byLang := make(map[string]ledgerIdentity, len(from))
	for _, row := range from {
		byLang[row.Lang] = row
	}
	for _, row := range own {
		f, ok := byLang[row.Lang]
		if !ok {
			return fmt.Errorf("%w: 回滚期间新增了语言账本 %s", errPublishStateMoved, row.Lang)
		}
		if row.ReceiptID != "" && f.ReceiptID != "" {
			if row.ReceiptID != f.ReceiptID {
				return fmt.Errorf("%w: 语言 %s 的账本已由另一次成功发布接管（from=%s own=%s）",
					errPublishStateMoved, row.Lang, f.ReceiptID, row.ReceiptID)
			}
			continue
		}

		if row.ArtifactID != f.ArtifactID {
			return fmt.Errorf("%w: 语言 %s 的账本产物在回滚期间发生变化", errPublishStateMoved, row.Lang)
		}
	}
	return nil
}

func containsPath(paths []string, want string) bool {
	w := strings.TrimSpace(want)
	if w == "" {
		return false
	}
	for _, p := range paths {
		if strings.TrimSpace(p) == w {
			return true
		}
	}
	return false
}

func (s *Service) RollbackDocument(ctx context.Context, req *presentationdto.RollbackDocumentReq) (res *presentationdto.InstanceResp, err error) {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" || strings.TrimSpace(req.SnapshotID) == "" {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.InstanceID)
	if err != nil {
		return nil, presentationenums.Err(presentationenums.ErrNotFound)
	}

	snap, serr := s.m.GetSnapshot(ctx, req.SnapshotID)
	if serr != nil {
		return nil, presentationenums.Err(presentationenums.ErrRollbackTargetMiss)
	}

	if snap.PresentationInstanceID != inst.ID {
		return nil, presentationenums.Err(presentationenums.ErrSnapshotMismatch)
	}
	mode := &instanceModePending{
		renderMode: presentationmodel.RenderModeDocument,
		document:   snap.Document,
	}
	if _, err = s.publishAllLangs(ctx, inst, publishIntent{
		resolveDoc: func(ctx context.Context, fresh *presentationmodel.InstanceEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
			bound, rerr := s.resolveBoundTemplate(ctx, fresh, "")
			if rerr != nil {
				return nil, rerr
			}
			pending := *fresh
			pending.OverrideDocument = snap.Document
			pending.RenderMode = presentationmodel.RenderModeDocument
			return instanceDocumentFor(&pending, bound), nil
		},
		mode:    mode,
		trigger: presentationmodel.PublishTriggerRollback,
	}); err != nil {
		return nil, publishFailedErr(err)
	}
	return s.toResp(ctx, inst)
}

func (s *Service) CountByTemplate(ctx context.Context, req *presentationdto.CountByTemplateReq) (
	res *presentationdto.CountByTemplateResp, err error) {
	if req == nil || strings.TrimSpace(req.TemplateID) == "" {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	tplMode, docMode, err := s.m.CountInstancesByTemplate(ctx, projectID, req.TemplateID)
	if err != nil {
		return nil, err
	}
	return &presentationdto.CountByTemplateResp{
		TemplateMode: tplMode, DocumentMode: docMode, Total: tplMode + docMode,
	}, nil
}

func (s *Service) ListSnapshots(ctx context.Context, req *presentationdto.ListSnapshotsReq) (
	list []*presentationdto.SnapshotSummary, err error) {
	if req == nil || strings.TrimSpace(req.InstanceID) == "" {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	inst, err := s.m.GetInstance(ctx, projectID, req.InstanceID)
	if err != nil {
		return nil, presentationenums.Err(presentationenums.ErrNotFound)
	}
	rows, err := s.m.ListSnapshots(ctx, inst.ID, req.Limit)
	if err != nil {
		return nil, err
	}
	list = make([]*presentationdto.SnapshotSummary, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		list = append(list, &presentationdto.SnapshotSummary{
			ID:                      r.ID,
			SourceTemplateVersionID: r.SourceTemplateVersionID,
			CreatedAt:               r.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	return list, nil
}
