package pageservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	mediacontract "go_wp/internal/module/media/contract"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pagemodel "go_wp/internal/module/page/model"
	projectcontract "go_wp/internal/module/project/contract"
	pubcontract "go_wp/internal/module/publication/contract"
	pubenums "go_wp/internal/module/publication/enums"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

func (s *Service) Create(ctx context.Context, req *pagedto.CreateReq) (res *pagedto.PageResp, err error) {
	if req == nil {
		return nil, ErrInvalidParam
	}
	if err = s.requireProject(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	if err = validateKind(req.Kind, req.ContentTargetType, req.ContentTargetID); err != nil {
		return nil, err
	}

	initial := req.DraftDocument
	if blueprintID := strings.TrimSpace(req.BlueprintID); blueprintID != "" {
		built, berr := s.initFromBlueprint(ctx, blueprintID)
		if berr != nil {
			return nil, berr
		}
		initial = built
	}
	if len(initial) == 0 {

		return nil, ErrInvalidParam
	}
	path, doc, err := validateDraft(req.DraftPath, initial)
	if err != nil {
		return nil, err
	}

	if doc, err = s.mergeActiveTheme(ctx, req.ProjectID, doc); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	page := &pagemodel.PageEntity{
		ID: uuid.NewString(), ProjectID: req.ProjectID, Kind: req.Kind,
		ContentTargetType: req.ContentTargetType, ContentTargetID: req.ContentTargetID,
		DraftPath: path, DraftDocument: doc, DraftVersion: 1, Stale: true,
		CreatedAt: now, UpdatedAt: now,
	}

	if themeID := s.ActiveThemeID(ctx, req.ProjectID); themeID != "" {
		page.ThemeID = &themeID
	}
	revision := &pagemodel.RevisionEntity{
		ID: uuid.NewString(), PageID: page.ID, Version: page.DraftVersion,
		DraftPath: path, DraftDocument: doc, SourceHash: hash(doc), CreatedAt: now,
	}

	if err = s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		if rerr := s.reservePathTx(ctx, tx, page.ProjectID, path, page.ID); rerr != nil {
			return rerr
		}
		return s.model.CreateWithRevisionTx(ctx, tx, page, revision)
	}); err != nil {
		return nil, mapPersistenceError(err)
	}
	return pageResp(page), nil
}

func (s *Service) initFromBlueprint(ctx context.Context, blueprintID string) (json.RawMessage, error) {
	if s.blueprints == nil {
		return nil, pageenums.Err(pageenums.ErrBlueprintUnavailable)
	}
	doc, err := s.blueprints.InitPageDocument(ctx, blueprintID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", pageenums.ErrBlueprintInvalid, err)
	}
	if len(doc) == 0 {
		return nil, pageenums.Err(pageenums.ErrBlueprintInvalid)
	}
	return doc, nil
}

func (s *Service) reservePathTx(ctx context.Context, tx *gorm.DB, projectID, path, pageID string) error {
	if s.routes == nil {
		return nil
	}
	routePaths, err := s.siteRoutePaths(ctx, projectID, path)
	if err != nil {
		return ErrInvalidPath
	}
	for _, routePath := range routePaths {
		rerr := s.routes.ReservePathTx(ctx, tx, &pubcontract.ReserveReq{ProjectID: projectID, Path: routePath, PageID: pageID})
		if rerr == nil {
			continue
		}

		if errors.Is(rerr, gorm.ErrDuplicatedKey) || strings.Contains(rerr.Error(), "23505") ||
			strings.Contains(rerr.Error(), pubenums.ErrRouteOccupied) {
			return ErrPathOccupied
		}
		return rerr
	}
	return nil
}

func (s *Service) Detail(ctx context.Context, req *pagedto.DetailReq) (res *pagedto.PageResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.ProjectID) == "" {
		return nil, ErrInvalidParam
	}
	if err = s.requireProject(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	page, err := s.model.GetByID(ctx, req.ID, req.ProjectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPageNotFound
	}
	if err != nil {
		return nil, err
	}
	res = pageResp(page)

	if res.Publications, err = s.publicationsOf(ctx, page.ID); err != nil {
		return nil, err
	}
	return res, nil
}

func (s *Service) ProjectOfPage(ctx context.Context, pageID string) (projectID string, err error) {
	id := strings.TrimSpace(pageID)
	if id == "" {
		return "", ErrInvalidParam
	}

	page, err := s.locatePageInProjects(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrPageNotFound
	}
	if err != nil {
		return "", err
	}
	return page.ProjectID, nil
}

func (s *Service) publicationsOf(ctx context.Context, pageID string) (out []pagedto.PagePublicationResp, err error) {
	rows, err := s.model.ListPublications(ctx, pageID)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out = make([]pagedto.PagePublicationResp, 0, len(rows))
	for _, r := range rows {
		at := r.PublishedAt
		out = append(out, pagedto.PagePublicationResp{
			Lang: r.Lang, ActivePath: r.ActivePath, ArtifactID: r.ArtifactID,
			ArtifactHash: r.ArtifactHash, PublishedAt: utils.NewJSONTimePtr(&at),
		})
	}
	return out, nil
}

