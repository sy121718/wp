package presentationservice

import (
	"context"
	"errors"
	"fmt"
	json "go_wp/pkg/jsonx"
	publish "go_wp/pkg/publish"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationenums "go_wp/internal/module/presentation/enums"
	presentationmodel "go_wp/internal/module/presentation/model"
	productcontract "go_wp/internal/module/product/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/builder"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	seoutil "go_wp/pkg/seo"
	"go_wp/pkg/upload"
)

func (s *Service) resolveTemplate(ctx context.Context, projectID, entityType, templateID string) (tpl *contenttemplatecontract.ResolvedTemplate, err error) {
	if id := strings.TrimSpace(templateID); id != "" {

		if s.templateTypeMismatch(ctx, projectID, id, entityType) {
			return nil, presentationenums.Err(presentationenums.ErrTemplateTypeMismatch)
		}

		tpl, err = s.templates.ResolveTemplateByIDScoped(ctx, projectID, id)
		if err != nil {

			return nil, err
		}
		return tpl, nil
	}
	tpl, err = s.templates.ResolveTemplateScoped(ctx, projectID, entityType)
	if err != nil {

		return nil, err
	}
	return tpl, nil
}

func (s *Service) templateTypeMismatch(ctx context.Context, projectID, templateID, entityType string) bool {
	if s.templates == nil {
		return false
	}
	tpl, err := s.templates.GetScoped(ctx, projectID, templateID)
	if err != nil || tpl == nil {
		return false
	}
	return tpl.EntityType != entityType
}

func (s *Service) resolveBoundTemplate(ctx context.Context, inst *presentationmodel.InstanceEntity,
	explicitID string) (*contenttemplatecontract.ResolvedTemplate, error) {
	if strings.TrimSpace(explicitID) != "" {
		return s.resolveTemplate(ctx, inst.ProjectID, inst.EntityType, explicitID)
	}
	if id := strings.TrimSpace(inst.TemplateID); id != "" {
		tpl, err := s.templates.ResolveTemplateByIDScoped(ctx, inst.ProjectID, id)
		if err == nil && tpl.EntityType == inst.EntityType {
			return tpl, nil
		}
		logger.Scene("build").With("presentation_id", inst.ID).With("template_id", id).
			Warn("实例绑定的模板不可用，本次重建回落到该类型的默认模板")
	}
	return s.resolveTemplate(ctx, inst.ProjectID, inst.EntityType, "")
}

func withInstanceDocument(inst *presentationmodel.InstanceEntity,
	tpl *contenttemplatecontract.ResolvedTemplate) *contenttemplatecontract.ResolvedTemplate {
	if inst != nil && len(inst.OverrideDocument) > 0 {
		override := *tpl
		override.Document = inst.OverrideDocument
		return &override
	}
	return tpl
}

func (s *Service) buildArtifact(ctx context.Context, entityType, entityID, urlPath, projectID, lang string,
	targetLangs []string, tpl *contenttemplatecontract.ResolvedTemplate) (built builtArtifact, err error) {

	references, err := s.freezeReferences(ctx, projectID, tpl.Document)
	if err != nil {
		return built, err
	}
	ctx = pipeline.WithReferenceSnapshot(ctx, references)
	usage := &pipeline.CompileUsage{}

	diags := builder.NewDegradeCollector()

	html, err := s.renderHTML(ctx, entityType, entityID, urlPath, projectID, lang, targetLangs, tpl, usage,
		builder.CompileModePublish, diags)
	if err != nil {
		return built, err
	}

	var slotDeps []publish.Dependency

	var collectionSources []string
	if pageDoc, perr := builder.ParsePage(tpl.Document); perr == nil {
		slotDeps = pipeline.StructureSlotDependencies(ctx, references, projectID, pageDoc.Settings.Structure)
		collectionSources = core.CollectionSourcesOf(pageDoc.Root)
	}
	sourceHash := publish.SHA256(tpl.Document)

	deps := presentationDependencies(entityType, entityID, tpl.TemplateID, projectID, collectionSources, usage)
	deps = append(deps, publish.I18NDependency(i18n.Revision()))
	if usage != nil && usage.ContentTranslation {
		deps = append(deps, publish.I18NContentDependency(i18n.ContentRevisionForProject(ctx, projectID)))
	}
	deps = append(deps, slotDeps...)
	artifact, err := publish.NewArtifact(html, &publish.Manifest{
		ManifestSchemaVersion:     1,
		PageDocumentSchemaVersion: 1,
		SourceID:                  entityID,
		SourceType:                publish.SourceTypePresentation,
		CanonicalPath:             urlPath,
		SourceHash:                sourceHash,
		References:                references.Clone().Unwrap(),
		BuildInputHash:            sourceHash,
		Lang:                      strings.TrimSpace(lang),
		Dependencies:              deps,
		Diagnostics:               diagnosticsOf(diags.Items()),
	})
	if err != nil {
		return built, err
	}
	loc, err := s.store.PutArtifact(artifact)
	if err != nil {
		return built, err
	}
	return builtArtifact{Hash: artifact.Hash, Loc: loc, Manifest: artifact.Manifest}, nil
}

