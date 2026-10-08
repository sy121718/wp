package pagehttp

// 背景：workbench.js 的 loadHistory 用 DOM 拼列表 + 每行绑恢复事件。
// 本文件把列表渲染搬到服务端；恢复动作也服务端化（查修订 → 覆盖保存草稿），
// 客户端只需确认与整页刷新。
//
// 端点（挂装配层传入的 /workbench 根级页面组）：
//   POST /workbench/history         → 修订列表片段
//   POST /workbench/history/restore → 恢复指定版本（覆盖草稿）

// 形状照 page_schedule_handle.go：**HTMX 片段 + 原生表单**（form-urlencoded + 隐藏
// csrf_token 域），写操作成功后重渲染同一片段。为什么不让 HTMX 直接打 JSON 接口：
// HTMX 的表单 POST 是 form-encoded，而 JSON 接口只收 JSON —— 要么引入 json-enc 扩展
// （新增前端依赖），要么在接口里做双形态绑定（两套解析路径，容易只测到一条）。
//
// 鉴权：写操作挂**后台页面组**并显式复用 API 的权限点路径（builtin.CasbinMiddlewareForPath）。
// 页面路径与权限点路径不一致，直接按页面路径 enforce 会因权限点表无此路径而拒绝所有用户
// （含超管）—— 与 /admin/page-schedules/* 同一手法。
//
// 文案：四条业务错误在调用点给中文兜底（pageLangErrText），未命中才落到既有归口
// （pageErrPageText：结构化日志 + shell.PageInternalText）。**不直出 err.Error()**，
// 也**不直出内部错误**。

// 页面挂在 authorizedAPI 组下（/api/page/redirect）而不是 /admin/*：page 模块在
// 分发给各模块注册）。这样挂的代价是 URL 少一层「后台感」，换来的是三层链
// （Session / CSRF / Casbin）与 API 完全一致，不需要改动 routes.go。
//
// 交互约定：GET 渲染整页；三个 POST 都是原生表单提交（带 csrf_token 隐藏域），
// 完成后 302 回本页（PRG，防重复提交）。失败也走 PRG，但把错误以 **i18n key**
// 放进 query（不是中文文案）：这样同一个 URL 在英文界面下显示英文提示。

// 两条通道，职责分开：
//
//   - **JSON 接口**（/api/page/schedule/{set,cancel,list}）：给工作台 / 脚本 / 后续的
//     批量排定用。错误经 pageErrorStatus / pageErrorMessage 分类，状态码与文案与其它
//     page 接口同源。
//   - **后台面板**（/admin/page-schedules/{panel,set,cancel}）：HTMX 片段，
//     表单是 form-urlencoded（原生表单 + 隐藏 csrf_token 域），成功后重新渲染面板片段。
//     为什么不是「HTMX 直接打 JSON 接口」：HTMX 的表单 POST 是 form-encoded，
//     而 JSON 接口只收 JSON —— 要么引入 json-enc 扩展（新增前端依赖），
//     要么在接口里做双形态绑定（两套解析路径，容易只测到一条）。挂页面组与
//     修订历史面板（workbenchPages.POST("/workbench/history")）同形，是本仓的既有做法。
//
// 面板与列表页的失败原因都**经词条取词**显示（pageenums.ScheduleFailureFallbacks）：
// page_schedules.last_error 存的是业务 key 而不是原文，原文只进日志 ——
// 直接把那一列渲染出来，英文界面上会显示 ErrRebuildRequired 这样的裸 key。

// 与站点级准入（U1，project.SaveLocales 的界面词条门槛）的分工写在这里与页面上：
// **U1 拦「这个语言整体没准备好」，U2 处理「语言准备好了、但某些页面的内容没译」**。
//
// 操作列的「取消该语言」直接调 `ExcludePageLang`（上一批实现）——不另写一套下线逻辑：
// 取消 = 下线该语言产物 + 清发布/暂存/路由/计划（同一事务）+ 写排除列，
// 那套语义（以及它与切换器 / hreflang / sitemap 的一致性）已经在那一处验证过。
//
// 挂**后台页面组**（`/admin/page-translation-misses`，Session + CSRF 已具备），
// 写操作显式复用 API 权限点路径（与 /admin/page-schedules/* 同一手法）。
// 工程选择器照页面列表页的既有形态（GET 表单 + select），多工程下按工程看报告。

// 页面管理列表页（后台「页面」入口）：列出/新建站点工程与页面，
// 行内直达可视化工作台。交互遵循后台 HTMX 规范：HTMX 请求返回
// Jet 片段，否则完整页面/重定向。

// 系统页面槽位把「结算页是哪一页」这类事实固定下来：购物车片段的「去结算」、
// 访客订单列表的「查看订单」、登录页与注册页的互跳，都从这里取路径。
// 后端接口（/api/page/site-slot/{list,bind,unbind}）早已就绪，后台此前没有界面 ——
// 菜单点进来是 404，本文件补齐这个入口。
//
// 四条与 page 模块的约定：
//
//  1. 跨模块只依赖 pagecontract（它已把槽位 DTO 重导出为契约别名）与不可变 pageenums，
//     不 import 其它模块的 dto / model / service。
//
//  2. pageenums 的错误常量值是**常量名**（"ErrInvalidSlot"），不是中文文案，而且 page
//     模块没有 order / cart 那样的 UserFacingMessages 白名单切片。所以本页自带一份
//     「错误常量名 → 中文面客文案」映射（siteSlotFacingMessages）：未命中的多半是数据库
//     原文（可能带表名甚至 SQL 片段），一律落到 shell.PageInternalText 的统一提示。
//     写动作的结论由 shell.RenderJump 渲染成整页提示（见 page_jump.go），不再经查询参数回显。
//
//  3. 槽位绑的是**页面 id**（uuid，不可变）而不是 URL：改 URL 是页面的常规操作，
//     绑 id 之后链接自动跟着走。页面没有「标题」这个概念，草稿路径是它唯一稳定的身份，
//     所以下拉的 value 是页面 id、显示文本是草稿路径。
//
//  4. 「绑定存在」与「访问面真的有产物」是两件事：Bound=true 但 Published=false 时
//     Path 为空，链接生成方据此降级（不输出链接）。页面上必须把这种状态显式标出来，
//     否则运营会以为配好了；PageDeleted 是绑定指向了已删页面，属于「需要立刻修」的状态
//     （红色提示 + 解绑入口）。**未绑定不是错误** —— 这个站没有博客、没有结算页是正常状态，
//     用中性徽章而不是红色。
//
//  5. 绑定 / 换绑是「先点这一行、再在表格下方的面板里选页面」的两步（行内不再内嵌下拉）：
//     每行内嵌一份候选意味着 N 行付 N×候选数 个 option 节点（10 行 × 14 = 140 个），
//     操作列宽度也被下拉撑开。候选现在只在面板里渲染一次，进入方式是行内那条
//     「绑定 / 换绑」链接（GET 回本页带 slot 参数）。面板是原生 form + 原生 select，
//     无 JS 也成立，鼠标 / 滚轮与触摸板 / 触屏 / 键盘都走原生路径。

// 批量操作的结论（AGENTS.md：「结论按『成功 N / 跳过 M』回带，不允许静默的部分成功」）
// 现在由 shell.RenderJump 渲染成整页提示：句子在响应体里，不进 URL，所以不存在
// 「URL 里的计数可被客户端改写」这条伪造面（旧形态 `?done=已删除 3 个页面` 曾靠读侧
// 归一化比对来防伪）。有跳过走失败档（不自动跳，用户下次会去看剩下那些）。

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

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	"go_wp/internal/middleware/builtin"
	"go_wp/internal/module/block/contract"
	"go_wp/internal/module/blueprint/contract"
	"go_wp/internal/module/blueprint/dto"
	"go_wp/internal/module/page/contract"
	"go_wp/internal/module/page/dto"
	"go_wp/internal/module/page/enums"
	"go_wp/internal/module/page/model"
	"go_wp/internal/module/page/service"
	"go_wp/internal/module/project/contract"
	"go_wp/internal/module/project/dto"
	"go_wp/internal/templates"
	"go_wp/internal/shell"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
	"go_wp/pkg/sitetz"
	"go_wp/pkg/utils"
)

// historyRowView 修订历史行视图。
type historyRowView struct {
	Version   int64
	Path      string
	CreatedAt string
}

// HistoryPanel 渲染修订历史列表片段。
func (h *pagesAdminHandle) HistoryPanel(c *gin.Context) {
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	// Revisions 用空切片而非 nil：Jet 的 len() 不接受 nil（会渲染失败）。
	// t 是片段模板的取词函数：片段不经 shell.Prepare，缺 t 时 Jet 把取词调用求值成空串。
	data := gin.H{"Revisions": []historyRowView{}, "Error": "", "t": shell.TranslateFor(c)}
	if pageID != "" && h.pages != nil {
		revs, err := h.pages.ListRevisions(c.Request.Context(), &pagecontract.RevisionReq{PageID: pageID})
		if err != nil {
			data["Error"] = shell.TranslateFor(c)("admin.pages.history.loadFailed", "加载失败")
		} else {
			views := make([]historyRowView, 0, len(revs))
			for _, r := range revs {
				views = append(views, historyRowView{
					Version: r.Version, Path: r.DraftPath,
					CreatedAt: r.CreatedAt.Time().Local().Format("2006-01-02 15:04"),
				})
			}
			data["Revisions"] = views
		}
	}
	c.HTML(http.StatusOK, "fragments/history_list", data)
}

// HistoryRestore 恢复指定修订到草稿（覆盖保存为新修订，由客户端随后整页刷新）。
func (h *pagesAdminHandle) HistoryRestore(c *gin.Context) {
	ctx := c.Request.Context()
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	version, err := strconv.ParseInt(strings.TrimSpace(c.PostForm("version")), 10, 64)
	if pageID == "" || err != nil || h.pages == nil {
		// 消息一律用模块 enums 的 key（pkg/response 按请求语言取词，中英各一条词条），
		// 不写字面量中文 —— 这里硬编码过「参数错误」，英文后台下是唯一的中文出口。
		response.ErrorWithMessage(c, http.StatusBadRequest, pageenums.ErrInvalidParam)
		return
	}
	revs, err := h.pages.ListRevisions(ctx, &pagecontract.RevisionReq{PageID: pageID})
	if err != nil {
		logger.Scene("page").With("page_id", pageID).Error(err, "读取页面修订失败")
		response.ErrorWithMessage(c, http.StatusInternalServerError, shell.MsgInternalError)
		return
	}
	var target *pagecontract.RevisionResp
	for i := range revs {
		if revs[i].Version == version {
			target = &revs[i]
			break
		}
	}
	if target == nil {
		response.ErrorWithMessage(c, http.StatusNotFound, pageenums.ErrRevisionNotFound)
		return
	}
	page, err := h.pageOf(c, pageID)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, pageenums.ErrPageNotFound)
		return
	}
	res, err := h.pages.SaveDraft(ctx, &pagecontract.SaveDraftReq{
		ID: pageID, ExpectedVersion: page.DraftVersion,
		DraftPath: target.DraftPath, DraftDocument: target.DraftDocument,
	})
	if err != nil {
		// 这里是「用某个修订覆盖保存草稿」：最常见的失败是乐观锁冲突（另一个标签页刚存过），
		// 与原先的 409 语义一致；错误原文只进日志，不再拼进响应体。
		logger.Scene("page").With("page_id", pageID).Error(err, "恢复页面修订失败")
		response.ErrorWithMessage(c, http.StatusConflict, pageenums.ErrDraftVersionConflict)
		return
	}
	response.Success(c, gin.H{"draftVersion": res.DraftVersion})
}

// pageLangRowView 面板里的一行语言（状态文案已按当前语言取词）。
type pageLangRowView struct {
	Lang string
	// IsDefault 站点默认语言（不可排除）。
	IsDefault bool
	// Excluded 本页已排除该语言。
	Excluded bool
	// Published 该语言当前有已激活产物。
	Published bool
	// Status 状态文案（已排除 / 已发布 / 未发布）。
	Status string
	// CanRepublish 可以点「重新发布」（未排除即可；已排除的要先恢复）。
	CanRepublish bool
	// Note 不可操作的原因（默认语言），空 = 可操作。
	Note string
}

// PageLangsPanel GET /admin/page-langs/panel?pageId=…：某页的语言产出范围面板（HTMX 片段）。
func (h *pagesAdminHandle) PageLangsPanel(c *gin.Context) {
	c.HTML(http.StatusOK, "fragments/page_langs", h.pageLangsPanelData(c, c.Query("pageId"), "", ""))
}

// PageLangExclude POST /admin/page-langs/exclude：排除某语言（同时下线其产物）。
func (h *pagesAdminHandle) PageLangExclude(c *gin.Context) {
	h.applyPageLangChange(c, false)
}

// PageLangRestore POST /admin/page-langs/restore：解除排除（不自动重新发布）。
func (h *pagesAdminHandle) PageLangRestore(c *gin.Context) {
	h.applyPageLangChange(c, true)
}

