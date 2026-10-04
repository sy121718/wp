// ai_session_page.go — 后台「AI 会话」页：会话列表 + 详情（投影 + 事件日志）+ 折叠操作。
//
// 与配置面的 ai_page.go 分开：命名独立（SessionPageHandle），共用同包的小工具
// （facingQuery / aiErrKey / redirectWhere / parseInt64 / userID），不引入配置面的任何状态。
//
// 写操作全部走 PRG：POST → 303 → GET（?id=/?done=/?err=）。回执里的 done/err 放的是
// **i18n key**（不是译文），渲染时再由 facingQuery 按请求语言翻译 —— 译文不能进 query，
// 否则换语言重放同一 URL 会得到另一种语言的文字，缓存与日志也跟着脏。
package aihttp

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/internal/web/shell"
)

const (
	sessionTemplate = "admin/ai/sessions"
	sessionPath     = "/admin/ai/sessions"
	sessionPageSize = 20
	sessionEventTop = 50
)

// SessionPageHandle 会话管理页处理器。
type SessionPageHandle struct{ svc *aiservice.SessionService }

// NewSessionPageHandle 构造。
func NewSessionPageHandle(svc *aiservice.SessionService) *SessionPageHandle {
	return &SessionPageHandle{svc: svc}
}

// SessionsPage GET /admin/ai/sessions：会话列表；带 ?id= 时同时渲染详情。
func (h *SessionPageHandle) SessionsPage(c *gin.Context) {
	ctx := c.Request.Context()
	page := sessionIntOr(c.DefaultQuery("page", "1"), 1)
	if page < 1 {
		page = 1
	}
	status := sessionIntOr(c.DefaultQuery("status", "-1"), -1)
	keyword := strings.TrimSpace(c.Query("keyword"))

	rows, total, err := h.svc.ListSessions(ctx, keyword, status, page, sessionPageSize)
	if err != nil {
		shell.PageError(c, "ai", err)
		return
	}
	data := shell.Prepare(c, gin.H{
		"Rows":    rows,
		"Total":   total,
		"Page":    page,
		"Keyword": keyword,
		"Status":  status,
		"Err":     facingQuery(c, "err"),
		"Done":    facingQuery(c, "done"),
	})

	if id := parseInt64(c.Query("id")); id > 0 {
		detail, derr := h.svc.GetSession(ctx, id)
		if derr != nil {
			data["DetailErr"] = aiErrText(c, derr)
		} else {
			data["Detail"] = detail
			if events, _, eerr := h.svc.ListEvents(ctx, id, 1, sessionEventTop); eerr == nil {
				data["Events"] = events
			}
			// 折叠建议：页面上直接给出「哪段值得折、能省多少」，避免用户凭感觉填序号。
			if plan, perr := h.svc.FoldPlan(ctx, aidto.FoldPlanReq{SessionID: id}); perr == nil {
				data["FoldPlan"] = plan
			}
		}
	}
	c.HTML(http.StatusOK, sessionTemplate, data)
}

// SessionAppend POST /admin/ai/sessions/append：给指定会话追加一条事件。
func (h *SessionPageHandle) SessionAppend(c *gin.Context) {
	id := parseInt64(c.PostForm("sessionId"))
	res, err := h.svc.AppendEvent(c.Request.Context(), aidto.AppendEventReq{
		SessionID: id,
		Kind:      strings.TrimSpace(c.PostForm("kind")),
		Content:   strings.TrimSpace(c.PostForm("content")),
		UserID:    userID(c),
	})
	if err != nil {
		h.redirectSession(c, id, "err", aiErrKey(err))
		return
	}
	h.redirectSession(c, res.Session.ID, "done", aienums.MsgSessionAppended)
}

// SessionRename POST /admin/ai/sessions/rename：只改标题（乐观锁版本随表单带回）。
func (h *SessionPageHandle) SessionRename(c *gin.Context) {
	id := parseInt64(c.PostForm("sessionId"))
	_, err := h.svc.RenameSession(c.Request.Context(), aidto.RenameSessionReq{
		ID:      id,
		Title:   strings.TrimSpace(c.PostForm("title")),
		Status:  -1,
		Version: parseInt64(c.PostForm("version")),
		UserID:  userID(c),
	})
	if err != nil {
		h.redirectSession(c, id, "err", aiErrKey(err))
		return
	}
	h.redirectSession(c, id, "done", aienums.MsgSessionRenamed)
}

// SessionArchive POST /admin/ai/sessions/archive：归档或恢复（表单 status=1 表示恢复）。
func (h *SessionPageHandle) SessionArchive(c *gin.Context) {
	id := parseInt64(c.PostForm("sessionId"))
	status := int16(aienums.SessionArchived)
	if c.PostForm("status") == "1" {
		status = int16(aienums.SessionActive)
	}
	_, err := h.svc.RenameSession(c.Request.Context(), aidto.RenameSessionReq{
		ID:      id,
		Status:  status,
		Version: parseInt64(c.PostForm("version")),
		UserID:  userID(c),
	})
	if err != nil {
		h.redirectSession(c, id, "err", aiErrKey(err))
		return
	}
	h.redirectSession(c, id, "done", aienums.MsgSessionRenamed)
}

// SessionFold POST /admin/ai/sessions/fold：提交一次折叠。
func (h *SessionPageHandle) SessionFold(c *gin.Context) {
	id := parseInt64(c.PostForm("sessionId"))
	_, err := h.svc.Fold(c.Request.Context(), aidto.FoldReq{
		SessionID: id,
		FromSeq:   parseInt64(c.PostForm("fromSeq")),
		ToSeq:     parseInt64(c.PostForm("toSeq")),
		Summary:   strings.TrimSpace(c.PostForm("summary")),
		UserID:    userID(c),
	})
	if err != nil {
		h.redirectSession(c, id, "err", aiErrKey(err))
		return
	}
	h.redirectSession(c, id, "done", aienums.MsgSessionFolded)
}

// redirectSession 回会话页（带 ?id= 与回执槽）；HTMX 与原生走 redirectWhere 的两条口径。
func (h *SessionPageHandle) redirectSession(c *gin.Context, id int64, slot, text string) {
	q := url.Values{}
	if id > 0 {
		q.Set("id", strconv.FormatInt(id, 10))
	}
	if strings.TrimSpace(text) != "" {
		q.Set(slot, text)
	}
	target := sessionPath
	if enc := q.Encode(); enc != "" {
		target += "?" + enc
	}
	redirectWhere(c, target)
}