func (s *Service) renderHTML(ctx context.Context, entityType, entityID, urlPath, projectID, lang string,
	targetLangs []string, tpl *contenttemplatecontract.ResolvedTemplate, usage *pipeline.CompileUsage,
	mode builder.CompileMode, diags *builder.DegradeCollector) (html []byte, err error) {
	page, err := builder.ParsePage(tpl.Document)
	if err != nil {
		return nil, err
	}
	if s.registry == nil {
		return nil, presentationenums.Err(presentationenums.ErrRegistryMissing)
	}

	if err = builder.ValidateFieldRefs(page, entityType, s.registry); err != nil {
		return nil, err
	}

	if strings.TrimSpace(lang) == "" {
		lang = s.resolveLang(ctx, projectID)
	}

	buildCtx := core.WithBuildProjectID(core.WithBuildLang(ctx, lang), projectID)
	logicalPath := pipeline.LogicalPathOf(ctx, s.project, projectID, urlPath)
	highlightPath := pipeline.HighlightPath(ctx, s.project, projectID, lang, logicalPath)
	if urlPath == "" {
		highlightPath = ""
	}
	resolver, err := s.registry.ResolverFor(buildCtx, entityType, entityID)
	if err != nil {
		return nil, err
	}

	if err = applyEntitySEO(page, entityType, urlPath, s.registry.FieldWhitelist(entityType), resolver); err != nil {
		return nil, err
	}
	asm := pipeline.LoadPluginAssembly(ctx, s.plugins)
	set, pluginOpts, err := pipeline.ComponentSetWithPlugins(asm)
	if err != nil {
		return nil, err
	}

	var blockAdapter core.BlockResolver = newBlockResolverAdapter(s.blocks, buildCtx, projectID)
	var structurePort pipeline.StructureTemplatePort = s
	if mode == builder.CompileModePublish {
		frozen := pipeline.ReferenceSnapshotFromContext(ctx, projectID)
		if frozen == nil {
			frozen, err = s.freezeReferences(ctx, projectID, tpl.Document)
			if err != nil {
				return nil, err
			}
		}
		blockAdapter = frozen
		structurePort = frozen
	}

	archiveOpts := []builder.CompileOption{}
	if entityType == "product" || (tpl != nil && strings.TrimSpace(tpl.TemplateRole) == contenttemplatecontract.TemplateRoleArchive) {
		archiveOpts = append(archiveOpts, builder.WithArchiveEntity(entityType, entityID))
	}
	compileOpts := []builder.CompileOption{
		builder.WithContext(buildCtx),
		builder.WithComponentSet(set),
		builder.WithContentResolver(resolver),
		builder.WithBlockResolver(blockAdapter),

		builder.WithCompileMode(mode),
		s.publishedActionOptions(ctx, projectID, lang),
	}
	if diags != nil {
		compileOpts = append(compileOpts, builder.WithDegradeCollector(diags))
	}
	compileOpts = append(compileOpts, pluginOpts...)
	compileOpts = append(compileOpts, archiveOpts...)
	compileOpts = append(compileOpts, pipeline.LocaleCompileOptions(lang)...)

	siteOpts, serr := pipeline.SiteCompileOptions(pipeline.SiteCompilePorts{
		Project: s.project, Navigation: s.navigation, MediaProbe: s.mediaProbe,
		RoutePublished: func(accessPath string) bool {
			state, ierr := s.publication.Inspect(accessPath)
			return ierr == nil && state != nil && state.Kind != "none"
		},
	}, pipeline.SiteCompileParams{
		Ctx: ctx, ProjectID: projectID, Lang: lang,
		LogicalPath: logicalPath, CurrentPath: highlightPath,
		TargetLangs: targetLangs,
	})
	if serr != nil {
		return nil, serr
	}
	compileOpts = append(compileOpts, siteOpts...)
	if page.Settings.Theme != nil {
		compileOpts = append(compileOpts, builder.WithThemeSettings(page.Settings.Theme))
	}
	compileOpts = append(compileOpts, pipeline.ClientAssetOptions()...)

	if usage != nil {
		compileOpts = append(compileOpts, builder.WithUsageRecorder(pipeline.NewActionUsage(usage)))
	}

	slotRes, slotErr := pipeline.BuildStructureSlots(ctx, pipeline.StructureSlotInput{
		Port: structurePort, ProjectID: projectID, Structure: page.Settings.Structure,
		Inner: blockAdapter, Mode: mode, Diagnostics: diags,
	})
	if slotErr != nil {

		return nil, slotErr
	}
	slotList, slotResolver := slotRes.Slots, slotRes.Resolver
	var contentTranslator *i18n.ContentTranslator
	var contentCandidates int
	compileOpts, contentTranslator, contentCandidates = pipeline.AppendContentTranslation(
		compileOpts, ctx, s.project, projectID, lang, page, slotResolver.ResolveBlockRoot, s.newContentTranslator)

	if s.collection != nil {
		compileOpts = append(compileOpts, builder.WithCollectionResolver(s.collection))
	}

	if s.productDS != nil {
		compileOpts = append(compileOpts, builder.WithProductDataSource(s.productDS))
	}

	if s.checkoutCountries != nil {
		if countries := s.checkoutCountries(ctx, lang); len(countries) > 0 {
			compileOpts = append(compileOpts, builder.WithCheckoutCountries(countries))
		}
	}
	compileOpts = append(compileOpts, pipeline.AnalyticsCompileOptions(ctx, s.project, projectID)...)
	if len(slotList) > 0 {
		compileOpts = append(compileOpts, builder.WithBlockResolver(slotResolver))
		compileOpts = append(compileOpts, builder.WithStructureSlots(slotList...))
	}
	compiled, err := builder.Compile(page, compileOpts...)
	if err != nil {
		return nil, err
	}
	if contentTranslator != nil {
		publish.LogContentTranslationMisses(lang, contentCandidates, contentTranslator.Misses())
	}

	if usage != nil && contentCandidates > 0 {
		usage.UseContentTranslation()
	}
	doc, err := builder.RenderDocument(compiled)
	if err != nil {
		return nil, err
	}
	return []byte(doc), nil
}

