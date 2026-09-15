package dashboardhttp

// product_translation_handle.go — 商品域翻译工作台（issue #12）。
//
// 入口：商品列表行内「多语言」按钮（/admin/products/translations?project=…[&product=…]），
// 与页面翻译工作台（/admin/page/translations）同构：语言下拉 + 分组表格 + 整表 POST
// 保存 + PRG 回跳，零自定义 JS（语言切换为原生 GET 表单）。
//
// 与构建期同源：候选来自 productservice.ProductTranslationCandidates（同一份
// contract 白名单 + 同一份语境界定），写入走 pkg/i18n.ContentWriter（与页面工作台
// 共用写入路径与 hash 校验）。
//
// 触发重建（验收 6：译文变更能触发受影响页面重建）：译文文本确实变化时
//   - page.MarkStaleForI18n 标记手工页面待重建（与页面工作台同一条链路，
//     产物 Manifest 的 i18n:content 依赖条目负责判定，标记负责排队）；
//   - presentation.MarkStaleByDependency 按 direct_content:{实体类型}:{实体id}
//     精确标记受影响实体的自动发布实例（商品页面就是这类实例，page 侧的全站标记
//     覆盖不到它们）。
//
// 失败只记日志（译文已落库，标记失败由运维重建兜底）。

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"

	dashboardenums "go_wp/internal/module/dashboard/enums"
	pagecontract "go_wp/internal/module/page/contract"
	productcontract "go_wp/internal/module/product/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

// productTranslationPort 商品翻译工作台使用的译文读写端口。
//
// 生产实现 = pkg/i18n.ContentWriter（sys_translation 表 + 默认数据库）。
//
// 工程作用域（审计 I18N-009）：商品译文与页面译文同一张表的同一套隔离规则，
// 工作台按本工程读写 —— 工作台本来就是按工程选商品的（project 查询参数）。
type productTranslationPort interface {
	LoadDetails(ctx context.Context, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	LoadDetailsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]i18n.ContentTargetInfo, error)
	LoadTargets(ctx context.Context, lang string, hashes []string) (map[string]string, error)
	LoadTargetsForProject(ctx context.Context, projectID, lang string, hashes []string) (map[string]string, error)
	Upsert(ctx context.Context, items []i18n.ContentWriteItem) (written int, err error)
}

// ProductTranslationInstancePort 商品译文变更后需要失效的自动发布实例端口。
//
// 消费者侧最窄接口：编排层把 presentation 契约实现传进来即可（跨模块只依赖契约）。
type ProductTranslationInstancePort interface {
	MarkStaleByDependency(ctx context.Context, kind, key string) (ids []string, err error)
}

// productTranslationHandle 商品域翻译页处理器。
type productTranslationHandle struct {
	products productcontract.ProductService
	projects projectcontract.ProjectService
	pages    pagecontract.PageService
	// instances 自动发布实例失效端口（可空：为空时只做 page 侧标记）。
	instances ProductTranslationInstancePort
	// writer 译文读写端口（为 nil 时按默认库惰性构造；测试注入隔离 schema 的写入器）。
	writer productTranslationPort
}

// NewProductTranslationHandle 构造商品域翻译页处理器（instances 可为 nil）。
func NewProductTranslationHandle(products productcontract.ProductService, projects projectcontract.ProjectService,
	pages pagecontract.PageService, instances ProductTranslationInstancePort) *productTranslationHandle {
	return &productTranslationHandle{products: products, projects: projects, pages: pages, instances: instances}
}

// SetContentTranslationStore 注入译文读写端口（测试用；生产走默认库）。
func (h *productTranslationHandle) SetContentTranslationStore(port productTranslationPort) {
	h.writer = port
}

// port 返回译文读写端口（未注入时用默认库）。
func (h *productTranslationHandle) port() (productTranslationPort, error) {
	if h.writer != nil {
		return h.writer, nil
	}
	return i18n.NewContentWriterDefault()
}

