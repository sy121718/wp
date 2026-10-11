package producthttp

import (
	"context"
	"go_wp/internal/adminui"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/pipeline"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"net/http"
	"sort"
	"strconv"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"

	"github.com/gin-gonic/gin"
)

// product_translations.go — 商品译文工作台：候选收集、逐行校验、保存与「按依赖标记待重建」。
//
// 候选来自构建期同一份收集逻辑（productcontract.TranslationCandidate），译文写 sys_translation
// 后要把用到这些 key 的实例标记待重建 —— 少标一次的表现是「译好了但线上还是旧语言」。

// productTranslationPort 商品翻译工作台使用的译文读写端口。
//
// 生产实现 = pkg/i18n.ContentWriter（sys_translation 表 + 默认数据库）。
//
// 工程作用域（审计 I18N-009）：商品译文与页面译文同一张表的同一套隔离规则，
// 工作台按本工程读写 —— 工作台本来就是按工程选商品的（project 查询参数）。
// i18n key 是字符串协议：这些 key 原登记在 dashboard/enums，页面回迁后随域归入本模块，
// 字面值与词条表（迁移 163 起）保持一致 —— 改 key 必须同步 sys_i18n 词条。

// product_translation_data.go - 商品译文工作台的数据装配（候选分组、目标校验、哈希与工程/语言选项）。

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
		productTranslationsJump(c, false, adminui.InternalMsg())
		return
	}
	// 保存成功的回执不再经 ?saved=1&n=N 回带（那条读侧已整批删除）：结论由提示页渲染。
	c.HTML(http.StatusOK, "admin/product/product_translations.html", adminui.Prepare(c, data.templateMap()))
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
		productTranslationsJump(c, false, adminui.InternalMsg())
		return
	}
	if !h.langAllowed(ctx, data.ProjectID, lang) {
		data.Errors = translationRowErrors(productenums.MsgTranslationLangInvalid)
		c.HTML(http.StatusOK, "admin/product/product_translations.html", adminui.Prepare(c, data.templateMap()))
		return
	}

	contexts := c.PostFormArray("rowContext")
	hashes := c.PostFormArray("rowHash")
	targets := c.PostFormArray("rowTarget")
	if len(contexts) != len(hashes) || len(contexts) != len(targets) {
		data.Errors = translationRowErrors(productenums.MsgTranslationInvalid)
		c.HTML(http.StatusOK, "admin/product/product_translations.html", adminui.Prepare(c, data.templateMap()))
		return
	}

	// 第一步：逐行校验（全部通过才写库，避免「部分成功」的中间态）。
	var rowErrors []productdto.TranslationRowError
	items := make([]i18n.ContentWriteItem, 0, len(contexts))
	writeKeys := make([]string, 0, len(contexts))
	queued := map[string]bool{}
	for i := range contexts {
		contextName := strings.TrimSpace(contexts[i])
		source, ok := data.sourceOf(contextName, hashes[i])
		if !ok {
			rowErrors = append(rowErrors, productdto.TranslationRowError{Label: contextName, Msg: translationFact(productenums.MsgTranslationStale)})
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
			rowErrors = append(rowErrors, productdto.TranslationRowError{Label: contextName, Msg: translationFact(verr)})
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
		c.HTML(http.StatusOK, "admin/product/product_translations.html", adminui.Prepare(c, data.templateMap()))
		return
	}
	if len(items) == 0 {
		productTranslationsJump(c, true, productTranslationsSavedMsg(c, 0))
		return
	}

	port, perr := h.port()
	if perr != nil {
		logger.Scene("product").With("project", projectID).Error(perr, "内容译文存储不可用")
		data.Errors = translationRowErrors(productenums.MsgTranslationSaveFailed)
		c.HTML(http.StatusOK, "admin/product/product_translations.html", adminui.Prepare(c, data.templateMap()))
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
			data.Errors = translationRowErrors(productenums.MsgTranslationSaveFailed)
			c.HTML(http.StatusOK, "admin/product/product_translations.html", adminui.Prepare(c, data.templateMap()))
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
	productTranslationsJump(c, true, productTranslationsSavedMsg(c, written))
}

// productTranslationsSavedMsg 译文保存成功的回执（0 条 = 没有需要写入的变化）。
//
// 复用工作台原有的两条词条（ProductTranslationsSavedNote / SavedNone），措辞与改造前一致；
// 只是改由提示页在响应体里渲染（取代 ?saved=1&n=N 的读侧判定）。
//
// **例外 3（具名占位符句式）**：ProductTranslationsSavedNote 用的是具名占位符 `{n}`
// （i18n.FillTranslate），不是 `%s`；而模板注入的 tf 只有 `%s` 位置参数，一段 key + Args
// 表达不了它。要收成一条 `%s` 词条属于独立的文案迁移（page 模块已按迁移 613 收掉了，
// product 这一条还没做），不夹在「控制器不取词」这一步里做。
func productTranslationsSavedMsg(c *gin.Context, written int) productMsg {
	if written > 0 {
		return productMsg{Fallback: i18n.FillTranslate(shell.TranslateFor(c), productenums.ProductTranslationsSavedNote,
			"已保存 {n} 条译文；译文变更已标记待重建（下次构建生效）。",
			map[string]string{"n": strconv.Itoa(written)})}
	}
	return productMsg{Key: productenums.ProductTranslationsSavedNone, Fallback: "没有需要写入的变化。"}
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

// translationEntityRef 受影响的商品域实体（自动发布实例标记用）。
type translationEntityRef struct {
	EntityType string
	EntityID   string
}

// productTranslationsData 工作台页面数据。
type productTranslationsData struct {
	Title     string
	Menu      string
	ProjectID string
	ProductID string
	Lang      string
	Langs     []productdto.TranslationLangOption
	// SourceLang 站点源语言（= 站点默认语言）；判定依据见 sourceLangOf。
	SourceLang string
	// NoTargetLang 没有任何「非源语言」的启用语言：没有可翻译的目标，页面给可行动空态。
	NoTargetLang bool
	Groups       []productdto.ProductTranslationGroup
	RowCount     int
	Done         int
	Total        int
	// Errors 逐行 / 页面级错误（校验或写入失败时重渲本页）。元素是**事实**
	//（哪一行 + 词条 key + 中文兜底），取词留给页面的 tf —— 见 productdto.TranslationRowError。
	Errors         []productdto.TranslationRowError
	ProjectOptions []productdto.ProjectTranslationOption
}

// templateMap 转为模板所需的小写键 map（layout 以 {{.title}}/{{.menu}} 取值）。
func (d *productTranslationsData) templateMap() gin.H {
	return gin.H{
		"title": d.Title, "menu": d.Menu,
		"ProjectID": d.ProjectID, "ProductID": d.ProductID,
		"Lang": d.Lang, "Langs": d.Langs, "Groups": d.Groups,
		"SourceLang": d.SourceLang, "NoTargetLang": d.NoTargetLang,
		"RowCount": d.RowCount, "Done": d.Done, "Total": d.Total,
		"Errors":         d.Errors,
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
//
// 返回值是**词条 key**（不是中文句子）：它们经 translationFact 收成一条条事实后进页面的
// 错误列表，中文兜底登记在 product_page_util.go 的 productenums.TranslationMsgFallbacks 里 ——
// 写死中文的话英文工作台上永远是中文，而这几条恰好是运营最常撞上的行级结论。
func validateProductTarget(contextName string, source sourceCandidate, target string) string {
	entityType, field, ok := parseContextField(contextName)
	if !ok || !productcontract.IsTranslatableField(entityType, field) {
		return productenums.ProductTranslationsErrContextInvalid
	}
	if !i18n.ShouldTranslateContent(source.SourceText) {
		return productenums.ProductTranslationsErrSourceSkipped
	}
	if strings.TrimSpace(target) == "" {
		return productenums.ProductTranslationsErrTargetEmpty
	}
	if source.Rich && hasMarkup(source.SourceText) != hasMarkup(target) {
		return productenums.ProductTranslationsErrShapeMismatch
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

// 商品翻译工作台的行级校验结论：值是 i18n key，常量在 productenums（ProductTranslationsErr*）；
// 中文兜底见 product_page_util.go 的 productenums.TranslationMsgFallbacks（那几条是页面上最常出现的
// 几句话，控制器只交事实、取词在模板）。

// build 组装工作台数据（工程 + 商品 + 语言 + 候选 + 现有译文）。
//
// 不收取词函数：控制器只交**事实**（key + 中文兜底），取词在 admin/product/product_translations.html
// 里由 Prepare 注入的 tr / tf 做（`Title` 交给页壳的 title 键，分组标题走 TitleKey / TitleFallback）。
func (h *productTranslationHandle) build(ctx context.Context, projectID, productID, wantLang string) (data *productTranslationsData, err error) {
	data = &productTranslationsData{
		// Title 存**词条 key**（不是取好词的中文）：页壳的 Prepare 会把 title 按当前请求语言翻一次
		//（fallback 就是 key 本身，与 page 模块的翻译工作台同一形态）。
		Title: productenums.ProductTranslationsTitle,
		Menu:  "products", ProjectID: projectID, ProductID: productID,
	}

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
	data.Langs = make([]productdto.TranslationLangOption, 0, len(targetLangs))
	for _, code := range targetLangs {
		data.Langs = append(data.Langs, productdto.TranslationLangOption{Code: code, Label: code, Active: code == data.Lang})
	}

	// 商品域候选：指定商品时只列该商品的（含引用实体），否则列整个工程。
	//
	// 单商品入口也要带工程 id（DB-009 收口）：products 的 RLS 策略使非超级角色下
	// 不带作用域的读静默 0 行，表现为「点开某个商品的翻译说商品不存在」。
	// 工程 id 用 data.ProjectID，与下面整工程入口、以及本页其它取数（语言 / 译文）同源。
	var cands []productcontract.TranslationCandidate
	if productID != "" {
		if cands, err = h.products.ProductTranslationCandidates(ctx, data.ProjectID, productID); err != nil {
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
//
// 不收取词函数（调用点给事实，取词在页面里）：分组标题里的实体类型名是**词条**
// （TitleKey / TitleFallback），实体名是**数据**（Title），两样分开交。
//
// 实体类型 → (key, 兜底) 的映射在 productenums.EntityTypeLabel（一份，不再就地 switch）：
// 契约侧 productcontract.EntityTypeLabel 的 tr 形态也接到同一份映射上。
func groupCandidates(cands []productcontract.TranslationCandidate, targets map[string]i18n.ContentTargetInfo) []productdto.ProductTranslationGroup {
	order := make([]string, 0, len(cands))
	index := map[string]*productdto.ProductTranslationGroup{}
	for _, c := range cands {
		key := c.EntityType + "/" + c.EntityID
		g, ok := index[key]
		if !ok {
			titleKey, titleFallback := productenums.EntityTypeLabel(c.EntityType)
			g = &productdto.ProductTranslationGroup{
				Key:           key,
				EntityType:    c.EntityType,
				EntityID:      c.EntityID,
				Title:         c.EntityName,
				TitleKey:      titleKey,
				TitleFallback: titleFallback,
				Subtitle:      c.EntityType,
			}
			index[key] = g
			order = append(order, key)
		}
		info := targets[i18n.ContentIndexKey(c.SourceHash, c.Context)]
		g.Rows = append(g.Rows, productdto.ProductTranslationRow{
			Context: c.Context, Field: c.Field, FieldLabel: c.FieldLabel,
			Source: c.SourceText, SourceHash: c.SourceHash,
			Target: info.TargetText, Engine: info.Engine,
			Translated: info.TargetText != "",
			EngineTone: i18n.ContentEngineTone(info.Engine),
			Rich:       c.Field == "description",
		})
	}
	out := make([]productdto.ProductTranslationGroup, 0, len(order))
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
func (h *productTranslationHandle) projectOptions(ctx context.Context, want string) (options []productdto.ProjectTranslationOption, ids []string, err error) {
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
		options = append(options, productdto.ProjectTranslationOption{ID: p.ID, Name: p.Name, Selected: p.ID == selected})
	}
	return options, ids, nil
}

// sourceLangOf 站点源语言。
//
// 判定依据（与构建期同一来源，见 pipeline.DefaultLocale）：project_locales 里 is_default
// 的那一行；清单缺失、没有默认标记或表不可读时，project 契约自身回退全局默认语言
// （sys_config 的 i18n 组 default_lang，见 internal/module/project/service/locale_service.go
// 的 DefaultLocale），这里再加一层兜底保证源语言永远非空。
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
