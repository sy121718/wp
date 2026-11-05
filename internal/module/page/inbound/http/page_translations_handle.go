package pagehttp

import (
	"go_wp/internal/backref"
	"context"
	"errors"
	pageview "go_wp/pkg/view"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/pkg/builder"
	"go_wp/pkg/builder/core"
	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/binding"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

type contentTranslationPort interface {
	LoadDetails(ctx context.Context, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	LoadDetailsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error)
	LoadTargetsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]string, error)
	Upsert(ctx context.Context, items []i18n.ContentWriteItem) (written int, err error)
}

func (h *pagesAdminHandle) SetContentTranslationStore(store contentTranslationPort) {
	h.contentStore = store
}

func (h *pagesAdminHandle) contentPort() (contentTranslationPort, error) {
	if h.contentStore != nil {
		return h.contentStore, nil
	}
	return i18n.NewContentWriterDefault()
}

func (h *pagesAdminHandle) pageOf(c *gin.Context, pageID string) (*pagecontract.PageResp, error) {
	if h.pages == nil {
		return nil, errors.New("页面服务未装配")
	}

	ctx := c.Request.Context()
	projectID, err := h.pages.ProjectOfPage(ctx, pageID)
	if err != nil {
		return nil, err
	}
	return h.pages.Detail(ctx, &pagecontract.DetailReq{ProjectID: projectID, ID: pageID})
}

var translationMsgFallback = map[string]string{
	pageenums.MsgTranslationSaveFailed:      "译文保存失败，请稍后重试",
	pageenums.MsgTranslationInvalid:         "提交数据不完整，请刷新页面后重试",
	pageenums.MsgTranslationStale:           "原文已变更，请刷新页面后重新翻译",
	pageenums.MsgTranslationLangInvalid:     "目标语言未启用，请先在站点设置里启用",
	pageenums.MsgTranslationDocInvalid:      "页面草稿无法解析，请先在工作台修复页面",
	pageenums.MsgTranslationSiteScanSkipped: "全站统计暂不可用，当前仅显示本页维度",
	pageenums.MsgTranslationSiteScanTooMany: "页面数超过全站扫描上限，当前仅显示本页维度",
}

func translationFact(msg string) pageMsg {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return pageMsg{}
	}
	return pageMsg{Key: msg, Fallback: translationMsgFallback[msg]}
}

func translationRowErrors(keys ...string) []TranslationRowErrorResp {
	out := make([]TranslationRowErrorResp, 0, len(keys))
	for _, k := range keys {
		if fact := translationFact(k); !fact.Empty() {
			out = append(out, TranslationRowErrorResp{Msg: fact})
		}
	}
	return out
}

type pageTranslationsData struct {
	Title     string `view:"title"`
	Menu      string `view:"menu"`
	PageID    string
	PagePath  string
	ProjectID string
	Lang      string
	Langs     []TranslationLangOptionResp
	Filter    string
	Groups    []TranslationGroupResp
	RowCount  int
	PageDone  int
	PageTotal int
	SiteDone  int
	SiteTotal int

	SiteNote pageMsg

	IsDefaultLang bool

	Errors []TranslationRowErrorResp

	LoadFailed bool
	LoadError  pageMsg

	ParamInvalid bool
	ParamError   pageMsg

	LangStates []PageLangResp

	LangStatesErr pageMsg
}

const (
	translationFilterAll     = "all"
	translationFilterMissing = "missing"
	translationFilterManual  = "manual"
	translationFilterAI      = "ai"
)

const translationsPageMenu = "pages"

var errPagesServiceMissing = errors.New("pages service 未装配")

