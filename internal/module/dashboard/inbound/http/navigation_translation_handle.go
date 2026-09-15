// navigation_translation_handle.go — 导航标签译文工作台（审计 I18N-007）。
//
// 为什么导航标签需要**自己**的维护入口：它的 label 不在页面文档里，而在 navigation
// 模块的节点上 —— 页面翻译工作台看不到它（Props 里根本没有这个值），组件侧声明的
// Translatable 白名单对它也无效。构建期由 pipeline.NavigationAdapter 用
// navigation.label 语境回填译文（I18N-018 的定案），本页是那个语境的唯一维护入口。
//
// 两项与其它工作台不同的地方：
//
//	· 语境恒为 navigation.label，不需要从字段推导 —— 菜单项只有一个可见文本；
//	· 节点按位置（header / footer）组织，标题取自节点自身（与菜单管理页同一视图）。
package dashboardhttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// navigationTranslationRow 一个菜单项的标签翻译行。
type navigationTranslationRow struct {
	Context    string
	NodeID     string
	Kind       string
	Path       string
	Source     string
	SourceHash string
	Target     string
	Translated bool
}

// navigationTranslationGroup 按位置（header / footer）分组。
type navigationTranslationGroup struct {
	Kind  string
	Title string
	Rows  []navigationTranslationRow
}

// navigationTranslationHandle 导航译文工作台。
type navigationTranslationHandle struct {
	navigations navigationcontract.NavigationService
	projects    projectcontract.ProjectService
	writer      *i18n.ContentWriter
	// pages 译文变更后标记手工页面待重建（与商品 / 文章工作台同一动作）。
	// 菜单文字出现在**每个页面**上，所以这里没有「精确到某页」的选项 ——
	// 按 i18n:content 依赖条目做全站标记，与页面翻译工作台同一链路。
	pages navTranslationPageMarker
}

// navTranslationPageMarker 页面侧的最小失效端口（消费者侧定义，跨模块只依赖这条）。
type navTranslationPageMarker interface {
	MarkStaleForI18n(ctx context.Context) error
}

// SetPageMarker 注入页面失效端口（装配期）。
func (h *navigationTranslationHandle) SetPageMarker(m navTranslationPageMarker) { h.pages = m }

// NewNavigationTranslationHandle 构造。
func NewNavigationTranslationHandle(navigations navigationcontract.NavigationService,
	projects projectcontract.ProjectService) *navigationTranslationHandle {
	return &navigationTranslationHandle{navigations: navigations, projects: projects}
}

// SetContentWriter 注入译文写入端口（装配期）。
func (h *navigationTranslationHandle) SetContentWriter(w *i18n.ContentWriter) { h.writer = w }

// navigationTranslationContext 菜单标签的固定语境（与构建期 resolver 逐字一致）。
func navigationTranslationContext() string {
	return i18n.ContentContext("navigation", "label")
}

// NavigationTranslations GET /admin/navigations/translations。
func (h *navigationTranslationHandle) NavigationTranslations(c *gin.Context) {
	projectID := strings.TrimSpace(c.Query("project"))
	lang := strings.TrimSpace(c.Query("lang"))
	data := h.build(c.Request.Context(), projectID, lang)
	if strings.TrimSpace(c.Query("saved")) == "1" {
		n, _ := strconv.Atoi(strings.TrimSpace(c.Query("n")))
		data.Saved = true
		if n > 0 {
			data.SavedNote = "已保存 " + strconv.Itoa(n) + " 条译文；已标记受影响页面待重建（下次构建生效）。"
		} else {
			data.SavedNote = "没有需要写入的变化。"
		}
	}
	c.HTML(http.StatusOK, "admin/navigation_translations.html", withCSRF(c, data.templateMap()))
}