func (s *Service) SaveDraft(ctx context.Context, req *pagedto.SaveDraftReq) (res *pagedto.PageResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrInvalidParam
	}
	path, doc, err := validateDraft(req.DraftPath, req.DraftDocument)
	if err != nil {
		return nil, err
	}

	page, err := s.locatePageInProjects(ctx, req.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPageNotFound
	}
	if err != nil {
		return nil, err
	}
	if req.ExpectedVersion != page.DraftVersion {
		return nil, ErrDraftVersionConflict
	}

	if doc, err = s.mergeActiveTheme(ctx, page.ProjectID, doc); err != nil {
		return nil, err
	}

	nextVersion := page.DraftVersion + 1
	now := time.Now().UTC()
	revision := &pagemodel.RevisionEntity{
		ID: uuid.NewString(), PageID: page.ID, Version: nextVersion,
		DraftPath: path, DraftDocument: doc, SourceHash: hash(doc), CreatedAt: now,
	}
	changedPath := page.DraftPath != path

	routeLangs := s.enabledLangsOf(ctx, page.ProjectID)

	if err = s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		if changedPath {
			if rerr := s.renameReservedAllLangsTx(ctx, tx, renameReservedInput{
				ProjectID: page.ProjectID, PageID: page.ID,
				OldLogical: page.DraftPath, NewLogical: path, Langs: routeLangs,
			}); rerr != nil {
				return rerr
			}
		}
		return s.model.SaveDraftWithRevisionTx(ctx, tx, page.ProjectID, page.ID, page.DraftVersion,
			path, doc, nextVersion, now, revision)
	}); err != nil {
		return nil, mapPersistenceError(err)
	}
	page.DraftPath = path
	page.DraftDocument = doc
	page.DraftVersion = nextVersion
	page.Stale = true
	page.UpdatedAt = now

	s.pruneRevisions(ctx, page.ProjectID, page.ID)
	return pageResp(page), nil
}

func (s *Service) ListRevisions(ctx context.Context, req *pagedto.RevisionReq) (res []pagedto.RevisionResp, err error) {
	if req == nil || strings.TrimSpace(req.PageID) == "" {
		return nil, ErrInvalidParam
	}

	page, err := s.locatePageInProjects(ctx, req.PageID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPageNotFound
	} else if err != nil {
		return nil, err
	}
	list, err := s.model.ListRevisions(ctx, page.ProjectID, req.PageID)
	if err != nil {
		return nil, err
	}
	res = make([]pagedto.RevisionResp, 0, len(list))
	for _, r := range list {
		res = append(res, pagedto.RevisionResp{
			ID: r.ID, PageID: r.PageID, Version: r.Version, DraftPath: r.DraftPath,
			DraftDocument: r.DraftDocument, SourceHash: r.SourceHash, CreatedAt: utils.NewJSONTime(r.CreatedAt),
		})
	}
	return res, nil
}

func (s *Service) requireProject(ctx context.Context, projectID string) error {

	if strings.TrimSpace(projectID) == "" {
		return ErrInvalidParam
	}

	if s.project == nil {
		return ErrProjectNotFound
	}
	exists, err := s.project.Exists(ctx, projectID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrProjectNotFound
	}
	return nil
}

func validateKind(kind, targetType string, targetID *string) error {
	if pageenums.ValidatePageContentContract(kind, targetType, targetID) {
		return nil
	}
	return ErrInvalidKind
}

func validateDraft(rawPath string, rawDoc json.RawMessage) (path string, doc json.RawMessage, err error) {
	path, err = normalizePagePath(rawPath)
	if err != nil {
		return "", nil, err
	}
	page, err := builder.ParsePage(rawDoc)
	if err != nil {
		return "", nil, ErrInvalidDocument
	}

	if _, err = builder.ValidatePageTolerant(page); err != nil {

		return "", nil, fmt.Errorf("%w: %s", ErrInvalidDocument, pageDocumentDetail(err))
	}

	doc, err = json.Marshal(page)
	if err != nil {
		return "", nil, ErrInvalidDocument
	}
	return path, doc, nil
}

func normalizePagePath(raw string) (string, error) {
	path, err := pipeline.NormalizeURL(raw)
	if err != nil {
		return "", ErrInvalidPath
	}
	return path, nil
}

func mapPersistenceError(err error) error {
	if errors.Is(err, pagemodel.ErrDraftVersionConflict) {
		return ErrDraftVersionConflict
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "duplicate key") || strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
		return ErrPathOccupied
	}

	if strings.Contains(err.Error(), pubenums.ErrRouteOccupied) {
		return ErrPathOccupied
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrPageNotFound
	}
	return err
}

func pageResp(page *pagemodel.PageEntity) *pagedto.PageResp {
	themeID := ""
	if page.ThemeID != nil {
		themeID = *page.ThemeID
	}
	return &pagedto.PageResp{
		ID: page.ID, ProjectID: page.ProjectID, ThemeID: themeID, Kind: page.Kind, ContentTargetType: page.ContentTargetType,
		ContentTargetID: page.ContentTargetID, DraftPath: page.DraftPath, ActivePath: page.ActivePath,
		StagedArtifactID: page.StagedArtifactID, ActiveArtifactID: page.ActiveArtifactID,
		DraftDocument: page.DraftDocument, DraftVersion: page.DraftVersion, Stale: page.Stale,
		CreatedAt: utils.NewJSONTime(page.CreatedAt), UpdatedAt: utils.NewJSONTime(page.UpdatedAt),
	}
}