func (s *Service) activate(urlPath string, built builtArtifact) error {
	if err := s.withPath(urlPath, func() error {
		return s.publication.Activate(urlPath, built.Loc)
	}); err != nil {
		return fmt.Errorf("激活 %s 失败: %w", urlPath, err)
	}
	return nil
}

func presentationDependencies(entityType, entityID, templateID, projectID string,
	collectionSources []string, usage *pipeline.CompileUsage) []publish.Dependency {
	dep := publish.DirectContentKey(entityType, entityID)
	out := []publish.Dependency{{Kind: dep.Kind, Key: dep.Key}}
	out = append(out, pipeline.ActionTargetDependencies(usage)...)

	for _, src := range collectionSources {
		if src = strings.TrimSpace(src); src == "" {
			continue
		}
		out = append(out, publish.Dependency{
			Kind: publish.DepKindContentCollection,
			Key:  "collection:" + src,
		})
	}
	if strings.TrimSpace(templateID) != "" {
		out = append(out, publish.Dependency{
			Kind: publish.DepKindContentTemplate,
			Key:  "content_template:" + templateID,
		})
	}
	if pid := strings.TrimSpace(projectID); pid != "" {
		for _, kind := range usage.MenuList() {
			k := publish.MenuKey(pid, kind)
			out = append(out, publish.Dependency{Kind: k.Kind, Key: k.Key})
		}

		for _, navID := range usage.NavigationList() {
			k := publish.NavigationKey(navID)
			out = append(out, publish.Dependency{Kind: k.Kind, Key: k.Key})
		}

		for _, blockID := range usage.BlockList() {
			k := publish.BlockKey(blockID)
			out = append(out, publish.Dependency{Kind: k.Kind, Key: k.Key})
		}
	}
	return out
}

