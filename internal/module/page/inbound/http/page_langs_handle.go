package pagehttp

// page_langs_handle.go — 页面级语言排除的后台面板（迁移 491）。
//
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

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	"go_wp/internal/web/shell"
)

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
	c.HTML(http.StatusOK, "fragments/page_langs", h.pageLangsPanelData(c, pageID, errText, doneText))
}

// applyPageLangChange 两个写入口的共同骨架：取参 → 调用 service → 重渲染面板。
//
// 失败**不改变 HTTP 状态码**（仍是 200 的片段）：HTMX 对 4xx/5xx 默认不替换目标节点，
// 返回错误码会让运营点了按钮却什么都没发生（面板不刷新、提示也看不到）——
// 与排定面板同一处理。
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