// SetupProductTranslationRoutes 注册商品域翻译页路由（挂 /admin 页面组）。
//
// saveGuard 由调用方传入（页面组层需要挂 Casbin 权限点中间件），与页面翻译工作台
// 同一口径：保存改的是商品的展示文本，复用商品更新权限点；GET 属安全方法，
// 页面组已有 Session + CSRF。
func SetupProductTranslationRoutes(adminPages *gin.RouterGroup, saveGuard gin.HandlerFunc,
	products productcontract.ProductService, projects projectcontract.ProjectService, pages pagecontract.PageService,
	instances ProductTranslationInstancePort) *productTranslationHandle {
	handle := NewProductTranslationHandle(products, projects, pages, instances)
	adminPages.GET("/products/translations", handle.ProductTranslations)
	if saveGuard != nil {
		adminPages.POST("/products/translations/save", saveGuard, handle.SaveProductTranslations)
		return handle
	}
	adminPages.POST("/products/translations/save", handle.SaveProductTranslations)
	return handle
}

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
	Title          string
	Menu           string
	ProjectID      string
	ProductID      string
	Lang           string
	Langs          []translationLangOption
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
		"RowCount": d.RowCount, "Done": d.Done, "Total": d.Total,
		"Saved": d.Saved, "SavedNote": d.SavedNote, "Errors": d.Errors,
		"ProjectOptions": d.ProjectOptions,
	}
}

// ProductTranslations GET /admin/products/translations：商品域翻译工作台。
func (h *productTranslationHandle) ProductTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.Query("project"))
	productID := strings.TrimSpace(c.Query("product"))
	lang := strings.TrimSpace(c.Query("lang"))

	data, err := h.build(ctx, projectID, productID, lang)
	if err != nil {
		logger.Scene("product").With("project", projectID).With("product", productID).
			Error(err, "打开商品翻译工作台失败")
		c.Redirect(http.StatusSeeOther, "/admin/products")
		return
	}
	if saved := strings.TrimSpace(c.Query("saved")); saved == "1" {
		data.Saved = true
		n, _ := strconv.Atoi(strings.TrimSpace(c.Query("n")))
		if n > 0 {
			data.SavedNote = "已保存 " + strconv.Itoa(n) + " 条译文；译文变更已标记待重建（下次构建生效）。"
		} else {
			data.SavedNote = "没有需要写入的变化。"
		}
	}
	c.HTML(http.StatusOK, "admin/product_translations.html", withCSRF(c, data.templateMap()))
}

