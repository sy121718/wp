package pagehttp

// page_translations_handle.go - 翻译工作台（多语言 P5c）：本页译文的查看与保存。

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"go_wp/internal/builder"
	pagecontract "go_wp/internal/module/page/contract"
	pageenums "go_wp/internal/module/page/enums"
	"go_wp/internal/web/shell"
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

// SetContentTranslationStore 注入内容译文端口（测试用；生产走默认库）。
func (h *pagesAdminHandle) SetContentTranslationStore(store contentTranslationPort) {
	h.contentStore = store
}

// contentPort 返回工作台的内容译文端口（未注入时用默认库）。
func (h *pagesAdminHandle) contentPort() (contentTranslationPort, error) {
	if h.contentStore != nil {
		return h.contentStore, nil
	}
	return i18n.NewContentWriterDefault()
}

// pageOf 按 id 读取页面（草稿保存与页面翻译页共用）。
//
// 抽成一个方法是因为多处调用需要同一份「页面不存在怎么回」的语义：
// 它们各自决定跳转目标（列表页 / 404 / 回本页），但取数口径必须一致 ——
// 曾经这里有一处直接调 Detail 而忘了判空，页面被删后成了 500。
func (h *pagesAdminHandle) pageOf(c *gin.Context, pageID string) (*pagecontract.PageResp, error) {
	if h.pages == nil {
		return nil, errors.New("页面服务未装配")
	}
	// Detail 把 projectID 当**必填的越权防护 scope**（少它只会得到「参数缺失」，
	// 看起来像「页面不存在」）。历史 / 译文这些路由手上只有 pageId，
	// 所以先用只读的 ProjectOfPage 问「这个页面属于谁」，再按 scope 取详情。
	ctx := c.Request.Context()
	projectID, err := h.pages.ProjectOfPage(ctx, pageID)
	if err != nil {
		return nil, err
	}
	return h.pages.Detail(ctx, &pagecontract.DetailReq{ProjectID: projectID, ID: pageID})
}

// translationMsgFallback 工作台提示的中文兜底。
//
// enums 常量是 sys_i18n 的 key（缺词条时由调用方给原文），工作台的
// 错误/提示直接渲染在页面上，因此在 handler 侧就翻好，模板不再二次取词。
var translationMsgFallback = map[string]string{
	pageenums.MsgTranslationSaveFailed:      "译文保存失败，请稍后重试",
	pageenums.MsgTranslationInvalid:         "提交数据不完整，请刷新页面后重试",
	pageenums.MsgTranslationStale:           "原文已变更，请刷新页面后重新翻译",
	pageenums.MsgTranslationLangInvalid:     "目标语言未启用，请先在站点设置里启用",
	pageenums.MsgTranslationDocInvalid:      "页面草稿无法解析，请先在工作台修复页面",
	pageenums.MsgTranslationSiteScanSkipped: "全站统计暂不可用，当前仅显示本页维度",
	pageenums.MsgTranslationSiteScanTooMany: "页面数超过全站扫描上限，当前仅显示本页维度",
}

// translationMsg 把 enums key 翻成当前语言；非 key（如 builder 校验的原始中文）原样返回。
func translationMsg(c *gin.Context, msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	return shell.TranslateFor(c)(msg, translationMsgFallback[msg])
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
func (h *pagesAdminHandle) PageTranslations(c *gin.Context) {
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
	c.HTML(http.StatusOK, "admin/page/page_translations", shell.Prepare(c, data.templateMap()))
}

// SavePageTranslations POST /admin/page/translations/save：保存本页译文。
func (h *pagesAdminHandle) SavePageTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	if pageID == "" {
		// 页面请求的失败出口是**页面**：303 回列表页并把原因经 ?err= 回带（读侧 pagePageErr
		// 白名单放行）。原先是 c.String(400, pageenums.MsgFieldRequired) —— 响应体是 i18n
		// 的 key 本身，用户看到内部标识符，且页面脱离页壳（与同文件其它失败分支不一致）。
		c.Redirect(http.StatusSeeOther, pagesBackURL(pageBulkTextOf(c, pagesLocalNoticeMissingPageID), ""))
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
		h.renderTranslationError(c, pageID, lang, []string{pageenums.MsgTranslationLangInvalid})
		return
	}

	contexts := c.PostFormArray("rowContext")
	sources := c.PostFormArray("rowSource")
	hashes := c.PostFormArray("rowHash")
	targets := c.PostFormArray("rowTarget")
	if len(contexts) != len(sources) || len(contexts) != len(hashes) || len(contexts) != len(targets) {
		h.renderTranslationError(c, pageID, lang, []string{pageenums.MsgTranslationInvalid})
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
			// 空输入 = 本行不写入（清空输入框不会删除库中已有译文）。
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
			rowErrors = append(rowErrors, contextName+"："+translationMsg(c, pageenums.MsgTranslationStale))
			continue
		}
		if verr := builder.ValidateContentTarget(contextName, source, target); verr != nil {
			rowErrors = append(rowErrors, contextName+"："+pageTranslationRowText(c, verr))
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
		h.renderTranslationError(c, pageID, lang, []string{pageenums.MsgTranslationSaveFailed})
		return
	}

	// 第二步：变更判定。只有译文文本确实变化才写库并触发全站重建：
	// 原样再保存一次不产生任何写入（幂等），也不触发无意义的全站重建。
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
			h.renderTranslationError(c, pageID, lang, []string{pageenums.MsgTranslationSaveFailed})
			return
		}
	}

	// 第三步：译文文本变化 → 全站标记待重建（i18n:content 依赖条目配套）。
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
func (h *pagesAdminHandle) renderTranslationError(c *gin.Context, pageID, lang string, errs []string) {
	data, err := h.buildPageTranslationsData(c.Request.Context(), pageID, lang, "")
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/admin/pages")
		return
	}
	data.Errors = translationMsgs(c, errs)
	data.SiteNote = translationMsg(c, data.SiteNote)
	c.HTML(http.StatusOK, "admin/page/page_translations", shell.Prepare(c, data.templateMap()))
}