// SaveNavigationTranslations POST /admin/navigations/translations/save（整表提交）。
func (h *navigationTranslationHandle) SaveNavigationTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.PostForm("project"))
	lang := strings.TrimSpace(c.PostForm("lang"))
	data := h.build(ctx, projectID, lang)

	contexts := c.PostFormArray("rowContext")
	hashes := c.PostFormArray("rowHash")
	targets := c.PostFormArray("rowTarget")
	if len(contexts) != len(hashes) || len(contexts) != len(targets) {
		data.Errors = []string{"提交的行数不一致，请刷新后重试"}
		c.HTML(http.StatusOK, "admin/navigation_translations.html", withCSRF(c, data.templateMap()))
		return
	}
	if h.writer == nil {
		data.Errors = []string{"译文存储不可用"}
		c.HTML(http.StatusOK, "admin/navigation_translations.html", withCSRF(c, data.templateMap()))
		return
	}
	var rowErrors []string
	items := make([]i18n.ContentWriteItem, 0, len(contexts))
	keys := make([]string, 0, len(contexts))
	queued := map[string]bool{}
	for i := range contexts {
		contextName := strings.TrimSpace(contexts[i])
		if contextName != navigationTranslationContext() {
			rowErrors = append(rowErrors, "语境非法：导航标签只接受 "+navigationTranslationContext())
			continue
		}
		source, ok := data.sourceOf(hashes[i])
		if !ok {
			rowErrors = append(rowErrors, "菜单文字已变化，请刷新后重试")
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
		if !i18n.ShouldTranslateContent(source.Source) {
			rowErrors = append(rowErrors, source.Source+"：该菜单文字不参与翻译（纯数字或纯符号）")
			continue
		}
		items = append(items, i18n.ContentWriteItem{
			SourceHash: source.SourceHash, Context: contextName, Lang: lang,
			SourceText: source.Source, TargetText: target, Engine: i18n.ContentEngineManual,
		})
		keys = append(keys, key)
	}
	if len(rowErrors) > 0 {
		data.Errors = rowErrors
		c.HTML(http.StatusOK, "admin/navigation_translations.html", withCSRF(c, data.templateMap()))
		return
	}
	if len(items) == 0 {
		c.Redirect(http.StatusSeeOther, navigationTranslationLocation(projectID, lang, 0))
		return
	}
	hashesAll := make([]string, 0, len(items))
	for _, it := range items {
		hashesAll = append(hashesAll, it.SourceHash)
	}
	before, berr := h.writer.LoadDetails(ctx, lang, hashesAll)
	if berr != nil {
		logger.Scene("dashboard").With("lang", lang).Error(berr, "读取现有导航译文失败，按全部变更处理")
		before = map[string]i18n.ContentTargetInfo{}
	}
	pending := make([]i18n.ContentWriteItem, 0, len(items))
	for i, item := range items {
		prev := before[keys[i]]
		if prev.TargetText == item.TargetText && prev.Engine == i18n.ContentEngineManual {
			continue
		}
		pending = append(pending, item)
	}
	written := 0
	if len(pending) > 0 {
		var uerr error
		written, uerr = h.writer.Upsert(ctx, pending)
		if uerr != nil {
			logger.Scene("dashboard").With("lang", lang).Error(uerr, "写入导航译文失败")
			data.Errors = []string{"保存失败：" + uerr.Error()}
			c.HTML(http.StatusOK, "admin/navigation_translations.html", withCSRF(c, data.templateMap()))
			return
		}
		// 译文已落库 → 标记页面待重建。失败只记日志：译文本身已经写好了，
		// 标记失败只影响「下次构建会不会主动带上这页」，不该让运营以为保存失败。
		if h.pages != nil {
			if merr := h.pages.MarkStaleForI18n(ctx); merr != nil {
				logger.Scene("dashboard").With("lang", lang).Error(merr, "导航译文保存后标记页面待重建失败")
			}
		}
	}
	c.Redirect(http.StatusSeeOther, navigationTranslationLocation(projectID, lang, written))
}

// navigationTranslationsData 页面数据。
type navigationTranslationsData struct {
	Title     string
	Menu      string
	ProjectID string
	Lang      string
	Langs     []translationLangOption
	Groups    []navigationTranslationGroup
	RowCount  int
	Done      int
	Saved     bool
	SavedNote string
	Errors    []string
}

// templateMap 转模板键 map。
func (d *navigationTranslationsData) templateMap() gin.H {
	return gin.H{
		"title": d.Title, "menu": d.Menu,
		"ProjectID": d.ProjectID, "Lang": d.Lang, "Langs": d.Langs,
		"Groups": d.Groups, "RowCount": d.RowCount, "Done": d.Done,
		"Saved": d.Saved, "SavedNote": d.SavedNote, "Errors": d.Errors,
	}
}

