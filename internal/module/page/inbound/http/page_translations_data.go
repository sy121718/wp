package pagehttp

// page_translations_data.go - 页面译文工作台的数据装配（工程量、行分组、筛选与哈希对比）。

import (
	"context"
	"sort"
	"strings"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	pagecontract "go_wp/internal/module/page/contract"
	pageenums "go_wp/internal/module/page/enums"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// buildPageTranslationsData 组装工作台数据。
//
// 步骤：页面草稿 → builder.CollectContentCandidates（与构建期同源）→
// 现有译文（含 engine）→ 全站索引（复用提示 + 全站完成度）→ 按组件分组 + 筛选。
func (h *pagesAdminHandle) buildPageTranslationsData(ctx context.Context, pageID, wantLang, filter string) (*pageTranslationsData, error) {
	// 这里没有 gin.Context（纯数据组装），因此就地解析一次工程 scope —— 与
	// pageOf 同一口径：Detail 的 projectID 是必填的越权防护 scope。
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
	for _, code := range langs {
		data.Langs = append(data.Langs, translationLangOption{Code: code, Label: code, Active: code == lang})
	}

	parsed, perr := builder.ParsePage(page.DraftDocument)
	if perr != nil {
		data.Errors = append(data.Errors, pageenums.MsgTranslationDocInvalid)
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
		data.SiteNote = pageenums.MsgTranslationSiteScanSkipped
	}
	if site != nil && site.skipped {
		site = nil
		data.SiteNote = pageenums.MsgTranslationSiteScanTooMany
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
func (h *pagesAdminHandle) enabledLangsOf(ctx context.Context, projectID string) []string {
	if h.projects != nil && strings.TrimSpace(projectID) != "" {
		if langs, err := h.projects.EnabledLangs(ctx, projectID); err == nil && len(langs) > 0 {
			return langs
		}
	}
	return []string{i18n.GetDefaultLang()}
}

// langAllowed 校验目标语言属于站点启用语言（禁止给未启用语言写译文）。
func (h *pagesAdminHandle) langAllowed(ctx context.Context, projectID, lang string) bool {
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
