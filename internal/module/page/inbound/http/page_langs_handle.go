package pagehttp

// 页面级语言产出范围的三条写动作（排除 / 解除排除 / 重新发布）。
//
// **入口在多语言工作台**（/admin/pages/translations）：产出范围与内容是同一件事的
// 两个面 —— 「这一页出不出这门语言」和「这一页这门语言的文字是什么」。
// 从前它们在页面列表操作列里各占一格（「语言」/「多语言」），运营要记住哪个管哪件；
// 而列表页那格按钮的实际表现是「点了没反应」（data-when 拿布尔比字面量、
// 行里没有 pageId、裸 tr 塞进 div），且同一份数据同时有 JSON 与 HTML 片段两个出口。
// 现在只留一处：工作台里的「产出范围」区块，服务端渲染，无局部刷新。
//
// 出口是**整页提示**（shell.RenderJump），成功后回工作台同一个 pageId + lang ——
// 写的是工作台里的东西，回跳目标就该是它。

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
//   - 跳到发布页：翻译工作台里没有发布按钮（发布入口在画布工作台），用户点了「译好了」
//     还得自己找路去发布 —— 一个本该一次点击的动作被拆成两次跳转；
//   - 异步入队：发布是慢操作（编译 + 落盘 + 站点文件刷新），入队的代价是**界面失去结果**，
//     要么加轮询、要么加通知，而这两样本项目都没有现成的形态；
//   - 同步两步（先 Build 再 Publish，与「一键发布全部语言」内部逐语言做的完全一样）：
//     结果当场可知（成功 → 回执；失败 → 错误文案，含具体原因），
//     复用的也是既有链路（含回执、依赖失效、互指刷新与回滚语义）。
//
// 代价写在明处：单语言发布会让这次请求等一次编译。这与既有的「一键发布全部语言」同量级，
// 不是新引入的行为；将来发布挪到队列 + 进度查询时，这里换成入队即可。
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

// republishLang 单语言发布 = 构建 + 激活，与 PublishAllLanguages 内部逐语言做的完全一样。
//
// 收成一个不带 gin.Context 的方法而不是在 handler 里写两段：控制器的判据是
// 「绑定 → 调业务 → 出口」，两段发布编排写在 handler 里会让它超过语句预算，
// 也会让「单语言发布与一键发布同源」这条事实分散到两个位置。
func (h *pagesAdminHandle) republishLang(ctx context.Context, pageID, lang string) error {
	if _, err := h.pages.Build(ctx, &pagedto.BuildReq{ID: pageID, Lang: lang}); err != nil {
		return err
	}
	_, err := h.pages.Publish(ctx, &pagedto.PublishReq{ID: pageID, Lang: lang})
	return err
}

// applyPageLangChange 两个写入口的共同骨架：取参 → 调用 service → 出口。
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

// pageLangStatesOf 本页各启用语言的排除 / 发布状态（工作台「产出范围」区块用）。
//
// 读失败不阻断工作台：给空切片 + 一条归口事实 —— 产出范围读不到时，
// 用户仍能翻译与保存（那是本页的主职能），只是看不到这一门语言该不该产出。
// 归口而不是透出驱动原文：这里拿不到业务错误（PageLangStates 只回驱动错误 / 工程缺失），
// 原文进日志，页面上只给一句「读取失败」。
func (h *pagesAdminHandle) pageLangStatesOf(ctx context.Context, pageID string) ([]pagedto.PageLangResp, pageMsg) {
	if h.pages == nil {
		return []pagedto.PageLangResp{}, pageMsg{}
	}
	states, err := h.pages.PageLangStates(ctx, pageID)
	if err != nil {
		logger.Scene("page").With("pageId", pageID).Error(err, "读取页面语言产出状态失败")
		return []pagedto.PageLangResp{}, pageInternalMsg()
	}
	rows := make([]pagedto.PageLangResp, 0, len(states))
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
		// 重新发布是**显式**动作：恢复排除只解除限制、不自动上线 —— 译好了要真的
		// 回到线上，用户需要一个一次点击的入口，而不是自己去找发布入口。
		row.CanRepublish = !st.Excluded
		rows = append(rows, row)
	}
	return rows, pageMsg{}
}
