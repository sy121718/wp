package productservice

// 商品域原文、译文的查询与保存编排。
// 查询返回可复用的内容事实；保存独立校验候选、检测变化并写入，返回保存结论。
// 候选与译文按工程隔离，字段白名单与构建期共用。

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

// ProductTranslationStorePort 译文读写端口（收窄：只要这一屏用到的两个方法）。
//
// 生产实现 = pkg/i18n.ContentWriter；测试注入隔离 schema 的写入器。
type ProductTranslationStorePort interface {
	LoadDetailsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	Upsert(ctx context.Context, items []i18n.ContentWriteItem) (written int, err error)
}

// ProductTranslationInstancePort 译文变更后需要失效的自动发布实例端口。
//
// 消费者侧最窄接口：装配层把 presentation 契约实现传进来即可（跨模块只依赖契约）。
type ProductTranslationInstancePort interface {
	MarkStaleByDependency(ctx context.Context, kind, key string) (ids []string, err error)
}

// I18nStaleMarker 手工页面的全站 i18n 依赖标记（page 契约实现；可空）。
type I18nStaleMarker interface {
	MarkStaleForI18n(ctx context.Context) error
}

// SetTranslationStore 注入译文读写端口（测试用；未注入时用默认库）。
func (s *Service) SetTranslationStore(port ProductTranslationStorePort) { s.translationStore = port }

// SetTranslationInstances 注入自动发布实例失效端口（可空：为空时只做 page 侧标记）。
func (s *Service) SetTranslationInstances(port ProductTranslationInstancePort) {
	s.translationInstances = port
}

// SetI18nStaleMarker 注入手工页面的 i18n 依赖标记端口（可空）。
func (s *Service) SetI18nStaleMarker(m I18nStaleMarker) { s.i18nStaleMarker = m }

// translationPort 返回译文读写端口（未注入时用默认库）。
func (s *Service) translationPort() (ProductTranslationStorePort, error) {
	if s.translationStore != nil {
		return s.translationStore, nil
	}
	return i18n.NewContentWriterDefault()
}

// QueryProductTranslations 返回工程范围内的译文事实，不生成表单选项。
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

// translationCandidates 商品域候选文本（单商品入口 / 整工程入口）。
//
// 单商品入口也要带工程 id（DB-009 收口）：products 的 RLS 策略使非超级角色下不带作用域的
// 读静默 0 行，表现为「点开某个商品的翻译说商品不存在」。工程 id 与本页其它取数同源。
func (s *Service) translationCandidates(ctx context.Context, projectID, productID string) ([]productcontract.TranslationCandidate, error) {
	if productID != "" {
		return s.ProductTranslationCandidates(ctx, projectID, productID)
	}
	if projectID != "" {
		return s.ProjectTranslationCandidates(ctx, projectID)
	}
	return nil, nil
}

