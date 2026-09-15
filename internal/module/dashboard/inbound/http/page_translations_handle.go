package dashboardhttp

// page_translations_handle.go — 翻译工作台（多语言 P5c，docs/06-D §7.8 决策 F10/F11/F12）。
//
// 入口（决策 F10：不做独立菜单）：页面列表每行操作栏的「多语言」按钮 →
//   GET  /admin/page/translations?pageId=…&lang=…
//   POST /admin/page/translations/save（整表提交，PRG 回跳）
//
// 清单与构建期同源（P5b 注意点 1）：本页可翻译行 = 本页草稿文档候选（builder.CollectContentCandidates）
// + 本页引用块内候选（页眉/页脚绑定块与 core.globalref 内联块，见 page_translations_blocks.go），
// 与构建期取词用的是同一套函数与同一份 core.TranslatableFields 白名单；
// 工作台不另写扫描，因此不会出现「工作台能改、构建期不取」的漂移。
//
// 写入链路（P5b 注意点 2/3/5/6）：
//  1. 逐行 builder.ValidateContentTarget：白名单 / 跳过规则 / 字段长度上限 / 富文本形态；
//  2. pkg/i18n.ContentWriter.Upsert：重算校验 sha256(source_text) == source_hash
//     （066 的 CHECK 只校验格式），ON CONFLICT (source_hash, context, lang) 幂等更新，
//     engine = manual；
//  3. 只有「译文文本确实变化」才触发 page.MarkStaleForI18n 全站标记待重建
//     （编排在 handler，模式同 docs/06-D §15.10；仅改 engine 不触发，因为产物字节未变）。
//
// 跨页面复用与完成度（决策 F11/F12）：由 page_translations_index.go 的全站扫描提供。
//
// 本轮不做（严格边界）：AI 翻译（按钮灰置 + 接口形态预留）、CMS 字段（文章未实现；
// 商品域已由 issue #12 落地，见 product_translation_handle.go）、
// 译文删除（清空输入框 = 不写入，孤儿行清理见 docs/06-D §14 D14）。

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	dashboardenums "go_wp/internal/module/dashboard/enums"
	pagecontract "go_wp/internal/module/page/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

// contentTranslationPort 工作台使用的内容译文读写端口。
//
// 生产实现 = pkg/i18n.ContentWriter（sys_translation 表 + 默认数据库）；
// 测试经 SetContentTranslationStore 注入隔离 schema 的写入器。
//
// 工程作用域（审计 I18N-009）：工作台是**按站点**看译文的地方，读写都必须带工程 id ——
// 否则 A 站点保存的译法会盖掉 B 站点的（写入侧），或者看不到本工程自己的覆盖（读取侧）。
type contentTranslationPort interface {
	LoadDetails(ctx context.Context, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	LoadDetailsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error)
	LoadTargetsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]string, error)
	Upsert(ctx context.Context, items []i18n.ContentWriteItem) (written int, err error)
}

// contentPort 返回工作台的内容译文端口（未注入时用默认库）。
func (h *Handle) contentPort() (contentTranslationPort, error) {
	if h.contentStore != nil {
		return h.contentStore, nil
	}
	return i18n.NewContentWriterDefault()
}

// SetContentTranslationStore 注入内容译文端口（测试用；生产走默认库）。
func (h *Handle) SetContentTranslationStore(store contentTranslationPort) {
	h.contentStore = store
}

// translationMsgFallback 工作台提示的中文兜底。
//
// dashboard enums 常量是 sys_i18n 的 key（缺词条时由调用方给原文），工作台的
// 错误/提示直接渲染在页面上，因此在 handler 侧就翻好，模板不再二次取词。
var translationMsgFallback = map[string]string{
	dashboardenums.MsgTranslationSaveFailed:      "译文保存失败，请稍后重试",
	dashboardenums.MsgTranslationInvalid:         "提交数据不完整，请刷新页面后重试",
	dashboardenums.MsgTranslationStale:           "原文已变更，请刷新页面后重新翻译",
	dashboardenums.MsgTranslationLangInvalid:     "目标语言未启用，请先在站点设置里启用",
	dashboardenums.MsgTranslationDocInvalid:      "页面草稿无法解析，请先在工作台修复页面",
	dashboardenums.MsgTranslationSiteScanSkipped: "全站统计暂不可用，当前仅显示本页维度",
	dashboardenums.MsgTranslationSiteScanTooMany: "页面数超过全站扫描上限，当前仅显示本页维度",
}