// PageLangRepublish POST /admin/page-langs/republish：显式重新发布某语言。
//
// 为什么是**同步**触发 Build + Publish，而不是「跳到发布入口让用户自己点」或「异步入队」：
//
//   - 跳到发布页：页面列表行内没有发布按钮（发布入口在工作台），用户点了「翻译好了」还得
//     自己找路去发布 —— 一个本该一次点击的动作被拆成两次跳转；
//   - 异步入队：发布是慢操作（编译 + 落盘 + 站点文件刷新），入队的代价是**界面失去结果**，
//     要么加轮询、要么加通知，而这两样本项目都没有现成的形态；
//   - 同步两步（先 Build 再 Publish，与「一键发布全部语言」内部逐语言做的完全一样）：
//     结果当场可知（成功 → 面板刷新 + 回执；失败 → 错误文案，含具体原因），
//     复用的也是既有链路（含回执、依赖失效、互指刷新与回滚语义）。
//
// 代价写在明处：单语言发布会让这次请求等一次编译。这与既有的「一键发布全部语言」同量级，
// 不是新引入的行为；将来发布挪到队列 + 进度查询时，这里换成入队即可（面板已有结果槽）。
func (h *pagesAdminHandle) PageLangRepublish(c *gin.Context) {
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	lang := strings.TrimSpace(c.PostForm("lang"))
	errText := ""
	doneText := ""
	if lang == "" {
		errText = pageLangErrText(c, pageservice.ErrInvalidParam)
	} else if _, berr := h.pages.Build(c.Request.Context(), &pagedto.BuildReq{ID: pageID, Lang: lang}); berr != nil {
		errText = pageLangErrText(c, berr)
	} else if _, perr := h.pages.Publish(c.Request.Context(), &pagedto.PublishReq{ID: pageID, Lang: lang}); perr != nil {
		errText = pageLangErrText(c, perr)
	} else {
		doneText = shell.TranslateFor(c)("admin.page.langs.republished",
			"已重新发布该语言：产物已上线，切换器 / hreflang / sitemap 同步恢复")
	}
	h.pageLangsPanelOrJump(c, pageID, errText, doneText)
}

// applyPageLangChange 两个写入口的共同骨架：取参 → 调用 service → 出口。
//
// htmx 请求重渲面板片段，失败**不改变 HTTP 状态码**（仍是 200）：HTMX 对 4xx/5xx 默认
// 不替换目标节点，返回错误码会让运营点了按钮却什么都没发生（面板不刷新、提示也看不到）。
// 原生表单（禁 JS / 从源码提交）改走整页提示 —— 不能把裸片段当整页返回（与排定面板同一处理）。
func (h *pagesAdminHandle) applyPageLangChange(c *gin.Context, restore bool) {
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	lang := strings.TrimSpace(c.PostForm("lang"))
	errText := ""
	var err error
	if restore {
		err = h.pages.RestorePageLang(c.Request.Context(), pageID, lang)
	} else {
		_, err = h.pages.ExcludePageLang(c.Request.Context(), pageID, lang)
	}
	doneText := ""
	if err != nil {
		errText = pageLangErrText(c, err)
	} else if restore {
		doneText = shell.TranslateFor(c)("admin.page.langs.restored", "已解除排除：该语言重新参与本页的产出（重新发布走常规发布入口）")
	} else {
		doneText = shell.TranslateFor(c)("admin.page.langs.excluded", "已排除该语言并下线其产物")
	}
	h.pageLangsPanelOrJump(c, pageID, errText, doneText)
}

// pageLangsPanelOrJump 语言面板写动作的出口分档：htmx 重渲片段、原生走整页提示。
func (h *pagesAdminHandle) pageLangsPanelOrJump(c *gin.Context, pageID, errText, doneText string) {
	if !shell.IsHXRequest(c) {
		if errText != "" {
			pageListJump(c, false, errText)
			return
		}
		pageListJump(c, true, doneText)
		return
	}
	c.HTML(http.StatusOK, "fragments/page_langs", h.pageLangsPanelData(c, pageID, errText, doneText))
}

// pageLangErrText 页面级语言排除的业务错误 → 当前语言文案（带中文兜底）。
//
// 四条哨兵的取值即 i18n key（pageenums），词条缺失时**在调用点兜底**而不是显示裸 key
// （与「未接好 i18n 时 ErrXxx 直接等于中文常量」的模块口径一致）。
// 未命中（数据库原文等）一律走既有归口：原文只进日志，页面给归口文案。
func pageLangErrText(c *gin.Context, err error) string {
	tr := shell.TranslateFor(c)
	switch {
	case errors.Is(err, pageservice.ErrCannotExcludeDefaultLang):
		return tr(pageenums.ErrCannotExcludeDefaultLang,
			"不能排除站点默认语言：它的产物承载 x-default，且「默认语言无前缀」的路径映射以它为锚点")
	case errors.Is(err, pageservice.ErrPageLangExcluded):
		return tr(pageenums.ErrPageLangExcluded, "该语言已被本页排除")
	case errors.Is(err, pageservice.ErrLangAlreadyExcluded):
		return tr(pageenums.ErrLangAlreadyExcluded, "该语言已被本页排除，无需重复操作")
	case errors.Is(err, pageservice.ErrLangNotExcluded):
		return tr(pageenums.ErrLangNotExcluded, "该语言未被本页排除，无从恢复")
	}
	return pageErrPageText(c, err)
}

// pageLangsPanelData 面板片段的模板数据（键一律总是存在：片段模板按点号取 map 键，
// 缺 key 会在运行期报错并让整段片段消失）。
func (h *pagesAdminHandle) pageLangsPanelData(c *gin.Context, pageID, errText, doneText string) gin.H {
	tr := shell.TranslateFor(c)
	token, terr := builtin.GetCSRFToken(c)
	if terr != nil {
		// token 拿不到不阻断渲染：提交会被 CSRF 中间件拒（与其它后台片段一致）。
		token = ""
	}
	pid := strings.TrimSpace(pageID)
	path := ""
	rows := []pageLangRowView{}
	if pid != "" && h.pages != nil {
		states, lerr := h.pages.PageLangStates(c.Request.Context(), pid)
		if lerr != nil {
			errText = pageErrPageText(c, lerr)
		} else {
			for _, st := range states {
				row := pageLangRowView{
					Lang: st.Lang, IsDefault: st.IsDefault, Excluded: st.Excluded, Published: st.Published,
				}
				switch {
				case st.Excluded:
					row.Status = tr("admin.page.langs.status.excluded", "已排除（不产出）")
				case st.Published:
					row.Status = tr("admin.page.langs.status.published", "已发布")
				default:
					row.Status = tr("admin.page.langs.status.draft", "未发布")
				}
				if st.IsDefault {
					row.Note = tr("admin.page.langs.note.default", "站点默认语言，始终产出")
				}
				// 重新发布是**显式**动作（V4）：恢复排除只解除限制、不自动上线 ——
				// 译好了要真的回到线上，用户需要一个一次点击的入口，而不是自己去找发布页
				// （页面列表行内没有发布按钮，发布入口在工作台）。
				row.CanRepublish = !st.Excluded
				rows = append(rows, row)
			}
		}
		// 页面路径取数据库的草稿路径（详情接口），不从 query 回显 —— 查询参数不是可信边界。
		if h.pages != nil {
			if projID, perr := h.pages.ProjectOfPage(c.Request.Context(), pid); perr == nil && projID != "" {
				if detail, derr := h.pages.Detail(c.Request.Context(), &pagedto.DetailReq{ProjectID: projID, ID: pid}); derr == nil && detail != nil {
					path = detail.DraftPath
				}
			}
		}
	}
	return gin.H{
		"t":         tr,
		"CSRFToken": token,
		"PageID":    pid,
		"Path":      path,
		"Items":     rows,
		"Error":     errText,
		"Done":      doneText,
	}
}

// redirectPagePath 管理页路径（与 page_router.go 里的挂载点、迁移 218 的权限点
// api_path 三处必须一致；改一处必须改三处）。
const redirectPagePath = "/api/page/redirect"

// RedirectPage 重定向管理页：列出当前工程的全部 301 并给出增删与合并入口。
func (h *Handle) RedirectPage(c *gin.Context) {
	res, err := h.svc.ListRedirects(c.Request.Context(), &pagedto.RedirectListReq{ProjectID: c.Query("project")})
	if err != nil {
		logger.Scene("page").Error(err, "重定向列表读取失败")
		c.HTML(http.StatusInternalServerError, "admin/page/page_redirects.html", redirectPageData(c, nil, redirectErrKey(err)))
		return
	}
	c.HTML(http.StatusOK, "admin/page/page_redirects.html", redirectPageData(c, res, ""))
}

// RedirectCreate 新增重定向。
//
// 失败**留在本页并回填输入**（redirectCreateFailure）：新建表单在抽屉里，一个业务错误
// 把用户已填的源 / 目标路径清掉，比少一句错误文案贵得多 —— 这是「写表单失败时原地
// 留住输入」的例外（同 content 的文章保存，见 internal/templates/CLAUDE.md）。
// 成功走整页提示（shell.RenderJump）：结论不再经 302 + ?ok= 回带。
func (h *Handle) RedirectCreate(c *gin.Context) {
	var req pagedto.RedirectCreateReq
	if err := c.ShouldBind(&req); err != nil {
		h.redirectCreateFailure(c, &req, pageenums.RedirectErrInvalid)
		return
	}
	if _, err := h.svc.CreateRedirect(c.Request.Context(), &req); err != nil {
		h.redirectCreateFailure(c, &req, redirectErrKey(err))
		return
	}
	redirectJump(c, true, redirectOkText(c, "created"))
}

func (h *Handle) redirectCreateFailure(c *gin.Context, req *pagedto.RedirectCreateReq, errKey string) {
	res, err := h.svc.ListRedirects(c.Request.Context(), &pagedto.RedirectListReq{ProjectID: req.ProjectID})
	if err != nil {
		logger.Scene("page").Error(err, "重定向创建失败后列表读取失败")
	}
	data := redirectPageData(c, res, errKey)
	if res == nil && req.ProjectID != "" {
		data["SelectedProject"] = req.ProjectID
	}
	data["CreateSource"] = req.SourcePath
	data["CreateTarget"] = req.TargetPath
	data["CreateFailed"] = true
	c.HTML(http.StatusOK, "admin/page/page_redirects.html", data)
}

// RedirectDelete 删除一条重定向。结论走整页提示（成功 1 秒后回本页）。
func (h *Handle) RedirectDelete(c *gin.Context) {
	var req pagedto.RedirectDeleteReq
	if err := c.ShouldBind(&req); err != nil {
		redirectJump(c, false, redirectErrText(c, pageenums.RedirectErrInvalid))
		return
	}
	if err := h.svc.DeleteRedirect(c.Request.Context(), &req); err != nil {
		redirectJump(c, false, redirectErrText(c, redirectErrKey(err)))
		return
	}
	redirectJump(c, true, redirectOkText(c, "deleted"))
}

// RedirectMerge 把一条重定向的跳转链合并为直达。结论走整页提示。
func (h *Handle) RedirectMerge(c *gin.Context) {
	var req pagedto.RedirectMergeReq
	if err := c.ShouldBind(&req); err != nil {
		redirectJump(c, false, redirectErrText(c, pageenums.RedirectErrInvalid))
		return
	}
	if _, err := h.svc.MergeRedirectChain(c.Request.Context(), &req); err != nil {
		redirectJump(c, false, redirectErrText(c, redirectErrKey(err)))
		return
	}
	redirectJump(c, true, redirectOkText(c, "merged"))
}

// redirectBulkDeleteReq 批量删除的表单形状：一个工程 + 一组源路径。
// 本页一次只显示一个工程的重定向，故 project 是标量而不是每行各带一个。
type redirectBulkDeleteReq struct {
	ProjectID string   `form:"project" binding:"required"`
	Paths     []string `form:"paths" binding:"required"`
}

// RedirectBulkDelete 批量删除重定向（POST /api/page/redirect/bulk-delete）。
//
// 逐条走同一条单条删除路径（同一个 svc.DeleteRedirect：解除访问面激活 + 清占用账）：
// 「目标不存在 / 访问面不可用」这类失败只计入跳过数，整批不中断 —— 整批回滚会让
// 用户以为「一条都没删」，然后反复重试。
//
// 结果按「已删除 N 条 / 跳过 M 条」渲染进整页提示（有跳过走失败档，不自动跳）：
// 结论文案由服务端按计数重新拼装，不进 URL。
func (h *Handle) RedirectBulkDelete(c *gin.Context) {
	var req redirectBulkDeleteReq
	if err := c.ShouldBind(&req); err != nil {
		redirectJump(c, false, redirectErrText(c, pageenums.RedirectErrInvalid))
		return
	}
	deleted, skipped := 0, 0
	for _, raw := range req.Paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		if err := h.svc.DeleteRedirect(c.Request.Context(), &pagedto.RedirectDeleteReq{
			ProjectID: req.ProjectID, Path: path,
		}); err != nil {
			logger.Scene("page").With("path", path).Error(err, "批量删除重定向失败")
			skipped++
			continue
		}
		deleted++
	}
	redirectJump(c, skipped == 0 && deleted > 0, redirectBulkDeleteText(c, deleted, skipped))
}