func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *Service) List(ctx context.Context, req *pagedto.ListReq) (res []pagedto.PageResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, ErrInvalidParam
	}
	if err = s.requireProject(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	entities, err := s.model.ListAll(ctx, req.ProjectID, req.ThemeID)
	if err != nil {
		return nil, err
	}
	lang := strings.TrimSpace(req.Lang)
	if req.ActionTargetsOnly && lang == "" {
		return nil, ErrInvalidParam
	}
	if lang == "" {
		res = make([]pagedto.PageResp, 0, len(entities))
		for i := range entities {
			res = append(res, *pageResp(&entities[i]))
		}
		return res, nil
	}

	ids := make([]string, 0, len(entities))
	for i := range entities {
		ids = append(ids, entities[i].ID)
	}
	pubs, err := s.model.ListPublicationsByPages(ctx, ids, lang)
	if err != nil {
		return nil, err
	}
	published := make(map[string]bool, len(pubs))
	publications := make(map[string][]pagedto.PagePublicationResp, len(pubs))
	for i := range pubs {
		pub := pubs[i]
		if req.ActionTargetsOnly {
			if pub.ArtifactID == nil || *pub.ArtifactID == "" {
				continue
			}
			path, pathErr := pipeline.ActivePublishedPath(s.publication, s.store, pub.ActivePath, pub.ArtifactHash)
			if pathErr != nil || path == "" {
				continue
			}
		}
		published[pub.PageID] = strings.TrimSpace(pub.ActivePath) != ""
		publications[pub.PageID] = append(publications[pub.PageID], pagedto.PagePublicationResp{
			Lang: pub.Lang, ActivePath: pub.ActivePath, ArtifactID: pub.ArtifactID,
			ArtifactHash: pub.ArtifactHash, PublishedAt: utils.NewJSONTimePtr(&pub.PublishedAt),
		})
	}
	want := pageenums.LangStatusParse(req.LangState, pageenums.LangStatusPublished)
	res = make([]pagedto.PageResp, 0, len(entities))
	for i := range entities {
		if req.ActionTargetsOnly && !published[entities[i].ID] {
			continue
		}
		state := pageenums.LangStatusOf(pageExcludesLang(&entities[i], lang), published[entities[i].ID])
		if state != want {
			continue
		}
		row := pageResp(&entities[i])
		row.LangStatus = state
		row.Publications = publications[entities[i].ID]
		res = append(res, *row)
	}
	return res, nil
}

func (s *Service) ListPageTitles(ctx context.Context, projectID string) (res []pagedto.PageTitleResp, err error) {
	projectID = strings.TrimSpace(projectID)
	if err = s.requireProject(ctx, projectID); err != nil {
		return nil, err
	}
	rows, err := s.model.ListPageTitles(ctx, projectID)
	if err != nil {
		return nil, err
	}
	res = make([]pagedto.PageTitleResp, 0, len(rows))
	for i := range rows {
		res = append(res, pagedto.PageTitleResp{
			ID:         rows[i].ID,
			Kind:       rows[i].Kind,
			DraftPath:  rows[i].DraftPath,
			ActivePath: rows[i].ActivePath,
			SEOTitle:   rows[i].SEOTitle,
		})
	}
	return res, nil
}

func (s *Service) ListDrafts(ctx context.Context) (res []pagedto.PageDraftResp, err error) {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	var entities []pagemodel.PageEntity
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		part, lerr := s.model.ListDraftDocuments(ctx, projectID)
		if lerr != nil {
			return nil, lerr
		}
		entities = append(entities, part...)
	}
	res = make([]pagedto.PageDraftResp, 0, len(entities))
	for i := range entities {
		res = append(res, pagedto.PageDraftResp{
			ID: entities[i].ID, ProjectID: entities[i].ProjectID,
			DraftPath: entities[i].DraftPath, DraftDocument: entities[i].DraftDocument,
			UpdatedAt: utils.NewJSONTime(entities[i].UpdatedAt),
		})
	}
	return res, nil
}