// translationMsg 把 enums key 翻成当前语言；非 key（如 builder 校验的原始中文）原样返回。
func translationMsg(c *gin.Context, msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	return translateFor(c)(msg, translationMsgFallback[msg])
}

// translationMsgs 批量翻译提示文案。
func translationMsgs(c *gin.Context, msgs []string) []string {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if text := translationMsg(c, m); text != "" {
			out = append(out, text)
		}
	}
	return out
}

// translationLangOption 工作台语言下拉项。
type translationLangOption struct {
	Code   string
	Label  string
	Active bool
}

// translationRow 工作台一行（一个可翻译取值）。
type translationRow struct {
	Context    string
	Component  string
	Field      string
	Source     string
	SourceHash string
	Target     string
	Engine     string
	Translated bool
	Rich       bool
	Limit      int
	// ReusePages 除本页外还用到该 (原文, 语境) 的页面数（0 = 仅本页）。
	ReusePages int
	// ReuseTotal 该 (原文, 语境) 在全站出现的页面总数。
	ReuseTotal int
	// ReuseHint 展开提示：出现该文本的页面路径（最多 6 条）。
	ReuseHint string
	// Origin 来源标签（空 = 本页文档；非空 = 页眉块/页脚块/全局块，全站共享文本）。
	Origin string
}

// translationGroup 按组件分组的行集合。
type translationGroup struct {
	Component string
	Rows      []translationRow
}

// pageTranslationsData 工作台页面数据。
type pageTranslationsData struct {
	Title     string
	Menu      string
	PageID    string
	PagePath  string
	ProjectID string
	Lang      string
	Langs     []translationLangOption
	Filter    string
	Groups    []translationGroup
	RowCount  int
	PageDone  int
	PageTotal int
	SiteDone  int
	SiteTotal int
	SiteNote  string
	// IsDefaultLang 当前语言即站点默认语言：构建期不取内容译文（产物即原文）。
	IsDefaultLang bool
	Saved         bool
	SavedCount    int
	Errors        []string
}

// templateMap 转为模板所需的小写键 map（layout 以 {{.title}}/{{.menu}} 取值）。
func (d *pageTranslationsData) templateMap() gin.H {
	return gin.H{
		"title": d.Title, "menu": d.Menu,
		"PageID": d.PageID, "PagePath": d.PagePath, "ProjectID": d.ProjectID,
		"Lang": d.Lang, "Langs": d.Langs, "Filter": d.Filter, "Groups": d.Groups,
		"RowCount": d.RowCount, "PageDone": d.PageDone, "PageTotal": d.PageTotal,
		"SiteDone": d.SiteDone, "SiteTotal": d.SiteTotal, "SiteNote": d.SiteNote,
		"IsDefaultLang": d.IsDefaultLang, "Saved": d.Saved, "SavedCount": d.SavedCount,
		"Errors": d.Errors,
	}
}

// 筛选取值（服务端渲染，纯链接，零 JS）。
const (
	translationFilterAll     = "all"
	translationFilterMissing = "missing"
	translationFilterManual  = "manual"
	translationFilterAI      = "ai"
)