func (h *pagesAdminHandle) PageTranslations(c *gin.Context) {
	pageID := strings.TrimSpace(c.Query("pageId"))
	lang := strings.TrimSpace(c.Query("lang"))
	if pageID == "" {

		h.renderTranslationsFailure(c, http.StatusBadRequest, pageTranslationsFailure{
			pageID: pageID, lang: lang,
			paramError: pagesLocalNoticeMissingPageIDGet,
		})
		return
	}
	if h.pages == nil {

		logger.Scene("page").With("pageId", pageID).Error(errPagesServiceMissing, "打开翻译工作台失败")
		h.renderTranslationsFailure(c, http.StatusInternalServerError, pageTranslationsFailure{
			pageID: pageID, lang: lang, loadError: adminui.InternalMsg(),
		})
		return
	}
	data, err := h.buildPageTranslationsData(c.Request.Context(), pageID, lang,
		strings.TrimSpace(c.Query("filter")))
	if err != nil {
		logger.Scene("page").With("pageId", pageID).Error(err, "打开翻译工作台失败")
		h.renderTranslationsFailure(c, translationsFailureStatus(err), pageTranslationsFailure{
			pageID: pageID, lang: lang, loadError: pageErrMsgQuiet(c, err),
		})
		return
	}
	pageview.RenderTemplate(c, PageTranslationsTemplate, data)
}

type pageTranslationsFailure struct {
	pageID     string
	lang       string
	paramError pageMsg
	loadError  pageMsg
}

func translationsFailureStatus(err error) int {
	switch status := response.ErrorStatus(err); status {
	case http.StatusNotFound, http.StatusBadRequest:
		return status
	default:
		return http.StatusInternalServerError
	}
}

func (h *pagesAdminHandle) renderTranslationsFailure(c *gin.Context, status int, f pageTranslationsFailure) {
	msg := f.loadError
	if !f.paramError.Empty() {
		msg = f.paramError
	}
	response.ErrorMsg(c, status, msg)
}

func (h *pagesAdminHandle) SavePageTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	req, ok := binding.Form[pagedto.PageTranslationsSaveFormReq](c)
	if !ok {
		return
	}
	pageID := strings.TrimSpace(req.PageID)
	if pageID == "" {

		pageListJump(c, false, pagesLocalNoticeMissingPageID)
		return
	}
	if h.pages == nil {
		pageListJump(c, false, pageInternalMsg())
		return
	}
	lang := strings.TrimSpace(req.Lang)

	page, err := h.pageOf(c, pageID)
	if err != nil {
		logger.Scene("page").With("pageId", pageID).Error(err, "读取页面失败")
		pageListJump(c, false, pageErrMsgQuiet(c, err))
		return
	}
	if !h.langAllowed(ctx, page.ProjectID, lang) {
		h.renderTranslationError(c, pageID, lang, translationRowErrors(pageenums.MsgTranslationLangInvalid))
		return
	}

	contexts := req.RowContext
	sources := req.RowSource
	hashes := req.RowHash
	targets := req.RowTarget
	if len(contexts) != len(sources) || len(contexts) != len(hashes) || len(contexts) != len(targets) {
		h.renderTranslationError(c, pageID, lang, translationRowErrors(pageenums.MsgTranslationInvalid))
		return
	}

	var rowErrors []TranslationRowErrorResp
	items := make([]i18n.ContentWriteItem, 0, len(contexts))
	writeKeys := make([]string, 0, len(contexts))
	queued := make(map[string]bool, len(contexts))
	for i := range contexts {
		contextName := strings.TrimSpace(contexts[i])
		source := sources[i]
		target := strings.TrimSpace(targets[i])
		if target == "" {

			continue
		}

		dedupeKey := i18n.ContentIndexKey(i18n.ContentHash(source), contextName)
		if queued[dedupeKey] {
			continue
		}
		queued[dedupeKey] = true

		if strings.TrimSpace(hashes[i]) != i18n.ContentHash(source) {
			rowErrors = append(rowErrors, TranslationRowErrorResp{Label: contextName,
				Msg: translationFact(pageenums.MsgTranslationStale)})
			continue
		}
		if verr := builder.ValidateContentTarget(contextName, source, target); verr != nil {
			rowErrors = append(rowErrors, TranslationRowErrorResp{Label: contextName,
				Msg: pageTranslationRowFact(c, verr)})
			continue
		}
		items = append(items, i18n.ContentWriteItem{

			ProjectID:  page.ProjectID,
			SourceHash: i18n.ContentHash(source), Context: contextName, Lang: lang,
			SourceText: source, TargetText: target, Engine: i18n.ContentEngineManual,
		})
		writeKeys = append(writeKeys, i18n.ContentIndexKey(i18n.ContentHash(source), contextName))
	}
	if len(rowErrors) > 0 {
		h.renderTranslationError(c, pageID, lang, rowErrors)
		return
	}
	if len(items) == 0 {
		response.SuccessMsg(c, pageTranslationsSavedMsg(0), gin.H{"written": 0})
		return
	}

	port, perr := h.contentPort()
	if perr != nil {
		logger.Scene("page").With("pageId", pageID).Error(perr, "内容译文存储不可用")
		h.renderTranslationError(c, pageID, lang, translationRowErrors(pageenums.MsgTranslationSaveFailed))
		return
	}

	before, berr := port.LoadDetailsForProject(ctx, page.ProjectID, lang, candidateHashes(items))
	if berr != nil {
		logger.Scene("page").With("pageId", pageID).Error(berr, "读取现有译文失败，按全部变更处理")
		before = map[string]i18n.ContentTargetInfo{}
	}
	pending := make([]i18n.ContentWriteItem, 0, len(items))
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
	}

	written := 0
	if len(pending) > 0 {
		var uerr error
		written, uerr = port.Upsert(ctx, pending)
		if uerr != nil {
			logger.Scene("page").With("pageId", pageID).With("lang", lang).Error(uerr, "写入译文失败")
			h.renderTranslationError(c, pageID, lang, translationRowErrors(pageenums.MsgTranslationSaveFailed))
			return
		}
	}

	if targetChanged && h.pages != nil {
		if merr := h.pages.MarkStaleForI18n(ctx); merr != nil {
			logger.Scene("page").With("pageId", pageID).Error(merr, "译文保存后标记全站待重建失败")
		}
	}

	response.SuccessMsg(c, pageTranslationsSavedMsg(written), gin.H{"written": written})
}