func (s *Service) Delete(ctx context.Context, req *pagedto.DeleteReq) (err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return ErrInvalidParam
	}

	page, err := s.locatePageInProjects(ctx, req.ID)
	if err != nil {
		return mapPersistenceError(err)
	}
	if err = s.ensureActionTargetUnused(ctx, page.ProjectID, page.ID); err != nil {
		return err
	}

	if s.routes != nil {
		paths, lerr := s.routes.ListActivePaths(ctx, &pubcontract.ListActivePathsReq{
			ProjectID: page.ProjectID, PageID: req.ID,
		})
		if lerr != nil {
			return lerr
		}
		paths = normalizeOldPaths(paths)
		if len(paths) > 0 {
			intent, rerr := s.registerDeactivationIntent(ctx, page.ProjectID, req.ID, page.DraftPath, paths)
			if rerr != nil {

				return rerr
			}
			if derr := s.deleteWithDeactivation(ctx, page, intent); derr != nil {
				return derr
			}
			return nil
		}
	}

	if s.media != nil {
		if _, rerr := s.media.SyncReferences(ctx, &mediacontract.SyncRefsInput{
			RefKind:  "page",
			RefID:    req.ID,
			RefTitle: page.DraftPath,
		}); rerr != nil {
			return rerr
		}
	}

	if err = s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		if s.routes != nil {
			if rerr := s.routes.DeleteRoutesByPageTx(ctx, tx, &pubcontract.DeleteRoutesReq{
				ProjectID: page.ProjectID, PageID: req.ID,
			}); rerr != nil {
				return rerr
			}
		}
		return s.model.SoftDeleteTx(ctx, tx, page.ProjectID, req.ID, time.Now().UTC())
	}); err != nil {
		return mapPersistenceError(err)
	}
	return nil
}

func (s *Service) deactivatePathsOf(projectID, pageID string, paths []string) error {
	if s == nil || s.publication == nil {
		return nil
	}
	cleaned := make([]string, 0, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		cleaned = append(cleaned, p)
	}
	if len(cleaned) == 0 {
		return nil
	}

	return s.withPaths(cleaned, func() error {
		if s.routes != nil && strings.TrimSpace(pageID) != "" {

			owned, lerr := s.routes.ListActivePaths(context.WithoutCancel(context.Background()),
				&pubcontract.ListActivePathsReq{ProjectID: projectID, PageID: pageID})
			if lerr != nil {
				return lerr
			}
			for _, p := range cleaned {
				if !containsPath(owned, p) {

					return fmt.Errorf("%w: %s", errOldPathTaken, p)
				}
			}
		}
		return s.deactivatePathsUnlocked(cleaned)
	})
}

func (s *Service) deactivatePathsUnlocked(paths []string) error {
	if s == nil || s.publication == nil {
		return nil
	}
	for _, p := range paths {
		if p == "" {
			continue
		}
		if derr := s.publication.Deactivate(p); derr != nil {
			return fmt.Errorf("解除访问面激活失败 %s: %w", p, derr)
		}
	}
	return nil
}

func pageDocumentDetail(err error) string {
	if err == nil {
		return i18n.ErrorDetail(pageenums.DetailDocStructureInvalid)
	}
	var depthErr *builder.NodeDepthError
	if errors.As(err, &depthErr) {
		return i18n.ErrorDetail(pageenums.DetailDocNodeDepthExceed,
			"index", strconv.Itoa(depthErr.Index),
			"depth", strconv.Itoa(depthErr.Depth),
			"max", strconv.Itoa(depthErr.Max))
	}
	var nodeErr *builder.NodeInvalidError
	if errors.As(err, &nodeErr) {
		return i18n.ErrorDetail(pageenums.DetailDocNodeInvalid, "index", strconv.Itoa(nodeErr.Index))
	}
	var setErr *builder.PageSettingsError
	if errors.As(err, &setErr) {
		return i18n.ErrorDetail(pageenums.DetailDocSettingsInvalid)
	}
	if errors.Is(err, builder.ErrPageDocumentEmpty) {
		return i18n.ErrorDetail(pageenums.DetailDocEmpty)
	}
	return i18n.ErrorDetail(pageenums.DetailDocStructureInvalid)
}

func (s *Service) KindsOfPaths(ctx context.Context, projectID string, paths []string) (kinds map[string]string, err error) {
	projectID = strings.TrimSpace(projectID)
	if err = s.requireProject(ctx, projectID); err != nil {
		return nil, err
	}
	return s.model.KindsOfPaths(ctx, projectID, paths)
}

var errCompileFailed = errors.New("页面编译失败")

func previewValidationProblem(page *builder.Page) error {
	if page == nil {
		return nil
	}
	if _, err := builder.ValidatePageTolerant(page); err != nil {
		return err
	}
	return nil
}

func (s *Service) assembleCompile(ctx context.Context, in pipeline.BuildInput) ([]byte, error) {
	page, err := builder.ParsePage(in.DocJSON)
	if err != nil {
		logger.Scene("build").With("err", err).Warn("页面文档解析失败，回退默认编译")
		return pipeline.DefaultCompile(ctx, in)
	}

	projectID, currentPath := s.pageContextOfWithDefault(ctx, in.PageID, in.Lang, planDefaultLang(in.Plan))

	html, err := s.compileDocument(ctx, page, projectID, currentPath, in.Lang, in.Usage, true,
		builder.CompileModePublish, in.Diagnostics, in.Plan,
		s.buildActionOptions(ctx, projectID, in.Lang, in.PageID, in.Path))
	if err != nil {
		if errors.Is(err, errCompileFailed) {
			logger.Scene("build").Error(err, "页面编译失败")
		}
		return nil, err
	}
	s.syncMediaRefs(ctx, in.PageID, currentPath, html)
	return html, nil
}