// PageTranslations GET /admin/page/translations：翻译工作台。
func (h *Handle) PageTranslations(c *gin.Context) {
	pageID := strings.TrimSpace(c.Query("pageId"))
	if pageID == "" || h.pages == nil {
		c.Redirect(http.StatusSeeOther, "/admin/pages")
		return
	}
	data, err := h.buildPageTranslationsData(c.Request.Context(), pageID,
		strings.TrimSpace(c.Query("lang")), strings.TrimSpace(c.Query("filter")))
	if err != nil {
		logger.Scene("page").With("pageId", pageID).Error(err, "打开翻译工作台失败")
		c.Redirect(http.StatusSeeOther, "/admin/pages")
		return
	}
	data.SiteNote = translationMsg(c, data.SiteNote)
	if saved := strings.TrimSpace(c.Query("saved")); saved == "1" {
		data.Saved = true
		data.SavedCount, _ = strconv.Atoi(strings.TrimSpace(c.Query("n")))
	}
	c.HTML(http.StatusOK, "admin/page_translations", withCSRF(c, data.templateMap()))
}

// SavePageTranslations POST /admin/page/translations/save：保存本页译文。
func (h *Handle) SavePageTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	if pageID == "" {
		c.String(http.StatusBadRequest, dashboardenums.MsgFieldRequired)
		return
	}
	if h.pages == nil {
		c.Redirect(http.StatusSeeOther, "/admin/pages")
		return
	}
	lang := strings.TrimSpace(c.PostForm("lang"))

	page, err := h.pageOf(c, pageID)
	if err != nil {
		logger.Scene("page").With("pageId", pageID).Error(err, "读取页面失败")
		c.Redirect(http.StatusSeeOther, "/admin/pages")
		return
	}
	if !h.langAllowed(ctx, page.ProjectID, lang) {
		h.renderTranslationError(c, pageID, lang, []string{dashboardenums.MsgTranslationLangInvalid})
		return
	}

	contexts := c.PostFormArray("rowContext")
	sources := c.PostFormArray("rowSource")
	hashes := c.PostFormArray("rowHash")
	targets := c.PostFormArray("rowTarget")
	if len(contexts) != len(sources) || len(contexts) != len(hashes) || len(contexts) != len(targets) {
		h.renderTranslationError(c, pageID, lang, []string{dashboardenums.MsgTranslationInvalid})
		return
	}

	// 第一步：逐行校验（全部通过才写库，避免「部分成功」的中间态）。
	var rowErrors []string
	items := make([]i18n.ContentWriteItem, 0, len(contexts))
	writeKeys := make([]string, 0, len(contexts))
	queued := make(map[string]bool, len(contexts))
	for i := range contexts {
		contextName := strings.TrimSpace(contexts[i])
		source := sources[i]
		target := strings.TrimSpace(targets[i])
		if target == "" {
			// 空输入 = 本行不写入（清空输入框不会删除库中已有译文，见文件头「本轮不做」）。
			continue
		}
		// 同一 (source_hash, context) 只允许入队一次：重复行会让 ON CONFLICT
		// 在单条 INSERT 内二次命中同一行（PostgreSQL 报错），故在此去重。
		dedupeKey := i18n.ContentIndexKey(i18n.ContentHash(source), contextName)
		if queued[dedupeKey] {
			continue
		}
		queued[dedupeKey] = true
		// 原文指纹必须与表单一致：不一致说明页面草稿已变或表单被篡改，
		// 让编辑者刷新后重试，绝不按旧指纹写入。
		if strings.TrimSpace(hashes[i]) != i18n.ContentHash(source) {
			rowErrors = append(rowErrors, contextName+"："+translationMsg(c, dashboardenums.MsgTranslationStale))
			continue
		}
		if verr := builder.ValidateContentTarget(contextName, source, target); verr != nil {
			rowErrors = append(rowErrors, contextName+"："+verr.Error())
			continue
		}
		items = append(items, i18n.ContentWriteItem{
			// 工程作用域（审计 I18N-009）：写入本工程自己的译文行，不污染其它站点。
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
		c.Redirect(http.StatusSeeOther, "/admin/page/translations?pageId="+pageID+"&lang="+lang+"&saved=1&n=0")
		return
	}

	port, perr := h.contentPort()
	if perr != nil {
		logger.Scene("page").With("pageId", pageID).Error(perr, "内容译文存储不可用")
		h.renderTranslationError(c, pageID, lang, []string{dashboardenums.MsgTranslationSaveFailed})
		return
	}

	// 第二步：变更判定。只有译文文本确实变化才写库并触发全站重建：
	// 原样再保存一次不产生任何写入（幂等），也不触发无意义的全站重建（§15.10 同一取舍）。
	// 变更判定按**本工程**读现有译文（工程行优先、回落全局行），否则别的站点的译法
	// 会被当成「已经是这个值」而跳过写入。
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
			continue // 完全没变化：不写、不触发
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
			h.renderTranslationError(c, pageID, lang, []string{dashboardenums.MsgTranslationSaveFailed})
			return
		}
	}

	// 第三步：译文文本变化 → 全站标记待重建（i18n:content 依赖条目配套，§9/§15.10）。
	// 失败只记日志：译文已落库，下次保存会重新判定并再试。
	if targetChanged && h.pages != nil {
		if merr := h.pages.MarkStaleForI18n(ctx); merr != nil {
			logger.Scene("page").With("pageId", pageID).Error(merr, "译文保存后标记全站待重建失败")
		}
	}
	c.Redirect(http.StatusSeeOther,
		"/admin/page/translations?pageId="+pageID+"&lang="+lang+"&saved=1&n="+strconv.Itoa(written))
}