func (h *pagesAdminHandle) renderTranslationError(c *gin.Context, pageID, lang string, errs []TranslationRowErrorResp) {
	c.HTML(http.StatusOK, "admin/page/page_translations_save_errors.html", adminui.PrepareBareView(c, &TranslationErrorsResp{
		Errors: errs,
	}))
}

func (h *pagesAdminHandle) buildPageTranslationsData(ctx context.Context, pageID, wantLang, filter string) (*pageTranslationsData, error) {

	projectID, err := h.pages.ProjectOfPage(ctx, pageID)
	if err != nil {
		return nil, err
	}
	page, err := h.pages.Detail(ctx, &pagecontract.DetailReq{ProjectID: projectID, ID: pageID})
	if err != nil {
		return nil, err
	}
	langs := h.enabledLangsOf(ctx, page.ProjectID)
	defaultLang := langs[0]
	lang := strings.TrimSpace(wantLang)
	if !containsString(langs, lang) {
		lang = defaultLang
	}

	data := &pageTranslationsData{
		Title: pageenums.MsgPageTranslationsTitle, Menu: "pages",
		PageID: page.ID, PagePath: page.DraftPath, ProjectID: page.ProjectID,
		Lang: lang, Filter: normalizeTranslationFilter(filter),
		IsDefaultLang: lang == defaultLang,
	}

	data.LangStates, data.LangStatesErr = h.pageLangStatesOf(ctx, page.ID)
	for _, code := range langs {
		data.Langs = append(data.Langs, TranslationLangOptionResp{Code: code, Label: code, Active: code == lang})
	}

	parsed, perr := builder.ParsePage(page.DraftDocument)
	if perr != nil {
		data.Errors = append(data.Errors, TranslationRowErrorResp{Msg: translationFact(pageenums.MsgTranslationDocInvalid)})
		return data, nil
	}

	pageCandidates := builder.CollectContentCandidates(parsed)
	blockInfo := h.collectBlockCandidates(ctx, page.ProjectID, parsed)
	candidates := mergeContentCandidates(pageCandidates, blockInfo.candidates)
	pageKeys := make(map[string]bool, len(pageCandidates))
	for _, cand := range pageCandidates {
		pageKeys[i18n.ContentIndexKey(i18n.ContentHash(cand.Source), cand.Context)] = true
	}
	data.PageTotal = len(candidates)
	if len(candidates) == 0 {
		return data, nil
	}

	details := map[string]i18n.ContentTargetInfo{}
	if port, cerr := h.contentPort(); cerr == nil {
		if got, lerr := port.LoadDetailsForProject(ctx, page.ProjectID, lang, builder.ContentHashes(candidates)); lerr == nil {
			details = got
		} else {
			logger.Scene("page").With("pageId", pageID).Error(lerr, "读取现有译文失败")
		}
	}

	site, serr := h.siteContentIndexOf(ctx)
	if serr != nil {
		logger.Scene("page").Error(serr, "全站翻译统计不可用，工作台退化为本页维度")
		site = nil
		data.SiteNote = translationFact(pageenums.MsgTranslationSiteScanSkipped)
	}
	if site != nil && site.skipped {
		site = nil
		data.SiteNote = translationFact(pageenums.MsgTranslationSiteScanTooMany)
	}
	if site != nil {
		data.SiteTotal = site.total()
		data.SiteDone = h.countTranslated(ctx, page.ProjectID, lang, site)
	}

	groups := make([]TranslationGroupResp, 0, 8)
	for _, cand := range candidates {
		component, field, ok := i18n.ParseContentContext(cand.Context)
		if !ok {
			continue
		}
		key := i18n.ContentIndexKey(i18n.ContentHash(cand.Source), cand.Context)
		meta, _ := core.TranslatableFieldMeta(component, field)
		limit, _ := builder.ContentTargetLimit(cand.Context)
		row := TranslationRowResp{
			Context: cand.Context, Component: component, Field: field,
			Source: cand.Source, SourceHash: i18n.ContentHash(cand.Source),
			Rich: meta.Rich(), Limit: limit,
		}

		if !pageKeys[key] {
			row.Origin = blockInfo.origin[key]
		}
		if info, hit := details[key]; hit && info.TargetText != "" {
			row.Target = info.TargetText
			row.Engine = info.Engine
			row.Translated = true
		}

		if site != nil {
			row.ReusePaths = site.reusePathsOf(key)
			row.ReusePages, row.ReuseTotal = reuseCounts(row.ReusePaths, page.DraftPath)
		}
		if len(groups) == 0 || groups[len(groups)-1].Component != component {
			groups = append(groups, TranslationGroupResp{Component: component})
		}
		groups[len(groups)-1].Rows = append(groups[len(groups)-1].Rows, row)
	}

	for _, g := range groups {
		for _, row := range g.Rows {
			if row.Translated {
				data.PageDone++
			}
		}
	}
	data.Groups = filterTranslationGroups(groups, data.Filter)
	for _, g := range data.Groups {
		data.RowCount += len(g.Rows)
	}
	return data, nil
}

