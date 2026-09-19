package producthttp

import (
	"context"
	"sort"
	"strconv"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
)

// product_translation_data.go - 商品译文工作台的数据装配（候选分组、目标校验、哈希与工程/语言选项）。

// projectTranslationOption 工程下拉项。
type projectTranslationOption struct {
	ID       string
	Name     string
	Selected bool
}

// productTranslationRow 工作台一行（一个可翻译取值）。
type productTranslationRow struct {
	Context    string
	Field      string
	FieldLabel string
	Source     string
	SourceHash string
	Target     string
	Engine     string
	Translated bool
	// Rich 富文本 / 多段文本字段（描述、属性值数组）：译文需与原文同形。
	Rich bool
}

// productTranslationGroup 按实体分组的行集合。
type productTranslationGroup struct {
	Key string
	// EntityType / EntityID 该组对应的实体（译文保存后据此精确标记自动发布实例）。
	EntityType string
	EntityID   string
	Title      string
	Subtitle   string
	Rows       []productTranslationRow
}

// productTranslationsData 工作台页面数据。
type productTranslationsData struct {
	Title     string
	Menu      string
	ProjectID string
	ProductID string
	Lang      string
	Langs     []translationLangOption
	// SourceLang 站点源语言（= 站点默认语言）；判定依据见 sourceLangOf。
	SourceLang string
	// NoTargetLang 没有任何「非源语言」的启用语言：没有可翻译的目标，页面给可行动空态。
	NoTargetLang   bool
	Groups         []productTranslationGroup
	RowCount       int
	Done           int
	Total          int
	Saved          bool
	SavedNote      string
	Errors         []string
	ProjectOptions []projectTranslationOption
}

// templateMap 转为模板所需的小写键 map（layout 以 {{.title}}/{{.menu}} 取值）。
func (d *productTranslationsData) templateMap() gin.H {
	return gin.H{
		"title": d.Title, "menu": d.Menu,
		"ProjectID": d.ProjectID, "ProductID": d.ProductID,
		"Lang": d.Lang, "Langs": d.Langs, "Groups": d.Groups,
		"SourceLang": d.SourceLang, "NoTargetLang": d.NoTargetLang,
		"RowCount": d.RowCount, "Done": d.Done, "Total": d.Total,
		"Saved": d.Saved, "SavedNote": d.SavedNote, "Errors": d.Errors,
		"ProjectOptions": d.ProjectOptions,
	}
}