// renderTranslationError 校验/写入失败：回渲染工作台（200）并展示逐行错误，不落库。
func (h *Handle) renderTranslationError(c *gin.Context, pageID, lang string, errs []string) {
	data, err := h.buildPageTranslationsData(c.Request.Context(), pageID, lang, "")
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/admin/pages")
		return
	}
	data.Errors = translationMsgs(c, errs)
	data.SiteNote = translationMsg(c, data.SiteNote)
	c.HTML(http.StatusOK, "admin/page_translations", withCSRF(c, data.templateMap()))
}

// buildPageTranslationsData 组装工作台数据。
//
// 步骤：页面草稿 → builder.CollectContentCandidates（与构建期同源）→
// 现有译文（含 engine）→ 全站索引（复用提示 + 全站完成度）→ 按组件分组 + 筛选。
func (h *Handle) buildPageTranslationsData(ctx context.Context, pageID, wantLang, filter string) (*pageTranslationsData, error) {
	// 这里没有 gin.Context（纯数据组装），因此就地解析一次工程 scope —— 与
	// Handle.pageOf 同一口径：Detail 的 projectID 是必填的越权防护 scope。
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
		Title: dashboardenums.MsgPageTranslationsTitle, Menu: "pages",
		PageID: page.ID, PagePath: page.DraftPath, ProjectID: page.ProjectID,
		Lang: lang, Filter: normalizeTranslationFilter(filter),
		IsDefaultLang: lang == defaultLang,
	}
	for _, code := range langs {
		data.Langs = append(data.Langs, translationLangOption{Code: code, Label: code, Active: code == lang})
	}

	parsed, perr := builder.ParsePage(page.DraftDocument)
	if perr != nil {
		data.Errors = append(data.Errors, dashboardenums.MsgTranslationDocInvalid)
		return data, nil
	}
	// 清单 = 本页文档候选 + 本页引用块（页眉/页脚绑定、core.globalref）内的候选。
	// 块内文本同样是本页产物的一部分（构建期装配内联），因此必须列在工作台里，
	// 否则它无法被翻译、完成度也会误报 100%（见 page_translations_blocks.go 文件头）。
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

	// 现有译文（P5a 读路径 + engine 投影）：读失败按「全部缺失」处理，页面照常可用。
	details := map[string]i18n.ContentTargetInfo{}
	if port, cerr := h.contentPort(); cerr == nil {
		if got, lerr := port.LoadDetailsForProject(ctx, page.ProjectID, lang, builder.ContentHashes(candidates)); lerr == nil {
			details = got
		} else {
			logger.Scene("page").With("pageId", pageID).Error(lerr, "读取现有译文失败")
		}
	}

	// 全站索引：跨页面复用提示 + 全站完成度分母（失败/超限则退化为本页维度）。
	site, serr := h.siteContentIndexOf(ctx)
	if serr != nil {
		logger.Scene("page").Error(serr, "全站翻译统计不可用，工作台退化为本页维度")
		site = nil
		data.SiteNote = dashboardenums.MsgTranslationSiteScanSkipped
	}
	if site != nil && site.skipped {
		site = nil
		data.SiteNote = dashboardenums.MsgTranslationSiteScanTooMany
	}
	if site != nil {
		data.SiteTotal = site.total()
		data.SiteDone = h.countTranslated(ctx, page.ProjectID, lang, site)
	}

	groups := make([]translationGroup, 0, 8)
	for _, cand := range candidates {
		component, field, ok := i18n.ParseContentContext(cand.Context)
		if !ok {
			continue
		}
		key := i18n.ContentIndexKey(i18n.ContentHash(cand.Source), cand.Context)
		meta, _ := core.TranslatableFieldMeta(component, field)
		limit, _ := builder.ContentTargetLimit(cand.Context)
		row := translationRow{
			Context: cand.Context, Component: component, Field: field,
			Source: cand.Source, SourceHash: i18n.ContentHash(cand.Source),
			Rich: meta.Rich(), Limit: limit,
		}
		// 来源标注：本页没有该 (原文, 语境) 时说明它来自哪个块（全站共享文本）。
		if !pageKeys[key] {
			row.Origin = blockInfo.origin[key]
		}
		if info, hit := details[key]; hit && info.TargetText != "" {
			row.Target = info.TargetText
			row.Engine = info.Engine
			row.Translated = true
		}
		if site != nil {
			row.ReusePages, row.ReuseTotal, row.ReuseHint = reuseHint(site.reusePathsOf(key), page.DraftPath)
		}
		if len(groups) == 0 || groups[len(groups)-1].Component != component {
			groups = append(groups, translationGroup{Component: component})
		}
		groups[len(groups)-1].Rows = append(groups[len(groups)-1].Rows, row)
	}

	// 完成度按「未筛选」的全量候选统计（筛选只影响展示）。
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