func (h *pagesAdminHandle) countTranslated(ctx context.Context, projectID, lang string, site *siteContentIndex) int {
	if site == nil || len(site.hashes) == 0 {
		return 0
	}
	port, err := h.contentPort()
	if err != nil {
		return 0
	}
	targets, err := port.LoadTargetsForProject(ctx, projectID, lang, site.hashes)
	if err != nil {
		logger.Scene("page").Error(err, "统计全站翻译完成度失败")
		return 0
	}
	done := 0
	for _, key := range site.keys {
		if targets[key] != "" {
			done++
		}
	}
	return done
}

func filterTranslationGroups(groups []TranslationGroupResp, filter string) []TranslationGroupResp {
	if filter == translationFilterAll {
		return groups
	}
	out := make([]TranslationGroupResp, 0, len(groups))
	for _, g := range groups {
		rows := make([]TranslationRowResp, 0, len(g.Rows))
		for _, row := range g.Rows {
			switch filter {
			case translationFilterMissing:
				if row.Translated {
					continue
				}
			case translationFilterManual:
				if row.Engine != i18n.ContentEngineManual {
					continue
				}
			case translationFilterAI:
				if row.Engine != i18n.ContentEngineAI {
					continue
				}
			}
			rows = append(rows, row)
		}
		if len(rows) > 0 {
			out = append(out, TranslationGroupResp{Component: g.Component, Rows: rows})
		}
	}
	return out
}