// translationAffectedEntities 本次写入的译文对应的实体（按 (指纹, 语境) 回查候选行）。
func (d *productTranslationsData) translationAffectedEntities(changedKeys []string) []translationEntityRef {
	byCandidate := map[string][]translationEntityRef{}
	for _, g := range d.Groups {
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

// translationLocation 保存后的回跳地址（PRG）。
func translationLocation(projectID, productID, lang string, written int) string {
	q := "/admin/products/translations?project=" + projectID + "&lang=" + lang + "&saved=1&n=" + strconv.Itoa(written)
	if productID != "" {
		q += "&product=" + productID
	}
	return q
}

// sourceCandidate 工作台校验用的一行原文。
type sourceCandidate struct {
	SourceText string
	SourceHash string
	Rich       bool
}

// sourceOf 按语境 + hash 反查本次提交对应的原文（防表单被裁剪/篡改）。
func (d *productTranslationsData) sourceOf(contextName, hash string) (sourceCandidate, bool) {
	for _, g := range d.Groups {
		for _, r := range g.Rows {
			if r.Context == contextName && r.SourceHash == hash {
				return sourceCandidate{SourceText: r.Source, SourceHash: r.SourceHash, Rich: r.Rich}, true
			}
		}
	}
	return sourceCandidate{}, false
}

// validateProductTarget 校验一条商品域译文的可写性（与构建期同源）。
//
// 三项（全部与构建期一致）：
//  1. 语境必须是本模块契约声明的可翻译字段（"实体类型.字段名"）；
//  2. 原文参与翻译（i18n.ShouldTranslateContent，纯数字/纯符号/空白被拒）；
//  3. 富文本字段的译文形态与原文一致（是否含 HTML 标签必须相同）。
func validateProductTarget(contextName string, source sourceCandidate, target string) string {
	entityType, field, ok := parseContextField(contextName)
	if !ok || !productcontract.IsTranslatableField(entityType, field) {
		return "语境非法：不是商品域的可翻译字段"
	}
	if !i18n.ShouldTranslateContent(source.SourceText) {
		return "原文不参与翻译（空串、纯数字或纯符号）"
	}
	if strings.TrimSpace(target) == "" {
		return "译文不能为空"
	}
	if source.Rich && hasMarkup(source.SourceText) != hasMarkup(target) {
		return "译文形态与原文不一致：原文含 HTML 标签时译文也必须含标签"
	}
	return ""
}

// hasMarkup 是否含 HTML 标签（与构建期 core.HasRichMarkup 同一判据的轻量版）。
func hasMarkup(s string) bool {
	return strings.Contains(s, "<") && strings.Contains(s, ">")
}

// parseContextField 拆语境 "实体类型.字段名"（按最后一个点拆）。
func parseContextField(contextName string) (entityType, field string, ok bool) {
	contextName = strings.TrimSpace(contextName)
	i := strings.LastIndex(contextName, ".")
	if i <= 0 || i == len(contextName)-1 {
		return "", "", false
	}
	return contextName[:i], contextName[i+1:], true
}

// build 组装工作台数据（工程 + 商品 + 语言 + 候选 + 现有译文）。
func (h *productTranslationHandle) build(ctx context.Context, projectID, productID, wantLang string) (data *productTranslationsData, err error) {
	data = &productTranslationsData{Title: "商品多语言", Menu: "products", ProjectID: projectID, ProductID: productID}

	options, projectIDs, oerr := h.projectOptions(ctx, projectID)
	if oerr != nil {
		return nil, oerr
	}
	data.ProjectOptions = options
	if data.ProjectID == "" && len(projectIDs) > 0 {
		data.ProjectID = projectIDs[0]
	}

	// 目标语言 = 站点启用语言剔除源语言（同语言互译没有意义）。
	// 一个都不剩时 data.Langs 为空 = NoTargetLang，页面走可行动空态；这里**不能**再
	// 退回「默认选中源语言」——那等于把 langs[0] 选中、翻了个寂寞，也看不出问题在哪。
	sourceLang := h.sourceLangOf(ctx, data.ProjectID)
	targetLangs := targetLangsOf(h.enabledLangsOf(ctx, data.ProjectID), sourceLang)
	data.SourceLang = sourceLang
	data.NoTargetLang = len(targetLangs) == 0
	data.Lang = strings.TrimSpace(wantLang)
	if !containsString(targetLangs, data.Lang) {
		data.Lang = ""
		if len(targetLangs) > 0 {
			data.Lang = targetLangs[0]
		}
	}
	data.Langs = make([]translationLangOption, 0, len(targetLangs))
	for _, code := range targetLangs {
		data.Langs = append(data.Langs, translationLangOption{Code: code, Label: code, Active: code == data.Lang})
	}

	// 商品域候选：指定商品时只列该商品的（含引用实体），否则列整个工程。
	var cands []productcontract.TranslationCandidate
	if productID != "" {
		if cands, err = h.products.ProductTranslationCandidates(ctx, productID); err != nil {
			return nil, err
		}
	} else if data.ProjectID != "" {
		if cands, err = h.products.ProjectTranslationCandidates(ctx, data.ProjectID); err != nil {
			return nil, err
		}
	}

	// 现有译文（含 engine，用于徽章与「是否变化」判定）。
	// 没有目标语言时不查译文：空语言码查不出任何东西，白跑一次库。
	targets := map[string]i18n.ContentTargetInfo{}
	if data.Lang != "" && len(cands) > 0 {
		if port, perr := h.port(); perr == nil {
			if got, lerr := port.LoadDetailsForProject(ctx, data.ProjectID, data.Lang, candidateHashesOf(cands)); lerr == nil {
				targets = got
			}
		}
	}

	data.Groups = groupCandidates(cands, targets)
	for _, g := range data.Groups {
		for _, r := range g.Rows {
			data.Total++
			if r.Translated {
				data.Done++
			}
		}
		data.RowCount += len(g.Rows)
	}
	return data, nil
}

// groupCandidates 按实体分组并挂上现有译文。
func groupCandidates(cands []productcontract.TranslationCandidate, targets map[string]i18n.ContentTargetInfo) []productTranslationGroup {
	order := make([]string, 0, len(cands))
	index := map[string]*productTranslationGroup{}
	for _, c := range cands {
		key := c.EntityType + "/" + c.EntityID
		g, ok := index[key]
		if !ok {
			g = &productTranslationGroup{
				Key:        key,
				EntityType: c.EntityType,
				EntityID:   c.EntityID,
				Title:      productcontract.EntityTypeLabel(c.EntityType) + " · " + c.EntityName,
				Subtitle:   c.EntityType,
			}
			index[key] = g
			order = append(order, key)
		}
		info := targets[i18n.ContentIndexKey(c.SourceHash, c.Context)]
		g.Rows = append(g.Rows, productTranslationRow{
			Context: c.Context, Field: c.Field, FieldLabel: c.FieldLabel,
			Source: c.SourceText, SourceHash: c.SourceHash,
			Target: info.TargetText, Engine: info.Engine,
			Translated: info.TargetText != "",
			Rich:       c.Field == "description",
		})
	}
	out := make([]productTranslationGroup, 0, len(order))
	for _, key := range order {
		out = append(out, *index[key])
	}
	return out
}

// candidateHashesOf 候选的 hash 去重集合（现有译文查询用）。
func candidateHashesOf(cands []productcontract.TranslationCandidate) []string {
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

// productWriteHashes 写入项的 hash 去重集合（变更判定用；与页面工作台的同名
// 辅助函数区分：本函数只服务商品域写入路径）。
func productWriteHashes(items []i18n.ContentWriteItem) []string {
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

// projectOptions 工程下拉（返回选项与工程 id 顺序；未指定时回退第一个工程）。
func (h *productTranslationHandle) projectOptions(ctx context.Context, want string) (options []projectTranslationOption, ids []string, err error) {
	if h.projects == nil {
		return nil, nil, nil
	}
	list, lerr := h.projects.List(ctx)
	if lerr != nil {
		return nil, nil, lerr
	}
	selected := strings.TrimSpace(want)
	if selected == "" && len(list) > 0 {
		selected = list[0].ID
	}
	for _, p := range list {
		ids = append(ids, p.ID)
		options = append(options, projectTranslationOption{ID: p.ID, Name: p.Name, Selected: p.ID == selected})
	}
	return options, ids, nil
}

// sourceLangOf 站点源语言。
//
// 判定依据（与构建期同一来源，见 pipeline.DefaultLocale）：project_locales 里 is_default
// 的那一行；清单缺失、没有默认标记或表不可读时，project 契约自身回退 i18n.default_lang
// （见 internal/module/project/service/locale_service.go 的 DefaultLocale），这里再加一层
// 兜底保证源语言永远非空。
// 商品文案（商品名 / 描述 / 分类名 / 品牌名 …）存的都是源语言原文，译文只对**非源语言**
// 有意义，所以翻译目标一律从启用语言里剔掉它。
func (h *productTranslationHandle) sourceLangOf(ctx context.Context, projectID string) string {
	if h.projects != nil && strings.TrimSpace(projectID) != "" {
		if lang, err := h.projects.DefaultLocale(ctx, projectID); err == nil && strings.TrimSpace(lang) != "" {
			return strings.TrimSpace(lang)
		}
	}
	return i18n.GetDefaultLang()
}

// targetLangsOf 启用语言剔除源语言后剩下的翻译目标（保持清单原顺序，
// 因此「默认语言在前」时第一个元素就是默认选中的目标语言）。
func targetLangsOf(langs []string, sourceLang string) []string {
	out := make([]string, 0, len(langs))
	for _, code := range langs {
		if code == sourceLang {
			continue
		}
		out = append(out, code)
	}
	return out
}

// enabledLangsOf 站点启用语言（默认语言在前；清单不可读时回退站点默认语言一种）。
func (h *productTranslationHandle) enabledLangsOf(ctx context.Context, projectID string) []string {
	if h.projects != nil && strings.TrimSpace(projectID) != "" {
		if langs, err := h.projects.EnabledLangs(ctx, projectID); err == nil && len(langs) > 0 {
			return langs
		}
	}
	return []string{i18n.GetDefaultLang()}
}

// langAllowed 目标语言是否属于站点启用语言。
//
// 判据只要求「在启用清单里」，**不额外排除源语言**：页面下拉已经不列源语言、默认目标也
// 不会落在它上面（见 build），但写入侧保留按源语言落库的兼容路径 —— 自动发布实例是逐
// 启用语言构建的（presentation.publishAllLangs），源语言那一份产物同样读 sys_translation，
// 收紧判据会让这条链路静默失效。
func (h *productTranslationHandle) langAllowed(ctx context.Context, projectID, lang string) bool {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return false
	}
	return containsString(h.enabledLangsOf(ctx, projectID), lang)
}
