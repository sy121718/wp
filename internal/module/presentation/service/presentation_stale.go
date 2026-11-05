package presentationservice

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	publish "go_wp/pkg/publish"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	mediacontract "go_wp/internal/module/media/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationdto "go_wp/internal/module/presentation/dto"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/builder"
	"go_wp/pkg/logger"
	"go_wp/pkg/seo"
)

func (s *Service) MarkStaleByDependency(ctx context.Context, kind, key string) (ids []string, err error) {
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(key) == "" {
		return nil, nil
	}
	if s.project == nil {
		return nil, presentationenums.Err(presentationenums.ErrProjectRequired)
	}
	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	at := time.Now().UTC()

	templateKind := kind == publish.DepKindContentTemplate
	seen := make(map[string]bool)
	for _, p := range projects {
		if ctx.Err() != nil {
			break
		}
		var hit []string
		var herr error
		if templateKind {
			hit, herr = s.m.MarkStaleTemplateModeByDependency(ctx, p.ID, kind, key, at)
		} else {
			hit, herr = s.m.MarkStaleByDependency(ctx, p.ID, kind, key, at)
		}
		if herr != nil {
			return nil, herr
		}
		for _, id := range hit {
			if seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *Service) MarkStaleByRegistryVersion(ctx context.Context, current string) (ids []string, err error) {
	if strings.TrimSpace(current) == "" {
		return nil, nil
	}
	if s.project == nil {
		return nil, presentationenums.Err(presentationenums.ErrProjectRequired)
	}
	currentArtifactIDs, err := s.m.ListCurrentArtifactIDs(ctx)
	if err != nil {
		return nil, err
	}
	stale, err := s.m.ListStaleInstanceIDsByRegistryVersion(ctx, current, currentArtifactIDs)
	if err != nil {
		return nil, err
	}
	if len(stale) == 0 {
		return nil, nil
	}
	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	at := time.Now().UTC()
	seen := make(map[string]bool, len(stale))
	for _, p := range projects {
		if ctx.Err() != nil {
			break
		}

		hit, merr := s.m.MarkStaleByIDs(ctx, p.ID, stale, at)
		if merr != nil {
			return nil, merr
		}
		for _, id := range hit {
			if seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	logger.Scene("presentation").With("count", len(ids)).With("registryVersion", current).
		With("sample", sampleInstanceIDs(ids)).
		Info("组件注册表版本变化：相关自动发布实例已标记待重建")
	return ids, nil
}

const registryImpactSampleLimit = 10

func sampleInstanceIDs(ids []string) []string {
	if len(ids) <= registryImpactSampleLimit {
		return ids
	}
	return ids[:registryImpactSampleLimit]
}

func (s *Service) SetBuildQueue(q presentationcontract.BuildQueueEnqueuer) {
	if s == nil {
		return
	}
	s.buildQueue = q
}

func (s *Service) RebuildStale(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if s.buildQueue != nil {
		return s.enqueueStaleRebuilds(ctx, ids)
	}
	if len(ids) > maxAutoRebuildInstances {
		logger.Scene("dependency").With("affected", len(ids)).With("limit", maxAutoRebuildInstances).
			Warn("自动重建超出单次上限，剩余实例保持 stale 等待后续触发")
		ids = ids[:maxAutoRebuildInstances]
	}
	rebuilt := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			return nil
		}
		if err := s.RebuildInstance(ctx, id); err != nil {
			logger.Scene("dependency").With("presentation_id", id).
				Error(err, "依赖失效后的自动重建失败（实例保持 stale）")
			continue
		}
		rebuilt++
	}
	if rebuilt > 0 {
		logger.Scene("dependency").With("rebuilt", rebuilt).Info("依赖失效后的自动重建完成")
	}
	return nil
}

func (s *Service) RebuildStaleSync(ctx context.Context, projectID string) (rebuilt int, err error) {
	pid := strings.TrimSpace(projectID)
	if pid == "" {
		return 0, presentationenums.Err(presentationenums.ErrProjectRequired)
	}
	ids, lerr := s.m.ListStaleInstanceIDs(ctx, pid)
	if lerr != nil {
		return 0, lerr
	}
	if len(ids) == 0 {

		return 0, nil
	}
	for _, id := range ids {
		if cerr := ctx.Err(); cerr != nil {
			return rebuilt, cerr
		}
		if rerr := s.RebuildInstance(ctx, id); rerr != nil {
			logger.Scene("dependency").With("presentation_id", id).With("project_id", pid).
				Error(rerr, "menu_stale 同步重建失败（实例保持 stale，错误上抛而不是再次入队）")
			return rebuilt, rerr
		}
		rebuilt++
	}
	logger.Scene("dependency").With("project_id", pid).With("rebuilt", rebuilt).
		Info("menu_stale 同步重建完成")
	return rebuilt, nil
}

func (s *Service) MarkInstanceMenuStaleTx(ctx context.Context, tx *gorm.DB,
	projectID, kind string, at time.Time) (n int64, err error) {
	if tx == nil {
		return 0, presentationenums.ErrDetail(presentationenums.ErrInvalidParam, "menu stale mark requires caller transaction")
	}
	pid := strings.TrimSpace(projectID)
	if pid == "" {
		return 0, presentationenums.Err(presentationenums.ErrProjectRequired)
	}
	if !presentationmodel.IsMenuStaleKind(kind) {
		return 0, presentationenums.ErrDetail(presentationenums.ErrInvalidParam, "unknown menu kind "+kind)
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return s.m.MarkInstanceMenuStaleTx(ctx, tx, pid, kind, at)
}

func (s *Service) MarkInstanceNavigationStaleTx(ctx context.Context, tx *gorm.DB,
	projectID, navigationID string, at time.Time) (n int64, err error) {
	if tx == nil {
		return 0, presentationenums.ErrDetail(presentationenums.ErrInvalidParam, "navigation stale mark requires caller transaction")
	}
	pid := strings.TrimSpace(projectID)
	if pid == "" {
		return 0, presentationenums.Err(presentationenums.ErrProjectRequired)
	}
	nid := strings.TrimSpace(navigationID)
	if nid == "" {
		return 0, presentationenums.ErrDetail(presentationenums.ErrInvalidParam, "navigation id is required")
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return s.m.MarkInstanceNavigationStaleTx(ctx, tx, pid, nid, at)
}

var _ presentationcontract.PresentationStalePort = (*Service)(nil)

func (s *Service) enqueueStaleRebuilds(ctx context.Context, ids []string) error {
	queued := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		if err := s.buildQueue.EnqueuePresentationBuild(ctx, id, s.projectOfInstance(ctx, id)); err != nil {
			logger.Scene("dependency").With("presentation_id", id).
				Error(err, "自动重建任务入队失败（实例保持 stale）")
			continue
		}
		queued++
	}
	logger.Scene("dependency").With("queued", queued).With("affected", len(ids)).
		Info("依赖失效的自动重建已交给构建队列")
	return nil
}

func (s *Service) RebuildInstance(ctx context.Context, instanceID string) error {
	inst, err := s.findOneInstanceAnyProject(ctx, instanceID)
	if err != nil {
		return presentationenums.ErrDetail(presentationenums.ErrNotFound, err.Error())
	}

	_, err = s.rebuildInstance(ctx, inst, publishIntent{
		resolveDoc: func(ctx context.Context, fresh *presentationmodel.InstanceEntity) (*contenttemplatecontract.ResolvedTemplate, error) {
			tpl, rerr := s.resolveBoundTemplate(ctx, fresh, "")
			if rerr != nil {
				return nil, rerr
			}

			return instanceDocumentFor(fresh, tpl), nil
		},

		trigger: presentationmodel.PublishTriggerAuto,
	})
	return err
}

func (s *Service) projectOfInstance(ctx context.Context, instanceID string) string {
	inst, err := s.findOneInstanceAnyProject(ctx, instanceID)
	if err != nil {
		return ""
	}
	return inst.ProjectID
}

func (s *Service) findOneInstanceAnyProject(ctx context.Context, instanceID string) (*presentationmodel.InstanceEntity, error) {
	if ids := strings.TrimSpace(instanceID); ids == "" {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	if s.project == nil {
		return nil, presentationenums.Err(presentationenums.ErrProjectRequired)
	}
	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		inst, gerr := s.m.GetInstance(ctx, p.ID, instanceID)
		if gerr == nil {
			return inst, nil
		}
		if !errors.Is(gerr, gorm.ErrRecordNotFound) {
			return nil, gerr
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (s *Service) PreviewInstance(ctx context.Context, req *presentationdto.PreviewInstanceReq) (res *presentationdto.PreviewInstanceResp, err error) {
	var tpl *contenttemplatecontract.ResolvedTemplate
	if req != nil && strings.TrimSpace(req.InstanceID) != "" {
		instance, err := s.Get(ctx, &presentationdto.GetReq{ID: req.InstanceID, ProjectID: req.ProjectID})
		if err != nil {
			return nil, err
		}
		effective := *req
		effective.EntityType, effective.EntityID, effective.ProjectID = instance.EntityType, instance.EntityID, instance.ProjectID
		effective.TemplateID = instance.TemplateID
		if len(effective.DraftDocument) == 0 {
			effective.DraftDocument = instance.Document
		}
		if len(effective.DraftDocument) == 0 {
			return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
		}

		tpl = &contenttemplatecontract.ResolvedTemplate{
			TemplateID: instance.TemplateID, VersionID: instance.SourceTemplateVersionID,
			EntityType: instance.EntityType, TemplateRole: instance.InstanceRole, Document: effective.DraftDocument,
		}
		req = &effective
	}
	if req == nil || req.EntityType == "" || req.EntityID == "" {
		return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if tpl == nil {
		tpl, err = s.resolveTemplate(ctx, projectID, req.EntityType, req.TemplateID)
		if err != nil {
			return nil, err
		}
	}
	if len(req.DraftDocument) > 0 {
		if !json.Valid(req.DraftDocument) {
			return nil, presentationenums.Err(presentationenums.ErrInvalidParam)
		}
		override := *tpl
		override.Document = req.DraftDocument
		tpl = &override
	}

	html, err := s.renderHTML(ctx, req.EntityType, req.EntityID, "", projectID, "", nil, tpl, nil,
		builder.CompileModePreview, nil)
	if err != nil {
		return nil, presentationenums.ErrDetail(presentationenums.ErrBuildFailed, err.Error())
	}
	return &presentationdto.PreviewInstanceResp{
		HTML: string(html), EntityType: req.EntityType, EntityID: req.EntityID,
		TemplateID: tpl.TemplateID, TemplateName: tpl.TemplateName,
		TemplateVersionID: tpl.VersionID, TemplateVersion: tpl.Version,
	}, nil
}

var _ mediacontract.StaleMarker = (*Service)(nil)

func (s *Service) RefKinds() []string { return []string{mediacontract.RefKindPresentation} }

func (s *Service) MarkStaleByMediaRefs(ctx context.Context, refs []mediacontract.MediaRef) (marked []string, err error) {
	if s == nil || s.m == nil {
		return nil, presentationenums.Err(presentationenums.ErrProjectRequired)
	}
	ids := make([]string, 0, len(refs))
	seen := map[string]bool{}
	for i := range refs {
		if strings.TrimSpace(refs[i].Kind) != mediacontract.RefKindPresentation {
			continue
		}
		id := strings.TrimSpace(refs[i].ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if s.project == nil {
		return nil, presentationenums.Err(presentationenums.ErrProjectRequired)
	}
	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	at := time.Now().UTC()
	markedSeen := map[string]bool{}
	for _, p := range projects {
		if ctx.Err() != nil {
			return marked, ctx.Err()
		}
		hit, merr := s.m.MarkStaleByIDs(ctx, p.ID, ids, at)
		if merr != nil {
			return nil, merr
		}
		for _, id := range hit {
			if id == "" || markedSeen[id] {
				continue
			}
			markedSeen[id] = true
			marked = append(marked, id)
		}
	}
	logger.Scene("presentation").With("affected", len(marked)).
		With("refs", len(ids)).Info("换图已标记自动发布实例待重建")
	return marked, nil
}

func presentationSiteBaseURL() string {
	return strings.TrimSpace(os.Getenv("WP_SITE_BASE_URL"))
}

func (s *Service) notifyIndexNow(ctx context.Context, inst *presentationmodel.InstanceEntity, paths ...string) {
	if s.project == nil || inst == nil || len(paths) == 0 {
		return
	}
	project, err := s.project.Detail(ctx, &projectcontract.DetailReq{ID: inst.ProjectID})
	if err != nil || project == nil {
		return
	}
	settings := projectcontract.ParseSiteSettings(project.Settings)
	seo.NotifyIndexNowAsync(presentationSiteBaseURL(), settings.IndexNowKey, paths)
}
