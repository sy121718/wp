package navigationhttp

// navigation_translation_handle.go — 导航标签译文工作台（审计 I18N-007）。
// 自 dashboard 迁回本模块。
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

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// translationLangOption 工作台语言下拉项（跨包引私有符号不成立，各页面包各自持有一份）。
type translationLangOption struct {
	Code   string
	Label  string
	Active bool
}

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
	// menus 译文变更后派发导航依赖失效（自动发布实例侧）。
	//
	// 只标页面是不够的：菜单文字同样烘在 presentation 实例的产物里，那些实例
	// 没有 i18n:content 全站标记的落点，会永远停在旧标签上（与刚修好的 menu 依赖
	// 缺口同源）。派发口径与导航项增删改完全一致（menu:{projectID}:{kind}），
	// 这里只按位置发起，键构造与扇出都在发布内核，不自创第三套。
	menus navTranslationMenuInvalidator
}

// navTranslationPageMarker 页面侧的最小失效端口（消费者侧定义，跨模块只依赖这条）。
type navTranslationPageMarker interface {
	MarkStaleForI18n(ctx context.Context) error
}

// navTranslationMenuInvalidator 导航依赖的最小失效端口（消费者侧定义）。
//
// 实现方是 navigation 模块自己的 Service —— 它持有 SetMenuStaleDispatcher 注入的
// 派发端口，所以「改菜单项」与「改菜单文字」走的是同一段失效逻辑、同一条装配链。
type navTranslationMenuInvalidator interface {
	InvalidateMenuLabels(ctx context.Context, projectID string, kinds []string)
}

// SetPageMarker 注入页面失效端口（装配期）。
func (h *navigationTranslationHandle) SetPageMarker(m navTranslationPageMarker) { h.pages = m }

