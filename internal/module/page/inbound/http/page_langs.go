package pagehttp

// 文案：四条业务错误在调用点给中文兜底（pageLangErrText），未命中才落到既有归口
// （pageErrPageText：结构化日志 + adminui.PageInternalText）。**不直出 err.Error()**，
// 也**不直出内部错误**。

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	"go_wp/internal/middleware/builtin"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	"go_wp/pkg/reqparams"
)

// PageLangsPanel GET /admin/page-langs/panel?pageId=…：某页的语言产出范围面板（HTMX 片段）。
func (h *pagesAdminHandle) PageLangsPanel(c *gin.Context) {
	c.HTML(http.StatusOK, "fragments/page_langs", h.pageLangsPanelData(c, c.Query("pageId"), pageMsg{}, pageMsg{}))
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
	var errMsg, doneMsg pageMsg
	if lang == "" {
		errMsg = pageErrMsg(c, pageservice.ErrInvalidParam)
	} else if _, berr := h.pages.Build(c.Request.Context(), &pagedto.BuildReq{ID: pageID, Lang: lang}); berr != nil {
		errMsg = pageErrMsg(c, berr)
	} else if _, perr := h.pages.Publish(c.Request.Context(), &pagedto.PublishReq{ID: pageID, Lang: lang}); perr != nil {
		errMsg = pageErrMsg(c, perr)
	} else {
		doneMsg = pageMsg{Key: pageenums.LangRepublished,
			Fallback: "已重新发布该语言：产物已上线，切换器 / hreflang / sitemap 同步恢复"}
	}
	h.pageLangsPanelOrJump(c, pageID, errMsg, doneMsg)
}

// applyPageLangChange 两个写入口的共同骨架：取参 → 调用 service → 出口。
//
// htmx 请求重渲面板片段，失败**不改变 HTTP 状态码**（仍是 200）：HTMX 对 4xx/5xx 默认
// 不替换目标节点，返回错误码会让运营点了按钮却什么都没发生（面板不刷新、提示也看不到）。
// 原生表单（禁 JS / 从源码提交）改走整页提示 —— 不能把裸片段当整页返回（与排定面板同一处理）。
func (h *pagesAdminHandle) applyPageLangChange(c *gin.Context, restore bool) {
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	lang := strings.TrimSpace(c.PostForm("lang"))
	var err error
	if restore {
		err = h.pages.RestorePageLang(c.Request.Context(), pageID, lang)
	} else {
		_, err = h.pages.ExcludePageLang(c.Request.Context(), pageID, lang)
	}
	var errMsg, doneMsg pageMsg
	if err != nil {
		errMsg = pageErrMsg(c, err)
	} else if restore {
		doneMsg = pageMsg{Key: pageenums.LangRestored,
			Fallback: "已解除排除：该语言重新参与本页的产出（重新发布走常规发布入口）"}
	} else {
		doneMsg = pageMsg{Key: pageenums.LangExcluded, Fallback: "已排除该语言并下线其产物"}
	}
	h.pageLangsPanelOrJump(c, pageID, errMsg, doneMsg)
}

// pageLangsPanelOrJump 语言面板写动作的出口分档：htmx 重渲片段、原生走整页提示。
func (h *pagesAdminHandle) pageLangsPanelOrJump(c *gin.Context, pageID string, errMsg, doneMsg pageMsg) {
	if !reqparams.IsHXRequest(c) {
		if errMsg.Key != "" || errMsg.Fallback != "" {
			pageListJump(c, false, errMsg)
			return
		}
		pageListJump(c, true, doneMsg)
		return
	}
	c.HTML(http.StatusOK, "fragments/page_langs",
		h.pageLangsPanelData(c, pageID, errMsg, doneMsg))
}

// pageLangsPanelData 面板片段的模板数据（键一律总是存在：片段模板按点号取 map 键，
// 缺 key 会在运行期报错并让整段片段消失）。
//
// errMsg / doneMsg 是**事实**（key + 中文兜底 + 参数），取词由片段模板做 ——
// 数据在返回前经 adminui.PrepareBare 注入 t / tf（片段不走 Prepare，见该函数注释）。
func (h *pagesAdminHandle) pageLangsPanelData(c *gin.Context, pageID string, errMsg, doneMsg pageMsg) gin.H {
	token, terr := builtin.GetCSRFToken(c)
	if terr != nil {
		// token 拿不到不阻断渲染：提交会被 CSRF 中间件拒（与其它后台片段一致）。
		token = ""
	}
	pid := strings.TrimSpace(pageID)
	path := ""
	rows := []pagedto.PageLangResp{}
	if pid != "" && h.pages != nil {
		states, lerr := h.pages.PageLangStates(c.Request.Context(), pid)
		if lerr != nil {
			errMsg = pageErrMsg(c, lerr)
		} else {
			for _, st := range states {
				row := pagedto.PageLangResp{
					Lang: st.Lang, IsDefault: st.IsDefault, Excluded: st.Excluded, Published: st.Published,
				}
				// 三档状态的取值与文案都归 pageenums（LangStatusOf 派生取值、
				// LangStatusLabel 给 key + 兜底）：控制器不再自己挑文案。
				row.StatusKey, row.StatusFallback = pageenums.LangStatusLabel(
					pageenums.LangStatusOf(st.Excluded, st.Published))
				if st.IsDefault {
					row.NoteKey, row.NoteFallback = pageenums.LangNoteDefault, "站点默认语言，始终产出"
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
	return adminui.PrepareBare(c, gin.H{
		"CSRFToken": token,
		"PageID":    pid,
		"Path":      path,
		"Items":     rows,
		"Err":       errMsg,
		"Done":      doneMsg,
	})
}
