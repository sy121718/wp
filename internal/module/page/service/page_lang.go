package pageservice

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	publish "go_wp/pkg/publish"
	"slices"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pagemodel "go_wp/internal/module/page/model"
	projectcontract "go_wp/internal/module/project/contract"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/builder"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

func buildLang(requested string) string {
	lang := strings.TrimSpace(requested)
	if lang == "" {
		return i18n.GetDefaultLang()
	}
	return lang
}

func (s *Service) langURLRuleOf(ctx context.Context, projectID string) publish.LangURLRule {
	return pipeline.LangURLRuleForProject(ctx, s.project, projectID)
}

func sitePath(rule publish.LangURLRule, lang, logical string) (string, error) {
	return rule.Path(lang, logical)
}

func (s *Service) siteRoutePaths(ctx context.Context, projectID, logical string) ([]string, error) {
	entries, err := s.siteRouteEntries(ctx, projectID, logical)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return out, nil
}

type siteRouteEntry struct {
	Lang string
	Path string
}

func (s *Service) siteRouteEntries(ctx context.Context, projectID, logical string) ([]siteRouteEntry, error) {
	return s.siteRouteEntriesForLangs(ctx, projectID, logical, s.enabledLangsOf(ctx, projectID))
}

func (s *Service) siteRouteEntriesForLangs(ctx context.Context, projectID, logical string, langs []string) ([]siteRouteEntry, error) {

	if len(langs) == 0 {
		return nil, ErrInvalidPath
	}
	rule := s.langURLRuleOf(ctx, projectID)
	if verr := rule.Validate(langs); verr != nil {
		return nil, ErrInvalidPath
	}
	seen := map[string]bool{}
	out := make([]siteRouteEntry, 0, len(langs))
	for _, lang := range langs {
		p, perr := sitePath(rule, lang, logical)
		if perr != nil {
			return nil, ErrInvalidPath
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, siteRouteEntry{Lang: lang, Path: p})
	}
	return out, nil
}

type renameReservedInput struct {
	ProjectID string
	PageID    string

	OldLogical string
	NewLogical string

	TargetLang string

	Langs []string
}

func (s *Service) renameReservedAllLangsTx(ctx context.Context, tx *gorm.DB, in renameReservedInput) error {
	if s.routes == nil {
		return nil
	}
	oldEntries, err := s.siteRouteEntriesForLangs(ctx, in.ProjectID, in.OldLogical, in.Langs)
	if err != nil {
		return ErrInvalidPath
	}
	newEntries, err := s.siteRouteEntriesForLangs(ctx, in.ProjectID, in.NewLogical, in.Langs)
	if err != nil {
		return ErrInvalidPath
	}
	if len(oldEntries) != len(newEntries) {

		return ErrInvalidPath
	}
	for i := range oldEntries {
		oldPath, newPath := oldEntries[i].Path, newEntries[i].Path
		if oldPath == newPath {
			continue
		}
		onlyReserved := in.TargetLang != "" && oldEntries[i].Lang != in.TargetLang
		if rerr := s.routes.RenameReservedTx(ctx, tx, &pubcontract.RenameReservedReq{
			ProjectID: in.ProjectID, PageID: in.PageID, OldPath: oldPath, NewPath: newPath,
			OnlyReserved: onlyReserved,
		}); rerr != nil {
			return rerr
		}
	}
	return nil
}

func (s *Service) publishLangsOf(ctx context.Context, projectID string) ([]string, error) {
	return pipeline.ResolveSiteLangs(ctx, s.project, projectID, pipeline.LangFallbackForbidden)
}

func (s *Service) enabledLangsOf(ctx context.Context, projectID string) []string {
	return pipeline.EnabledLangs(ctx, s.project, projectID)
}

func (s *Service) defaultLocaleOf(ctx context.Context, projectID string) string {
	return pipeline.DefaultLocale(ctx, s.project, projectID)
}

