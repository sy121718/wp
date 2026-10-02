package pagehttp

// page_translation_miss_handle.go — 缺译报告（U2）。
//
// 与站点级准入（U1，project.SaveLocales 的界面词条门槛）的分工写在这里与页面上：
// **U1 拦「这个语言整体没准备好」，U2 处理「语言准备好了、但某些页面的内容没译」**。
//
// 操作列的「取消该语言」直接调 `ExcludePageLang`（上一批实现）——不另写一套下线逻辑：
// 取消 = 下线该语言产物 + 清发布/暂存/路由/计划（同一事务）+ 写排除列，
// 那套语义（以及它与切换器 / hreflang / sitemap 的一致性）已经在那一处验证过。
//
// 挂 `/api/page` 路由组（与 `/api/page/redirect` 同一形态）：本页是**自足外壳**的后台页，
// 不依赖 layout 的侧栏导航；权限点复用 page:list（读）与 page:publish（写）。

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	"go_wp/internal/web/shell"
)

// translationMissRowView 报告里的一行（值已按当前语言取词）。
type translationMissRowView struct {
	PageID     string
	Path       string
	Lang       string
	Misses     int64
	Candidates int64
}

// TranslationMissesPage 缺译报告页：列出「页面 × 语言」中内容缺译的组合。
func (h *Handle) TranslationMissesPage(c *gin.Context) {
	// 工程：查询参数优先；缺省由调用方（页面组 handler）在 URL 上带，缺失时报告页
	// 直接把「需要显式工程作用域」摆出来，而不是静默取第一个工程（多工程下那会看错站点）。
	projectID := strings.TrimSpace(c.Query("project"))
	rows := []translationMissRowView{}
	errText := ""
	if projectID == "" {
		// 文案在调用点给中文兜底：读侧的 helper 是「key + 词条」，词条缺失时它会回落
		// 成裸 key（测试环境 / 新装库未 seed 时都会）—— 用户该看到一句话而不是 ErrXxx。
		errText = shell.TranslateFor(c)(pageenums.ErrProjectRequired, "请先选择站点工程：缺译报告按工程统计")
	} else if list, lerr := h.svc.UntranslatedPageLangs(c.Request.Context(), projectID); lerr != nil {
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
		h.translationMissPageData(c, projectID, rows, errText, strings.TrimSpace(c.Query("done"))))
}

// TranslationMissCancel 把某个（页面 × 语言）取消：调 ExcludePageLang（不另写下线逻辑）。
func (h *Handle) TranslationMissCancel(c *gin.Context) {
	projectID := strings.TrimSpace(c.PostForm("project"))
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	lang := strings.TrimSpace(c.PostForm("lang"))
	errText := ""
	if _, err := h.svc.ExcludePageLang(c.Request.Context(), pageID, lang); err != nil {
		errText = pageLangErrText(c, err)
	}
	rows := []translationMissRowView{}
	if list, lerr := h.svc.UntranslatedPageLangs(c.Request.Context(), projectID); lerr == nil {
		for _, r := range list {
			rows = append(rows, translationMissRowView{
				PageID: r.PageID, Path: r.DraftPath, Lang: r.Lang,
				Misses: r.Misses, Candidates: r.Candidates,
			})
		}
	}
	done := ""
	if errText == "" {
		done = "canceled"
	}
	c.HTML(http.StatusOK, "admin/page/page_translation_misses.html",
		h.translationMissPageData(c, projectID, rows, errText, done))
}

// translationMissPageData 报告页模板数据（键一律总是存在）。
func (h *Handle) translationMissPageData(c *gin.Context, projectID string, rows []translationMissRowView, errText, done string) gin.H {
	tr := shell.TranslateFor(c)
	token, terr := builtin.GetCSRFToken(c)
	if terr != nil {
		token = ""
	}
	doneText := ""
	if done == "canceled" {
		doneText = tr("admin.page.translation_misses.canceled", "已取消该语言：产物已下线，它也不再出现在切换器 / hreflang / sitemap 里")
	}
	return gin.H{
		"t":               tr,
		"csrf_token":      token,
		"SelectedProject": projectID,
		"Rows":            rows,
		"Err":             errText,
		"Done":            doneText,
	}
}

// 编译期用途说明：报告行的形状来自 dto（跨模块可见即入契约），这里只用它的字段。
var _ = pagedto.TranslationMissRow{}