// redirectBulkText 重定向批量删除的结论文案模板（i18n key + 中文原文，pageBulkText 同型）。
//
// 文案由服务端按计数重新拼装（见 redirectBulkDeleteText），直接渲染进整页提示，
// 不进 URL —— 所以不存在「读侧候选」这一层。
// 仍按同一套取法（pageBulkTextOf）取当前语言：否则英文界面上这四个分支永远是中文。
var (
	redirectBulkNoneSelected = pageBulkText{pageenums.BulkRedirectNoneSelected, "没有勾选任何重定向，列表未改动。"}
	redirectBulkAllDeleted   = pageBulkText{pageenums.BulkRedirectAllDeleted, "已删除 %s 条重定向。"}
	redirectBulkAllSkipped   = pageBulkText{pageenums.BulkRedirectAllSkipped, "%s 条重定向都未能删除，列表未改动。"}
	redirectBulkPartial      = pageBulkText{pageenums.BulkRedirectPartial, "已删除 %s 条，%s 条未能删除（可能已不存在或访问面不可用）。"}
)

// redirectBulkDeleteText 批量删除的结果文案（成功几条、跳过几条都要说清楚 ——
// 只报「操作完成」会把部分成功静默成全部成功，用户不会再去看剩下那几条）。
func redirectBulkDeleteText(c *gin.Context, deleted, skipped int) string {
	switch {
	case deleted == 0 && skipped == 0:
		return pageBulkTextOf(c, redirectBulkNoneSelected)
	case skipped == 0:
		return fmt.Sprintf(pageBulkTextOf(c, redirectBulkAllDeleted), strconv.Itoa(deleted))
	case deleted == 0:
		return fmt.Sprintf(pageBulkTextOf(c, redirectBulkAllSkipped), strconv.Itoa(skipped))
	default:
		return fmt.Sprintf(pageBulkTextOf(c, redirectBulkPartial), strconv.Itoa(deleted), strconv.Itoa(skipped))
	}
}

// redirectPageData 组装模板数据（i18n 与 CSRF 与后台页面同一手法：
// t 是模板取词函数，中文兜底写在模板里，真文案在 sys_i18n）。
//
// 写动作的结论不在本页回显（走 shell.RenderJump，见 page_jump.go）：ErrKey 只剩
// 「新建失败后原地回填输入」这一条来源（redirectCreateFailure），其余读侧键已删。
func redirectPageData(c *gin.Context, res *pagedto.RedirectListResp, errKey string) gin.H {
	lang := response.RequestLanguage(c)
	t := templates.TranslateFunc(lang)
	token, err := builtin.GetCSRFToken(c)
	if err != nil {
		// token 拿不到时不阻断渲染：页面照常显示，提交会被 CSRF 中间件拒（与其它后台页一致）。
		token = ""
	}
	// 全部键都预置默认值：Jet 缺键会导致渲染器返回 500 通用错误页，
	// 列表与失败回填所需的键必须由同一装配入口提供。
	data := gin.H{
		"lang":            lang,
		"t":               t,
		"csrf_token":      token,
		"Title":           t("admin.redirect.title", "重定向管理"),
		"PagePath":        redirectPagePath,
		"ErrKey":          errKey,
		"CreateSource":    "",
		"CreateTarget":    "",
		"CreateFailed":    false,
		"Projects":        []pagedto.RedirectProjectOption{},
		"Items":           []pagedto.RedirectItem{},
		"SelectedProject": "",
		"Total":           0,
		"EffectiveCount":  0,
		"MultiHopCount":   0,
		"LoopCount":       0,
	}
	if res != nil {
		data["Projects"] = res.Projects
		data["SelectedProject"] = res.ProjectID
		data["Items"] = res.Items
		data["Total"] = res.Total
		data["EffectiveCount"] = res.EffectiveCount
		data["MultiHopCount"] = res.MultiHopCount
		data["LoopCount"] = res.LoopCount
	}
	return data
}

// redirectErrKey 业务错误 → 词条 key。
func redirectErrKey(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, pageservice.ErrRedirectOccupied), errors.Is(err, pageservice.ErrPathOccupied):
		return pageenums.RedirectErrOccupied
	case errors.Is(err, pageservice.ErrRedirectTargetMiss):
		return pageenums.RedirectErrTargetMissing
	case errors.Is(err, pageservice.ErrRedirectLoop):
		return pageenums.RedirectErrLoop
	case errors.Is(err, pageservice.ErrRedirectNotFound):
		return pageenums.RedirectErrNotFound
	case errors.Is(err, pageservice.ErrRedirectUnavailable):
		return pageenums.RedirectErrUnavailable
	case errors.Is(err, pageservice.ErrInvalidParam), errors.Is(err, pageservice.ErrInvalidPath):
		return pageenums.RedirectErrInvalid
	default:
		logger.Scene("page").Error(err, "重定向操作失败")
		return pageenums.RedirectErrInternal
	}
}

// SetSchedule 排定一次到点动作（JSON）。
func (h *Handle) SetSchedule(c *gin.Context) {
	var req pagedto.ScheduleSetReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	// 发起人由服务端填，不从请求体读（审计字段不能由请求方自报）。
	req.CreateBy = int64(shell.CurrentUserID(c))
	res, err := h.svc.SetPageSchedule(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	fillScheduleFailureText(c, []*pagedto.ScheduleItem{res})
	response.SuccessWithMessage(c, pageenums.MsgScheduleSet, res)
}

// CancelSchedule 取消一条尚未执行的排定（JSON）。
func (h *Handle) CancelSchedule(c *gin.Context) {
	var req pagedto.ScheduleCancelReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	if err := h.svc.CancelPageSchedule(c.Request.Context(), &req); err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	response.SuccessWithMessage(c, pageenums.MsgScheduleCanceled, nil)
}

// ListSchedules 列出某页面的排定（JSON）。
func (h *Handle) ListSchedules(c *gin.Context) {
	var req pagedto.ScheduleListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		response.ParamError(c, pageenums.ErrInvalidParam)
		return
	}
	res, err := h.svc.ListPageSchedules(c.Request.Context(), &req)
	if err != nil {
		response.ErrorWithMessage(c, pageErrorStatus(err), pageErrorMessage(c, err))
		return
	}
	if res != nil {
		items := make([]*pagedto.ScheduleItem, 0, len(res.Items))
		for i := range res.Items {
			items = append(items, &res.Items[i])
		}
		fillScheduleFailureText(c, items)
	}
	response.Success(c, res)
}

// fillScheduleFailureText 把 last_error（业务 key）翻成当前语言文本。
//
// 未登记的 key 落到归口文案而不是裸 key：那一列的值由 service 写、读侧才知道词条，
// 两边对不上时用户看到的该是「排定执行失败」而不是一串常量名。
func fillScheduleFailureText(c *gin.Context, items []*pagedto.ScheduleItem) {
	tr := shell.TranslateFor(c)
	for _, item := range items {
		if item == nil || strings.TrimSpace(item.LastError) == "" {
			continue
		}
		item.LastErrorText = scheduleFailureText(tr, item.LastError)
	}
}

// scheduleFailureText 单条失败原因的取词（写侧只写 key，读侧统一走这里）。
func scheduleFailureText(tr func(key, fallback string) string, key string) string {
	fallback, ok := pageenums.ScheduleFailureFallbacks[key]
	if !ok {
		return tr(pageenums.ErrScheduleApplyFailed, pageenums.ScheduleFailureFallbacks[pageenums.ErrScheduleApplyFailed])
	}
	return tr(key, fallback)
}

// —— 后台面板 ——
//
// 面板挂在 /admin 页面组（Session + CSRF 已具备），写操作显式复用 API 的权限点路径
// （builtin.CasbinMiddlewareForPath），与页面列表页的行内写操作同法 ——
// 页面路径与权限点路径不一致，直接按页面路径 enforce 会因权限点表无此路径而全员 403。

// scheduleRowView 面板里的一行排定（模板字段，值都已按当前语言取词）。
type scheduleRowView struct {
	ID       int64
	Action   string
	Lang     string
	Time     string
	Status   string
	StatusID string
	Attempts int
	Note     string
	// CanCancel 只有 pending 能取消（running 已在执行、终态没有可取消的东西）。
	CanCancel bool
}

// schedulePanelData 面板片段的模板数据（gin.H，键与模板逐一对应）。
//
// 键一律**总是注入**（空串 / 空切片而不是缺失）：后台片段模板用点号访问 map 键，
// 缺 key 会在运行期报错并让整段片段消失（inert 的失败，页面上只是少了东西）。
func (h *pagesAdminHandle) schedulePanelData(c *gin.Context, pageID, errText, doneText string) gin.H {
	tr := shell.TranslateFor(c)
	// token 拿不到不阻断渲染：提交会被 CSRF 中间件拒（与其它后台页一致）。
	token, terr := builtin.GetCSRFToken(c)
	if terr != nil {
		token = ""
	}
	// 面板标题里的页面路径：取**数据库**的草稿路径（detail），而不是从 query 参数回显 ——
	// 查询参数不是可信边界（page_err.go 的读侧收口给出的正是这条判据），
	// 而这里的数据本来就有一个真源。
	path := ""
	if pageID != "" && h.pages != nil {
		if pid, perr := h.pages.ProjectOfPage(c.Request.Context(), pageID); perr == nil && pid != "" {
			if detail, derr := h.pages.Detail(c.Request.Context(), &pagedto.DetailReq{ProjectID: pid, ID: pageID}); derr == nil && detail != nil {
				path = detail.DraftPath
			}
		}
	}
	rows := []scheduleRowView{}
	if pageID != "" && h.pages != nil {
		res, lerr := h.pages.ListPageSchedules(c.Request.Context(), &pagedto.ScheduleListReq{PageID: pageID})
		if lerr != nil {
			errText = pageErrPageText(c, lerr)
		} else if res != nil {
			for i := range res.Items {
				item := res.Items[i]
				rows = append(rows, scheduleRowView{
					ID: item.ID, Action: scheduleActionText(tr, item.Action), Lang: item.Lang,
					Time:   item.ScheduledAt.Time().Local().Format("2006-01-02 15:04"),
					Status: scheduleStatusText(tr, item.Status), StatusID: item.Status,
					Attempts: item.Attempts,
					Note:     scheduleFailureText(tr, item.LastError),
					// 只取消 pending 与 running 中的前者：running 的行已经在执行了。
					CanCancel: item.Status == pagemodel.ScheduleStatusPending,
				})
			}
		}
	}
	return gin.H{
		"t":         tr,
		"CSRFToken": token,
		"PageID":    pageID,
		"Path":      path,
		"Items":     rows,
		"Error":     errText,
		"Done":      doneText,
		// 默认到点时刻 = 站点时区下的「一小时后」，省掉每次手填（运营多数排的是近期动作）。
		"DefaultAt": sitetz.FormatDateTime(time.Now().Add(time.Hour)),
	}
}

// scheduleRowTime 列表页徽标里的到点时刻（**站点时区**、到分钟）。
//
// 时区口径与面板一致（sitetz）：库里的 scheduled_at 是绝对时刻，
// 直接按 Local 格式化会在「服务器时区 ≠ 站点时区」的部署上显示成另一个时刻。
func scheduleRowTime(item *pagedto.ScheduleItem) string {
	if item == nil {
		return ""
	}
	at := item.ScheduledAt.Time()
	if at.IsZero() {
		return ""
	}
	return at.In(sitetz.Location()).Format("2006-01-02 15:04")
}

// scheduleActionText 动作文案。
func scheduleActionText(tr func(key, fallback string) string, action string) string {
	if action == pagemodel.ScheduleActionOffline {
		return tr("admin.page.schedule.action.offline", "下线")
	}
	return tr("admin.page.schedule.action.publish", "上线")
}

// scheduleStatusText 排定状态文案。
func scheduleStatusText(tr func(key, fallback string) string, status string) string {
	switch status {
	case pagemodel.ScheduleStatusRunning:
		return tr("admin.page.schedule.status.running", "执行中")
	case pagemodel.ScheduleStatusDone:
		return tr("admin.page.schedule.status.done", "已完成")
	case pagemodel.ScheduleStatusFailed:
		return tr("admin.page.schedule.status.failed", "已失败")
	case pagemodel.ScheduleStatusCanceled:
		return tr("admin.page.schedule.status.canceled", "已取消")
	default:
		return tr("admin.page.schedule.status.pending", "待执行")
	}
}

// SchedulePanel 渲染某个页面的排定面板片段（HTMX：列表页行内「定时」按钮的落点）。
func (h *pagesAdminHandle) SchedulePanel(c *gin.Context) {
	pageID := strings.TrimSpace(c.Query("pageId"))
	c.HTML(http.StatusOK, "fragments/page_schedule_panel",
		h.schedulePanelData(c, pageID, "", ""))
}