func (s *Service) resolveLang(ctx context.Context, projectID string) string {
	if s.project == nil || strings.TrimSpace(projectID) == "" {
		return ""
	}
	lang, err := s.project.DefaultLocale(ctx, projectID)
	if err != nil {
		logger.Scene("build").With("project_id", projectID).
			Warn("构建语言解析失败，本次构建按原文输出: " + err.Error())
		return ""
	}
	return strings.TrimSpace(lang)
}

func (s *Service) resolveProjectID(ctx context.Context, explicit string) (string, error) {
	if s.project == nil {
		return "", presentationenums.Err(presentationenums.ErrProjectRequired)
	}
	if id := strings.TrimSpace(explicit); id != "" {
		ok, err := s.project.Exists(ctx, id)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", presentationenums.Err(presentationenums.ErrProjectNotFound)
		}
		return id, nil
	}
	list, err := s.project.List(ctx)
	if err != nil {
		return "", err
	}
	if len(list) != 1 {
		return "", presentationenums.Err(presentationenums.ErrProjectRequired)
	}
	return list[0].ID, nil
}

func seoCanonicalPath(path string) string {
	return seoutil.CanonicalPublicPath(path)
}

const (
	seoTitleLimitBytes       = 200
	seoDescriptionLimitBytes = 500
)

const (
	entityTypeArticle = "article"
	entityTypeProduct = productcontract.EntityTypeProduct
)

var (
	seoTitleCandidates       = []string{"seoTitle", "title", "name"}
	seoDescriptionCandidates = []string{"seoDescription", "excerpt", "description"}
)

func schemaTypeOf(entityType string) string {
	switch entityType {
	case entityTypeArticle:
		return entityTypeArticle
	case entityTypeProduct:
		return entityTypeProduct
	}
	return ""
}

var entityImageCandidates = map[string][]string{
	entityTypeArticle: {"featuredImage"},
	entityTypeProduct: {"defaultImage", "images"},
}