func (s *Service) syncMediaRefs(ctx context.Context, pageID, pagePath string, html []byte) {
	if s.media == nil || strings.TrimSpace(pageID) == "" || len(html) == 0 {
		return
	}
	if _, err := s.media.SyncReferencesFromHTML(ctx, "page", pageID, pagePath, string(html)); err != nil {
		logger.Scene("build").With("page_id", pageID).Warn("媒体引用缓存同步失败（已降级，不阻断构建）")
	}
}

func (s *Service) compileDocument(ctx context.Context, page *builder.Page, projectID, currentPath, lang string, usage core.UsageRecorder, withPublishScope bool, mode builder.CompileMode, diags *builder.DegradeCollector, plan *pipeline.PublicationPlan, extra ...builder.CompileOption) ([]byte, error) {

	asm := pipeline.LoadPluginAssembly(ctx, s.plugins)
	set, pluginOpts, err := pipeline.ComponentSetWithPlugins(asm)
	if err != nil {
		return nil, err
	}
	var resolver core.BlockResolver = newBlockResolverAdapter(s, ctx, projectID)
	structurePort := s.structureTemplates
	if mode == builder.CompileModePublish {
		frozen := pipeline.ReferenceSnapshotFromContext(ctx, projectID)
		if plan != nil && plan.References != nil {
			frozen = plan.References.Clone()
		}
		if frozen == nil {
			raw, marshalErr := json.Marshal(page)
			if marshalErr != nil {
				return nil, marshalErr
			}
			frozen, err = s.freezeReferences(ctx, projectID, raw)
			if err != nil {
				return nil, err
			}
		}
		if frozen.ProjectID != projectID {
			return nil, fmt.Errorf("冻结区块工程不匹配")
		}
		resolver = frozen
		structurePort = frozen
	}

	opts := []builder.CompileOption{
		builder.WithContext(ctx), builder.WithBlockResolver(resolver), builder.WithComponentSet(set),
		builder.WithCompileMode(mode),
		s.publishedActionOptions(ctx, projectID, lang),
	}
	if diags != nil {
		opts = append(opts, builder.WithDegradeCollector(diags))
	}
	opts = append(opts, pluginOpts...)
	opts = append(opts, pipeline.LocaleCompileOptions(lang)...)
	opts = append(opts, pipeline.ClientAssetOptions()...)
	if s.content != nil {
		opts = append(opts, builder.WithCollectionResolver(s.content))
	}

	if s.productDS != nil {
		opts = append(opts, builder.WithProductDataSource(s.productDS))
	}

	if s.checkoutCountries != nil {
		if countries := s.checkoutCountries(ctx, lang); len(countries) > 0 {
			opts = append(opts, builder.WithCheckoutCountries(countries))
		}
	}

	var mediaProbe func(context.Context, string) []mediacontract.VariantRef
	if s.media != nil {
		mediaProbe = s.media.ProbeImageVariants
	}
	siteOpts, serr := pipeline.SiteCompileOptions(pipeline.SiteCompilePorts{
		Project: s.project, Navigation: s.navigation, MediaProbe: mediaProbe,

		RoutePublished: publishScope(withPublishScope, func(accessPath string) bool {
			state, ierr := s.publication.Inspect(accessPath)
			return ierr == nil && state != nil && state.Kind != "none"
		}),
	}, pipeline.SiteCompileParams{
		Ctx: ctx, ProjectID: projectID, Lang: lang, LogicalPath: currentPath,
		CurrentPath: pipeline.HighlightPathWithDefault(ctx, s.project, projectID, compileDefaultLang(ctx, s.project, projectID, plan), lang, currentPath),
		Plan:        plan,
	})
	if serr != nil {
		return nil, fmt.Errorf("%w: %v", errCompileFailed, serr)
	}
	opts = append(opts, siteOpts...)

	if page.Settings.Theme != nil {
		opts = append(opts, builder.WithThemeSettings(page.Settings.Theme))
	}

	slotRes, slotErr := pipeline.BuildStructureSlots(ctx, pipeline.StructureSlotInput{
		Port: structurePort, ProjectID: projectID, Structure: page.Settings.Structure,
		Inner: resolver, Mode: mode, Diagnostics: diags,
	})
	if slotErr != nil {

		return nil, fmt.Errorf("%w: %v", errCompileFailed, slotErr)
	}
	slotList, slotResolver := slotRes.Slots, slotRes.Resolver

	var contentTranslator *i18n.ContentTranslator
	var contentCandidates int
	opts, contentTranslator, contentCandidates = pipeline.AppendContentTranslationFor(
		opts, ctx, projectID, compileDefaultLang(ctx, s.project, projectID, plan), lang,
		page, slotResolver.ResolveBlockRoot, s.newContentTranslator)
	opts = append(opts, pipeline.AnalyticsCompileOptions(ctx, s.project, projectID)...)
	if len(slotList) > 0 {

		opts = append(opts, builder.WithBlockResolver(slotResolver))
		opts = append(opts, builder.WithStructureSlots(slotList...))
	}

	if usage != nil {
		opts = append(opts, builder.WithUsageRecorder(usage))
	}

	opts = append(opts, extra...)
	compiled, err := builder.Compile(page, opts...)
	if err != nil {

		if verr := previewValidationProblem(page); verr != nil {
			return nil, fmt.Errorf("%w: %w", errCompileFailed,
				pagecontract.NewPreviewProblem(verr.Error(), errCompileFailed))
		}
		return nil, fmt.Errorf("%w: %w", errCompileFailed, err)
	}

	if contentTranslator != nil {
		pipeline.LogContentTranslationMisses(lang, contentCandidates, contentTranslator.Misses())
	}
	doc, err := builder.RenderDocument(compiled)
	if err != nil {
		return nil, err
	}
	return []byte(doc), nil
}