func (s *Service) localizeMenuURL(ctx context.Context, projectID, lang, raw string) string {
	return pipeline.LocalizeMenuURL(ctx, s.project, projectID, lang, raw)
}

func (s *Service) MarkStaleByRegistryVersion(ctx context.Context, current string) (ids []string, err error) {
	if strings.TrimSpace(current) == "" || s.artifacts == nil {
		return nil, nil
	}
	currentArtifactIDs, err := s.model.ListCurrentArtifactIDs(ctx)
	if err != nil {
		return nil, err
	}
	ids, err = s.artifacts.ListStalePageIDs(ctx, current, currentArtifactIDs)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}

	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	at := time.Now().UTC()
	marked := &staleIDCollector{}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		hit, merr := s.model.MarkStaleByIDs(ctx, projectID, ids, at)
		if merr != nil {
			return nil, merr
		}
		marked.add(hit)
	}
	out := marked.list()
	logger.Scene("page").With("count", len(out)).With("registryVersion", current).
		Info("组件注册表版本变化：相关页面已标记待重建")

	s.logStaleImpact(ctx, "registry:"+current, out)
	return out, nil
}

type I18nStalePeer interface {
	MarkStaleForI18n(ctx context.Context) error

	MarkStaleForI18nTx(ctx context.Context, tx *gorm.DB, projectID string) error
}

func (s *Service) SetI18nStalePeer(peer I18nStalePeer) {
	s.i18nPeer = peer
}

func (s *Service) MarkStaleForI18n(ctx context.Context) error {
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return err
	}
	hit := &staleIDCollector{}
	at := time.Now().UTC()
	var failed []error
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			break
		}
		var ids []string
		terr := s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
			got, merr := s.model.MarkStaleForI18nTx(ctx, tx, projectID, at)
			if merr != nil {
				return merr
			}
			ids = got
			if s.i18nPeer != nil {
				return s.i18nPeer.MarkStaleForI18nTx(ctx, tx, projectID)
			}
			return nil
		})
		if terr != nil {

			failed = append(failed, fmt.Errorf("工程 %s 的译文失效标记失败: %w", projectID, terr))
			continue
		}
		hit.add(ids)
	}

	s.logStaleImpact(ctx, "i18n", hit.list())
	if len(failed) > 0 {
		return errors.Join(failed...)
	}
	return nil
}

func (s *Service) buildDependencies(ctx context.Context, in pipeline.BuildInput) []publish.Dependency {

	page, pageErr := s.locatePageInProjects(ctx, in.PageID)
	projectID := ""
	if page != nil {
		projectID = page.ProjectID
	}
	deps := []publish.Dependency{publish.I18NDependency(i18n.Revision())}
	deps = append(deps, pipeline.ActionTargetDependencies(in.Usage)...)
	if s.pageUsesContentTranslation(ctx, in) {

		deps = append(deps, publish.I18NContentDependency(i18n.ContentRevisionForProject(ctx, projectID)))
	}

	if pid := strings.TrimSpace(projectID); pid != "" {
		for _, kind := range in.Usage.MenuList() {
			k := publish.MenuKey(pid, kind)
			deps = append(deps, publish.Dependency{Kind: k.Kind, Key: k.Key})
		}

		for _, navID := range in.Usage.NavigationList() {
			k := publish.NavigationKey(navID)
			deps = append(deps, publish.Dependency{Kind: k.Kind, Key: k.Key})
		}

		for _, blockID := range in.Usage.BlockList() {
			k := publish.BlockKey(blockID)
			deps = append(deps, publish.Dependency{Kind: k.Kind, Key: k.Key})
		}
	}

	if pageErr == nil {
		deps = append(deps, s.pageDependencyKeys(ctx, page)...)
	} else {
		logger.Scene("dependency").With("page_id", in.PageID).
			Warn("页面依赖源登记跳过：页面记录读取失败（已降级，不阻断构建）")
	}
	return deps
}