// SaveProductTranslations POST /admin/products/translations/save：保存译文（整表提交）。
func (h *productTranslationHandle) SaveProductTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	projectID := strings.TrimSpace(c.PostForm("project"))
	productID := strings.TrimSpace(c.PostForm("product"))
	lang := strings.TrimSpace(c.PostForm("lang"))

	data, err := h.build(ctx, projectID, productID, lang)
	if err != nil {
		logger.Scene("product").With("project", projectID).Error(err, "商品翻译工作台保存前重建数据失败")
		c.Redirect(http.StatusSeeOther, "/admin/products")
		return
	}
	if !h.langAllowed(ctx, data.ProjectID, lang) {
		data.Errors = translationMsgs(c, []string{dashboardenums.MsgTranslationLangInvalid})
		c.HTML(http.StatusOK, "admin/product_translations.html", withCSRF(c, data.templateMap()))
		return
	}

	contexts := c.PostFormArray("rowContext")
	hashes := c.PostFormArray("rowHash")
	targets := c.PostFormArray("rowTarget")
	if len(contexts) != len(hashes) || len(contexts) != len(targets) {
		data.Errors = translationMsgs(c, []string{dashboardenums.MsgTranslationInvalid})
		c.HTML(http.StatusOK, "admin/product_translations.html", withCSRF(c, data.templateMap()))
		return
	}

	// 第一步：逐行校验（全部通过才写库，避免「部分成功」的中间态）。
	var rowErrors []string
	items := make([]i18n.ContentWriteItem, 0, len(contexts))
	writeKeys := make([]string, 0, len(contexts))
	queued := map[string]bool{}
	for i := range contexts {
		contextName := strings.TrimSpace(contexts[i])
		source, ok := data.sourceOf(contextName, hashes[i])
		if !ok {
			rowErrors = append(rowErrors, contextName+"："+translationMsg(c, dashboardenums.MsgTranslationStale))
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
		if verr := validateProductTarget(contextName, source, target); verr != "" {
			rowErrors = append(rowErrors, contextName+"："+verr)
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
		data.Errors = rowErrors
		c.HTML(http.StatusOK, "admin/product_translations.html", withCSRF(c, data.templateMap()))
		return
	}
	if len(items) == 0 {
		c.Redirect(http.StatusSeeOther, translationLocation(projectID, productID, lang, 0))
		return
	}

	port, perr := h.port()
	if perr != nil {
		logger.Scene("product").With("project", projectID).Error(perr, "内容译文存储不可用")
		data.Errors = translationMsgs(c, []string{dashboardenums.MsgTranslationSaveFailed})
		c.HTML(http.StatusOK, "admin/product_translations.html", withCSRF(c, data.templateMap()))
		return
	}

	// 第二步：变更判定。只有译文文本确实变化才写库并触发重建（幂等，重复保存零写入）。
	// 变更判定按**本工程**读现有译文（工程行优先、回落全局行）。
	before, berr := port.LoadDetailsForProject(ctx, projectID, lang, productWriteHashes(items))
	if berr != nil {
		logger.Scene("product").With("project", projectID).Error(berr, "读取现有译文失败，按全部变更处理")
		before = map[string]i18n.ContentTargetInfo{}
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
			logger.Scene("product").With("project", projectID).With("lang", lang).Error(uerr, "写入商品译文失败")
			data.Errors = translationMsgs(c, []string{dashboardenums.MsgTranslationSaveFailed})
			c.HTML(http.StatusOK, "admin/product_translations.html", withCSRF(c, data.templateMap()))
			return
		}
	}

	// 第三步：译文变化 → 标记待重建。
	//   1) 手工页面：i18n:content 依赖条目配套的全站标记（与页面翻译工作台同一链路）；
	//   2) 自动发布实例：按 direct_content 键精确标记受影响实体（商品页面属这类）。
	if targetChanged {
		if h.pages != nil {
			if merr := h.pages.MarkStaleForI18n(ctx); merr != nil {
				logger.Scene("product").With("project", projectID).Error(merr, "商品译文保存后标记页面待重建失败")
			}
		}
		h.markInstancesStale(ctx, data, changedKeys)
	}
	c.Redirect(http.StatusSeeOther, translationLocation(projectID, productID, lang, written))
}

// translationEntityRef 受影响的商品域实体（自动发布实例标记用）。
type translationEntityRef struct {
	EntityType string
	EntityID   string
}

// markInstancesStale 按 direct_content 键标记受影响实体的自动发布实例待重建。
//
// 译文按 (原文 hash, 语境) 寻址，实体身份只在候选里；同一段文本可能来自多个实体，
// 因此按「语境 + 指纹」回查本次渲染出的候选行，命中即视为该实体受影响
// （保守超集：宁可多标记，也不漏标记 —— 与页面侧「引用块即登记依赖」同一口径）。
//
// 未注入实例端口 / 无变化 / 标记失败：只记日志，不影响保存结果（译文已落库）。
func (h *productTranslationHandle) markInstancesStale(ctx context.Context, data *productTranslationsData, changedKeys []string) {
	if h.instances == nil || data == nil || len(changedKeys) == 0 {
		return
	}
	for _, ref := range data.translationAffectedEntities(changedKeys) {
		dep := pipeline.DirectContentKey(ref.EntityType, ref.EntityID)
		if _, err := h.instances.MarkStaleByDependency(ctx, dep.Kind, dep.Key); err != nil {
			logger.Scene("product").With("entity_type", ref.EntityType).With("entity_id", ref.EntityID).
				Error(err, "商品译文保存后标记自动发布实例待重建失败")
		}
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

	langs := h.enabledLangsOf(ctx, data.ProjectID)
	data.Lang = strings.TrimSpace(wantLang)
	if !containsString(langs, data.Lang) {
		data.Lang = langs[0]
	}
	data.Langs = make([]translationLangOption, 0, len(langs))
	for _, code := range langs {
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
	targets := map[string]i18n.ContentTargetInfo{}
	if len(cands) > 0 {
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
func (h *productTranslationHandle) langAllowed(ctx context.Context, projectID, lang string) bool {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return false
	}
	return containsString(h.enabledLangsOf(ctx, projectID), lang)
}