type blockResolverAdapter struct {
	s *Service

	projectID string
	ctx       context.Context
	cache     map[string]*builder.Page
	errs      map[string]error
}

func newBlockResolverAdapter(s *Service, ctx context.Context, projectID string) *blockResolverAdapter {
	return &blockResolverAdapter{
		s: s, ctx: ctx, projectID: projectID,
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
	if a.cache != nil {
		if page, ok := a.cache[blockID]; ok {
			return page, nil
		}
	}
	if a.errs != nil {
		if err, ok := a.errs[blockID]; ok {
			return nil, err
		}
	}
	fail := func(err error) (*builder.Page, error) {
		if a.errs != nil {
			a.errs[blockID] = err
		}
		return nil, err
	}
	if a.s == nil || a.s.blocks == nil {
		return fail(fmt.Errorf("全局块 %s 不可用", blockID))
	}
	block, err := a.s.blocks.Detail(a.ctx, &blockcontract.DetailReq{ProjectID: a.projectID, ID: blockID})
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
	if a.cache != nil {
		a.cache[blockID] = page
	}
	return page, nil
}

func planDefaultLang(plan *pipeline.PublicationPlan) string {
	if plan == nil {
		return ""
	}
	return strings.TrimSpace(plan.DefaultLang)
}

func compileDefaultLang(ctx context.Context, project projectcontract.ProjectService, projectID string, plan *pipeline.PublicationPlan) string {
	if plan != nil && strings.TrimSpace(plan.DefaultLang) != "" {
		return strings.TrimSpace(plan.DefaultLang)
	}
	return pipeline.DefaultLocale(ctx, project, projectID)
}

func publishScope(enabled bool, fn func(string) bool) func(string) bool {
	if !enabled {
		return nil
	}
	return fn
}

func (s *Service) CompilePreview(ctx context.Context, docJSON []byte, projectID, currentPath, lang string, canvasFrames bool) (html []byte, err error) {
	var page *builder.Page
	if err = json.Unmarshal(docJSON, &page); err != nil || page == nil {
		if err == nil {
			err = errors.New("草稿文档为空")
		}
		logger.Scene("build").With("err", err).Warn("预览文档解析失败")
		return nil, fmt.Errorf("%w: %v", pagecontract.ErrPreviewInvalidDocument, err)
	}

	var extra []builder.CompileOption
	if canvasFrames {
		extra = append(extra, builder.WithCanvasSlotFrames())
	}
	html, err = s.compileDocument(ctx, page, projectID, currentPath, buildLang(lang), nil, false,
		builder.CompileModePreview, nil, nil, extra...)
	if err != nil {
		if errors.Is(err, errCompileFailed) {
			logger.Scene("build").Error(err, "预览编译失败")

			return nil, fmt.Errorf("%w: %w", pagecontract.ErrPreviewCompileFailed, err)
		}
		logger.Scene("build").Error(err, "预览文档渲染失败")
		return nil, err
	}
	return html, nil
}

func (s *Service) deleteWithDeactivation(ctx context.Context, page *pagemodel.PageEntity, in *deactivationIntent) error {

	if s.media != nil {
		if _, rerr := s.media.SyncReferences(ctx, &mediacontract.SyncRefsInput{
			RefKind:  "page",
			RefID:    in.PageID,
			RefTitle: page.DraftPath,
		}); rerr != nil {
			return rerr
		}
	}

	callbackCompleted := false
	txErr := s.model.TransactionScoped(ctx, in.ProjectID, func(tx *gorm.DB) error {

		if aerr := s.model.LockPageEntityAdvisoryTx(ctx, tx, in.PageID); aerr != nil {
			return aerr
		}

		if _, lerr := s.model.LockPageForActivationTx(ctx, tx, in.ProjectID, in.PageID); lerr != nil {
			return lerr
		}

		current, cerr := s.routes.ListActivePaths(ctx, &pubcontract.ListActivePathsReq{
			ProjectID: in.ProjectID, PageID: in.PageID,
		})
		if cerr != nil {
			return cerr
		}
		if uerr := deactivationPathsUnchanged(in.Paths, current); uerr != nil {
			return uerr
		}
		if rerr := s.routes.DeleteRoutesByPageTx(ctx, tx, &pubcontract.DeleteRoutesReq{
			ProjectID: in.ProjectID, PageID: in.PageID,
		}); rerr != nil {
			return rerr
		}
		if serr := s.model.SoftDeleteTx(ctx, tx, in.ProjectID, in.PageID, time.Now().UTC()); serr != nil {
			return serr
		}
		if merr := s.markDeactivationCommitted(ctx, tx, in); merr != nil {
			return merr
		}
		callbackCompleted = true
		return nil
	})
	if txErr != nil && !callbackCompleted {

		s.abortPublishReceipt(ctx, in.ReceiptID, "删除业务事务未提交，链接保持原样")
		logger.Scene("publication").With("pageId", in.PageID).Error(txErr, "页面删除失败（业务事务未提交）")
		return mapPersistenceError(txErr)
	}
	if txErr != nil {

		s.keepPublishReceiptPending(in.ReceiptID, "删除业务事务提交结果不明")
		logger.Scene("publication").With("pageId", in.PageID).Error(txErr, "页面删除提交结果不明（回执保持 pending 交收敛重判）")
		return mapPersistenceError(txErr)
	}
	if !callbackCompleted {

		s.abortPublishReceipt(ctx, in.ReceiptID, "删除业务事务未置提交标记")
		return nil
	}
	if werr := s.withdrawDeactivatedLinks(ctx, in.ProjectID, in.PageID, in.ReceiptID); werr != nil {
		s.keepPublishReceiptPending(in.ReceiptID, "删除已提交，撤链失败")
		logger.Scene("publication").With("pageId", in.PageID).
			Error(werr, "删除已提交但撤链失败（回执保持 pending 交收敛重放）")
		return nil
	}
	if cerr := s.completePublishReceipt(ctx, in.ReceiptID); cerr != nil {
		s.NotifyPendingReceipt()
		return nil
	}
	return nil
}

func (s *Service) withdrawDeactivatedLinks(ctx context.Context, projectID, pageID, receiptID string) error {
	if s == nil || s.publication == nil || s.routes == nil {

		return fmt.Errorf("%w: 缺少访问面或回执端口，无法复核归属", errOldPathTaken)
	}
	return s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {

		if aerr := s.model.LockPageEntityAdvisoryTx(ctx, tx, pageID); aerr != nil {
			return aerr
		}

		row, lerr := s.routes.LoadDeactivationReceiptTx(ctx, tx, pageReceiptOwner(pageID), projectID, receiptID)
		if lerr != nil {
			return lerr
		}
		if row.ReceiptState != "pending" {

			return nil
		}
		if !deactivationBusinessCommittedFromData(row.ReceiptData) {

			return fmt.Errorf("%w: 下线意图的业务变更尚未提交", errOldPathTaken)
		}
		paths := normalizeOldPaths(deactivationOldPathsFromData(row.ReceiptData))
		if len(paths) == 0 {

			return fmt.Errorf("%w: 回执缺少路径事实，无法复核归属", errOldPathTaken)
		}
		rows := deactivationPathArtifactsFromData(row.ReceiptData)
		return s.withPaths(paths, func() error {
			for _, p := range paths {
				if strings.TrimSpace(p) == "" {
					continue
				}
				identity, ok := rows[p]
				if !ok || identity.Locator == "" && identity.ArtifactID == "" && identity.ReceiptID == "" {

					return fmt.Errorf("%w: 路径 %s 缺少登记时的归属身份", errOldPathTaken, p)
				}
				if oerr := s.verifyDeactivatedPathOwner(ctx, tx, projectID, pageID, p, identity); oerr != nil {
					return oerr
				}
				if derr := s.publication.Deactivate(p); derr != nil {
					return fmt.Errorf("撤掉访问路径 %s 失败: %w", p, derr)
				}
			}
			return nil
		})
	})
}