func (s *Service) pageUsesContentTranslation(ctx context.Context, in pipeline.BuildInput) bool {
	lang := strings.TrimSpace(in.Lang)
	if lang == "" {
		return false
	}
	projectID, _ := s.pageContextOf(ctx, in.PageID, lang)

	defaultLang := s.defaultLocaleOf(ctx, projectID)
	if in.Plan != nil && strings.TrimSpace(in.Plan.DefaultLang) != "" {
		defaultLang = strings.TrimSpace(in.Plan.DefaultLang)
	}
	if lang == defaultLang {
		return false
	}
	page, err := builder.ParsePage(in.DocJSON)
	if err != nil {
		return false
	}
	return pageMayUseContentTranslation(page)
}

func pageMayUseContentTranslation(page *builder.Page) bool {
	if page == nil {
		return false
	}
	if len(builder.CollectContentCandidates(page)) > 0 {
		return true
	}

	if !page.Settings.Structure.IsEmpty() {
		return true
	}
	return len(builder.ReferencedBlockIDs(page.Root)) > 0
}

func (s *Service) contentTranslationEnabled(ctx context.Context, projectID, lang string) bool {
	return pipeline.ContentTranslationEnabled(ctx, s.project, projectID, lang)
}

func reportContentMisses(lang string, candidates int, misses int64) {
	if misses <= 0 {
		return
	}
	logger.Scene("build").
		With("lang", lang).
		With("candidates", candidates).
		With("misses", misses).
		Warn("构建期内容译文缺失，已回退原文（补齐译文后需重建，docs/06-D §9）")
}

var (
	ErrPageLangExcluded = facing.New(pageenums.ErrPageLangExcluded)

	ErrCannotExcludeDefaultLang = facing.New(pageenums.ErrCannotExcludeDefaultLang)

	ErrLangAlreadyExcluded = facing.New(pageenums.ErrLangAlreadyExcluded)

	ErrLangNotExcluded = facing.New(pageenums.ErrLangNotExcluded)
)

func pageExcludesLang(page *pagemodel.PageEntity, lang string) bool {
	if page == nil {
		return false
	}
	l := strings.TrimSpace(lang)
	if l == "" {
		return false
	}
	for _, raw := range page.ExcludedLangs {
		if strings.TrimSpace(raw) == l {
			return true
		}
	}
	return false
}

func (s *Service) PageLangStates(ctx context.Context, pageID string) (rows []pagedto.PageLangStateResp, err error) {
	page, err := s.getExistingPage(ctx, pageID)
	if err != nil {
		return nil, err
	}
	langs := s.enabledLangsOf(ctx, page.ProjectID)
	def := s.defaultLocaleOf(ctx, page.ProjectID)
	pubs, err := s.model.ListPublications(ctx, page.ID)
	if err != nil {
		return nil, err
	}
	published := make(map[string]bool, len(pubs))
	for i := range pubs {
		published[strings.TrimSpace(pubs[i].Lang)] = strings.TrimSpace(pubs[i].ActivePath) != ""
	}
	rows = make([]pagedto.PageLangStateResp, 0, len(langs))
	for _, lang := range langs {
		rows = append(rows, pagedto.PageLangStateResp{
			Lang:      lang,
			IsDefault: lang == def,
			Excluded:  pageExcludesLang(page, lang),
			Published: published[lang],
		})
	}
	return rows, nil
}