// sourceOf 按原文哈希反查本次提交对应的菜单项（防表单被裁剪）。
func (d *navigationTranslationsData) sourceOf(hash string) (navigationTranslationRow, bool) {
	for _, g := range d.Groups {
		for _, r := range g.Rows {
			if r.SourceHash == hash {
				return r, true
			}
		}
	}
	return navigationTranslationRow{}, false
}

// navigationTranslationLocation 保存后的回跳地址（PRG）。
func navigationTranslationLocation(projectID, lang string, written int) string {
	q := "/admin/navigations/translations?lang=" + lang + "&saved=1&n=" + strconv.Itoa(written)
	if projectID != "" {
		q += "&project=" + projectID
	}
	return q
}

// build 组装工作台数据：两个位置的全部菜单项 → 现有译文。
func (h *navigationTranslationHandle) build(ctx context.Context, projectID, lang string) *navigationTranslationsData {
	data := &navigationTranslationsData{
		Title: "导航译文", Menu: "navigation-translations",
		ProjectID: projectID, Lang: lang,
		Groups: []navigationTranslationGroup{},
	}
	for _, code := range i18n.AvailableLangs() {
		data.Langs = append(data.Langs, translationLangOption{Code: code, Label: code, Active: code == lang})
	}
	if lang == "" || h.navigations == nil {
		return data
	}
	// 没指定工程时取第一个（与菜单管理页同一默认规则：单工程站点不必先选）。
	if projectID == "" && h.projects != nil {
		if projects, perr := h.projects.List(ctx); perr == nil && len(projects) > 0 {
			projectID = projects[0].ID
			data.ProjectID = projectID
		}
	}
	if projectID == "" {
		return data
	}
	seen := map[string]bool{}
	hashes := make([]string, 0, 8)
	type rowRef struct {
		groupIdx int
		rowIdx   int
		hash     string
	}
	var refs []rowRef
	for _, kind := range []string{"header", "footer"} {
		nodes, terr := h.navigations.Tree(ctx, projectID, kind)
		if terr != nil {
			logger.Scene("dashboard").With("kind", kind).Error(terr, "读取导航树失败")
			continue
		}
		group := navigationTranslationGroup{Kind: kind, Title: navKindTitle(kind), Rows: []navigationTranslationRow{}}
		var walk func(list []*navigationdto.NavigationNode)
		walk = func(list []*navigationdto.NavigationNode) {
			for _, n := range list {
				if n == nil {
					continue
				}
				src := strings.TrimSpace(n.Title)
				if src != "" && i18n.ShouldTranslateContent(src) {
					h := i18n.ContentHash(src)
					// 同一段文字在多个菜单项里出现时只保留一行（译文按原文寻址，
					// 列出多行会让「改哪一行才算数」变得不清楚）。
					if !seen[h] {
						seen[h] = true
						hashes = append(hashes, h)
						refs = append(refs, rowRef{groupIdx: len(data.Groups), rowIdx: len(group.Rows), hash: h})
						group.Rows = append(group.Rows, navigationTranslationRow{
							Context: navigationTranslationContext(), NodeID: n.ID, Kind: kind,
							Path: n.Path, Source: src, SourceHash: h,
						})
					}
				}
				walk(n.Children)
			}
		}
		walk(nodes)
		data.RowCount += len(group.Rows)
		data.Groups = append(data.Groups, group)
	}
	if h.writer != nil && len(hashes) > 0 {
		targets, lerr := h.writer.LoadTargets(ctx, lang, hashes)
		if lerr != nil {
			logger.Scene("dashboard").With("lang", lang).Error(lerr, "读取现有导航译文失败")
		} else {
			for _, r := range refs {
				if t, ok := targets[i18n.ContentIndexKey(r.hash, navigationTranslationContext())]; ok && t != "" {
					data.Groups[r.groupIdx].Rows[r.rowIdx].Target = t
					data.Groups[r.groupIdx].Rows[r.rowIdx].Translated = true
					data.Done++
				}
			}
		}
	}
	return data
}

// navKindTitle 菜单位置的中文名。
func navKindTitle(kind string) string {
	if kind == "footer" {
		return "页脚导航"
	}
	return "页眉导航"
}