func pickEntityImage(entityType string, writable []string, resolver core.ContentResolver) string {
	if resolver == nil {
		return ""
	}
	for _, field := range entityImageCandidates[entityType] {
		if !fieldWritable(writable, field) {
			continue
		}
		v, err := resolver.ResolveString(entityType + "." + field)
		if err != nil {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if strings.HasPrefix(v, "[") {
			var list []string
			if jerr := json.Unmarshal([]byte(v), &list); jerr == nil && len(list) > 0 {
				v = strings.TrimSpace(list[0])
			}
		}
		if v != "" {
			return v
		}
	}
	return ""
}

func applyEntitySEO(page *builder.Page, entityType, urlPath string,
	writable []string, resolver core.ContentResolver) error {
	title, err := pickSEOField(entityType, writable, resolver, seoTitleCandidates, seoPlainText)
	if err != nil {
		return err
	}
	description, err := pickSEOField(entityType, writable, resolver, seoDescriptionCandidates, seoDescriptionText)
	if err != nil {
		return err
	}

	if title != "" {
		page.Settings.SEO.Title = clampSEOField(entityType, "seoTitle", title, seoTitleLimitBytes)
	}
	if description != "" {
		page.Settings.SEO.Description = clampSEOField(entityType, "seoDescription", description, seoDescriptionLimitBytes)
	}
	if st := schemaTypeOf(entityType); st != "" {
		page.Settings.SEO.SchemaType = st
	}
	if entityType == entityTypeProduct {
		if offer := productOfferLD(entityType, writable, resolver); offer != nil {
			page.Settings.SEO.ProductOffer = offer
		}
	}

	if img := pickEntityImage(entityType, writable, resolver); img != "" {

		page.Settings.SEO.OGImage = upload.StorageURL(img)
	}

	if path := strings.TrimSpace(urlPath); path != "" {
		page.Settings.SEO.Canonical = seoCanonicalPath(path)
	}
	return nil
}

func productOfferLD(entityType string, writable []string, resolver core.ContentResolver) *builder.ProductOfferLD {
	read := func(field string) string {
		if !fieldWritable(writable, field) {
			return ""
		}
		v, err := resolver.ResolveString(entityType + "." + field)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(v)
	}
	price := read("price")
	if price == "" {
		return nil
	}
	offer := &builder.ProductOfferLD{
		SKU:   read("sku"),
		Price: price,

		PriceCurrency: i18n.GetDefaultCurrency(),
		Availability:  "InStock",
	}
	if variants := read("variants"); variants == "" || variants == "[]" {
		offer.Availability = "OutOfStock"
	}
	if rc := read("ratingCount"); rc != "" {
		if n, err := strconv.Atoi(rc); err == nil && n > 0 {
			offer.RatingCount = n
			if rv := read("rating"); rv != "" {
				if f, err := strconv.ParseFloat(rv, 64); err == nil && f > 0 {
					offer.RatingValue = f
				}
			}
		}
	}
	return offer
}

func clampSEOField(entityType, field, value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := value[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	logger.Scene("build").
		With("entityType", entityType).
		With("field", field).
		With("limitBytes", limit).
		With("actualBytes", len(value)).
		Warn("实体 SEO 字段超过页面设置上限，已按上限截断")
	return strings.TrimSpace(cut)
}

func pickSEOField(entityType string, writable []string, resolver core.ContentResolver,
	candidates []string, normalize func(string) string) (string, error) {
	for _, name := range candidates {
		if !fieldWritable(writable, name) {
			continue
		}
		raw, err := resolver.ResolveString(entityType + "." + name)
		if err != nil {

			return "", fmt.Errorf("读取实体 SEO 字段 %s.%s 失败: %w", entityType, name, err)
		}
		if v := normalize(raw); v != "" {
			return v, nil
		}
	}
	return "", nil
}

func fieldWritable(writable []string, name string) bool {
	for _, f := range writable {
		if f == name {
			return true
		}
	}
	return false
}

var blockBoundaryRe = regexp.MustCompile(`(?i)</(p|div|li|h[1-6]|tr|td|th|blockquote|summary|details|thead|tbody|tfoot|caption|dt|dd)>|<br\s*/?>`)

func seoPlainText(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || !core.HasRichMarkup(v) {
		return v
	}
	v = blockBoundaryRe.ReplaceAllString(v, " ")
	return strings.TrimSpace(core.StripRichTags(v))
}

func seoDescriptionText(v string) string {
	return strings.Join(strings.Fields(seoPlainText(v)), " ")
}

type langBuildResult struct {
	lang       string
	accessPath string
	built      builtArtifact
	artifactID string
}

func (s *Service) instanceLogicalPath(ctx context.Context, inst *presentationmodel.InstanceEntity) string {
	return pipeline.LogicalPathOf(ctx, s.project, inst.ProjectID, inst.URLPath)
}

func (s *Service) normalizeLogicalPath(ctx context.Context, projectID, raw string) (string, error) {
	normalized, err := publish.NormalizeURL(raw)
	if err != nil {
		return "", err
	}
	return pipeline.LogicalPathOf(ctx, s.project, projectID, normalized), nil
}

func (s *Service) ensureLogicalPathFree(ctx context.Context, projectID, logicalPath, excludeInstanceID string) error {
	entries, err := pipeline.SiteRouteEntries(ctx, s.project, projectID, logicalPath)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := s.ensurePathFree(ctx, projectID, e.Path, excludeInstanceID); err != nil {
			return err
		}
	}

	if _, err := s.m.FindInstanceByPath(ctx, projectID, logicalPath, excludeInstanceID); err == nil {
		logger.Scene("build").With("url", logicalPath).Warn("详情页逻辑路径预检被拒绝：已被其他展示实例占用")
		return presentationenums.Err(presentationenums.ErrPathOccupied)
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

func (s *Service) publishLangsOf(ctx context.Context, projectID string) ([]string, error) {
	return pipeline.ResolveSiteLangs(ctx, s.project, projectID, pipeline.LangFallbackForbidden)
}

func (s *Service) publishAllLangs(ctx context.Context, inst *presentationmodel.InstanceEntity,
	intent publishIntent) (primaryArtifactID string, err error) {

	metrics := publishSessionMetrics{startedAt: time.Now()}

	var frozenLangs []string

	defer func() {
		s.logPublishSession(inst, metrics, err)
		s.recordPublishAttempt(ctx, inst, intent, frozenLangs, metrics.attempts, primaryArtifactID, err, metrics.total())
	}()

	var moved error
	for attempt := 1; attempt <= publishAttempts; attempt++ {
		metrics.attempts = attempt
		frozen, freezeTiming, ferr := s.freezePublish(ctx, inst, intent)
		metrics.lockWaitFreeze += freezeTiming.lockWait
		metrics.freeze += freezeTiming.work
		if ferr != nil {
			return "", ferr
		}
		metrics.langs = len(frozen.langs)
		frozenLangs = frozen.langs

		results, compileWork, cerr := s.compileLangs(ctx, inst, frozen)
		metrics.compile += compileWork
		if cerr != nil {
			return "", cerr
		}

		var commitTiming phaseTiming
		primaryArtifactID, commitTiming, moved = s.commitPublish(ctx, inst, frozen, results, intent.oldPaths)
		metrics.lockWaitCommit += commitTiming.lockWait
		metrics.commit += commitTiming.work
		if moved == nil {
			return primaryArtifactID, nil
		}

		if !errors.Is(moved, errPublishStateMoved) && !errors.Is(moved, errInputVersionMoved) {
			return "", moved
		}

		logger.Scene("build").With("instanceId", inst.ID).With("attempt", attempt).
			With("reason", moved.Error()).
			Warn("发布会话的实例状态或编译输入已被推进，丢弃本次编译结果并重新冻结: " + moved.Error())
	}
	return "", fmt.Errorf("发布会话连续 %d 次被并发的发布批次推进实例状态，本次中止: %w", publishAttempts, moved)
}

func (s *Service) finalizeMultiLangBatch(ctx context.Context, inst *presentationmodel.InstanceEntity,
	snapID, primaryArtifactID string, now time.Time) error {
	if strings.TrimSpace(primaryArtifactID) == "" || strings.TrimSpace(snapID) == "" {
		return errors.New("多语言发布缺少默认语言产物或快照，无法推进实例指针")
	}
	inst.CurrentSnapshotID = &snapID
	inst.StagedSnapshotID = &snapID
	inst.StagedArtifactID = &primaryArtifactID
	inst.ActiveArtifactID = &primaryArtifactID
	inst.Stale = false
	inst.PublishedAt = &now
	inst.UpdatedAt = now
	return s.m.UpdateInstancePointers(ctx, inst)
}

func (s *Service) markBatchUnconverged(ctx context.Context, inst *presentationmodel.InstanceEntity, cause error) {
	inst.Stale = true
	if _, err := s.m.MarkStale(ctx, inst.ProjectID, []string{inst.ID}, time.Now().UTC()); err != nil {
		logger.Scene("build").With("instanceId", inst.ID).Error(err, "标记实例待重建失败")
	}
	logger.Scene("build").With("instanceId", inst.ID).
		Error(cause, "多语言发布批次未收敛（实例已标记待重建）")
}

func (s *Service) publishedArtifactsByLang(ctx context.Context, instanceID string) map[string]string {
	out := map[string]string{}
	pubs, err := s.m.ListPublications(ctx, instanceID)
	if err != nil {
		return out
	}
	for _, pub := range pubs {
		if pub.ArtifactID != nil {
			out[pub.Lang] = *pub.ArtifactID
		}
	}
	return out
}

func (s *Service) publishedReceiptIDsByLang(ctx context.Context, instanceID string,
	results []langBuildResult) map[string]string {
	out := make(map[string]string, len(results))
	for i := range results {
		lang := strings.TrimSpace(results[i].lang)
		if lang == "" {
			continue
		}
		id, err := s.m.PublishedReceiptID(ctx, instanceID, lang)
		if err != nil {

			logger.Scene("publication").With("instanceId", instanceID).With("lang", lang).
				Warn("读取语言账本成功回执身份失败，本次按未知上送")
			continue
		}
		out[lang] = strings.TrimSpace(id)
	}
	return out
}

func (s *Service) persistMultiLangArtifactsTx(ctx context.Context, tx *gorm.DB, inst *presentationmodel.InstanceEntity,
	tpl *contenttemplatecontract.ResolvedTemplate, results []langBuildResult, now time.Time,
	logicalPath, defaultLang string, mode *instanceModePending) (primaryArtifactID, snapID string, err error) {
	snapID = uuid.NewString()
	snap := &presentationmodel.SnapshotEntity{
		ID: snapID, PresentationInstanceID: inst.ID,
		SourceTemplateVersionID: tpl.VersionID, SourceEntityRevisionID: inst.EntityID,
		Document: tpl.Document, CreatedAt: now,
	}

	if cerr := s.m.CreateSnapshotTx(tx, snap); cerr != nil {
		return "", "", cerr
	}

	if mode != nil {
		if uerr := s.m.UpdateInstanceModeTx(tx, inst.ProjectID, inst.ID, mode.renderMode, mode.document, now); uerr != nil {
			return "", "", uerr
		}
		inst.RenderMode = mode.renderMode
		if mode.renderMode == presentationmodel.RenderModeDocument {
			inst.OverrideDocument = mode.document
		} else {
			inst.OverrideDocument = nil
		}
	}
	if inst.TemplateID != tpl.TemplateID {

		if uerr := s.m.RecordBatchTemplateTx(tx, inst.ProjectID, inst.ID, tpl.TemplateID, now); uerr != nil {
			return "", "", uerr
		}
		inst.TemplateID = tpl.TemplateID

		if mode == nil && (presentationmodel.IsDocumentMode(inst.RenderMode) || len(inst.OverrideDocument) > 0) {
			if cerr := s.m.ClearInstanceModeTx(tx, inst.ProjectID, inst.ID, now); cerr != nil {
				return "", "", cerr
			}
			inst.RenderMode = presentationmodel.RenderModeTemplate
			inst.OverrideDocument = nil
		}
	}
	if logicalPath != "" && inst.URLPath != logicalPath {
		if uerr := s.m.RecordBatchURLTx(tx, inst.ProjectID, inst.ID, logicalPath, now); uerr != nil {
			return "", "", uerr
		}
		inst.URLPath = logicalPath
	}
	version, verr := s.m.NextArtifactVersionTx(tx, inst.ID)
	if verr != nil {
		return "", "", verr
	}
	for i := range results {
		b := &results[i]
		presetID := b.artifactID
		aid, aerr := s.recordArtifactTx(ctx, tx, inst, snapID, b.built, b.lang, version, presetID, now)
		if aerr != nil {
			return "", "", aerr
		}

		if presetID != "" && aid != presetID {
			return "", "", fmt.Errorf("产物身份与登记的发布回执不一致（账本 %s / 回执 %s）: %w",
				aid, presetID, presentationenums.Err(presentationenums.ErrBuildFailed))
		}
		b.artifactID = aid
		if b.lang == defaultLang {
			primaryArtifactID = aid
		}
	}
	if primaryArtifactID == "" && len(results) > 0 {
		primaryArtifactID = results[0].artifactID
	}

	if len(results) > 0 {
		deps := results[0].built.Manifest.Dependencies
		if derr := s.persistDependenciesTx(tx, inst.ID, primaryArtifactID, deps, now); derr != nil {
			return "", "", derr
		}
	}
	return primaryArtifactID, snapID, nil
}

// diagnosticsOf 把构建器的降级诊断转成发布内核的中立事实。
func diagnosticsOf(items []builder.Degrade) []publish.Diagnostic {
	if len(items) == 0 {
		return nil
	}
	out := make([]publish.Diagnostic, 0, len(items))
	for _, d := range items {
		out = append(out, publish.Diagnostic{
			Slot: d.Slot, NodeID: d.NodeID, NodeType: d.NodeType,
			Kind: string(d.Kind), RefID: d.RefID, Reason: d.Reason,
		})
	}
	return out
}