func (s *Service) ExcludePageLang(ctx context.Context, pageID, lang string) (retired int, err error) {
	page, err := s.getExistingPage(ctx, pageID)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err == nil {
			s.invalidateActionTarget(ctx, page.ID)
		}
	}()

	l := strings.TrimSpace(lang)
	if l == "" {
		return 0, ErrInvalidParam
	}
	if pageExcludesLang(page, l) {
		return 0, ErrLangAlreadyExcluded
	}
	if l == strings.TrimSpace(s.defaultLocaleOf(ctx, page.ProjectID)) {
		return 0, ErrCannotExcludeDefaultLang
	}
	if !langEnabled(ctx, s, page.ProjectID, l) {
		return 0, ErrInvalidParam
	}

	pub, perr := s.model.GetPublication(ctx, page.ID, l)
	if perr != nil && !errors.Is(perr, gorm.ErrRecordNotFound) {
		return 0, perr
	}
	activePath := ""
	if pub != nil {
		activePath = strings.TrimSpace(pub.ActivePath)
	}

	excluded := make([]string, 0, len(page.ExcludedLangs)+1)
	for _, raw := range page.ExcludedLangs {
		if v := strings.TrimSpace(raw); v != "" && v != l {
			excluded = append(excluded, v)
		}
	}
	excluded = append(excluded, l)

	if activePath != "" {

		intent, rerr := s.registerDeactivationIntent(ctx, page.ProjectID, page.ID, activePath, []string{activePath})
		if rerr != nil {
			return 0, rerr
		}
		if derr := s.excludeLangWithDeactivation(ctx, page, l, activePath, excluded, pub != nil, intent); derr != nil {
			return 0, derr
		}
		logger.Scene("publication").With("pageId", page.ID).With("lang", l).With("retired", 1).
			Info("已排除本页的该语言并下线其产物")
		return 1, nil
	}

	if terr := s.model.TransactionScoped(ctx, page.ProjectID, func(tx *gorm.DB) error {
		if uerr := s.model.UpdateExcludedLangsTx(ctx, tx, page.ProjectID, page.ID, excluded); uerr != nil {
			return uerr
		}
		if pub != nil {
			if derr := s.model.DeletePublicationsByLangTx(ctx, tx, page.ProjectID, page.ID, l); derr != nil {
				return derr
			}
		}
		if serr := s.model.DeleteStagingsByLangTx(ctx, tx, page.ProjectID, page.ID, l); serr != nil {
			return serr
		}

		if plerr := s.model.DeletePublicationPlansByLangTx(ctx, tx, page.ProjectID, page.ID, l); plerr != nil {
			return plerr
		}
		return nil
	}); terr != nil {
		return 0, terr
	}

	logger.Scene("publication").With("pageId", page.ID).With("lang", l).With("retired", retired).
		Info("已排除本页的该语言并下线其产物")
	return retired, nil
}

func (s *Service) excludeLangWithDeactivation(ctx context.Context, page *pagemodel.PageEntity, lang, activePath string,
	excluded []string, hasPub bool, in *deactivationIntent) error {

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
		got := normalizeOldPaths(current)

		if !containsPath(got, activePath) {
			return fmt.Errorf("%w: 路径 %s 已不在本页的激活集合内", errOldPathTaken, activePath)
		}
		if uerr := s.model.UpdateExcludedLangsTx(ctx, tx, in.ProjectID, in.PageID, excluded); uerr != nil {
			return uerr
		}
		if hasPub {
			if derr := s.model.DeletePublicationsByLangTx(ctx, tx, in.ProjectID, in.PageID, lang); derr != nil {
				return derr
			}
		}
		if serr := s.model.DeleteStagingsByLangTx(ctx, tx, in.ProjectID, in.PageID, lang); serr != nil {
			return serr
		}
		if plerr := s.model.DeletePublicationPlansByLangTx(ctx, tx, in.ProjectID, in.PageID, lang); plerr != nil {
			return plerr
		}

		if rerr := s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
			ProjectID: in.ProjectID, Path: activePath, PageID: in.PageID,
		}); rerr != nil {
			return rerr
		}
		if merr := s.markDeactivationCommitted(ctx, tx, in); merr != nil {
			return merr
		}
		callbackCompleted = true
		return nil
	})
	if txErr != nil && !callbackCompleted {
		s.abortPublishReceipt(ctx, in.ReceiptID, "排除语言的业务事务未提交，访问面保持原样")
		logger.Scene("publication").With("pageId", in.PageID).With("lang", lang).
			Error(txErr, "排除语言失败（业务事务未提交）")
		return mapPersistenceError(txErr)
	}
	if txErr != nil {

		s.keepPublishReceiptPending(in.ReceiptID, "排除语言业务事务提交结果不明")
		logger.Scene("publication").With("pageId", in.PageID).With("lang", lang).
			Error(txErr, "排除语言提交结果不明（回执保持 pending 交收敛重判）")
		return mapPersistenceError(txErr)
	}
	if !callbackCompleted {
		s.abortPublishReceipt(ctx, in.ReceiptID, "排除语言的业务事务未置提交标记")
		return nil
	}
	if werr := s.withdrawDeactivatedLinks(ctx, in.ProjectID, in.PageID, in.ReceiptID); werr != nil {
		s.keepPublishReceiptPending(in.ReceiptID, "排除语言已提交，撤链失败")
		logger.Scene("publication").With("pageId", in.PageID).With("lang", lang).
			Error(werr, "排除语言已提交但撤链失败（回执保持 pending 交收敛重放）")
		return nil
	}
	if cerr := s.completePublishReceipt(ctx, in.ReceiptID); cerr != nil {
		s.NotifyPendingReceipt()
	}
	return nil
}