// countTranslated 统计全站已翻译条数（完成度分子）：只统计索引里确实用到的键。
//
// 按工程统计（审计 I18N-009）：本工程自己有译文的算已翻译，未覆盖时看到的是全局译文，
// 因此完成度反映的是「这个站点实际会渲染成什么」，而不是全库有没有这条译文。
func (h *Handle) countTranslated(ctx context.Context, projectID, lang string, site *siteContentIndex) int {
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

// filterTranslationGroups 按筛选条件裁剪行（空分组丢弃）。
func filterTranslationGroups(groups []translationGroup, filter string) []translationGroup {
	if filter == translationFilterAll {
		return groups
	}
	out := make([]translationGroup, 0, len(groups))
	for _, g := range groups {
		rows := make([]translationRow, 0, len(g.Rows))
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
			out = append(out, translationGroup{Component: g.Component, Rows: rows})
		}
	}
	return out
}

// normalizeTranslationFilter 归一筛选参数（非法值回退 all）。
func normalizeTranslationFilter(raw string) string {
	switch strings.TrimSpace(raw) {
	case translationFilterMissing, translationFilterManual, translationFilterAI:
		return strings.TrimSpace(raw)
	default:
		return translationFilterAll
	}
}

// enabledLangsOf 站点启用语言（默认语言在前；清单不可读时回退站点默认语言一种）。
func (h *Handle) enabledLangsOf(ctx context.Context, projectID string) []string {
	if h.projects != nil && strings.TrimSpace(projectID) != "" {
		if langs, err := h.projects.EnabledLangs(ctx, projectID); err == nil && len(langs) > 0 {
			return langs
		}
	}
	return []string{i18n.GetDefaultLang()}
}

// langAllowed 校验目标语言属于站点启用语言（禁止给未启用语言写译文）。
func (h *Handle) langAllowed(ctx context.Context, projectID, lang string) bool {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return false
	}
	return containsString(h.enabledLangsOf(ctx, projectID), lang)
}

// candidateHashes 写入项的 hash 去重集合（变更判定用）。
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

// containsString 判断切片是否含目标值。
func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