func normalizeTranslationFilter(raw string) string {
	switch strings.TrimSpace(raw) {
	case translationFilterMissing, translationFilterManual, translationFilterAI:
		return strings.TrimSpace(raw)
	default:
		return translationFilterAll
	}
}

func (h *pagesAdminHandle) enabledLangsOf(ctx context.Context, projectID string) []string {
	if h.projects != nil && strings.TrimSpace(projectID) != "" {
		if langs, err := h.projects.EnabledLangs(ctx, projectID); err == nil && len(langs) > 0 {
			return langs
		}
	}
	return []string{i18n.GetDefaultLang()}
}

func (h *pagesAdminHandle) langAllowed(ctx context.Context, projectID, lang string) bool {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return false
	}
	return containsString(h.enabledLangsOf(ctx, projectID), lang)
}

var translationBackKeys = []string{"pageId", "lang"}

const pageTranslationsPath = "/admin/pages/translations"

func translationBack(c *gin.Context) string {
	return backref.BackPath(c, pageTranslationsPath, translationBackKeys...)
}

var translationBackMsg = pageMsg{Key: pageenums.MsgPageTranslationsTitle, Fallback: "多语言"}

func pageTranslationsSavedMsg(written int) pageMsg {
	n := strconv.Itoa(written)
	if written > 0 {
		return pageMsg{
			Key:      pageenums.TranslationsSavedRebuild,
			Fallback: "已保存 %s 条译文。译文变更已标记全站待重建（下次构建生效）。",
			Args:     []string{n},
		}
	}
	return pageMsg{
		Key:      pageenums.TranslationsSaved,
		Fallback: "已保存 %s 条译文。",
		Args:     []string{n},
	}
}

var pageTranslationSentinels = []error{
	builder.ErrContentContextInvalid,
	builder.ErrContentSourceSkipped,
	builder.ErrContentTargetEmpty,
	builder.ErrContentTargetTooLong,
	builder.ErrContentTargetShape,
}

func pageTranslationRowFact(c *gin.Context, err error) pageMsg {
	if err == nil {
		return pageMsg{}
	}
	for _, sentinel := range pageTranslationSentinels {
		if errors.Is(err, sentinel) {
			return pageMsg{Fallback: err.Error()}
		}
	}
	return pageErrMsgQuiet(c, err)
}

var (
	pagesLocalNoticeMissingPageID = pageMsg{Key: pageenums.MsgFieldRequired, Fallback: "必填字段不能为空，未保存。"}

	pagesLocalNoticeMissingPageIDGet = pageMsg{Key: pageenums.ErrInvalidParam, Fallback: "请求参数无效：地址里缺少页面标识，请从页面列表进入翻译工作台。"}
)

type blockCandidateInfo struct {
	candidates []builder.ContentCandidate
	origin     map[string]string
}