func (s *Service) RestorePageLang(ctx context.Context, pageID, lang string) (err error) {
	page, err := s.getExistingPage(ctx, pageID)
	if err != nil {
		return err
	}
	l := strings.TrimSpace(lang)
	if l == "" {
		return ErrInvalidParam
	}
	if !pageExcludesLang(page, l) {
		return ErrLangNotExcluded
	}
	remaining := make([]string, 0, len(page.ExcludedLangs))
	for _, raw := range page.ExcludedLangs {
		if strings.TrimSpace(raw) == l {
			continue
		}
		remaining = append(remaining, strings.TrimSpace(raw))
	}
	if err = s.model.UpdateExcludedLangs(ctx, page.ProjectID, page.ID, remaining); err != nil {
		return err
	}
	logger.Scene("publication").With("pageId", page.ID).With("lang", l).
		Info("已解除本页的语言排除（不自动重新发布，重新发布走常规发布入口）")
	return nil
}

func appendExcludedLang(existing []string, lang string) []string {
	out := make([]string, 0, len(existing)+1)
	for _, raw := range existing {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	out = append(out, lang)
	return out
}

func langEnabled(ctx context.Context, s *Service, projectID, lang string) bool {
	langs, err := s.publishLangsOf(ctx, projectID)
	if err != nil {
		return false
	}
	for _, l := range langs {
		if strings.TrimSpace(l) == lang {
			return true
		}
	}
	return false
}

func dropExcludedLangs(inputs pipeline.SiteLangInputs, excluded []string) pipeline.SiteLangInputs {
	if len(excluded) == 0 || len(inputs.SiteLangs) == 0 {
		return inputs
	}
	skip := make(map[string]bool, len(excluded))
	for _, raw := range excluded {
		if v := strings.TrimSpace(raw); v != "" {
			skip[v] = true
		}
	}
	out := make([]string, 0, len(inputs.SiteLangs))
	for _, lang := range inputs.SiteLangs {
		if skip[strings.TrimSpace(lang)] {
			continue
		}
		out = append(out, lang)
	}
	if len(out) == 0 {

		return inputs
	}
	inputs.SiteLangs = out
	if skip[inputs.DefaultLang] {
		inputs.DefaultLang = out[0]
	}
	return inputs
}

func planHasExcludedLang(plan publish.PublicationPlan, excluded []string) bool {
	if len(excluded) == 0 {
		return false
	}
	skip := make(map[string]bool, len(excluded))
	for _, raw := range excluded {
		if v := strings.TrimSpace(raw); v != "" {
			skip[v] = true
		}
	}
	for _, lang := range plan.SiteLangs {
		if skip[strings.TrimSpace(lang)] {
			return true
		}
	}
	return false
}

const langPublishSkipped = "skipped"

type localeActiveRoute struct {
	PageID string
	Path   string
}

func (s *Service) LocaleRetireImpact(ctx context.Context, projectID, lang string) (affected int, err error) {
	routes, err := s.localeActiveRoutes(ctx, projectID, lang)
	if err != nil {
		return 0, err
	}
	return len(routes), nil
}

func (s *Service) RetireLocale(ctx context.Context, projectID, lang string) (retired int, err error) {
	routes, err := s.localeActiveRoutes(ctx, projectID, lang)
	if err != nil {
		return 0, err
	}
	if len(routes) == 0 {
		return 0, nil
	}
	if s.routes == nil {
		return 0, nil
	}

	pathsByPage := make(map[string][]string, len(routes))
	pageOrder := make([]string, 0, len(routes))
	for _, r := range routes {
		pageID := strings.TrimSpace(r.PageID)
		path := strings.TrimSpace(r.Path)
		if pageID == "" || path == "" {
			continue
		}
		if _, ok := pathsByPage[pageID]; !ok {
			pageOrder = append(pageOrder, pageID)
		}
		pathsByPage[pageID] = append(pathsByPage[pageID], path)
	}
	if len(pageOrder) == 0 {
		return 0, nil
	}
	sort.Strings(pageOrder)
	intents := make([]*deactivationIntent, 0, len(pageOrder))
	cleanup := func() {

		for _, in := range intents {
			s.abortPublishReceipt(ctx, in.ReceiptID, "语言退役未开始，回执作废")
		}
	}
	for _, pageID := range pageOrder {
		paths := normalizeOldPaths(pathsByPage[pageID])
		if len(paths) == 0 {
			continue
		}
		in, rerr := s.registerDeactivationIntent(ctx, projectID, pageID, firstNonEmptyPath(paths), paths)
		if rerr != nil {
			cleanup()
			return 0, rerr
		}
		intents = append(intents, in)
	}
	if len(intents) == 0 {
		return 0, nil
	}

	callbackCompleted := false
	txErr := s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		for _, in := range intents {

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
			got := normalizeOldPaths(current)
			for _, p := range in.Paths {
				if !containsPath(got, p) {
					return fmt.Errorf("%w: 路径 %s 已不在本页的激活集合内", errOldPathTaken, p)
				}
			}
			for _, p := range in.Paths {

				if derr := s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
					ProjectID: projectID, Path: p, PageID: in.PageID,
				}); derr != nil {
					return derr
				}
			}

			if perr := s.model.DeletePublicationsByLangTx(ctx, tx, projectID, in.PageID, lang); perr != nil {
				return perr
			}
			if serr := s.model.DeleteStagingsByLangTx(ctx, tx, projectID, in.PageID, lang); serr != nil {
				return serr
			}

			if plerr := s.model.DeletePublicationPlansByLangTx(ctx, tx, projectID, in.PageID, lang); plerr != nil {
				return plerr
			}

			if merr := s.markDeactivationCommitted(ctx, tx, in); merr != nil {
				return merr
			}
		}
		callbackCompleted = true
		return nil
	})
	if txErr != nil && !callbackCompleted {

		for _, in := range intents {
			s.abortPublishReceipt(ctx, in.ReceiptID, "语言退役的业务事务未提交，访问面保持原样")
		}
		return 0, txErr
	}
	if txErr != nil {

		for _, in := range intents {
			s.keepPublishReceiptPending(in.ReceiptID, "语言退役业务事务提交结果不明")
		}
		logger.Scene("publication").With("lang", lang).
			Error(txErr, "语言退役提交结果不明（回执全部保持 pending 交收敛重判）")
		return 0, txErr
	}
	if !callbackCompleted {
		for _, in := range intents {
			s.abortPublishReceipt(ctx, in.ReceiptID, "语言退役的业务事务未置提交标记")
		}
		return 0, nil
	}

	for _, in := range intents {
		if werr := s.withdrawDeactivatedLinks(ctx, projectID, in.PageID, in.ReceiptID); werr != nil {
			s.keepPublishReceiptPending(in.ReceiptID, "语言退役已提交，撤链失败")
			logger.Scene("publication").With("pageId", in.PageID).With("lang", lang).
				Warn("语言退役已提交但撤链失败（回执保持 pending 交收敛重放）: " + werr.Error())
			continue
		}
		if cerr := s.completePublishReceipt(ctx, in.ReceiptID); cerr != nil {
			s.NotifyPendingReceipt()
		}
	}
	return len(routes), nil
}

