package pagehttp

// page_translations_handle.go — 译文（多语言）这一个业务功能的全部控制器：
// 翻译工作台（页面面）、支撑它的全站 (source_hash, context) 索引、缺译报告（兄弟页）。
//
// 为什么三者在一个文件（internal/module/CLAUDE.md §inbound/http「一个文件 = 一个业务功能」）：
// 工作台的分母（全站完成度）与复用提示来自全站索引，缺译报告的「缺译」口径也来自同一批
// 候选（builder.CollectContentCandidates），改口径时三处必须一起改 —— 拆成三个文件时
// 「工作台说 100%、报告页说还缺 3 条」这类分叉不报错，只在页面上互相矛盾。
//
// 分段：
//   · `// === 翻译工作台（页面面 + 保存）===`：整页 GET / 保存 / 失败重渲；
//   · `// === 全站索引（分母与复用提示）===`：扫描全站草稿 + 块内候选，进程内缓存 30s；
//   · `// === 缺译报告（页面 × 语言）===`：巡检视图与「取消该语言」入口。
//
// 鉴权：读复用「列出页面」，写复用「保存草稿」—— 保存会写 sys_translation（engine=manual）
// 并触发全站标记待重建，与保存草稿同一量级的影响面。

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/middleware/builtin"
	blockcontract "go_wp/internal/module/block/contract"
	pagecontract "go_wp/internal/module/page/contract"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/shell"
	"go_wp/pkg/binding"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

// === 翻译工作台（页面面 + 保存）===
// 页面翻译工作台（多语言 P5c，docs/06-D §7.8）：页面列表行内「多语言」按钮进来的那一页。
//
// 整页服务端渲染 + HTMX 片段刷新；本文件做三件事 —— 装配页面数据（译文分组、复用提示、
// 完成度）、保存译文，以及本功能自己的回跳与回执。支撑它的「全站 (source_hash, context)
// 视图」在同文件的「全站索引」一节。
//
// 鉴权：读复用「列出页面」，写复用「保存草稿」—— 保存会写 sys_translation（engine=manual）
// 并触发全站标记待重建，与保存草稿同一量级的影响面。
//
// 与缺译报告（U2，同文件的「缺译报告」一节）的分工：那一页处理「语言准备好了、但某些页面的
// 内容没译」；站点级准入（U1，project.SaveLocales 的界面词条门槛）拦「这个语言整体没准备好」。
// 两者的口径分别写在各自文件头。

// 块内文本**应当**出现在页面翻译工作台里：
//   - 页面产物本来就含页眉/页脚块与 core.globalref 内联块的文本（构建期装配），
//     工作台又是唯一的译文录入入口；不列出则这段文本永远无法翻译，
//     且完成度会显示 100% 而页眉仍是中文（错误的完成度信号）。
//   - 写入路径天然是全局的：sys_translation 主键 (source_hash, context, lang)，
//     保存后调用 page.MarkStaleForI18n 全站标记待重建——共享文本语义已由 P5c 支撑。
//   - 行上标注来源（页眉块/页脚块/全局块）与复用提示，避免「在 A 页改动了全站页眉」
//     被误读成本页局部修改。
//
// 与构建期同源：候选一律来自 builder.CollectContentCandidates（块文档同样过这一份
// 白名单与跳过规则），不另写扫描逻辑。本文件只读 block 契约。

