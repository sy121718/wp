package dashboardhttp

// history_handle.go — 修订历史面板的服务端渲染（HTMX 化，docs/09 §3）。
//
// 背景：workbench.js 的 loadHistory 用 DOM 拼列表 + 每行绑恢复事件。
// 本文件把列表渲染搬到服务端；恢复动作也服务端化（查修订 → 覆盖保存草稿），
// 客户端只需确认与整页刷新。
//
// 端点：
//   POST /workbench/history         → 修订列表片段
//   POST /workbench/history/restore → 恢复指定版本（覆盖草稿）

import (
	"net/http"
	"strconv"
	"strings"

	pagecontract "go_wp/internal/module/page/contract"
	"go_wp/pkg/response"

	"github.com/gin-gonic/gin"
)

// historyRowView 修订历史行视图。
type historyRowView struct {
	Version   int64
	Path      string
	CreatedAt string
}

// HistoryPanel 渲染修订历史列表片段。
func (h *Handle) HistoryPanel(c *gin.Context) {
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	// Revisions 用空切片而非 nil：Jet 的 len() 不接受 nil（会渲染失败）。
	data := gin.H{"Revisions": []historyRowView{}, "Error": ""}
	if pageID != "" && h.pages != nil {
		revs, err := h.pages.ListRevisions(c.Request.Context(), &pagecontract.RevisionReq{PageID: pageID})
		if err != nil {
			data["Error"] = "加载失败"
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
func (h *Handle) HistoryRestore(c *gin.Context) {
	ctx := c.Request.Context()
	pageID := strings.TrimSpace(c.PostForm("pageId"))
	version, err := strconv.ParseInt(strings.TrimSpace(c.PostForm("version")), 10, 64)
	if pageID == "" || err != nil || h.pages == nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, "参数错误")
		return
	}
	revs, err := h.pages.ListRevisions(ctx, &pagecontract.RevisionReq{PageID: pageID})
	if err != nil {
		response.ErrorWithMessage(c, http.StatusInternalServerError, "加载修订失败")
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
		response.ErrorWithMessage(c, http.StatusNotFound, "修订版本不存在")
		return
	}
	page, err := h.pageOf(c, pageID)
	if err != nil {
		response.ErrorWithMessage(c, http.StatusNotFound, "页面不存在")
		return
	}
	res, err := h.pages.SaveDraft(ctx, &pagecontract.SaveDraftReq{
		ID: pageID, ExpectedVersion: page.DraftVersion,
		DraftPath: target.DraftPath, DraftDocument: target.DraftDocument,
	})
	if err != nil {
		response.ErrorWithMessage(c, http.StatusConflict, "恢复失败（草稿可能已被其他会话修改）")
		return
	}
	response.Success(c, gin.H{"draftVersion": res.DraftVersion})
}