// ScheduleSet 后台表单排定（form-urlencoded），成功后重新渲染面板片段。
func (h *pagesAdminHandle) ScheduleSet(c *gin.Context) {
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	req := &pagedto.ScheduleSetReq{
		PageID:       pageID,
		Lang:         strings.TrimSpace(c.PostForm("lang")),
		Action:       strings.TrimSpace(c.PostForm("action")),
		ScheduledAt:  strings.TrimSpace(c.PostForm("scheduledAt")),
		RedirectPath: strings.TrimSpace(c.PostForm("redirectPath")),
		CreateBy:     int64(shell.CurrentUserID(c)),
	}
	doneText, errText := "", ""
	if h.pages == nil {
		errText = shell.PageInternalText(c)
	} else if _, err := h.pages.SetPageSchedule(c.Request.Context(), req); err != nil {
		errText = pageErrPageText(c, err)
	} else {
		doneText = shell.TranslateFor(c)(pageenums.MsgScheduleSet, pageenums.MsgScheduleSet)
	}
	h.renderSchedulePanel(c, pageID, errText, doneText)
}

// ScheduleCancel 后台表单取消排定。
func (h *pagesAdminHandle) ScheduleCancel(c *gin.Context) {
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	id, perr := strconv.ParseInt(strings.TrimSpace(c.PostForm("id")), 10, 64)
	doneText, errText := "", ""
	switch {
	case h.pages == nil:
		errText = shell.PageInternalText(c)
	case perr != nil || id <= 0:
		errText = pageErrPageText(c, pageservice.ErrInvalidParam)
	default:
		if err := h.pages.CancelPageSchedule(c.Request.Context(), &pagedto.ScheduleCancelReq{
			PageID: pageID, ID: id,
		}); err != nil {
			errText = pageErrPageText(c, err)
		} else {
			doneText = shell.TranslateFor(c)(pageenums.MsgScheduleCanceled, pageenums.MsgScheduleCanceled)
		}
	}
	h.renderSchedulePanel(c, pageID, errText, doneText)
}

// renderSchedulePanel 排定面板写动作的出口分档：htmx 重渲片段、原生走整页提示。
//
// 为什么非 HTMX 不渲染片段：浏览器直接 POST（禁用了 JS、或从表单源码提交）时，
// 把片段当成整页返回会得到一个没有外壳的裸片段。原生档因此改走 shell.RenderJump
// （结论走响应体，取代原先「303 回列表页、什么也不说」）。
func (h *pagesAdminHandle) renderSchedulePanel(c *gin.Context, pageID, errText, doneText string) {
	if !shell.IsHXRequest(c) {
		if errText != "" {
			pageListJump(c, false, errText)
			return
		}
		pageListJump(c, true, doneText)
		return
	}
	c.HTML(http.StatusOK, "fragments/page_schedule_panel",
		h.schedulePanelData(c, pageID, errText, doneText))
}

// translationMissRowView 报告里的一行（值已按当前语言取词）。
type translationMissRowView struct {
	PageID     string
	Path       string
	Lang       string
	Misses     int64
	Candidates int64
}

// TranslationMissesPage 缺译报告页：列出「页面 × 语言」中内容缺译的组合。
func (h *pagesAdminHandle) TranslationMissesPage(c *gin.Context) {
	// 工程：查询参数优先；只有一个工程时直接选中它（多工程且未选时给显式提示，
	// 不静默取第一个 —— 那会让人看着 A 工程的报告以为是 B 的）。
	projectID := strings.TrimSpace(c.Query("project"))
	rows := []translationMissRowView{}
	errText := ""
	if projectID == "" && h.projects != nil {
		if list, lerr := h.projects.List(c.Request.Context()); lerr == nil && len(list) == 1 {
			projectID = list[0].ID
		}
	}
	if projectID == "" {
		// 文案在调用点给中文兜底：取词 helper 是「key + 词条」，词条缺失时它会回落
		// 成裸 key（测试环境 / 新装库未 seed 时都会）—— 用户该看到一句话而不是 ErrXxx。
		errText = shell.TranslateFor(c)(pageenums.ErrProjectRequired, "请先选择站点工程：缺译报告按工程统计")
	} else if list, lerr := h.pages.UntranslatedPageLangs(c.Request.Context(), projectID); lerr != nil {
		errText = pageErrPageText(c, lerr)
	} else {
		for _, r := range list {
			rows = append(rows, translationMissRowView{
				PageID: r.PageID, Path: r.DraftPath, Lang: r.Lang,
				Misses: r.Misses, Candidates: r.Candidates,
			})
		}
	}
	c.HTML(http.StatusOK, "admin/page/page_translation_misses.html",
		h.translationMissPageData(c, projectID, rows, errText))
}

// TranslationMissCancel 把某个（页面 × 语言）取消：调 ExcludePageLang（不另写下线逻辑）。
//
// 结论走整页提示（成功 1 秒后回报告页）：不再原地重渲报告页 —— 写动作的结论统一
// 由 shell.RenderJump 呈现（见 page_jump.go）。
func (h *pagesAdminHandle) TranslationMissCancel(c *gin.Context) {
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	lang := strings.TrimSpace(c.PostForm("lang"))
	if _, err := h.pages.ExcludePageLang(c.Request.Context(), pageID, lang); err != nil {
		translationMissJump(c, false, pageLangErrText(c, err))
		return
	}
	translationMissJump(c, true, shell.TranslateFor(c)("admin.page.translation_misses.canceled",
		"已取消该语言：产物已下线，它也不再出现在切换器 / hreflang / sitemap 里"))
}

// translationMissPageData 报告页模板数据（键一律总是存在）。
func (h *pagesAdminHandle) translationMissPageData(c *gin.Context, projectID string, rows []translationMissRowView, errText string) gin.H {
	tr := shell.TranslateFor(c)
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
	return gin.H{
		"t":               tr,
		"csrf_token":      token,
		"Projects":        projects,
		"SelectedProject": projectID,
		"Rows":            rows,
		"Err":             errText,
	}
}

// 编译期用途说明：报告行的形状来自 dto（跨模块可见即入契约），这里只用它的字段。
var _ = pagedto.TranslationMissRow{}

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
		"IsDefaultLang": d.IsDefaultLang,
		"Errors":        d.Errors,
	}
}

// 筛选取值（服务端渲染，纯链接，零 JS）。
const (
	translationFilterAll     = "all"
	translationFilterMissing = "missing"
	translationFilterManual  = "manual"
	translationFilterAI      = "ai"
)

// PageTranslations GET /admin/pages/translations：翻译工作台。
func (h *pagesAdminHandle) PageTranslations(c *gin.Context) {
	pageID := strings.TrimSpace(c.Query("pageId"))
	if pageID == "" || h.pages == nil {
		c.Redirect(http.StatusSeeOther, "/admin/pages")
		return
	}
	data, err := h.buildPageTranslationsData(c.Request.Context(), pageID,
		strings.TrimSpace(c.Query("lang")), strings.TrimSpace(c.Query("filter")), shell.TranslateFor(c))
	if err != nil {
		logger.Scene("page").With("pageId", pageID).Error(err, "打开翻译工作台失败")
		c.Redirect(http.StatusSeeOther, "/admin/pages")
		return
	}
	data.SiteNote = translationMsg(c, data.SiteNote)
	c.HTML(http.StatusOK, "admin/page/page_translations", shell.Prepare(c, data.templateMap()))
}