// 用途（两个都需要「全站 (source_hash, context) 视图」）：
//  1. 跨页面复用提示：一行译文改一次，用到它的所有页面同时变，必须让编辑者看见
//     （「↳ 还用在另外 N 个页面（修改后全站同步生效）」）；
//  2. 全站翻译完成度：分母 = 全站去重后的 (source_hash, context) 条数，
//     分子 = 其中在目标语言已有译文的条数。
//
// 为什么不是一条 SQL：候选集合由「组件白名单 + 跳过规则」决定，白名单在 Go 里
// （core.TranslatableFields），SQL 侧无法表达；因此只能把全站草稿文档读回来，
// 用与构建期同一个函数 builder.CollectContentCandidates 收集。
//
// 代价与取舍（详见 docs/06-D §15.12）：
//   - 一次扫描 = 一条 SELECT 取回全站 pages.draft_document（JSONB）+ 全量 JSON 解析，
//     内存占用与「页面数 × 文档大小」同阶；
//   - 进程内缓存 TTL 30s，页面数超过 siteContentScanPageLimit 时主动跳过全站统计
//     （工作台退化为「本页维度」并在页面上说明），避免大站把后台拖垮；
//   - 更彻底的做法是新增「候选使用表」（page_id, source_hash, context，草稿保存时维护），
//     代价是每次草稿写入多一次索引维护与一张新表，本轮不做（遗留项）。
//
// 块内文本已纳入本索引（docs/06-D §15.14）：扫描页面草稿时同时记录「哪些页面引用了哪些块」
// （settings.structure 页眉/页脚绑定 + core.globalref 节点），再按块文档收集候选，
// 归属到引用它的页面——这样工作台行、复用提示与全站完成度分母三者口径一致。
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

// translationFact enums key → 一条文案**事实**（不取词；取词在模板里做）。
//
// 返回值判空用 Msg.Empty()：空 key 与空兜底同时出现才算「没有这条文案」，
// 生产 `pageMsg{}` / 测试用 `Msg{Key: ...}` 两种构造都能被它认出来。
func translationFact(msg string) pageMsg {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return pageMsg{}
	}
	return pageMsg{Key: msg, Fallback: translationMsgFallback[msg]}
}

// translationRowErrors 页面级错误行（没有 Label —— 对应不到具体某一行）：批量包成事实。
//
// 与逐行错误共用同一个切片类型，是因为模板只渲染一个列表、顺序就是发生顺序：
// 分成两个列表会让「先校验行、再报页面级」的先后关系在页面上重排。
func translationRowErrors(keys ...string) []pagedto.TranslationRowErrorResp {
	out := make([]pagedto.TranslationRowErrorResp, 0, len(keys))
	for _, k := range keys {
		if fact := translationFact(k); !fact.Empty() {
			out = append(out, pagedto.TranslationRowErrorResp{Msg: fact})
		}
	}
	return out
}

// pageTranslationsData 工作台页面数据。
//
// 字段名就是模板契约键，**但只有两个是壳键**（layout 的 {{.title}} / {{.menu}} 是小写），
// 其余与模板一致（模板读 {{.PageID}} / {{.Groups}} …）。小写那两个用 view 标签表达，
// 而不是再写一层 templateMap() 把 21 个字段手工搬一遍 ——
// 那层转换本身就是「加一个字段要改两处」的来源。
type pageTranslationsData struct {
	Title     string `view:"title"`
	Menu      string `view:"menu"`
	PageID    string
	PagePath  string
	ProjectID string
	Lang      string
	Langs     []pagedto.TranslationLangOptionResp
	Filter    string
	Groups    []pagedto.TranslationGroupResp
	RowCount  int
	PageDone  int
	PageTotal int
	SiteDone  int
	SiteTotal int
	// SiteNote 全站统计的降级说明（「全站统计暂不可用」之类）：事实，取词在模板里做。
	SiteNote pageMsg
	// IsDefaultLang 当前语言即站点默认语言：构建期不取内容译文（产物即原文）。
	IsDefaultLang bool
	// Errors 逐行错误（校验 / 写入失败时回渲本页，见 renderTranslationError）。
	// 元素是**事实**（哪一行 + 词条），取词留给模板。
	Errors []pagedto.TranslationRowErrorResp
	// LoadFailed / LoadError 主数据**没读到**（取数失败）：LoadFailed 为真时这一页没成立，
	// LoadError 是归口后的那条词条事实（原文只进日志）。契约：docs/rules/template-boundary.md
	// 的「整页 GET 的失败出口」；模板必须**先判失败、再判空态**。
	LoadFailed bool
	LoadError  pageMsg
	// ParamInvalid / ParamError 参数非法（地址里少了这一页赖以成立的东西）：400 档。
	// 与取数失败分开，是因为「改地址就能好」和「稍后重试」不是同一件事。
	ParamInvalid bool
	ParamError   pageMsg
}

