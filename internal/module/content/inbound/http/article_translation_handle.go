// article_translation_handle.go — 文章内容翻译工作台（审计 I18N-006）。
//
// 与商品域工作台（product_translation_handle.go）同一套机制：列表按实体分组、
// 逐行显示原文与译文、整表提交、只写真正变化的行、保存后精确标记受影响实例待重建。
//
// 差异只在两处：
//
//	· 数据源是 content 模块（文章），字段清单来自 contentcontract（title / body / excerpt /
//	  seoTitle / seoDescription）；
//	· body 是**富文本**：模板给出多行输入，且校验要求译文与原文**形态一致**
//	  （原文含标签则译文也必须含标签）—— 形态检查在这里，标签白名单在构建期
//	  （core.SanitizeRichHTML）：保存期挡住明显的错误，构建期挡住恶意内容。
//
// 验收对应（I18N-006）：文章译文在对应语言产物中生效（构建期取词）/ 改原文后旧译文按
// hash 失效并回退原文（寻址机制天然保证）/ 译文正文经过白名单清洗（构建期清洗）。
package contenthttp

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/module/content/contract"
	contentdto "go_wp/internal/module/content/dto"
	"go_wp/internal/web/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// articleTranslationRow 工作台一行（一个可翻译取值）。
type articleTranslationRow struct {
	Context    string
	Field      string
	FieldLabel string
	Source     string
	SourceHash string
	Target     string
	Engine     string
	Translated bool
	// Rich 富文本字段（body）：译文需与原文同形。
	Rich bool
}

// articleTranslationGroup 按文章分组的行集合。
type articleTranslationGroup struct {
	Key        string
	EntityType string
	EntityID   string
	Title      string
	Subtitle   string
	Rows       []articleTranslationRow
}

// articleTranslationHandle 文章翻译工作台。
type articleTranslationHandle struct {
	contents contentcontract.ContentService
	writer   *i18n.ContentWriter
}

// NewArticleTranslationHandle 构造。
func NewArticleTranslationHandle(contents contentcontract.ContentService) *articleTranslationHandle {
	return &articleTranslationHandle{contents: contents}
}

// SetContentWriter 注入译文写入端口（装配期）。
func (h *articleTranslationHandle) SetContentWriter(w *i18n.ContentWriter) { h.writer = w }

// articleFieldLabel 字段的展示名（工作台用）：字段名 → {i18n key, 中文兜底}。
var articleFieldLabel = map[string]articleTranslationText{
	"title":          {"admin.article.translations.field.title", "标题"},
	"body":           {"admin.article.translations.field.body", "正文"},
	"excerpt":        {"admin.article.translations.field.excerpt", "摘要"},
	"seoTitle":       {"admin.article.translations.field.seoTitle", "SEO 标题"},
	"seoDescription": {"admin.article.translations.field.seoDescription", "SEO 描述"},
}

// articleTranslationText 一条待取词文案（key + 中文兜底）。
type articleTranslationText struct{ Key, Fallback string }

// articleTranslationTextField 字段展示名 → 当前语言文案（未登记字段原样回显字段名，便于排查）。
func articleTranslationTextField(tr func(key, fallback string) string, field string) string {
	if item, ok := articleFieldLabel[field]; ok {
		return tr(item.Key, item.Fallback)
	}
	return field
}

// articleTranslationTextOf 本页文案的当前语言文本（key 直接给，兜底在下方 map 里）。
func articleTranslationTextOf(c *gin.Context, key, fallback string) string {
	return shell.TranslateFor(c)(key, fallback)
}

// ArticleTranslations GET /admin/articles/translations。
func (h *articleTranslationHandle) ArticleTranslations(c *gin.Context) {
	data := h.build(c, strings.TrimSpace(c.Query("lang")), strings.TrimSpace(c.Query("project")), strings.TrimSpace(c.Query("keyword")))
	if saved := strings.TrimSpace(c.Query("saved")); saved == "1" {
		n, _ := strconv.Atoi(strings.TrimSpace(c.Query("n")))
		data.Saved = true
		if n > 0 {
			data.SavedNote = i18n.FillTranslate(shell.TranslateFor(c),
				"admin.article.translations.saved", "已保存 {count} 条译文（下次构建生效）。",
				map[string]string{"count": strconv.Itoa(n)})
		} else {
			data.SavedNote = articleTranslationTextOf(c, "admin.article.translations.savedNone", "没有需要写入的变化。")
		}
	}
	c.HTML(http.StatusOK, "admin/content/article_translations.html", shell.Prepare(c, data.templateMap()))
}