// SavePageTranslations POST /admin/pages/translations/save：保存本页译文。
//
// 成功走整页提示（1 秒后回工作台）；**校验 / 写入失败仍原地重渲工作台并逐行报错**
// （renderTranslationError）：一个业务错误把编辑者刚填的整屏译文清掉，比少一句提示贵得多
// —— 这是「写表单失败时原地留住输入」的例外（同 content 的文章保存，
// 见 internal/templates/CLAUDE.md）。
func (h *pagesAdminHandle) SavePageTranslations(c *gin.Context) {
	ctx := c.Request.Context()
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	if pageID == "" {
		// 缺 pageId 是参数级失败（表单被裁剪 / 手拼提交）：回列表页的整页提示。
		// 原先是 c.String(400, pageenums.MsgFieldRequired) —— 响应体是 i18n 的 key 本身，
		// 用户看到内部标识符，且页面脱离页壳。
		pageListJump(c, false, pageBulkTextOf(c, pagesLocalNoticeMissingPageID))
		return
	}
	if h.pages == nil {
		pageListJump(c, false, shell.PageInternalText(c))
		return
	}
	lang := strings.TrimSpace(c.PostForm("lang"))

	page, err := h.pageOf(c, pageID)
	if err != nil {
		logger.Scene("page").With("pageId", pageID).Error(err, "读取页面失败")
		pageListJump(c, false, pageFacingOrInternal(c, err))
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
		translationJump(c, true, pageTranslationsSavedText(c, 0))
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
	// 第四步：结论走整页提示（不再经 ?saved=1&n= 回带工作台）。
	translationJump(c, true, pageTranslationsSavedText(c, written))
}

// renderTranslationError 校验/写入失败：回渲染工作台（200）并展示逐行错误，不落库。
func (h *pagesAdminHandle) renderTranslationError(c *gin.Context, pageID, lang string, errs []string) {
	data, err := h.buildPageTranslationsData(c.Request.Context(), pageID, lang, "", shell.TranslateFor(c))
	if err != nil {
		c.Redirect(http.StatusSeeOther, "/admin/pages")
		return
	}
	data.Errors = translationMsgs(c, errs)
	data.SiteNote = translationMsg(c, data.SiteNote)
	c.HTML(http.StatusOK, "admin/page/page_translations", shell.Prepare(c, data.templateMap()))
}

// pagesAdminHandle 页面列表与翻译工作台的页面处理器（从 dashboard 回迁）。
// 只持有本页面需要的契约；构造见 NewPagesAdminHandle。
type pagesAdminHandle struct {
	pages    pagecontract.PageService
	projects projectcontract.ProjectService
	// blocks 全局块契约（只读）：翻译工作台收集页眉/页脚绑定与 globalref 内的候选。
	blocks blockcontract.BlockService
	// blueprints 蓝图候选（新建页面时的空白草稿模板）。
	// 可空：端口未注入时表单不显示蓝图选项，建页照常（blueprintOptions 返回空切片）。
	blueprints blueprintcontract.BlueprintService
	// siteIndex 全站可翻译内容索引缓存（跨页面复用提示 + 全站完成度，见
	// page_translations_index.go）。
	siteIndex siteContentIndexCache
	// contentStore 内容译文读写端口（翻译工作台）。
	// 为 nil 时按默认实现（pkg/i18n.ContentWriter + 默认数据库）惰性构造；
	// 测试经 SetContentTranslationStore 注入隔离 schema 的写入器。
	contentStore contentTranslationPort
}

// NewPagesAdminHandle 创建页面列表 / 翻译工作台处理器；各契约为对应模块 contract。
func NewPagesAdminHandle(pages pagecontract.PageService, projects projectcontract.ProjectService,
	blocks blockcontract.BlockService, blueprints blueprintcontract.BlueprintService) *pagesAdminHandle {
	return &pagesAdminHandle{pages: pages, projects: projects, blocks: blocks, blueprints: blueprints}
}

// PagesAdminHandle 导出类型别名：外部测试包（public/test/page/feature）需要命名
// 构造器返回的句柄类型；别名指向未导出类型是合法 Go，读起来也明确指向后者。
type PagesAdminHandle = pagesAdminHandle

// SetBlueprints 注入蓝图契约（装配期调用；可空）。
func (h *pagesAdminHandle) SetBlueprints(b blueprintcontract.BlueprintService) { h.blueprints = b }

// blueprintOptions 拉取蓝图候选；蓝图端口未注入或查询失败时返回空列表
// （表单不显示蓝图选项，建页照常走空白草稿）。
func (h *pagesAdminHandle) blueprintOptions(ctx context.Context) []blueprintOption {
	if h.blueprints == nil {
		return nil
	}
	list, err := h.blueprints.List(ctx, &blueprintdto.ListReq{})
	if err != nil {
		logger.Scene("page").With("err", err).Warn("蓝图列表读取失败，新建页面表单不显示蓝图选项")
		return nil
	}
	out := make([]blueprintOption, 0, len(list))
	for _, b := range list {
		out = append(out, blueprintOption{ID: b.ID, Name: b.Name})
	}
	return out
}

// pagesPageData 页面列表页数据。
// 模板键统一小写（admin/layout.html 以 {{.title}}/{{.menu}} 取值，
// Jet 对 map 键不做大小写兜底）。
type pagesPageData struct {
	Title    string
	Menu     string
	Projects []projectcontract.ProjectResp
	Pages    []pageRow
	// Blueprints 蓝图候选（审计 VIS-010）：「从蓝图开始」是新建页面流程里的一个选项，
	// 不是另一个需要先去的页面。
	Blueprints []blueprintOption
	// Err 是**列表取数失败**的提示条（写动作的结论走 shell.RenderJump，不再回带）。
	// 批量结果按「已删除 N 个页面 / 跳过 M 个」渲染进提示页，不在这里回显。
	Err string

	// 发布回执收敛的只读观测（本轮接入）：待收敛条数 / 最老一条已等待多久 / 本进程最近一次
	// 收敛时刻。列表页是运维每天的落点，积压只写在日志与 /readyz 里等于不可见 ——
	// /readyz 又不参与就绪判定，没有人会因为它去看。
	//
	// ReceiptAlert 由条数派生（> 0），模板据此在「警告条」与「正常」之间分流：
	// 把判断留在 Go 侧，模板不必为 int64 与字面量的类型匹配操心（Jet 的比较要求同型）。
	ReceiptPending      int64
	ReceiptOldest       string
	ReceiptLastConverge string
	ReceiptAlert        bool
	// ReceiptKnown 这份回执观测**这次请求真的读到了**（false = 本页装载失败、走降级渲染）。
	//
	// 为什么需要它：降级渲染时 ReceiptPending 等是零值，而模板会把「零积压」
	// 渲染成「发布回执收敛正常」—— 那是**错误的乐观断言**（这一页根本没读到回执状态）。
	// 判据在 handler 算好，模板只读一个布尔（与 project 域主题页的 NoProjectEmpty 同形：
	// 模板是磁盘热读文件，Go 侧改动要等重编译，判据写在模板里会随两边不同步而漂移）。
	ReceiptKnown bool

	// SelectedProject 当前聚焦的站点工程 id（筛选栏下拉的回显值；无工程时为空串）。
	SelectedProject string
	// FilteredProject 用户是否**指定**了工程（?project= 命中工程列表）。
	//
	// 空态据此分档：指定了工程却没页面 → 「这个站点工程还没有页面」（下一步是换工程或就地建页），
	// 否则是「还没有页面」（下一步是建第一个页面）。判据留在 Go 侧：模板是磁盘热读文件，
	// 判据写在模板里会随两边不同步而漂移（与 ReceiptKnown 同一理由）。
	FilteredProject bool

	// StaleOverview 全站待重建区块的渲染数据（只读观测，取数见 staleOverview）。
	//
	// 三种状态由**键 + Available** 一起表达，模板只读它们、不做取数：
	//   · nil（键不存在）→ 本次请求没装配这份数据（整页装载失败走降级渲染）：整块不渲染，
	//     顶部已有归口提示（Err），不在这里重复第二遍；
	//   · Available=false → 读不到（ListStalePages 失败）：显示「本次读不到」，
	//     绝不显示成「0 个待重建」（那会把一次读取失败渲染成「一切正常」）；
	//   · Available=true → 由 Total 分流「折叠清单」与「当前没有待重建的页面」。
	//
	// 为什么与 ReceiptKnown 的形态不同（那里总是给键、用布尔分流）：那份观测只有「读到 / 没读到」
	// 两种状态；这份还有「读到了但是空」这一种，而 nil 切片与空切片在模板里长得一样 ——
	// 必须由比较列表多一个 Available 才能分开。两者共用的判据是**降级渲染时不得给出乐观结论**。
	StaleOverview gin.H
}

// blueprintOption 新建页面表单里的蓝图选项。
type blueprintOption struct {
	ID   string
	Name string
}

// templateMap 转为模板所需的小写键 map。
func (d *pagesPageData) templateMap() gin.H {
	return gin.H{
		"title":      d.Title,
		"menu":       d.Menu,
		"Projects":   d.Projects,
		"Pages":      d.Pages,
		"Blueprints": d.Blueprints,
		"Err":        d.Err,

		"ReceiptPending":      d.ReceiptPending,
		"ReceiptOldest":       d.ReceiptOldest,
		"ReceiptLastConverge": d.ReceiptLastConverge,
		"ReceiptAlert":        d.ReceiptAlert,
		"ReceiptKnown":        d.ReceiptKnown,

		"SelectedProject": d.SelectedProject,
		"FilteredProject": d.FilteredProject,

		// 全站待重建区块（可选键）：字段为 nil 时这里输出 nil，模板的 isset 判为假
		//（Jet 的 isset 同时覆盖「键不存在」与「值为 nil」两种情形）→ 整块不渲染。
		"StaleOverview": d.StaleOverview,
	}
}

// pageRow 列表行投影（含状态文案）。
type pageRow struct {
	ID        string
	ProjectID string
	Kind      string
	DraftPath string
	Active    bool
	Staged    bool
	Stale     bool
	Version   int64
	UpdatedAt string

	// 定时上下线（PIPE-7）的行内投影：有待执行排定时显示「已排定 + 到点时刻」，
	// 有失败排定时显示「排定失败 + 原因」。
	//
	// 判据与文案都在 Go 侧算好（模板是磁盘热读文件，判据写进模板会随两边不同步而漂移）：
	// 时间已按**站点时区**格式化，失败原因已按当前语言取词 —— 模板只输出这两个字符串。
	SchedulePendingAt  string
	ScheduleFailedNote string
	// ScheduleAlert 是否有需要运营看一眼的失败排定（模板据此选徽标样式）。
	ScheduleAlert bool
}

// PagesList 页面列表页。
//
// 装载失败**降级渲染**（空列表 + 归口提示，HTTP 200）：页面结构必须保留 ——
// 换菜单、去别的页面、刷新重试都还得能用。原先这里是 500 + `response.ErrorWithMessage`
// （一块 JSON），浏览器停在 JSON 上，用户既看不到列表也无从判断「是这一页没读出来、
// 还是整个后台坏了」。原文只进日志（pageErrPageText）。
func (h *pagesAdminHandle) PagesList(c *gin.Context) {
	data, err := h.buildPagesData(c)
	if err != nil {
		data = &pagesPageData{
			Title: pageenums.MsgPagesTitle, Menu: "pages",
			// 装载失败是**这次请求**真实发生的事，提示条据此渲染。
			Err: pageErrPageText(c, err),
			// ReceiptKnown 留 false：这次没读到回执状态，模板不该报「收敛正常」。
		}
	}
	c.HTML(http.StatusOK, "admin/page/pages", shell.Prepare(c, data.templateMap()))
}

// buildPagesData 组装列表页数据。
//
// 页面按主题浏览（020_themes.sql：主题下面才是页面）：取当前聚焦工程的
// 激活主题过滤页面；无工程或无主题时 themeID 为空列全部页面。
//
// 「当前聚焦工程」由筛选栏的 ?project=<id> 决定（审计 02-L §2 P1-12）：这一页此前只能看
// 第一个工程的页面，工程一多就无从切换。维度取自 service —— page Service.ListReq.ProjectID
// 本来就支持它，缺的只是「有人从 query 读它」。未指定 / 指定的工程不在列表里（陈旧链接、
// 手改的 URL、刚被删）时回退到第一个工程：一条过期的 URL 不该把整页变成错误页。
func (h *pagesAdminHandle) buildPagesData(c *gin.Context) (*pagesPageData, error) {
	ctx := c.Request.Context()
	projects, err := h.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	// 第一步：定位当前聚焦工程（筛选栏的输入）。
	projectID, filteredProject := focusProjectID(projects, c.Query("project"))
	// 第二步：取该工程的激活主题。
	themeID := ""
	if projectID != "" {
		if theme, err := h.projects.GetActiveTheme(ctx, projectID); err == nil && theme != nil {
			themeID = theme.ID
		}
	}
	// 第三步：按聚焦工程与激活主题取页面。
	pages, err := h.pages.List(ctx, &pagecontract.ListReq{ProjectID: projectID, ThemeID: themeID})
	if err != nil {
		return nil, err
	}
	// 排定投影（PIPE-7）：一次批量取回这批页面的待执行 / 最近失败记录。
	// 读取失败只记日志、页面照常渲染：与回执观测同一口径 ——
	// 一个附加观测不该让整张列表页 500（缺的是两个徽标，不是列表本身）。
	schedules := map[string]pagecontract.SchedulePageSummary{}
	pageIDs := make([]string, 0, len(pages))
	for i := range pages {
		pageIDs = append(pageIDs, pages[i].ID)
	}
	if len(pageIDs) > 0 {
		got, serr := h.pages.ListSchedulesForPages(ctx, pageIDs)
		if serr != nil {
			logger.Scene("page").Error(serr, "读取页面排定投影失败（列表页不显示定时徽标）")
		} else {
			schedules = got
		}
	}
	rows := make([]pageRow, 0, len(pages))
	for _, p := range pages {
		row := pageRow{
			ID: p.ID, ProjectID: p.ProjectID, Kind: p.Kind,
			DraftPath: p.DraftPath, Active: p.ActiveArtifactID != nil,
			Staged: p.StagedArtifactID != nil, Stale: p.Stale,
			Version: p.DraftVersion, UpdatedAt: p.UpdatedAt.Time().Format("2006-01-02 15:04"),
		}
		if summary, ok := schedules[p.ID]; ok {
			row.SchedulePendingAt = scheduleRowTime(summary.Pending)
			if summary.Failed != nil {
				row.ScheduleAlert = true
				row.ScheduleFailedNote = scheduleFailureText(shell.TranslateFor(c), summary.Failed.LastError)
			}
		}
		rows = append(rows, row)
	}
	// 待收敛回执观测（只读）：读取失败只记日志，页面照常渲染 ——
	// 一个观测字段不该让整张列表页 500。
	receiptPending, receiptOldest, receiptLast := h.receiptBacklog(ctx)
	// 全站待重建区块（只读）：取数作用域是**全部站点工程**，与上面按工程聚焦的页面列表
	// 不是一个数（块 / 文章 / 主题 / 词条改动影响的是全站）。读不到时它自己给失败态，
	// 同样不让整张列表页失败。
	stale := h.staleOverview(ctx, shell.TranslateFor(c))
	return &pagesPageData{
		Title: pageenums.MsgPagesTitle, Menu: "pages",
		Projects: projects, Pages: rows,
		// 筛选栏的两个键：SelectedProject 供下拉回显，FilteredProject 供空态分档
		//（「这个工程还没有页面」≠「全站还没有页面」，两者的下一步动作不同）。
		SelectedProject: projectID,
		FilteredProject: filteredProject,
		// 蓝图候选（审计 VIS-010）：把「从蓝图开始」放进新建页面流程，
		// 而不是要求编辑者先去另一个页面建好蓝图再回来。
		Blueprints: h.blueprintOptions(ctx),

		ReceiptPending:      receiptPending,
		ReceiptOldest:       receiptOldest,
		ReceiptLastConverge: receiptLast,
		ReceiptAlert:        receiptPending > 0,
		// 走到这里说明本页数据装配完成（回执观测读不到只记日志、不给零值以外的信号，
		// 见 receiptBacklog），所以这条观测条可以展示。
		ReceiptKnown: true,

		// 全站待重建区块：非 nil 即「本次装配到了这份数据」，读到与否由 Available 表达。
		StaleOverview: stale,
	}, nil
}

// focusProjectID 从工程列表里挑出当前聚焦的工程 id。
//
// want 来自筛选栏的 ?project=（`<select name="project">` 提交的 get 参数）。
// 命中即采纳；未命中（空串 / 伪造 id / 刚被删的工程）回退到第一个工程 —— 那是本页既有的
// 默认语义，回退而不是报错是因为：一条过期的 URL 不该把整页变成错误页，用户要的是列表。
//
// 第二个返回值表示「用户的指定真的被采纳了」：模板据此把空态分成两档
// （指定了工程却没页面，与全站还没有页面，下一步动作不同）。判据放这里而不是模板里，
// 是因为模板里的「有没有筛过」只能靠 query 猜，而这里同时知道 query 与工程列表。
func focusProjectID(projects []projectcontract.ProjectResp, want string) (id string, filtered bool) {
	want = strings.TrimSpace(want)
	if want != "" {
		for i := range projects {
			if projects[i].ID == want {
				return projects[i].ID, true
			}
		}
	}
	if len(projects) == 0 {
		return "", false
	}
	return projects[0].ID, false
}

// receiptBacklogObserver 收敛积压的只读观测（page service 实现）。
//
// 用隐式接口而不是扩 pagecontract：可观测不是跨模块能力（与 routers 侧的
// pendingReceiptBacklog 同一判据），契约扩一次会让所有测试替身跟着实现一遍，
// 而这只服务列表页上的一个状态条。
type receiptBacklogObserver interface {
	PendingReceiptBacklog(ctx context.Context) (pending int64, oldestAge time.Duration, lastConvergeAt time.Time, err error)
}

// receiptBacklog 读取待收敛回执的观测值，转成可直接渲染的三元文本。
//
// 未实现观测（测试替身 / 降级装配）与读取失败都返回零值：状态条退化成「正常」那一支，
// 列表页本身不受影响。失败只记日志 —— 这是观测，不是页面数据
// （原文不进模板，见 AGENTS.md 的错误文案三件套）。
func (h *pagesAdminHandle) receiptBacklog(ctx context.Context) (pending int64, oldest, last string) {
	observer, ok := h.pages.(receiptBacklogObserver)
	if !ok {
		return 0, "", ""
	}
	count, oldestAge, lastConvergeAt, err := observer.PendingReceiptBacklog(ctx)
	if err != nil {
		logger.Scene("page").With("err", err).Warn("读取待收敛发布回执失败（列表页不显示回执状态）")
		return 0, "", ""
	}
	if count > 0 && oldestAge > 0 {
		oldest = formatReceiptAge(oldestAge)
	}
	if !lastConvergeAt.IsZero() {
		last = lastConvergeAt.Local().Format("2006-01-02 15:04:05")
	}
	return count, oldest, last
}

// formatReceiptAge 把等待时长压成人读的一行（秒 / 分 / 小时 / 天）。
//
// 不做 i18n：单位是 SI 记法（s/m/h/d），中英文都读得懂，也不需要复数规则 ——
// 为它造四条词条只会让词条表更长，不带来任何可读性。
func formatReceiptAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

// staleOverviewLimit 「全站待重建」区块一次列出的页面数。
//
// 这是**消费者口径**，所以定义在调用方：ListStalePages 的 limit 由调用方给
// （service 不写死业务条件），不填时才落到 model 的 DefaultStaleListLimit = 50 ——
// 一份 50 行的折叠清单会把页面列表顶出首屏，而那正是审计 02-L P1-10 记录的原缺陷
// （只读影响面卡占了列表主位）。
//
// 数为什么是 8：只读区块的作用是「让人看见影响面」，不是完整清单；被截断的条数由 Total
// 给出并在页面上显式说明。service 里那份同名同值的未导出常量已随之删除（它没有任何调用方，
// 留着会让「这个数由谁定」出现两个答案）。
const staleOverviewLimit = 8

// staleOverviewItem 「全站待重建」清单的一行。
//
// 跨工程清单必须带工程名：每个工程都可能有一个一模一样的 /about，只给路径分不清是哪一个
// （页面列表页本身有「所属工程」上下文，这一块没有）。
type staleOverviewItem struct {
	ID          string
	Path        string
	ProjectName string
	// Published 是否已上线（有活跃产物路径）：用来区分「已发布但有更新未发布」与「从未上线」——
	// 后者的处置方式不同（重建也还不会出现在访问面，要先发布）。
	Published bool
	// FailedNote 最近一次自动重建失败的一句文案（空串 = 没有失败痕迹）。
	// 非空即「这页不是还没轮到，而是重建失败过」—— 与 stale 徽标合起来才能回答
	// 「为什么它还在这儿」。
	FailedNote string
}

// staleOverview 取「全站待重建」区块的数据（只读观测）。
//
// 作用域：ProjectID 传空 = **全部站点工程**。这与本页下方列表的口径不同 —— 列表是单工程聚焦
// （focusProjectID 从 ?project= 解析），而块 / 文章 / 主题 / 词条改动影响的是全站。
// 两个数不是同一个，模板侧把「全站」写进标题与说明。
//
// 失败不降级成空清单：ListStalePages 的语义是「读不到即失败」，这里把它翻成 Available=false
// （模板显示「本次读不到」），**绝不渲染成「0 个待重建」**—— 「影响面 0」与「读不到影响面」
// 混在一起，会让一次读取失败在页面上看起来像一切正常，运维再也不会去看
// （判据与发布回执观测的 ReceiptKnown 一致）。原文只进日志。
//
// 唯一的例外是「一个站点工程都没有」：ListStalePages 按语义返回 ErrProjectRequired
// （没有可作用域的工程），而那时全站确实没有任何页面 —— 那是确定的事实，不是读取失败，
// 按空态处理（此时下方列表也正落在「还没有页面」那一档）。
// tr 由调用方传入（取词只在 handler 层做）：本函数要组装「最近一次重建失败」的文案，
// 而它自己拿不到 gin.Context。
func (h *pagesAdminHandle) staleOverview(ctx context.Context, tr func(key, fallback string) string) gin.H {
	empty := gin.H{
		"Available": true, "Total": 0, "Pages": []staleOverviewItem{},
		// Limit 给真实口径（模板在空态下不读它，但零值会让「清单上限是多少」在两个分支里
		// 出现两种答案 —— 将来若空态也要说一句「最多列 8 条」，零值就是错的）。
		"Limit": staleOverviewLimit, "Truncated": false,
	}
	unavailable := gin.H{
		"Available": false, "Total": 0, "Pages": []staleOverviewItem{},
		"Limit": 0, "Truncated": false,
	}
	if h == nil || h.pages == nil {
		// 契约未注入（降级装配、或只覆盖写路径的测试句柄）：与「读不到」同形，
		// 但没有错误可记，静默给失败态即可。
		return unavailable
	}
	res, err := h.pages.ListStalePages(ctx, &pagecontract.StalePageListReq{
		// 条数由本页给（不填会落到 model 的 50 条默认），排序按标记时间倒序：
		// 「最近这次改动影响的」排在最前（service 文件头对这个消费者的描述就是这个次序）。
		Limit:      staleOverviewLimit,
		Descending: true,
	})
	if err != nil {
		if errors.Is(err, pageservice.ErrProjectRequired) {
			return empty
		}
		logger.Scene("page").With("err", err).Warn("读取全站待重建清单失败（页面列表页的该区块显示为不可用）")
		return unavailable
	}
	if res == nil {
		// 契约返回 (nil, nil) 是异常形态：按「读不到」处理，不给「0 个待重建」的假结论。
		logger.Scene("page").Warn("全站待重建清单返回空结果（契约实现异常）")
		return unavailable
	}
	items := make([]staleOverviewItem, 0, len(res.Pages))
	for i := range res.Pages {
		items = append(items, staleOverviewItem{
			ID:          res.Pages[i].ID,
			Path:        strings.TrimSpace(res.Pages[i].Path),
			ProjectName: strings.TrimSpace(res.Pages[i].ProjectName),
			Published:   res.Pages[i].Published,
			FailedNote:  rebuildFailureNote(tr, res.Pages[i].RebuildFailedStage, res.Pages[i].RebuildFailedAt),
		})
	}
	return gin.H{
		"Available": true,
		"Total":     res.Total,
		"Pages":     items,
		"Limit":     res.Limit,
		"Truncated": res.Truncated,
	}
}

// CreateProject 新建站点工程。
//
// 出口：结论由 shell.RenderJump 渲染成整页提示（见 page_jump.go）—— 失败不自动跳
// （运营要看清楚原因），成功 1 秒后自动回列表页。表单是原生
// `<form method="post" action="/admin/projects/create">`（admin/pages.html 的
// #tpl-project-create 抽屉，不是 hx-post）。
//
// 为什么不是 `c.String(400, …)`：那会把用户导航到一块只有一行字的页面上，
// 抽屉、页壳、他刚填的名称一并丢失。
func (h *pagesAdminHandle) CreateProject(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		pageListJump(c, false, pageBulkTextOf(c, pagesLocalNoticeProjectNameRequired))
		return
	}
	if _, err := h.projects.Create(c.Request.Context(), &projectcontract.CreateReq{
		Name: name, Settings: json.RawMessage("{}"),
	}); err != nil {
		logger.Scene("page").With("name", name).Error(err, "创建站点工程失败")
		// 页面路径的文案出口：业务 sentinel 翻成中文，其余落归口文案
		//（原文只进日志 —— 上面那条日志已带 name，这里用不记日志的变体，免得同一错误记两遍）。
		pageListJump(c, false, pageFacingOrInternal(c, err))
		return
	}
	pageListJump(c, true, pageProjectCreatedText(c))
}

