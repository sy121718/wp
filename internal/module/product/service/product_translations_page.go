package productservice

import (
	"context"
	"sort"
	"strconv"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/pipeline"
	"go_wp/pkg/facing"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

type ProductTranslationStorePort interface {
	LoadDetailsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	Upsert(ctx context.Context, items []i18n.ContentWriteItem) (written int, err error)
}

type ProductTranslationInstancePort interface {
	MarkStaleByDependency(ctx context.Context, kind, key string) (ids []string, err error)
}

type I18nStaleMarker interface {
	MarkStaleForI18n(ctx context.Context) error
}

func (s *Service) SetTranslationStore(port ProductTranslationStorePort) { s.translationStore = port }

func (s *Service) SetTranslationInstances(port ProductTranslationInstancePort) {
	s.translationInstances = port
}

func (s *Service) SetI18nStaleMarker(m I18nStaleMarker) { s.i18nStaleMarker = m }

func (s *Service) translationPort() (ProductTranslationStorePort, error) {
	if s.translationStore != nil {
		return s.translationStore, nil
	}
	return i18n.NewContentWriterDefault()
}

func (s *Service) QueryProductTranslations(ctx context.Context, req *productdto.ProductTranslationsPageReq) (*productcontract.ProductTranslations, error) {
	if req == nil {
		req = &productdto.ProductTranslationsPageReq{}
	}
	scope, err := s.ResolveCatalogScope(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	result := &productcontract.ProductTranslations{ProjectID: scope.ProjectID, ProductID: strings.TrimSpace(req.ProductID), Groups: []productcontract.TranslationEntity{}}
	result.SourceLang = s.translationSourceLang(ctx, scope.ProjectID)
	result.Langs = targetTranslationLangs(s.translationEnabledLangs(ctx, scope.ProjectID), result.SourceLang)
	result.Lang = strings.TrimSpace(req.Lang)
	if !containsTranslationLang(result.Langs, result.Lang) {
		result.Lang = ""
		if len(result.Langs) > 0 {
			result.Lang = result.Langs[0]
		}
	}
	cands, err := s.translationCandidates(ctx, scope.ProjectID, result.ProductID)
	if err != nil {
		return nil, err
	}
	targets := map[string]i18n.ContentTargetInfo{}
	if result.Lang != "" && len(cands) > 0 {
		port, err := s.translationPort()
		if err != nil {
			return nil, err
		}
		targets, err = port.LoadDetailsForProject(ctx, scope.ProjectID, result.Lang, translationCandidateHashes(cands))
		if err != nil {
			return nil, err
		}
	}
	result.Groups = translationGroups(cands, targets)
	for _, g := range result.Groups {
		for _, row := range g.Rows {
			result.Total++
			if row.Target != "" {
				result.Done++
			}
		}
	}
	return result, nil
}

func (s *Service) translationCandidates(ctx context.Context, projectID, productID string) ([]productcontract.TranslationCandidate, error) {
	if productID != "" {
		return s.ProductTranslationCandidates(ctx, projectID, productID)
	}
	if projectID != "" {
		return s.ProjectTranslationCandidates(ctx, projectID)
	}
	return nil, nil
}

func (s *Service) SaveProductTranslations(ctx context.Context, req *productdto.SaveProductTranslationsReq) (res *productcontract.TranslationSaveResult, err error) {
	if req == nil {
		req = &productdto.SaveProductTranslationsReq{}
	}
	scope, err := s.ResolveCatalogScope(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	cands, err := s.translationCandidates(ctx, scope.ProjectID, strings.TrimSpace(req.ProductID))
	if err != nil {
		return nil, err
	}
	groups := translationGroups(cands, nil)
	view := &productcontract.TranslationSaveResult{}

	projectID := scope.ProjectID
	lang := strings.TrimSpace(req.Lang)

	if !s.translationLangAllowed(ctx, projectID, lang) {
		view.Errors = translationRowErrorFacts(productenums.MsgTranslationLangInvalid)
		return view, nil
	}

	contexts, hashes, targets := req.Contexts, req.Hashes, req.Targets
	if len(contexts) != len(hashes) || len(contexts) != len(targets) {
		view.Errors = translationRowErrorFacts(productenums.MsgTranslationInvalid)
		return view, nil
	}

	var rowErrors []facing.Labeled
	items := make([]i18n.ContentWriteItem, 0, len(contexts))
	writeKeys := make([]string, 0, len(contexts))
	queued := map[string]bool{}
	for i := range contexts {
		contextName := strings.TrimSpace(contexts[i])
		source, ok := translationSourceOf(groups, contextName, hashes[i])
		if !ok {
			rowErrors = append(rowErrors, facing.Labeled{Label: contextName, Msg: translationFactOf(productenums.MsgTranslationStale)})
			continue
		}
		target := strings.TrimSpace(targets[i])
		if target == "" {
			continue
		}
		key := i18n.ContentIndexKey(source.SourceHash, contextName)
		if queued[key] {
			continue
		}
		queued[key] = true
		if verr := validateProductTranslationTarget(contextName, source, target); verr != "" {
			rowErrors = append(rowErrors, facing.Labeled{Label: contextName, Msg: translationFactOf(verr)})
			continue
		}
		items = append(items, i18n.ContentWriteItem{

			ProjectID:  projectID,
			SourceHash: source.SourceHash, Context: contextName, Lang: lang,
			SourceText: source.SourceText, TargetText: target, Engine: i18n.ContentEngineManual,
		})
		writeKeys = append(writeKeys, key)
	}
	if len(rowErrors) > 0 {
		view.Errors = rowErrors
		return view, nil
	}
	if len(items) == 0 {
		view.Saved = true
		return view, nil
	}

	port, perr := s.translationPort()
	if perr != nil {
		return nil, perr
	}

	before, berr := port.LoadDetailsForProject(ctx, projectID, lang, translationWriteHashes(items))
	if berr != nil {
		return nil, berr
	}
	pending := make([]i18n.ContentWriteItem, 0, len(items))
	changedKeys := make([]string, 0, len(items))
	targetChanged := false
	for i, item := range items {
		prev := before[writeKeys[i]]
		if prev.TargetText == item.TargetText && prev.Engine == i18n.ContentEngineManual {
			continue
		}
		if prev.TargetText != item.TargetText {
			targetChanged = true
		}
		pending = append(pending, item)
		changedKeys = append(changedKeys, writeKeys[i])
	}

	written := 0
	if len(pending) > 0 {
		var uerr error
		written, uerr = port.Upsert(ctx, pending)
		if uerr != nil {
			return nil, uerr
		}
	}

	if targetChanged {
		if s.i18nStaleMarker != nil {
			if merr := s.i18nStaleMarker.MarkStaleForI18n(ctx); merr != nil {
				logger.Scene("product").With("project", projectID).Error(merr, "商品译文保存后标记页面待重建失败")

				view.StaleMarkFailed = true
			}
		}
		if s.markTranslationInstancesStale(ctx, groups, changedKeys) {
			view.StaleMarkFailed = true
		}
	}
	view.Saved = true
	view.Written = written
	return view, nil
}

func (s *Service) markTranslationInstancesStale(ctx context.Context, groups []productcontract.TranslationEntity, changedKeys []string) bool {
	if s.translationInstances == nil || len(changedKeys) == 0 {
		return false
	}
	failed := false
	for _, ref := range translationAffectedEntities(groups, changedKeys) {
		dep := pipeline.DirectContentKey(ref.EntityType, ref.EntityID)
		if _, err := s.translationInstances.MarkStaleByDependency(ctx, dep.Kind, dep.Key); err != nil {
			failed = true
			logger.Scene("product").With("entity_type", ref.EntityType).With("entity_id", ref.EntityID).
				Error(err, "商品译文保存后标记自动发布实例待重建失败")
		}
	}
	return failed
}

type translationEntityRef struct {
	EntityType string
	EntityID   string
}

func translationAffectedEntities(groups []productcontract.TranslationEntity, changedKeys []string) []translationEntityRef {
	byCandidate := map[string][]translationEntityRef{}
	for _, g := range groups {
		for _, r := range g.Rows {
			key := i18n.ContentIndexKey(r.SourceHash, r.Context)
			byCandidate[key] = append(byCandidate[key], translationEntityRef{EntityType: g.EntityType, EntityID: g.EntityID})
		}
	}
	seen := map[string]bool{}
	out := make([]translationEntityRef, 0, len(changedKeys))
	for _, key := range changedKeys {
		for _, ref := range byCandidate[key] {
			id := ref.EntityType + "/" + ref.EntityID
			if ref.EntityID == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, ref)
		}
	}
	return out
}

type translationSourceCandidate struct {
	SourceText string
	SourceHash string
	Rich       bool
}

func translationSourceOf(groups []productcontract.TranslationEntity, contextName, hash string) (translationSourceCandidate, bool) {
	for _, g := range groups {
		for _, r := range g.Rows {
			if r.Context == contextName && r.SourceHash == hash {
				return translationSourceCandidate{SourceText: r.Source, SourceHash: r.SourceHash, Rich: r.Field == "description"}, true
			}
		}
	}
	return translationSourceCandidate{}, false
}

func validateProductTranslationTarget(contextName string, source translationSourceCandidate, target string) string {
	entityType, field, ok := parseTranslationContext(contextName)
	if !ok || !productcontract.IsTranslatableField(entityType, field) {
		return productenums.ProductTranslationsErrContextInvalid
	}
	if !i18n.ShouldTranslateContent(source.SourceText) {
		return productenums.ProductTranslationsErrSourceSkipped
	}
	if strings.TrimSpace(target) == "" {
		return productenums.ProductTranslationsErrTargetEmpty
	}
	if source.Rich && hasTranslationMarkup(source.SourceText) != hasTranslationMarkup(target) {
		return productenums.ProductTranslationsErrShapeMismatch
	}
	return ""
}

func hasTranslationMarkup(s string) bool {
	return strings.Contains(s, "<") && strings.Contains(s, ">")
}

func parseTranslationContext(contextName string) (entityType, field string, ok bool) {
	contextName = strings.TrimSpace(contextName)
	i := strings.LastIndex(contextName, ".")
	if i <= 0 || i == len(contextName)-1 {
		return "", "", false
	}
	return contextName[:i], contextName[i+1:], true
}

func translationFactOf(msg string) facing.Msg {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return facing.Msg{}
	}
	return facing.Msg{Key: msg, Fallback: productenums.TranslationMsgFallbacks[msg]}
}

func translationRowErrorFacts(keys ...string) []facing.Labeled {
	out := make([]facing.Labeled, 0, len(keys))
	for _, k := range keys {
		if fact := translationFactOf(k); fact.Key != "" || fact.Fallback != "" {
			out = append(out, facing.Labeled{Msg: fact})
		}
	}
	return out
}

func translationGroups(cands []productcontract.TranslationCandidate, targets map[string]i18n.ContentTargetInfo) []productcontract.TranslationEntity {
	order := make([]string, 0, len(cands))
	index := map[string]*productcontract.TranslationEntity{}
	for _, c := range cands {
		key := c.EntityType + "/" + c.EntityID
		g, ok := index[key]
		if !ok {
			g = &productcontract.TranslationEntity{EntityType: c.EntityType, EntityID: c.EntityID, EntityName: c.EntityName}

			index[key] = g
			order = append(order, key)
		}
		info := targets[i18n.ContentIndexKey(c.SourceHash, c.Context)]
		g.Rows = append(g.Rows, productcontract.TranslationValue{
			Context: c.Context, Field: c.Field,
			Source: c.SourceText, SourceHash: c.SourceHash,
			Target: info.TargetText, Engine: info.Engine,
		})
	}
	out := make([]productcontract.TranslationEntity, 0, len(order))
	for _, key := range order {
		out = append(out, *index[key])
	}
	return out
}

func translationCandidateHashes(cands []productcontract.TranslationCandidate) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		if seen[c.SourceHash] {
			continue
		}
		seen[c.SourceHash] = true
		out = append(out, c.SourceHash)
	}
	sort.Strings(out)
	return out
}