// SetMenuInvalidator 注入导航依赖失效端口（装配期）。
func (h *navigationTranslationHandle) SetMenuInvalidator(m navTranslationMenuInvalidator) {
	h.menus = m
}

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
	c.HTML(http.StatusOK, "admin/navigation/navigation_translations.html", shell.Prepare(c, data.templateMap()))
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
		c.HTML(http.StatusOK, "admin/navigation/navigation_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}
	if h.writer == nil {
		data.Errors = []string{"译文存储不可用"}
		c.HTML(http.StatusOK, "admin/navigation/navigation_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}
	var rowErrors []string
	items := make([]i18n.ContentWriteItem, 0, len(contexts))
	keys := make([]string, 0, len(contexts))
	// kinds 与 items / keys 同下标：这条译文来自被提交的那一行所在的菜单位置。
	// 跨位置的同名文字在列表里只出现一行（见 build 的去重），因此它只是**回退值** ——
	// 权威的「哪些位置受影响」按原文哈希回查（见 kindsForHashes）。
	kinds := make([]string, 0, len(contexts))
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
		kinds = append(kinds, source.Kind)
	}
	if len(rowErrors) > 0 {
		data.Errors = rowErrors
		c.HTML(http.StatusOK, "admin/navigation/navigation_translations.html", shell.Prepare(c, data.templateMap()))
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
		logger.Scene("navigation").With("lang", lang).Error(berr, "读取现有导航译文失败，按全部变更处理")
		before = map[string]i18n.ContentTargetInfo{}
	}
	pending := make([]i18n.ContentWriteItem, 0, len(items))
	// 只有**真的变了**的译文才派发失效：没变化的提交不该把产物标一遍 stale
	//（那会让下一次构建白跑，且查不出是谁改的 —— 与 invalidateMenu 的注释同一理由）。
	changedHashes := map[string]bool{}
	fallbackKinds := make([]string, 0, 2)
	seenKind := map[string]bool{}
	for i, item := range items {
		prev := before[keys[i]]
		if prev.TargetText == item.TargetText && prev.Engine == i18n.ContentEngineManual {
			continue
		}
		pending = append(pending, item)
		changedHashes[item.SourceHash] = true
		if k := strings.TrimSpace(kinds[i]); k != "" && !seenKind[k] {
			seenKind[k] = true
			fallbackKinds = append(fallbackKinds, k)
		}
	}
	changedKinds := data.kindsForHashes(changedHashes, fallbackKinds)
	written := 0
	if len(pending) > 0 {
		var uerr error
		written, uerr = h.writer.Upsert(ctx, pending)
		if uerr != nil {
			logger.Scene("navigation").With("lang", lang).Error(uerr, "写入导航译文失败")
			// 归口文案：命中 enums 白名单的业务文案原样透出，其余（数据库原文：
			// 表名 / 约束名 / SQLSTATE）只进上面那条日志，页面拿归口提示。
			data.Errors = []string{"保存失败：" + navigationErrPageText(c, uerr)}
			c.HTML(http.StatusOK, "admin/navigation/navigation_translations.html", shell.Prepare(c, data.templateMap()))
			return
		}
		// 译文已落库 → 标记待重建。失败只记日志：译文本身已经写好了，
		// 标记失败只影响「下次构建会不会主动带上这些页」，不该让运营以为保存失败。
		if h.pages != nil {
			if merr := h.pages.MarkStaleForI18n(ctx); merr != nil {
				logger.Scene("navigation").With("lang", lang).Error(merr, "导航译文保存后标记页面待重建失败")
			}
		}
		// 自动发布实例侧同批派发（menu:{projectID}:{kind}）：只标手工页面会让
		// 详情页 / 产品页停在旧菜单标签上，而线上与库里都看不出任何异常。
		if h.menus != nil && len(changedKinds) > 0 {
			h.menus.InvalidateMenuLabels(ctx, scopeProjectID(data, projectID), changedKinds)
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
	// kindsByHash 本次渲染中「原文哈希 → 出现过该文字的全部菜单位置」。
	//
	// 与 Groups 的差别：Groups 按原文去重（同一段文字在页眉与页脚都出现时只列一行，
	// 因为译文按 (原文, 语境) 寻址，列两行反而不知道改哪一行才算数），这份记录**不去重** ——
	// 保存译文后要按它派发每个位置的失效，只看被提交那一行会漏掉另一个位置。
	kindsByHash map[string][]string
}

// noteHashKind 记下「某个原文出现在某个菜单位置」（不受列表去重影响）。
func (d *navigationTranslationsData) noteHashKind(hash, kind string) {
	if d == nil || hash == "" || kind == "" {
		return
	}
	if d.kindsByHash == nil {
		d.kindsByHash = map[string][]string{}
	}
	for _, k := range d.kindsByHash[hash] {
		if k == kind {
			return
		}
	}
	d.kindsByHash[hash] = append(d.kindsByHash[hash], kind)
}

// kindsForHashes 本次变更的译文覆盖了哪些菜单位置（失效派发用）。
//
// 按原文哈希回查而不是取被提交那一行的 Kind：跨位置的同名文字只列一行，
// 提交的行只带其中一个位置 —— 照它派发会漏掉另一个位置，表现为「页脚实例的菜单标签
// 没被重建」（与 NAV 批的失效不完全同源，但同样是静默不收敛）。
// 回查不到时（理论上不会：能提交的行一定来自 build 出来的行）回落被提交行的位置：
// 宁可只标已知的位置，也不静默不标。
func (d *navigationTranslationsData) kindsForHashes(hashes map[string]bool, fallback []string) []string {
	if d == nil || len(hashes) == 0 {
		return nil
	}
	out := make([]string, 0, 2)
	seen := map[string]bool{}
	for h := range hashes {
		for _, kind := range d.kindsByHash[h] {
			if strings.TrimSpace(kind) == "" || seen[kind] {
				continue
			}
			seen[kind] = true
			out = append(out, kind)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
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

// scopeProjectID 失效派发用的工程作用域：以 build 解析出的为准。
//
// build 在表单没带 project 时会回落到第一个工程（与菜单管理页同一默认规则），
// 而表单变量仍是空串 —— 拿空串去打 menu:{pid}:{kind} 只会得到一条不匹配任何依赖的键，
// 表现为「保存成功、实例侧却没有任何失效」（静默失效比报错更难查）。
func scopeProjectID(data *navigationTranslationsData, formProjectID string) string {
	if data != nil && strings.TrimSpace(data.ProjectID) != "" {
		return strings.TrimSpace(data.ProjectID)
	}
	return strings.TrimSpace(formProjectID)
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
			logger.Scene("navigation").With("kind", kind).Error(terr, "读取导航树失败")
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
					// 位置记录**先于去重**：同一段文字同时出现在页眉与页脚时只列一行，
					// 但两个位置都要能被失效派发命中（见 kindsForHashes）。
					data.noteHashKind(h, kind)
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
			logger.Scene("navigation").With("lang", lang).Error(lerr, "读取现有导航译文失败")
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