// SaveProductTranslations 保存译文的编排（整表提交）。
//
// 三步，顺序是判据不是风格：
//  1. 逐行校验（全部通过才写库）：任一行不合格就整批不写 —— 不做「部分成功」的中间态，
//     运营看到的是同一屏上哪几行要改，改完再提交一次；
//  2. 变更判定：只有译文文本确实变化才写库并触发重建（幂等，重复保存零写入）；
//  3. 译文变化 → 标记待重建：手工页面走全站 i18n 标记，自动发布实例按 direct_content
//     键精确标记受影响实体（商品页面属这类）。少标一次的表现是「译好了但线上还是旧语言」。
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

	// 工程使用已验证的作用域，语言保留请求值；写入不得回退到另一个目标语言。
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

	// 第一步：逐行校验（全部通过才写库，避免「部分成功」的中间态）。
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
			continue // 空输入 = 本行不写入（不删除库中已有译文）
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
			// 工程作用域（审计 I18N-009）：写入本工程自己的译文行。
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

	// 第二步：变更判定。只有译文文本确实变化才写库并触发重建（幂等，重复保存零写入）。
	// 变更判定按**本工程**读现有译文（工程行优先、回落全局行）。
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

	// 第三步：译文变化 → 标记待重建。
	//
	// 标记失败**必须让调用方看见**：它决定「已发布的页面会不会被重建」，而译文已经落库
	// （Upsert 成功）。原先把失败只写进日志、然后照常 `view.Saved = true` ——
	// 用户看到「已保存」、页面上却仍是旧译文，而且没有任何重试入口：
	// 下次保存同样的值会走 `prev.TargetText == item.TargetText` 那条「无变化」短路，
	// 永远补不上这次标记。
	//
	// 但不能反过来返回错误：数据确实已经写进去了，报失败会让用户重试并以为写丢了。
	// 所以走「已保存 + 逐行事实里带一条待重建失败的提示」——页面照常提示保存成功，
	// 同时告诉运营「产物需要重新发布一次」。
	if targetChanged {
		if s.i18nStaleMarker != nil {
			if merr := s.i18nStaleMarker.MarkStaleForI18n(ctx); merr != nil {
				logger.Scene("product").With("project", projectID).Error(merr, "商品译文保存后标记页面待重建失败")
				// 让运营知道产物没被标记：这一条不是「保存失败」，是「需要手动重新发布」。
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

// markTranslationInstancesStale 按 direct_content 键标记受影响实体的自动发布实例待重建。
//
// 译文按 (原文 hash, 语境) 寻址，实体身份只在候选里；同一段文本可能来自多个实体，
// 因此按「语境 + 指纹」回查本次渲染出的候选行，命中即视为该实体受影响
// （保守超集：宁可多标记，也不漏标记 —— 与页面侧「引用块即登记依赖」同一口径）。
//
// 返回是否有标记失败；译文已落库，失败事实进入保存结论，提醒重新发布。
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

// translationEntityRef 受影响的商品域实体（自动发布实例标记用）。
type translationEntityRef struct {
	EntityType string
	EntityID   string
}

// translationAffectedEntities 本次写入的译文对应的实体（按 (指纹, 语境) 回查候选行）。
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

// translationSourceCandidate 工作台校验用的一行原文。
type translationSourceCandidate struct {
	SourceText string
	SourceHash string
	Rich       bool
}

// translationSourceOf 按语境 + hash 反查本次提交对应的原文（防表单被裁剪/篡改）。
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

// validateProductTranslationTarget 校验一条商品域译文的可写性（与构建期同源）。
//
// 四项（全部与构建期一致）：
//  1. 语境必须是本模块契约声明的可翻译字段（"实体类型.字段名"）；
//  2. 原文参与翻译（i18n.ShouldTranslateContent，纯数字/纯符号/空白被拒）；
//  3. 译文非空（空输入在调用方已按「本行不写入」跳过，走到这里就是真的空）；
//  4. 富文本字段的译文形态与原文一致（是否含 HTML 标签必须相同）。
//
// 返回值是**词条 key**（不是中文句子）：它们经 translationFactOf 收成一条条事实后进页面的
// 错误列表，中文兜底登记在 productenums.TranslationMsgFallbacks 里 —— 写死中文的话英文
// 工作台上永远是中文，而这几条恰好是运营最常撞上的行级结论。
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

// hasTranslationMarkup 是否含 HTML 标签（与构建期 core.HasRichMarkup 同一判据的轻量版）。
func hasTranslationMarkup(s string) bool {
	return strings.Contains(s, "<") && strings.Contains(s, ">")
}

// parseTranslationContext 拆语境 "实体类型.字段名"（按最后一个点拆）。
func parseTranslationContext(contextName string) (entityType, field string, ok bool) {
	contextName = strings.TrimSpace(contextName)
	i := strings.LastIndex(contextName, ".")
	if i <= 0 || i == len(contextName)-1 {
		return "", "", false
	}
	return contextName[:i], contextName[i+1:], true
}

// translationFactOf enums key → 一条文案**事实**（不取词；取词在页面模板里做）。
//
// 空 key 与空兜底同时出现才算「没有这条文案」（模板判空写
// `{{if x.Key != "" || x.Fallback != ""}}`，结构体恒为真，不能写 `{{if x}}`）。
func translationFactOf(msg string) facing.Msg {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return facing.Msg{}
	}
	return facing.Msg{Key: msg, Fallback: productenums.TranslationMsgFallbacks[msg]}
}

// translationRowErrorFacts 页面级错误（对应不到具体某一行）→ 事实行，顺序即发生顺序。
func translationRowErrorFacts(keys ...string) []facing.Labeled {
	out := make([]facing.Labeled, 0, len(keys))
	for _, k := range keys {
		if fact := translationFactOf(k); fact.Key != "" || fact.Fallback != "" {
			out = append(out, facing.Labeled{Msg: fact})
		}
	}
	return out
}

// translationGroups 按实体身份关联原文与现有译文，不生成标题或字段标签。
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

// translationCandidateHashes 候选的 hash 去重集合（现有译文查询用）。
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

// translationWriteHashes 写入项的 hash 去重集合（变更判定用）。
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

// translationSourceLang 站点源语言。
//
// 判定依据（与构建期同一来源，见 pipeline.DefaultLocale）：project_locales 里 is_default
// 的那一行；清单缺失、没有默认标记或表不可读时，project 契约自身回退全局默认语言
// （sys_config 的 i18n 组 default_lang，见 project/service 的 DefaultLocale），这里再加一层
// 兜底保证源语言永远非空。商品文案（商品名 / 描述 / 分类名 / 品牌名 …）存的都是源语言原文，
// 译文只对**非源语言**有意义，所以翻译目标一律从启用语言里剔掉它。
func (s *Service) translationSourceLang(ctx context.Context, projectID string) string {
	if s.project != nil && strings.TrimSpace(projectID) != "" {
		if lang, err := s.project.DefaultLocale(ctx, projectID); err == nil && strings.TrimSpace(lang) != "" {
			return strings.TrimSpace(lang)
		}
	}
	return i18n.GetDefaultLang()
}

// targetTranslationLangs 启用语言剔除源语言后剩下的翻译目标（保持清单原顺序，
// 因此「默认语言在前」时第一个元素就是默认选中的目标语言）。
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

// translationEnabledLangs 站点启用语言（默认语言在前；清单不可读时回退站点默认语言一种）。
func (s *Service) translationEnabledLangs(ctx context.Context, projectID string) []string {
	if s.project != nil && strings.TrimSpace(projectID) != "" {
		if langs, err := s.project.EnabledLangs(ctx, projectID); err == nil && len(langs) > 0 {
			return langs
		}
	}
	return []string{i18n.GetDefaultLang()}
}

// translationLangAllowed 目标语言是否属于站点启用语言。
//
// 判据只要求「在启用清单里」，**不额外排除源语言**：页面下拉已经不列源语言、默认目标也
// 不会落在它上面（见 QueryProductTranslations），但写入侧保留按源语言落库的兼容路径 ——
// 自动发布实例是逐启用语言构建的（presentation.publishAllLangs），源语言那一份产物同样读
// sys_translation，收紧判据会让这条链路静默失效。
func (s *Service) translationLangAllowed(ctx context.Context, projectID, lang string) bool {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return false
	}
	return containsTranslationLang(s.translationEnabledLangs(ctx, projectID), lang)
}

// containsTranslationLang 语言码是否在清单里。
func containsTranslationLang(langs []string, want string) bool {
	for _, code := range langs {
		if code == want {
			return true
		}
	}
	return false
}

// TranslationsSavedMsg 译文保存回执事实；具名参数由响应出口填充。
//
// staleMarkFailed 为真时换一条词条：译文写进去了，但「标记页面待重建」失败 ——
// 那种情况下原句「译文变更已标记待重建」是假话，而运营需要知道「产物要手动重新发布一次」。
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

// TranslationsErrorsTitle 保存被拒时逐行结论的标题事实。
func TranslationsErrorsTitle() facing.Msg {
	return facing.Msg{Key: productenums.ProductTranslationsErrorsTitle, Fallback: "未保存：请先修正以下问题"}
}
