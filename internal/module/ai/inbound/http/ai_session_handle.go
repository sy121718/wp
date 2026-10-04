// ai_session_handle.go — 会话层的 JSON 出口（事件追加、投影详情、折叠）。
//
// 与配置面的 ai_handle.go 分开：那是「供应商与模型目录」，这是「会话与事件日志」，
// 两个聚合的消费方与权限面都不一样（本文件按会话资源声明权限点：ai:session_* 一族）。
package aihttp

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
	aiservice "go_wp/internal/module/ai/service"
	"go_wp/pkg/response"
)

// SessionHandle 会话层的 JSON 出口。
type SessionHandle struct{ svc *aiservice.SessionService }

// NewSessionHandle 构造。
func NewSessionHandle(svc *aiservice.SessionService) *SessionHandle { return &SessionHandle{svc: svc} }

// sessionAppendBody 追加事件的请求体。
type sessionAppendBody struct {
	SessionKey  string         `json:"sessionKey"`
	SessionID   int64          `json:"sessionId"`
	Kind        string         `json:"kind"`
	Content     string         `json:"content"`
	Tokens      int64          `json:"tokens"`
	Meta        map[string]any `json:"meta"`
	Title       string         `json:"title"`
	ProviderKey string         `json:"providerKey"`
	ModelID     string         `json:"modelId"`
}

// sessionRenameBody 改标题 / 换模型 / 归档的请求体。
type sessionRenameBody struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	ProviderKey string `json:"providerKey"`
	ModelID     string `json:"modelId"`
	// Status 缺省为 -1（不改状态）；0 归档、1 恢复。
	Status  *int16 `json:"status"`
	Version int64  `json:"version"`
}

// sessionFoldPlanBody 折叠建议的请求体。
type sessionFoldPlanBody struct {
	SessionID  int64 `json:"sessionId"`
	KeepRecent int   `json:"keepRecent"`
}

// sessionFoldBody 提交折叠的请求体。
type sessionFoldBody struct {
	SessionID     int64  `json:"sessionId"`
	FromSeq       int64  `json:"fromSeq"`
	ToSeq         int64  `json:"toSeq"`
	Summary       string `json:"summary"`
	SummaryTokens int64  `json:"summaryTokens"`
}

// List GET /api/ai/session/list：分页列出会话。
func (h *SessionHandle) List(c *gin.Context) {
	page := sessionIntOr(c.DefaultQuery("page", "1"), 1)
	size := sessionIntOr(c.DefaultQuery("size", "20"), 20)
	status := sessionIntOr(c.DefaultQuery("status", "-1"), -1)
	rows, total, err := h.svc.ListSessions(c.Request.Context(), c.Query("keyword"), status, page, size)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.Success(c, gin.H{"items": rows, "total": total, "page": page, "size": size})
}

// Get GET /api/ai/session/get：会话详情（会话头 + 投影后的当前上下文）。
func (h *SessionHandle) Get(c *gin.Context) {
	id, err := bindID(c)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	detail, err := h.svc.GetSession(c.Request.Context(), id)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.Success(c, detail)
}

// Events GET /api/ai/session/events：原始事件日志（压缩后的原文仍在这里，供展开与审计）。
func (h *SessionHandle) Events(c *gin.Context) {
	id, err := bindID(c)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	page := sessionIntOr(c.DefaultQuery("page", "1"), 1)
	size := sessionIntOr(c.DefaultQuery("size", "50"), 50)
	rows, total, err := h.svc.ListEvents(c.Request.Context(), id, page, size)
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.Success(c, gin.H{"items": rows, "total": total})
}

// Append POST /api/ai/session/append：追加一条事件（会话层唯一的写入口）。
func (h *SessionHandle) Append(c *gin.Context) {
	var body sessionAppendBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	res, err := h.svc.AppendEvent(c.Request.Context(), aidto.AppendEventReq{
		SessionKey:  body.SessionKey,
		SessionID:   body.SessionID,
		Kind:        body.Kind,
		Content:     body.Content,
		Tokens:      body.Tokens,
		Meta:        body.Meta,
		Title:       body.Title,
		ProviderKey: body.ProviderKey,
		ModelID:     body.ModelID,
		UserID:      userID(c),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgSessionAppended, res)
}

// Rename POST /api/ai/session/rename：改标题 / 换当前模型（乐观锁）。
func (h *SessionHandle) Rename(c *gin.Context) {
	var body sessionRenameBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	status := int16(-1)
	if body.Status != nil {
		status = *body.Status
	}
	res, err := h.svc.RenameSession(c.Request.Context(), aidto.RenameSessionReq{
		ID:          body.ID,
		Title:       body.Title,
		ProviderKey: body.ProviderKey,
		ModelID:     body.ModelID,
		Status:      status,
		Version:     body.Version,
		UserID:      userID(c),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgSessionRenamed, res)
}

// Archive POST /api/ai/session/archive：归档（status=0）。
//
// 单独开一个端点而不是让调用方自己拼 status：归档是「只往前、不回头地停写」这个语义，
// 把它做成具名动作，接口读起来才是那个意思。
func (h *SessionHandle) Archive(c *gin.Context) {
	var body sessionRenameBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	archived := int16(aienums.SessionArchived)
	body.Status = &archived
	res, err := h.svc.RenameSession(c.Request.Context(), aidto.RenameSessionReq{
		ID:      body.ID,
		Status:  archived,
		Version: body.Version,
		UserID:  userID(c),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgSessionRenamed, res)
}

// FoldPlan POST /api/ai/session/fold/plan：算建议折叠区间与净收益（只读）。
func (h *SessionHandle) FoldPlan(c *gin.Context) {
	var body sessionFoldPlanBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	plan, err := h.svc.FoldPlan(c.Request.Context(), aidto.FoldPlanReq{
		SessionID:  body.SessionID,
		KeepRecent: body.KeepRecent,
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.Success(c, plan)
}

// Fold POST /api/ai/session/fold：提交一次折叠（区间 + 摘要）。
func (h *SessionHandle) Fold(c *gin.Context) {
	var body sessionFoldBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.ErrorWithMessage(c, http.StatusBadRequest, aienums.ErrInvalidParam)
		return
	}
	res, err := h.svc.Fold(c.Request.Context(), aidto.FoldReq{
		SessionID:     body.SessionID,
		FromSeq:       body.FromSeq,
		ToSeq:         body.ToSeq,
		Summary:       body.Summary,
		SummaryTokens: body.SummaryTokens,
		UserID:        userID(c),
	})
	if err != nil {
		response.ErrorAuto(c, http.StatusBadRequest, "ai", err)
		return
	}
	response.SuccessWithMessage(c, aienums.MsgSessionFolded, res)
}

// sessionIntOr 解析整数，失败时回落默认值（查询参数不受信）。
func sessionIntOr(raw string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return def
	}
	return v
}