func translationWriteHashes(items []i18n.ContentWriteItem) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if seen[item.SourceHash] {
			continue
		}
		seen[item.SourceHash] = true
		out = append(out, item.SourceHash)
	}
	sort.Strings(out)
	return out
}

func (s *Service) translationSourceLang(ctx context.Context, projectID string) string {
	if s.project != nil && strings.TrimSpace(projectID) != "" {
		if lang, err := s.project.DefaultLocale(ctx, projectID); err == nil && strings.TrimSpace(lang) != "" {
			return strings.TrimSpace(lang)
		}
	}
	return i18n.GetDefaultLang()
}

func targetTranslationLangs(langs []string, sourceLang string) []string {
	out := make([]string, 0, len(langs))
	for _, code := range langs {
		if code == sourceLang {
			continue
		}
		out = append(out, code)
	}
	return out
}

func (s *Service) translationEnabledLangs(ctx context.Context, projectID string) []string {
	if s.project != nil && strings.TrimSpace(projectID) != "" {
		if langs, err := s.project.EnabledLangs(ctx, projectID); err == nil && len(langs) > 0 {
			return langs
		}
	}
	return []string{i18n.GetDefaultLang()}
}

func (s *Service) translationLangAllowed(ctx context.Context, projectID, lang string) bool {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return false
	}
	return containsTranslationLang(s.translationEnabledLangs(ctx, projectID), lang)
}

func containsTranslationLang(langs []string, want string) bool {
	for _, code := range langs {
		if code == want {
			return true
		}
	}
	return false
}

func TranslationsSavedMsg(written int, staleMarkFailed bool) facing.Msg {
	if written > 0 {
		if staleMarkFailed {
			return facing.Msg{
				Key:      productenums.ProductTranslationsSavedStaleMarkFailed,
				Fallback: "已保存 {n} 条译文，但未能标记页面待重建 —— 请手动重新发布一次，否则前台仍是旧译文。",
				Named:    map[string]string{"n": strconv.Itoa(written)},
			}
		}
		return facing.Msg{
			Key:      productenums.ProductTranslationsSavedNote,
			Fallback: "已保存 {n} 条译文；译文变更已标记待重建（下次构建生效）。",
			Named:    map[string]string{"n": strconv.Itoa(written)},
		}
	}
	return facing.Msg{Key: productenums.ProductTranslationsSavedNone, Fallback: "没有需要写入的变化。"}
}

func TranslationsErrorsTitle() facing.Msg {
	return facing.Msg{Key: productenums.ProductTranslationsErrorsTitle, Fallback: "未保存：请先修正以下问题"}
}