// 筛选取值（服务端渲染，纯链接，零 JS）。
const (
	translationFilterAll     = "all"
	translationFilterMissing = "missing"
	translationFilterManual  = "manual"
	translationFilterAI      = "ai"
)

// translationsTemplate 翻译工作台的模板名：成功页与失败页**必须是同一个**模板，
// 否则「失败也是这一页」这条契约就靠不住（契约：整页 GET 的失败出口）。
const translationsTemplate = "admin/page/page_translations"

// translationsPageMenu 工作台高亮的侧栏项（与 buildPageTranslationsData 同值）。
const translationsPageMenu = "pages"

// errPagesServiceMissing 页面服务未装配：装配期错误，不是用户能修的请求事实。
var errPagesServiceMissing = errors.New("pages service 未装配")

// PageTranslations GET /admin/pages/translations：翻译工作台。
//
// 整页 GET 的失败一律走 JSON 错误体（用户裁定：所有提示只需要一个提示，不渲染完整页）：
// 读到 → 200；参数缺失 → 400；资源不存在 → 404；取数失败 → 500。
// 从前这三档渲染「同一页 + 错误槽」：同一张模板要维护两套必需键，而失败时页壳给的信息
// （筛选项、语言切换、整张表）没有一条是这次失败需要的。
// 更早的版本是 303 回列表页 —— 用户被弹出工作台，既看不到缺了什么参数，也看不到哪一步没读到。
func (h *pagesAdminHandle) PageTranslations(c *gin.Context) {
	pageID := strings.TrimSpace(c.Query("pageId"))
	lang := strings.TrimSpace(c.Query("lang"))
	if pageID == "" {
		// 参数非法档：没有页面标识，这一页无从成立 —— 但仍是「这一页」，带参数错误槽与回列表入口。
		h.renderTranslationsFailure(c, http.StatusBadRequest, pageTranslationsFailure{
			pageID: pageID, lang: lang,
			paramError: pagesLocalNoticeMissingPageIDGet,
		})
		return
	}
	if h.pages == nil {
		// 装配缺失是**主数据读不了**（不是用户能修的参数）→ 与取数失败同档；原文只进日志。
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
	c.HTML(http.StatusOK, translationsTemplate, adminui.PrepareView(c, data))
}

// pageTranslationsFailure 失败页的两个事实档：参数非法（400）与主数据没读到（404 / 500）。
type pageTranslationsFailure struct {
	pageID     string
	lang       string
	paramError pageMsg // 非空 = 参数非法档
	loadError  pageMsg // 非空 = 主数据没读到档
}

// translationsFailureStatus 取数失败的状态码：资源不存在 → 404，参数不合法 → 400，其余 → 500。
//
// 复用模块既有的「错误 → 状态码」映射（pageErrorStatus，与 JSON 出口同一判据），不另建一套分类：
// 同一类错误在两个出口给出不同结论，运营看到的与接口返回的就会互相矛盾。
func translationsFailureStatus(err error) int {
	switch status := pageErrorStatus(err); status {
	case http.StatusNotFound, http.StatusBadRequest:
		return status
	default:
		return http.StatusInternalServerError
	}
}

// renderTranslationsFailure 页面 GET 的失败出口：写 JSON 错误体（状态码由调用方给）。
//
// 参数非法与取数失败之所以分开两档，是因为「改地址就能好」和「稍后重试」不是同一件事 ——
// 这个区分留在状态码上，不需要两个模板分支。
func (h *pagesAdminHandle) renderTranslationsFailure(c *gin.Context, status int, f pageTranslationsFailure) {
	msg := f.loadError
	if !f.paramError.Empty() {
		msg = f.paramError
	}
	response.ErrorMsg(c, status, msg)
}

// SavePageTranslations POST /admin/pages/translations/save：保存本页译文。
//
// 成功走整页提示（1 秒后回工作台）；**校验 / 写入失败仍原地重渲工作台并逐行报错**
// （renderTranslationError）：一个业务错误把编辑者刚填的整屏译文清掉，比少一句提示贵得多
// —— 这是「写表单失败时原地留住输入」的例外（同 content 的文章保存，
// 见 internal/templates/CLAUDE.md）。
func (h *pagesAdminHandle) SavePageTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	req, ok := binding.Form[pagedto.PageTranslationsSaveFormReq](c)
	if !ok {
		return
	}
	pageID := strings.TrimSpace(req.PageID)
	if pageID == "" {
		// 缺 pageId 是参数级失败（表单被裁剪 / 手拼提交）：回列表页的整页提示。
		// 原先是 c.String(400, pageenums.MsgFieldRequired) —— 响应体是 i18n 的 key 本身，
		// 用户看到内部标识符，且页面脱离页壳。
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

	// 第一步：逐行校验（全部通过才写库，避免「部分成功」的中间态）。
	var rowErrors []pagedto.TranslationRowErrorResp
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
			rowErrors = append(rowErrors, pagedto.TranslationRowErrorResp{Label: contextName,
				Msg: translationFact(pageenums.MsgTranslationStale)})
			continue
		}
		if verr := builder.ValidateContentTarget(contextName, source, target); verr != nil {
			rowErrors = append(rowErrors, pagedto.TranslationRowErrorResp{Label: contextName,
				Msg: pageTranslationRowFact(c, verr)})
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
		translationJump(c, true, pageTranslationsSavedMsg(0))
		return
	}

	port, perr := h.contentPort()
	if perr != nil {
		logger.Scene("page").With("pageId", pageID).Error(perr, "内容译文存储不可用")
		h.renderTranslationError(c, pageID, lang, translationRowErrors(pageenums.MsgTranslationSaveFailed))
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
			h.renderTranslationError(c, pageID, lang, translationRowErrors(pageenums.MsgTranslationSaveFailed))
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
	// 第四步：结论走整页提示（不再经 ?saved=1&n= 回带工作台）。
	translationJump(c, true, pageTranslationsSavedMsg(written))
}

// renderTranslationError 校验/写入失败：回渲染工作台（200）并展示逐行错误，不落库。
func (h *pagesAdminHandle) renderTranslationError(c *gin.Context, pageID, lang string, errs []pagedto.TranslationRowErrorResp) {
	data, err := h.buildPageTranslationsData(c.Request.Context(), pageID, lang, "")
	if err != nil {
		// 重渲失败：这次写动作已经没有可回的工作台（「原样留下输入」做不到），走**写侧**的整页提示
		// 通道给一句结论 + 回列表入口 —— 静默 303 会把用户刚填的整屏译文连同原因一起吞掉。
		logger.Scene("page").With("pageId", pageID).Error(err, "重渲翻译工作台失败")
		pageListJump(c, false, pageErrMsgQuiet(c, err))
		return
	}
	data.Errors = errs
	c.HTML(http.StatusOK, "admin/page/page_translations", adminui.PrepareView(c, data))
}

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
		data.Langs = append(data.Langs, pagedto.TranslationLangOptionResp{Code: code, Label: code, Active: code == lang})
	}

	parsed, perr := builder.ParsePage(page.DraftDocument)
	if perr != nil {
		data.Errors = append(data.Errors, pagedto.TranslationRowErrorResp{Msg: translationFact(pageenums.MsgTranslationDocInvalid)})
		return data, nil
	}
	// 清单 = 本页文档候选 + 本页引用块（页眉/页脚绑定、core.globalref）内的候选。
	// 块内文本同样是本页产物的一部分（构建期装配内联），因此必须列在工作台里，
	// 否则它无法被翻译、完成度也会误报 100%（见本文件「全站索引」一节的块引用收集）。
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

	groups := make([]pagedto.TranslationGroupResp, 0, 8)
	for _, cand := range candidates {
		component, field, ok := i18n.ParseContentContext(cand.Context)
		if !ok {
			continue
		}
		key := i18n.ContentIndexKey(i18n.ContentHash(cand.Source), cand.Context)
		meta, _ := core.TranslatableFieldMeta(component, field)
		limit, _ := builder.ContentTargetLimit(cand.Context)
		row := pagedto.TranslationRowResp{
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
		// 分档不在这里产出：模板按 row.Engine 这个事实选档（ai → info、其余 mute），
		// 见 internal/templates/admin/page/page_translations.html。
		if site != nil {
			row.ReusePaths = site.reusePathsOf(key)
			row.ReusePages, row.ReuseTotal = reuseCounts(row.ReusePaths, page.DraftPath)
		}
		if len(groups) == 0 || groups[len(groups)-1].Component != component {
			groups = append(groups, pagedto.TranslationGroupResp{Component: component})
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
func filterTranslationGroups(groups []pagedto.TranslationGroupResp, filter string) []pagedto.TranslationGroupResp {
	if filter == translationFilterAll {
		return groups
	}
	out := make([]pagedto.TranslationGroupResp, 0, len(groups))
	for _, g := range groups {
		rows := make([]pagedto.TranslationRowResp, 0, len(g.Rows))
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
			out = append(out, pagedto.TranslationGroupResp{Component: g.Component, Rows: rows})
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

// —— 本功能自己的回跳与回执（不借 page_jump.go 的共享出口）——

// translationBackKeys 回跳筛选键：渲染时拼进表单 action 的 query、POST 回来时由
// shell.BackPath 读回 —— 两处必须同一份，否则「写完跳回去筛选静默丢了」。
var translationBackKeys = []string{"pageId", "lang"}

const pageTranslationsPath = "/admin/pages/translations"

func translationBack(c *gin.Context) string {
	return shell.BackPath(c, pageTranslationsPath, translationBackKeys...)
}

// translationBackMsg 回跳链接文字（复用本页标题词条，不新增全站词条）。
var translationBackMsg = pageMsg{Key: pageenums.MsgPageTranslationsTitle, Fallback: "多语言"}

// translationJump 回翻译工作台的提示页出口（保存成功）。
func translationJump(c *gin.Context, ok bool, msg pageMsg) {
	pageJump(c, ok, msg, translationBack(c), translationBackMsg)
}

// pageTranslationsSavedMsg 译文保存成功回执 —— 一句**带 %s 的整句词条** + 数字参数。
//
// 迁入前这里是三段词条（「已保存 」+ N + 「 条译文。」+ 可选尾句）各取一次词再拼接：
// 那是控制器取词，改一句话要动 Go，且英文语序拼不出来（"Saved N translations."）。
// 现在只交 key + 参数，取词在 jump.html 的 tf 里做。词条见迁移 613。
//
// written == 0 与 > 0 是**两条不同的整句**而不是「加不加后缀」：>
// 后缀版英文的时态与主语都变（"The changes marked …"），不能靠拼接得到。
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

// pageTranslationSentinels 译文逐行校验（builder.ValidateContentTarget）的 sentinel。
//
// 它们是**纯校验**（不碰库、不碰文件）的固定中文提示，且带字段名 / 字节数等明细，
// 属于可直接展示给编辑者的业务文案 —— 命中即原样透出。
//
// 这一份**没有随着 page_err.go 一起删**：它们来自 builder 包，消息形态是中文原文而非
// `Err*` / `a.b.c` 的 key（构造于 internal/builder/content_validate.go），过不了
// pkg/response.IsBusinessError。等 builder 侧把这几条也枚举化（消息取 enums 常量）之后，
// 这里应当只剩一句 `return pageErrMsgQuiet(c, err)`。
var pageTranslationSentinels = []error{
	builder.ErrContentContextInvalid,
	builder.ErrContentSourceSkipped,
	builder.ErrContentTargetEmpty,
	builder.ErrContentTargetTooLong,
	builder.ErrContentTargetShape,
}

// pageTranslationRowFact 译文逐行校验错误 → 一条文案**事实**。
//
// 命中 builder 的 5 个 sentinel → 空 Key + 中文原文兜底（含「上限 N 字节，实际 M 字节」
// 这类明细，它是 builder 拼出来的中文，没有词条对应的 key）；
// 未命中（将来若混进依赖错误）→ 走错误出口（记日志 + 归口）。
//
// 交事实而不是成品串：模板拿 key 取词，命中 sentinel 的那一档按兜底原样渲染 ——
// 这一档注定是中文，但它**只是这一行**，不会把整页文案锁死。
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

// —— 工作台的参数级提示（key + 中文兜底；key 与原文只有这一份）——

var (
	// pagesLocalNoticeMissingPageID 翻译工作台保存时缺页面 id
	// （POST /admin/pages/translations/save 的 pageId 为空）。
	//
	// 该分支原先 `c.String(400, pageenums.MsgFieldRequired)`：响应体只有 i18n 的 **key 本身**
	// （16 字节的 "MsgFieldRequired"）—— 用户看到的是内部标识符，而不是给运营看的文案，
	// 而且脱离页壳。key 复用通用必填词条（词条值就是「必填字段不能为空」）。
	pagesLocalNoticeMissingPageID = pageMsg{Key: pageenums.MsgFieldRequired, Fallback: "必填字段不能为空，未保存。"}

	// pagesLocalNoticeMissingPageIDGet 打开翻译工作台时地址里缺 pageId
	// （GET /admin/pages/translations 的 pageId 为空 → 400 参数错误槽）。
	//
	// 与写侧那条分开：写侧那句的结论是「未保存」，这条要说的是「这一页打不开，请从列表进」——
	// 同一事实、两种结论，合成一句会两边都不准。
	pagesLocalNoticeMissingPageIDGet = pageMsg{Key: pageenums.ErrInvalidParam, Fallback: "请求参数无效：地址里缺少页面标识，请从页面列表进入翻译工作台。"}
)

// === 全站索引（分母与复用提示）===
// blockCandidateInfo 块内候选集合：候选列表 + 每个 (hash, context) 的来源标签。
type blockCandidateInfo struct {
	candidates []builder.ContentCandidate
	origin     map[string]string // ContentIndexKey(hash, context) → 来源标签
}

// collectBlockCandidates 收集本页引用块（页眉/页脚绑定 + core.globalref，递归）的候选。
//
// 块不可用（已删除/模板块/解析失败）时跳过：与构建期一致降级，不阻断工作台渲染。
// 同一块只解析一次（visited 兼作引用环保护）。
// projectID 必须一起传：block.Detail 把工程归属当作必填的越权防护 scope，
// 只给块 ID 会拿到「参数缺失」—— 而这一层是「读不到就跳过」的降级路径，
// 症状不是报错，而是工作台里**块内文本一条都不列**（完成度还会误报 100%）。
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
		// 块内再引用块：沿用同一来源标签（外层来源即编辑者看到的入口）。
		for _, nested := range builder.ReferencedBlockIDs(blockPage.Root) {
			walk(nested, origin)
		}
	}
	// 槽位绑定逐个 walk，来源标签按槽位区分：编辑者要能看出某段文字来自页眉还是公告条。
	slotBindings := page.Settings.Structure.SlotBindings()
	for _, slot := range builder.SortedSlots(slotBindings) {
		// 槽位 → 来源标签的映射归 pageenums（值域与词条 key 都属本模块 enums）；
		// 标签值本身是模板的分支判据（展示 key 与兜底在 Jet）。
		walk(slotBindings[slot], pageenums.TranslationOriginOfSlot(slot))
	}
	for _, ref := range builder.ReferencedBlockIDs(page.Root) {
		walk(ref, pageenums.TranslationOriginBlock)
	}
	return info
}

// blockPageOf 解析块文档为 builder.Page（cache 为单次调用内缓存；不可用返回 nil）。
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

// mergeContentCandidates 合并本页与块内候选（按 (context, source) 去重，保持确定性顺序）。
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

const (
	// siteContentIndexTTL 全站索引缓存有效期（后台页面可接受的陈旧窗口）。
	siteContentIndexTTL = 30 * time.Second
	// siteContentScanPageLimit 全站扫描的页数上限：超过则跳过全站统计。
	siteContentScanPageLimit = 1000
)

// errSiteContentIndexSkipped 全站扫描被跳过（页数超限 / page 契约缺失）。
var errSiteContentIndexSkipped = errors.New("全站翻译统计已跳过")

// siteContentIndex 全站可翻译内容索引。
type siteContentIndex struct {
	// keys 去重后的 (source_hash + NUL + context) 键，字典序。
	keys []string
	// hashes 去重后的 source_hash（批量取译文用），字典序。
	hashes []string
	// usage 键 → 出现该 (hash, context) 的页面路径（去重，字典序）。
	usage map[string][]string
	// pages 参与扫描的页面数。
	pages int
	// skipped 是否因页数超限跳过（true 时 keys/hashes 为空）。
	skipped bool
}

// total 全站可翻译条目数（完成度分母）。
func (s *siteContentIndex) total() int {
	if s == nil {
		return 0
	}
	return len(s.keys)
}

// reusePagesOf 返回该键出现的页面数（含本页；0 = 未统计）。
func (s *siteContentIndex) reusePagesOf(key string) int {
	if s == nil {
		return 0
	}
	return len(s.usage[key])
}

// reusePathsOf 返回该键出现的页面路径（已排序）。
func (s *siteContentIndex) reusePathsOf(key string) []string {
	if s == nil {
		return nil
	}
	return s.usage[key]
}

// siteContentIndexCache 进程内缓存（pagesAdminHandle 持有）。
type siteContentIndexCache struct {
	mu  sync.Mutex
	at  time.Time
	idx *siteContentIndex
}

// siteContentIndexOf 取全站索引（命中缓存直接返回，过期重建）。
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

// buildSiteContentIndex 扫描全站草稿文档建索引（调用方持锁）。
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
	// add 登记一个候选（去重 + 记录出现该 (hash, context) 的页面路径）。
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
	// 块引用：块 ID → 引用该块的页面路径（块内文本的「出现在哪些页面」由此得出）。
	blockPaths := map[string][]string{}
	// blockProject 块 id → 所属工程：块查询要求工程 scope（越权防护的必填项），
	// 而这里遍历的是**全站**草稿（可能跨工程），所以按引用它的页面把工程记下来。
	// 块属于工程、id 全局唯一，同一个块不会出现在两个工程里。
	blockProject := map[string]string{}
	blockPathSeen := map[string]map[string]bool{}
	for i := range drafts {
		doc := drafts[i].DraftDocument
		if len(doc) == 0 {
			continue
		}
		page, perr := builder.ParsePage(doc)
		if perr != nil {
			// 单页解析失败不拖垮全站统计：跳过该页（构建期会自行报错）。
			continue
		}
		path := drafts[i].DraftPath
		if path == "" {
			path = drafts[i].ID
		}
		// SEO 文本字段不在 AST 里（组件侧白名单管不到），但构建期会取它们的译文 ——
		// 工作台必须看到同一份候选，否则作者根本找不到这两个字段可填（审计 I18N-014）。
		for _, cand := range builder.AppendSEOCandidates(page, builder.CollectContentCandidates(page)) {
			add(cand, path)
		}
		refs := builder.ReferencedBlockIDs(page.Root)
		// 槽位绑定统一从 SlotBindings 取：漏掉新槽位的表现是「公告条里的文案在翻译页面上找不到」，
		// 运营只能手工去找是哪个块。
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
	// 块内文本同样进全站索引（分母/复用提示与工作台行保持一致）：
	// 块被哪些页面引用，其文本就算出现在哪些页面；块内再引用块沿用同一批页面。
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

// reuseCounts preserves the site-index counting rule; it never formats UI text.
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
			// 当前页不在索引里（例如草稿路径刚改过）：总数即其他页面数。
			others = total
		}
	}
	return others, total
}

// TranslationMissesPage 缺译报告页：列出「页面 × 语言」中内容缺译的组合。
func (h *pagesAdminHandle) TranslationMissesPage(c *gin.Context) {
	// 工程：查询参数优先；只有一个工程时直接选中它（多工程且未选时给显式提示，
	// 不静默取第一个 —— 那会让人看着 A 工程的报告以为是 B 的）。
	projectID := strings.TrimSpace(c.Query("project"))
	rows := []pagedto.TranslationMissResp{}
	// errFact 是**事实**（key + 中文兜底），取词留给模板；空值表示本次没有错误。
	var errFact pageMsg
	if projectID == "" && h.projects != nil {
		if list, lerr := h.projects.List(c.Request.Context()); lerr == nil && len(list) == 1 {
			projectID = list[0].ID
		}
	}
	if projectID == "" {
		// 中文兜底写在事实里：词条缺失时（测试环境 / 新装库未 seed）用户该看到一句话
		// 而不是 ErrProjectRequired 这样的常量名。
		errFact = pageMsg{Key: pageenums.ErrProjectRequired,
			Fallback: "请先选择站点工程：缺译报告按工程统计"}
	} else if list, lerr := h.pages.UntranslatedPageLangs(c.Request.Context(), projectID); lerr != nil {
		errFact = pageErrMsg(c, lerr)
	} else {
		// 行的形状直接复用契约里的 TranslationMissResp（跨模块可见即入契约），
		// 不再另造一份字段相同的渲染行 —— 两份必须逐字段等价的定义就是静默分叉的温床。
		rows = append(rows, list...)
	}
	c.HTML(http.StatusOK, "admin/page/page_translation_misses.html",
		h.translationMissPageData(c, projectID, rows, errFact))
}

// TranslationMissCancel 把某个（页面 × 语言）取消：调 ExcludePageLang（不另写下线逻辑）。
//
// 结论走整页提示（成功 1 秒后回报告页）：不再原地重渲报告页 —— 写动作的结论统一
// 由 shell.RenderJump 呈现（见 page_jump.go）。
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

// translationMissPageData 报告页模板数据（键一律总是存在）。
//
// 本页是不走 layout 的整页（自带 <!DOCTYPE html>），因此用 PrepareBare 补 t / tf ——
// 与 adminui.Prepare 的区别只是少了 layout 需要的那些键（sidebarmenus / permset 等）。
func (h *pagesAdminHandle) translationMissPageData(c *gin.Context, projectID string, rows []pagedto.TranslationMissResp, errFact pageMsg) gin.H {
	token, terr := builtin.GetCSRFToken(c)
	if terr != nil {
		token = ""
	}
	// 工程列表（选择器用）：照页面列表页的既有形态，多工程下要能切换着看。
	// 读失败给空列表 —— 选择器不渲染，报告仍按查询参数给出的工程显示（缺参数时上面已给提示）。
	projects := []projectdto.ProjectResp{}
	if h.projects != nil {
		if list, lerr := h.projects.List(c.Request.Context()); lerr == nil {
			projects = list
		}
	}
	return adminui.PrepareBare(c, gin.H{
		"csrf_token":      token,
		"Projects":        projects,
		"SelectedProject": projectID,
		"Rows":            rows,
		"Err":             errFact,
	})
}

// —— 本功能自己的回跳（不借 page_jump.go 的共享出口）——

// translationMissBackKeys 回跳筛选键：渲染时拼进表单 action 的 query、POST 回来时由
// shell.BackPath 读回 —— 两处必须同一份，否则「写完跳回去筛选静默丢了」。
var translationMissBackKeys = []string{"project"}

const pageTranslationMissPath = "/admin/page-translation-misses"

func translationMissBack(c *gin.Context) string {
	return shell.BackPath(c, pageTranslationMissPath, translationMissBackKeys...)
}

// translationMissBackMsg 回跳链接文字（复用本页标题词条，不新增全站词条）。
var translationMissBackMsg = pageMsg{Key: pageenums.TranslationMissTitle, Fallback: "内容缺译报告"}

// translationMissJump 回缺译报告的提示页出口（取消某语言）。
func translationMissJump(c *gin.Context, ok bool, msg pageMsg) {
	pageJump(c, ok, msg, translationMissBack(c), translationMissBackMsg)
}
