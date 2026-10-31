package pagehttp

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	pagedto "go_wp/internal/module/page/dto"
	pageenums "go_wp/internal/module/page/enums"
	pageservice "go_wp/internal/module/page/service"
	"go_wp/pkg/binding"
	"go_wp/pkg/logger"
)

func (h *pagesAdminHandle) PageLangExclude(c *gin.Context) {
	h.applyPageLangChange(c, false)
}

func (h *pagesAdminHandle) PageLangRestore(c *gin.Context) {
	h.applyPageLangChange(c, true)
}

func (h *pagesAdminHandle) PageLangRepublish(c *gin.Context) {
	req, ok := binding.Form[pagedto.PageLangFormReq](c)
	if !ok {
		return
	}
	pageID := strings.TrimSpace(req.PageID)
	lang := strings.TrimSpace(req.Lang)
	var errMsg, doneMsg pageMsg
	if lang == "" {
		errMsg = pageErrMsg(c, pageservice.ErrInvalidParam)
	} else if rerr := h.republishLang(c.Request.Context(), pageID, lang); rerr != nil {
		errMsg = pageErrMsg(c, rerr)
	} else {
		doneMsg = pageMsg{Key: pageenums.LangRepublished,
			Fallback: "已重新发布该语言：产物已上线，切换器 / hreflang / sitemap 同步恢复"}
	}
	msg := doneMsg
	if !errMsg.Empty() {
		msg = errMsg
	}
	pageJump(c, errMsg.Empty(), msg, pageTranslationsBack(c, pageID, lang), translationBackMsg)
}

func (h *pagesAdminHandle) republishLang(ctx context.Context, pageID, lang string) error {
	if _, err := h.pages.Build(ctx, &pagedto.BuildReq{ID: pageID, Lang: lang}); err != nil {
		return err
	}
	_, err := h.pages.Publish(ctx, &pagedto.PublishReq{ID: pageID, Lang: lang})
	return err
}

func (h *pagesAdminHandle) applyPageLangChange(c *gin.Context, restore bool) {
	req, ok := binding.Form[pagedto.PageLangFormReq](c)
	if !ok {
		return
	}
	pageID := strings.TrimSpace(req.PageID)
	lang := strings.TrimSpace(req.Lang)
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
	msg := doneMsg
	if !errMsg.Empty() {
		msg = errMsg
	}
	pageJump(c, errMsg.Empty(), msg, pageTranslationsBack(c, pageID, lang), translationBackMsg)
}

func (h *pagesAdminHandle) pageLangStatesOf(ctx context.Context, pageID string) ([]PageLangResp, pageMsg) {
	if h.pages == nil {
		return []PageLangResp{}, pageMsg{}
	}
	states, err := h.pages.PageLangStates(ctx, pageID)
	if err != nil {
		logger.Scene("page").With("pageId", pageID).Error(err, "读取页面语言产出状态失败")
		return []PageLangResp{}, pageInternalMsg()
	}
	rows := make([]PageLangResp, 0, len(states))
	for _, st := range states {
		row := PageLangResp{
			Lang: st.Lang, IsDefault: st.IsDefault, Excluded: st.Excluded, Published: st.Published,
		}

		row.StatusKey, row.StatusFallback = pageenums.LangStatusLabel(
			pageenums.LangStatusOf(st.Excluded, st.Published))
		if st.IsDefault {
			row.NoteKey, row.NoteFallback = pageenums.LangNoteDefault, "站点默认语言，始终产出"
		}

		row.CanRepublish = !st.Excluded
		rows = append(rows, row)
	}
	return rows, pageMsg{}
}