// SaveArticleTranslations POST /admin/articles/translations/save（整表提交）。
func (h *articleTranslationHandle) SaveArticleTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	lang := strings.TrimSpace(c.PostForm("lang"))
	projectID := strings.TrimSpace(c.PostForm("project"))
	keyword := strings.TrimSpace(c.PostForm("keyword"))
	page, _ := strconv.Atoi(c.PostForm("page"))
	data := h.build(c, lang, projectID, keyword)
	// 保存校验必须使用提交时的同一页，否则第二页的行会被误判为原文已变化。
	if page != data.Page || c.PostForm("limit") != strconv.Itoa(data.Limit) {
		data.Errors = []string{articleTranslationTextOf(c, "admin.article.translations.err.pageChanged", "列表页码已变化，请刷新后重试")}
		c.HTML(http.StatusOK, "admin/content/article_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}

	contexts := c.PostFormArray("rowContext")
	hashes := c.PostFormArray("rowHash")
	targets := c.PostFormArray("rowTarget")
	if len(contexts) != len(hashes) || len(contexts) != len(targets) {
		data.Errors = []string{articleTranslationTextOf(c, "admin.article.translations.err.rowCountMismatch", "提交的行数不一致，请刷新后重试")}
		c.HTML(http.StatusOK, "admin/content/article_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}
	if h.writer == nil {
		data.Errors = []string{articleTranslationTextOf(c, "admin.article.translations.err.storageUnavailable", "译文存储不可用")}
		c.HTML(http.StatusOK, "admin/content/article_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}

	// 第一步：逐行校验（全部通过才写库，避免「部分成功」的中间态）。
	var rowErrors []string
	items := make([]i18n.ContentWriteItem, 0, len(contexts))
	keys := make([]string, 0, len(contexts))
	queued := map[string]bool{}
	for i := range contexts {
		contextName := strings.TrimSpace(contexts[i])
		source, ok := data.sourceOf(contextName, hashes[i])
		if !ok {
			rowErrors = append(rowErrors, i18n.FillTranslate(shell.TranslateFor(c),
				"admin.article.translations.err.sourceChanged", "{context}：原文已变化，请刷新后重试",
				map[string]string{"context": contextName}))
			continue
		}
		target := strings.TrimSpace(targets[i])
		if target == "" {
			continue // 空输入 = 本行不写入（不动库中已有译文）
		}
		key := i18n.ContentIndexKey(source.SourceHash, contextName)
		if queued[key] {
			continue
		}
		queued[key] = true
		if verr := validateArticleTarget(contextName, source, target); verr != "" {
			// verr 是**词条 key**：直接拼到页面上会显示裸 key，必须取词（兜底在下方那句中文里）。
			rowErrors = append(rowErrors, i18n.FillTranslate(shell.TranslateFor(c), verr, articleTranslationFallbacks[verr],
				map[string]string{"context": contextName}))
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
		c.HTML(http.StatusOK, "admin/content/article_translations.html", shell.Prepare(c, data.templateMap()))
		return
	}
	if len(items) == 0 {
		c.Redirect(http.StatusSeeOther, articleTranslationLocation(projectID, lang, keyword, data.Page, data.Limit, 0))
		return
	}

	// 第二步：只写真正变化的行（重复保存零写入、不推进 revision）。
	hashesAll := make([]string, 0, len(items))
	for _, it := range items {
		hashesAll = append(hashesAll, it.SourceHash)
	}
	before, berr := h.writer.LoadDetails(ctx, lang, hashesAll)
	if berr != nil {
		logger.Scene("content").With("lang", lang).Error(berr, "读取现有文章译文失败，按全部变更处理")
		before = map[string]i18n.ContentTargetInfo{}
	}
	pending := make([]i18n.ContentWriteItem, 0, len(items))
	changed := make([]string, 0, len(items))
	for i, item := range items {
		prev := before[keys[i]]
		if prev.TargetText == item.TargetText && prev.Engine == i18n.ContentEngineManual {
			continue
		}
		pending = append(pending, item)
		changed = append(changed, keys[i])
	}
	written := 0
	if len(pending) > 0 {
		var uerr error
		written, uerr = h.writer.Upsert(ctx, pending)
		if uerr != nil {
			logger.Scene("content").
				With("lang", lang).
				With("user_id", shell.CurrentUserID(c)).
				Error(uerr, "写入文章译文失败")
			// 这一处已记过带 lang 的日志：文案出口用不记日志的版本，避免同一错误记两条。
			data.Errors = []string{i18n.FillTranslate(shell.TranslateFor(c),
				"admin.article.translations.err.saveFailed", "保存失败：{detail}",
				map[string]string{"detail": articleFacingOrInternal(c, uerr)})}
			c.HTML(http.StatusOK, "admin/content/article_translations.html", shell.Prepare(c, data.templateMap()))
			return
		}
	}
	c.Redirect(http.StatusSeeOther, articleTranslationLocation(projectID, lang, keyword, data.Page, data.Limit, written))
}

// articleTranslationsData 页面数据。
type articleTranslationsData struct {
	Title      string
	Menu       string
	Lang       string
	Keyword    string
	Page       int
	Limit      int
	ProjectID  string
	HasData    bool
	Pagination gin.H
	Langs      []translationLangOption
	Groups     []articleTranslationGroup
	RowCount   int
	Done       int
	Total      int
	Saved      bool
	SavedNote  string
	Errors     []string
}

// templateMap 转小写键 map（layout 以 {{.title}} / {{.menu}} 取值）。
func (d *articleTranslationsData) templateMap() gin.H {
	out := gin.H{
		"title": d.Title, "menu": d.Menu,
		"Lang": d.Lang, "Langs": d.Langs, "Groups": d.Groups,
		"Keyword": d.Keyword, "Page": d.Page, "Limit": d.Limit, "ProjectID": d.ProjectID, "HasData": d.HasData,
		"RowCount": d.RowCount, "Done": d.Done, "Total": d.Total,
		"Saved": d.Saved, "SavedNote": d.SavedNote, "Errors": d.Errors,
	}
	for k, v := range d.Pagination {
		out[k] = v
	}
	return out
}

// sourceOf 按语境 + hash 反查本次提交对应的原文（防表单被裁剪 / 篡改）。
func (d *articleTranslationsData) sourceOf(contextName, hash string) (articleTranslationRow, bool) {
	for _, g := range d.Groups {
		for _, r := range g.Rows {
			if r.Context == contextName && r.SourceHash == hash {
				return r, true
			}
		}
	}
	return articleTranslationRow{}, false
}

// articleTranslationFallbacks 校验类文案 key → 中文兜底（写成一张表而不是散在 return 里：
// 取词点要拿同一个兜底，两处各写一份就会「词条改了、兜底没改」）。
var articleTranslationFallbacks = map[string]string{
	"admin.article.translations.err.contextInvalid":  "语境非法：不是文章的可翻译字段",
	"admin.article.translations.err.notTranslatable": "原文不参与翻译（空串、纯数字或纯符号）",
	"admin.article.translations.err.richMismatch":    "正文译文的形态与原文不一致：原文含 HTML 标签时译文也必须含标签",
}

// validateArticleTarget 校验一条文章译文的可写性（与构建期同源）。
func validateArticleTarget(contextName string, source articleTranslationRow, target string) string {
	entityType, field, ok := i18n.ParseContentContext(contextName)
	if !ok || !contentcontract.IsTranslatableField(entityType, field) {
		return "admin.article.translations.err.contextInvalid"
	}
	if !i18n.ShouldTranslateContent(source.Source) {
		return "admin.article.translations.err.notTranslatable"
	}
	if source.Rich && hasMarkup(source.Source) != hasMarkup(target) {
		return "admin.article.translations.err.richMismatch"
	}
	return ""
}

// articleTranslationLocation 保存后的回跳地址（PRG）。
func articleTranslationLocation(projectID, lang, keyword string, page, limit, written int) string {
	q := url.Values{"lang": {lang}, "saved": {"1"}, "n": {strconv.Itoa(written)}}
	if projectID != "" {
		q.Set("project", projectID)
	}
	if keyword != "" {
		q.Set("keyword", keyword)
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if limit != 20 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return "/admin/articles/translations?" + q.Encode()
}

// build 组装工作台数据：文章列表 → 可翻译字段 → 现有译文。
//
// 译文**批量预载**（LoadTargets 一次查询）再回填：按行逐条查会随文章数放大成 N 次 SQL，
// 而工作台一屏就要列出 50 篇的全部可翻译字段。
func (h *articleTranslationHandle) build(c *gin.Context, lang, projectID, keyword string) *articleTranslationsData {
	ctx := c.Request.Context()
	page, limit := shell.PageParams(c)
	if c.Request.Method == http.MethodPost {
		page, _ = strconv.Atoi(c.PostForm("page"))
		limit, _ = strconv.Atoi(c.PostForm("limit"))
		if limit < 1 || limit > 100 {
			limit = 20
		}
	}
	if page < 1 {
		page = 1
	}
	data := &articleTranslationsData{
		Title: "admin.article.translations.heading", Menu: "article-translations", Lang: lang,
		Keyword: keyword, Page: page, Limit: limit, ProjectID: projectID,
		Groups: []articleTranslationGroup{},
	}
	for _, code := range i18n.AvailableLangs() {
		data.Langs = append(data.Langs, translationLangOption{Code: code, Label: code, Active: code == lang})
	}
	if lang == "" {
		return data
	}
	if h.contents == nil {
		data.Errors = []string{articleTranslationTextOf(c, "admin.article.translations.err.depsMissing", "内容模块未装配")}
		return data
	}
	filter := &contentdto.ListReq{EntityType: "article", Keyword: keyword}
	total, err := h.contents.Count(ctx, filter)
	if err != nil {
		data.Errors = []string{i18n.FillTranslate(shell.TranslateFor(c),
			"admin.article.translations.err.countFailed", "读取文章总数失败：{detail}",
			map[string]string{"detail": articleFacingError(c, err)})}
		return data
	}
	data.HasData = total > 0
	if keyword != "" && total == 0 {
		// 空匹配与真空数据不同：额外计数只在筛选零结果时执行。
		unfiltered, countErr := h.contents.Count(ctx, &contentdto.ListReq{EntityType: "article"})
		if countErr != nil {
			data.Errors = []string{i18n.FillTranslate(shell.TranslateFor(c),
				"admin.article.translations.err.countFailed", "读取文章总数失败：{detail}",
				map[string]string{"detail": articleFacingError(c, countErr)})}
			return data
		}
		data.HasData = unfiltered > 0
	}
	page = articleClampPage(page, limit, total)
	data.Page = page
	filter.Limit, filter.Offset = limit, (page-1)*limit
	list, err := h.contents.List(ctx, filter)
	if err != nil {
		// 形态③（模板数据 Errors）：err.Error() 直接拼进来会把 PG 原文摆到页面上。
		data.Errors = []string{i18n.FillTranslate(shell.TranslateFor(c),
			"admin.article.translations.err.listFailed", "读取文章列表失败：{detail}",
			map[string]string{"detail": articleFacingError(c, err)})}
		return data
	}
	data.Pagination = shell.BuildPagination(total, page, limit,
		shell.FilterBaseURL("/admin/articles/translations", map[string]string{
			"lang": lang, "project": projectID, "keyword": keyword, "limit": strconv.Itoa(limit),
		}), shell.TranslateFor(c)).TemplateKeys()
	fields := contentcontract.TranslatableFields("article")
	// 记录每一行在工作台里的位置：回填译文时要按 (组下标, 行下标) 写回。
	type rowRef struct {
		groupIdx int
		rowIdx   int
		hash     string
		context  string
	}
	var refs []rowRef
	groups := make([]articleTranslationGroup, 0, len(list))
	for _, item := range list {
		group := articleTranslationGroup{
			Key: item.ID, EntityType: "article", EntityID: item.ID,
			Subtitle: item.Slug, Rows: []articleTranslationRow{},
		}
		if t, ok := item.Data["title"].(string); ok {
			group.Title = t
		}
		for _, f := range fields {
			src, ok := item.Data[f].(string)
			if !ok || !i18n.ShouldTranslateContent(src) {
				continue
			}
			contextName := i18n.ContentContext("article", f)
			h := i18n.ContentHash(src)
			refs = append(refs, rowRef{
				groupIdx: len(groups), rowIdx: len(group.Rows), hash: h, context: contextName,
			})
			group.Rows = append(group.Rows, articleTranslationRow{
				Context: contextName, Field: f, FieldLabel: articleTranslationTextField(shell.TranslateFor(c), f),
				Source: src, SourceHash: h, Rich: f == "body",
			})
		}
		data.RowCount += len(group.Rows)
		data.Total++
		groups = append(groups, group)
	}
	data.Groups = groups
	// 预载现有译文并回填（writer 未注入时跳过：工作台仍可看原文，只是显示不出已有译文）。
	if h.writer != nil && len(refs) > 0 {
		hashes := make([]string, 0, len(refs))
		for _, r := range refs {
			hashes = append(hashes, r.hash)
		}
		targets, lerr := h.writer.LoadTargets(ctx, lang, hashes)
		if lerr != nil {
			logger.Scene("content").With("lang", lang).Error(lerr, "读取现有文章译文失败")
		} else {
			for _, r := range refs {
				if t, ok := targets[i18n.ContentIndexKey(r.hash, r.context)]; ok && t != "" {
					data.Groups[r.groupIdx].Rows[r.rowIdx].Target = t
					data.Groups[r.groupIdx].Rows[r.rowIdx].Translated = true
					data.Done++
				}
			}
		}
	}
	return data
}