// CreatePage 新建页面（默认空白草稿，创建后可进工作台编辑）。
//
// 出口同 CreateProject：原生表单 + 整页提示（取代原先的 303 + ?err=）。
func (h *pagesAdminHandle) CreatePage(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	path := strings.TrimSpace(c.PostForm("draftPath"))
	// 两条必填分开报，不合成一句「项目与页面路径不能为空」：合成句把「没选工程」
	// 与「没填路径」说成同一件事，而两者的修法完全不同（选择器 vs 输入框）。
	if projectID == "" {
		pageListJump(c, false, pageFacingKey(c, pageenums.ErrProjectRequired))
		return
	}
	if path == "" {
		pageListJump(c, false, pageBulkTextOf(c, pagesLocalNoticePathRequired))
		return
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// 默认空白草稿：layout.mode 为编译端必填校验项（full/boxed）。
	// 选了蓝图则以蓝图为准（审计 VIS-010）：page.Create 会用 InitPageDocument 复制
	// 蓝图 AST 并重生成节点 ID，这里传的空白文档只是「没选蓝图」时的兜底。
	if _, err := h.pages.Create(c.Request.Context(), &pagecontract.CreateReq{
		ProjectID:         projectID,
		Kind:              "home",
		ContentTargetType: "none",
		DraftPath:         path,
		DraftDocument:     json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[]}`),
		BlueprintID:       strings.TrimSpace(c.PostForm("blueprintId")),
	}); err != nil {
		logger.Scene("page").With("projectId", projectID).With("path", path).Error(err, "创建页面失败")
		pageListJump(c, false, pageFacingOrInternal(c, err))
		return
	}
	pageListJump(c, true, pageCreatedText(c))
}

// DeletePage 单条删除页面（POST /admin/pages/delete）。
//
// 语义**逐字复用** API 的 /api/page/delete（同一个 svc.Delete）：页面有已激活产物时
// 不拒绝，而是先按 active 路径把访问面下线、清掉路由占用与媒体引用，再软删页面
// （见 service/page_delete.go）。批量删除必须走这同一条路径 —— 单条拒绝 / 批量跳过的
// 规则都由 service 决定，handler 不另立一套。
func (h *pagesAdminHandle) DeletePage(c *gin.Context) {
	id := strings.TrimSpace(c.PostForm("id"))
	if id == "" {
		pageListJump(c, false, pageBulkTextOf(c, pagesLocalNoticeMissingID))
		return
	}
	if err := h.pages.Delete(c.Request.Context(), &pagecontract.DeleteReq{ID: id}); err != nil {
		logger.Scene("page").With("pageId", id).Error(err, "删除页面失败")
		// 页面路径的文案出口：业务 sentinel 翻成中文，其余落归口文案（原文只进日志）。
		pageListJump(c, false, pageFacingOrInternal(c, err))
		return
	}
	pageListJump(c, true, pageDeletedText(c))
}

// PagesBulkDelete 批量删除页面（POST /admin/pages/bulk-delete）。
//
// 逐条走同一条单条删除路径：某一条失败（已不存在、路径清理失败等）只计入跳过数，
// 整批不中断 —— 整批回滚会让用户以为「一个都没删」，然后反复重试。
// 结果按「已删除 N 个 / 跳过 M 个」渲染进整页提示，不静默部分成功。
//
// 有跳过（或一条都没处理）走失败档（OK:false、不自动跳）：运营要看清剩下哪些没删掉；
// 全部成功才 1 秒后自动回列表页。
func (h *pagesAdminHandle) PagesBulkDelete(c *gin.Context) {
	// 批量 id 统一入口（去空白 / 去重 / 上限）：超限整批拒绝并说明原因，不静默截断。
	ids, berr := shell.BulkIDs(c)
	if berr != nil {
		// 受控提示（一次最多操作 N 项）走 shell 的受控文案出口（类型判定，不认文本）。
		pageListJump(c, false, shell.BulkIDsFacingText(c, berr))
		return
	}
	deleted, skipped := 0, 0
	for _, id := range ids {
		if err := h.pages.Delete(c.Request.Context(), &pagecontract.DeleteReq{ID: id}); err != nil {
			logger.Scene("page").With("pageId", id).Error(err, "批量删除页面失败")
			skipped++
			continue
		}
		deleted++
	}
	pageListJump(c, skipped == 0 && deleted > 0, pagesBulkDeleteResult(c, deleted, skipped))
}

// pagesBulkDeleteResult 批量删除的结果文案：成功几个、跳过几个都要说清楚
// （只报「操作完成」会把部分成功静默成全部成功，用户不会再去看剩下那几个）。
func pagesBulkDeleteResult(c *gin.Context, deleted, skipped int) string {
	switch {
	case deleted == 0 && skipped == 0:
		return pageBulkTextOf(c, pagesBulkResultTemplates[0])
	case skipped == 0:
		return fmt.Sprintf(pageBulkTextOf(c, pagesBulkResultTemplates[1]), strconv.Itoa(deleted))
	case deleted == 0:
		return fmt.Sprintf(pageBulkTextOf(c, pagesBulkResultTemplates[2]), strconv.Itoa(skipped))
	default:
		return fmt.Sprintf(pageBulkTextOf(c, pagesBulkResultTemplates[3]), strconv.Itoa(deleted), strconv.Itoa(skipped))
	}
}

// rebuildFailureNote 组装「最近一次自动重建失败」的一句文案；没有失败痕迹时返回空串。
//
// 阶段与时刻来自迁移 474 落在 pages 上的两列；**不含错误原文**（后台页面不得直出内部错误，
// 原文只进结构化日志，带 page_id 可定位）。与 /admin/articles、/admin/blocks 上同名函数
// 是三份：各自在自己的模块包内（跨模块共用要走契约，而这是纯展示装配）。
// 三处文案与阶段取值必须一致，改一处请同步另两处。
func rebuildFailureNote(tr func(key, fallback string) string, stage string, at *utils.JSONTime) string {
	if strings.TrimSpace(stage) == "" && at == nil {
		return ""
	}
	label := stage
	switch stage {
	case "plan":
		label = tr("admin.pages.impact.stage_plan", "计划阶段（站点语言清单或旧发布范围读不到）")
	case "build":
		label = tr("admin.pages.impact.stage_build", "构建 / 发布阶段")
	}
	prefix := tr("admin.pages.impact.rebuild_failed", "最近一次自动重建失败：")
	if at == nil {
		return prefix + label
	}
	return prefix + label + "（" + time.Time(*at).Local().Format("2006-01-02 15:04") + "）"
}

const (
	// siteSlotPageTitle 页面标题（**i18n key**：shell.Prepare 会对 "title" 取词；
	// 词条 admin.site_slots.title 已存在，值即原来的「系统页面」）。
	siteSlotPageTitle = "admin.site_slots.title"
	// siteSlotSitePrefix 访问面静态站点的公开前缀（routes.go 把 ActiveRoot 挂在 /site）。
	// 页面发布记录的 active_path 是站点内逻辑路径（含语言前缀，以 "/" 开头），
	// 拼上这个前缀才是浏览器能打开的地址。
	siteSlotSitePrefix = "/site"
	// siteSlotEmptyField 空字段的展示占位（表格里的空白单元格读不出「没有值」）。
	siteSlotEmptyField = "—"
	// 绑定 / 解绑的成功回执（经 shell.RenderJump 渲染进提示页，也登记在
	// siteSlotFacingMessages 里供取词兜底）。
	// 常量值是 **i18n key**，中文兜底在同文件的 siteSlotFacingMessages —— 两者必须成对，
	// 取词统一走 siteSlotText。
	siteSlotBoundText   = "admin.site_slots.ok.bound"
	siteSlotUnboundText = "admin.site_slots.ok.unbound"
	// 参数级错误（本页自造；同样登记白名单 —— 自造文案不登记取词就没有中文兜底）。
	siteSlotNoProjectText = "admin.site_slots.err.noProject"
	siteSlotNoPageText    = "admin.site_slots.err.noPage"
	// 绑定 / 换绑的话术：动作与服务端完全一致（bind 是 upsert），
	// 差别只在「这会改掉现有绑定」要不要说出来。两处取词同源（行内操作列与绑定面板）。
	siteSlotBindAction   = "admin.site_slots.action.bind"
	siteSlotRebindAction = "admin.site_slots.action.rebind"
)

// siteSlotTrs 取词函数的可选变参：不传时回落「原样返回兜底」。
//
// 为什么用变参而不是加一个必填参数：siteSlotPageData / siteSlotFacingText 被同包测试
// 直接调用（page_page_err_test.go、site_slot_facing_test.go），加必填参数会波及那些
// 调用点 —— 而它们要验的是装配结果与判定语义，与语言无关。
// （同先例：NewBlockPageHandle 的 pages 可选变参。）
func siteSlotTr(trs []func(key, fallback string) string) func(key, fallback string) string {
	if len(trs) > 0 && trs[0] != nil {
		return trs[0]
	}
	return func(_, fallback string) string { return fallback }
}

// siteSlotText 一条本页文案的当前语言文本（key + 表内中文兜底）。
func siteSlotText(c *gin.Context, key string) string {
	return shell.TranslateFor(c)(key, siteSlotFacingMessages[key])
}

// siteSlotFacingMessages 本页可以原样展示给运营的文案（白名单）。
//
// **键是 i18n key**（两类都是）：page 模块错误常量的值就是常量名、而常量名即词条 key；
// 本页自造的成功 / 参数级文案的常量值也已经是 key（见上面的常量块）。值是**中文兜底**，
// 与词条缺失时页面上显示的那句话逐字相同。
//
// 用常量做键而不是手抄字符串：page 模块调整常量值时这里跟着一起变，不会静默失配。
var siteSlotFacingMessages = map[string]string{
	pageenums.ErrInvalidParam:    "提交的信息不完整或格式不对，请回到列表页重新操作。",
	pageenums.ErrProjectNotFound: "站点工程不存在，请回到列表页重新选择工程。",
	pageenums.ErrPageNotFound:    "要绑定的页面不存在，可能已被删除。",
	pageenums.ErrInvalidSlot:     "槽位键不在白名单内，请回到列表页重新选择。",
	pageenums.ErrSlotPageMiss:    "要绑定的页面不存在、已被删除，或不属于当前工程。",
	siteSlotBoundText:            "已绑定。该工程的页面已标记待重建，重新构建那些页面后新链接才会生效。",
	siteSlotUnboundText:          "已解绑。该工程的页面已标记待重建，重新构建那些页面后访问面才会去掉旧链接。",
	siteSlotNoProjectText:        "没有可用的站点工程：先去页面管理里建一个工程，槽位是挂在工程下的。",
	siteSlotNoPageText:           "请先在下拉里选一个页面再提交。",
}

// siteSlotStateLabels 槽位状态标签：状态键 → {i18n key, 中文兜底}。
var siteSlotStateLabels = map[string]struct{ Key, Fallback string }{
	"unbound":     {"admin.site_slots.state.unbound", "未绑定"},
	"deleted":     {"admin.site_slots.state.deleted", "绑定已失效"},
	"published":   {"admin.site_slots.state.published", "已发布"},
	"unpublished": {"admin.site_slots.state.unpublished", "已绑定但未发布"},
}

// siteSlotStateLabel 槽位状态 → 当前语言标签。
func siteSlotStateLabel(tr func(key, fallback string) string, state string) string {
	item := siteSlotStateLabels[state]
	return tr(item.Key, item.Fallback)
}

// siteSlotPageHandle 系统页面槽位页处理器。
type siteSlotPageHandle struct {
	pages    pagecontract.PageService
	projects projectcontract.ProjectService
}

// NewSiteSlotPageHandle 构造。
func NewSiteSlotPageHandle(pages pagecontract.PageService, projects projectcontract.ProjectService) *siteSlotPageHandle {
	return &siteSlotPageHandle{pages: pages, projects: projects}
}

// SiteSlotsPage 系统页面槽位页（GET /admin/site-slots）。
//
// 工程列表装载失败**降级渲染**（空列表 + 归口提示，HTTP 200，与 project 域主题页、
// admin 六页同一判据）：侧栏、页头、概览、菜单全部保留，运营看得出「是这一页没读出来」，
// 还能换菜单、刷新重试。原先这里是 `c.String(500, …)` —— 浏览器里只剩一块纯文本，
// 用户既改不了也退不回（AGENTS.md 形态 ①）。原文只进日志（siteSlotFacingError）。
func (h *siteSlotPageHandle) SiteSlotsPage(c *gin.Context) {
	ctx := c.Request.Context()
	tr := shell.TranslateFor(c)
	projects, err := h.projects.List(ctx)
	if err != nil {
		c.HTML(http.StatusOK, "admin/page/site_slots.html",
			shell.Prepare(c, siteSlotPageData(nil, "", nil, nil, siteSlotFacingError(c, err), tr)))
		return
	}
	selected := strings.TrimSpace(c.Query("project"))
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}

	// 写动作的结论不在本页回显（走 shell.RenderJump，见 page_jump.go）：Err 只剩
	// **取数失败**这一条来源（工程列表 / 页面候选 / 槽位清单读不到）。
	pageErr := ""

	// 绑定面板的槽位参数：行内那条「绑定 / 换绑」链接带过来的（GET 回本页 + #slot-bind-panel 锚点）。
	// 这里只取值，渲染与否由 siteSlotApplyBindPanel 对照槽位清单判定 —— 手拼的 slot 一律不渲染。
	bindSlot := strings.TrimSpace(c.Query("slot"))

	candidates := []pagecontract.PageResp{}
	var slots *pagecontract.SiteSlotListResp

	if selected != "" {
		// 页面下拉候选：themeID 传空串 = 列全部页面（与页面管理页按激活主题过滤不同 ——
		// 槽位要指向任何一页，包括还没挂主题的）。
		list, lerr := h.pages.List(ctx, &pagecontract.ListReq{ProjectID: selected})
		if lerr != nil {
			pageErr = siteSlotFirstNonEmpty(pageErr, siteSlotFacingError(c, lerr))
		} else {
			candidates = list
		}

		// Lang 留空 = 站点默认语言：这里要的是「访问面上这一页在哪」，
		// 与后台界面语言（zh-CN / en-US）是两件事，不能把界面语言当站点语言传进去。
		resp, serr := h.pages.ListSiteSlots(ctx, &pagecontract.SiteSlotListReq{ProjectID: selected})
		if serr != nil {
			pageErr = siteSlotFirstNonEmpty(pageErr, siteSlotFacingError(c, serr))
		} else {
			slots = resp
		}
	}

	data := siteSlotPageData(projects, selected, candidates, slots, pageErr, tr)
	siteSlotApplyBindPanel(data, candidates, slots, bindSlot, tr)
	c.HTML(http.StatusOK, "admin/page/site_slots.html", shell.Prepare(c, data))
}

// siteSlotPageData 组装渲染数据（纯函数：不取数、不依赖 gin.Context）。
//
// 取词函数以可选变参传入（不传时按中文兜底渲染）：键名与计数口径只在这里定义一次，
// 真实渲染测试可以直接喂数据走同一条组装路径，而不必构造 gin.Context 去抽语言。
//
// 为什么抽出来：渲染键名与计数口径只在这里定义一次，真实渲染测试可以直接喂数据走
// 同一条组装路径，而不是在测试里手抄一份键名 —— 手抄的那一份会随模板演进静默失配，
// 而那正是「页面上少了一块、断言却通过」的成因。
func siteSlotPageData(projects []projectcontract.ProjectResp, selected string,
	candidates []pagecontract.PageResp, slots *pagecontract.SiteSlotListResp,
	pageErr string, trs ...func(key, fallback string) string) gin.H {
	tr := siteSlotTr(trs)
	rows := make([]gin.H, 0)
	boundCount, total, unpublished, deleted := 0, 0, 0, 0
	if slots != nil {
		total, boundCount = slots.Total, slots.BoundCount
		for _, it := range slots.Items {
			// 计数口径：绑了但页面已删（悬空）与绑了但没发布（降级）分开数，
			// 前者要换绑或解绑，后者要发布或换绑 —— 两件事，不能合成一个数字。
			switch {
			case it.PageDeleted:
				deleted++
			case it.Bound && !it.Published:
				unpublished++
			}
			rows = append(rows, siteSlotRow(tr, it, candidates))
		}
	}
	return gin.H{
		"title":           siteSlotPageTitle,
		"menu":            "site-slots",
		"Projects":        projects,
		"SelectedProject": selected,
		"Rows":            rows,
		// 工程里一个页面都没有时，绑定表单不渲染空下拉（空下拉只会让运营点一个必然失败的提交）。
		"HasPages":         len(candidates) > 0,
		"NoPages":          len(candidates) == 0,
		"BoundCount":       boundCount,
		"Total":            total,
		"UnpublishedCount": unpublished,
		"DeletedCount":     deleted,
		"Err":              pageErr,
		// NoProjectEmpty 「还没有站点工程」空态：工程列表为空 **且** 本次没有出错。
		//
		// 不能只判 len(Projects) == 0：装载失败时工程列表同样是空的，但那时该显示的是
		// 错误条，而不是「先去页面管理建一个工程」—— 用户明明有工程，是这一页没读出来，
		// 引导他去建一个已存在的工程是比没有提示更坏的结果（同一判据见 project 域主题页的
		// themeManageData.NoProjectEmpty）。
		//
		// 判据在 handler 算好、模板只读一个布尔：模板是磁盘热读文件，而 Go 侧改动要等
		// air 重编译，两边短暂不同步 —— 缺键直接参与判断会让**整页中断**
		//（HTTP 200 + 后面整块 HTML 消失），所以模板侧一律 isset 包裹。
		"NoProjectEmpty": len(projects) == 0 && pageErr == "",
	}
}

// siteSlotApplyBindPanel 把「绑定面板」需要的键补进渲染数据（就地改 data）。
//
// 行内不再内嵌页面下拉（每行一份 13 项候选 → N 行 N×13 个 option 节点，操作列还被下拉撑宽），
// 改成「点这一行 → GET 回本页带 slot 参数 → 表格下方的面板里选页面」：候选只渲染一份，
// 面板本身是原生 form + 原生 select，无 JS 也成立，鼠标 / 滚轮与触摸板 / 触屏 / 键盘都走原生路径。
//
// 为什么不并进 siteSlotPageData：那个函数被同包的渲染测试直接调用（page_page_err_test.go），
// 改签名会波及清单外的文件 —— 所以面板单独一步装配，既有调用点一个都不动。
//
// 三种情况一律不渲染面板（fail closed，不给一个点了必然失败的下拉）：
//   - 没带 slot 参数（常态浏览列表）；
//   - slot 不在这个工程的槽位清单里（手拼 URL 猜槽位键）；
//   - 这个工程一个页面都没有（没有候选可绑 —— 与列表页「不给空下拉」同一判据）。
func siteSlotApplyBindPanel(data gin.H, candidates []pagecontract.PageResp,
	slots *pagecontract.SiteSlotListResp, bindSlot string, trs ...func(key, fallback string) string) {
	if bindSlot == "" || slots == nil || len(candidates) == 0 {
		return
	}
	tr := siteSlotTr(trs)
	for _, it := range slots.Items {
		if it.Slot != bindSlot {
			continue
		}
		data["BindSlot"] = it.Slot
		data["BindSlotName"] = tr(it.SlotName, pageenums.SiteSlotName(it.SlotName))
		data["BindLabel"] = siteSlotBindLabel(tr, it.Bound)
		data["BindDeleted"] = it.PageDeleted
		data["BindPageOptions"] = siteSlotPageOptions(candidates, it.PageID)
		// 行内那条链接据此标 aria-current：读屏用户能听出「面板正开着的是这一行」。
		if rows, ok := data["Rows"].([]gin.H); ok {
			for _, row := range rows {
				if slot, _ := row["Slot"].(string); slot == it.Slot {
					row["Active"] = true
				}
			}
		}
		return
	}
}

// siteSlotBindLabel 绑定 / 换绑的话术（已绑定时说的是「这会改掉现有绑定」）。
func siteSlotBindLabel(tr func(key, fallback string) string, bound bool) string {
	if bound {
		return tr(siteSlotRebindAction, "换绑")
	}
	return tr(siteSlotBindAction, "绑定")
}

// SiteSlotBind 绑定 / 换绑（POST /admin/site-slots/bind）。
//
// 绑定与换绑是同一个动作（服务端是 upsert），因此只有一条路由：
// 表单只表达「这个槽位绑到哪一页」，绑第二次就是把绑定改成另一个页面。
// 结论走整页提示（成功 1 秒后回本页）—— 不再经 302 + ?ok= / ?err= 回显。
func (h *siteSlotPageHandle) SiteSlotBind(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	if pageID == "" {
		siteSlotJump(c, false, siteSlotText(c, siteSlotNoPageText))
		return
	}
	err := h.pages.BindSiteSlot(c.Request.Context(), &pagecontract.SiteSlotBindReq{
		ProjectID: projectID,
		Slot:      strings.TrimSpace(c.PostForm("slot")),
		PageID:    pageID,
	})
	if err != nil {
		siteSlotJump(c, false, siteSlotFacingError(c, err))
		return
	}
	siteSlotJump(c, true, siteSlotText(c, siteSlotBoundText))
}

// SiteSlotUnbind 解绑（POST /admin/site-slots/unbind）。
//
// 解绑是幂等的（本来没绑也返回成功），也是页面已删那种悬空绑定唯一的收场方式。
// 结论走整页提示。
func (h *siteSlotPageHandle) SiteSlotUnbind(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("projectId"))
	err := h.pages.UnbindSiteSlot(c.Request.Context(), &pagecontract.SiteSlotUnbindReq{
		ProjectID: projectID,
		Slot:      strings.TrimSpace(c.PostForm("slot")),
	})
	if err != nil {
		siteSlotJump(c, false, siteSlotFacingError(c, err))
		return
	}
	siteSlotJump(c, true, siteSlotText(c, siteSlotUnboundText))
}

// —— 页面取数（视图组装：模板不做判断与算术）——

// siteSlotRow 一个槽位 → 模板视图。
//
// 四种状态在这里定型，模板只做分支渲染：
//
//	unbound      未绑定（正常状态，中性徽章）；
//	published    已绑定且已发布（链接生成方会输出链接）；
//	unpublished  已绑定但未发布（**降级状态**：链接生成方跳过它，必须显眼）；
//	deleted      绑定的页面已被删除（**必须立刻修**，红色 + 解绑入口）。
func siteSlotRow(tr func(key, fallback string) string, it pagecontract.SiteSlotItem, candidates []pagecontract.PageResp) gin.H {
	row := gin.H{
		"Slot":        it.Slot,
		"SlotName":    tr(it.SlotName, pageenums.SiteSlotName(it.SlotName)),
		"Usage":       tr(it.Usage, pageenums.SiteSlotUsage(it.Usage)),
		"Bound":       it.Bound,
		"Unbound":     !it.Bound,
		"PageDeleted": it.PageDeleted,
		"PageID":      it.PageID,
		"DraftPath":   siteSlotTextOrEmpty(it.DraftPath),
		"Path":        it.Path,
		"PublicURL":   siteSlotPublicURL(it.Path),
		"Published":   it.Published,
		// Unpublished 单独给一个布尔：它就是「运营以为配好了、实际链接生成方会跳过」的那一类。
		"Unpublished": it.Bound && !it.PageDeleted && !it.Published,
		"BindLabel":   siteSlotBindLabel(tr, it.Bound),
		"PageOptions": siteSlotPageOptions(candidates, it.PageID),
	}
	switch {
	case !it.Bound:
		row["Badge"], row["StateLabel"] = "badge-mute", siteSlotStateLabel(tr, "unbound")
	case it.PageDeleted:
		row["Badge"], row["StateLabel"] = "badge-danger", siteSlotStateLabel(tr, "deleted")
	case it.Published:
		row["Badge"], row["StateLabel"] = "badge-success", siteSlotStateLabel(tr, "published")
	default:
		row["Badge"], row["StateLabel"] = "badge-warning", siteSlotStateLabel(tr, "unpublished")
	}
	return row
}

// siteSlotPageOptions 页面下拉候选（value = 页面 id，显示文本 = 草稿路径，当前绑定预先选中）。
func siteSlotPageOptions(pages []pagecontract.PageResp, selectedID string) []gin.H {
	out := make([]gin.H, 0, len(pages))
	for _, p := range pages {
		out = append(out, gin.H{
			"ID":       p.ID,
			"Label":    siteSlotTextOrEmpty(p.DraftPath),
			"Selected": p.ID == selectedID,
		})
	}
	return out
}

// —— 表单与文案工具 ——

// siteSlotFacingError 把 page 模块的错误转成可展示文案。
//
// 命中白名单的（本页知道怎么解释的业务错误）返回中文原文，其余一律落到统一提示：
// 未命中的通常是数据库错误的 Error()，带表名甚至 SQL 片段，那是给运维看的。
func siteSlotFacingError(c *gin.Context, err error) string {
	if err == nil {
		return ""
	}
	if msg := siteSlotFacingText(err.Error(), shell.TranslateFor(c)); msg != "" {
		return msg
	}
	// 未命中：原文只进日志（场景 + user_id + 原始错误），对外给归口文案。
	// 少了这一条，未归类的失败在日志与页面上**同时消失** —— 页面看不到、日志也查不到。
	logger.Scene(pageErrScene).
		With("user_id", shell.CurrentUserID(c)).
		Error(err, "站点槽位页操作失败（非业务错误，只对外给归口文案）")
	return siteSlotInternalText(c)
}

// siteSlotFacingText 白名单校验：入参是 page 模块错误常量的**值**（= 常量名，也是 i18n key）。
//
// 命中 → 该 key 的当前语言译文（缺词条回落 map 里的中文兜底）；未命中 → 空串，
// 由 siteSlotFacingError 落归口文案。
//
// 历史上这里还认「已经转好的成品文案」（读侧回显 ?err= 时写侧给的是译文）—— 那条通道
// 随「结论走 shell.RenderJump」删除：现在唯一的调用点是 siteSlotFacingError，入参恒为
// service 错误的 Error()（常量名形态）。
//
// trs 为可选取词函数（不传时原样返回兜底文案，见 siteSlotTr）。
func siteSlotFacingText(raw string, trs ...func(key, fallback string) string) string {
	key := strings.TrimSpace(raw)
	if key == "" {
		return ""
	}
	fallback, ok := siteSlotFacingMessages[key]
	if !ok || fallback == "" {
		return ""
	}
	return siteSlotTr(trs)(key, fallback)
}

// siteSlotInternalText 统一内部错误文案（走当前语言的译文，缺词条回退中文原文）。
func siteSlotInternalText(c *gin.Context) string {
	return shell.PageInternalText(c)
}

// siteSlotPublicURL 站点内逻辑路径 → 浏览器可打开的访问面地址。
//
// active_path 理论上恒带前导 "/"，这里仍做一次归一：拼出 "/sitecheckout"
// 这种地址的错法是静默的（链接能渲染、点了才 404），不值得押注上游格式。
func siteSlotPublicURL(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return ""
	}
	return siteSlotSitePrefix + "/" + strings.TrimPrefix(p, "/")
}

// siteSlotTextOrEmpty 空值统一显示成「—」。
func siteSlotTextOrEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return siteSlotEmptyField
	}
	return value
}

// siteSlotFirstNonEmpty 取第一个非空文案（本页多处「错误提示只留第一条」的收口）。
func siteSlotFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// 批量回执的 query 槽位（noticeBulk* / pageBulkNoticeDone / pageBulkNoticeErr /
// bulkNoticeQueryOf / pageBulkNoticeText）随「结论走 shell.RenderJump」整批删除：
// 结论文案现在直接渲染进响应体，不再经 URL 回带，读侧判定随之不需要。
// 结论文案本身（pagesBulkDeleteResult / redirectBulkDeleteText）保留在 page_err.go
// 与上面的 redirectBulkText 里。

// 块内文本的来源标签（工作台行徽章）：值是 **i18n key**，取词在 buildPageTranslationsData。
const (
	translationOriginHeader = "admin.page_translations.origin.header"
	translationOriginFooter = "admin.page_translations.origin.footer"
	translationOriginBlock  = "admin.page_translations.origin.block"
)

// translationOriginFallback 来源徽章的中文兜底（词条缺失时显示的原文）。
//
// 与 key 一一对应；这里是三处唯一的兜底来源，取词点只按 key 查这张表 ——
// 各写一份兜底的后果是「缺词条时显示什么」变成第二份真相。
var translationOriginFallback = map[string]string{
	translationOriginHeader: "页眉块",
	translationOriginFooter: "页脚块",
	translationOriginBlock:  "全局块",
}

// translationOriginText 来源标签 → 当前语言文案（key + 中文兜底）。
func translationOriginText(tr func(key, fallback string) string, origin string) string {
	if origin == "" {
		return ""
	}
	return tr(origin, translationOriginFallback[origin])
}

// pageTranslationsTr 取词函数的可选变参：不传时原样返回兜底文案。
//
// 用变参而不是必填参数：buildPageTranslationsData 被同包测试直接调用，
// 那些用例验的是分组 / 筛选 / 完成度口径，与语言无关（同先例：siteSlotTr）。
func pageTranslationsTr(trs []func(key, fallback string) string) func(key, fallback string) string {
	if len(trs) > 0 && trs[0] != nil {
		return trs[0]
	}
	return func(_, fallback string) string { return fallback }
}

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
		origin := translationOriginBlock
		switch slot {
		case builder.SlotHeader:
			origin = translationOriginHeader
		case builder.SlotFooter:
			origin = translationOriginFooter
		}
		walk(slotBindings[slot], origin)
	}
	for _, ref := range builder.ReferencedBlockIDs(page.Root) {
		walk(ref, translationOriginBlock)
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

// buildPageTranslationsData 组装工作台数据。
//
// 步骤：页面草稿 → builder.CollectContentCandidates（与构建期同源）→
// 现有译文（含 engine）→ 全站索引（复用提示 + 全站完成度）→ 按组件分组 + 筛选。
func (h *pagesAdminHandle) buildPageTranslationsData(ctx context.Context, pageID, wantLang, filter string,
	trs ...func(key, fallback string) string) (*pageTranslationsData, error) {
	// 取词函数可选（不传时按中文兜底渲染）：来源徽章的三条 key + 兜底在
	// page_translations_blocks.go，本函数只负责把 key 翻成当前语言。
	tr := pageTranslationsTr(trs)
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
			row.Origin = translationOriginText(tr, blockInfo.origin[key])
		}
		if info, hit := details[key]; hit && info.TargetText != "" {
			row.Target = info.TargetText
			row.Engine = info.Engine
			row.Translated = true
		}
		if site != nil {
			row.ReusePages, row.ReuseTotal, row.ReuseHint = reuseHint(site.reusePathsOf(key), page.DraftPath, tr)
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

const (
	// siteContentIndexTTL 全站索引缓存有效期（后台页面可接受的陈旧窗口）。
	siteContentIndexTTL = 30 * time.Second
	// siteContentScanPageLimit 全站扫描的页数上限：超过则跳过全站统计。
	siteContentScanPageLimit = 1000
	// siteContentReuseHintMax 复用提示里最多列出的页面路径数（超出用「等 N 个」）。
	siteContentReuseHintMax = 6
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

// reuseHint 组装复用提示文案（行内提示 + 展开用页面路径）。
//
// 返回 (其他页面数, 该文本出现的页面总数, 展开文案)。仅本页出现时返回 0。
func reuseHint(paths []string, currentPath string, trs ...func(key, fallback string) string) (others int, total int, hint string) {
	tr := pageTranslationsTr(trs)
	total = len(paths)
	if total <= 1 {
		return 0, total, ""
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
	shown := paths
	if len(shown) > siteContentReuseHintMax {
		shown = shown[:siteContentReuseHintMax]
	}
	hint = joinPaths(tr, shown)
	if len(paths) > len(shown) {
		hint = hint + tr(pageenums.ReuseMoreSuffix, " 等页面")
	}
	return others, total, hint
}

// joinPaths 以当前语言的分隔符连接页面路径（模板侧不再做拼接逻辑）。
//
// 分隔符也要取词：中文用「、」，英文用「, 」—— 写死全角顿号会让英文界面上
// 出现一整串中文标点串起来的路径。
func joinPaths(tr func(key, fallback string) string, paths []string) string {
	sep := tr(pageenums.ReusePathSeparator, "、")
	out := ""
	for i, p := range paths {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}