func (h *pagesAdminHandle) collectBlockCandidates(ctx context.Context, projectID string, page *builder.Page) blockCandidateInfo {
	info := blockCandidateInfo{origin: map[string]string{}}
	if h == nil || h.blocks == nil || page == nil {
		return info
	}
	cache := map[string]*builder.Page{}
	visited := map[string]bool{}
	seen := map[string]bool{}

	var walk func(blockID, origin string)
	walk = func(blockID, origin string) {
		blockID = strings.TrimSpace(blockID)
		if blockID == "" || visited[blockID] {
			return
		}
		visited[blockID] = true
		blockPage := h.blockPageOf(ctx, projectID, blockID, cache)
		if blockPage == nil {
			return
		}
		for _, cand := range builder.CollectContentCandidates(blockPage) {
			key := i18n.ContentIndexKey(i18n.ContentHash(cand.Source), cand.Context)
			if _, ok := info.origin[key]; !ok {
				info.origin[key] = origin
			}
			dedupe := cand.Context + "\x00" + cand.Source
			if seen[dedupe] {
				continue
			}
			seen[dedupe] = true
			info.candidates = append(info.candidates, cand)
		}

		for _, nested := range builder.ReferencedBlockIDs(blockPage.Root) {
			walk(nested, origin)
		}
	}

	slotBindings := page.Settings.Structure.SlotBindings()
	for _, slot := range builder.SortedSlots(slotBindings) {

		walk(slotBindings[slot], pageenums.TranslationOriginOfSlot(slot))
	}
	for _, ref := range builder.ReferencedBlockIDs(page.Root) {
		walk(ref, pageenums.TranslationOriginBlock)
	}
	return info
}

func (h *pagesAdminHandle) blockPageOf(ctx context.Context, projectID, blockID string, cache map[string]*builder.Page) *builder.Page {
	if cache != nil {
		if p, ok := cache[blockID]; ok {
			return p
		}
	}
	block, err := h.blocks.Detail(ctx, &blockcontract.DetailReq{ProjectID: projectID, ID: blockID})
	if err != nil || block == nil || len(block.Document) == 0 {
		logger.Scene("page").With("block", blockID).Warn("工作台读取块文档失败，已跳过该块的文本")
		if cache != nil {
			cache[blockID] = nil
		}
		return nil
	}
	page, perr := builder.ParsePage(block.Document)
	if perr != nil {
		logger.Scene("page").With("block", blockID).Error(perr, "工作台解析块文档失败，已跳过该块的文本")
		page = nil
	}
	if cache != nil {
		cache[blockID] = page
	}
	return page
}

func mergeContentCandidates(pageCands, blockCands []builder.ContentCandidate) []builder.ContentCandidate {
	if len(blockCands) == 0 {
		return pageCands
	}
	seen := make(map[string]bool, len(pageCands)+len(blockCands))
	out := make([]builder.ContentCandidate, 0, len(pageCands)+len(blockCands))
	for _, c := range pageCands {
		seen[c.Context+"\x00"+c.Source] = true
		out = append(out, c)
	}
	for _, c := range blockCands {
		if seen[c.Context+"\x00"+c.Source] {
			continue
		}
		seen[c.Context+"\x00"+c.Source] = true
		out = append(out, c)
	}
	return out
}