func (s *Service) verifyDeactivatedPathOwner(ctx context.Context, tx *gorm.DB, projectID, pageID, path string,
	registered pubcontract.ReceiptPathIdentity) error {
	if s.routes != nil {
		owner, oerr := s.routes.RouteOwnerTx(ctx, tx, projectID, path)
		if oerr != nil && !errors.Is(oerr, gorm.ErrRecordNotFound) {
			return oerr
		}
		if oerr == nil && owner != nil {
			if owner.ProjectID != projectID {
				return fmt.Errorf("%w: 路径 %s 已属于其它工程", errOldPathTaken, path)
			}
			if owner.PageID == nil || *owner.PageID != pageID {
				return fmt.Errorf("%w: 路径 %s 的路由占用已转给其它实体", errOldPathTaken, path)
			}
		}
	}
	if s.publication == nil {
		return fmt.Errorf("%w: 缺少访问面存储，无法复核路径 %s 的链接身份", errOldPathTaken, path)
	}
	if strings.TrimSpace(registered.Locator) == "" {

		return fmt.Errorf("%w: 路径 %s 缺少登记时的访问面定位器，无法复核归属", errOldPathTaken, path)
	}
	st, ierr := s.publication.Inspect(path)
	if ierr != nil {
		return ierr
	}
	if st == nil || st.Kind == pipeline.PublicationNone || st.Locator == nil {

		return nil
	}
	if current := strings.TrimSpace(st.Locator.Provider + ":" + st.Locator.Key); current != registered.Locator {
		return fmt.Errorf("%w: 路径 %s 的访问面链接已指向别的产物", errOldPathTaken, path)
	}
	return nil
}