func (s *Service) localeActiveRoutes(ctx context.Context, projectID, lang string) (out []localeActiveRoute, err error) {
	if s.routes == nil || strings.TrimSpace(projectID) == "" || strings.TrimSpace(lang) == "" {
		return nil, nil
	}
	pages, err := s.model.ListAll(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(pages))
	for i := range pages {
		ids = append(ids, pages[i].ID)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	pubs, err := s.model.ListPublicationsByPages(ctx, ids, lang)
	if err != nil {
		return nil, err
	}
	for i := range pubs {
		if strings.TrimSpace(pubs[i].ActivePath) == "" {
			continue
		}
		out = append(out, localeActiveRoute{PageID: pubs[i].PageID, Path: pubs[i].ActivePath})
	}
	return out, nil
}

var _ projectcontract.LocaleRetirePort = (*Service)(nil)

func (s *Service) UntranslatedPageLangs(ctx context.Context, projectID string) (rows []pagedto.TranslationMissResp, err error) {
	pid := strings.TrimSpace(projectID)
	if pid == "" {
		return nil, ErrInvalidParam
	}

	titles, err := s.model.ListPageTitles(ctx, pid)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(titles))
	pathOf := make(map[string]string, len(titles))
	for i := range titles {
		ids = append(ids, titles[i].ID)
		pathOf[titles[i].ID] = titles[i].DraftPath
	}
	if s.pageArtifacts == nil {
		return nil, errors.New("page: artifact 契约未注入，无法读取产物缺译计数")
	}
	raw, err := s.pageArtifacts.TranslationMisses(ctx, ids)
	if err != nil {
		return nil, err
	}
	excluded, err := s.excludedLangsOfProject(ctx, pid)
	if err != nil {
		return nil, err
	}
	rows = make([]pagedto.TranslationMissResp, 0, len(raw))
	for i := range raw {
		r := raw[i]
		if excluded[r.PageID][strings.TrimSpace(r.Lang)] {
			continue
		}
		rows = append(rows, pagedto.TranslationMissResp{
			PageID: r.PageID, DraftPath: pathOf[r.PageID], Lang: r.Lang,
			Misses: r.Misses, Candidates: r.Candidates,
		})
	}

	slices.SortStableFunc(rows, func(a, b pagedto.TranslationMissResp) int {
		return cmp.Or(
			cmp.Compare(b.Misses, a.Misses),
			cmp.Compare(a.DraftPath, b.DraftPath),
			cmp.Compare(a.Lang, b.Lang),
		)
	})
	return rows, nil
}

func (s *Service) excludedLangsOfProject(ctx context.Context, projectID string) (map[string]map[string]bool, error) {
	pages, err := s.model.ListAll(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	out := make(map[string]map[string]bool, len(pages))
	for i := range pages {
		if len(pages[i].ExcludedLangs) == 0 {
			continue
		}
		set := make(map[string]bool, len(pages[i].ExcludedLangs))
		for _, l := range pages[i].ExcludedLangs {
			if v := strings.TrimSpace(l); v != "" {
				set[v] = true
			}
		}
		out[pages[i].ID] = set
	}
	return out, nil
}

var _ = pagemodel.PageTitleRow{}