func candidateHashes(items []i18n.ContentWriteItem) []string {
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

func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

const (
	siteContentIndexTTL = 30 * time.Second

	siteContentScanPageLimit = 1000
)

var errSiteContentIndexSkipped = errors.New("全站翻译统计已跳过")

type siteContentIndex struct {
	keys []string

	hashes []string

	usage map[string][]string

	pages int

	skipped bool
}

func (s *siteContentIndex) total() int {
	if s == nil {
		return 0
	}
	return len(s.keys)
}

func (s *siteContentIndex) reusePagesOf(key string) int {
	if s == nil {
		return 0
	}
	return len(s.usage[key])
}

func (s *siteContentIndex) reusePathsOf(key string) []string {
	if s == nil {
		return nil
	}
	return s.usage[key]
}

type siteContentIndexCache struct {
	mu  sync.Mutex
	at  time.Time
	idx *siteContentIndex
}

func (h *pagesAdminHandle) siteContentIndexOf(ctx context.Context) (*siteContentIndex, error) {
	if h == nil {
		return nil, errSiteContentIndexSkipped
	}
	h.siteIndex.mu.Lock()
	defer h.siteIndex.mu.Unlock()
	if h.siteIndex.idx != nil && time.Since(h.siteIndex.at) < siteContentIndexTTL {
		return h.siteIndex.idx, nil
	}
	idx, err := h.buildSiteContentIndex(ctx)
	if err != nil {
		return nil, err
	}
	h.siteIndex.idx, h.siteIndex.at = idx, time.Now()
	return idx, nil
}

func (h *pagesAdminHandle) buildSiteContentIndex(ctx context.Context) (*siteContentIndex, error) {
	if h.pages == nil {
		return nil, errSiteContentIndexSkipped
	}
	drafts, err := h.pages.ListDrafts(ctx)
	if err != nil {
		return nil, err
	}
	if len(drafts) > siteContentScanPageLimit {
		return &siteContentIndex{skipped: true, pages: len(drafts)}, nil
	}

	idx := &siteContentIndex{usage: map[string][]string{}, pages: len(drafts)}
	seenKey := make(map[string]bool)
	seenHash := make(map[string]bool)
	seenPage := make(map[string]map[string]bool)

	add := func(cand builder.ContentCandidate, path string) {
		hash := i18n.ContentHash(cand.Source)
		key := i18n.ContentIndexKey(hash, cand.Context)
		if !seenKey[key] {
			seenKey[key] = true
			idx.keys = append(idx.keys, key)
		}
		if !seenHash[hash] {
			seenHash[hash] = true
			idx.hashes = append(idx.hashes, hash)
		}
		if seenPage[key] == nil {
			seenPage[key] = map[string]bool{}
		}
		if !seenPage[key][path] {
			seenPage[key][path] = true
			idx.usage[key] = append(idx.usage[key], path)
		}
	}

	blockPaths := map[string][]string{}

	blockProject := map[string]string{}
	blockPathSeen := map[string]map[string]bool{}
	for i := range drafts {
		doc := drafts[i].DraftDocument
		if len(doc) == 0 {
			continue
		}
		page, perr := builder.ParsePage(doc)
		if perr != nil {

			continue
		}
		path := drafts[i].DraftPath
		if path == "" {
			path = drafts[i].ID
		}

		for _, cand := range builder.AppendSEOCandidates(page, builder.CollectContentCandidates(page)) {
			add(cand, path)
		}
		refs := builder.ReferencedBlockIDs(page.Root)

		bindings := page.Settings.Structure.SlotBindings()
		for _, slot := range builder.SortedSlots(bindings) {
			refs = append(refs, bindings[slot])
		}
		for _, blockID := range refs {
			if blockPathSeen[blockID] == nil {
				blockPathSeen[blockID] = map[string]bool{}
			}
			if blockPathSeen[blockID][path] {
				continue
			}
			blockPathSeen[blockID][path] = true
			blockPaths[blockID] = append(blockPaths[blockID], path)
			if _, ok := blockProject[blockID]; !ok {
				blockProject[blockID] = drafts[i].ProjectID
			}
		}
	}

	if h.blocks != nil && len(blockPaths) > 0 {
		cache := map[string]*builder.Page{}
		visited := map[string]bool{}
		queue := make([]string, 0, len(blockPaths))
		for blockID := range blockPaths {
			queue = append(queue, blockID)
		}
		sort.Strings(queue)
		for len(queue) > 0 {
			blockID := queue[0]
			queue = queue[1:]
			if visited[blockID] {
				continue
			}
			visited[blockID] = true
			paths := blockPaths[blockID]
			blockPage := h.blockPageOf(ctx, blockProject[blockID], blockID, cache)
			if blockPage == nil {
				continue
			}
			for _, cand := range builder.CollectContentCandidates(blockPage) {
				for _, path := range paths {
					add(cand, path)
				}
			}
			for _, nested := range builder.ReferencedBlockIDs(blockPage.Root) {
				if visited[nested] {
					continue
				}
				if blockPathSeen[nested] == nil {
					blockPathSeen[nested] = map[string]bool{}
				}
				for _, path := range paths {
					if blockPathSeen[nested][path] {
						continue
					}
					blockPathSeen[nested][path] = true
					blockPaths[nested] = append(blockPaths[nested], path)
				}
				queue = append(queue, nested)
			}
		}
	}
	sort.Strings(idx.keys)
	sort.Strings(idx.hashes)
	for key := range idx.usage {
		sort.Strings(idx.usage[key])
	}
	return idx, nil
}

func reuseCounts(paths []string, currentPath string) (others int, total int) {
	total = len(paths)
	if total <= 1 {
		return 0, total
	}
	others = total - 1
	if currentPath != "" {
		found := false
		for _, p := range paths {
			if p == currentPath {
				found = true
				break
			}
		}
		if !found {

			others = total
		}
	}
	return others, total
}

func (h *pagesAdminHandle) TranslationMissesPage(c *gin.Context) {

	projectID := strings.TrimSpace(c.Query("project"))
	rows := []pagedto.TranslationMissResp{}

	var errFact pageMsg
	if projectID == "" && h.projects != nil {
		if list, lerr := h.projects.List(c.Request.Context()); lerr == nil && len(list) == 1 {
			projectID = list[0].ID
		}
	}
	if projectID == "" {

		errFact = pageMsg{Key: pageenums.ErrProjectRequired,
			Fallback: "请先选择站点工程：缺译报告按工程统计"}
	} else if list, lerr := h.pages.UntranslatedPageLangs(c.Request.Context(), projectID); lerr != nil {
		errFact = pageErrMsg(c, lerr)
	} else {

		rows = append(rows, list...)
	}
	pageview.RenderTemplate(c, PageTranslationMissesTemplate, h.translationMissPageData(c, projectID, rows, errFact))
}

func (h *pagesAdminHandle) TranslationMissCancel(c *gin.Context) {
	req, ok := binding.Form[pagedto.PageLangFormReq](c)
	if !ok {
		return
	}
	pageID := strings.TrimSpace(req.PageID)
	lang := strings.TrimSpace(req.Lang)
	if _, err := h.pages.ExcludePageLang(c.Request.Context(), pageID, lang); err != nil {
		translationMissJump(c, false, pageErrMsg(c, err))
		return
	}
	translationMissJump(c, true, pageMsg{Key: pageenums.TranslationMissCanceled,
		Fallback: "已取消该语言：产物已下线，它也不再出现在切换器 / hreflang / sitemap 里"})
}

func (h *pagesAdminHandle) translationMissPageData(c *gin.Context, projectID string, rows []pagedto.TranslationMissResp, errFact pageMsg) gin.H {
	token, terr := builtin.GetCSRFToken(c)
	if terr != nil {
		token = ""
	}

	projects := []projectdto.ProjectResp{}
	if h.projects != nil {
		if list, lerr := h.projects.List(c.Request.Context()); lerr == nil {
			projects = list
		}
	}
	return adminui.PrepareBareView(c, &TranslationMissPageResp{
		CSRFToken:       token,
		Projects:        projects,
		SelectedProject: projectID,
		Rows:            rows,
		Err:             errFact,
	})
}

var translationMissBackKeys = []string{"project"}

const pageTranslationMissPath = "/admin/page-translation-misses"

func translationMissBack(c *gin.Context) string {
	return backref.BackPath(c, pageTranslationMissPath, translationMissBackKeys...)
}

var translationMissBackMsg = pageMsg{Key: pageenums.TranslationMissTitle, Fallback: "内容缺译报告"}

func translationMissJump(c *gin.Context, ok bool, msg pageMsg) {
	pageJump(c, ok, msg, translationMissBack(c), translationMissBackMsg)
}

func pageTranslationsBack(c *gin.Context, pageID, lang string) string {
	if back := translationBack(c); strings.Contains(back, "pageId=") {
		return back
	}
	back := pageTranslationsPath + "?pageId=" + url.QueryEscape(pageID)
	if lang != "" {
		back += "&lang=" + url.QueryEscape(lang)
	}
	return back
}