func (s *Service) withdrawProjectDeactivatedLinks(ctx context.Context, projectID, receiptID string) error {
	if s == nil || s.publication == nil || s.routes == nil {
		return fmt.Errorf("%w: 缺少访问面或回执端口，无法复核归属", errOldPathTaken)
	}
	return s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		if aerr := s.model.LockProjectEntityAdvisoryTx(ctx, tx, projectID); aerr != nil {
			return aerr
		}
		row, lerr := s.routes.LoadDeactivationReceiptTx(ctx, tx, projectReceiptOwner(projectID), projectID, receiptID)
		if lerr != nil {
			return lerr
		}
		if row.ReceiptState != "pending" {
			return nil
		}
		if !deactivationBusinessCommittedFromData(row.ReceiptData) {
			return fmt.Errorf("%w: 下线意图的业务变更尚未提交", errOldPathTaken)
		}
		paths := normalizeOldPaths(deactivationOldPathsFromData(row.ReceiptData))
		if len(paths) == 0 {
			return fmt.Errorf("%w: 回执缺少路径事实，无法复核归属", errOldPathTaken)
		}
		rows := deactivationPathArtifactsFromData(row.ReceiptData)
		return s.withPaths(paths, func() error {
			for _, p := range paths {
				if strings.TrimSpace(p) == "" {
					continue
				}
				identity, ok := rows[p]
				if !ok || identity.Locator == "" && identity.ArtifactID == "" && identity.ReceiptID == "" {
					return fmt.Errorf("%w: 路径 %s 缺少登记时的归属身份", errOldPathTaken, p)
				}
				if oerr := s.verifyProjectRedirectOwner(ctx, tx, projectID, p, identity); oerr != nil {
					return oerr
				}
				if derr := s.publication.Deactivate(p); derr != nil {
					return fmt.Errorf("撤掉访问路径 %s 失败: %w", p, derr)
				}
			}
			return nil
		})
	})
}

const publisherRouteKindRedirect = pagemodel.RouteKindRedirect

const publisherRouteKindReserved = "reserved"

func (s *Service) verifyProjectRedirectOwner(ctx context.Context, tx *gorm.DB, projectID, path string,
	registered pubcontract.ReceiptPathIdentity) error {
	if s.routes != nil {
		owner, oerr := s.routes.RouteOwnerTx(ctx, tx, projectID, path)
		if oerr != nil && !errors.Is(oerr, gorm.ErrRecordNotFound) {
			return oerr
		}
		if oerr == nil && owner != nil {
			if strings.TrimSpace(owner.ProjectID) != "" && owner.ProjectID != projectID {
				return fmt.Errorf("%w: 路径 %s 已属于其它工程", errOldPathTaken, path)
			}
			if owner.RouteKind != "" && owner.RouteKind != publisherRouteKindRedirect {
				return fmt.Errorf("%w: 路径 %s 已被其它实体作为 %s 占用", errOldPathTaken, path, owner.RouteKind)
			}
		}
	}
	if s.publication == nil {
		return fmt.Errorf("%w: 缺少访问面存储，无法复核路径 %s 的链接身份", errOldPathTaken, path)
	}
	if strings.TrimSpace(registered.Locator) == "" {
		return fmt.Errorf("%w: 路径 %s 缺少登记时的访问面定位器，无法复核归属", errOldPathTaken, path)
	}
	st, ierr := s.publication.Inspect(path)
	if ierr != nil {
		return ierr
	}
	if st == nil || st.Kind == pipeline.PublicationNone || st.Locator == nil {
		return nil
	}
	if st.Kind != pipeline.PublicationRedirect {
		return fmt.Errorf("%w: 路径 %s 的访问面已被改成 %s", errOldPathTaken, path, st.Kind)
	}

	if current := strings.TrimSpace(st.Locator.Provider + ":" + st.Locator.Key); current != registered.Locator {
		return fmt.Errorf("%w: 路径 %s 的访问面链接已指向别的产物", errOldPathTaken, path)
	}
	return nil
}

func deactivationPathArtifactsFromData(raw []byte) map[string]pubcontract.ReceiptPathIdentity {
	if len(raw) == 0 {
		return nil
	}
	var probe struct {
		PathArtifacts map[string]pubcontract.ReceiptPathIdentity `json:"pathArtifacts"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	return probe.PathArtifacts
}

func DeactivationBusinessCommittedFromData(raw []byte) bool {
	return deactivationBusinessCommittedFromData(raw)
}

func DeactivationPathArtifactsFromData(raw []byte) map[string]pubcontract.ReceiptPathIdentity {
	return deactivationPathArtifactsFromData(raw)
}

func deactivationBusinessCommittedFromData(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var probe struct {
		BusinessCommitted bool `json:"businessCommitted"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.BusinessCommitted
}

func deactivationOldPathsFromData(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var probe struct {
		OldPaths []string `json:"oldPaths"`
		OldPath  string   `json:"oldPath"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	if len(probe.OldPaths) == 0 && strings.TrimSpace(probe.OldPath) != "" {
		return []string{probe.OldPath}
	}
	return probe.OldPaths
}
